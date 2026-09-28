package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

// ContextPins reads exact opening input messages in raw chronological order.
// Content references remain references, and an incomplete or oversized set is
// rejected rather than returned as a partial context.
func (s *Store) ContextPins(ctx context.Context, owner session.SessionID, ids []session.MessageID) ([]session.Message, error) {
	return readContextPins(ctx, s.db, owner, ids)
}

func readContextPins(ctx context.Context, q querier, owner session.SessionID, ids []session.MessageID) ([]session.Message, error) {
	if len(ids) > session.MaxCompactionPins {
		return nil, fmt.Errorf("%w: too many context pins", session.ErrInvalid)
	}
	seen := make(map[session.MessageID]bool, len(ids))
	for _, id := range ids {
		if err := session.ValidateID(string(id)); err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: duplicate context pin", session.ErrInvalid)
		}
		seen[id] = true
	}
	result := []session.Message{}
	if len(ids) == 0 {
		_, err := readContextHead(ctx, q, owner)
		return result, err
	}
	size := 0
	for _, id := range ids {
		// A bounded series of primary-key lookups avoids SQLite choosing a
		// session history scan to satisfy an IN-list query's chronological sort.
		message, err := scanMessage(q.QueryRowContext(ctx, messageSelect+` WHERE m.id=? AND m.session_id=?
 AND m.role='user' AND i.kind='prompt'`, id, owner))
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		if len(raw) > MaxPageBytes-size {
			return nil, fmt.Errorf("%w: context pins exceed 4 MiB", ErrLimit)
		}
		size += len(raw)
		result = append(result, message)
	}
	slices.SortFunc(result, func(a, b session.Message) int { return cmp.Compare(a.Sequence, b.Sequence) })
	return result, nil
}

// ContextBoundaryPin identifies the opening input that a partial prompt turn
// must retain. A fully covered terminal turn and a mail-only turn require none.
func (s *Store) ContextBoundaryPin(ctx context.Context, owner session.SessionID, through int64) (*session.MessageID, error) {
	_, pin, err := contextBoundaryPin(ctx, s.db, owner, through)
	return pin, err
}

func contextBoundaryPin(ctx context.Context, q querier, owner session.SessionID, through int64) (session.TurnID, *session.MessageID, error) {
	if through < 1 {
		return "", nil, fmt.Errorf("%w: context boundary must identify a raw message", session.ErrInvalid)
	}
	var turn session.TurnID
	var pin *session.MessageID
	var required bool
	// Naming every role lets the existing turn/role/sequence index seek past
	// the boundary without scanning this turn or later turns in the session.
	err := q.QueryRowContext(ctx, `SELECT boundary.turn_id,opening.id,
 (t.state IN ('running','cancelling') OR EXISTS(SELECT 1 FROM messages later
  WHERE later.turn_id=boundary.turn_id AND later.role IN ('system','user','assistant','tool') AND later.sequence>boundary.sequence))
 FROM messages boundary JOIN turns t ON t.id=boundary.turn_id
 LEFT JOIN inputs i ON i.turn_id=t.id AND i.kind='prompt'
 LEFT JOIN messages opening ON opening.input_id=i.id
 WHERE boundary.session_id=? AND boundary.sequence=?`, owner, through).Scan(&turn, &pin, &required)
	if !required {
		pin = nil
	}
	return turn, pin, found(err)
}
