package daemon

import (
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/session"
)

// DefinitionFor resolves the agent definition a session executes. Tool hosts
// run no agent and report false. An agent session names its definition; rows
// written before definitions existed resolve to the coding agent. An unknown id
// is an error so a daemon that no longer knows the definition refuses the
// session instead of running it as a different agent.
func DefinitionFor(meta session.Meta) (agentdef.Definition, bool, error) {
	if meta.Kind != session.SessionKindAgent {
		return agentdef.Definition{}, false, nil
	}
	id := meta.Definition
	if id == "" {
		id = "coding"
	}
	definition, ok := agentdef.Lookup(id)
	if !ok {
		return agentdef.Definition{}, false, fmt.Errorf("session %s uses unknown agent definition %q (available: %s)", meta.ID, id, strings.Join(agentdef.IDs(), ", "))
	}
	return definition, true, nil
}

// rootGrants derives a new root's bootstrap grants from its definition. A
// session without a definition receives full grants.
func rootGrants(definition agentdef.Definition, hasDefinition bool) session.RootGrants {
	if !hasDefinition {
		return session.FullRootGrants()
	}
	files, shell, mcp := agentdef.Operations(definition.Capabilities)
	return session.RootGrants{Files: files, Shell: shell, MCP: mcp}
}
