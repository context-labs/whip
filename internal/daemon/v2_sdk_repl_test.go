//go:build integration && unix

package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// The browser opts into synthetic recorded executions, including more history
// than the client retains. Child transcripts use normal admission/turn commits.
func seedSDKREPLHistory(t *testing.T, store *session.Store, rootID, cwd string) {
	t.Helper()
	if _, err := store.EnsureAuthority(t.Context(), rootID); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(rootID, 0, sdkREPLMessages(t, "Root", 180), "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"repl-child", "repl-empty"} {
		_, err := store.AdmitAgent(t.Context(), session.AgentAdmission{
			RootID: rootID, ParentAgentID: rootID, ChildAgentID: id,
			Name: id, Model: "model", Provider: "provider", CWD: cwd,
			Prompt: session.RuntimePayload{Data: []byte("Synthetic REPL history")},
		})
		if err != nil {
			t.Fatal(err)
		}
		turnID := id + "-seed"
		started, err := store.StartAgentTurn(t.Context(), rootID, id, turnID)
		if err != nil || len(started.Items) != 1 {
			t.Fatalf("seed child turn: %+v, %v", started, err)
		}
		messages := []llm.Message{{Role: "assistant", Content: "No Starlark has run in this agent yet."}}
		if id == "repl-child" {
			messages = sdkREPLMessages(t, "Child", 4)
		}
		if err := store.FinishAgentTurn(t.Context(), rootID, id, session.AgentTurnCommit{
			TurnID: turnID, Status: "succeeded", AcknowledgedInbox: []int64{started.Items[0].Seq}, Messages: messages,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.TerminalizeSubtree(t.Context(), rootID, rootID, id, "stopped"); err != nil {
			t.Fatal(err)
		}
	}
}

func sdkREPLMessages(t *testing.T, label string, count int) []llm.Message {
	t.Helper()
	messages := make([]llm.Message, 0, count*4)
	for index := range count {
		id := fmt.Sprintf("%s-cell-%03d", label, index)
		call := llm.ToolCall{ID: id, Type: "function"}
		call.Function.Name = "rlm_exec"
		code := fmt.Sprintf("# %s cell %03d\nvalues = [n * n for n in range(4)]\nprint(values)\nvalues", label, index)
		arguments, err := json.Marshal(map[string]string{"code": code})
		if err != nil {
			t.Fatal(err)
		}
		call.Function.Arguments = string(arguments)
		output := fmt.Sprintf("%s output %03d\n", label, index) + strings.Repeat("A bounded line of printed evidence.\n", 10)
		result, err := json.Marshal(map[string]any{"value": []int{0, 1, 4, 9}, "output": output, "steps": 27 + index})
		if err != nil {
			t.Fatal(err)
		}
		if index%11 == 0 {
			result = []byte("Error: synthetic Starlark division by zero")
		}
		analysis := fmt.Sprintf("%s analysis %03d. Recorded cells remain independently inspectable.\n\n%s",
			label, index, strings.Repeat("The conversation and notebook preserve separate reading positions. ", 5))
		messages = append(messages,
			llm.Message{Role: "user", Authored: true, Content: fmt.Sprintf("Inspect %s cell %03d.", label, index)},
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
			llm.Message{Role: "tool", Name: "rlm_exec", ToolCallID: id, Content: string(result)},
			llm.Message{Role: "assistant", Content: analysis},
		)
	}
	return messages
}

type sdkREPLProbe struct {
	kind    string
	payload any
}

// Fixed, bounded test steps go through the durable event stream, not an app mock.
// These routes exist only on the opt-in isolated fixture frontend.
func registerSDKREPLProbes(mux *http.ServeMux, store *session.Store, rootID string) {
	var count atomic.Uint32
	mux.HandleFunc("POST /control/repl/{step}", func(w http.ResponseWriter, r *http.Request) {
		events := []sdkREPLProbe{}
		switch r.PathValue("step") {
		case "start":
			events = append(events, sdkREPLProbe{kind: "stream.tool.call", payload: StreamEvent{
				ID: "browser-live", Name: "rlm_exec", Args: `{"code":"print(\"live`,
			}})
		case "progress":
			events = append(events,
				sdkREPLProbe{kind: "stream.tool.started", payload: StreamEvent{
					ID: "browser-live", Name: "rlm_exec",
					Args: `{"code":"print(\"live evidence\")\nfiles.list(path=\".\")"}`,
				}},
				sdkREPLProbe{kind: "stream.cell.host", payload: StreamEvent{
					ID: "browser-live", Name: "files.list", Args: "path=.", Text: "12ms",
				}},
				sdkREPLProbe{kind: "stream.cell.host", payload: StreamEvent{
					ID: "browser-live", Name: "files.list", Args: "path=.", Text: "8ms",
				}},
				sdkREPLProbe{kind: "stream.tool.output", payload: StreamEvent{
					ID: "browser-live", Text: "live line 1\nlive line 2\nlive line 3\nlive line 4\n" +
						"live line 5\nlive line 6\nlive line 7\nlive line 8\n",
				}},
			)
		case "complete":
			events = append(events,
				sdkREPLProbe{kind: "stream.tool.completed", payload: StreamEvent{
					ID: "browser-live", Name: "rlm_exec",
					Result: `{"value":{"files":4},"output":"live line 1\nlive line 2\nlive line 3\nlive line 4\n` +
						`live line 5\nlive line 6\nlive line 7\nlive line 8\n","steps":42}`,
				}},
				sdkREPLProbe{kind: "scratch.restored", payload: session.LifecycleEvent{
					AgentID: rootID, Restored: []string{"values", "files"},
				}},
			)
		case "child":
			events = append(events,
				sdkREPLProbe{kind: "stream.tool.started", payload: StreamEvent{
					AgentID: "repl-child", ID: "browser-child-live", Name: "rlm_exec",
					Args: `{"code":"print(\"child live evidence\")"}`,
				}},
				sdkREPLProbe{kind: "stream.cell.host", payload: StreamEvent{
					AgentID: "repl-child", ID: "browser-child-live", Name: "files.read",
					Args: "path=missing.txt", Text: "2ms", Result: "Synthetic file unavailable",
				}},
				sdkREPLProbe{kind: "stream.tool.completed", payload: StreamEvent{
					AgentID: "repl-child", ID: "browser-child-live", Name: "rlm_exec",
					Result: "Error: synthetic child failure",
				}},
			)
		default:
			http.Error(w, "unknown REPL fixture step", http.StatusBadRequest)
			return
		}
		if count.Add(1) > 32 {
			http.Error(w, "REPL fixture step limit reached", http.StatusTooManyRequests)
			return
		}
		for _, event := range events {
			data, err := json.Marshal(event.payload)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := store.AppendRootEvent(r.Context(), rootID, event.kind, session.RuntimePayload{
				Data: data, MediaType: "application/json", Source: event.kind,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestSDKREPLProbesRecordFixedEventsAndBoundRequests(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	mux := http.NewServeMux()
	registerSDKREPLProbes(mux, store, rootID)
	request := func(step string) int {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("POST", "/control/repl/"+step, nil))
		return response.Code
	}
	if status := request("invalid"); status != http.StatusBadRequest {
		t.Fatalf("unknown step status = %d", status)
	}
	for _, step := range []string{"start", "progress", "complete", "child"} {
		if status := request(step); status != http.StatusNoContent {
			t.Fatalf("%s status = %d", step, status)
		}
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, 128)
	if err != nil || len(events) != 10 {
		t.Fatalf("REPL probe events = %d, %v", len(events), err)
	}
	if events[5].Kind != "stream.tool.completed" || events[6].Kind != "scratch.restored" {
		t.Fatalf("unexpected completion/restart: %+v", events[5:7])
	}
	for range 28 {
		if status := request("start"); status != http.StatusNoContent {
			t.Fatalf("bounded start status = %d", status)
		}
	}
	if status := request("start"); status != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d", status)
	}
}
