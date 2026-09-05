package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestCancelledChildResumesQueuedFollowup(t *testing.T) {
	started, followedUp := make(chan struct{}), make(chan struct{})
	var childCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), "child-cancel-original") {
			if childCalls.Add(1) == 1 {
				close(started)
				<-request.Context().Done()
				return
			}
			if !strings.Contains(string(body), "child-cancel-followup") {
				t.Error("child resumed without the queued follow-up")
			}
			if childCalls.Load() == 2 {
				close(followedUp)
			}
		} else {
			// Keep the parent's cancellation-notice turn from completing and
			// accidentally rescuing a lost child wake through reconciliation.
			<-request.Context().Done()
			return
		}
		streamText(w, "done")
	}))
	t.Cleanup(server.Close)
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
		"name": "cancel-child", "prompt": "child-cancel-original", "report": "message",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["id"].(string)
	receiveActorValue(t, started)
	if err := root.routeControl(t.Context(), func(ctx context.Context) error {
		_, err := root.clientAgentSubmit(ctx, id, "child-cancel-followup", "queued")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !runtime.CancelAgentTurn(id) {
		t.Fatal("child was not running")
	}
	receiveActorValue(t, followedUp)
	runtime.mu.RLock()
	child := runtime.agents[id]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	items, err := store.LoadQueuedInbox(t.Context(), root.ID(), id, 0, 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("queued follow-up was stranded: %+v, %v", items, err)
	}
	if childCalls.Load() != 2 {
		t.Fatalf("child model calls = %d; want cancelled turn and follow-up", childCalls.Load())
	}
}

func TestIntentionalChildTerminalizationKeepsRootUsable(t *testing.T) {
	for _, operation := range []string{"stop", "delete"} {
		t.Run(operation, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				if strings.Contains(string(body), "terminal-child-block") {
					close(started)
					<-request.Context().Done()
					return
				}
				streamText(w, "done")
			}))
			t.Cleanup(server.Close)
			store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
			result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
				"name": "terminal-child", "prompt": "terminal-child-block", "report": "message",
			})
			if err != nil {
				t.Fatal(err)
			}
			id := result.(map[string]any)["id"].(string)
			receiveActorValue(t, started)
			runtime.mu.RLock()
			child := runtime.agents[id]
			runtime.mu.RUnlock()
			if _, err := runtime.rootNode.host.Call(t.Context(), "agents", operation, map[string]any{"id": id}); err != nil {
				t.Fatal(err)
			}
			waitAgentIdle(t, child)
			durable, err := store.LoadAgent(t.Context(), root.ID(), id)
			want := "stopped"
			if operation == "delete" {
				want = "deleted"
			}
			if err != nil || durable.Status != want {
				t.Fatalf("late completion revived child: %+v, %v", durable, err)
			}
			receipt, err := root.Submit(t.Context(), "continue root task")
			if err != nil {
				t.Fatalf("intentional child %s failed root: %v", operation, err)
			}
			if result := waitReceipt(t, receipt); result.Err != nil {
				t.Fatalf("root no longer usable after child %s: %v", operation, result.Err)
			}
		})
	}
}

func TestChildCommitFailureInterruptsRootWithoutFalseSuccess(t *testing.T) {
	targetStarted, otherStarted := make(chan struct{}), make(chan struct{})
	targetRelease, otherRelease := make(chan struct{}), make(chan struct{})
	var targetOnce, otherOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		release := targetRelease
		if strings.Contains(string(body), "unrelated child task") {
			release = otherRelease
			otherOnce.Do(func() { close(otherStarted) })
		} else {
			targetOnce.Do(func() { close(targetStarted) })
		}
		select {
		case <-release:
			streamText(w, "done")
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	t.Cleanup(func() { _ = store.Close() })
	targetID, otherID := createRoot(t, store), createRoot(t, store)
	runtimes := map[string]*RecursiveRuntime{}
	owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, rlm.BuildPrompt(meta.CWD, nil), tools.NewServices())
		value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
		limits := rlm.DefaultLimits()
		limits.MaxWorkers = 2
		runtime, err := NewRecursiveRuntime(RecursiveRuntimeOptions{
			Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(2), KernelCommand: recursiveKernelCommand,
		})
		if err != nil {
			return Components{}, err
		}
		runtimes[meta.ID] = runtime
		return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	target, err := owner.Open(targetID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := owner.Open(otherID)
	if err != nil {
		t.Fatal(err)
	}
	spawn := func(root *Session, name, prompt string) string {
		t.Helper()
		report := "message"
		if root == target {
			report = "notice"
		}
		result, err := runtimes[root.ID()].rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
			"name": name, "prompt": prompt, "report": report,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.(map[string]any)["id"].(string)
	}
	targetChild := spawn(target, "target", "target child task")
	otherChild := spawn(other, "other", "unrelated child task")
	receiveActorValue(t, targetStarted)
	receiveActorValue(t, otherStarted)

	observer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	// Fail after the inbox, transcript, and turn updates, so the complete
	// transaction must roll back before root failure settles the old claim.
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_child_commit BEFORE INSERT ON events
		WHEN NEW.root_id='%s' AND NEW.kind='agent.turn.succeeded'
		BEGIN SELECT RAISE(ABORT,'injected child commit failure'); END`, strings.ReplaceAll(targetID, "'", "''"))
	if _, err := observer.ExecContext(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	close(targetRelease)
	receiveActorValue(t, target.Done())
	if err := target.Err(); err == nil || !strings.Contains(err.Error(), "injected child commit failure") {
		t.Fatalf("root did not surface failed child commit: %v", err)
	}
	for _, table := range []string{"turns", "inbox"} {
		var running int
		if err := observer.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE root_id=? AND status='running'`, targetID).Scan(&running); err != nil {
			t.Fatal(err)
		}
		if running != 0 {
			t.Fatalf("failed root retained %d running %s", running, table)
		}
	}
	failedChild, err := store.LoadAgent(t.Context(), targetID, targetChild)
	if err != nil || failedChild.Status != "idle" {
		t.Fatalf("failed child claim retained durable state: %+v, %v", failedChild, err)
	}
	var notices int
	if err := observer.QueryRowContext(t.Context(), `SELECT count(*) FROM agent_messages WHERE root_id=? AND sender_agent_id=? AND kind=?`, targetID, targetChild, session.MessageKindAgentCompleted).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 0 {
		t.Fatalf("rolled-back turn emitted %d successful completion notices", notices)
	}
	otherAgent, err := store.LoadAgent(t.Context(), otherID, otherChild)
	if err != nil || otherAgent.Status != "running" {
		t.Fatalf("unrelated root's active child changed: %+v, %v", otherAgent, err)
	}
	select {
	case <-other.Done():
		t.Fatalf("unrelated root stopped: %v", other.Err())
	default:
	}
	close(otherRelease)
	runtimes[otherID].mu.RLock()
	otherNode := runtimes[otherID].agents[otherChild]
	runtimes[otherID].mu.RUnlock()
	waitAgentIdle(t, otherNode)
	otherAgent, err = store.LoadAgent(t.Context(), otherID, otherChild)
	if err != nil || otherAgent.Status != "idle" {
		t.Fatalf("unrelated child could not finish: %+v, %v", otherAgent, err)
	}
}
