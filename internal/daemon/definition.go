package daemon

import (
	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/session"
)

// DefinitionFor selects the agent definition a session kind executes. Only
// model-driven sessions have one; tool hosts run no agent.
func DefinitionFor(kind session.SessionKind) (agentdef.Definition, bool) {
	if kind != session.SessionKindAgent {
		return agentdef.Definition{}, false
	}
	return agentdef.Coding(), true
}
