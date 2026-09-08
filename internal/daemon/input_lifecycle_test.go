package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func TestLifecycleBoundaryReadFailurePreservesClaimForRetry(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, filepath.Join(dir, "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, sessionstore.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	id := rootID + ":reader"
	if err := root.AdmitAgent(t.Context(), sessionstore.AgentAdmission{
		ParentAgentID: rootID, ChildAgentID: id, Name: "reader", Prompt: sessionstore.RuntimePayload{Data: []byte("work")},
	}); err != nil {
		t.Fatal(err)
	}
	start, err := root.StartAgentTurn(t.Context(), id, id+":turn")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("input", 2000)
	if _, err := store.EnqueueInbox(t.Context(), sessionstore.InboxEnqueue{
		RootID: rootID, AgentID: id, Kind: "steer", Payload: sessionstore.RuntimePayload{Data: []byte(body)},
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := store.LoadQueuedInbox(t.Context(), rootID, id, 0, 10)
	if err != nil || len(queued) != 1 {
		t.Fatalf("queued input: %+v, %v", queued, err)
	}
	path := filepath.Join(dir, "artifacts", "sha256", queued[0].Payload.Digest)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	node := &AgentSession{id: id, parentID: rootID, root: root}
	messages, err := node.pullSteers(t.Context(), start.TurnID)
	if !errors.Is(err, syscall.EISDIR) || errors.Is(err, sessionstore.ErrInvalidInput) || len(messages) != 0 {
		t.Fatalf("boundary must propagate infrastructure failure: %+v, %v", messages, err)
	}
	journal := node.turnJournal()
	if len(journal.ClaimedInbox) != 1 || len(journal.DeliveredInbox) != 0 {
		t.Fatalf("claim ownership lost: %+v", journal)
	}
	if err := root.FinishAgentTurn(t.Context(), id, sessionstore.AgentTurnCommit{
		TurnID: start.TurnID, Status: "failed", RetryInput: true, Error: err.Error(),
	}); err != nil {
		t.Fatal(err)
	}
	queued, err = store.LoadQueuedInbox(t.Context(), rootID, id, 0, 10)
	if err != nil || len(queued) != 2 {
		t.Fatalf("read failure discarded retryable input: %+v, %v", queued, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ResolveInboxPayload(t.Context(), queued[1]); err != nil || string(got) != body {
		t.Fatalf("input did not survive infrastructure recovery: %d bytes, %v", len(got), err)
	}
}

func TestLifecycleLargeChildPromptRuns(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); streamText(w, "done") }))
	defer server.Close()
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "large", "prompt": strings.Repeat("x", 9000), "report": "message"})
	if err != nil {
		t.Fatal(err)
	}
	childID := result.(map[string]any)["id"].(string)
	waitRunTurn(t, runs, childID, 1)
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	durable, err := store.LoadAgent(t.Context(), root.ID(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() == 0 {
		t.Fatalf("spawn returned success; child is live idle but durable status=%q, model calls=0", durable.Status)
	}
}

func TestLifecycleRootPreservesLargeInput(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{}
	owner, err := New(store, func(context.Context, sessionstore.Meta, []llm.Message) (Components, error) {
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
	body := strings.Repeat("x", sessionstore.MaxContentRead) + "TAIL_REQUIREMENT"
	receipt, err := root.Submit(t.Context(), body)
	if err != nil {
		t.Fatal(err)
	}
	got := waitReceipt(t, receipt)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if got.Output != body {
		t.Fatalf("accepted %d bytes; runner received %d; tail preserved=%v", len(body), len(got.Output), strings.Contains(got.Output, "TAIL_REQUIREMENT"))
	}
}

func TestLifecyclePreflightFailureDoesNotReplayJournal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); streamText(w, "done") }))
	defer server.Close()
	_, root, _ := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	receipt, err := root.Submit(t.Context(), "first task")
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, receipt); result.Err != nil {
		t.Fatal(result.Err)
	}
	_, before, err := root.History()
	if err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root.WorkingDirectory(), ".agents", "skills", "oversize")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: oversize\ndescription: review\n---\n"+strings.Repeat("a", maxInvokedSkillBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err = root.Submit(t.Context(), "$oversize")
	if err != nil {
		t.Fatal(err)
	}
	result := waitReceipt(t, receipt)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "exceeds") {
		t.Fatalf("expected validation error, got %v", result.Err)
	}
	_, after, err := root.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("preflight error replayed previous journal: history before=%d after=%d; model calls=%d", len(before), len(after), calls.Load())
	}
}

// Capture through the contentRunner interface so this checks complete decoding,
// rather than merely checking that an unsupported runner fails recoverably.
type lifecyclePartsRunner struct {
	fakeRunner
	parts []llm.ContentPart
}

func (r *lifecyclePartsRunner) TurnParts(ctx context.Context, text string, parts []llm.ContentPart, started func(), accepted func(string)) (string, error) {
	r.parts = parts
	return r.Turn(ctx, text, true, started, accepted)
}

func TestLifecycleMultipartAndMalformedInput(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &lifecyclePartsRunner{}
	owner, err := New(store, func(context.Context, sessionstore.Meta, []llm.Message) (Components, error) {
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
	tail := strings.Repeat("x", sessionstore.MaxContentRead) + "TAIL_REQUIREMENT"
	payload, err := json.Marshal(SubmitPayload{Text: "multipart", Parts: []llm.ContentPart{{Type: "text", Text: tail}}})
	if err != nil {
		t.Fatal(err)
	}
	_, receipt, err := root.AdmitCommand(t.Context(), sessionstore.CommandAdmission{ClientID: "client", CommandID: "parts", Kind: "submit.parts", RequestDigest: "parts", Payload: sessionstore.RuntimePayload{Data: payload}})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, receipt); result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(runner.parts) != 1 || runner.parts[0].Text != tail {
		t.Fatal("multipart input was truncated")
	}
	before := store.RawMessages(rootID)
	_, receipt, err = root.AdmitCommand(t.Context(), sessionstore.CommandAdmission{ClientID: "client", CommandID: "bad", Kind: "submit.parts", RequestDigest: "bad", Payload: sessionstore.RuntimePayload{Data: []byte(strings.Repeat(" ", 9000) + "{")}})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, receipt); !errors.Is(result.Err, sessionstore.ErrInvalidInput) {
		t.Fatalf("malformed input: %+v", result)
	}
	after := store.RawMessages(rootID)
	if len(after) != len(before) || runner.calls.Load() != 1 {
		t.Fatal("decoding failure reused a journal or invoked the runner")
	}
	command, err := store.LoadCommand(t.Context(), "client", "bad")
	if err != nil || command.Status != "failed" {
		t.Fatalf("bad command: %+v %v", command, err)
	}
	next, err := root.Submit(t.Context(), "next valid input")
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, next); result.Err != nil || result.Output != "next valid input" {
		t.Fatalf("session did not recover: %+v", result)
	}
}

func TestLifecycleBoundaryRejectsMalformedSteerAndDeliversCompleteInput(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		streamText(w, "done")
	}))
	defer server.Close()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	runtime.rootNode.agent.ContextLimit = 1 << 20
	receipt, err := root.Submit(t.Context(), "initial")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	_, bad, err := root.AdmitCommand(t.Context(), sessionstore.CommandAdmission{ClientID: "client", CommandID: "bad-steer", Kind: "steer.parts", RequestDigest: "bad", Payload: sessionstore.RuntimePayload{Data: []byte("{")}})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("x", sessionstore.MaxContentRead) + "STEER_TAIL"
	good, err := root.Steer(t.Context(), text)
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	if result := waitReceipt(t, receipt); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := waitReceipt(t, bad); !errors.Is(result.Err, sessionstore.ErrInvalidInput) {
		t.Fatalf("bad steer: %+v", result)
	}
	if result := waitReceipt(t, good); result.Err != nil {
		t.Fatalf("good steer: %+v", result)
	}
	history := store.RawMessages(root.ID())
	found := false
	for _, message := range history {
		if message.Content == text {
			found = true
		}
	}
	if !found || calls.Load() != 2 {
		t.Fatalf("full steer missing or incorrect rounds: found=%v calls=%d", found, calls.Load())
	}
	command, err := store.LoadCommand(t.Context(), "client", "bad-steer")
	if err != nil || command.Status != "failed" {
		t.Fatalf("bad steer command=%+v %v", command, err)
	}
}

func TestLifecycleChildPayloadFailureSettlesClaimAndAcceptsNextInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { streamText(w, "done") }))
	defer server.Close()
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	entered, release := make(chan *AgentSession, 1), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	runtime.setRunTurnHook(func(node *AgentSession) {
		if node.parentID != "" {
			entered <- node
			<-release
		}
	})
	result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "invalid-child", "prompt": strings.Repeat("x", 9000), "report": "message"})
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["id"].(string)
	var child *AgentSession
	select {
	case child = <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	items, err := store.LoadQueuedInbox(t.Context(), root.ID(), id, 0, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("child input: %+v %v", items, err)
	}
	if err := store.RevokeContentGrant(t.Context(), items[0].Payload.ReferenceID, root.ID(), id); err != nil {
		t.Fatal(err)
	}
	unblock()
	waitAgentIdle(t, child)
	durable, err := store.LoadAgent(t.Context(), root.ID(), id)
	if err != nil || durable.Status != "idle" {
		t.Fatalf("child stranded: %+v %v", durable, err)
	}
	work, err := store.AgentWorkStatus(t.Context(), root.ID(), id, time.Now())
	if err != nil || work.HasExplicitInput {
		t.Fatalf("invalid input retried: %+v %v", work, err)
	}
	if len(child.agent.MessagesSnapshot()) != 1 {
		t.Fatal("invalid child input reached the model")
	}
	runtime.setRunTurnHook(nil)
	if _, err := root.SubmitAgentInput(t.Context(), root.AgentID(), id, "submit", "valid child input", "test"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := store.LoadAgentTranscript(t.Context(), root.ID(), id)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if message.Role == "assistant" && message.Content == "done" {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("child did not execute next valid input")
}

func TestLifecycleFailedTurnSteerReceiptMatchesDurableCommand(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			streamText(w, "working")
			return
		}
		http.Error(w, `{"error":{"message":"injected model failure"}}`, http.StatusBadRequest)
	}))
	defer server.Close()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	store, root, _ := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	first, err := root.Submit(t.Context(), "initial task")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	admission := sessionstore.CommandAdmission{ClientID: "client", CommandID: "steer", Kind: "steer", RequestDigest: "steer", Payload: sessionstore.RuntimePayload{Data: []byte("do this too")}}
	_, steer, err := root.AdmitCommand(t.Context(), admission)
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	if result := waitReceipt(t, first); result.Err == nil {
		t.Fatal("model failure was not returned")
	}
	live := waitReceipt(t, steer)
	durable, err := store.LoadCommand(t.Context(), "client", "steer")
	if err != nil {
		t.Fatal(err)
	}
	if durable.Status != "failed" || live.Err == nil {
		t.Fatalf("live=%+v durable=%+v", live, durable)
	}
	retry, receipt, err := root.AdmitCommand(t.Context(), admission)
	if err != nil || retry.New || receipt == nil || retry.Command.Status != durable.Status {
		t.Fatalf("retry=%+v receipt=%v err=%v", retry, receipt, err)
	}
	repeated := waitReceipt(t, receipt)
	if repeated.Err == nil || repeated.Err.Error() != live.Err.Error() {
		t.Fatalf("retry error=%v original=%v", repeated.Err, live.Err)
	}
}

type lifecycleJournalRunner struct {
	fakeRunner
	journals atomic.Int32
}

func (r *lifecycleJournalRunner) turnJournal() turnJournal {
	r.journals.Add(1)
	return turnJournal{Messages: r.History()}
}

func TestLifecycleUnsupportedPartsNeverReuseRunnerJournal(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	id := createRoot(t, store)
	runner := &lifecycleJournalRunner{}
	owner, err := New(store, func(context.Context, sessionstore.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	first, err := root.Submit(t.Context(), "first")
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, first); result.Err != nil {
		t.Fatal(result.Err)
	}
	_, receipt, err := root.AdmitCommand(t.Context(), sessionstore.CommandAdmission{ClientID: "client", CommandID: "parts", Kind: "submit.parts", RequestDigest: "parts", Payload: sessionstore.RuntimePayload{Data: []byte(`{"parts":[{"type":"text","text":"hello"}]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, receipt); !errors.Is(result.Err, sessionstore.ErrInvalidInput) {
		t.Fatalf("unsupported input=%+v", result)
	}
	if runner.journals.Load() != 1 || runner.calls.Load() != 1 || len(store.RawMessages(id)) != 2 {
		t.Fatalf("stale journal read: calls=%d journals=%d history=%d", runner.calls.Load(), runner.journals.Load(), len(store.RawMessages(id)))
	}
}
