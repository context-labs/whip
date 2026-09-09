//go:build integration && unix

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
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
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
	for _, id := range []string{"repl-child", "repl-empty", "repl-paged", "repl-sparse"} {
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
		if id == "repl-paged" {
			messages = sdkREPLMessages(t, "Paged", 80)
		}
		if id == "repl-sparse" {
			messages = sdkREPLMessages(t, "Sparse", 4)
			for range 128 {
				messages = append(messages, llm.Message{Role: "assistant", Content: "No execution in this message."})
			}
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
		case "refresh":
			events = append(events, sdkREPLProbe{kind: "blackboard.set", payload: session.LifecycleEvent{AgentID: rootID}})
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
	for _, step := range []string{"start", "progress", "complete", "child", "refresh"} {
		if status := request(step); status != http.StatusNoContent {
			t.Fatalf("%s status = %d", step, status)
		}
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, 128)
	if err != nil || len(events) != 11 {
		t.Fatalf("REPL probe events = %d, %v", len(events), err)
	}
	if events[5].Kind != "stream.tool.completed" || events[6].Kind != "scratch.restored" {
		t.Fatalf("unexpected completion/restart: %+v", events[5:7])
	}
	for range 27 {
		if status := request("start"); status != http.StatusNoContent {
			t.Fatalf("bounded start status = %d", status)
		}
	}
	if status := request("start"); status != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d", status)
	}
}

// This acceptance path exercises an actual subprocess kernel and durable scratch
// store. Only checkpoint persistence is deliberately failed after restoring.
type sdkScratchFailure struct {
	scratchStore
	fail bool
}

func (s *sdkScratchFailure) Save(ctx context.Context, snapshot string, manifest rlm.SnapshotManifest) error {
	if s.fail {
		return errors.New("injected checkpoint storage failure")
	}
	return s.scratchStore.Save(ctx, snapshot, manifest)
}

func (r *sdkFixtureRunner) scratchResult(ctx context.Context, started func()) (string, error) {
	started()
	scratch := &sdkScratchFailure{scratchStore: scratchStore{node: &AgentSession{root: r.root, id: r.root.AgentID()}}}
	kernel, err := rlm.NewKernel(rlm.KernelOptions{Command: recursiveKernelCommand, Scratch: scratch})
	if err != nil {
		return "", err
	}
	defer kernel.Close()
	if _, err := kernel.Exec(ctx, "saved = 42\nunsupported = files.read"); err != nil {
		return "", err
	}
	if err := kernel.Suspend(); err != nil {
		return "", err
	}
	scratch.fail = true
	code := "saved = 43\nprint(saved)\nfail('cell failed')"
	args, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return "", err
	}
	const callID = "scratch-result-cell"
	r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{kind: "stream.tool.started", event: StreamEvent{ID: callID, Name: "rlm_exec", Args: string(args)}}})
	output := tools.ExecuteWithSuggester(ctx, []tools.Tool{rlm.Tool(kernel)}, "rlm_exec", args, nil)
	r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{kind: "stream.tool.completed", event: StreamEvent{ID: callID, Name: "rlm_exec", Result: output}}})
	call := llm.ToolCall{ID: callID, Type: "function"}
	call.Function.Name, call.Function.Arguments = "rlm_exec", string(args)
	r.mu.Lock()
	r.history = append(r.history,
		llm.Message{Role: "user", Content: "scratch-result", Authored: true},
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		llm.Message{Role: "tool", Name: "rlm_exec", ToolCallID: callID, Content: output},
		llm.Message{Role: "assistant", Content: "The cell failed after printing; do not replay its effects."},
	)
	r.mu.Unlock()
	return "The cell failed after printing; do not replay its effects.", nil
}

func seedSDKTurnFailures(t *testing.T, store *session.Store, rootID, cwd string) {
	t.Helper()
	if _, err := store.EnsureAuthority(t.Context(), rootID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"turn-failed-empty", "turn-failed-history", "turn-failed-long"} {
		if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{
			RootID: rootID, ParentAgentID: rootID, ChildAgentID: id, Name: "Architecture researcher " + id, Model: "model", Provider: "provider", CWD: cwd,
			Prompt: session.RuntimePayload{Data: []byte("Inspect tab behavior")},
		}); err != nil {
			t.Fatal(err)
		}
		turn := id + "-turn"
		if _, err := store.StartAgentTurn(t.Context(), rootID, id, turn); err != nil {
			t.Fatal(err)
		}
		message := `400 Bad Request: Invalid 'prompt_cache_key': maximum length 64, received 82.`
		var messages []llm.Message
		if id == "turn-failed-history" {
			messages = sdkREPLMessages(t, "Prior", 1)
		}
		if id == "turn-failed-long" {
			message += strings.Repeat("\nProvider diagnostic: "+strings.Repeat("long-detail-", 40), 24)
		}
		if err := store.FinishAgentTurn(t.Context(), rootID, id, session.AgentTurnCommit{TurnID: turn, Status: "failed", Error: message, Messages: messages}); err != nil {
			t.Fatal(err)
		}
	}
}

func registerSDKTurnOutcomeProbe(mux *http.ServeMux, store *session.Store, rootID string) {
	mux.HandleFunc("POST /control/turn-outcome/succeed", func(w http.ResponseWriter, r *http.Request) {
		const id, turn = "turn-failed-empty", "successful-followup"
		if _, err := store.EnqueueInbox(r.Context(), session.InboxEnqueue{RootID: rootID, AgentID: id, Kind: "submit", Payload: session.RuntimePayload{Data: []byte("Continue")}}); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if _, err := store.StartAgentTurn(r.Context(), rootID, id, turn); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := store.FinishAgentTurn(r.Context(), rootID, id, session.AgentTurnCommit{TurnID: turn, Status: "succeeded", Messages: []llm.Message{{Role: "assistant", Content: "Follow-up completed."}}}); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
