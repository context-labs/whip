package daemon

import (
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
)

// encodeCommandOutcome writes the v2 result at completion, so reconnect never
// needs the original request or a decoder for historical outcome formats.
func encodeCommandOutcome(operation, output string, failure error) []byte {
	var value any = protocol.Empty{}
	if failure != nil {
		value = rpcFromError(failure)
	} else {
		switch operation {
		case "submit", "steer", "submit.parts", "steer.parts", "shell.run", "tool.call":
			value = protocol.TextResult{Text: output}
		case "session.create", "session.open", "session.fork", "session.delete":
			value = protocol.RootIDResult{RootID: output}
		case "workspace.inspect", "workspace.set":
			value = protocol.PathResult{Path: output}
		case "session.rename":
			value = protocol.TitleResult{Title: output}
		case "goal.set", "goal.run", "goal.from-context":
			value = protocol.GoalResult{Goal: output}
		case "session.effort", "session.effort.get":
			value = protocol.EffortResult{Effort: output}

		default:
			if json.Valid([]byte(output)) {
				value = json.RawMessage(output)
			}
		}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"code":-32603,"message":"cannot encode command outcome"}`)
	}
	return body
}

// encodeTurnOutcome writes a submit turn's result: the final text and, when
// the definition declares an output contract, the validated JSON value.
func encodeTurnOutcome(text string, output json.RawMessage, failure error) []byte {
	if failure != nil || len(output) == 0 {
		return encodeCommandOutcome("submit", text, failure)
	}
	body, err := json.Marshal(protocol.TextResult{Text: text, Output: output})
	if err != nil {
		return []byte(`{"code":-32603,"message":"cannot encode command outcome"}`)
	}
	return body
}
