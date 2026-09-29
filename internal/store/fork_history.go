package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func preflightFork(ctx context.Context, tx *sql.Tx, request session.ForkRequest) error {
	var messages, groups, bytes, private int64
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(DISTINCT m.group_id),
 COALESCE(SUM(length(CAST(COALESCE(m.parts,i.parts,r.body,'') AS BLOB))+COALESCE(length(CAST(r.subject AS BLOB)),0)+COALESCE(length(CAST(COALESCE(m.design_context,i.design_context) AS BLOB)),0)+COALESCE(length(CAST(m.presentation AS BLOB)),0)),0),
 COALESCE(SUM(length(CAST(m.model_continuation AS BLOB))),0)
 FROM messages m LEFT JOIN inputs i ON i.id=m.input_id
 LEFT JOIN mail_revisions r ON r.mail_id=m.mail_id AND r.revision=m.mail_revision
 WHERE m.session_id=? AND m.retired_revision IS NULL AND m.sequence<=?`, request.SessionID, request.KeepThrough).Scan(&messages, &groups, &bytes, &private)
	if err != nil {
		return err
	}
	if messages > session.MaxForkMessages || groups > session.MaxForkGroups || bytes > session.MaxForkHistoryBytes || private > session.MaxForkContinuationBytes {
		return fmt.Errorf("%w: fork history import exceeds count or byte bounds", ErrLimit)
	}
	var references, contentBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(b.size),0) FROM content_references r
 JOIN content_bodies b ON b.digest=r.digest WHERE r.owner_session_id=?`, request.SessionID).Scan(&references, &contentBytes); err != nil {
		return err
	}
	if references > session.MaxContentReferences || contentBytes > session.MaxSessionContentBytes {
		return fmt.Errorf("%w: fork content import exceeds count or byte bounds", ErrLimit)
	}
	return nil
}

func importForkHistory(ctx context.Context, tx *sql.Tx, request session.ForkRequest, owner session.SessionID) (map[session.MessageID]session.MessageID, error) {
	// Read only bounded identities first. Resolve one immutable message at a time
	// so the import never hydrates the entire history or any content bodies.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM messages WHERE session_id=? AND retired_revision IS NULL AND sequence<=? ORDER BY sequence`, request.SessionID, request.KeepThrough)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []session.MessageID
	for rows.Next() {
		var id session.MessageID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	groups := map[session.HistoryGroupID]session.HistoryGroupID{}
	messages := make(map[session.MessageID]session.MessageID, len(ids))
	bytes := 0
	for _, id := range ids {
		value, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", id))
		if err != nil {
			return nil, err
		}
		parts, err := encode(value.Parts)
		if err != nil {
			return nil, err
		}
		var design *string
		if value.DesignContext != nil {
			design, err = encodeDesignContext(&value.DesignContext.DesignContext)
			if err != nil {
				return nil, err
			}
			bytes += len(*design)
		}
		display, err := encodePresentation(value.Presentation)
		if err != nil {
			return nil, err
		}
		if display != nil {
			bytes += len(*display)
		}
		bytes += len(parts)
		if bytes > session.MaxForkHistoryBytes {
			return nil, fmt.Errorf("%w: encoded fork history exceeds byte bound", ErrLimit)
		}
		if err := validateContentReferences(ctx, tx, request.SessionID, value.Parts); err != nil {
			return nil, err
		}
		var continuation *string
		if err := tx.QueryRowContext(ctx, "SELECT model_continuation FROM messages WHERE id=?", id).Scan(&continuation); err != nil {
			return nil, err
		}
		// Keep canonical visible parts and their validated private envelope together.
		// The model adapter independently verifies route scope and visible matching
		// before replay; imports never rebind a continuation to new credentials.
		if _, err := decodeContinuation(continuation); err != nil {
			return nil, err
		}
		group, ok := groups[value.GroupID]
		if !ok {
			group = session.HistoryGroupID(newID("group"))
			evidence, err := forkAttemptPresentations(ctx, tx, request.SessionID, value.GroupID, group)
			if err != nil {
				return nil, err
			}
			if evidence != nil {
				bytes += len(*evidence)
				if bytes > session.MaxForkHistoryBytes {
					return nil, fmt.Errorf("%w: fork display evidence exceeds byte bound", ErrLimit)
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO history_groups(id,session_id,source_session_id,source_group_id,created_at,attempt_presentations) VALUES(?,?,?,?,?,?)`, group, owner, request.SessionID, value.GroupID, now(), evidence); err != nil {
				return nil, err
			}
			groups[value.GroupID] = group
		}
		copied := session.MessageID(newID("message"))
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages(id,session_id,group_id,sequence,opening_input,source_session_id,source_message_id,source_sequence,role,parts,created_at,model_continuation,design_context,presentation)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, copied, owner, group, value.Sequence, value.OpeningInput, request.SessionID, value.ID, value.Sequence, value.Role, parts, now(), continuation, design, display); err != nil {
			return nil, err
		}
		messages[value.ID] = copied
	}
	return messages, nil
}

func importForkCompactions(ctx context.Context, tx *sql.Tx, request session.ForkRequest, owner session.SessionID, messages map[session.MessageID]session.MessageID) error {
	head, err := readContextHead(ctx, tx, request.SessionID)
	if err != nil || head.CompactionID == nil {
		return err
	}
	var chain []session.Compaction
	ids := map[session.CompactionID]session.CompactionID{}
	for id := head.CompactionID; id != nil; {
		if len(chain) == session.MaxForkCompactions {
			return fmt.Errorf("%w: fork compaction chain exceeds count bound", ErrLimit)
		}
		if _, exists := ids[*id]; exists {
			return fmt.Errorf("%w: cyclic compaction ancestry", session.ErrInvalid)
		}
		value, err := readCompaction(ctx, tx, request.SessionID, *id)
		if err != nil {
			return err
		}
		// A selection outside the chosen prefix has no valid destination projection.
		if value.ThroughSequence > request.KeepThrough {
			return nil
		}
		if err := activeCompaction(ctx, tx, value); errors.Is(err, ErrConflict) {
			return nil
		} else if err != nil {
			return err
		}
		for i, pin := range value.PinnedMessageIDs {
			copied, ok := messages[pin]
			if !ok {
				return nil
			}
			value.PinnedMessageIDs[i] = copied
		}
		ids[value.ID] = session.CompactionID(newID("compaction"))
		chain = append(chain, value)
		id = value.BaseID
	}
	for _, value := range chain {
		var base *session.CompactionID
		if value.BaseID != nil {
			base = new(ids[*value.BaseID])
		}
		pins, err := encode(append([]session.MessageID{}, value.PinnedMessageIDs...))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO compactions(id,session_id,history_revision,source_session_id,source_compaction_id,base_id,expected_revision,through_sequence,pinned_message_ids,text,created_at)
 VALUES(?,?,1,?,?,?,?,?,?,?,?)`, ids[value.ID], owner, request.SessionID, value.ID, base, value.ExpectedRevision, value.ThroughSequence, pins, value.Text, now()); err != nil {
			return err
		}
	}
	_, err = selectCompaction(ctx, tx, session.ContextHead{SessionID: owner}, new(ids[*head.CompactionID]))
	return err
}
