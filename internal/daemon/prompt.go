package daemon

import (
	"context"
	"time"

	"github.com/context-labs/whip/internal/rlm"
)

// refreshPrompt runs once at the common root/child turn boundary. The snapshot
// also supplies explicit skill expansion and the context inspection UI.
func (session *AgentSession) refreshPrompt(ctx context.Context) error {
	options := rlm.PromptOptions{WorkingDirectory: session.agent.WorkingDir, Identity: session.identity()}
	if session.root != nil {
		roots, err := session.root.store.CapabilityPaths(ctx, session.root.ID(), session.root.AgentID(), session.root.authority.Files)
		if err != nil {
			return err
		}
		options.ProjectRoots = roots
	}
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
