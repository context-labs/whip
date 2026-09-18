//go:build integration && unix

package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/llm"
)

// Commit a real synthetic turn through the normal worker completion path. The
// five operations fall outside both count-limited and byte-limited snapshots.
func (r *sdkFixtureRunner) historyGapTurn(ctx context.Context, input string, started func()) (string, error) {
	started()
	turnID, err := r.root.store.RunningTurnID(ctx, r.root.ID(), r.root.AgentID())
	if err != nil {
		return "", err
	}
	messages := []llm.Message{{Role: "user", Content: input, Authored: true}}
	for index := range 5 {
		id := fmt.Sprintf("%s-operation-%d", input, index)
		partID := turnID + ":" + id
		call := llm.ToolCall{ID: id, Type: "function"}
		call.Function.Name, call.Function.Arguments = "rlm_exec", `{"code":"42"}`
		host := llm.PresentationHost{InvocationID: id + ":host", Name: "files.read", Status: "completed", Display: &llm.OperationDisplay{Target: fmt.Sprintf("gap-proof-%d.md", index)}}
		messages = append(messages,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}, Presentation: &llm.TranscriptPresentation{Version: 1, TurnID: turnID, Parts: []llm.PresentationPart{
				{ID: partID, Kind: "tool", CallID: id, ToolName: "rlm_exec", Hosts: []llm.PresentationHost{host}},
			}}},
			llm.Message{Role: "tool", Name: "rlm_exec", ToolCallID: id, Content: `{"output":"Synthetic saved result","value":42,"steps":1}`},
		)
		for _, event := range []struct {
			kind    string
			payload StreamEvent
		}{
			{"stream.tool.started", StreamEvent{ID: id, PartID: partID, TurnID: turnID, Name: "rlm_exec", Args: call.Function.Arguments}},
			{"stream.cell.host", StreamEvent{ID: id, PartID: partID, TurnID: turnID, InvocationID: host.InvocationID, Name: host.Name, HostStatus: "completed", Display: host.Display}},
			{"stream.tool.completed", StreamEvent{ID: id, PartID: partID, TurnID: turnID, Name: "rlm_exec", Result: messages[len(messages)-1].Content}},
		} {
			r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{kind: event.kind, event: event.payload}})
		}
	}
	count, padding := 80, ""
	if input == "history-gap:bytes" {
		count, padding = 58, strings.Repeat("Synthetic retained history. ", 190)
	}
	if input == "history-gap:large" {
		count = 700
	}
	for index := range count {
		messages = append(messages, llm.Message{Role: "assistant", Content: fmt.Sprintf("Gap recovery %s record %03d. %s", input, index, padding)})
	}
	output := "Completed " + input + ". The five earlier operations belong before this reply."
	messages = append(messages, llm.Message{Role: "assistant", Content: output})
	// Hold before committing so clients demonstrably observe all five operations.
	r.control.mu.Lock()
	release := r.control.hold(input)
	r.control.mu.Unlock()
	select {
	case <-release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	r.mu.Lock()
	r.history = append(r.history, messages...)
	r.mu.Unlock()
	return output, nil
}
