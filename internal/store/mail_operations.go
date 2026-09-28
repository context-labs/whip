package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type mailItems struct {
	Items []session.MailMetadata `json:"items"`
}

// ApplyMailOperation commits authorization, observation or mutation, and the
// operation outcome together. Read bodies are reconstructed from their immutable
// revision; the operation stores only the metadata identifying that revision.
func (s *Store) ApplyMailOperation(ctx context.Context, id session.OperationID) (result json.RawMessage, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		switch operation.Capability {
		case "mail.send", "mail.list", "mail.read", "mail.complete", "mail.defer":
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
			if operation.Capability == "mail.read" {
				result, err = readMailResult(ctx, tx, owner.ID, result)
			}
			return err
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		var stored json.RawMessage
		stored, result, err = applyMailOperation(ctx, tx, operation)
		if err != nil {
			return err
		}
		operation.State = session.OperationDispatched
		_, err = settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationSucceeded, Value: stored})
		return err
	})
	return
}

func applyMailOperation(ctx context.Context, tx *sql.Tx, operation session.Operation) (json.RawMessage, json.RawMessage, error) {
	var value any
	switch operation.Capability {
	case "mail.send":
		var request session.MailSend
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, nil, err
		}
		spec := session.MailSpec{MailSend: request, ID: session.MailID(fmt.Sprintf("mail_%x", sha256.Sum256([]byte(operation.ID)))), SenderID: operation.SessionID}
		if err := validateMailSpec(spec); err != nil {
			return nil, nil, err
		}
		result, err := sendMail(ctx, tx, spec)
		if err != nil {
			return nil, nil, err
		}
		value = result
	case "mail.list":
		var request session.MailList
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, nil, err
		}
		items, err := listMail(ctx, tx, operation.SessionID, request.State, request.After, request.Limit)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			if err := observeMail(ctx, tx, operation.TurnID, item.MailReceipt, false); err != nil {
				return nil, nil, err
			}
		}
		value = mailItems{Items: items}
	case "mail.read":
		var request session.MailRead
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, nil, err
		}
		mail, err := readMail(ctx, tx, operation.SessionID, request.ID)
		if err != nil {
			return nil, nil, err
		}
		if err := observeMail(ctx, tx, operation.TurnID, mail.MailReceipt, true); err != nil {
			return nil, nil, err
		}
		stored, err := json.Marshal(mail.MailMetadata)
		if err != nil {
			return nil, nil, err
		}
		result, err := json.Marshal(mail)
		return stored, result, err
	case "mail.complete":
		var request session.MailComplete
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, nil, err
		}
		items, err := completeMail(ctx, tx, operation.SessionID, operation.TurnID, request.Receipts)
		if err != nil {
			return nil, nil, err
		}
		value = mailItems{Items: items}
	case "mail.defer":
		var request session.MailDefer
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, nil, err
		}
		if request.AvailableAt.IsZero() || !request.AvailableAt.After(time.Now()) || request.AvailableAt.Year() > 9999 {
			return nil, nil, session.ErrInvalid
		}
		current, err := observedMail(ctx, tx, operation.SessionID, operation.TurnID, request.Receipt)
		if err != nil {
			return nil, nil, err
		}
		if current.State == session.MailDone {
			return nil, nil, ErrConflict
		}
		spec := session.MailSpec{ID: current.ID, SenderID: current.SenderID, RecipientID: current.RecipientID, Subject: current.Subject, Body: current.Body, Delivery: current.Delivery, AvailableAt: &request.AvailableAt}
		next, err := replaceMail(ctx, tx, spec, current)
		if err != nil {
			return nil, nil, err
		}
		value = next.MailMetadata
	}
	raw, err := json.Marshal(value)
	return raw, raw, err
}

func readMailResult(ctx context.Context, q querier, owner session.SessionID, raw json.RawMessage) (json.RawMessage, error) {
	var metadata session.MailMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	if metadata.RecipientID != owner {
		return nil, ErrConflict
	}
	var body string
	if err := q.QueryRowContext(ctx, "SELECT r.body FROM mail_revisions r JOIN mail m ON m.id=r.mail_id WHERE r.mail_id=? AND r.revision=? AND m.recipient_id=? AND m.deleted_at IS NULL", metadata.ID, metadata.Revision, owner).Scan(&body); err != nil {
		return nil, found(err)
	}
	return json.Marshal(session.Mail{MailMetadata: metadata, Body: body})
}

func observedMail(ctx context.Context, q querier, owner session.SessionID, turn session.TurnID, receipt session.MailReceipt) (session.Mail, error) {
	if err := receipt.Validate(); err != nil {
		return session.Mail{}, err
	}
	mail, err := readMail(ctx, q, owner, receipt.ID)
	if err != nil {
		return mail, err
	}
	if mail.Revision != receipt.Revision {
		return mail, ErrConflict
	}
	var observed bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM turn_mail_observations WHERE turn_id=? AND mail_id=? AND revision=?)", turn, receipt.ID, receipt.Revision).Scan(&observed); err != nil {
		return mail, err
	}
	if !observed {
		return mail, ErrConflict
	}
	return mail, nil
}

func completeMail(ctx context.Context, tx *sql.Tx, owner session.SessionID, turn session.TurnID, receipts []session.MailReceipt) ([]session.MailMetadata, error) {
	if len(receipts) < 1 || len(receipts) > 100 {
		return nil, session.ErrInvalid
	}
	seen := map[session.MailID]bool{}
	result := make([]session.MailMetadata, 0, len(receipts))
	for _, receipt := range receipts {
		if seen[receipt.ID] {
			return nil, session.ErrInvalid
		}
		seen[receipt.ID] = true
		mail, err := observedMail(ctx, tx, owner, turn, receipt)
		if err != nil {
			return nil, err
		}
		result = append(result, mail.MailMetadata)
	}
	for i := range result {
		if _, err := tx.ExecContext(ctx, "UPDATE mail SET state='done' WHERE id=?", result[i].ID); err != nil {
			return nil, err
		}
		result[i].State = session.MailDone
	}
	return result, nil
}
