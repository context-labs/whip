package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/context-labs/whip/internal/schedule"
	"github.com/context-labs/whip/internal/session"
)

type Submission struct {
	Delivery      session.InputDelivery
	TargetTurnID  *session.TurnID
	DesignContext *session.DesignContext
	HostOperation *session.HostOperation
	Goal          *session.GoalRef
	SessionID     session.SessionID
	Source        session.InputSource
	Kind          session.InputKind
	Parts         []session.Part
	Schedule      *session.ScheduleOccurrence
}

// Admission is a projection, not an independently persisted status.
type Admission struct {
	Receipt session.Receipt
	Input   *session.Input
	Turn    *session.Turn
}

func readReceipt(ctx context.Context, q querier, identity session.RequestIdentity) (result session.Receipt, err error) {
	var created int64
	var deleted sql.NullInt64
	result.RequestIdentity = identity
	err = q.QueryRowContext(ctx, "SELECT digest,input_id,deleted_at,created_at FROM receipts WHERE client_id=? AND request_id=?", identity.ClientID, identity.RequestID).
		Scan(&result.Digest, &result.InputID, &deleted, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	result.DeletedAt = optionalTime(deleted)
	return
}

func readInput(ctx context.Context, q querier, id session.InputID) (result session.Input, err error) {
	var raw string
	var design *string
	var steeringID *session.InputSteeringID
	var targetTurn *session.TurnID
	var consumed bool
	var created int64
	var cancelled sql.NullInt64
	var scheduleID, slot, goalID sql.NullString
	var goalRevision sql.NullInt64
	err = q.QueryRowContext(ctx, `SELECT id,session_id,source,kind,parts,COALESCE(turn_id,steered_turn_id),cancelled_at,created_at,schedule_id,scheduled_for,goal_id,goal_revision,design_context,
 (SELECT id FROM input_steering s WHERE s.input_id=inputs.id),(SELECT turn_id FROM input_steering s WHERE s.input_id=inputs.id),steered_turn_id IS NOT NULL FROM inputs WHERE id=?`, id).
		Scan(&result.ID, &result.SessionID, &result.Source, &result.Kind, &raw, &result.TurnID, &cancelled, &created, &scheduleID, &slot, &goalID, &goalRevision, &design, &steeringID, &targetTurn, &consumed)
	if err != nil {
		return result, found(err)
	}
	if steeringID != nil && targetTurn != nil {
		result.Steering = &session.InputSteeringRef{ID: *steeringID, TurnID: *targetTurn, Consumed: consumed}
	}
	if goalID.Valid {
		result.Goal = &session.GoalRef{ID: session.GoalID(goalID.String), Revision: goalRevision.Int64}
	}
	if scheduleID.Valid {
		due, parseErr := time.Parse(time.RFC3339Nano, slot.String)
		if parseErr != nil {
			return result, parseErr
		}
		result.Schedule = &session.ScheduleOccurrence{ScheduleID: session.ScheduleID(scheduleID.String), ScheduledFor: due}
	}
	result.CreatedAt = timestamp(created)
	result.State = session.Queued
	if result.TurnID != nil {
		result.State = session.Claimed
	} else if cancelled.Valid {
		result.State = session.InputCancelled
	}
	err = json.Unmarshal([]byte(raw), &result.Parts)
	if err == nil && design != nil {
		err = json.Unmarshal([]byte(*design), &result.DesignContext)
	}
	if err == nil && result.Kind == session.HostOperationInputKind {
		value, readErr := readHostOperation(ctx, q, result.ID)
		result.HostOperation, err = &value, readErr
	}
	return
}

const turnColumns = `id,session_id,config_revision,history_revision,state,failure,started_at,finished_at,
 COALESCE((SELECT kind FROM inputs WHERE turn_id=turns.id),'prompt'),goal_id,goal_revision`

func readTurn(ctx context.Context, q querier, id session.TurnID) (session.Turn, error) {
	return scanTurn(q.QueryRowContext(ctx, "SELECT "+turnColumns+" FROM turns WHERE id=?", id))
}

func scanTurn(row scanner) (result session.Turn, err error) {
	var started int64
	var goalID *session.GoalID
	var goalRevision sql.NullInt64
	var finished sql.NullInt64
	err = row.Scan(&result.ID, &result.SessionID, &result.ConfigRevision, &result.HistoryRevision,
		&result.State, &result.Failure, &started, &finished, &result.Kind, &goalID, &goalRevision)
	if err != nil {
		return result, found(err)
	}
	if goalID != nil {
		result.Goal = &session.GoalRef{ID: *goalID, Revision: goalRevision.Int64}
	}
	result.StartedAt = timestamp(started)
	result.FinishedAt = optionalTime(finished)
	return
}

func readAdmission(ctx context.Context, q querier, identity session.RequestIdentity) (result Admission, err error) {
	result.Receipt, err = readReceipt(ctx, q, identity)
	if err != nil || result.Receipt.InputID == nil {
		return
	}
	input, err := readInput(ctx, q, *result.Receipt.InputID)
	if err != nil {
		return result, err
	}
	result.Input = &input
	if input.TurnID != nil {
		turn, err := readTurn(ctx, q, *input.TurnID)
		if err != nil {
			return result, err
		}
		result.Turn = &turn
	}
	return result, nil
}

// Public admissions cannot occupy identities owned by internal durable work.
// Recovery/inspection intentionally accepts these returned receipt identities.
func validatePublicIdentity(identity session.RequestIdentity) error {
	if identity.ClientID == childTransferClient || identity.ClientID == "schedule" || identity.ClientID == "operation" || identity.ClientID == "goal" || identity.ClientID == titleClient {
		return fmt.Errorf("%w: client identity is reserved for internal admissions", session.ErrInvalid)
	}
	return nil
}

func (s *Store) Admit(ctx context.Context, identity session.RequestIdentity, request Submission) (result Admission, err error) {
	if err := validatePublicIdentity(identity); err != nil {
		return result, err
	}
	for _, id := range []string{identity.ClientID, identity.RequestID, string(request.SessionID)} {
		if err := session.ValidateID(id); err != nil {
			return result, err
		}
	}
	request, err = normalizeSubmission(request)
	if err != nil {
		return result, err
	}
	digest, err := requestDigest("submit", request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
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
		result, err = admitInput(ctx, tx, identity, digest, request)
		return err
	})
	return
}

func normalizeSubmission(request Submission) (Submission, error) {
	if err := validateInputDelivery(request.Delivery, request.TargetTurnID); err != nil {
		return request, err
	}
	if request.Delivery == "" {
		request.Delivery = session.DeliveryQueued
	}
	if (request.Source != session.UserInput && request.Source != session.AgentInput) || request.Schedule != nil || request.Goal != nil || request.HostOperation != nil {
		return request, fmt.Errorf("%w: invalid input source", session.ErrInvalid)
	}
	if request.Kind == "" {
		request.Kind = session.PromptInput
	}
	if request.DesignContext != nil {
		if request.Kind != session.PromptInput || request.Source != session.UserInput {
			return request, session.ErrInvalid
		}
		if _, err := request.DesignContext.Presentation(request.Parts); err != nil {
			return request, err
		}
		request.DesignContext = request.DesignContext.Clone()
	}
	switch request.Kind {
	case session.PromptInput:
		if err := session.ValidateInputParts(request.Parts); err != nil {
			return request, err
		}
	case session.CompactInput:
		if request.Delivery != session.DeliveryQueued || request.TargetTurnID != nil {
			return request, session.ErrInvalid
		}
		if len(request.Parts) != 0 {
			return request, fmt.Errorf("%w: compact input cannot contain prompt parts", session.ErrInvalid)
		}
		request.Parts = []session.Part{}
	default:
		return request, fmt.Errorf("%w: unknown input kind", session.ErrInvalid)
	}
	return request, nil
}

func requestDigest(kind string, request any) (string, error) {
	raw, err := json.Marshal(struct {
		Kind    string
		Request any
	}{kind, request})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func admitInput(ctx context.Context, tx *sql.Tx, identity session.RequestIdentity, digest string, request Submission) (result Admission, err error) {
	if err := checkChildTransferReservation(ctx, tx, identity, digest, request); err != nil {
		return result, err
	}
	if request.Kind == "" {
		request.Kind = session.PromptInput
	}
	current, err := readSession(ctx, tx, request.SessionID)
	if err != nil {
		return result, err
	}
	if current.Lifecycle != session.Active {
		return result, ErrStopped
	}
	if request.Kind != session.PromptInput && request.Kind != session.HostOperationInputKind {
		if err := requireNoDirectWork(ctx, tx, current.ID); err != nil {
			return result, err
		}
	}
	if request.Kind == session.PromptInput || request.Kind == session.CompactInput {
		if err := current.Config.Model.Validate(); err != nil {
			return result, fmt.Errorf("%w: this session has no configured model", session.ErrInvalid)
		}
	}
	if err := validateContentReferences(ctx, tx, current.ID, request.Parts); err != nil {
		return result, err
	}
	if err := validateDesignContext(ctx, tx, request); err != nil {
		return result, err
	}
	design, err := encodeDesignContext(request.DesignContext)
	if err != nil {
		return result, err
	}
	parts, err := encode(request.Parts)
	if err != nil {
		return result, err
	}
	var scheduleID, slot any
	if request.Source == session.ScheduledInput {
		if request.Schedule == nil || request.Kind != session.PromptInput {
			return result, session.ErrInvalid
		}
		stamp, err := schedule.Stamp(request.Schedule.ScheduledFor)
		if err != nil {
			return result, err
		}
		scheduleID = request.Schedule.ScheduleID
		slot = stamp
	} else if request.Schedule != nil {
		return result, session.ErrInvalid
	}
	var goalID, goalRevision any
	if request.Source == session.GoalInput {
		if request.Goal == nil || request.Kind != session.PromptInput {
			return result, session.ErrInvalid
		}
		if err := request.Goal.Validate(); err != nil {
			return result, err
		}
		goalID, goalRevision = request.Goal.ID, request.Goal.Revision
	} else if request.Goal != nil {
		return result, session.ErrInvalid
	}
	target, err := steeringTarget(ctx, tx, request)
	if err != nil {
		return result, err
	}
	inputID := session.InputID(newID("input"))
	created := now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO inputs (id,session_id,source,kind,parts,created_at,schedule_id,scheduled_for,goal_id,goal_revision,design_context) VALUES (?,?,?,?,?,?,?,?,?,?,?)", inputID, current.ID, request.Source, request.Kind, parts, created, scheduleID, slot, goalID, goalRevision, design); err != nil {
		return result, err
	}
	if target != nil {
		steering := session.SteerInputRequest{ID: session.InputSteeringID("steer_" + string(inputID)), SessionID: current.ID, InputID: inputID, TurnID: *target}
		steeringDigest, err := requestDigest("input_steer", steering)
		if err != nil {
			return result, err
		}
		if err := insertInputSteering(ctx, tx, steering, steeringDigest); err != nil {
			return result, err
		}
	}
	if request.Kind == session.HostOperationInputKind {
		if request.HostOperation == nil {
			return result, session.ErrInvalid
		}
		host := request.HostOperation
		if _, err := tx.ExecContext(ctx, "INSERT INTO host_operation_inputs (input_id,module,name,arguments) VALUES (?,?,?,?)", inputID, host.Module, host.Name, string(host.Arguments)); err != nil {
			return result, err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO receipts VALUES (?,?,?,?,NULL,?)", identity.ClientID, identity.RequestID, digest, inputID, created); err != nil {
		return result, err
	}
	if err := checkResources(ctx, tx, current.ID, session.ResourceQueuedInputs); err != nil {
		return result, err
	}
	if request.Kind == session.PromptInput {
		reason := "ineligible"
		if request.Source == session.UserInput {
			reason = "authored"
		}
		if err := initializeTitle(ctx, tx, current, &session.Input{ID: inputID, Parts: request.Parts}, reason); err != nil {
			return result, err
		}
	}
	result, err = readAdmission(ctx, tx, identity)
	return result, err
}

func (s *Store) Admission(ctx context.Context, identity session.RequestIdentity) (result Admission, err error) {
	// A transaction gives the multi-row projection one consistent snapshot.
	err = s.write(ctx, func(tx *sql.Tx) error { var err error; result, err = readAdmission(ctx, tx, identity); return err })
	return
}

func (s *Store) Input(ctx context.Context, id session.InputID) (session.Input, error) {
	return readInput(ctx, s.db, id)
}

func (s *Store) Turn(ctx context.Context, id session.TurnID) (session.Turn, error) {
	return readTurn(ctx, s.db, id)
}

type Claim struct {
	Turn          session.Turn
	Input         *session.Input
	Configuration session.Configuration
}

func (s *Store) Claim(ctx context.Context, id session.SessionID) (result Claim, err error) {
	skipped := false
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readSession(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Lifecycle != session.Active {
			return ErrStopped
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM turns WHERE session_id=? AND state IN ('running','cancelling')) +
 (SELECT count(*) FROM workspace_actions WHERE session_id=? AND state='claimed')`, id, id).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return ErrBusy
		}
		var inputID session.InputID
		err = tx.QueryRowContext(ctx, "SELECT id FROM inputs WHERE session_id=? AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL ORDER BY ordinal LIMIT 1", id).Scan(&inputID)
		if errors.Is(err, sql.ErrNoRows) {
			ready, err := mailReady(ctx, tx, id)
			if err != nil {
				return err
			}
			if !ready {
				title, err := admitTitle(ctx, tx, current)
				if err != nil {
					return err
				}
				if title == nil {
					return ErrNoWork
				}
				inputID = title.ID
			}
		} else if err != nil {
			return err
		}
		var input *session.Input
		if inputID != "" {
			value, e := readInput(ctx, tx, inputID)
			if e != nil {
				return e
			}
			input = &value
		}
		if input == nil {
			if err := initializeTitle(ctx, tx, current, nil, "ineligible"); err != nil {
				return err
			}
		} else if input.Kind == session.AutomaticTitleInputKind {
			decision, err := readTitleDecision(ctx, tx, current.TreeID)
			if err != nil {
				return err
			}
			current.ConfigRevision = decision.ConfigRevision
			var raw string
			if err := tx.QueryRowContext(ctx, "SELECT configuration FROM session_configurations WHERE session_id=? AND revision=?", current.ID, current.ConfigRevision).Scan(&raw); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(raw), &current.Config); err != nil {
				return err
			}
		}
		goal, skip, err := claimGoal(ctx, tx, current, input)
		if err != nil {
			return err
		}
		if skip {
			skipped = true
			return nil
		}
		var goalID, goalRevision any
		if goal != nil {
			goalID, goalRevision = goal.ID, goal.Revision
		}
		turnID := session.TurnID(newID("turn"))
		started := now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO turns (id,session_id,config_revision,history_revision,state,started_at,goal_id,goal_revision) VALUES (?,?,?,?,'running',?,?,?)", turnID, id, current.ConfigRevision, current.HistoryRevision, started, goalID, goalRevision); err != nil {
			return err
		}
		result.Turn, err = readTurn(ctx, tx, turnID)
		if err != nil {
			return err
		}
		if err := acquireTurnPermit(ctx, tx, result.Turn); err != nil {
			return err
		}
		if input == nil || input.Kind == session.PromptInput {
			if _, err := tx.ExecContext(ctx, "INSERT INTO history_groups (id,session_id,turn_id,created_at) VALUES (?,?,?,?)", turnID, id, turnID, started); err != nil {
				return err
			}
		}
		if inputID != "" {
			update, err := tx.ExecContext(ctx, "UPDATE inputs SET turn_id=? WHERE id=? AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL", turnID, inputID)
			if err != nil {
				return err
			}
			affected, err := update.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return ErrConflict
			}
			input, err := readInput(ctx, tx, inputID)
			if err != nil {
				return err
			}
			result.Input = &input
			result.Turn.Kind = input.Kind
			if input.Kind == session.PromptInput {
				if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id,session_id,turn_id,group_id,opening_input,sequence,role,input_id,created_at)
   SELECT ?,?,?,?,1,COALESCE(MAX(sequence),0)+1,'user',?,? FROM messages WHERE session_id=?`,
					"message_"+string(inputID), id, turnID, turnID, inputID, started, id); err != nil {
					return err
				}
			}
		}

		if result.Turn.Kind == session.PromptInput {
			if _, err := observeMailBoundary(ctx, tx, result.Turn, false); err != nil {
				return err
			}
		}
		result.Configuration = current.Config
		return nil
	})
	if err == nil && skipped {
		err = ErrNoWork
	}
	return
}

const messageSelect = `SELECT m.id,m.session_id,COALESCE(m.turn_id,''),m.sequence,m.role,m.input_id,
 COALESCE(m.parts,i.parts),m.created_at,m.mail_id,m.mail_revision,m.mail_presentation,r.subject,r.body,mail.source_kind,mail.source_id,r.evidence_ref,COALESCE(m.design_context,i.design_context),` + historyProvenanceColumns + `
 FROM messages m LEFT JOIN inputs i ON i.id=m.input_id
 LEFT JOIN mail_revisions r ON r.mail_id=m.mail_id AND r.revision=m.mail_revision
 LEFT JOIN mail ON mail.id=m.mail_id`

func scanMessage(row scanner) (result session.Message, err error) {
	var raw sql.NullString
	var design *string
	var created int64
	var mailID *session.MailID
	var revision sql.NullInt64
	var presentation, subject, body, sourceKind, sourceID sql.NullString
	var evidence *string
	var sourceOwner *session.SessionID
	var sourceMessage *session.MessageID
	var sourceSequence sql.NullInt64
	err = row.Scan(&result.ID, &result.SessionID, &result.TurnID, &result.Sequence, &result.Role, &result.InputID, &raw, &created, &mailID, &revision, &presentation, &subject, &body, &sourceKind, &sourceID, &evidence, &design, &result.GroupID, &result.OpeningInput, &sourceOwner, &sourceMessage, &sourceSequence, &result.RetiredBy, &result.RetiredRevision)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	if sourceOwner != nil {
		result.Source = &session.MessageSource{SessionID: *sourceOwner, MessageID: *sourceMessage, Sequence: sourceSequence.Int64}
	}
	if mailID != nil {
		result.Mail = &session.MailRef{ID: *mailID, Revision: revision.Int64, Presentation: session.MailPresentation(presentation.String)}
		result.Parts = mailParts(*result.Mail, session.MailSource{Kind: sourceKind.String, ID: sourceID.String}, subject.String, body.String, evidence)
	} else {
		err = json.Unmarshal([]byte(raw.String), &result.Parts)
	}
	if err == nil {
		result.DesignContext, err = decodeDesignContext(design, result.Parts)
	}
	return
}

func validDraft(draft session.MessageDraft) error {
	if err := session.ValidateID(string(draft.ID)); err != nil {
		return err
	}
	if draft.Role != session.Assistant && draft.Role != session.Tool && draft.Role != session.System {
		return fmt.Errorf("%w: user messages are created only by claiming input", session.ErrInvalid)
	}
	if draft.Continuation != nil {
		if draft.Role != session.Assistant {
			return fmt.Errorf("%w: only assistant messages can retain provider continuation", session.ErrInvalid)
		}
		if err := draft.Continuation.Validate(); err != nil {
			return err
		}
	}
	return session.ValidateMessage(draft.Role, draft.Parts)
}

func appendMessage(ctx context.Context, tx *sql.Tx, turn session.Turn, draft session.MessageDraft) (session.Message, error) {
	if turn.Kind == session.HostOperationInputKind {
		return session.Message{}, session.ErrInvalid
	}
	if turn.Kind != session.PromptInput {
		return session.Message{}, fmt.Errorf("%w: maintenance cannot author transcript messages", session.ErrInvalid)
	}
	if err := validDraft(draft); err != nil {
		return session.Message{}, err
	}
	existing, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", draft.ID))
	if err == nil {
		if existing.TurnID != turn.ID || existing.Role != draft.Role || !reflect.DeepEqual(existing.Parts, draft.Parts) {
			return session.Message{}, ErrConflict
		}
		continuation, err := readContinuation(ctx, tx, draft.ID)
		if err != nil {
			return session.Message{}, err
		}
		if !reflect.DeepEqual(continuation, draft.Continuation) {
			return session.Message{}, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return session.Message{}, err
	}
	if turn.State.Terminal() {
		return session.Message{}, ErrConflict
	}
	if err := validateCallOrder(ctx, tx, turn.ID, draft); err != nil {
		return session.Message{}, err
	}
	if err := validateContentReferences(ctx, tx, turn.SessionID, draft.Parts); err != nil {
		return session.Message{}, err
	}
	raw, err := encode(draft.Parts)
	if err != nil {
		return session.Message{}, err
	}
	var continuation any
	if draft.Continuation != nil {
		continuation, err = encode(draft.Continuation)
		if err != nil {
			return session.Message{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id,session_id,turn_id,group_id,sequence,role,parts,model_continuation,created_at)
  SELECT ?,?,?,?,COALESCE(MAX(sequence),0)+1,?,?,?,? FROM messages WHERE session_id=?`,
		draft.ID, turn.SessionID, turn.ID, turn.ID, draft.Role, raw, continuation, now(), turn.SessionID); err != nil {
		return session.Message{}, err
	}
	return scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", draft.ID))
}

func (s *Store) AppendMessage(ctx context.Context, id session.TurnID, draft session.MessageDraft) (result session.Message, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		turn, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		result, err = appendMessage(ctx, tx, turn, draft)
		return err
	})
	return
}

func (s *Store) History(ctx context.Context, id session.SessionID, after int64, limit int) ([]session.Message, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, fmt.Errorf("%w: negative history cursor", session.ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, messageSelect+" WHERE m.session_id=? AND m.retired_revision IS NULL AND m.sequence>? ORDER BY m.sequence LIMIT ?", id, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Message{}
	size := 0
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(message)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, message)
	}
	return result, rows.Err()
}

func (s *Store) Finish(ctx context.Context, id session.TurnID, state session.TurnState, failure *string, messages []session.MessageDraft) (result session.Turn, err error) {
	if !state.Terminal() || (state == session.Succeeded && failure != nil) || len(messages) > 128 {
		return result, fmt.Errorf("%w: invalid terminal outcome", session.ErrInvalid)
	}
	if failure != nil {
		if err := session.ValidateText(*failure, 16384); err != nil {
			return result, err
		}
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.State.Terminal() {
			if current.State != state || !reflect.DeepEqual(current.Failure, failure) {
				return ErrConflict
			}
		} else if !current.State.CanTransitionTo(state) {
			return ErrConflict
		}
		pending, err := unfinishedExecution(ctx, tx, id)
		if err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		for _, draft := range messages {
			if _, err := appendMessage(ctx, tx, current, draft); err != nil {
				return err
			}
		}
		if !current.State.Terminal() {
			if state == session.Succeeded {
				_, pending, err := pendingCalls(ctx, tx, id)
				if err != nil {
					return err
				}
				if len(pending) != 0 {
					return ErrBusy
				}
			} else if err := reconcileCalls(ctx, tx, current, "turn ended"); err != nil {
				return err
			}
			if state == session.Succeeded && current.Kind == session.PromptInput {
				if err := acknowledgeMail(ctx, tx, id); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM turn_permits WHERE turn_id=?", id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE turns SET state=?,failure=?,finished_at=? WHERE id=?", state, failure, now(), id); err != nil {
				return err
			}
		}
		result, err = readTurn(ctx, tx, id)
		if err == nil && !current.State.Terminal() {
			if err = finishGoal(ctx, tx, result); err == nil {
				err = captureCompletion(ctx, tx, result)
			}
		}
		return err
	})
	return
}
