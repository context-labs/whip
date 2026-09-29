package rpc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestExecutorPublicProjectionFitsWholePageAndPreservesExactCounters(t *testing.T) {
	invocation := executor.Invocation{ID: "invocation", Lease: executor.Lease{Epoch: "epoch", Definition: session.DefinitionRef{ID: "definition", Revision: strings.Repeat("a", 64)}, Generation: 9007199254740993, Tools: []string{"lookup"}, Hooks: []string{}}, Kind: executor.Tool, Name: "lookup", Request: executor.Request{SessionID: "session", TurnID: "turn", CellID: "cell", OperationID: "operation", Operation: "tools.lookup", Arguments: json.RawMessage(`{"data":"` + strings.Repeat("x", executor.MaxRequestBytes-256) + `"}`)}, Deadline: time.UnixMilli(9007199254740993)}
	wire := executorInvocation(invocation)
	page := protocol.ExecutorPendingResult{Items: []protocol.ExecutorInvocation{wire, wire, wire, wire}}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > protocol.MaxFrameBytes-1024 {
		t.Fatal("pending page exceeds response frame", len(raw))
	}
	if err := protocol.Validate("ExecutorPendingResult", raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"generation":"9007199254740993"`) {
		t.Fatal("generation rounded")
	}
	for _, method := range []string{"executor.invoke", "executor.cancel"} {
		event := protocol.ExecutorEvent{JSONRPC: "2.0", Method: method, Epoch: "epoch", Generation: 9007199254740993, InvocationID: "invocation", Invocation: &wire}
		raw, _ := json.Marshal(event)
		if (protocol.Validate("ExecutorEvent", raw) == nil) != (method == "executor.invoke") {
			t.Fatal("wrong event shape accepted", method)
		}
		event.Invocation = nil
		raw, _ = json.Marshal(event)
		if (protocol.Validate("ExecutorEvent", raw) == nil) != (method == "executor.cancel") {
			t.Fatal("wrong null event shape accepted", method)
		}
	}
}
