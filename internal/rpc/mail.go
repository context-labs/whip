package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchMail(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "mail.send":
		return decode(raw, func(p protocol.SendMailParams) (any, error) {
			request, err := p.Domain()
			if err != nil {
				return nil, err
			}
			value, err := r.SendMail(ctx, request)
			return protocol.MailAdmissionFromDomain(value), err
		})
	case "mail.list":
		return decode(raw, func(p protocol.ListMailParams) (any, error) {
			var state session.MailState
			var after session.MailID
			if p.State != nil {
				state = session.MailState(*p.State)
			}
			if p.After != nil {
				after = session.MailID(*p.After)
			}
			values, err := r.ListMail(ctx, session.SessionID(p.SessionID), state, after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.ListMailResult{Items: []protocol.MailMetadata{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.MailMetadataFromDomain(value))
			}
			return result, nil
		})
	case "mail.read":
		return decode(raw, func(p protocol.ReadMailParams) (any, error) {
			value, err := r.ReadMail(ctx, session.SessionID(p.SessionID), session.MailID(p.MailID))
			return protocol.ReadMailResult{Mail: protocol.MailMetadataFromDomain(value.MailMetadata), Body: value.Body}, err
		})
	default:
		return nil, ErrMethod
	}
}
