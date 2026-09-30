package commandpresentation

import (
	"encoding/json"
	"strings"

	"github.com/context-labs/whip/internal/protocol"
)

// Decode formats stored command outcomes for existing text presenters.
func Decode(operation string, body []byte, status string) (string, string) {
	if len(body) == 0 {
		return "", ""
	}
	if status == "failed" || status == "cancelled" || status == "interrupted" {
		var failure protocol.RPCError
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
