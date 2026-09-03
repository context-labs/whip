package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

type controlRequest struct {
	work func(context.Context) error
	done chan error
}

// Control serializes daemon-wide state changes independently of root actors.
type Control struct {
	ctx      context.Context
	requests chan controlRequest
	done     chan struct{}
	store    *session.Store
}

func newControl(ctx context.Context, store *session.Store) *Control {
	control := &Control{ctx: ctx, requests: make(chan controlRequest), done: make(chan struct{}), store: store}
	go control.run()
	return control
}

func (c *Control) run() {
	defer close(c.done)
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
	Kind     session.SessionKind `json:"kind"`
	CWD      string              `json:"cwd"`
	Model    string              `json:"model"`
	Provider string              `json:"provider"`
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
		record, err = c.store.CreateSessionForCommand(actorCtx, admission.ClientID, admission.CommandID, create.Kind, create.CWD, create.Model, create.Provider)
		if err != nil {
			_, finishErr := c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "failed", session.RuntimePayload{Data: []byte(err.Error())})
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

func (c *Control) DeleteSession(ctx context.Context, admission session.CommandAdmission, rootID string, remove func(context.Context, string) error) (record session.CommandRecord, err error) {
	err = c.route(ctx, func(actorCtx context.Context) error {
		admission.Scope = session.CommandScopeDaemon
		admission.RootID = ""
		admission.AgentID = ""
		admission.Kind = "session.delete"
		admitted, err := c.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return err
		}
		record = admitted.Command
		if !admitted.New {
			return nil
		}
		if err := remove(actorCtx, rootID); err != nil {
			return c.finishFailure(actorCtx, admission, err, &record)
		}
		record.Outcome, err = c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "succeeded", session.RuntimePayload{
			Data: []byte(rootID), MediaType: "text/plain", Source: "session delete",
		})
		if err == nil {
			record.Status = "succeeded"
		}
		return err
	})
	return record, err
}

func (c *Control) finishFailure(ctx context.Context, admission session.CommandAdmission, actionErr error, record *session.CommandRecord) error {
	value, finishErr := c.store.FinishCommand(ctx, admission.ClientID, admission.CommandID, "failed", session.RuntimePayload{
		Data: []byte(actionErr.Error()), MediaType: "text/plain", Source: admission.Kind,
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
