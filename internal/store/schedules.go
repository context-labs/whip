package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/schedule"
	"github.com/context-labs/whip/internal/session"
)

var ErrScheduleBlocked = errors.New("schedule successor is outside the supported date range")

// List projections read bounded metadata, not every stored template.
const scheduleSelect = `SELECT id,session_id,first_due,every_ns,next_due,cancelled_at,failure,created_at,
 length(CAST(parts AS BLOB)),substr(CAST(coalesce(json_extract(parts,'$[0].text'),'') AS BLOB),1,2048),
 length(CAST(coalesce(json_extract(parts,'$[0].text'),'') AS BLOB)) FROM schedules`

func scanSchedule(row scanner) (value session.ScheduleMetadata, err error) {
	var first string
	var next sql.NullString
	var every, cancelled sql.NullInt64
	var created, previewBytes int64
	var preview []byte
	err = row.Scan(&value.ID, &value.SessionID, &first, &every, &next, &cancelled, &value.Failure, &created, &value.PartsBytes, &preview, &previewBytes)
	if err != nil {
		return value, found(err)
	}
	value.FirstDue, err = time.Parse(time.RFC3339Nano, first)
	if err != nil {
		return value, err
	}
	parsed := schedule.Schedule{At: value.FirstDue}
	if every.Valid {
		parsed = schedule.Schedule{Every: time.Duration(every.Int64)}
	}
	value.Expression = parsed.String()
	if next.Valid {
		due, e := time.Parse(time.RFC3339Nano, next.String)
		if e != nil {
			return value, e
		}
		value.NextDue = &due
	}
	value.CreatedAt = timestamp(created)
	value.CancelledAt = optionalTime(cancelled)
	for !utf8.Valid(preview) && len(preview) > 0 {
		preview = preview[:len(preview)-1]
	}
	value.Preview = string(preview)
	value.PreviewTruncated = int64(len(preview)) < previewBytes
	return value, nil
}

//nolint:nilnil // A schedule has no latest input until its first occurrence is admitted.
func scheduleLatest(ctx context.Context, q querier, id session.ScheduleID) (*session.ScheduleInput, error) {
	var value session.ScheduleInput
	var slot string
	err := q.QueryRowContext(ctx, `SELECT i.id,i.scheduled_for,r.client_id,r.request_id FROM inputs i
 JOIN receipts r ON r.input_id=i.id WHERE i.schedule_id=? ORDER BY i.ordinal DESC LIMIT 1`, id).
		Scan(&value.InputID, &slot, &value.ClientID, &value.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value.ScheduleID = id
	value.ScheduledFor, err = time.Parse(time.RFC3339Nano, slot)
	return &value, err
}

func readSchedule(ctx context.Context, q querier, owner session.SessionID, id session.ScheduleID) (session.ScheduleMetadata, error) {
	value, err := scanSchedule(q.QueryRowContext(ctx, scheduleSelect+" WHERE id=? AND session_id=? AND deleted_at IS NULL", id, owner))
	if err == nil {
		value.Latest, err = scheduleLatest(ctx, q, id)
	}
	return value, err
}

func scheduleAdmission(ctx context.Context, q querier, owner session.SessionID, id session.ScheduleID) (session.ScheduleAdmission, error) {
	result := session.ScheduleAdmission{ID: id}
	var deleted sql.NullInt64
	if err := q.QueryRowContext(ctx, "SELECT deleted_at FROM schedules WHERE id=? AND session_id=?", id, owner).Scan(&deleted); err != nil {
		return result, found(err)
	}
	if deleted.Valid {
		result.DeletedAt = optionalTime(deleted)
		return result, nil
	}
	value, err := readSchedule(ctx, q, owner, id)
	result.Schedule = &value
	return result, err
}

func (s *Store) CreateSchedule(ctx context.Context, owner session.SessionID, id session.ScheduleID, spec session.ScheduleSpec) (result session.ScheduleAdmission, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = createSchedule(ctx, tx, owner, id, spec); return err })
	return
}

func createSchedule(ctx context.Context, tx *sql.Tx, owner session.SessionID, id session.ScheduleID, spec session.ScheduleSpec) (session.ScheduleAdmission, error) {
	for _, value := range []string{string(owner), string(id)} {
		if err := session.ValidateID(value); err != nil {
			return session.ScheduleAdmission{}, err
		}
	}
	if err := spec.Validate(); err != nil {
		return session.ScheduleAdmission{}, err
	}
	parsed, err := schedule.Parse(spec.Expression)
	if err != nil {
		return session.ScheduleAdmission{}, err
	}
	spec.Expression = parsed.String()
	digest, err := requestDigest("schedule", struct {
		Owner session.SessionID
		Spec  session.ScheduleSpec
	}{owner, spec})
	if err != nil {
		return session.ScheduleAdmission{}, err
	}
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT initial_digest FROM schedules WHERE id=?", id).Scan(&previous)
	if err == nil {
		if previous != digest {
			return session.ScheduleAdmission{}, ErrConflict
		}
		return scheduleAdmission(ctx, tx, owner, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return session.ScheduleAdmission{}, err
	}
	current, err := readSession(ctx, tx, owner)
	if err != nil {
		return session.ScheduleAdmission{}, err
	}
	if current.Lifecycle != session.Active {
		return session.ScheduleAdmission{}, ErrStopped
	}
	if err := current.Config.Model.Validate(); err != nil {
		return session.ScheduleAdmission{}, fmt.Errorf("%w: scheduled prompts require a configured model", session.ErrInvalid)
	}
	if err := validateContentReferences(ctx, tx, owner, spec.Parts); err != nil {
		return session.ScheduleAdmission{}, err
	}
	parts, err := encode(spec.Parts)
	if err != nil {
		return session.ScheduleAdmission{}, err
	}
	first := parsed.At
	var every any
	if parsed.Every > 0 {
		first = time.Now().UTC()
		every = int64(parsed.Every)
	}
	stamp, err := schedule.Stamp(first)
	if err != nil {
		return session.ScheduleAdmission{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedules (id,session_id,initial_digest,first_due,every_ns,next_due,parts,created_at)
 VALUES (?,?,?,?,?,?,?,?)`, id, owner, digest, stamp, every, stamp, parts, now()); err != nil {
		return session.ScheduleAdmission{}, err
	}
	if err := checkResources(ctx, tx, owner, session.ResourceSchedules); err != nil {
		return session.ScheduleAdmission{}, err
	}
	if err := chargeWrite(ctx, tx, owner, "schedule", string(id), 0, int64(len(spec.Expression))+inputWriteBytes(spec.Parts)); err != nil {
		return session.ScheduleAdmission{}, err
	}
	return scheduleAdmission(ctx, tx, owner, id)
}

func (s *Store) Schedule(ctx context.Context, owner session.SessionID, id session.ScheduleID) (result session.Schedule, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result.ScheduleMetadata, err = readSchedule(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		var raw string
		if err := tx.QueryRowContext(ctx, "SELECT parts FROM schedules WHERE id=?", id).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal([]byte(raw), &result.Parts)
	})
	return
}

func (s *Store) CancelSchedule(ctx context.Context, owner session.SessionID, id session.ScheduleID) (result session.ScheduleAdmission, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = cancelSchedule(ctx, tx, owner, id); return err })
	return
}

func cancelSchedule(ctx context.Context, tx *sql.Tx, owner session.SessionID, id session.ScheduleID) (session.ScheduleAdmission, error) {
	if _, err := tx.ExecContext(ctx, "UPDATE schedules SET cancelled_at=? WHERE id=? AND session_id=? AND cancelled_at IS NULL AND deleted_at IS NULL", now(), id, owner); err != nil {
		return session.ScheduleAdmission{}, err
	}
	return scheduleAdmission(ctx, tx, owner, id)
}

func validateScheduleList(request session.ScheduleList) error {
	if request.Limit < 1 || request.Limit > 100 {
		return session.ErrInvalid
	}
	if request.Upcoming {
		if request.After != "" {
			return session.ErrInvalid
		}
		if request.Cursor != nil {
			if err := session.ValidateID(string(request.Cursor.ID)); err != nil {
				return err
			}
			if _, err := schedule.Stamp(request.Cursor.Due); err != nil {
				return session.ErrInvalid
			}
		}
	} else if request.Cursor != nil {
		return session.ErrInvalid
	}
	if request.After != "" {
		return session.ValidateID(string(request.After))
	}
	return nil
}

func (s *Store) Schedules(ctx context.Context, owner session.SessionID, request session.ScheduleList) (result session.SchedulePage, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = listSchedules(ctx, tx, owner, request); return err })
	return
}

func listSchedules(ctx context.Context, q querier, owner session.SessionID, request session.ScheduleList) (session.SchedulePage, error) {
	result := session.SchedulePage{Items: []session.ScheduleMetadata{}}
	if err := validateScheduleList(request); err != nil {
		return result, err
	}
	if _, err := readSession(ctx, q, owner); err != nil {
		return result, err
	}
	query := scheduleSelect + " WHERE session_id=? AND deleted_at IS NULL"
	args := []any{owner}
	if request.Upcoming {
		query += " AND next_due IS NOT NULL AND cancelled_at IS NULL"
		if request.Cursor != nil {
			stamp, _ := schedule.Stamp(request.Cursor.Due)
			query += " AND (next_due,id)>(?,?)"
			args = append(args, stamp, request.Cursor.ID)
		}
		query += " ORDER BY next_due,id LIMIT ?"
	} else {
		query += " AND id>? ORDER BY id LIMIT ?"
		args = append(args, request.After)
	}
	args = append(args, request.Limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		value, err := scanSchedule(rows)
		if err != nil {
			_ = rows.Close()
			return result, err
		}
		result.Items = append(result.Items, value)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return result, err
	}
	for i := range result.Items {
		result.Items[i].Latest, err = scheduleLatest(ctx, q, result.Items[i].ID)
		if err != nil {
			return result, err
		}
	}
	if len(result.Items) == request.Limit {
		last := result.Items[len(result.Items)-1]
		if request.Upcoming {
			result.NextCursor = &session.ScheduleCursor{Due: *last.NextDue, ID: last.ID}
		} else {
			result.NextAfter = &last.ID
		}
	}
	return result, nil
}

// ScheduleSweep captures a disposable upper bound on already admitted inputs.
// A later input excludes that schedule from this sweep, so cursor movement cannot
// repeatedly prefer a fast overdue recurrence over schedules later in the index.
func (s *Store) ScheduleSweep(ctx context.Context) (int64, error) {
	var through int64
	err := s.db.QueryRowContext(ctx, "SELECT coalesce(max(ordinal),0) FROM inputs").Scan(&through)
	return through, err
}

// DueSchedules is a bounded global scan independent of loaded sessions/workers.
func (s *Store) DueSchedules(ctx context.Context, after *session.ScheduleCursor, through int64, limit int) ([]session.ScheduleMetadata, error) {
	if limit < 1 || limit > 100 || through < 0 {
		return nil, session.ErrInvalid
	}
	stamp, err := schedule.Stamp(time.Now())
	if err != nil {
		return nil, err
	}
	query := scheduleSelect + ` WHERE next_due<=? AND next_due IS NOT NULL AND cancelled_at IS NULL
 AND deleted_at IS NULL AND failure IS NULL AND EXISTS(SELECT 1 FROM sessions WHERE sessions.id=schedules.session_id AND lifecycle='active')
 AND NOT EXISTS(SELECT 1 FROM inputs WHERE schedule_id=schedules.id AND ordinal>?)`
	args := []any{stamp, through}
	if after != nil {
		cursor, e := schedule.Stamp(after.Due)
		if e != nil {
			return nil, e
		}
		query += " AND (next_due,id)>(?,?)"
		args = append(args, cursor, after.ID)
	}
	query += " ORDER BY next_due,id LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]session.ScheduleMetadata, 0, limit)
	for rows.Next() {
		value, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// FireSchedule atomically consumes one exact slot with its ordinary input and
// charge. A failed successor is persisted without consuming or skipping a slot.
func (s *Store) FireSchedule(ctx context.Context, id session.ScheduleID, due time.Time) (result Admission, err error) {
	return s.fireSchedule(ctx, id, due, time.Now())
}

func (s *Store) fireSchedule(ctx context.Context, id session.ScheduleID, due, instant time.Time) (result Admission, err error) {
	slot, err := schedule.Stamp(due)
	if err != nil {
		return result, session.ErrInvalid
	}
	blocked := false
	err = s.write(ctx, func(tx *sql.Tx) error {
		var owner session.SessionID
		if err := tx.QueryRowContext(ctx, "SELECT session_id FROM schedules WHERE id=?", id).Scan(&owner); err != nil {
			return found(err)
		}
		identity := session.RequestIdentity{ClientID: "schedule", RequestID: fmt.Sprintf("slot_%x", sha256.Sum256([]byte(string(id)+"\x00"+slot)))}
		occurrence := session.ScheduleOccurrence{ScheduleID: id, ScheduledFor: due.UTC()}
		digest, err := requestDigest("schedule_fire", occurrence)
		if err != nil {
			return err
		}
		receipt, err := readReceipt(ctx, tx, identity)
		if err == nil {
			if receipt.Digest != digest {
				return ErrConflict
			}
			result, err = readAdmission(ctx, tx, identity)
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		value, err := readSchedule(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		if value.CancelledAt != nil || value.NextDue == nil || !value.NextDue.Equal(due) {
			return ErrConflict
		}
		if value.Failure != nil {
			blocked = true
			return nil
		}
		if due.After(instant) {
			return ErrNoWork
		}
		current, err := readSession(ctx, tx, owner)
		if err != nil {
			return err
		}
		if current.Lifecycle != session.Active {
			return ErrStopped
		}
		if value.Latest != nil {
			var outstanding bool
			if err := tx.QueryRowContext(ctx, `SELECT (turn_id IS NULL AND cancelled_at IS NULL) OR
 EXISTS(SELECT 1 FROM turns WHERE turns.id=inputs.turn_id AND state IN ('running','cancelling')) FROM inputs WHERE id=?`, value.Latest.InputID).Scan(&outstanding); err != nil {
				return err
			}
			if outstanding {
				return ErrBusy
			}
		}
		parsed, err := schedule.Parse(value.Expression)
		if err != nil {
			return err
		}
		var next any
		if parsed.Every > 0 {
			successor, e := schedule.Successor(due, parsed.Every)
			if e != nil {
				if _, err := tx.ExecContext(ctx, "UPDATE schedules SET failure='successor_out_of_range' WHERE id=?", id); err != nil {
					return err
				}
				blocked = true
				return nil
			}
			next, err = schedule.Stamp(successor)
			if err != nil {
				return err
			}
		}
		var raw string
		if err := tx.QueryRowContext(ctx, "SELECT parts FROM schedules WHERE id=?", id).Scan(&raw); err != nil {
			return err
		}
		var parts []session.Part
		if err := json.Unmarshal([]byte(raw), &parts); err != nil {
			return err
		}
		result, err = admitInput(ctx, tx, identity, digest, Submission{SessionID: owner, Source: session.ScheduledInput, Parts: parts, Schedule: &occurrence})
		if err != nil {
			return err
		}
		if err := chargeWrite(ctx, tx, owner, "input", string(result.Input.ID), 0, inputWriteBytes(parts)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE schedules SET next_due=? WHERE id=?", next, id)
		return err
	})
	if err == nil && blocked {
		err = ErrScheduleBlocked
	}
	return
}

func (s *Store) ApplyScheduleOperation(ctx context.Context, id session.OperationID) (result json.RawMessage, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		switch operation.Capability {
		case "schedules.create", "schedules.list", "schedules.cancel":
		default:
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, operation.SessionID)
		if err != nil {
			return err
		}
		if operation.Resource != string(owner.TreeID) {
			return ErrConflict
		}
		if operation.State == session.OperationSucceeded && operation.Result != nil {
			result = operation.Result.Value
			return nil
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		var value any
		switch operation.Capability {
		case "schedules.create":
			var request session.ScheduleSpec
			if err := json.Unmarshal(operation.Arguments, &request); err != nil {
				return err
			}
			value, err = createSchedule(ctx, tx, owner.ID, session.ScheduleID(fmt.Sprintf("schedule_%x", sha256.Sum256([]byte(id)))), request)
		case "schedules.list":
			var request session.ScheduleList
			if err := json.Unmarshal(operation.Arguments, &request); err != nil {
				return err
			}
			value, err = listSchedules(ctx, tx, owner.ID, request)
		case "schedules.cancel":
			var request session.ScheduleIDRequest
			if err := json.Unmarshal(operation.Arguments, &request); err != nil {
				return err
			}
			value, err = cancelSchedule(ctx, tx, owner.ID, request.ID)
		}
		if err != nil {
			return err
		}
		result, err = json.Marshal(value)
		if err != nil {
			return err
		}
		operation.State = session.OperationDispatched
		_, err = settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationSucceeded, Value: result})
		return err
	})
	return
}
