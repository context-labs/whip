package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

type controlRequest struct {
	work func(context.Context) error
	done chan error
}

// Control serializes daemon-wide state changes independently of root actors.
type Control struct {
	ctx        context.Context
	requests   chan controlRequest
	done       chan struct{}
	store      *session.Store
	deletes    chan func()
	deleteDone chan struct{}
}

func newControl(ctx context.Context, store *session.Store) *Control {
	control := &Control{ctx: ctx, requests: make(chan controlRequest), done: make(chan struct{}), store: store, deletes: make(chan func(), 64), deleteDone: make(chan struct{})}
	go func() {
		defer close(control.deleteDone)
		for {
			select {
			case <-ctx.Done():
				return
			case work := <-control.deletes:
				work()
			}
		}
	}()
	go control.run()
	return control
}

func (c *Control) run() {
	defer close(c.done)
	defer func() { <-c.deleteDone }()
	for {
		select {
		case <-c.ctx.Done():
			return
		case request := <-c.requests:
			request.done <- request.work(c.ctx)
		}
	}
}

func (c *Control) route(ctx context.Context, work func(context.Context) error) error {
	request := controlRequest{work: work, done: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return ErrClosed
	case c.requests <- request:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return ErrClosed
	case err := <-request.done:
		return err
	}
}

type CreateSession struct {
	ExecutionEngine string `json:"execution_engine,omitempty"`
	// Definition selects the agent definition; empty means the coding agent.
	// DefinitionRevision is pinned by the daemon for registered definitions and
	// is not accepted from clients.
	Definition         string              `json:"definition,omitempty"`
	DefinitionRevision string              `json:"definition_revision,omitempty"`
	Kind               session.SessionKind `json:"kind"`
	CWD                string              `json:"cwd"`
	Model              string              `json:"model"`
	Provider           string              `json:"provider"`
	PermissionMode     string              `json:"permission_mode,omitempty"`
}

func (c *Control) CreateSession(ctx context.Context, admission session.CommandAdmission, create CreateSession) (record session.CommandRecord, err error) {
	err = c.route(ctx, func(actorCtx context.Context) error {
		admission.Scope = session.CommandScopeDaemon
		admission.RootID = ""
		admission.AgentID = ""
		admission.Kind = "session.create"
		admitted, err := c.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return err
		}
		record = admitted.Command
		if !admitted.New {
			return nil
		}
		create, err = resolveSessionDefaults(actorCtx, c.store, create)
		if err == nil {
			record, err = c.store.CreateSessionForCommandWithDefinition(actorCtx, admission.ClientID, admission.CommandID, create.Kind, create.CWD, create.Model, create.Provider, create.PermissionMode, create.ExecutionEngine, create.Definition, create.DefinitionRevision)
		}
		if err != nil {
			_, finishErr := c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "failed", session.RuntimePayload{Data: encodeCommandOutcome("session.create", "", err), MediaType: "application/json"})
			return errors.Join(err, finishErr)
		}
		return nil
	})
	return record, err
}

func (c *Control) ListSessions(ctx context.Context, admission session.CommandAdmission, limit int) (record session.CommandRecord, err error) {
	err = c.route(ctx, func(actorCtx context.Context) error {
		admission.Scope = session.CommandScopeDaemon
		admission.RootID = ""
		admission.AgentID = ""
		admission.Kind = "session.list"
		admitted, err := c.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return err
		}
		record = admitted.Command
		if !admitted.New {
			return nil
		}
		metas, err := c.store.RecentContext(actorCtx, limit)
		if err != nil {
			return c.finishFailure(actorCtx, admission, err, &record)
		}
		outcome, err := json.Marshal(metas)
		if err != nil {
			return c.finishFailure(actorCtx, admission, err, &record)
		}
		record.Outcome, err = c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "succeeded", session.RuntimePayload{
			Data: outcome, MediaType: "application/json", Source: "session list",
		})
		if err == nil {
			record.Status = "succeeded"
		}
		return err
	})
	return record, err
}

// AcceptDeleteSession commits admission before asynchronous root shutdown begins.
func (c *Control) AcceptDeleteSession(ctx context.Context, admission session.CommandAdmission, rootID string, remove func(context.Context, string) error) (session.CommandRecord, error) {
	record, _, err := c.deleteSession(ctx, admission, rootID, remove)
	return record, err
}

func (c *Control) DeleteSession(ctx context.Context, admission session.CommandAdmission, rootID string, remove func(context.Context, string) error) (session.CommandRecord, error) {
	record, completion, err := c.deleteSession(ctx, admission, rootID, remove)
	if err != nil || completion == nil {
		return record, err
	}
	select {
	case completed := <-completion:
		return completed.record, completed.err
	case <-ctx.Done():
		return record, ctx.Err()
	case <-c.ctx.Done():
		return record, ErrClosed
	}
}

type deletionResult struct {
	record session.CommandRecord
	err    error
}

func (c *Control) deleteSession(ctx context.Context, admission session.CommandAdmission, rootID string, remove func(context.Context, string) error) (record session.CommandRecord, completion chan deletionResult, err error) {
	err = c.route(ctx, func(actorCtx context.Context) error {
		admission.Scope = session.CommandScopeDaemon
		admission.RootID, admission.AgentID = "", ""
		admission.Kind = "session.delete"
		// Only this actor produces work. Reserve capacity before committing new
		// work, while allowing retries to observe an existing command.
		if len(c.deletes) == cap(c.deletes) {
			if _, loadErr := c.store.LoadCommand(actorCtx, admission.ClientID, admission.CommandID); loadErr != nil {
				return errors.New("daemon deletion capacity exhausted")
			}
		}
		admitted, err := c.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return err
		}
		record = admitted.Command
		if !admitted.New {
			return nil
		}
		completion = make(chan deletionResult, 1)
		done := completion
		accepted := record
		c.deletes <- func() {
			result := accepted
			err := c.store.SetCommandState(c.ctx, admission.ClientID, admission.CommandID, "running")
			if err == nil {
				err = remove(c.ctx, rootID)
			}
			actionErr := err
			err = c.route(c.ctx, func(actorCtx context.Context) error {
				if actionErr != nil {
					return c.finishFailure(actorCtx, admission, actionErr, &result)
				}
				var finishErr error
				result.Outcome, finishErr = c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "succeeded", session.RuntimePayload{
					Data: encodeCommandOutcome("session.delete", rootID, nil), MediaType: "application/json", Source: "session delete",
				})
				if finishErr == nil {
					result.Status = "succeeded"
				}
				return finishErr
			})
			done <- deletionResult{record: result, err: err}
		}
		return nil
	})
	return record, completion, err
}

func (c *Control) finishFailure(ctx context.Context, admission session.CommandAdmission, actionErr error, record *session.CommandRecord) error {
	value, finishErr := c.store.FinishCommand(ctx, admission.ClientID, admission.CommandID, "failed", session.RuntimePayload{
		Data: encodeCommandOutcome(admission.Kind, "", actionErr), MediaType: "application/json", Source: admission.Kind,
	})
	record.Outcome = value
	record.Status = "failed"
	return errors.Join(actionErr, finishErr)
}

func (c *Control) Checkpoint(ctx context.Context, admission session.CommandAdmission, generation int64) (record session.CommandRecord, err error) {
	err = c.route(ctx, func(actorCtx context.Context) error {
		admission.Scope = session.CommandScopeDaemon
		admission.RootID = ""
		admission.AgentID = ""
		admission.Kind = "daemon.checkpoint"
		admitted, err := c.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return err
		}
		record = admitted.Command
		if !admitted.New {
			return nil
		}
		cursors, err := c.store.RootCursors(actorCtx)
		if err != nil {
			return err
		}
		outcome, err := json.Marshal(RestartNotice{Generation: generation, Cursors: cursors})
		if err != nil {
			return err
		}
		record.Outcome, err = c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "succeeded", session.RuntimePayload{
			Data: outcome, MediaType: "application/json", Source: "daemon checkpoint",
		})
		if err == nil {
			record.Status = "succeeded"
		}
		return err
	})
	return record, err
}

// Resolve omitted routing on the execution host, after deduplication. A retry
// must observe the original session even if host defaults have since changed.
func resolveSessionDefaults(ctx context.Context, source DefinitionSource, create CreateSession) (CreateSession, error) {
	create.DefinitionRevision = ""
	if create.Kind != session.SessionKindAgent {
		if create.Definition != "" {
			return create, errors.New("only agent sessions run an agent definition")
		}
		return sessionDefaults(create, agentdef.Definition{})
	}
	if create.Definition == "" {
		create.Definition = "coding"
	}
	definition, revision, err := latestDefinition(ctx, source, create.Definition)
	if err != nil {
		return create, err
	}
	create.DefinitionRevision = revision
	return sessionDefaults(create, definition)
}

// sessionDefaults fills omitted routing from the definition's model defaults,
// then from host configuration.
func sessionDefaults(create CreateSession, definition agentdef.Definition) (CreateSession, error) {
	if create.ExecutionEngine != "" && create.ExecutionEngine != "starlark" && create.ExecutionEngine != "quickjs" {
		return create, fmt.Errorf("unknown execution engine %q", create.ExecutionEngine)
	}
	if create.Kind == session.SessionKindAgent && create.Model == "" && definition.Model.Model != "" {
		create.Model, create.Provider = definition.Model.Model, definition.Model.Provider
	}
	needRoute := create.Kind == session.SessionKindAgent && (create.Model == "" || create.Provider == "")
	if !needRoute && create.ExecutionEngine != "" {
		return create, nil
	}
	cfg, _, err := config.ReadVersioned()
	if err != nil {
		return create, err
	}
	if create.ExecutionEngine == "" {
		create.ExecutionEngine = cfg.RLM.Engine()
	}
	if create.ExecutionEngine != "starlark" && create.ExecutionEngine != "quickjs" {
		return create, fmt.Errorf("unknown default execution engine %q", create.ExecutionEngine)
	}
	if !needRoute {
		return create, nil
	}
	provider, _, _, _, err := cfg.ResolveRoute(create.Model, create.Provider)
	if err != nil {
		return create, err
	}
	if create.Model == "" {
		create.Model = cfg.DefaultModel
	}
	create.Provider = provider
	return create, nil
}
