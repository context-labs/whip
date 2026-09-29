package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

// ChildTransferIntent captures the exact native owners reviewed for one spawn.
// The request remains the receipt payload; live scope is operation evidence and
// cannot restore browser authority after restart.
type ChildTransferIntent struct {
	Request  ChildRequest           `json:"request"`
	ChildID  session.SessionID      `json:"child_id"`
	Parents  []session.BrowserScope `json:"parents"`
	Children []session.BrowserScope `json:"children"`
}

func TransferChildID(id session.OperationID) session.SessionID {
	sum := sha256.Sum256([]byte("browser-child\x00" + string(id)))
	return session.SessionID("session_" + hex.EncodeToString(sum[:]))
}

func childTransferIntent(ctx context.Context, q querier, spec session.OperationSpec) (ChildTransferIntent, session.Configuration, error) {
	var intent ChildTransferIntent
	decoder := json.NewDecoder(bytes.NewReader(spec.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, session.Configuration{}, err
	}
	invalid := func() (ChildTransferIntent, session.Configuration, error) {
		return intent, session.Configuration{}, ErrConflict
	}
	if spec.Capability != "agents.spawn" || intent.ChildID != TransferChildID(spec.ID) || len(intent.Request.BrowserAttachments) == 0 || len(intent.Parents) != len(intent.Request.BrowserAttachments) || len(intent.Children) != len(intent.Parents) {
		return invalid()
	}
	if err := validateChildRequest(session.RequestIdentity{ClientID: "operation", RequestID: string(spec.ID)}, intent.Request); err != nil {
		return intent, session.Configuration{}, err
	}
	ownerID, turnID, err := operationOwnerLive(ctx, q, spec)
	if err != nil {
		return intent, session.Configuration{}, err
	}
	if spec.DirectTurnID != "" {
		accepted, _, err := acceptedChildTransfer(ctx, q, spec.DirectTurnID)
		if err != nil {
			return intent, session.Configuration{}, err
		}
		if accepted.Request.ParentID != ownerID {
			return invalid()
		}
	}
	parent, err := readSession(ctx, q, ownerID)
	if err != nil {
		return intent, session.Configuration{}, err
	}
	if intent.Request.ParentID != ownerID || spec.Resource != string(parent.TreeID) {
		return invalid()
	}
	var revision session.Revision
	if err := q.QueryRowContext(ctx, "SELECT config_revision FROM turns WHERE id=?", turnID).Scan(&revision); err != nil {
		return intent, session.Configuration{}, found(err)
	}
	parent, err = capturedSession(ctx, q, parent, revision)
	if err != nil {
		return intent, session.Configuration{}, err
	}
	preview, err := resolveChild(ctx, q, parent, intent.Request.SpawnSession)
	if err != nil {
		return intent, session.Configuration{}, err
	}
	if !slices.Contains(parent.Config.Modules, "agents") || !slices.Contains(parent.Config.Modules, "browser") || !slices.Contains(preview.Configuration.Modules, "browser") {
		return invalid()
	}
	grants, err := delegatedGrants(ctx, q, ownerID, intent.Request.GrantIDs)
	if err != nil {
		return intent, session.Configuration{}, err
	}
	seen := map[string]bool{}
	for i, original := range intent.Parents {
		next := intent.Children[i]
		if original.Validate() != nil || next.Validate() != nil || original.Resource() != next.Resource() || original.AttachmentID == next.AttachmentID || original.AttachmentGeneration == next.AttachmentGeneration || !slices.Contains(intent.Request.BrowserAttachments, original.AttachmentID) || seen[original.AttachmentID] {
			return invalid()
		}
		seen[original.AttachmentID] = true
		delegated := false
		for _, grant := range grants {
			if grant.Capability == "browser.control" && grant.Resource == original.Resource() {
				delegated = true
				break
			}
		}
		if !delegated {
			return invalid()
		}
	}
	return intent, parent.Config, nil
}

// isChildTransfer distinguishes the fixed transfer envelope from ordinary spawn.
// Malformed transfer envelopes fail closed in childTransferIntent.
func isChildTransfer(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, exists := fields["request"]
	return exists
}

// CheckChildTransfer rechecks consent, captured narrowing, all delegated issuer
// chains and child capacity immediately before native handoff. The ordinary SQL
// admission is exercised under a savepoint, then entirely rolled back; no child,
// receipt or resource reservation is ever published by this check.
func (s *Store) CheckChildTransfer(ctx context.Context, id session.OperationID, dispatched bool) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		op, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if dispatched && op.State != session.OperationDispatched || !dispatched && op.State != session.OperationReady {
			return ErrConflict
		}
		intent, captured, err := childTransferIntent(ctx, tx, op.OperationSpec)
		if err != nil {
			return err
		}
		if err := authorizeOperation(ctx, tx, op); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT child_transfer_preview"); err != nil {
			return err
		}
		identity, digest, err := childTransferReceipt(ctx, tx, op, intent)
		if err != nil {
			return err
		}
		_, admissionErr := spawnChildAccepted(ctx, tx, intent.ChildID, identity, digest, intent.Request, &captured)
		_, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO child_transfer_preview")
		_, releaseErr := tx.ExecContext(ctx, "RELEASE child_transfer_preview")
		return errors.Join(admissionErr, rollbackErr, releaseErr)
	})
}

// CommitChildTransfer runs only after the native atomic handoff acknowledged.
// It commits the child, delegated grants, initial input, exact receipt and success
// together. Native ownership itself is not SQL and must never be replayed on an
// uncertain transaction result.
func (s *Store) CommitChildTransfer(ctx context.Context, id session.OperationID, children []session.BrowserScope) (result ChildAdmission, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		op, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		var original ChildTransferIntent
		if json.Unmarshal(op.Arguments, &original) != nil {
			return ErrConflict
		}
		identity, digest, err := childTransferReceipt(ctx, tx, op, original)
		if err != nil {
			return err
		}
		if op.State == session.OperationSucceeded {
			if json.Unmarshal(op.Arguments, &original) != nil || original.ChildID != TransferChildID(id) || !reflect.DeepEqual(children, original.Children) {
				return ErrConflict
			}
			result, err = readChildAdmission(ctx, tx, identity)
			return err
		}
		if op.State != session.OperationDispatched {
			return ErrConflict
		}
		intent, captured, err := childTransferIntent(ctx, tx, op.OperationSpec)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(children, intent.Children) {
			return ErrConflict
		}
		if err := authorizeOperation(ctx, tx, op); err != nil {
			return err
		}
		result, err = spawnChildAccepted(ctx, tx, intent.ChildID, identity, digest, intent.Request, &captured)
		if err != nil {
			return err
		}
		value, err := ChildAdmissionValue(result)
		if err != nil {
			return err
		}
		_, err = settleOperation(ctx, tx, op, session.OperationResult{State: session.OperationSucceeded, Value: value})
		return err
	})
	return
}

// ChildAdmissionValue is shared with the host dispatcher so its acknowledgement
// repeats the exact already-committed settlement rather than a second outcome.
func ChildAdmissionValue(result ChildAdmission) (json.RawMessage, error) {
	if result.Session == nil || result.Admission.Input == nil {
		return nil, ErrNotFound
	}
	return json.Marshal(map[string]any{"session_id": string(result.Session.ID), "input_id": string(result.Admission.Input.ID)})
}
