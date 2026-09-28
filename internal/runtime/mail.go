package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) SendMail(ctx context.Context, request session.MailSpec) (session.MailAdmission, error) {
	if err := r.Err(); err != nil {
		return session.MailAdmission{}, err
	}
	result, err := r.store.SendMail(ctx, request)
	if err == nil {
		r.Wake()
	}
	return result, err
}

// ListMail and ReadMail are client inspection. They never establish agent
// observations, acknowledge delivery or admit execution.
func (r *Runtime) ListMail(ctx context.Context, recipient session.SessionID, state session.MailState, after session.MailID, limit int) ([]session.MailMetadata, error) {
	return r.store.ListMail(ctx, recipient, state, after, limit)
}

func (r *Runtime) ReadMail(ctx context.Context, recipient session.SessionID, id session.MailID) (session.Mail, error) {
	return r.store.ReadMail(ctx, recipient, id)
}

func (r *Runtime) ObserveSteers(ctx context.Context, turn session.TurnID) ([]session.Message, error) {
	return r.store.ObserveSteers(ctx, turn)
}

func (r *Runtime) prepareMail(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var request any
	switch call.Name {
	case "send":
		request = &session.MailSend{Delivery: "queued"}
	case "list":
		request = &session.MailList{State: "pending", Limit: 20}
	case "read":
		request = &session.MailRead{}
	case "complete":
		request = &session.MailComplete{}
	case "defer":
		request = &session.MailDefer{}
	default:
		return tool.Prepared{}, fmt.Errorf("%w: unsupported mail operation %s", session.ErrInvalid, call.Name)
	}
	if err := decodeArguments(call.Arguments, request); err != nil {
		return tool.Prepared{}, err
	}
	if send, ok := request.(*session.MailSend); ok {
		if err := send.Validate(); err != nil {
			return tool.Prepared{}, err
		}
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{
		Capability: "mail." + call.Name, Resource: string(current.TreeID), Arguments: arguments,
		Apply: func(ctx context.Context, id session.OperationID) (any, error) {
			value, err := r.store.ApplyMailOperation(ctx, id)
			if err == nil && (call.Name == "send" || call.Name == "defer") {
				r.Wake()
			}
			return value, err
		},
	}, nil
}
