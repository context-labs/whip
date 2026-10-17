package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type lifecycleTitleRunner struct {
	*fakeRunner
	generate func(context.Context, string) (string, llm.Usage, error)
	titles   atomic.Int32
}

func (r *lifecycleTitleRunner) GenerateTitle(ctx context.Context, prompt string) (string, llm.Usage, error) {
	r.titles.Add(1)
	return r.generate(ctx, prompt)
}

func openTitleLifecycle(t *testing.T, store *session.Store, rootID string, runner Runner) (*Daemon, *Session) {
	t.Helper()
	processes := newTestProcesses(t)
	owner, err := New(store, processes, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	return owner, root
}

func waitTitleLifecycle(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("title lifecycle did not reach expected state")
	}
}

func settleTitleLifecycle(t *testing.T, root *Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		idle, err := routeControlValue(root, ctx, func(context.Context) (bool, error) {
			return root.titleWork == nil && root.running == nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if idle {
			if err := root.routeControl(ctx, func(context.Context) error { return nil }); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
}

func assertTitleLifecycle(t *testing.T, store *session.Store, rootID, want string) {
	t.Helper()
	meta, _, err := store.Load(rootID)
	if err != nil || meta.Title != want {
		t.Fatalf("persisted title=%q, want %q, error=%v", meta.Title, want, err)
	}
}

func TestTitleLifecycleKeepsShortDeterministicTitles(t *testing.T) {
	for _, test := range []struct {
		name, prompt, deterministic string
		wantCalls                   int32
	}{
		{"short", "Session naming", "Session naming", 0},
		{"19 characters", strings.Repeat("x", 19), strings.Repeat("x", 19), 0},
		{"20 characters", strings.Repeat("x", 20), strings.Repeat("x", 20), 1},
		{"19 Unicode characters", strings.Repeat("界", 19), strings.Repeat("界", 19), 0},
		{"20 Unicode characters", strings.Repeat("界", 20), strings.Repeat("界", 20), 1},
		{"normalized whitespace", "  Session" + strings.Repeat(" ", 30) + "naming  ", "Session naming", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			t.Cleanup(func() { _ = store.Close() })
			rootID := createRoot(t, store)
			runner := &lifecycleTitleRunner{
				fakeRunner: &fakeRunner{},
				generate: func(context.Context, string) (string, llm.Usage, error) {
					return "Generated title", llm.Usage{}, nil
				},
			}
			_, root := openTitleLifecycle(t, store, rootID, runner)
			receipt, err := root.Submit(t.Context(), test.prompt)
			if err != nil {
				t.Fatal(err)
			}
			if result := waitReceipt(t, receipt); result.Err != nil {
				t.Fatal(result.Err)
			}
			settleTitleLifecycle(t, root)
			want := test.deterministic
			if test.wantCalls != 0 {
				want = "Generated title"
			}
			assertTitleLifecycle(t, store, rootID, want)
			// A later long message must not reconsider an intentionally short title.
			receipt, err = root.Submit(t.Context(), "A much longer follow-up message about this session")
			if err != nil {
				t.Fatal(err)
			}
			if result := waitReceipt(t, receipt); result.Err != nil {
				t.Fatal(result.Err)
			}
			settleTitleLifecycle(t, root)
			assertTitleLifecycle(t, store, rootID, want)
			if runner.titles.Load() != test.wantCalls {
				t.Fatalf("title calls = %d, want %d", runner.titles.Load(), test.wantCalls)
			}
		})
	}
}

func TestTitleLifecycleAdmissionPrecedesTurnAndDeduplicates(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	runner := &lifecycleTitleRunner{fakeRunner: &fakeRunner{}}
	runner.generate = func(ctx context.Context, prompt string) (string, llm.Usage, error) {
		if prompt != "Investigate flaky workers" {
			t.Errorf("title prompt=%q", prompt)
		}
		close(started)
		select {
		case <-release:
			return "Worker Queue Investigation", llm.Usage{}, nil
		case <-ctx.Done():
			return "", llm.Usage{}, ctx.Err()
		}
	}
	_, root := openTitleLifecycle(t, store, rootID, runner)
	t.Cleanup(unblock)
	assertTitleLifecycle(t, store, rootID, "")
	if runner.titles.Load() != 0 {
		t.Fatal("opening an unused session requested a title")
	}
	if err := root.routeControl(t.Context(), func(context.Context) error {
		root.clientBusy = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admission := session.CommandAdmission{
		ClientID: "title-client", CommandID: "first", Kind: "submit", Operation: "submit", RequestDigest: "first",
		Payload: session.RuntimePayload{Data: []byte("Investigate flaky workers")},
	}
	accepted, receipt, err := root.AdmitCommand(t.Context(), admission)
	if err != nil || !accepted.New {
		t.Fatalf("admission=%+v, error=%v", accepted, err)
	}
	assertTitleLifecycle(t, store, rootID, "Investigate flaky workers")
	waitTitleLifecycle(t, started)
	if runner.calls.Load() != 0 {
		t.Fatal("conversation ran while dispatch was blocked")
	}
	duplicate, _, err := root.AdmitCommand(t.Context(), admission)
	if err != nil || duplicate.New {
		t.Fatalf("duplicate=%+v, error=%v", duplicate, err)
	}
	unblock()
	settleTitleLifecycle(t, root)
	assertTitleLifecycle(t, store, rootID, "Worker Queue Investigation")
	if runner.calls.Load() != 0 {
		t.Fatal("generated title needed a conversation turn")
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, event := range events {
		if event.Kind == "session.title.updated" {
			var update SessionUpdateEvent
			if err := json.Unmarshal(event.Payload.Inline, &update); err != nil {
				t.Fatal(err)
			}
			titles = append(titles, update.Title)
		}
	}
	if fmt.Sprint(titles) != "[Investigate flaky workers Worker Queue Investigation]" {
		t.Fatalf("title publication order=%q", titles)
	}
	if err := root.routeControl(t.Context(), func(context.Context) error {
		root.clientBusy = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	later, err := root.Submit(t.Context(), "A later user message")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, later); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	settleTitleLifecycle(t, root)
	if runner.titles.Load() != 1 || runner.calls.Load() != 2 {
		t.Fatalf("title calls=%d, conversation calls=%d", runner.titles.Load(), runner.calls.Load())
	}
}

func TestTitleLifecycleFailureKeepsFallbackWithoutRestartRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	runner := &lifecycleTitleRunner{
		fakeRunner: &fakeRunner{},
		generate: func(context.Context, string) (string, llm.Usage, error) {
			return "", llm.Usage{}, errors.New("title provider unavailable")
		},
	}
	owner, root := openTitleLifecycle(t, store, rootID, runner)
	receipt, err := root.Submit(t.Context(), "Keep the fallback title")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatalf("title failure failed the conversation: %v", completion.Err)
	}
	settleTitleLifecycle(t, root)
	assertTitleLifecycle(t, store, rootID, "Keep the fallback title")
	if runner.titles.Load() != 1 {
		t.Fatalf("title attempts=%d", runner.titles.Load())
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	t.Cleanup(func() { _ = store.Close() })
	restarted := &lifecycleTitleRunner{fakeRunner: &fakeRunner{}, generate: runner.generate}
	_, root = openTitleLifecycle(t, store, rootID, restarted)
	receipt, err = root.Submit(t.Context(), "Do not try naming again")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	settleTitleLifecycle(t, root)
	assertTitleLifecycle(t, store, rootID, "Keep the fallback title")
	if restarted.titles.Load() != 0 {
		t.Fatalf("restart retried naming %d times", restarted.titles.Load())
	}
}

func TestTitleLifecycleRenameInvalidatesCompletedResult(t *testing.T) {
	for _, names := range [][]string{{"Original user request"}, {"Manual title", "Original user request"}} {
		t.Run(strings.Join(names, "/"), func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			t.Cleanup(func() { _ = store.Close() })
			rootID := createRoot(t, store)
			runner := &titleRunner{
				fakeRunner: &fakeRunner{}, title: "Generated Too Late",
				started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
			}
			_, root := openTitleLifecycle(t, store, rootID, runner)
			unblock := sync.OnceFunc(func() { close(runner.release) })
			t.Cleanup(unblock)
			receipt, err := root.Submit(t.Context(), "Original user request")
			if err != nil {
				t.Fatal(err)
			}
			if completion := waitReceipt(t, receipt); completion.Err != nil {
				t.Fatal(completion.Err)
			}
			waitTitleLifecycle(t, runner.started)
			if err := root.routeControl(t.Context(), func(ctx context.Context) error {
				// Finish the provider while the actor is held, then rename before it
				// can process the completed title event. Cancellation cannot save us.
				unblock()
				deadline := time.Now().Add(5 * time.Second)
				for {
					root.supervisor.mu.Lock()
					posted := false
					for _, event := range root.supervisor.events {
						posted = posted || event.kind == workerTitle
					}
					root.supervisor.mu.Unlock()
					if posted {
						break
					}
					if time.Now().After(deadline) {
						return errors.New("title result did not reach the actor")
					}
					time.Sleep(time.Millisecond)
				}
				for _, name := range names {
					raw, _ := json.Marshal(map[string]string{"title": name})
					if _, err := root.applyClientCommand(ctx, "session.rename", raw); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			settleTitleLifecycle(t, root)
			assertTitleLifecycle(t, store, rootID, "Original user request")
		})
	}
}

func TestTitleLifecycleOutlivesConversationFailureAndCancellation(t *testing.T) {
	for _, cancelTurn := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelTurn), func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			t.Cleanup(func() { _ = store.Close() })
			rootID := createRoot(t, store)
			turnStarted := make(chan struct{})
			turnFailure := errors.New("conversation failed")
			runner := &titleRunner{
				fakeRunner: &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) {
					close(turnStarted)
					if cancelTurn {
						<-ctx.Done()
						return "", ctx.Err()
					}
					return "", turnFailure
				}},
				title: "Independent Session Title", started: make(chan struct{}),
				release: make(chan struct{}), finished: make(chan struct{}),
			}
			_, root := openTitleLifecycle(t, store, rootID, runner)
			unblock := sync.OnceFunc(func() { close(runner.release) })
			t.Cleanup(unblock)
			receipt, err := root.Submit(t.Context(), "Investigate a failed conversation")
			if err != nil {
				t.Fatal(err)
			}
			waitTitleLifecycle(t, runner.started)
			waitTitleLifecycle(t, turnStarted)
			if cancelTurn {
				target, err := store.ActiveTurn(t.Context(), rootID, root.AgentID())
				if err != nil {
					t.Fatal(err)
				}
				result := clientCommand(t, root, "title-client", "cancel", "cancel", map[string]string{"turn_id": target})
				if result.Status != "succeeded" {
					t.Fatalf("cancel=%+v", result)
				}
				turnFailure = context.Canceled
			}
			if completion := waitReceipt(t, receipt); !errors.Is(completion.Err, turnFailure) {
				t.Fatalf("conversation error=%v, want %v", completion.Err, turnFailure)
			}
			select {
			case <-runner.finished:
				t.Fatal("conversation completion cancelled the title request")
			default:
			}
			unblock()
			settleTitleLifecycle(t, root)
			assertTitleLifecycle(t, store, rootID, runner.title)
		})
	}
}

func TestTitleLifecycleShutdownAndDeletionJoinWorker(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(fmt.Sprintf("delete=%t", deleting), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, path)
			t.Cleanup(func() { _ = store.Close() })
			rootID := createRoot(t, store)
			started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			runner := &lifecycleTitleRunner{fakeRunner: &fakeRunner{}}
			runner.generate = func(ctx context.Context, _ string) (string, llm.Usage, error) {
				close(started)
				<-ctx.Done()
				close(cancelled)
				<-release
				return "", llm.Usage{}, ctx.Err()
			}
			owner, root := openTitleLifecycle(t, store, rootID, runner)
			t.Cleanup(unblock)
			receipt, err := root.Submit(t.Context(), "Preserve this fallback")
			if err != nil {
				t.Fatal(err)
			}
			if completion := waitReceipt(t, receipt); completion.Err != nil {
				t.Fatal(completion.Err)
			}
			waitTitleLifecycle(t, started)
			stopped := make(chan error, 1)
			go func() {
				if deleting {
					stopped <- owner.DeleteSession(t.Context(), rootID)
				} else {
					stopped <- owner.Close()
				}
			}()
			waitTitleLifecycle(t, cancelled)
			select {
			case err := <-stopped:
				t.Fatalf("stop returned before title worker exited: %v", err)
			default:
			}
			unblock()
			select {
			case err := <-stopped:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stop did not join title worker")
			}
			if !deleting {
				// Daemon.Close owns the store; reopen to check durable fallback.
				reopened := openStore(t, path)
				t.Cleanup(func() { _ = reopened.Close() })
				assertTitleLifecycle(t, reopened, rootID, "Preserve this fallback")
			} else if _, _, err := store.Load(rootID); err == nil {
				t.Fatal("deleted session still exists")
			}
		})
	}
}

func TestGenerateTitlePromptOnlyAndRouteSnapshot(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("compact=%t", compact), func(t *testing.T) {
			requests := make(chan llm.Request, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request, ok := modelAccountingRequest(t, w, r)
				if !ok {
					return
				}
				if r.Header.Get("Authorization") != "Bearer original-key" {
					t.Errorf("request used mutated credentials: %q", r.Header.Get("Authorization"))
				}
				requests <- request
				modelAccountingReply(w, request, "Snapshot Session Title", modelAccountingUsage())
			}))
			t.Cleanup(provider.Close)
			client := llm.New(provider.URL, "original-key")
			client.MaxRetries = 7
			owner := agent.NewRuntime(client, "main-model", 128, "", tools.NewServices())
			wantModel := "main-model"
			if compact {
				owner.CompactClient, owner.CompactModel = client, "compact-model"
				wantModel = "compact-model"
			}
			// No runtime, kernel, store, or conversation history exists.
			runner := &AgentSession{agent: owner}
			generate := runner.prepareTitle("  Inspect\n" + strings.Repeat("界", 310))
			client.BaseURL, client.APIKey = "http://127.0.0.1:1", "mutated-key"
			owner.Model, owner.CompactModel = "mutated-main", "mutated-compact"
			replacement := agent.NewRuntime(client, "replacement", 128, "", tools.NewServices())
			runner.agent = replacement
			title, usage, err := generate(t.Context())
			if err != nil || title != "Snapshot Session Title" {
				t.Fatalf("title=%q, error=%v", title, err)
			}
			request := <-requests
			wantPrompt := "Inspect " + strings.Repeat("界", 292)
			if request.Model != wantModel || request.MaxTokens != 24 || len(request.Messages) != 2 ||
				request.Messages[0].Role != "system" || request.Messages[1].Role != "user" ||
				request.Messages[1].Content != wantPrompt {
				t.Fatalf("prompt-only snapshot request=%+v", request)
			}
			for _, instruction := range []string{
				"usually 2–6 words", "Prefer a short noun", "Avoid filler",
				"Use sentence case: capitalize only the first word, proper nouns, and",
				"abbreviations. Preserve the spelling and casing of technical identifiers.",
				"Return only the title", "Do not answer the user's message.",
			} {
				if !strings.Contains(request.Messages[0].Content, instruction) {
					t.Errorf("title prompt missing style instruction %q", instruction)
				}
			}
			if owner.Usage().PromptTokens != usage.PromptTokens || owner.Usage().PromptTokens != 10 ||
				replacement.Usage().PromptTokens != 0 {
				t.Fatalf("usage moved to replacement: owner=%+v replacement=%+v", owner.Usage(), replacement.Usage())
			}
			if client.MaxRetries != 7 {
				t.Fatal("title changed the conversation retry policy")
			}
		})
	}
}

func TestGenerateTitleInvalidOutputAndTransportAreSingleAttempt(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		status int
	}{
		{name: "empty"},
		{name: "multiline", output: "First title\nSecond title"},
		{name: "too long", output: strings.Repeat("界", 81)},
		{name: "transient transport", status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request, ok := modelAccountingRequest(t, w, r)
				if !ok {
					return
				}
				requests.Add(1)
				if test.status != 0 {
					http.Error(w, "temporarily unavailable", test.status)
					return
				}
				modelAccountingReply(w, request, test.output, modelAccountingUsage())
			}))
			t.Cleanup(provider.Close)
			client := llm.New(provider.URL, "test-key")
			client.MaxRetries = 7
			runner := &AgentSession{agent: agent.NewRuntime(client, "model", 128, "", tools.NewServices())}
			title, _, err := runner.GenerateTitle(t.Context(), "Name this request without history")
			if err == nil || title != "" || requests.Load() != 1 {
				t.Fatalf("title=%q error=%v requests=%d", title, err, requests.Load())
			}
			if client.MaxRetries != 7 {
				t.Fatal("title changed the conversation retry policy")
			}
		})
	}
}
