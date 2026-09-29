package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
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
	daemon     *Daemon
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
	// Accepted work can still be writing caller-owned results when cancellation
	// arrives. Observe its completion before returning ownership to the caller.
	// Actor work is bounded and uses c.ctx; slow deletion runs outside this actor.
	return <-request.done
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
	// Effort is "off" or a catalog level. Blank asks the daemon to resolve the
	// definition's default, else the configured default, against the model.
	Effort         string `json:"effort,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
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
			record, err = c.store.CreateSessionForCommandWithDefinition(actorCtx, admission.ClientID, admission.CommandID, create.Kind, create.CWD, create.Model, create.Provider, create.Effort, create.PermissionMode, create.ExecutionEngine, create.Definition, create.DefinitionRevision)
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

var errMetadataRootLive = errors.New("metadata command requires root actor")

// SessionMetadataCommand mutates cold session metadata without reconstructing
// its runner (the workspace may be gone). The registry check runs on this
// actor, before admission; callers reroute errMetadataRootLive outside it.
// Holding the registry lock through the cold mutation excludes concurrent Open.
func (c *Control) SessionMetadataCommand(ctx context.Context, admission session.CommandAdmission, operation string, payload json.RawMessage, rootID string) (record session.CommandRecord, err error) {
	titleChanged := false
	err = c.route(ctx, func(actorCtx context.Context) error {
		if c.daemon != nil {
			c.daemon.mu.Lock()
			defer c.daemon.mu.Unlock()
			if c.daemon.closing {
				return ErrClosed
			}
			if entry := c.daemon.roots[rootID]; entry != nil {
				select {
				case <-entry.ready:
					if entry.root != nil {
						select {
						case <-entry.root.Done():
						default:
							return errMetadataRootLive
						}
					}
				default:
					return errMetadataRootLive
				}
			}
		}
		// Daemon scope, like session.delete: root admission enqueues an inbox
		// item, which requires the agent row created only by opening the root.
		admission.Scope = session.CommandScopeDaemon
		admission.RootID = ""
		admission.AgentID = ""
		admission.Kind = operation
		admitted, err := c.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return err
		}
		record = admitted.Command
		if !admitted.New {
			return nil
		}
		if err := c.store.SetCommandState(actorCtx, admission.ClientID, admission.CommandID, "running"); err != nil {
			return c.finishFailure(actorCtx, admission, err, &record)
		}
		var output string
		var eventKind string
		var event protocol.SessionUpdateEvent
		switch operation {
		case "session.archive":
			var params protocol.ArchiveParams
			if err := json.Unmarshal(payload, &params); err != nil {
				return c.finishFailure(actorCtx, admission, err, &record)
			}
			if err := c.store.SetArchived(actorCtx, rootID, params.Archived); err != nil {
				return c.finishFailure(actorCtx, admission, err, &record)
			}
			output, err = marshalClientOutput(protocol.ArchiveResult(params), nil)
			eventKind = "session.archived.updated"
			event.Archived = &params.Archived
		case "session.rename":
			var params protocol.TitleParams
			if err := json.Unmarshal(payload, &params); err != nil {
				return c.finishFailure(actorCtx, admission, err, &record)
			}
			title := strings.TrimSpace(params.Title)
			if title == "" {
				return c.finishFailure(actorCtx, admission, errors.New("session title is required"), &record)
			}
			if err := c.store.SetTitle(rootID, title); err != nil {
				return c.finishFailure(actorCtx, admission, err, &record)
			}
			titleChanged = true
			output, err = marshalClientOutput(protocol.TitleResult{Title: title}, nil)
			eventKind = "session.title.updated"
			event.Title = title
		default:
			return c.finishFailure(actorCtx, admission, fmt.Errorf("unsupported metadata command %q", operation), &record)
		}
		if err == nil {
			var eventData []byte
			eventData, err = json.Marshal(event)
			if err == nil {
				_, err = c.store.AppendRootEvent(actorCtx, rootID, eventKind, session.RuntimePayload{
					Data: eventData, MediaType: "application/json", Source: operation,
				})
			}
		}
		if err != nil {
			return c.finishFailure(actorCtx, admission, err, &record)
		}
		record.Outcome, err = c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "succeeded", session.RuntimePayload{
			Data: []byte(output), MediaType: "application/json", Source: operation,
		})
		if err == nil {
			record.Status = "succeeded"
		}
		return err
	})
	// Even an event/command-outcome failure cannot undo the committed title.
	// route returns ownership only after releasing the registry lock.
	if titleChanged && c.daemon != nil {
		c.daemon.notifyTitleChanged(rootID)
	}
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

// defaultPermissionMode fails closed for legacy or unrecognized host settings.
func defaultPermissionMode(cfg *config.Config) string {
	if cfg.DefaultPermissionMode == session.PermissionModeAutomatic {
		return session.PermissionModeAutomatic
	}
	return session.PermissionModePrompt
}

// sessionDefaults fills omitted routing from the definition's model defaults,
// then omitted routing, engine, and permission mode from host configuration.
func sessionDefaults(create CreateSession, definition agentdef.Definition) (CreateSession, error) {
	if create.ExecutionEngine != "" && create.ExecutionEngine != "starlark" && create.ExecutionEngine != "quickjs" {
		return create, fmt.Errorf("unknown execution engine %q", create.ExecutionEngine)
	}
	if create.Kind == session.SessionKindAgent && create.Model == "" && definition.Model.Model != "" {
		create.Model, create.Provider = definition.Model.Model, definition.Model.Provider
	}
	needRoute := create.Kind == session.SessionKindAgent && (create.Model == "" || create.Provider == "")
	needPermissionMode := create.Kind == session.SessionKindAgent && create.PermissionMode == ""
	needEffort := create.Kind == session.SessionKindAgent
	if !needRoute && !needPermissionMode && !needEffort && create.ExecutionEngine != "" {
		return create, nil
	}
	cfg, _, err := config.ReadVersioned()
	if err != nil {
		return create, err
	}
	if needPermissionMode {
		create.PermissionMode = defaultPermissionMode(cfg)
	}
	if create.ExecutionEngine == "" {
		create.ExecutionEngine = cfg.RLM.Engine()
	}
	if create.ExecutionEngine != "starlark" && create.ExecutionEngine != "quickjs" {
		return create, fmt.Errorf("unknown default execution engine %q", create.ExecutionEngine)
	}
	if needRoute {
		provider, _, _, _, err := cfg.ResolveRoute(create.Model, create.Provider)
		if err != nil {
			return create, err
		}
		if create.Model == "" {
			create.Model = cfg.DefaultModel
		}
		create.Provider = provider
	}
	if needEffort {
		if create.Effort != "" {
			if err := validateConfiguredEffort(cfg, create.Model, create.Provider, create.Effort); err != nil {
				return create, err
			}
		}
		create.Effort = resolveEffort(cfg, create.Model, create.Provider, create.Effort, definition.Model.Effort)
	}
	return create, nil
}

// resolveEffort turns a requested or inherited effort into the concrete value
// a session stores: the explicit request, else the definition's default, else
// the configured default, resolved against the model's catalog entry so a new
// session never opens on a level its provider rejects. The result is "off" or
// a level; blank is never stored.
func resolveEffort(cfg *config.Config, model, provider, requested, definitionDefault string) string {
	pinned := requested
	if pinned == "" {
		pinned = definitionDefault
	}
	if pinned == "" {
		pinned = cfg.DefaultEffort
	}
	catalogProvider, modelID := provider, model
	if resolved, _, _, apiID, err := cfg.ResolveRoute(model, provider); err == nil {
		catalogProvider, modelID = resolved, apiID
	}
	return config.ResolveEffort(config.LoadCatalogs(), catalogProvider, modelID, pinned)
}
