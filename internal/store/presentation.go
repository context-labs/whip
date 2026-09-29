package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

func encodePresentation(p *session.MessagePresentation) (*string, error) {
	if p == nil {
		return nil, nil
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	raw, err := encode(p)
	return &raw, err
}
func decodePresentation(raw *string) (*session.MessagePresentation, error) {
	if raw == nil {
		return nil, nil
	}
	var p session.MessagePresentation
	if err := json.Unmarshal([]byte(*raw), &p); err != nil {
		return nil, err
	}
	return &p, p.Validate()
}

// AttemptPresentations observes bounded failed display evidence for the same
// retained history window. Include the group touching `after` so a newly failed
// attempt remains observable after its opening input has already been read.
// Older omitted attempts remain exactly pageable through ModelAttempts.
func (s *Store) AttemptPresentations(ctx context.Context, owner session.SessionID, after, through int64, revision session.Revision) ([]session.AttemptPresentation, bool, error) {
	rows, err := s.db.QueryContext(ctx, `WITH window AS (
 SELECT g.id,g.session_id,g.attempt_presentations,MAX(m.sequence) AS sequence FROM history_groups g
 JOIN messages m ON m.group_id=g.id AND m.retired_revision IS NULL
 WHERE g.session_id=? AND m.sequence<=? AND m.sequence>=? GROUP BY g.id
), evidence AS (
 SELECT w.id AS group_id,w.sequence,a.id,a.turn_id,a.logical_id||'_answer' AS message_id,a.state,
 json_extract(a.result,'$.presentation') AS presentation,NULL AS source_session_id,a.created_at AS created,a.number AS number
 FROM window w JOIN model_attempts a ON a.turn_id=w.id
 WHERE a.state IN ('failed','cancelled','uncertain') AND json_type(a.result,'$.presentation')='object'
 UNION ALL
 SELECT w.id,w.sequence,json_extract(j.value,'$.attempt_id'),json_extract(j.value,'$.turn_id'),COALESCE(json_extract(j.value,'$.message_id'),''),
 json_extract(j.value,'$.state'),json_extract(j.value,'$.presentation'),json_extract(j.value,'$.source_session_id'),0,CAST(j.key AS INTEGER)
 FROM window w,json_each(w.attempt_presentations) j
)
 SELECT s.history_revision,e.id,e.turn_id,e.message_id,e.state,e.presentation,e.group_id,e.source_session_id
 FROM sessions s LEFT JOIN evidence e ON 1=1 WHERE s.id=?
 ORDER BY e.sequence DESC,e.created DESC,e.number DESC,e.id DESC LIMIT 65`, owner, through, after, owner)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.AttemptPresentation{}
	size := 0
	exists := false
	truncated := false
	for rows.Next() {
		var actual session.Revision
		var id, turn, message, state, raw, group, source *string
		if err := rows.Scan(&actual, &id, &turn, &message, &state, &raw, &group, &source); err != nil {
			return nil, false, err
		}
		exists = true
		if actual != revision {
			return nil, false, ErrConflict
		}
		if id == nil {
			continue
		}
		if len(result) == 64 {
			truncated = true
			break
		}
		p, err := decodePresentation(raw)
		if err != nil {
			return nil, false, err
		}
		var sourceID *session.SessionID
		if source != nil {
			sourceID = new(session.SessionID(*source))
		}
		value := session.AttemptPresentation{GroupID: session.HistoryGroupID(*group), SourceSessionID: sourceID, AttemptID: session.ModelAttemptID(*id), TurnID: session.TurnID(*turn), MessageID: provisionalMessageID(*message), State: session.ModelAttemptState(*state), Presentation: p}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		size += len(encoded)
		// Leave room for history (4 MiB), live preview and the response envelope.
		if size > 2<<20 {
			truncated = true
			break
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, ErrNotFound
	}
	slices.Reverse(result)
	return result, truncated, nil
}

// Imported evidence is immutable and retained even if its source is deleted.
func forkAttemptPresentations(ctx context.Context, tx *sql.Tx, owner session.SessionID, source, destination session.HistoryGroupID) (*string, error) {
	var imported *string
	if err := tx.QueryRowContext(ctx, "SELECT attempt_presentations FROM history_groups WHERE id=? AND session_id=?", source, owner).Scan(&imported); err != nil {
		return nil, err
	}
	evidence := []session.AttemptPresentation{}
	if imported != nil {
		if err := json.Unmarshal([]byte(*imported), &evidence); err != nil {
			return nil, err
		}
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT id,turn_id,logical_id,state,json_extract(result,'$.presentation') FROM model_attempts
 WHERE turn_id=? AND state IN ('failed','cancelled','uncertain') AND json_type(result,'$.presentation')='object'
 ORDER BY created_at,number,id LIMIT 65`, source)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var value session.AttemptPresentation
			var logical string
			var raw *string
			if err := rows.Scan(&value.AttemptID, &value.TurnID, &logical, &value.State, &raw); err != nil {
				return nil, err
			}
			value.MessageID = provisionalMessageID(logical + "_answer")
			value.SourceSessionID = new(owner)
			value.Presentation, err = decodePresentation(raw)
			if err != nil {
				return nil, err
			}
			evidence = append(evidence, value)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(evidence) == 0 {
		return nil, nil
	}
	if len(evidence) > 64 {
		return nil, fmt.Errorf("%w: fork attempt presentation count", ErrLimit)
	}
	for i := range evidence {
		evidence[i].GroupID = destination
		if err := evidence[i].Presentation.Validate(); err != nil {
			return nil, err
		}
	}
	raw, err := encode(evidence)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxPageBytes {
		return nil, fmt.Errorf("%w: fork attempt presentation bytes", ErrLimit)
	}
	return &raw, nil
}

func provisionalMessageID(value string) session.MessageID {
	if session.ValidateID(value) != nil {
		return ""
	}
	return session.MessageID(value)
}
