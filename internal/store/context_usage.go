package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func readContextScope(ctx context.Context, q querier, owner session.SessionID) (v session.ModelContextScope, err error) {
	err = q.QueryRowContext(ctx, `SELECT s.id,s.config_revision,s.history_revision,COALESCE(h.revision,0),
 COALESCE((SELECT MAX(sequence) FROM messages WHERE session_id=s.id AND retired_revision IS NULL),0)
 FROM sessions s LEFT JOIN context_heads h ON h.session_id=s.id WHERE s.id=?`, owner).
		Scan(&v.SessionID, &v.ConfigRevision, &v.HistoryRevision, &v.ContextRevision, &v.ThroughSequence)
	return v, found(err)
}

// ModelContextScope is one scalar read snapshot. The turn's captured config
// remains authoritative even if a human configures the next turn concurrently.
func (s *Store) ModelContextScope(ctx context.Context, id session.TurnID) (session.ModelContextScope, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return session.ModelContextScope{}, err
	}
	defer func() { _ = tx.Rollback() }()
	turn, err := readTurn(ctx, tx, id)
	if err != nil {
		return session.ModelContextScope{}, err
	}
	value, err := readContextScope(ctx, tx, turn.SessionID)
	if err != nil {
		return session.ModelContextScope{}, err
	}
	if value.HistoryRevision != turn.HistoryRevision {
		return session.ModelContextScope{}, ErrConflict
	}
	value.ConfigRevision = turn.ConfigRevision
	if err = tx.Commit(); err != nil {
		return session.ModelContextScope{}, err
	}
	return value, nil
}

func validateContextEvidence(ctx context.Context, q querier, turn session.Turn, v *session.ModelContextEvidence) error {
	if v == nil {
		return nil
	}
	if v.SessionID != turn.SessionID || v.ConfigRevision != turn.ConfigRevision || v.HistoryRevision != turn.HistoryRevision {
		return session.ErrInvalid
	}
	current, err := readContextScope(ctx, q, turn.SessionID)
	if err != nil {
		return err
	}
	if current.HistoryRevision != v.HistoryRevision || current.ContextRevision != v.ContextRevision || current.ThroughSequence != v.ThroughSequence {
		return ErrConflict
	}
	return nil
}

// ContextUsage reads the latest actually dispatched ordinary request through a
// derived owner index. Rowid is its durable insertion order, not wall-clock time.
// A helper, cancelled reservation, or another session cannot replace it.
func (s *Store) ContextUsage(ctx context.Context, owner session.SessionID) (session.ContextUsage, error) {
	if session.ValidateID(string(owner)) != nil {
		return session.ContextUsage{}, session.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return session.ContextUsage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := readContextScope(ctx, tx, owner)
	if err != nil {
		return session.ContextUsage{}, err
	}
	value, err := readContextUsage(ctx, tx, scope)
	if err != nil {
		return session.ContextUsage{}, err
	}
	if err = tx.Commit(); err != nil {
		return session.ContextUsage{}, err
	}
	return value, nil
}

const contextUsageQuery = `SELECT id,turn_id,json_extract(request,'$.model'),json_extract(request,'$.context'),json_extract(result,'$.usage.input')
 FROM model_attempts WHERE json_extract(request,'$.context.session_id')=? AND dispatched_at IS NOT NULL ORDER BY rowid DESC LIMIT 1`

func readContextUsage(ctx context.Context, q querier, scope session.ModelContextScope) (session.ContextUsage, error) {
	value := session.ContextUsage{ModelContextScope: scope, UnavailableReason: "no_evidence"}
	var prefill session.ContextPrefill
	var selection, evidence string
	var reported *int64
	err := q.QueryRowContext(ctx, contextUsageQuery, scope.SessionID).Scan(&prefill.AttemptID, &prefill.TurnID, &selection, &evidence, &reported)
	if errors.Is(err, sql.ErrNoRows) {
		return value, nil
	}
	if err != nil {
		return session.ContextUsage{}, err
	}
	var captured session.ModelContextEvidence
	if err = json.Unmarshal([]byte(evidence), &captured); err != nil {
		return session.ContextUsage{}, err
	}
	if err = captured.Validate(); err != nil {
		return session.ContextUsage{}, err
	}
	if err = json.Unmarshal([]byte(selection), &prefill.Model); err != nil {
		return session.ContextUsage{}, err
	}
	switch {
	case captured.SessionID != scope.SessionID:
		return session.ContextUsage{}, ErrConflict
	case captured.ConfigRevision != scope.ConfigRevision:
		value.UnavailableReason = "configuration_changed"
	case captured.HistoryRevision != scope.HistoryRevision:
		value.UnavailableReason = "history_changed"
	case captured.ContextRevision != scope.ContextRevision:
		value.UnavailableReason = "selection_changed"
	default:
		prefill.ThroughSequence = captured.ThroughSequence
		prefill.InputTokens, prefill.InputSource = captured.EstimatedTokens, "estimated"
		if reported != nil {
			prefill.InputTokens, prefill.InputSource = *reported, "reported"
		}
		prefill.ContextWindowTokens = captured.ContextWindowTokens
		prefill.Stale = captured.ThroughSequence != scope.ThroughSequence
		value.Prefill, value.UnavailableReason = &prefill, ""
	}
	return value, nil
}
