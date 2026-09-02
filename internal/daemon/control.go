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
	CWD      string `json:"cwd"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
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
		record, err = c.store.CreateSessionForCommand(actorCtx, admission.ClientID, admission.CommandID, create.CWD, create.Model, create.Provider)
		if err != nil {
			_, finishErr := c.store.FinishCommand(actorCtx, admission.ClientID, admission.CommandID, "failed", session.RuntimePayload{Data: []byte(err.Error())})
			return errors.Join(err, finishErr)
		}
		return nil
	})
	return record, err
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
