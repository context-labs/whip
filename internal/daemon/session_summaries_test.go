package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestSessionSummariesAcrossTransports(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	if _, err := f.store.EnsureAuthority(t.Context(), f.rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: f.rootID, ParentAgentID: f.rootID, ChildAgentID: "summary-child", Name: "child", Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := f.store.EnqueueInbox(t.Context(), session.InboxEnqueue{RootID: f.rootID, AgentID: "summary-child", Kind: "submit", Payload: session.RuntimePayload{Data: []byte("do not return this prompt")}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.StartAgentTurn(t.Context(), f.rootID, "summary-child", "summary-turn"); err != nil {
		t.Fatal(err)
	}
	var first protocol.SessionSummariesResult
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			client := f.dial(transport, "summaries-"+transport)
			initialize := client.InitializeResult()
			if initialize.ProtocolMajor != ProtocolMajor || !slices.Contains(initialize.Capabilities, "session_summaries") {
				t.Fatalf("summary capability absent: %+v", initialize)
			}
			var result protocol.SessionSummariesResult
			if err := client.Call(t.Context(), "sessions.summaries", protocol.SessionSummariesParams{RootIDs: []string{"missing", f.rootID, "summary-child"}}, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != 3 || !result.Items[0].Missing || result.Items[1].Missing || !result.Items[2].Missing || result.Items[1].RunningAgents != 1 || result.Items[1].QueuedAgents != 1 {
				t.Fatalf("root/child activity %+v", result)
			}
			if first.Items == nil {
				first = result
			} else if !reflect.DeepEqual(first, result) {
				t.Fatalf("transport summaries differ: %+v %+v", first, result)
			}
			for _, invalid := range []protocol.SessionSummariesParams{
				{RootIDs: []string{f.rootID, f.rootID}},
				{RootIDs: []string{""}},
				{RootIDs: make([]string, 33)},
				{RootIDs: []string{strings.Repeat("界", 128)}},
			} {
				err := client.Call(t.Context(), "sessions.summaries", invalid, &result)
				failure, ok := errors.AsType[*RPCError](err)
				if !ok || failure.Code != -32602 || failure.Data == nil || failure.Data.Kind != "invalid_arguments" {
					t.Fatalf("invalid summary bounds: %v", err)
				}
			}
		})
	}
	client, err := DialWebSocketClient(t.Context(), f.endpoint, InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: "summary-negotiation", ClientKind: "automation", Capabilities: []string{"session_summaries", "unknown", "session_summaries"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if got := client.InitializeResult().NegotiatedCapabilities; !slices.Equal(got, []string{"session_summaries"}) {
		t.Fatalf("negotiated capabilities %v", got)
	}
	var result protocol.SessionSummariesResult
	if err := client.Call(t.Context(), "sessions.summaries", protocol.SessionSummariesParams{RootIDs: []string{f.rootID}}, &result); err != nil || result.Items[0].Missing {
		t.Fatalf("trusted automation navigation: %+v %v", result, err)
	}
}

func TestSessionSummariesDoNotOpenRootsAndObserveConcurrentQuestionAnswers(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	opens := 0
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		opens++
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	server := &Server{daemon: owner}
	ids := make([]string, 32)
	for index := range ids {
		ids[index] = createRoot(t, store)
	}
	params := protocol.SessionSummariesParams{RootIDs: ids}
	if result, err := server.sessionSummaries(t.Context(), params); err != nil || len(result.Items) != 32 || opens != 0 {
		t.Fatalf("summaries opened roots: %+v %d %v", result, opens, err)
	}
	root, err := owner.Open(ids[31])
	if err != nil {
		t.Fatal(err)
	}
	question := &questionWaiter{event: session.LifecycleEvent{QuestionID: "summary-question", AgentID: root.AgentID(), Question: "private question", Options: []session.QuestionOption{{Label: "yes"}}, Questions: []session.QuestionSet{{Question: "private question", Options: []session.QuestionOption{{Label: "yes"}}}}}, done: make(chan struct{})}
	root.questions.mu.Lock()
	root.questions.pending = map[string]*questionWaiter{"summary-question": question}
	root.questions.mu.Unlock()
	if result, err := server.sessionSummaries(t.Context(), params); err != nil || result.Items[31].PendingQuestions != 1 || opens != 1 {
		t.Fatalf("live question not counted: %+v %d %v", result, opens, err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for range 10 {
				result, err := server.sessionSummaries(t.Context(), params)
				if err != nil || result.Items[31].PendingQuestions < 0 || result.Items[31].PendingQuestions > 1 {
					t.Errorf("concurrent question observation: %+v %v", result, err)
					return
				}
			}
		})
	}
	if _, err := root.answerQuestion(t.Context(), "summary-question", protocol.QuestionAnswerParams{ID: "summary-question", Answer: []string{"yes"}}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	result, err := server.sessionSummaries(t.Context(), params)
	if err != nil || result.Items[31].PendingQuestions != 0 || opens != 1 {
		t.Fatalf("answered question retained or inspection opened roots: %+v %d %v", result, opens, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "private question") {
		t.Fatalf("question body leaked: %s %v", encoded, err)
	}
}

func TestSessionSummariesBoundEncodedOutputWithoutDroppingIdentityOrCounts(t *testing.T) {
	items := make([]session.SessionNavigationSummary, 32)
	for index := range items {
		items[index] = session.SessionNavigationSummary{
			RootID: strings.Repeat("\x01", 255) + string(rune(index+1)),
			Title:  strings.Repeat("<", 128), CWD: strings.Repeat("&", 128), WorkspaceID: strings.Repeat("a", 64),
			RunningAgents: 1<<63 - 1, QueuedAgents: 1<<63 - 1,
			PendingPermissions: 1<<63 - 1, PendingQuestions: 1<<63 - 1,
		}
	}
	before := slices.Clone(items)
	result, err := boundSessionSummaries(items)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 64<<10 || len(result.Items) != 32 {
		t.Fatalf("unbounded result: %d bytes, %d items, %v", len(encoded), len(result.Items), err)
	}
	for index, item := range result.Items {
		if item.RootID != before[index].RootID || item.Missing || !item.Truncated || item.WorkspaceID != before[index].WorkspaceID || item.RunningAgents != before[index].RunningAgents || item.PendingQuestions != before[index].PendingQuestions || item.QueuedAgents != before[index].QueuedAgents || item.PendingPermissions != before[index].PendingPermissions || !utf8.ValidString(item.Title) || !utf8.ValidString(item.CWD) {
			t.Fatalf("lost identity/count or truncation flag: %+v", item)
		}
	}
	if !strings.Contains(string(encoded), `"running_agents":"9223372036854775807"`) {
		t.Fatal("navigation counters were encoded as JavaScript numbers")
	}
}
