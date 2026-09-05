package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type askResult struct {
	value any
	err   error
}

func askUser(node *AgentSession, ctx context.Context, arguments map[string]any) <-chan askResult {
	results := make(chan askResult, 1)
	go func() {
		value, err := node.host.Call(ctx, "user", "ask", arguments)
		results <- askResult{value: value, err: err}
	}()
	return results
}

func waitAsk(t *testing.T, results <-chan askResult) askResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("user.ask did not return")
		return askResult{}
	}
}

// waitQuestionEvent replays the root's events until one of kind arrives after
// the seq cursor and returns its decoded payload.
func waitQuestionEvent(t *testing.T, store *session.Store, rootID, kind string, after int64) (session.LifecycleEvent, int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, _, err := store.ReplayEvents(t.Context(), rootID, after, session.MaxEventReplay)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Kind != kind {
				continue
			}
			payload, err := store.ResolveRuntimeValue(t.Context(), rootID, event.Payload)
			if err != nil {
				t.Fatal(err)
			}
			var decoded session.LifecycleEvent
			if err := json.Unmarshal(payload, &decoded); err != nil {
				t.Fatal(err)
			}
			return decoded, event.Seq
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s event", kind)
	return session.LifecycleEvent{}, 0
}

func questionArguments(multiple bool) map[string]any {
	return map[string]any{
		"question": "Which database?",
		"options": []any{
			map[string]any{"label": "SQLite", "description": "embedded"},
			map[string]any{"label": "Postgres"},
		},
		"multiple": multiple,
	}
}

func TestUserAskRoundTripsThroughQuestionEvents(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	node := runtime.rootNode

	results := askUser(node, t.Context(), questionArguments(false))
	pending, cursor := waitQuestionEvent(t, store, root.ID(), "question.pending", 0)
	if pending.AgentID != root.AgentID() || pending.QuestionID == "" || pending.Question != "Which database?" || pending.Multiple {
		t.Fatalf("question.pending = %+v", pending)
	}
	if len(pending.Options) != 2 || pending.Options[0] != (session.QuestionOption{Label: "SQLite", Description: "embedded"}) || pending.Options[1].Label != "Postgres" {
		t.Fatalf("options = %+v", pending.Options)
	}

	if _, err := node.host.Call(t.Context(), "user", "ask", questionArguments(false)); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("second ask while one is open err = %v", err)
	}
	// A client that connects now missed question.pending: the snapshot lists it.
	if snapshot, err := root.Snapshot(t.Context()); err != nil || len(snapshot.Questions) != 1 || snapshot.Questions[0].QuestionID != pending.QuestionID || len(snapshot.Questions[0].Options) != 2 {
		t.Fatalf("snapshot questions = %+v, %v", snapshot.Questions, err)
	}
	for name, payload := range map[string]clientActionPayload{
		"unknown label":  {ID: pending.QuestionID, Answer: []string{"MySQL"}},
		"two for single": {ID: pending.QuestionID, Answer: []string{"SQLite", "Postgres"}},
		"empty":          {ID: pending.QuestionID},
		"unknown id":     {ID: "question-missing", Answer: []string{"SQLite"}},
	} {
		if result := clientCommand(t, root, "tui", "bad-"+strings.ReplaceAll(name, " ", "-"), "question.answer", payload); result.Status != "failed" {
			t.Fatalf("%s: result = %+v", name, result)
		}
	}
	select {
	case result := <-results:
		t.Fatalf("rejected answers resolved the question: %+v", result)
	default:
	}

	if result := clientCommand(t, root, "tui", "answer-1", "question.answer", clientActionPayload{ID: pending.QuestionID, Answer: []string{"Postgres"}}); result.Status != "succeeded" || result.Output != "answered" {
		t.Fatalf("answer result = %+v", result)
	}
	result := waitAsk(t, results)
	if result.err != nil {
		t.Fatal(result.err)
	}
	value := result.value.(map[string]any)
	if answer, _ := value["answer"].([]string); len(answer) != 1 || answer[0] != "Postgres" || value["dismissed"] != false {
		t.Fatalf("ask value = %#v", value)
	}
	answered, _ := waitQuestionEvent(t, store, root.ID(), "question.answered", cursor)
	if answered.QuestionID != pending.QuestionID || len(answered.Answer) != 1 || answered.Answer[0] != "Postgres" || answered.Dismissed {
		t.Fatalf("question.answered = %+v", answered)
	}
	if result := clientCommand(t, root, "tui", "answer-again", "question.answer", clientActionPayload{ID: pending.QuestionID, Answer: []string{"Postgres"}}); result.Status != "failed" || !strings.Contains(result.Error, "not open") {
		t.Fatalf("answering a settled question = %+v", result)
	}
	if snapshot, err := root.Snapshot(t.Context()); err != nil || len(snapshot.Questions) != 0 {
		t.Fatalf("settled question still in the snapshot: %+v, %v", snapshot.Questions, err)
	}

	// Multiple choice accepts several distinct labels.
	results = askUser(node, t.Context(), questionArguments(true))
	pending, _ = waitQuestionEvent(t, store, root.ID(), "question.pending", cursor)
	if !pending.Multiple {
		t.Fatalf("question.pending = %+v", pending)
	}
	if result := clientCommand(t, root, "tui", "answer-2", "question.answer", clientActionPayload{ID: pending.QuestionID, Answer: []string{"SQLite", "Postgres"}}); result.Status != "succeeded" {
		t.Fatalf("multiple answer = %+v", result)
	}
	if result := waitAsk(t, results); result.err != nil || len(result.value.(map[string]any)["answer"].([]string)) != 2 {
		t.Fatalf("multiple ask = %+v", result)
	}
}

func TestUserAskDismissAndCancelClose(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	node := runtime.rootNode

	results := askUser(node, t.Context(), questionArguments(false))
	pending, cursor := waitQuestionEvent(t, store, root.ID(), "question.pending", 0)
	if result := clientCommand(t, root, "tui", "dismiss", "question.answer", clientActionPayload{ID: pending.QuestionID, Dismissed: true, Answer: []string{"ignored"}}); result.Status != "succeeded" || result.Output != "dismissed" {
		t.Fatalf("dismiss result = %+v", result)
	}
	result := waitAsk(t, results)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if value := result.value.(map[string]any); len(value["answer"].([]string)) != 0 || value["dismissed"] != true {
		t.Fatalf("dismissed value = %#v", value)
	}
	if answered, _ := waitQuestionEvent(t, store, root.ID(), "question.answered", cursor); !answered.Dismissed || len(answered.Answer) != 0 {
		t.Fatalf("question.answered = %+v", answered)
	}

	// Cancelling the turn closes the open question and returns ctx.Err().
	ctx, cancel := context.WithCancel(t.Context())
	results = askUser(node, ctx, questionArguments(false))
	pending, cursor = waitQuestionEvent(t, store, root.ID(), "question.pending", cursor)
	cancel()
	if result := waitAsk(t, results); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("cancelled ask err = %v", result.err)
	}
	closed, _ := waitQuestionEvent(t, store, root.ID(), "question.closed", cursor)
	if closed.QuestionID != pending.QuestionID || closed.Error != context.Canceled.Error() {
		t.Fatalf("question.closed = %+v", closed)
	}
	if result := clientCommand(t, root, "tui", "late", "question.answer", clientActionPayload{ID: pending.QuestionID, Answer: []string{"SQLite"}}); result.Status != "failed" {
		t.Fatalf("answering a closed question = %+v", result)
	}
	root.questions.mu.Lock()
	open := len(root.questions.pending)
	root.questions.mu.Unlock()
	if open != 0 {
		t.Fatalf("%d questions still registered", open)
	}
}

func TestUserAskIsRootOnlyAndValidatesArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
	defer server.Close()
	_, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	node := runtime.rootNode

	for name, arguments := range map[string]map[string]any{
		"missing question": {"options": questionArguments(false)["options"]},
		"one option":       {"question": "q", "options": []any{map[string]any{"label": "only"}}},
		"seven options":    {"question": "q", "options": []any{map[string]any{"label": "1"}, map[string]any{"label": "2"}, map[string]any{"label": "3"}, map[string]any{"label": "4"}, map[string]any{"label": "5"}, map[string]any{"label": "6"}, map[string]any{"label": "7"}}},
		"duplicate labels": {"question": "q", "options": []any{map[string]any{"label": "same"}, map[string]any{"label": "same"}}},
		"blank label":      {"question": "q", "options": []any{map[string]any{"label": " "}, map[string]any{"label": "ok"}}},
		"options not list": {"question": "q", "options": "SQLite, Postgres"},
	} {
		if _, err := node.host.Call(t.Context(), "user", "ask", arguments); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err := node.host.Call(t.Context(), "user", "tell", questionArguments(false)); err == nil {
		t.Fatal("unknown user operation was accepted")
	}
	root.questions.mu.Lock()
	open := len(root.questions.pending)
	root.questions.mu.Unlock()
	if open != 0 {
		t.Fatalf("rejected asks left %d questions registered", open)
	}

	spawned, err := node.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "child", "prompt": "work"})
	if err != nil {
		t.Fatal(err)
	}
	childID := spawned.(map[string]any)["id"].(string)
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	_, err = child.host.Call(t.Context(), "user", "ask", questionArguments(false))
	if err == nil || err.Error() != "only the root agent can ask the user; send your parent a message instead" {
		t.Fatalf("descendant ask err = %v", err)
	}
	waitAgentIdle(t, child)
}
