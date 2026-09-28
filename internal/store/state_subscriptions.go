package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

const stateSubscriptionSelect = `SELECT id,tree_id,session_id,key,delivery,cursor,created_at,cancelled_at FROM state_subscriptions`

func scanStateSubscription(row scanner) (value session.StateSubscription, err error) {
	var created int64
	var cancelled sql.NullInt64
	err = row.Scan(&value.ID, &value.TreeID, &value.SessionID, &value.Key, &value.Delivery, &value.Cursor, &created, &cancelled)
	value.CreatedAt, value.CancelledAt = timestamp(created), optionalTime(cancelled)
	return value, found(err)
}

func (s *Store) SubscribeState(ctx context.Context, actor session.SessionID, id string, request session.StateSubscribe) (result session.StateSubscription, err error) {
	if err := session.ValidateID(id); err != nil {
		return result, err
	}
	if err := request.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = subscribeState(ctx, tx, actor, id, request); return err })
	return
}

func subscribeState(ctx context.Context, tx *sql.Tx, actor session.SessionID, id string, request session.StateSubscribe) (session.StateSubscription, error) {
	owner, err := readSession(ctx, tx, actor)
	if err != nil {
		return session.StateSubscription{}, err
	}
	digest, err := requestDigest("subscribe_state", request)
	if err != nil {
		return session.StateSubscription{}, err
	}
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT initial_digest FROM state_subscriptions WHERE id=?", id).Scan(&previous)
	if err == nil {
		existing, err := scanStateSubscription(tx.QueryRowContext(ctx, stateSubscriptionSelect+" WHERE id=?", id))
		if err != nil {
			return existing, err
		}
		if existing.SessionID != actor || previous != digest {
			return existing, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return session.StateSubscription{}, err
	}
	var duplicate bool
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM state_subscriptions WHERE session_id=? AND key=? AND cancelled_at IS NULL)", actor, request.Key).Scan(&duplicate); err != nil {
		return session.StateSubscription{}, err
	}
	if duplicate {
		return session.StateSubscription{}, ErrConflict
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM state_subscriptions WHERE tree_id=?", owner.TreeID).Scan(&count); err != nil {
		return session.StateSubscription{}, err
	}
	if count >= session.MaxStateSubscriptions {
		return session.StateSubscription{}, ErrLimit
	}
	latest, err := readState(ctx, tx, owner.TreeID, nil, request.Key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return session.StateSubscription{}, err
	}
	if request.After > latest.Revision {
		return session.StateSubscription{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO state_subscriptions VALUES (?,?,?,?,?,?,?,?,NULL)", id, owner.TreeID, actor, request.Key, request.Delivery, digest, request.After, now()); err != nil {
		return session.StateSubscription{}, err
	}
	if err := checkResources(ctx, tx, actor, session.ResourceSubscriptions); err != nil {
		return session.StateSubscription{}, err
	}
	if err := chargeWrite(ctx, tx, actor, "subscription", id, 0, int64(len(request.Key))); err != nil {
		return session.StateSubscription{}, err
	}
	value, err := scanStateSubscription(tx.QueryRowContext(ctx, stateSubscriptionSelect+" WHERE id=?", id))
	if err != nil {
		return value, err
	}
	if latest.Revision > request.After {
		if err := notifyStateSubscription(ctx, tx, value, latest); err != nil {
			return value, err
		}
	}
	return scanStateSubscription(tx.QueryRowContext(ctx, stateSubscriptionSelect+" WHERE id=?", id))
}

func (s *Store) StateSubscriptions(ctx context.Context, actor session.SessionID, after string, limit int) ([]session.StateSubscription, error) {
	return listStateSubscriptions(ctx, s.db, actor, after, limit)
}

func listStateSubscriptions(ctx context.Context, q querier, actor session.SessionID, after string, limit int) ([]session.StateSubscription, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	if _, err := readSession(ctx, q, actor); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, stateSubscriptionSelect+" WHERE session_id=? AND id>? ORDER BY id LIMIT ?", actor, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.StateSubscription{}
	for rows.Next() {
		value, err := scanStateSubscription(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Store) UnsubscribeState(ctx context.Context, actor session.SessionID, id string) (result session.StateSubscription, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = unsubscribeState(ctx, tx, actor, id); return err })
	return
}

func unsubscribeState(ctx context.Context, tx *sql.Tx, actor session.SessionID, id string) (session.StateSubscription, error) {
	value, err := scanStateSubscription(tx.QueryRowContext(ctx, stateSubscriptionSelect+" WHERE id=? AND session_id=?", id, actor))
	if err != nil {
		return value, err
	}
	if value.CancelledAt != nil {
		return value, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE state_subscriptions SET cancelled_at=? WHERE id=?", now(), id); err != nil {
		return value, err
	}
	// Cancellation stops future notifications. Existing revisioned mail remains
	// valid evidence and can be handled through the ordinary mail operations.
	return scanStateSubscription(tx.QueryRowContext(ctx, stateSubscriptionSelect+" WHERE id=?", id))
}

func notifyState(ctx context.Context, tx *sql.Tx, value session.StateValue) error {
	rows, err := tx.QueryContext(ctx, stateSubscriptionSelect+" WHERE tree_id=? AND key=? AND cancelled_at IS NULL AND cursor<? ORDER BY id", value.TreeID, value.Key, value.Revision)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var subscriptions []session.StateSubscription
	for rows.Next() {
		item, err := scanStateSubscription(rows)
		if err != nil {
			return err
		}
		subscriptions = append(subscriptions, item)
	}
	scanErr := rows.Err()
	closeErr := rows.Close()
	if err := errors.Join(scanErr, closeErr); err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if err := notifyStateSubscription(ctx, tx, subscription, value); err != nil {
			return err
		}
	}
	return nil
}

func notifyStateSubscription(ctx context.Context, tx *sql.Tx, subscription session.StateSubscription, value session.StateValue) error {
	if subscription.SessionID != value.AuthorID {
		if err := stateNotification(ctx, tx, subscription, value); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE state_subscriptions SET cursor=? WHERE id=?", value.Revision, subscription.ID)
	return err
}

func stateNotification(ctx context.Context, tx *sql.Tx, subscription session.StateSubscription, value session.StateValue) error {
	body, err := json.Marshal(session.StateChange{VersionID: value.ID, Key: value.Key, Revision: value.Revision, AuthorID: value.AuthorID})
	if err != nil {
		return err
	}
	source := session.MailSource{Kind: "state", ID: subscription.ID}
	current, err := scanMail(tx.QueryRowContext(ctx, mailSelect+" WHERE m.source_kind='state' AND m.source_id=? AND m.recipient_id=? AND m.deleted_at IS NULL AND m.state='pending' AND r.revision=m.revision ORDER BY m.created_at DESC LIMIT 1", subscription.ID, subscription.SessionID))
	spec := session.MailSpec{RecipientID: subscription.SessionID, Delivery: subscription.Delivery, Subject: subscription.Key, Body: string(body)}
	if err == nil {
		spec.ID = current.ID
		spec.AvailableAt = &current.AvailableAt
		_, err = replaceMail(ctx, tx, spec, current)
		return err
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	spec.ID = session.MailID(fmt.Sprintf("mail_%x", sha256.Sum256([]byte(subscription.ID+"\x00"+value.ID))))
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := mailCapacity(ctx, tx, source, spec.RecipientID, spec.ID, true); err != nil {
		return err
	}
	digest, err := requestDigest("state_notification", spec)
	if err != nil {
		return err
	}
	created := now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO mail VALUES (?,?,?,?,?,1,'pending',?,NULL)", spec.ID, source.Kind, source.ID, spec.RecipientID, digest, created); err != nil {
		return err
	}
	return insertMailRevision(ctx, tx, spec, 1, created)
}
