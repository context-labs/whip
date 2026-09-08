package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

var ErrHistoryRevision = errors.New("history revision changed; resynchronize the transcript")

// TranscriptReadOptions bounds a stable raw-history view. Revision nil captures
// the current revision; subsequent pages must send the returned revision.
// Recent reads backwards before BeforeSeq (zero means the current end), but
// returns messages in chronological order. Other reads advance AfterSeq.
type TranscriptReadOptions struct {
	AfterSeq   int
	BeforeSeq  int
	ThroughSeq int
	Revision   *int64
	Limit      int
	MaxBytes   int
	Recent     bool
}

type TranscriptPageEntry struct {
	Role     string        `json:"role,omitempty"`
	Authored bool          `json:"authored,omitempty"`
	SentAt   *time.Time    `json:"sent_at,omitempty"`
	Seq      int           `json:"seq"`
	Message  *llm.Message  `json:"message,omitempty"`
	Body     *RuntimeValue `json:"body,omitempty"`
}

type BoundedTranscriptPage struct {
	HistoryRevision int64                 `json:"history_revision,string"`
	ThroughSeq      int                   `json:"through_seq"`
	NextSeq         int                   `json:"next_seq"`
	HasMore         bool                  `json:"has_more"`
	Messages        []TranscriptPageEntry `json:"messages"`
}

// ReadTranscriptPage never mixes a rewind with an older page view. Large
// message bodies receive existing content references with agent-scoped grants.
// MaxBytes bounds the complete JSON-encoded page, not just message text.
func (s *Store) ReadTranscriptPage(ctx context.Context, rootID, agentID string, opts TranscriptReadOptions) (BoundedTranscriptPage, error) {
	if opts.AfterSeq < 0 || opts.BeforeSeq < 0 || opts.ThroughSeq < -1 || opts.Limit < 1 || opts.Limit > 128 || opts.MaxBytes < 1024 || opts.MaxBytes > 512*1024 {
		return BoundedTranscriptPage{}, errors.New("transcript page requires valid cursors, limit 1..128 and max_bytes 1024..524288")
	}
	if (opts.AfterSeq > 0 || opts.BeforeSeq > 0) && opts.Revision == nil {
		return BoundedTranscriptPage{}, errors.New("continuing transcript pages requires history revision")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoundedTranscriptPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	source, args, err := transcriptSource(ctx, tx, rootID, agentID)
	if err != nil {
		return BoundedTranscriptPage{}, err
	}
	page := BoundedTranscriptPage{ThroughSeq: opts.ThroughSeq, NextSeq: opts.AfterSeq, Messages: []TranscriptPageEntry{}}
	if err := tx.QueryRowContext(ctx, `SELECT history_revision FROM sessions WHERE id=?`, rootID).Scan(&page.HistoryRevision); err != nil {
		return page, err
	}
	if opts.Revision != nil && *opts.Revision != page.HistoryRevision {
		return BoundedTranscriptPage{}, ErrHistoryRevision
	}
	if page.ThroughSeq == -1 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM `+source, args...).Scan(&page.ThroughSeq); err != nil {
			return page, err
		}
	}
	direction := ` AND seq>? AND seq<=? ORDER BY seq LIMIT ?`
	queryArgs := append(slices.Clone(args), opts.AfterSeq, page.ThroughSeq, opts.Limit+1)
	if opts.Recent {
		before := opts.BeforeSeq
		if before == 0 {
			before = page.ThroughSeq + 1
		}
		page.NextSeq = before
		direction = ` AND seq<? AND seq<=? ORDER BY seq DESC LIMIT ?`
		queryArgs = append(slices.Clone(args), before, page.ThroughSeq, opts.Limit+1)
	}
	// Fetch metadata first, then one candidate body at a time. Oversized bodies
	// are fetched only when creating their authorized content handle.
	//nolint:gosec // Source and direction are fixed SQL fragments; all caller values are bound parameters.
	rows, err := tx.QueryContext(ctx, `SELECT seq,length(CAST(content AS BLOB)) FROM `+source+direction, queryArgs...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	type rawEntry struct {
		seq, size int
		data      []byte
	}
	var raw []rawEntry
	for rows.Next() {
		var entry rawEntry
		if err := rows.Scan(&entry.seq, &entry.size); err != nil {
			return page, err
		}
		raw = append(raw, entry)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}
	for i, entry := range raw {
		if i == opts.Limit {
			page.HasMore = true
			break
		}
		item := TranscriptPageEntry{Seq: entry.seq}
		if entry.size <= opts.MaxBytes {
			if err := tx.QueryRowContext(ctx, `SELECT content FROM `+source+` AND seq=?`, append(slices.Clone(args), entry.seq)...).Scan(&entry.data); err != nil {
				return page, err
			}
			var message llm.Message
			if err := json.Unmarshal(entry.data, &message); err != nil {
				return page, fmt.Errorf("decode transcript message %s/%d: %w", agentID, entry.seq, err)
			}
			if message.Role == "" {
				return page, fmt.Errorf("transcript message %s/%d has no role", agentID, entry.seq)
			}
			message.RawSequence = entry.seq
			item.Message = &message
		}
		page.Messages = append(page.Messages, item)
		encoded, err := json.Marshal(page)
		if err != nil {
			return page, err
		}
		if item.Message == nil || len(encoded)+32 > opts.MaxBytes {
			page.Messages = page.Messages[:len(page.Messages)-1]
			if len(page.Messages) > 0 {
				page.HasMore = true
				break
			}
			if entry.size > opts.MaxBytes {
				if err := tx.QueryRowContext(ctx, `SELECT content FROM `+source+` AND seq=?`, append(slices.Clone(args), entry.seq)...).Scan(&entry.data); err != nil {
					return page, err
				}
			}
			var metadata struct {
				Role     string     `json:"role"`
				Authored bool       `json:"authored"`
				SentAt   *time.Time `json:"sent_at"`
			}
			if err := json.Unmarshal(entry.data, &metadata); err != nil {
				return page, err
			}
			item.Role, item.Authored, item.SentAt = metadata.Role, metadata.Authored, metadata.SentAt
			grant := ContentGrant{RootID: rootID, AgentID: agentID, Scope: ContentGrantAgent}
			if rootID == agentID {
				grant.AgentID, grant.Scope = "", ContentGrantRoot
			}
			value, err := s.prepareContentReference(RuntimePayload{Data: entry.data, MediaType: "application/json", Source: "transcript"}, grant)
			if err != nil {
				return page, err
			}
			var existing string
			err = tx.QueryRowContext(ctx, `SELECT r.id FROM content_references r JOIN content_grants g ON g.reference_id=r.id WHERE r.digest=? AND r.source='transcript' AND g.root_id=? AND g.agent_id=? AND g.scope=? AND g.revoked_at='' LIMIT 1`, value.Digest, grant.RootID, grant.AgentID, grant.Scope).Scan(&existing)
			if err == nil {
				value.ReferenceID = existing
			} else if errors.Is(err, sql.ErrNoRows) {
				if err := insertRuntimeValue(ctx, tx, value, now()); err != nil {
					return page, err
				}
			} else {
				return page, err
			}
			body := value.RuntimeValue
			item.Message, item.Body = nil, &body
			page.Messages = append(page.Messages, item)
		}
		page.NextSeq = entry.seq
	}
	if opts.Recent {
		slices.Reverse(page.Messages)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		return page, err
	}
	if len(encoded) > opts.MaxBytes {
		return page, errors.New("transcript metadata exceeds page budget")
	}
	return page, tx.Commit()
}
