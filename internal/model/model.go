// Package model owns provider request/response values and adapters. These are
// projections of the transcript, never an alternative conversation authority.
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
type Response struct {
	Parts               []session.Part
	Usage               session.ModelUsage
	ReportedCostNanoUSD *int64
}

// Prepared freezes the actual route, pricing and encoded request before durable
// admission. Execute is one external attempt; it must not hide provider retries.
type Prepared struct {
	Snapshot session.ModelRequestSnapshot
	Execute  func(context.Context) (Response, error)
}

func (s Scripted) Prepare(_ context.Context, request Request) (Prepared, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return Prepared{}, err
	}
	hash := sha256.Sum256(raw)
	zero := new(int64(0))
	return Prepared{Snapshot: session.ModelRequestSnapshot{
		Purpose: "turn", Model: request.Selection, Route: "scripted://fixture", Adapter: "scripted", RequestDigest: hex.EncodeToString(hash[:]),
		Prices: session.ModelPrices{Input: zero, Output: zero, Reasoning: zero, CachedInput: zero, CachedOutput: zero}, MaxOutputTokens: 4096, TimeoutMillis: 600000,
	}, Execute: func(ctx context.Context) (Response, error) { return s.Complete(ctx, request) }}, nil
}

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
