package client

import (
	"context"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
)

// Usage returns cumulative accounting, including captured descendant charges
// for a root. It is not a context-window measurement or one turn's usage.
func (s *Session) Usage(ctx context.Context) (protocol.Usage, error) {
	var value protocol.Usage
	if err := s.client.Call(ctx, "usage.get", protocol.SessionParams{SessionID: s.id}, &value); err != nil {
		return protocol.Usage{}, err
	}
	if value.SessionID != s.id {
		return protocol.Usage{}, errors.New("usage ownership mismatch")
	}
	return value, nil
}

func (s *Session) TurnUsage(ctx context.Context, turn protocol.ID) (protocol.TurnUsage, error) {
	var value protocol.TurnUsage
	if err := s.client.Call(ctx, "usage.turn", protocol.TurnUsageParams{SessionID: s.id, TurnID: turn}, &value); err != nil {
		return protocol.TurnUsage{}, err
	}
	if value.Usage.SessionID != s.id || value.TurnID != turn {
		return protocol.TurnUsage{}, errors.New("turn usage ownership mismatch")
	}
	return value, nil
}

// ContextUsage retains the host's evidence basis, captured tail, unknown
// capacity and staleness. Callers must not relabel it as current occupancy.
func (s *Session) ContextUsage(ctx context.Context) (protocol.ContextUsage, error) {
	var value protocol.ContextUsage
	if err := s.client.Call(ctx, "context.usage", protocol.SessionParams{SessionID: s.id}, &value); err != nil {
		return protocol.ContextUsage{}, err
	}
	if value.SessionID != s.id {
		return protocol.ContextUsage{}, errors.New("context usage ownership mismatch")
	}
	return value, nil
}

// CellOutput is an ephemeral preview. A consumer must also match its epoch and
// history revision to its current observation before displaying it.
func (s *Session) CellOutput(ctx context.Context) (protocol.CellOutput, error) {
	var value protocol.CellOutput
	if err := s.client.Call(ctx, "cells.output", protocol.SessionParams{SessionID: s.id}, &value); err != nil {
		return protocol.CellOutput{}, err
	}
	if value.Preview != nil && (value.Preview.SessionID != s.id || len(value.Preview.Text) > 64<<10) {
		return protocol.CellOutput{}, errors.New("cell output ownership or byte bound mismatch")
	}
	return value, nil
}
