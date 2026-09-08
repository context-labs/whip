package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/llm"
)

// TranscriptBounds identifies the retained raw log for one agent. Sequences
// are stable message identities, independent of the compacted model view.
type TranscriptBounds struct {
	FirstSeq int
	LastSeq  int
	Count    int
}

type TranscriptMessage struct {
	Seq     int
	Message llm.Message
}

type TranscriptPage struct {
	Messages   []TranscriptMessage
	ThroughSeq int
	NextSeq    int
	HasMore    bool
}

// transcriptSource returns SQL chosen only from these two constant sources.
// Root history can exist before its live agent is initialized (e.g. a fork).
func transcriptSource(ctx context.Context, tx *sql.Tx, rootID, agentID string) (string, []any, error) {
	if rootID == "" || agentID == "" {
		return "", nil, ErrAgentAccess
	}
	var exists bool
	if agentID == rootID {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND kind='agent')`, rootID).Scan(&exists); err != nil {
			return "", nil, err
		}
		if !exists {
			return "", nil, ErrAgentAccess
		}
		return "messages WHERE session_id=?", []any{rootID}, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE root_id=? AND id=?)`, rootID, agentID).Scan(&exists); err != nil {
		return "", nil, err
	}
	if !exists {
		return "", nil, ErrAgentAccess
	}
	return "transcript_messages WHERE root_id=? AND agent_id=?", []any{rootID, agentID}, nil
}

func (s *Store) TranscriptBounds(ctx context.Context, rootID, agentID string) (TranscriptBounds, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TranscriptBounds{}, err
	}
	defer func() { _ = tx.Rollback() }()
	source, args, err := transcriptSource(ctx, tx, rootID, agentID)
	if err != nil {
		return TranscriptBounds{}, err
	}
	var bounds TranscriptBounds
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq),0),COALESCE(MAX(seq),0),COUNT(*) FROM `+source, args...).Scan(&bounds.FirstSeq, &bounds.LastSeq, &bounds.Count); err != nil {
		return TranscriptBounds{}, err
	}
	return bounds, tx.Commit()
}

// ReadTranscript reads raw rows after afterSeq through the inclusive upper
// sequence. Pass throughSeq=-1 to capture the current upper sequence, then
// reuse the returned ThroughSeq and NextSeq for subsequent pages. Zero is an
// explicit empty snapshot, so a new turn cannot shift an existing page view.
// Storage and decoding errors are returned, never converted to missing rows.
func (s *Store) ReadTranscript(ctx context.Context, rootID, agentID string, afterSeq, throughSeq, limit int) (TranscriptPage, error) {
	if afterSeq < 0 || throughSeq < -1 || limit < 1 || limit > 128 {
		return TranscriptPage{}, errors.New("transcript read requires nonnegative cursor, through >= -1, and limit 1..128")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TranscriptPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	source, args, err := transcriptSource(ctx, tx, rootID, agentID)
	if err != nil {
		return TranscriptPage{}, err
	}
	if throughSeq == -1 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM `+source, args...).Scan(&throughSeq); err != nil {
			return TranscriptPage{}, err
		}
	}
	queryArgs := append(append([]any(nil), args...), afterSeq, throughSeq, limit)
	//nolint:gosec // transcriptSource returns one of two fixed SQL fragments; all caller values are bound parameters.
	rows, err := tx.QueryContext(ctx, `SELECT seq,content FROM `+source+` AND seq>? AND seq<=? ORDER BY seq LIMIT ?`, queryArgs...)
	if err != nil {
		return TranscriptPage{}, err
	}
	defer func() { _ = rows.Close() }()
	page := TranscriptPage{ThroughSeq: throughSeq, NextSeq: afterSeq}
	for rows.Next() {
		var item TranscriptMessage
		var data string
		if err := rows.Scan(&item.Seq, &data); err != nil {
			return TranscriptPage{}, err
		}
		if err := json.Unmarshal([]byte(data), &item.Message); err != nil {
			return TranscriptPage{}, fmt.Errorf("decode transcript message %s/%d: %w", agentID, item.Seq, err)
		}
		if item.Message.Role == "" {
			return TranscriptPage{}, fmt.Errorf("transcript message %s/%d has no role", agentID, item.Seq)
		}
		item.Message.RawSequence = item.Seq
		page.Messages = append(page.Messages, item)
		page.NextSeq = item.Seq
	}
	if err := rows.Err(); err != nil {
		return TranscriptPage{}, err
	}
	if err := rows.Close(); err != nil {
		return TranscriptPage{}, err
	}
	moreArgs := append(append([]any(nil), args...), page.NextSeq, throughSeq)
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+source+` AND seq>? AND seq<=?)`, moreArgs...).Scan(&page.HasMore); err != nil {
		return TranscriptPage{}, err
	}
	return page, tx.Commit()
}

// RecordRawCompaction records an idle agent's explicit compaction. The exact
// raw sequence is resolved and validated in the same transaction as the
// summary write, using the same mapping as turn-journal compactions.
func (s *Store) RecordRawCompaction(ctx context.Context, rootID, agentID string, rawCutoff int, summary string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := appendCompactionsTx(ctx, tx, rootID, agentID, []RootCompaction{{Summary: summary, RawCutoff: &rawCutoff}}, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// appendCompactionsTx stores a derived view in the same transaction as its
// raw journal. Only the legacy index-based callers need the fallback mapping.
func appendCompactionsTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, compactions []RootCompaction, stamp string) error {
	if len(compactions) == 0 {
		return nil
	}
	source, args, err := transcriptSource(ctx, tx, rootID, agentID)
	if err != nil {
		return err
	}
	var generation, rawCutoff, rawCount int
	if err := tx.QueryRowContext(ctx, `SELECT seq,cutoff FROM compactions WHERE session_id=? AND agent_id=? ORDER BY seq DESC LIMIT 1`, rootID, agentID).Scan(&generation, &rawCutoff); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+source, args...).Scan(&rawCount); err != nil {
		return err
	}
	for _, compaction := range compactions {
		if compaction.Summary == "" {
			return errors.New("turn compaction requires a summary")
		}
		previousCutoff := rawCutoff
		if compaction.RawCutoff != nil {
			if *compaction.RawCutoff < 1 {
				return errors.New("turn compaction requires a positive raw cutoff")
			}
			var lastSeq int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(seq),0) FROM `+source+` AND seq<=?`, append(append([]any(nil), args...), *compaction.RawCutoff)...).Scan(&rawCutoff, &lastSeq); err != nil {
				return err
			}
			if lastSeq != *compaction.RawCutoff {
				return errors.New("turn compaction raw cutoff is not a retained message")
			}
		} else if compaction.Cutoff < 1 {
			return errors.New("turn compaction requires a cutoff")
		} else if generation > 0 {
			tailStart := compaction.RawTailStart
			if tailStart < 1 {
				tailStart = 2
			}
			rawCutoff += compaction.Cutoff - tailStart
		} else {
			rawCutoff = compaction.Cutoff
			var firstRole string
			if err := tx.QueryRowContext(ctx, `SELECT role FROM `+source+` ORDER BY seq LIMIT 1`, args...).Scan(&firstRole); err != nil {
				return err
			}
			if firstRole != "system" {
				rawCutoff--
			}
		}
		if rawCutoff < previousCutoff || rawCutoff < 1 || rawCutoff > rawCount {
			return errors.New("turn compaction cutoff is outside the retained raw prefix")
		}
		generation++
		if _, err := tx.ExecContext(ctx, `INSERT INTO compactions(session_id,agent_id,seq,cutoff,summary,created_at) VALUES(?,?,?,?,?,?)`, rootID, agentID, generation, rawCutoff, compaction.Summary, stamp); err != nil {
			return err
		}
	}
	return nil
}
