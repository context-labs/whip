package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/session"
)

// DefinitionSource reads registered definitions. *session.Store implements it;
// nil means no registered definitions exist.
type DefinitionSource interface {
	LatestDefinition(context.Context, string) (session.DefinitionRecord, error)
	LoadDefinition(context.Context, string, string) (session.DefinitionRecord, error)
	ListDefinitions(context.Context) ([]session.DefinitionRecord, error)
}

// DefinitionFor resolves the agent definition a session executes. Tool hosts
// run no agent and report false. Built-ins resolve from the binary by id; a
// registered definition resolves from the store by id and pinned revision. Rows
// written before definitions existed resolve to the coding agent. A missing id
// or revision is an error so a daemon that no longer knows the definition
// refuses the session instead of running it as a different agent.
func DefinitionFor(ctx context.Context, source DefinitionSource, meta session.Meta) (agentdef.Definition, bool, error) {
	if meta.Kind != session.SessionKindAgent {
		return agentdef.Definition{}, false, nil
	}
	id := meta.Definition
	if id == "" {
		id = "coding"
	}
	if definition, ok := agentdef.Lookup(id); ok {
		return definition, true, nil
	}
	if source == nil || meta.DefinitionRevision == "" {
		return agentdef.Definition{}, false, fmt.Errorf("session %s uses unknown agent definition %q (available: %s)", meta.ID, id, strings.Join(agentdef.IDs(), ", "))
	}
	record, err := source.LoadDefinition(ctx, id, meta.DefinitionRevision)
	if errors.Is(err, session.ErrNoDefinition) {
		return agentdef.Definition{}, false, fmt.Errorf("session %s pins agent definition %q revision %s, which is not registered", meta.ID, id, meta.DefinitionRevision)
	}
	if err != nil {
		return agentdef.Definition{}, false, err
	}
	definition, err := agentdef.Decode(record.Body)
	if err != nil {
		return agentdef.Definition{}, false, fmt.Errorf("session %s agent definition %q revision %s: %w", meta.ID, id, meta.DefinitionRevision, err)
	}
	return definition, true, nil
}

// latestDefinition resolves an id for a new session: a built-in, or the latest
// registered revision. The revision is empty for built-ins.
func latestDefinition(ctx context.Context, source DefinitionSource, id string) (agentdef.Definition, string, error) {
	if definition, ok := agentdef.Lookup(id); ok {
		return definition, "", nil
	}
	var record session.DefinitionRecord
	var err error
	if source != nil {
		record, err = source.LatestDefinition(ctx, id)
	} else {
		err = session.ErrNoDefinition
	}
	if errors.Is(err, session.ErrNoDefinition) {
		return agentdef.Definition{}, "", fmt.Errorf("unknown agent definition %q (available: %s)", id, strings.Join(availableDefinitionIDs(ctx, source), ", "))
	}
	if err != nil {
		return agentdef.Definition{}, "", err
	}
	definition, err := agentdef.Decode(record.Body)
	if err != nil {
		return agentdef.Definition{}, "", fmt.Errorf("agent definition %q revision %s: %w", id, record.Revision, err)
	}
	return definition, record.Revision, nil
}

// availableDefinitionIDs lists built-ins then registered ids, for error messages.
func availableDefinitionIDs(ctx context.Context, source DefinitionSource) []string {
	ids := agentdef.IDs()
	if source == nil {
		return ids
	}
	records, err := source.ListDefinitions(ctx)
	if err != nil {
		return ids
	}
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

// rootGrants derives a new root's bootstrap grants from its definition. A
// session without a definition receives full grants.
func rootGrants(definition agentdef.Definition, hasDefinition bool) session.RootGrants {
	if !hasDefinition {
		return session.FullRootGrants()
	}
	files, shell, mcp := agentdef.Operations(definition.Capabilities)
	return session.RootGrants{Files: files, Shell: shell, MCP: mcp, Tools: agentdef.ToolOperations(definition.ToolNames())}
}
