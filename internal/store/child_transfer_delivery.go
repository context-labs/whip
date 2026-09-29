package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

const childTransferClient = "browser-transfer"

var (
	ErrTransferFailed      = errors.New("accepted child browser transfer failed; it will not be repeated")
	ErrTransferUncertain   = errors.New("accepted child browser transfer has an uncertain effect; it will not be repeated")
	ErrTransferInterrupted = errors.New("accepted child browser transfer was interrupted; it will not be repeated")
	ErrTransferCancelled   = errors.New("accepted child browser transfer was cancelled; it will not be repeated")
	ErrTransferDeleted     = errors.New("accepted child browser transfer was deleted; it will not be repeated")
)

// ChildTransferRequest is the immutable private input behind sessions.spawn.
// The public identity does not name this input or acquire its reserved receipt.
// Only the successful handoff publishes the public receipt pointing to a child.
type ChildTransferRequest struct {
	Identity session.RequestIdentity `json:"identity"`
	Request  ChildRequest            `json:"request"`
}

func childTransferIdentity(identity session.RequestIdentity) session.RequestIdentity {
	sum := sha256.Sum256([]byte(identity.ClientID + "\x00" + identity.RequestID))
	return session.RequestIdentity{ClientID: childTransferClient, RequestID: hex.EncodeToString(sum[:])}
}

func validateChildTransferRequest(identity session.RequestIdentity, request ChildRequest) (string, error) {
	if err := validatePublicIdentity(identity); err != nil {
		return "", err
	}
	if len(request.BrowserAttachments) == 0 {
		return "", session.ErrInvalid
	}
	if err := validateChildRequest(identity, request); err != nil {
		return "", err
	}
	return childRequestDigest(request)
}

// BeginChildTransfer admits exactly one private, model-free host input. Retrying
// only reads the accepted input; caller cancellation cannot cancel runtime work.
func (s *Store) BeginChildTransfer(ctx context.Context, identity session.RequestIdentity, request ChildRequest) (result Admission, err error) {
	digest, err := validateChildTransferRequest(identity, request)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(ChildTransferRequest{Identity: identity, Request: request})
	if err != nil {
		return result, err
	}
	submission, _, err := hostOperationSubmission(request.ParentID, session.HostOperation{Module: "agents", Name: "spawn", Arguments: raw})
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		receipt, readErr := readReceipt(ctx, tx, identity)
		if readErr == nil && receipt.Digest != digest {
			return ErrConflict
		}
		if readErr != nil && !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		result, err = admitHostOperation(ctx, tx, childTransferIdentity(identity), submission, digest)
		return err
	})
	return
}

func acceptedChildTransfer(ctx context.Context, q querier, turnID session.TurnID) (ChildTransferRequest, string, error) {
	var accepted ChildTransferRequest
	var raw, client, requestID, digest string
	var owner session.SessionID
	err := q.QueryRowContext(ctx, `SELECT h.arguments,r.client_id,r.request_id,r.digest,i.session_id FROM inputs i
 JOIN host_operation_inputs h ON h.input_id=i.id JOIN receipts r ON r.input_id=i.id
 WHERE i.turn_id=? AND h.module='agents' AND h.name='spawn'`, turnID).Scan(&raw, &client, &requestID, &digest, &owner)
	if err != nil {
		return accepted, "", found(err)
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&accepted); err != nil {
		return accepted, "", err
	}
	expected, err := validateChildTransferRequest(accepted.Identity, accepted.Request)
	if err != nil {
		return accepted, "", err
	}
	private := childTransferIdentity(accepted.Identity)
	if owner != accepted.Request.ParentID || client != private.ClientID || requestID != private.RequestID || expected != digest {
		return accepted, "", ErrConflict
	}
	return accepted, digest, nil
}

func (s *Store) AcceptedChildTransfer(ctx context.Context, turn session.TurnID) (ChildTransferRequest, error) {
	value, _, err := acceptedChildTransfer(ctx, s.db, turn)
	return value, err
}

// checkChildTransferReservation stops unrelated public admissions from occupying
// the public identity while its private handoff is accepted. Only the dispatched
// transfer's deterministic child may publish that identity. A ready operation
// may exercise the same check inside the rolled-back preflight savepoint; the
// committing path independently requires dispatch. No new ledger exists.
func checkChildTransferReservation(ctx context.Context, q querier, identity session.RequestIdentity, digest string, request Submission) error {
	if identity.ClientID == childTransferClient {
		return nil
	}
	private, err := readAdmission(ctx, q, childTransferIdentity(identity))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if private.Receipt.Digest != digest || private.Input == nil || private.Turn == nil || request.Source != session.AgentInput || request.Kind != "" && request.Kind != session.PromptInput {
		return ErrConflict
	}
	accepted, _, err := acceptedChildTransfer(ctx, q, private.Turn.ID)
	if err != nil {
		return err
	}
	if accepted.Identity != identity {
		return ErrConflict
	}
	var operation session.OperationID
	if err := q.QueryRowContext(ctx, `SELECT id FROM operations WHERE direct_turn_id=? AND capability='agents.spawn' AND state IN ('ready','dispatched')`, private.Turn.ID).Scan(&operation); err != nil {
		return ErrConflict
	}
	if request.SessionID != TransferChildID(operation) {
		return ErrConflict
	}
	return nil
}

func childTransferReceipt(ctx context.Context, q querier, op session.Operation, intent ChildTransferIntent) (session.RequestIdentity, string, error) {
	if op.DirectTurnID == "" {
		digest, err := childRequestDigest(intent.Request)
		return session.RequestIdentity{ClientID: "operation", RequestID: string(op.ID)}, digest, err
	}
	accepted, digest, err := acceptedChildTransfer(ctx, q, op.DirectTurnID)
	if err != nil {
		return session.RequestIdentity{}, "", err
	}
	if accepted.Request.ParentID != op.SessionID {
		return session.RequestIdentity{}, "", ErrConflict
	}
	return accepted.Identity, digest, nil
}

// ChildTransferResult resolves exact original bytes before lifecycle or live
// browser checks. An accepted private input is never reported as missing. Failed
// or interrupted work cannot mint a new child on any later explicit retry.
func (s *Store) ChildTransferResult(ctx context.Context, identity session.RequestIdentity, request ChildRequest) (result ChildAdmission, err error) {
	digest, err := validateChildTransferRequest(identity, request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		receipt, err := readReceipt(ctx, tx, identity)
		if err == nil {
			if receipt.Digest != digest {
				return ErrConflict
			}
			result, err = readChildAdmission(ctx, tx, identity)
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		private, err := readAdmission(ctx, tx, childTransferIdentity(identity))
		if err != nil {
			return err
		}
		if private.Receipt.Digest != digest {
			return ErrConflict
		}
		if private.Receipt.DeletedAt != nil {
			return ErrTransferDeleted
		}
		if private.Input == nil {
			return ErrConflict
		}
		if private.Input.State == session.InputCancelled {
			return ErrTransferCancelled
		}
		if private.Turn == nil {
			return ErrBusy
		}
		var state session.OperationState
		err = tx.QueryRowContext(ctx, `SELECT state FROM operations WHERE direct_turn_id=? AND capability='agents.spawn'`, private.Turn.ID).Scan(&state)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		switch state {
		case session.OperationUncertain:
			return ErrTransferUncertain
		case session.OperationFailed, session.OperationDenied:
			return ErrTransferFailed
		case session.OperationCancelled:
			return ErrTransferCancelled
		case session.OperationSucceeded:
			return ErrConflict // Success and public receipt are atomic.
		}
		switch private.Turn.State {
		case session.Failed:
			return ErrTransferFailed
		case session.Interrupted:
			return ErrTransferInterrupted
		case session.Cancelled:
			return ErrTransferCancelled
		case session.Succeeded:
			return ErrConflict
		default:
			return ErrBusy
		}
	})
	return
}
