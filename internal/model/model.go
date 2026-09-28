// Package model owns provider request/response values and adapters. These are
// projections of the transcript, never an alternative conversation authority.
package model

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type Message struct {
	Role  session.Role
	Parts []session.Part
}
type Request struct {
	SessionID    session.SessionID
	TurnID       session.TurnID
	Selection    session.ModelSelection
	Instructions string
	Messages     []Message
}
type Response struct{ Parts []session.Part }

// Scripted is a deterministic provider for disposable development and contract
// fixtures. It is injected into the ordinary runner, not a second runtime path.
type Scripted struct{ Delay time.Duration }

func (s Scripted) Complete(ctx context.Context, request Request) (Response, error) {
	if request.Selection.Provider != "scripted" || request.Selection.Name != "scripted" {
		return Response{}, errors.New("scripted provider requires the scripted model selection")
	}
	if s.Delay > 0 {
		timer := time.NewTimer(s.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	for _, v := range slices.Backward(request.Messages) {
		if v.Role != session.User {
			continue
		}
		for _, part := range v.Parts {
			if part.Type == "text" {
				return Response{Parts: []session.Part{{Type: "text", Text: "ack: " + part.Text}}}, nil
			}
		}
	}
	return Response{}, errors.New("scripted provider requires text input")
}
