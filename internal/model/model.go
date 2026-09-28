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
	Purpose      string
	SessionID    session.SessionID
	TurnID       session.TurnID
	Selection    session.ModelSelection
	Instructions string
	Messages     []Message
	Contents     map[string]Content
	Tools        []Tool
}

type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Content is a bounded, authorized request projection. Durable messages keep
// only references; these bytes live only while preparing a provider request.
type Content struct {
	MediaType string
	Data      []byte
}
type Response struct {
	Parts               []session.Part
	Usage               session.ModelUsage
	ReportedCostNanoUSD *int64
	UsageNote           *string
}

// Chunk is provisional output from one provider attempt. Its fields contain
// incremental fragments, not snapshots or executable transcript parts.
type Chunk struct {
	Text string
	Call *CallChunk
}

type CallChunk struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// Prepared freezes the actual route, pricing and encoded request before durable
// admission. Execute is one external attempt; it must not hide provider retries.
// Callbacks run synchronously and stop before Execute returns.
type Prepared struct {
	Snapshot    session.ModelRequestSnapshot
	Execute     func(context.Context, func(Chunk)) (Response, error)
	MaxAttempts int
}

func (s Scripted) Prepare(_ context.Context, request Request) (Prepared, error) {
	if request.Purpose == "" {
		request.Purpose = "turn"
	}
	if err := session.ValidateID(request.Purpose); err != nil {
		return Prepared{}, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return Prepared{}, err
	}
	var frozen Request
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return Prepared{}, err
	}
	request = frozen
	hash := sha256.Sum256(raw)
	zero := new(int64(0))
	return Prepared{Snapshot: session.ModelRequestSnapshot{
		Purpose: request.Purpose, Model: request.Selection, Route: "scripted://fixture", Adapter: "scripted", RequestDigest: hex.EncodeToString(hash[:]),
		Prices: session.ModelPrices{Input: zero, Output: zero, Reasoning: zero, CachedInput: zero, CachedOutput: zero}, MaxOutputTokens: 4096, TimeoutMillis: 600000,
		InputTokenBound: new(int64(0)), // Scripted execution bills no model input tokens.
	}, Execute: func(ctx context.Context, emit func(Chunk)) (Response, error) {
		response, err := s.Complete(ctx, request)
		if err == nil {
			emitResponse(response, emit)
		}
		return response, err
	}}, nil
}

func emitResponse(response Response, emit func(Chunk)) {
	if emit == nil {
		return
	}
	index := 0
	for _, part := range response.Parts {
		if part.Type == "text" {
			emit(Chunk{Text: part.Text})
		} else if part.Call != nil {
			emit(Chunk{Call: &CallChunk{Index: index, ID: part.Call.ID, Name: part.Call.Name, Arguments: string(part.Call.Arguments)}})
			index++
		}
	}
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
