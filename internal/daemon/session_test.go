package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type fakeRunner struct {
	mu        sync.Mutex
	history   []llm.Message
	turn      func(context.Context, string, bool) (string, error)
	historyFn func() []llm.Message
	closed    atomic.Bool
	calls     atomic.Int32
	closeFn   func()
}

type titleRunner struct {
	*fakeRunner
	title    string
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (r *titleRunner) GenerateTitle(ctx context.Context) (string, llm.Usage, error) {
	if r.started != nil {
		close(r.started)
	}
	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return "", llm.Usage{}, ctx.Err()
		}
	}
	if r.finished != nil {
		close(r.finished)
	}
	return r.title, llm.Usage{PromptTokens: 2, CompletionTokens: 1}, nil
}

func (r *fakeRunner) Turn(ctx context.Context, input string, authored bool, started func(), _ func(string)) (string, error) {
	r.calls.Add(1)
	if started != nil {
		started()
	}
	output, err := input, error(nil)
	if r.turn != nil {
		output, err = r.turn(ctx, input, authored)
	}
	r.mu.Lock()
	r.history = append(r.history, llm.Message{Role: "user", Content: input, Authored: authored})
	if output != "" {
		r.history = append(r.history, llm.Message{Role: "assistant", Content: output})
	}
	r.mu.Unlock()
	return output, err
}

func (r *fakeRunner) History() []llm.Message {
	if r.historyFn != nil {
		return r.historyFn()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]llm.Message(nil), r.history...)
}

func (r *fakeRunner) Close() {
	r.closed.Store(true)
	if r.closeFn != nil {
		r.closeFn()
	}
}

type fakeCloser struct{ closed atomic.Bool }

func (c *fakeCloser) Close() { c.closed.Store(true) }

type bindErrorRunner struct {
	fakeRunner
	err error
}

func (r *bindErrorRunner) bind(*Session) error { return r.err }

type fakeMCP struct {
	fakeCloser
	processes *capability.ProcessManager
	rootID    string
	cwd       string
}

func (m *fakeMCP) SetProcessOptions(processes *capability.ProcessManager, rootID, cwd string, _ map[string]string) {
	m.processes, m.rootID, m.cwd = processes, rootID, cwd
}

type panicLifecycle struct {
	fakeCloser
	launcher func(string, func()) bool
}

func (l *panicLifecycle) SetLauncher(launcher func(string, func()) bool) { l.launcher = launcher }
func (l *panicLifecycle) Start(context.Context) {
	l.launcher("MCP lifecycle", func() { panic("MCP exploded") })
}

func openStore(t *testing.T, path string) *session.Store {
	t.Helper()
	store, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func createRoot(t *testing.T, store *session.Store) string {
	t.Helper()
	rootID, err := store.Create(session.SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	return rootID
}

func waitReceipt(t *testing.T, receipt *Receipt) Completion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	completion, err := receipt.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return completion
}

func TestAutomaticTitlePublishesUpdateAndCannotOverwriteRename(t *testing.T) {
	t.Run("publishes title", func(t *testing.T) {
		store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
		rootID := createRoot(t, store)
		runner := &titleRunner{fakeRunner: &fakeRunner{}, title: "Worker Queue Investigation", finished: make(chan struct{})}
		value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
			return Components{Runner: runner}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = value.Close() })
		root, err := value.Open(rootID)
		if err != nil {
			t.Fatal(err)
		}
		if result := clientCommand(t, root, "tui", "autotitle", "session.autotitle", map[string]bool{"enabled": true}); result.Status != "succeeded" {
			t.Fatalf("enable automatic title=%+v", result)
		}
		receipt, err := root.Submit(t.Context(), "Investigate flaky workers")
		if err != nil {
			t.Fatal(err)
		}
		waitReceipt(t, receipt)
		select {
		case <-runner.finished:
		case <-time.After(time.Second):
			t.Fatal("automatic title did not finish")
		}
		var meta session.Meta
		deadline := time.Now().Add(time.Second)
		for meta.Title != runner.title && time.Now().Before(deadline) {
			meta, _, err = store.Load(rootID)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
		if meta.Title != runner.title {
			t.Fatalf("automatic title=%q", meta.Title)
		}
		events, _, err := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, event := range events {
			found = found || event.Kind == "session.title.updated"
		}
		if !found {
			t.Fatal("automatic title update event was not published")
		}
	})

	t.Run("explicit rename wins", func(t *testing.T) {
		store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
		rootID := createRoot(t, store)
		runner := &titleRunner{
			fakeRunner: &fakeRunner{}, title: "Generated Too Late",
			started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
		}
		value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
			return Components{Runner: runner}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = value.Close() })
		root, err := value.Open(rootID)
		if err != nil {
			t.Fatal(err)
		}
		clientCommand(t, root, "tui", "autotitle", "session.autotitle", map[string]bool{"enabled": true})
		receipt, err := root.Submit(t.Context(), "Investigate flaky workers")
		if err != nil {
			t.Fatal(err)
		}
		waitReceipt(t, receipt)
		select {
		case <-runner.started:
		case <-time.After(time.Second):
			t.Fatal("automatic title did not start")
		}
		if result := clientCommand(t, root, "tui", "rename", "session.rename", map[string]string{"args": "Explicit Name"}); result.Status != "succeeded" {
			t.Fatalf("rename=%+v", result)
		}
		close(runner.release)
		select {
		case <-runner.finished:
		case <-time.After(time.Second):
			t.Fatal("automatic title did not finish")
		}
		time.Sleep(5 * time.Millisecond)
		meta, _, err := store.Load(rootID)
		if err != nil || meta.Title != "Explicit Name" {
			t.Fatalf("title after rename race=%q err=%v", meta.Title, err)
		}
	})
}

func TestConcurrentSubmitUsesCommittedInboxOrder(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	var mu sync.Mutex
	var executed []string
	runner := &fakeRunner{turn: func(_ context.Context, input string, authored bool) (string, error) {
		if !authored {
			t.Errorf("submit %q was not authored", input)
		}
		mu.Lock()
		executed = append(executed, input)
		mu.Unlock()
		return "done " + input, nil
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	const posts = 24
	type admitted struct {
		text    string
		receipt *Receipt
	}
	admissions := make(chan admitted, posts)
	var writers sync.WaitGroup
	for i := range posts {
		writers.Go(func() {
			text := fmt.Sprintf("post-%02d", i)
			receipt, err := root.Submit(context.Background(), text)
			if err != nil {
				t.Errorf("submit: %v", err)
				return
			}
			admissions <- admitted{text: text, receipt: receipt}
		})
	}
	writers.Wait()
	close(admissions)

	bySequence := make(map[int64]string, posts)
	var receipts []*Receipt
	for admission := range admissions {
		bySequence[admission.receipt.Sequence] = admission.text
		receipts = append(receipts, admission.receipt)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Sequence < receipts[j].Sequence })
	for i, receipt := range receipts {
		if receipt.Sequence != int64(i+1) {
			t.Fatalf("receipt %d sequence=%d", i, receipt.Sequence)
		}
		if completion := waitReceipt(t, receipt); completion.Err != nil {
			t.Fatalf("sequence %d: %v", receipt.Sequence, completion.Err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != posts {
		t.Fatalf("executed %d posts", len(executed))
	}
	for i, text := range executed {
		if want := bySequence[int64(i+1)]; text != want {
			t.Fatalf("execution %d=%q, want committed sequence text %q", i+1, text, want)
		}
	}
}

func TestRootsRunConcurrentlyAndCallerCancellationDoesNotOwnWork(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootIDs := []string{createRoot(t, store), createRoot(t, store)}
	started := make(chan string, 2)
	release := make(chan struct{})
	runners := map[string]*fakeRunner{}
	for _, rootID := range rootIDs {
		id := rootID
		runners[id] = &fakeRunner{turn: func(ctx context.Context, input string, _ bool) (string, error) {
			started <- id
			select {
			case <-release:
				return input, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}}
	}
	daemon, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		runners[meta.ID].history = append([]llm.Message(nil), history...)
		return Components{Runner: runners[meta.ID]}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	first, _ := daemon.Open(rootIDs[0])
	second, _ := daemon.Open(rootIDs[1])

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	firstReceipt, err := first.Submit(callerCtx, "first")
	if err != nil {
		t.Fatal(err)
	}
	secondReceipt, err := second.Submit(context.Background(), "second")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatal("roots did not enter turns concurrently")
		}
	}
	if len(seen) != 2 {
		t.Fatalf("started roots=%v", seen)
	}
	cancelCaller()
	if _, err := firstReceipt.Wait(callerCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observer error=%v", err)
	}
	close(release)
	if completion := waitReceipt(t, firstReceipt); completion.Err != nil {
		t.Fatalf("detached work: %v", completion.Err)
	}
	if completion := waitReceipt(t, secondReceipt); completion.Err != nil {
		t.Fatalf("second root: %v", completion.Err)
	}
}

func TestUnobservedCompletionCannotBlockActor(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, _ := daemon.Open(rootID)
	if _, err := root.Submit(context.Background(), "ignored"); err != nil {
		t.Fatal(err)
	}
	observed, err := root.Submit(context.Background(), "observed")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, observed); completion.Output != "observed" || completion.Err != nil {
		t.Fatalf("second completion=%+v", completion)
	}
}

func TestSteerWhileIdleRunsAsAuthoredTurn(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, _ := daemon.Open(rootID)
	steer, err := root.Steer(context.Background(), "boundary")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, steer); completion.Err != nil || completion.Output != "boundary" {
		t.Fatalf("idle steer completion=%+v", completion)
	}
	history := runner.History()
	if len(history) < 1 || history[0].Content != "boundary" || !history[0].Authored {
		t.Fatalf("idle steer history=%+v", history)
	}
}

func TestSteerDuringTurnRunsAsNextTurn(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	started := make(chan struct{})
	release := make(chan struct{})
	inputs := make(chan string, 2)
	runner := &fakeRunner{turn: func(ctx context.Context, input string, _ bool) (string, error) {
		inputs <- input
		if input == "turn" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return input, nil
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, _ := daemon.Open(rootID)
	turn, _ := root.Submit(context.Background(), "turn")
	<-started
	steer, _ := root.Steer(context.Background(), "next")
	close(release)
	if completion := waitReceipt(t, turn); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	if completion := waitReceipt(t, steer); completion.Output != "next" || completion.Err != nil {
		t.Fatalf("steer completion=%+v", completion)
	}
	if first, second := <-inputs, <-inputs; first != "turn" || second != "next" {
		t.Fatalf("turn inputs=%q, %q", first, second)
	}
}

func TestReturnedTurnErrorIsRecoverable(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{turn: func(_ context.Context, input string, _ bool) (string, error) {
		if input == "recoverable" {
			return "", errors.New("try again")
		}
		return "recovered", nil
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, _ := daemon.Open(rootID)
	first, err := root.Submit(context.Background(), "recoverable")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, first); completion.Err == nil || completion.Err.Error() != "try again" {
		t.Fatalf("turn error=%v", completion.Err)
	}
	select {
	case <-root.Done():
		t.Fatal("returned turn error stopped the root")
	default:
	}
	second, err := root.Submit(context.Background(), "next")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, second); completion.Output != "recovered" || completion.Err != nil {
		t.Fatalf("next completion=%+v", completion)
	}
}

func TestGoalContinuationRunsWithoutClientAndClearsGoal(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	if err := store.SetGoal(rootID, "ship it"); err != nil {
		t.Fatal(err)
	}
	continued := make(chan bool, 1)
	runner := &fakeRunner{turn: func(_ context.Context, input string, authored bool) (string, error) {
		if input == "start" {
			return "still working", nil
		}
		if !strings.Contains(input, "ship it") {
			t.Errorf("goal continuation=%q", input)
		}
		continued <- authored
		return "GOAL_MET - verified", nil
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner, GoalMaxRounds: 2}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, _ := daemon.Open(rootID)
	receipt, _ := root.Submit(context.Background(), "start")
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	select {
	case authored := <-continued:
		if authored {
			t.Fatal("goal continuation was authored")
		}
	case <-time.After(time.Second):
		t.Fatal("goal did not continue after client-independent completion")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		meta, _, err := store.Load(rootID)
		if err == nil && meta.Goal == "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("completed goal remained persisted")
}

func TestWorkerAndActorPanicsAreRootLocalAndAwaitCleanup(t *testing.T) {
	for _, test := range []struct {
		name   string
		runner *fakeRunner
		want   string
	}{
		{name: "worker", runner: &fakeRunner{turn: func(context.Context, string, bool) (string, error) {
			panic("worker exploded")
		}}, want: "worker exploded"},
		{name: "actor", runner: &fakeRunner{historyFn: func() []llm.Message {
			panic("actor exploded")
		}}, want: "actor exploded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			badID, goodID := createRoot(t, store), createRoot(t, store)
			goodRunner := &fakeRunner{}
			mcp := &fakeCloser{}
			daemon, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
				if meta.ID == badID {
					return Components{Runner: test.runner, MCP: mcp}, nil
				}
				return Components{Runner: goodRunner}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = daemon.Close() })
			bad, _ := daemon.Open(badID)
			good, _ := daemon.Open(goodID)

			descendantStarted := make(chan struct{})
			descendantExited := make(chan struct{})
			if err := bad.supervisor.launch("descendant", func(ctx context.Context) workerCompletion {
				close(descendantStarted)
				<-ctx.Done()
				close(descendantExited)
				return workerCompletion{}
			}); err != nil {
				t.Fatal(err)
			}
			<-descendantStarted
			processStopped := make(chan struct{})
			unregister, err := store.Processes().RegisterStop(badID, func() error {
				close(processStopped)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer unregister()

			if _, err := bad.Submit(context.Background(), "panic"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-bad.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("failed root did not stop")
			}
			for name, ch := range map[string]<-chan struct{}{
				"descendant": descendantExited,
				"process":    processStopped,
			} {
				select {
				case <-ch:
				default:
					t.Errorf("%s was not awaited before root completion", name)
				}
			}
			if !test.runner.closed.Load() || !mcp.closed.Load() {
				t.Fatal("runner and MCP were not closed before root completion")
			}
			if err := bad.Err(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("root error=%v", err)
			}
			goodReceipt, err := good.Submit(context.Background(), "still alive")
			if err != nil {
				t.Fatal(err)
			}
			if completion := waitReceipt(t, goodReceipt); completion.Err != nil {
				t.Fatalf("healthy root failed: %v", completion.Err)
			}
		})
	}
}

func TestMCPLifecyclePanicFailsOnlyItsRoot(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	badID, goodID := createRoot(t, store), createRoot(t, store)
	lifecycle := &panicLifecycle{}
	daemon, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		if meta.ID == badID {
			return Components{Runner: &fakeRunner{}, MCP: lifecycle}, nil
		}
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	bad, openErr := daemon.Open(badID)
	if openErr == nil {
		select {
		case <-bad.Done():
		case <-time.After(time.Second):
			t.Fatal("MCP panic did not stop root")
		}
		openErr = bad.Err()
	}
	if openErr == nil || !strings.Contains(openErr.Error(), "MCP exploded") {
		t.Fatalf("MCP root error=%v", openErr)
	}
	if !lifecycle.closed.Load() {
		t.Fatal("failed MCP lifecycle was not closed")
	}
	good, err := daemon.Open(goodID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := good.Submit(context.Background(), "still alive")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("healthy root failed: %v", err)
	}
}

func TestToolPanicReentersRootSupervisor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"panic-tool","type":"function","function":{"name":"explode","arguments":"{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	ag := agent.NewRuntime(llm.New(server.URL, "key"), "model", 100, "system", tools.NewServices())
	ag.Tools = append(ag.Tools, tools.Tool{
		Def: llm.NewTool("explode", "panic", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) { panic("tool exploded") },
	})
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &AgentSession{agent: ag}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Submit(context.Background(), "run it"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-root.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("tool panic escaped supervision")
	}
	if err := root.Err(); err == nil || !strings.Contains(err.Error(), "tool exploded") {
		t.Fatalf("tool panic error=%v", err)
	}
}

func TestAgentStreamEventsAreDurableOrderedAndSnapshotRestorable(t *testing.T) {
	firstChunk := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"hel"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		close(firstChunk)
		<-release
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	agentValue := agent.NewRuntime(llm.New(server.URL, "key"), "model", 100, "system", tools.NewServices())
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &AgentSession{agent: agentValue}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := root.Submit(t.Context(), "answer")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstChunk:
	case <-time.After(time.Second):
		t.Fatal("provider did not send its first stream chunk")
	}

	deadline := time.Now().Add(time.Second)
	var streamSequence int64
	for streamSequence == 0 && time.Now().Before(deadline) {
		events, _, replayErr := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
		if replayErr != nil {
			t.Fatal(replayErr)
		}
		for _, event := range events {
			if event.Kind == "stream.text" {
				streamSequence = event.Seq
			}
		}
		if streamSequence == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if streamSequence == 0 {
		t.Fatal("stream event did not cross the actor mailbox")
	}
	snapshot, err := root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Presentation) != 1 || snapshot.Presentation[0].Seq != streamSequence {
		t.Fatalf("in-progress presentation = %+v", snapshot.Presentation)
	}
	var streamed StreamEvent
	if err := json.Unmarshal(snapshot.Presentation[0].Payload, &streamed); err != nil || streamed.Text != "hel" {
		t.Fatalf("snapshot stream = %+v, %v", streamed, err)
	}

	close(release)
	if completion := waitReceipt(t, receipt); completion.Err != nil || completion.Output != "hello" {
		t.Fatalf("turn completion = %+v", completion)
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
	if err != nil {
		t.Fatal(err)
	}
	var terminalSequence int64
	for _, event := range events {
		if event.Kind == "turn.succeeded" {
			terminalSequence = event.Seq
		}
	}
	if terminalSequence <= streamSequence {
		t.Fatalf("stream seq=%d terminal seq=%d events=%+v", streamSequence, terminalSequence, events)
	}
	snapshot, err = root.Snapshot(t.Context())
	if err != nil || len(snapshot.Presentation) != 0 {
		t.Fatalf("settled presentation = %+v, %v", snapshot.Presentation, err)
	}
}

func TestOpenBindsMCPProcesses(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	ag := agent.NewRuntime(llm.New("http://unused", "key"), "model", 100, "system", tools.NewServices())
	manager := &fakeMCP{}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &AgentSession{agent: ag}, MCP: manager}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if manager.processes != store.Processes() || manager.rootID != rootID || manager.cwd != root.meta.CWD {
		t.Fatalf("MCP process scope=%p %q %q", manager.processes, manager.rootID, manager.cwd)
	}
	if len(ag.AllTools()) != 0 {
		t.Fatal("MCP process setup changed the model-facing tool surface")
	}
}

func TestStopAwaitsWorkerAndUnblocksReceipt(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	started := make(chan struct{})
	exited := make(chan struct{})
	runner := &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) {
		close(started)
		<-ctx.Done()
		close(exited)
		return "", ctx.Err()
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, _ := daemon.Open(rootID)
	receipt, err := root.Submit(context.Background(), "block")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	root.Stop()
	select {
	case <-exited:
	default:
		t.Fatal("Stop returned before worker exited")
	}
	completion := waitReceipt(t, receipt)
	if !errors.Is(completion.Err, ErrStopped) {
		t.Fatalf("stopped receipt error=%v", completion.Err)
	}
	if !errors.Is(root.Err(), ErrStopped) {
		t.Fatalf("session error=%v", root.Err())
	}
	meta, history, err := store.Load(rootID)
	if err != nil || len(history) != 0 || meta.ID != rootID {
		t.Fatalf("stopped reconstruction meta=%+v history=%+v err=%v", meta, history, err)
	}
	queued, err := store.LoadQueuedInbox(context.Background(), rootID, root.authority.AgentID, 0, 10)
	if err != nil || len(queued) != 0 {
		t.Fatalf("stopped inbox replay=%+v err=%v", queued, err)
	}
	root.Stop()
}

func TestOpenSharesLiveRootWithoutHoldingRegistryLockDuringFactory(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	slowID, fastID := createRoot(t, store), createRoot(t, store)
	slowFactoryEntered := make(chan struct{})
	releaseSlowFactory := make(chan struct{})
	var calls atomic.Int32
	daemon, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		calls.Add(1)
		if meta.ID == slowID {
			close(slowFactoryEntered)
			<-releaseSlowFactory
		}
		return Components{Runner: &fakeRunner{history: append([]llm.Message(nil), history...)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })

	type opened struct {
		root *Session
		err  error
	}
	slowResult := make(chan opened, 1)
	go func() {
		root, err := daemon.Open(slowID)
		slowResult <- opened{root: root, err: err}
	}()
	<-slowFactoryEntered
	fastResult := make(chan opened, 1)
	go func() {
		root, err := daemon.Open(fastID)
		fastResult <- opened{root: root, err: err}
	}()
	select {
	case result := <-fastResult:
		if result.err != nil || result.root == nil {
			t.Fatalf("fast open=%v, %v", result.root, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("factory work held the registry lock")
	}
	close(releaseSlowFactory)
	first := <-slowResult
	if first.err != nil {
		t.Fatal(first.err)
	}
	second, err := daemon.Open(slowID)
	if err != nil {
		t.Fatal(err)
	}
	if first.root != second {
		t.Fatal("Open returned two live actors for one root")
	}
	if calls.Load() != 2 {
		t.Fatalf("factory calls=%d, want one per root", calls.Load())
	}
}

func TestDispatchWaitsForAdmissionPublication(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueInbox(context.Background(), session.InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit",
		Payload: session.RuntimePayload{Data: []byte("work")},
	}); err != nil {
		t.Fatal(err)
	}
	root := newSession(store, meta, authority, Components{Runner: &fakeRunner{}})
	t.Cleanup(root.supervisor.stop)
	root.admitMu.RLock()
	dispatched := make(chan error, 1)
	go func() { dispatched <- root.dispatch() }()
	select {
	case err := <-dispatched:
		root.admitMu.RUnlock()
		t.Fatalf("inbox was claimed before admission publication: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	root.admitMu.RUnlock()
	if err := <-dispatched; err != nil {
		t.Fatal(err)
	}
	if root.running == nil || root.running.seq == 0 {
		t.Fatalf("dispatch did not claim the queued row: %+v", root.running)
	}
}

func TestOpenCanonicalizesAliasesAndReportsStoppedRoots(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	var calls atomic.Int32
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		calls.Add(1)
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	first, err := daemon.Open(rootID[:4])
	if err != nil {
		t.Fatal(err)
	}
	second, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || calls.Load() != 1 {
		t.Fatalf("alias opened roots %p/%p with %d factories", first, second, calls.Load())
	}
	first.Stop()
	if _, err := daemon.Open(rootID); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped root open error=%v", err)
	}
}

func TestResumeActiveOpensDurableRootsAndReportsFailures(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "active.db"))
	rootID := createRoot(t, store)
	if _, err := store.AddSchedule(rootID, "@every 1h", "wake", time.Now()); err != nil {
		t.Fatal(err)
	}
	var opened atomic.Int32
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		opened.Add(1)
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.ResumeActive(context.Background()); err != nil || opened.Load() != 1 {
		t.Fatalf("resume active = %v, opened=%d", err, opened.Load())
	}
	if err := value.Close(); err != nil {
		t.Fatal(err)
	}

	failingStore := openStore(t, filepath.Join(t.TempDir(), "failing.db"))
	failingRootID := createRoot(t, failingStore)
	if _, err := failingStore.AddSchedule(failingRootID, "@every 1h", "wake", time.Now()); err != nil {
		t.Fatal(err)
	}
	factoryErr := errors.New("factory failed")
	failing, err := New(failingStore, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{}, factoryErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := failing.ResumeActive(context.Background()); !errors.Is(err, factoryErr) {
		t.Fatalf("resume factory error = %v", err)
	}
	_ = failing.Close()

	closedStore := openStore(t, filepath.Join(t.TempDir(), "closed-active.db"))
	closed, err := New(closedStore, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := closedStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closed.ResumeActive(context.Background()); err == nil {
		t.Fatal("closed store resumed active roots")
	}
	_ = closed.Close()
}

func TestSupervisorReportsWorkerPanics(t *testing.T) {
	supervisor := newSupervisor()
	want := errors.New("background failed")
	supervisor.report("background", want)
	if events := supervisor.take(); len(events) != 1 || !errors.Is(events[0].err, want) {
		t.Fatalf("reported events = %+v", events)
	}
	if !supervisor.launchWorker("panic worker", func() { panic("boom") }) {
		t.Fatal("worker did not launch")
	}
	supervisor.wait()
	events := supervisor.take()
	if len(events) != 1 || events[0].err == nil || !strings.Contains(events[0].err.Error(), "boom") {
		t.Fatalf("panic events = %+v", events)
	}
	supervisor.stop()
	if err := supervisor.launch("stopped", func(context.Context) workerCompletion { return workerCompletion{} }); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped supervisor launch = %v", err)
	}
	if supervisor.launchWorker("stopped", func() {}) {
		t.Fatal("stopped supervisor launched work")
	}
}

func TestSessionWakeAndReferencedInboxFailuresAreReported(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", session.InlineValueLimit+1)
	sequence, err := store.EnqueueInbox(context.Background(), session.InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: session.RuntimePayload{Data: []byte(large)},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, 1)
	if err != nil || len(items) != 1 || items[0].Seq != sequence.InboxSeq {
		t.Fatalf("referenced inbox = %+v, %v", items, err)
	}
	root := newSession(store, meta, authority, Components{Runner: &fakeRunner{}})
	text, err := root.inboxText(items[0])
	if err != nil || text != large {
		t.Fatalf("resolved inbox = %d bytes, %v", len(text), err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	root.enqueueWake("steer", "wake")
	events := root.supervisor.take()
	if len(events) != 1 || events[0].err == nil {
		t.Fatalf("failed wake events = %+v", events)
	}
}

func TestAgentRunnerSafeCloseEdges(t *testing.T) {
	agentValue := agent.NewRuntime(llm.New("http://unused", "key"), "model", 1, "system", tools.NewServices())
	runner := &AgentSession{agent: agentValue}
	if err := safeClose("normal", func() {}); err != nil {
		t.Fatal(err)
	}
	if err := safeClose("panic", func() { panic("close exploded") }); err == nil || !strings.Contains(err.Error(), "close exploded") {
		t.Fatalf("panic close = %v", err)
	}
	runner.Close()
}

func TestFactoryPanicSettlesOpenAndClose(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		panic("factory exploded")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Open(rootID); err == nil || !strings.Contains(err.Error(), "factory exploded") {
		t.Fatalf("factory panic error=%v", err)
	}
	done := make(chan error, 1)
	go func() { done <- daemon.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked after factory panic")
	}
}

func TestNewAndOpenValidateAndCleanUpFailedConstruction(t *testing.T) {
	factory := func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	}
	if _, err := New(nil, factory); err == nil {
		t.Fatal("nil store was accepted")
	}
	closedStore := openStore(t, filepath.Join(t.TempDir(), "closed.db"))
	if err := closedStore.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(closedStore, factory); err == nil {
		t.Fatal("closed store recovery succeeded")
	}
	if !closedStore.AcquireDaemon() {
		t.Fatal("failed recovery retained daemon ownership")
	}
	closedStore.ReleaseDaemon()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	if _, err := New(store, nil); err == nil {
		t.Fatal("nil factory was accepted")
	}
	bindRootID, missingRunnerRootID := createRoot(t, store), createRoot(t, store)
	bindErr := errors.New("bind failed")
	runner := &bindErrorRunner{err: bindErr}
	bindMCP, missingRunnerMCP := &fakeCloser{}, &fakeCloser{}
	var bindProcessStopped, missingRunnerProcessStopped atomic.Bool
	daemon, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		stopped := &bindProcessStopped
		components := Components{Runner: runner, MCP: bindMCP}
		if meta.ID == missingRunnerRootID {
			stopped = &missingRunnerProcessStopped
			components = Components{MCP: missingRunnerMCP}
		}
		if _, err := store.Processes().RegisterStop(meta.ID, func() error {
			stopped.Store(true)
			return nil
		}); err != nil {
			return Components{}, err
		}
		return components, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	if _, err := daemon.Open(""); err == nil {
		t.Fatal("empty root ID was accepted")
	}
	if _, err := daemon.Open(bindRootID); !errors.Is(err, bindErr) {
		t.Fatalf("bind error=%v", err)
	}
	if !runner.closed.Load() || !bindMCP.closed.Load() || !bindProcessStopped.Load() {
		t.Fatalf("bind cleanup runner=%v MCP=%v process=%v", runner.closed.Load(), bindMCP.closed.Load(), bindProcessStopped.Load())
	}
	if _, err := daemon.Open(missingRunnerRootID); err == nil || !strings.Contains(err.Error(), "no runner") {
		t.Fatalf("missing runner error=%v", err)
	}
	if !missingRunnerMCP.closed.Load() || !missingRunnerProcessStopped.Load() {
		t.Fatalf("missing runner cleanup MCP=%v process=%v", missingRunnerMCP.closed.Load(), missingRunnerProcessStopped.Load())
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Open(bindRootID); !errors.Is(err, ErrClosed) {
		t.Fatalf("open after close error=%v", err)
	}
}

func TestCloseCancelsInFlightFactory(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	entered := make(chan struct{})
	daemon, err := New(store, func(ctx context.Context, _ session.Meta, _ []llm.Message) (Components, error) {
		close(entered)
		<-ctx.Done()
		return Components{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan error, 1)
	go func() {
		_, err := daemon.Open(rootID)
		opened <- err
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- daemon.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel the root factory")
	}
	if err := <-opened; !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
		t.Fatalf("Open error=%v", err)
	}
}

func TestSupervisorCoalescesCompatibleStreamEvents(t *testing.T) {
	supervisor := newSupervisor()
	t.Cleanup(supervisor.stop)
	supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.text", event: StreamEvent{ID: "turn", Text: "hello "},
	}})
	supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.text", event: StreamEvent{ID: "turn", Text: "world"},
	}})
	if events := supervisor.take(); len(events) != 1 || events[0].stream.event.Text != "hello world" {
		t.Fatalf("coalesced text events = %+v", events)
	}
	supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.tool.call", event: StreamEvent{ID: "tool", Args: `{"partial":`},
	}})
	supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.tool.call", event: StreamEvent{ID: "tool", Args: `{"complete":true}`},
	}})
	if events := supervisor.take(); len(events) != 1 || events[0].stream.event.Args != `{"complete":true}` {
		t.Fatalf("coalesced tool events = %+v", events)
	}
	large := strings.Repeat("x", 32<<10)
	supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.reasoning", event: StreamEvent{ID: "turn", Text: large},
	}})
	supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.reasoning", event: StreamEvent{ID: "turn", Text: "overflow"},
	}})
	if events := supervisor.take(); len(events) != 2 {
		t.Fatalf("oversized stream events were coalesced: %d", len(events))
	}
}

func TestRouteControlDoesNotDeadlockDuringShutdown(t *testing.T) {
	root := &Session{supervisor: newSupervisor()}
	finished := make(chan error, 1)
	if !root.supervisor.launchWorker("control", func() {
		finished <- root.routeControl(context.Background(), func(context.Context) error { return nil })
	}) {
		t.Fatal("control worker was not launched")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		root.supervisor.mu.Lock()
		queued := len(root.supervisor.events)
		root.supervisor.mu.Unlock()
		if queued == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	root.admitMu.Lock()
	root.stopping = true
	root.admitMu.Unlock()
	root.supervisor.stop()
	if err := root.drainWorkers(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("shutdown control error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("control remained blocked after supervisor shutdown")
	}
}

func TestNewRejectsSecondDaemonOwner(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	factory := func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	}
	daemon, err := New(store, factory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	if _, err := New(store, factory); err == nil || !strings.Contains(err.Error(), "already has a daemon owner") {
		t.Fatalf("second daemon error=%v", err)
	}
}

func TestClosePreservesClaimedScheduleWake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	started := make(chan struct{})
	runner := &fakeRunner{
		turn: func(ctx context.Context, _ string, _ bool) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Submit(context.Background(), "block"); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := store.EnqueueInbox(context.Background(), session.InboxEnqueue{
		RootID: rootID, AgentID: root.authority.AgentID, Kind: "schedule",
		Payload: session.RuntimePayload{Data: []byte("scheduled wake")},
	}); err != nil {
		t.Fatal(err)
	}
	root.notify()
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	t.Cleanup(func() { _ = store.Close() })
	queued, err := store.LoadQueuedInbox(context.Background(), rootID, root.authority.AgentID, 0, 10)
	if err != nil || len(queued) != 1 || queued[0].Kind != "schedule" {
		t.Fatalf("preserved schedule=%+v err=%v", queued, err)
	}
}

func TestReopenReconstructsHistoryAndRunsQueuedSubmitOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	toolCall := llm.ToolCall{ID: "dangling"}
	toolCall.Function.Name = "read"
	history := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{toolCall}},
	}
	if err := store.Save(rootID, 0, history, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueInbox(context.Background(), session.InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit",
		Payload: session.RuntimePayload{Data: []byte("do not replay")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, path)
	runner := &fakeRunner{}
	var restored []llm.Message
	daemon, err := New(store, func(_ context.Context, _ session.Meta, history []llm.Message) (Components, error) {
		restored = append([]llm.Message(nil), history...)
		runner.history = append([]llm.Message(nil), history...)
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	if _, err := daemon.Open(rootID); err != nil {
		t.Fatal(err)
	}
	if len(restored) != 3 || restored[2].Role != "tool" || !strings.Contains(restored[2].Content, "interrupted") {
		t.Fatalf("restored history=%+v", restored)
	}
	// The interrupted tool call is not replayed; the queued submit is durable
	// work and runs exactly once after the restart.
	deadline := time.Now().Add(5 * time.Second)
	for runner.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("queued submit ran %d times after reopen", runner.calls.Load())
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		queued, err := store.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(queued) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("queued submit was not consumed after reopen")
}

func TestReopenResumesQueuedScheduleAsUnauthored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueInbox(context.Background(), session.InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "schedule",
		Payload: session.RuntimePayload{Data: []byte("scheduled wake")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, path)
	started := make(chan bool, 1)
	runner := &fakeRunner{turn: func(_ context.Context, input string, authored bool) (string, error) {
		if input != "scheduled wake" {
			t.Errorf("schedule input=%q", input)
		}
		started <- authored
		return "done", nil
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	if _, err := daemon.Open(rootID); err != nil {
		t.Fatal(err)
	}
	select {
	case authored := <-started:
		if authored {
			t.Fatal("schedule wake was marked authored")
		}
	case <-time.After(time.Second):
		t.Fatal("queued schedule did not resume")
	}
}
