package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
)

const mailSelect = `SELECT m.id,r.revision,m.source_kind,m.source_id,m.recipient_id,r.delivery,r.subject,length(CAST(r.body AS BLOB)),m.state,r.available_at,m.created_at,r.created_at,r.evidence_ref,r.body
 FROM mail m JOIN mail_revisions r ON r.mail_id=m.id`

const mailMetadataSelect = `SELECT m.id,r.revision,m.source_kind,m.source_id,m.recipient_id,r.delivery,r.subject,length(CAST(r.body AS BLOB)),m.state,r.available_at,m.created_at,r.created_at,r.evidence_ref
 FROM mail m JOIN mail_revisions r ON r.mail_id=m.id`

func scanMailMetadata(row scanner) (result session.MailMetadata, err error) {
	var available, created, revised int64
	err = row.Scan(&result.ID, &result.Revision, &result.Source.Kind, &result.Source.ID, &result.RecipientID, &result.Delivery, &result.Subject, &result.BodyBytes, &result.State, &available, &created, &revised, &result.EvidenceRef)
	result.AvailableAt, result.CreatedAt, result.RevisedAt = timestamp(available), timestamp(created), timestamp(revised)
	return result, found(err)
}

func scanMail(row scanner) (result session.Mail, err error) {
	var available, created, revised int64
	err = row.Scan(&result.ID, &result.Revision, &result.Source.Kind, &result.Source.ID, &result.RecipientID, &result.Delivery, &result.Subject, &result.BodyBytes, &result.State, &available, &created, &revised, &result.EvidenceRef, &result.Body)
	result.AvailableAt, result.CreatedAt, result.RevisedAt = timestamp(available), timestamp(created), timestamp(revised)
	return result, found(err)
}

func readMail(ctx context.Context, q querier, recipient session.SessionID, id session.MailID) (session.Mail, error) {
	return scanMail(q.QueryRowContext(ctx, mailSelect+" WHERE m.recipient_id=? AND m.id=? AND m.deleted_at IS NULL AND r.revision=m.revision", recipient, id))
}

func (s *Store) ReadMail(ctx context.Context, recipient session.SessionID, id session.MailID) (session.Mail, error) {
	return readMail(ctx, s.db, recipient, id)
}

func listMail(ctx context.Context, q querier, recipient session.SessionID, state session.MailState, after session.MailID, limit int) ([]session.MailMetadata, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	if state != "" && state != session.MailPending && state != session.MailDelivered && state != session.MailDone {
		return nil, session.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, mailMetadataSelect+" WHERE m.recipient_id=? AND m.deleted_at IS NULL AND r.revision=m.revision AND (?='' OR m.state=?) AND m.id>? ORDER BY m.id LIMIT ?", recipient, state, state, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.MailMetadata{}
	for rows.Next() {
		mail, err := scanMailMetadata(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, mail)
	}
	return result, rows.Err()
}

func (s *Store) ListMail(ctx context.Context, recipient session.SessionID, state session.MailState, after session.MailID, limit int) ([]session.MailMetadata, error) {
	return listMail(ctx, s.db, recipient, state, after, limit)
}

func validateMailSpec(spec session.MailSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	for _, id := range []string{string(spec.ID), string(spec.SenderID)} {
		if err := session.ValidateID(id); err != nil {
			return err
		}
	}
	return nil
}

func mailMembers(ctx context.Context, q querier, senderID, recipientID session.SessionID) error {
	sender, err := readSession(ctx, q, senderID)
	if err != nil {
		return err
	}
	recipient, err := readSession(ctx, q, recipientID)
	if err != nil {
		return err
	}
	if sender.TreeID != recipient.TreeID {
		return ErrConflict
	}
	related := sender.ID == recipient.ID || sender.ParentID != nil && *sender.ParentID == recipient.ID || recipient.ParentID != nil && *recipient.ParentID == sender.ID || sender.ParentID != nil && recipient.ParentID != nil && *sender.ParentID == *recipient.ParentID
	if !related {
		return fmt.Errorf("%w: mail recipient must be self or a direct relative", ErrConflict)
	}
	return nil
}

func mailCapacity(ctx context.Context, q querier, source session.MailSource, recipient session.SessionID, id session.MailID, newIdentity bool) error {
	var count, pending, backlog int
	err := q.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(state='pending'),0),COALESCE(sum(source_kind=? AND source_id=? AND state<>'done'),0)
 FROM mail WHERE recipient_id=? AND deleted_at IS NULL AND id<>?`, source.Kind, source.ID, recipient, id).Scan(&count, &pending, &backlog)
	if err != nil {
		return err
	}
	if count >= session.MaxMailPerSession || pending >= session.MaxPendingMail || backlog >= session.MaxMailBacklog {
		return ErrLimit
	}
	if newIdentity {
		var recent int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM mail WHERE source_kind=? AND source_id=? AND created_at>?", source.Kind, source.ID, now()-int64(10*time.Second/time.Microsecond)).Scan(&recent); err != nil {
			return err
		}
		if recent >= 30 {
			return ErrLimit
		}
	}
	return nil
}

func (s *Store) SendMail(ctx context.Context, spec session.MailSpec) (result session.MailAdmission, err error) {
	if err := validateMailSpec(spec); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = sendMail(ctx, tx, spec); return err })
	return
}

func sendMail(ctx context.Context, tx *sql.Tx, spec session.MailSpec) (session.MailAdmission, error) {
	result := session.MailAdmission{ID: spec.ID}
	digest, err := requestDigest("send_mail", spec)
	if err != nil {
		return result, err
	}
	var existing string
	var deleted sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT initial_digest,deleted_at FROM mail WHERE id=?", spec.ID).Scan(&existing, &deleted)
	if err == nil {
		if existing != digest {
			return result, ErrConflict
		}
		if deleted.Valid {
			result.DeletedAt = optionalTime(deleted)
			return result, nil
		}
		mail, err := readMail(ctx, tx, spec.RecipientID, spec.ID)
		result.Mail = &mail.MailMetadata
		return result, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if err := mailMembers(ctx, tx, spec.SenderID, spec.RecipientID); err != nil {
		return result, err
	}
	if err := mailCapacity(ctx, tx, session.MailSource{Kind: "session", ID: string(spec.SenderID)}, spec.RecipientID, spec.ID, true); err != nil {
		return result, err
	}
	if err := shareMailEvidence(ctx, tx, &spec); err != nil {
		return result, err
	}
	created := now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO mail VALUES (?,'session',?,?,?,1,'pending',?,NULL)", spec.ID, spec.SenderID, spec.RecipientID, digest, created); err != nil {
		return result, err
	}
	if err := insertMailRevision(ctx, tx, spec, 1, created); err != nil {
		return result, err
	}
	if err := chargeWrite(ctx, tx, spec.SenderID, "mail", string(spec.ID), 1, int64(len(spec.Subject)+len(spec.Body))); err != nil {
		return result, err
	}
	mail, err := readMail(ctx, tx, spec.RecipientID, spec.ID)
	result.Mail = &mail.MailMetadata
	return result, err
}

func insertMailRevision(ctx context.Context, tx *sql.Tx, spec session.MailSpec, revision, created int64) error {
	available := created
	if spec.AvailableAt != nil {
		available = spec.AvailableAt.UTC().UnixMicro()
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO mail_revisions (mail_id,revision,delivery,subject,body,available_at,created_at,evidence_ref,recipient_id) VALUES (?,?,?,?,?,?,?,?,?)", spec.ID, revision, spec.Delivery, spec.Subject, spec.Body, available, created, spec.EvidenceRef, spec.RecipientID)
	return err
}

func (s *Store) ReplaceMail(ctx context.Context, spec session.MailSpec, expectedRevision int64) (result session.Mail, err error) {
	if err := validateMailSpec(spec); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readMail(ctx, tx, spec.RecipientID, spec.ID)
		if err != nil {
			return err
		}
		if current.State != session.MailPending || current.Revision != expectedRevision || current.Source != (session.MailSource{Kind: "session", ID: string(spec.SenderID)}) {
			return ErrConflict
		}
		if err := mailMembers(ctx, tx, spec.SenderID, spec.RecipientID); err != nil {
			return err
		}
		if err := shareMailEvidence(ctx, tx, &spec); err != nil {
			return err
		}
		result, err = replaceMail(ctx, tx, spec, current)
		if err == nil {
			err = chargeWrite(ctx, tx, spec.SenderID, "mail", string(spec.ID), result.Revision, int64(len(spec.Subject)+len(spec.Body)))
		}
		return err
	})
	return
}

func replaceMail(ctx context.Context, tx *sql.Tx, spec session.MailSpec, current session.Mail) (session.Mail, error) {
	if current.Revision >= session.MaxMailRevisions {
		return session.Mail{}, ErrLimit
	}
	if err := mailCapacity(ctx, tx, current.Source, spec.RecipientID, spec.ID, false); err != nil {
		return session.Mail{}, err
	}
	if err := insertMailRevision(ctx, tx, spec, current.Revision+1, now()); err != nil {
		return session.Mail{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE mail SET revision=revision+1,state='pending' WHERE id=?", spec.ID); err != nil {
		return session.Mail{}, err
	}
	return readMail(ctx, tx, spec.RecipientID, spec.ID)
}

// Only authored admission transfers access. Revision-only changes such as
// deferral already carry the recipient's immutable reference.
func shareMailEvidence(ctx context.Context, tx *sql.Tx, spec *session.MailSpec) error {
	if spec.EvidenceRef == nil {
		return nil
	}
	reference, err := shareContent(ctx, tx, spec.SenderID, spec.RecipientID, *spec.EvidenceRef)
	if err != nil {
		return err
	}
	spec.EvidenceRef = &reference.ID
	return nil
}
