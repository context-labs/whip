package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) AdmitHostOperation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, operation session.HostOperation) (store.Admission, error) {
	if err := r.Err(); err != nil {
		return store.Admission{}, err
	}
	result, err := r.store.AdmitHostOperation(ctx, identity, owner, operation)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) runHostOperation(ctx context.Context, claim store.Claim) (runner.Outcome, error) {
	if claim.Input == nil || claim.Input.HostOperation == nil {
		return runner.Outcome{}, errors.New("direct host input is missing")
	}
	accepted := claim.Input.HostOperation
	decoder := json.NewDecoder(bytes.NewReader(accepted.Arguments))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil {
		return runner.Outcome{}, err
	}
	_, _, err := r.tools.Call(ctx, tool.Invocation{SessionID: claim.Turn.SessionID, DirectTurnID: claim.Turn.ID, RequestID: string(claim.Input.ID), Module: accepted.Module, Name: accepted.Name, Arguments: arguments})
	if err != nil {
		return runner.Failure(err), nil
	}
	return runner.Outcome{State: session.Succeeded}, nil
}

// invocationTurn reads real provenance for both cell calls and human work.
func (r *Runtime) invocationTurn(ctx context.Context, owner session.SessionID, call tool.Invocation) (session.TurnID, error) {
	if call.DirectTurnID != "" {
		if call.CellID != "" {
			return "", session.ErrInvalid
		}
		if _, err := r.store.HostTurnSession(ctx, owner, call.DirectTurnID); err != nil {
			return "", err
		}
		return call.DirectTurnID, nil
	}
	cell, err := r.store.Cell(ctx, call.CellID)
	if err != nil {
		return "", err
	}
	if cell.SessionID != owner {
		return "", store.ErrConflict
	}
	return cell.TurnID, nil
}
