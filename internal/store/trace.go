package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// TracePage returns current canonical evidence at one SQLite snapshot. A fixed
// revision must remain unchanged across pages; callers restart a conflicted scan.
// It never hydrates workers, replays effects or reconstructs provider input bodies.
func (s *Store) TracePage(ctx context.Context, query session.TraceQuery) (session.TracePage, error) {
	result := session.TracePage{Items: []session.TraceRow{}, Next: query.After}
	if err := query.Validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	var current bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND parent_id IS NULL),COALESCE(MAX(sequence),0) FROM trace_index WHERE root_id=?`, query.RootID, query.RootID).Scan(&current, &result.Revision)
	if err != nil {
		return result, err
	}
	if !current && result.Revision == 0 {
		return result, ErrNotFound
	}
	if query.After > result.Revision || query.ExpectedRevision != nil && *query.ExpectedRevision != result.Revision {
		return result, fmt.Errorf("%w: trace revision changed", ErrConflict)
	}
	// Scan work is independently bounded, including when filters match nothing.
	rows, err := tx.QueryContext(ctx, `SELECT sequence,root_id,session_id,turn_id,kind,source_id FROM trace_index WHERE root_id=? AND sequence>? ORDER BY sequence LIMIT 2049`, query.RootID, query.After)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	var selected []session.TraceRow
	for rows.Next() {
		var row session.TraceRow
		if err := rows.Scan(&row.Sequence, &row.RootID, &row.SessionID, &row.TurnID, &row.SourceKind, &row.SourceID); err != nil {
			return result, err
		}
		selected = append(selected, row)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	projector := traceProjector{q: tx, turns: map[session.TurnID]traceCause{}}
	used := 512
	for index, row := range selected {
		if index == 2048 || len(result.Items) == query.Limit {
			result.HasMore = true
			break
		}
		row.SpanID = session.TraceSpanID(row.SourceKind, row.SourceID)
		span, present, err := projector.source(ctx, row)
		if err != nil {
			return result, err
		}
		if present {
			row.Span = &span
		}
		// Tombstones always pass filters: vanished provenance cannot safely be
		// reconstructed. Removing an unknown span ID is harmless for filtered clients.
		if row.Span != nil && (query.TraceID != "" && span.TraceID != query.TraceID || query.RootsOnly && span.ParentSpanID != nil) {
			result.Next = row.Sequence
			continue
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return result, err
		}
		if used+len(raw)+256 > query.MaxBytes {
			if len(result.Items) == 0 {
				return result, fmt.Errorf("%w: trace row exceeds page byte budget", ErrLimit)
			}
			result.HasMore = true
			break
		}
		used += len(raw) + 256
		result.Items = append(result.Items, row)
		result.Next = row.Sequence
	}
	if !result.HasMore {
		result.Next = result.Revision
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

type (
	traceCause struct {
		traceID string
		parent  *string
		present bool
	}
	traceProjector struct {
		q     querier
		turns map[session.TurnID]traceCause
	}
)

// Turn causality exists only for the exact accepted child input named by a
// successful agents.spawn/submit operation. Caller-authored receipt names alone
// are not authority. Human submissions and mail without a source turn start traces.
func (p *traceProjector) cause(ctx context.Context, turn session.TurnID, depth int) (traceCause, error) {
	if cached, ok := p.turns[turn]; ok {
		return cached, nil
	}
	if depth >= 256 || len(p.turns) >= 8192 {
		return traceCause{}, fmt.Errorf("%w: trace causality bound", ErrLimit)
	}
	var owner session.SessionID
	err := p.q.QueryRowContext(ctx, "SELECT session_id FROM turns WHERE id=?", turn).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return traceCause{}, nil
	}
	if err != nil {
		return traceCause{}, err
	}
	result := traceCause{traceID: session.TraceID(turn), present: true}
	var operation session.OperationID
	var parentTurn session.TurnID
	err = p.q.QueryRowContext(ctx, `SELECT o.id,COALESCE(c.turn_id,o.direct_turn_id)
 FROM inputs i JOIN sessions child ON child.id=i.session_id
 JOIN receipts r ON r.input_id=i.id AND r.client_id='operation'
 JOIN operations o ON o.id=r.request_id AND o.state='succeeded' AND o.capability IN ('agents.spawn','agents.submit')
 LEFT JOIN cells c ON c.id=o.cell_id JOIN turns pt ON pt.id=COALESCE(c.turn_id,o.direct_turn_id)
 JOIN sessions parent ON parent.id=pt.session_id
 WHERE i.turn_id=? AND i.source='agent' AND child.parent_id=parent.id AND child.tree_id=parent.tree_id
 AND json_extract(o.result,'$.value.session_id')=i.session_id AND json_extract(o.result,'$.value.input_id')=i.id`, turn).Scan(&operation, &parentTurn)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return traceCause{}, err
	}
	if err == nil {
		parent, err := p.cause(ctx, parentTurn, depth+1)
		if err != nil {
			return traceCause{}, err
		}
		if parent.present {
			result.traceID = parent.traceID
			result.parent = new(session.TraceSpanID("operation", string(operation)))
		}
	}
	p.turns[turn] = result
	return result, nil
}
