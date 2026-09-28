package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

// A failed latest turn suppresses autonomous mail retries. Explicit input can
// still claim; its successful finish restores autonomous delivery at every depth.
const mailRetryAllowed = `COALESCE((SELECT t.state FROM turns t LEFT JOIN inputs i ON i.turn_id=t.id
 WHERE t.session_id=m.recipient_id AND COALESCE(i.kind,'prompt')='prompt'
 ORDER BY t.started_at DESC,t.rowid DESC LIMIT 1),'succeeded')='succeeded'`

func mailReady(ctx context.Context, q querier, owner session.SessionID) (bool, error) {
	var ready bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mail m JOIN mail_revisions r ON r.mail_id=m.id AND r.revision=m.revision
 WHERE m.recipient_id=? AND m.state='pending' AND m.deleted_at IS NULL AND r.delivery<>'next_turn' AND r.available_at<=? AND `+mailRetryAllowed+")", owner, now()).Scan(&ready)
	return ready, err
}

func mailParts(reference session.MailRef, source session.MailSource, subject, body string) []session.Part {
	if reference.Presentation == session.MailDigest {
		original := body
		if len(body) > 2048 {
			body = body[:2048]
			for !utf8.ValidString(body) {
				body = body[:len(body)-1]
			}
		}
		lines := strings.SplitN(body, "\n", 21)
		if len(lines) > 20 {
			body = strings.Join(lines[:20], "\n")
		}
		if body != original {
			body += "…"
		}
	}
	return []session.Part{{Type: "text", Text: fmt.Sprintf("[Mail %s revision %d from %s %s]\nSubject: %s\n%s", reference.ID, reference.Revision, source.Kind, source.ID, subject, body)}}
}

func observeMail(ctx context.Context, tx *sql.Tx, turn session.TurnID, receipt session.MailReceipt, presented bool) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM turn_mail_observations WHERE turn_id=? AND mail_id=? AND revision=?)", turn, receipt.ID, receipt.Revision).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM turn_mail_observations WHERE turn_id=?", turn).Scan(&count); err != nil {
			return err
		}
		if count >= session.MaxMailObservations {
			return ErrLimit
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO turn_mail_observations VALUES (?,?,?,?)
 ON CONFLICT(turn_id,mail_id,revision) DO UPDATE SET presented=max(presented,excluded.presented)`, turn, receipt.ID, receipt.Revision, presented)
	return err
}

func observeMailBoundary(ctx context.Context, tx *sql.Tx, turn session.Turn, steersOnly bool) ([]session.Message, error) {
	if turn.State != session.Running {
		return nil, ErrStopped
	}
	if turn.Kind == session.CompactInput {
		return []session.Message{}, nil
	}
	_, pending, err := pendingCalls(ctx, tx, turn.ID)
	if err != nil {
		return nil, err
	}
	if len(pending) != 0 {
		return nil, ErrBusy
	}
	result := []session.Message{}
	for range 20 {
		mail, err := scanMail(tx.QueryRowContext(ctx, mailSelect+` WHERE m.recipient_id=? AND m.state='pending' AND m.deleted_at IS NULL
 AND r.revision=m.revision AND r.available_at<=? AND (?=0 OR r.delivery='steer')
 AND NOT EXISTS(SELECT 1 FROM turn_mail_observations o WHERE o.turn_id=? AND o.mail_id=m.id AND o.revision=r.revision AND o.presented=1)
 ORDER BY r.available_at,m.created_at,m.id LIMIT 1`, turn.SessionID, now(), steersOnly, turn.ID))
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := observeMail(ctx, tx, turn.ID, mail.MailReceipt, true); err != nil {
			return nil, err
		}
		id := session.MessageID(newID("message"))
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id,session_id,turn_id,sequence,role,mail_id,mail_revision,mail_presentation,created_at)
 SELECT ?,?,?,COALESCE(MAX(sequence),0)+1,'user',?,?,'digest',? FROM messages WHERE session_id=?`, id, turn.SessionID, turn.ID, mail.ID, mail.Revision, now(), turn.SessionID); err != nil {
			return nil, err
		}
		message, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", id))
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, nil
}

func (s *Store) ObserveSteers(ctx context.Context, id session.TurnID) (result []session.Message, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		turn, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		result, err = observeMailBoundary(ctx, tx, turn, true)
		return err
	})
	return
}

func acknowledgeMail(ctx context.Context, tx *sql.Tx, turn session.TurnID) error {
	_, err := tx.ExecContext(ctx, `UPDATE mail SET state='delivered' WHERE state='pending' AND deleted_at IS NULL
 AND EXISTS(SELECT 1 FROM turn_mail_observations o WHERE o.turn_id=? AND o.mail_id=mail.id AND o.revision=mail.revision AND o.presented=1)`, turn)
	return err
}

func deleteRecipientMail(ctx context.Context, tx *sql.Tx, owner session.SessionID) error {
	if _, err := tx.ExecContext(ctx, subtree+" UPDATE mail SET revision=NULL,state=NULL,deleted_at=? WHERE recipient_id IN (SELECT id FROM subtree) AND deleted_at IS NULL", owner, now()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, subtree+" DELETE FROM mail_revisions WHERE mail_id IN (SELECT id FROM mail WHERE recipient_id IN (SELECT id FROM subtree) AND deleted_at IS NOT NULL)", owner)
	return err
}
