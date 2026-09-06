package daemon

import (
	"encoding/json"
	"strings"

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

// decodeCommandPresentation formats only v2 structured outcomes for existing
// text presenters. It never accepts an old plain-text stored outcome.
func decodeCommandPresentation(operation string, body []byte, status string) (string, string) {
	if len(body) == 0 {
		return "", ""
	}
	if status == "failed" || status == "cancelled" || status == "interrupted" {
		var failure RPCError
		if err := json.Unmarshal(body, &failure); err != nil {
			return "", "invalid structured command failure"
		}
		return "", failure.Message
	}
	field := ""
	switch operation {
	case "submit", "steer", "submit.parts", "steer.parts", "shell.run", "tool.call":
		field = "text"
	case "session.create", "session.open", "session.fork", "session.delete":
		field = "root_id"
	case "workspace.inspect", "workspace.set":
		field = "path"
	case "session.rename":
		field = "title"
	case "goal.set", "goal.run", "goal.from-context":
		field = "goal"
	case "session.effort", "session.effort.get":
		field = "effort"
	case "session.model", "session.model.get", "session.reload":
		var result protocol.ModelResult
		if err := json.Unmarshal(body, &result); err != nil {
			return "", "invalid model result"
		}
		return strings.TrimSpace(result.Model + " @ " + result.Provider), ""
	}
	if field != "" {
		var values map[string]json.RawMessage
		if json.Unmarshal(body, &values) != nil {
			return "", "invalid command result"
		}
		var text string
		if json.Unmarshal(values[field], &text) != nil {
			return "", "invalid command result field"
		}
		return text, ""
	}
	return string(body), ""
}

func fillCommandPresentation(result *CommandResult) {
	result.Output, result.Error = decodeCommandPresentation(result.Operation, result.Result, result.Status)
	if result.Failure != nil {
		result.Error = result.Failure.Message
	}
}
