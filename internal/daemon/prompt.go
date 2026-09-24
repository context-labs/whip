package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/rlm"
)

// refreshPrompt runs once at the common root/child turn boundary. The snapshot
// also supplies explicit skill expansion and the context inspection UI.
func (session *AgentSession) refreshPrompt(ctx context.Context) error {
	options := rlm.PromptOptions{Engine: session.executionEngine(), WorkingDirectory: session.agent.WorkingDir, Identity: session.identity()}
	session.mu.Lock()
	override := session.promptOverride
	session.mu.Unlock()
	var snapshot rlm.PromptSnapshot
	if override != "" && session.parentID == "" {
		// An explicit root override is complete and remains verbatim. Source
		// files that the user chose to replace must not block its execution.
		snapshot = rlm.PromptSnapshot{
			Prompt: override, WorkingDirectory: options.WorkingDirectory, AppliedAt: time.Now(),
			Sources: []rlm.PromptSource{{Kind: "system_override", Scope: "root", Bytes: len(override)}},
		}
	} else {
		var err error
		options, err = session.promptOptions(ctx)
		if err != nil {
			return err
		}
		snapshot, err = rlm.ComposePrompt(options)
		if err != nil {
			return err
		}
	}
	session.agent.SetSystemPrompt(snapshot.Prompt)
	session.mu.Lock()
	session.prompt = snapshot
	session.mu.Unlock()
	return nil
}

func (session *AgentSession) promptOptions(ctx context.Context) (rlm.PromptOptions, error) {
	options := session.effectiveDefinition().PromptOptions(rlm.PromptOptions{Engine: session.executionEngine(), WorkingDirectory: session.agent.WorkingDir, Identity: session.identity()})
	if session.root == nil {
		return options, nil
	}
	roots, err := session.root.store.CapabilityPaths(ctx, session.root.ID(), session.root.AgentID(), session.root.authority.Files)
	if err != nil && !errors.Is(err, capability.ErrDenied) {
		return options, err
	}
	options.ProjectRoots = roots
	options.ProjectDirectoryAllowed = func(path string) (bool, error) {
		err := session.root.store.AuthorizeCapability(ctx, session.root.ID(), session.id, session.authority.Files, "read", path)
		if errors.Is(err, capability.ErrDenied) {
			return false, nil
		}
		return err == nil, err
	}
	return options, nil
}

// effectiveDefinition is the node's definition. A node built outside a runtime
// has the zero value, which means the coding agent, as it does for
// RecursiveRuntimeOptions and Components.
func (session *AgentSession) effectiveDefinition() agentdef.Definition {
	if session.definition.ID == "" {
		return agentdef.Coding()
	}
	return session.definition
}

func (session *AgentSession) executionEngine() string {
	if session.runtime != nil {
		return session.runtime.engine
	}
	if session.root != nil && session.root.meta.ExecutionEngine != "" {
		return session.root.meta.ExecutionEngine
	}
	return rlm.EngineStarlark
}
