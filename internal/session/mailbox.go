package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Delivery classes decide when a message enters the recipient's model context.
const (
	MessageDeliverySteer    = "steer"     // next loop boundary of a running turn; starts a turn when idle
	MessageDeliveryQueued   = "queued"    // its own turn once the recipient is idle
	MessageDeliveryNextTurn = "next_turn" // rides along with whatever turn starts next
)

// Message kinds. Agent-authored messages are "message"; the runtime posts the
// others so child lifecycle and blackboard changes share one canonical table.
const (
	MessageKindMessage        = "message"
	MessageKindAgentCompleted = "agent.completed"
	MessageKindAgentFailed    = "agent.failed"
	MessageKindAgentCancelled = "agent.cancelled"
	MessageKindStateChanged   = "state.changed"
)

const (
	// MailboxExcerptBytes bounds the body excerpt carried by digests and
	// boundary injections; the full body stays behind messages.read.
	MailboxExcerptBytes = 2 << 10
	// MailboxDigestLines caps the per-message lines in a synthesized digest.
	MailboxDigestLines = 20
	maxMailboxSubject  = 256
)

// MailboxMessage is the canonical durable record. Status moves
// pending -> delivered -> done; delivered is set only when the turn that
// showed the message commits, so a crashed turn redelivers it.
type MailboxMessage struct {
	ID                  string       `json:"id"`
	SenderAgentID       string       `json:"sender"`
	RecipientAgentID    string       `json:"recipient"`
	Kind                string       `json:"kind"`
	Delivery            string       `json:"delivery"`
	Subject             string       `json:"subject,omitempty"`
	Excerpt             string       `json:"excerpt,omitempty"`
	Body                RuntimeValue `json:"body,omitempty"`
	EvidenceReferenceID string       `json:"evidence_handle,omitempty"`
	Status              string       `json:"status"`
	AvailableAt         time.Time    `json:"available_at,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	DeliveredAt         time.Time    `json:"delivered_at,omitempty"`
	DeliveredTurnID     string       `json:"delivered_turn_id,omitempty"`
	DoneAt              time.Time    `json:"done_at,omitempty"`
}

// MailboxSend describes one message to store. A zero AvailableAt means now.
// UpsertKey replaces a still-pending message with the same recipient, kind,
// and key instead of inserting one more (blackboard subscriptions use it so a
// hot key cannot flood a mailbox).
type MailboxSend struct {
	Kind                string
	Delivery            string
	Subject             string
	Body                string
	EvidenceReferenceID string
	AvailableAt         time.Time
	UpsertKey           string
}

type MailboxSummary struct {
	UnreadCount int      `json:"unread_count"`
	Senders     []string `json:"senders,omitempty"`
	OldestAt    string   `json:"oldest_at,omitempty"`
	NewestAt    string   `json:"newest_at,omitempty"`
}

// MailboxDigest is the bounded view a turn receives about the recipient's
// mailbox: ready pending messages (oldest first, capped), counts, and the
// sender metadata needed to render one line per message.
type MailboxDigest struct {
	Pending        []MailboxMessage
	PendingTotal   int
	DeliveredOpen  int
	NextDeferredAt time.Time
	SenderNames    map[string]string
	Relationships  map[string]string // sender id -> parent|child|sibling
}

// AgentWork is the derived readiness of one node. It is computed from
// canonical rows so a lost in-memory wake is harmless.
type AgentWork struct {
	HasExplicitInput bool
	HasReadyMail     bool
	NextDeferredAt   time.Time
}

func validMessageDelivery(delivery string) bool {
	return delivery == MessageDeliverySteer || delivery == MessageDeliveryQueued || delivery == MessageDeliveryNextTurn
}

func utf8Prefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func timestampArg(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func parseTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

// Sender-side caps keep one chatty node from flooding a relative's mailbox:
// a body above MaxMailboxBody belongs in artifacts.put with an evidence
// handle, a sender may have at most MaxPendingPerPair pending messages to one
// recipient, and at most MaxMessagesPerWindow sends per MessageRateWindow.
// Upserted runtime messages replace a pending row and are exempt.
const (
	MaxMailboxBody       = 16 << 10
	MaxPendingPerPair    = 20
	MaxMessagesPerWindow = 30
	MessageRateWindow    = 10 * time.Second
)

var (
	ErrMailboxBacklog     = fmt.Errorf("recipient already has %d pending messages from this sender; wait for it to complete some", MaxPendingPerPair)
	ErrMailboxRateLimited = fmt.Errorf("message rate limit exceeded (%d per %s)", MaxMessagesPerWindow, MessageRateWindow)
)

// SendMailboxMessage stores one agent-authored message for a direct relative.
// The row itself is the durable wake condition; no separate notification is
// written.
func (s *Store) SendMailboxMessage(ctx context.Context, rootID, senderAgentID, recipientAgentID string, send MailboxSend) (MailboxMessage, error) {
	if rootID == "" || senderAgentID == "" || recipientAgentID == "" || senderAgentID == recipientAgentID {
		return MailboxMessage{}, ErrAgentAccess
	}
	if send.Kind == "" {
		send.Kind = MessageKindMessage
	}
	if send.Delivery == "" {
		send.Delivery = MessageDeliveryQueued
	}
	if !validMessageDelivery(send.Delivery) {
		return MailboxMessage{}, fmt.Errorf("invalid message delivery %q", send.Delivery)
	}
	if strings.TrimSpace(send.Body) == "" && send.EvidenceReferenceID == "" {
		return MailboxMessage{}, errors.New("message requires a body or evidence handle")
	}
	if len(send.Subject) > maxMailboxSubject {
		return MailboxMessage{}, fmt.Errorf("message subject exceeds %d bytes", maxMailboxSubject)
	}
	if len(send.Body) > MaxMailboxBody {
		return MailboxMessage{}, fmt.Errorf("message body exceeds %d bytes; store it with artifacts.put and send the evidence handle", MaxMailboxBody)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MailboxMessage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateDirectRelativeTx(ctx, tx, rootID, senderAgentID, recipientAgentID); err != nil {
		return MailboxMessage{}, err
	}
	if send.UpsertKey == "" {
		var pending, recent int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_messages WHERE root_id=? AND sender_agent_id=? AND recipient_agent_id=? AND status='pending'`,
			rootID, senderAgentID, recipientAgentID).Scan(&pending); err != nil {
			return MailboxMessage{}, err
		}
		if pending >= MaxPendingPerPair {
			return MailboxMessage{}, ErrMailboxBacklog
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_messages WHERE root_id=? AND sender_agent_id=? AND created_at>=?`,
			rootID, senderAgentID, timestampArg(time.Now().Add(-MessageRateWindow))).Scan(&recent); err != nil {
			return MailboxMessage{}, err
		}
		if recent >= MaxMessagesPerWindow {
			return MailboxMessage{}, ErrMailboxRateLimited
		}
	}
	message, err := s.insertMailboxMessageTx(ctx, tx, rootID, senderAgentID, recipientAgentID, send, now())
	if err != nil {
		return MailboxMessage{}, err
	}
	if err := tx.Commit(); err != nil {
		return MailboxMessage{}, err
	}
	return message, nil
}

// insertMailboxMessageTx writes (or upserts) one message row and its event.
// Callers validate reach; runtime-authored kinds skip the relative check.
func (s *Store) insertMailboxMessageTx(ctx context.Context, tx *sql.Tx, rootID, senderAgentID, recipientAgentID string, send MailboxSend, stamp string) (MailboxMessage, error) {
	if send.EvidenceReferenceID != "" {
		if err := promoteMessageEvidence(ctx, tx, rootID, senderAgentID, recipientAgentID, send.EvidenceReferenceID, stamp); err != nil {
			return MailboxMessage{}, err
		}
	}
	prepared, err := s.prepareRuntimeValue(RuntimePayload{
		Data: []byte(send.Body), MediaType: "text/plain", Source: "agent message body",
	}, ContentGrant{RootID: rootID, AgentID: recipientAgentID, Scope: ContentGrantAgent})
	if err != nil {
		return MailboxMessage{}, err
	}
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return MailboxMessage{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	excerpt := utf8Prefix(send.Body, MailboxExcerptBytes)
	availableAt := timestampArg(send.AvailableAt)
	message := MailboxMessage{
		SenderAgentID: senderAgentID, RecipientAgentID: recipientAgentID, Kind: send.Kind, Delivery: send.Delivery,
		Subject: send.Subject, Excerpt: excerpt, EvidenceReferenceID: send.EvidenceReferenceID, Status: "pending",
		AvailableAt: send.AvailableAt, CreatedAt: parseTimestamp(stamp),
	}
	if send.UpsertKey != "" {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND upsert_key=? AND status='pending'`,
			rootID, recipientAgentID, send.UpsertKey).Scan(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return MailboxMessage{}, err
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `UPDATE agent_messages SET sender_agent_id=?,kind=?,delivery=?,subject=?,excerpt=?,body_inline=?,body_ref=?,evidence_ref=?,available_at=?,created_at=? WHERE id=?`,
				senderAgentID, send.Kind, send.Delivery, send.Subject, excerpt, inline, reference, nullableString(send.EvidenceReferenceID), availableAt, stamp, existing); err != nil {
				return MailboxMessage{}, err
			}
			message.ID = existing
			_, err = s.insertActorEventTx(ctx, tx, rootID, "message.updated", actorEvent{
				AgentID: recipientAgentID, SenderAgentID: senderAgentID, MessageID: existing, Delivery: send.Delivery, Status: "pending",
			}, stamp)
			return message, err
		}
	}
	id, err := runtimeID()
	if err != nil {
		return MailboxMessage{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_messages
		(id,root_id,sender_agent_id,recipient_agent_id,kind,delivery,upsert_key,subject,excerpt,body_inline,body_ref,evidence_ref,status,available_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'pending',?,?)`,
		id, rootID, senderAgentID, recipientAgentID, send.Kind, send.Delivery, send.UpsertKey, send.Subject, excerpt,
		inline, reference, nullableString(send.EvidenceReferenceID), availableAt, stamp); err != nil {
		return MailboxMessage{}, err
	}
	message.ID = id
	_, err = s.insertActorEventTx(ctx, tx, rootID, "message.queued", actorEvent{
		AgentID: recipientAgentID, SenderAgentID: senderAgentID, MessageID: id, Delivery: send.Delivery, Status: "pending",
	}, stamp)
	return message, err
}

func promoteMessageEvidence(ctx context.Context, tx *sql.Tx, rootID, senderAgentID, recipientAgentID, referenceID, stamp string) error {
	authorized, err := contentAuthorizedTx(ctx, tx, referenceID, rootID, senderAgentID)
	if err != nil {
		return err
	}
	if !authorized {
		return ErrContentAccess
	}
	recipientAuthorized, err := contentAuthorizedTx(ctx, tx, referenceID, rootID, recipientAgentID)
	if err != nil || recipientAuthorized {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO content_grants(reference_id,root_id,agent_id,scope,created_at) VALUES(?,?,?,'agent',?)
		ON CONFLICT(reference_id,root_id,agent_id,scope) DO UPDATE SET revoked_at='',created_at=excluded.created_at`,
		referenceID, rootID, recipientAgentID, stamp)
	return err
}

func normalizeMessageStatus(status string) (string, error) {
	switch status {
	case "", "pending", "unread":
		return "pending", nil
	case "delivered":
		return "delivered", nil
	case "done", "acked":
		return "done", nil
	case "all":
		return "all", nil
	}
	return "", fmt.Errorf("invalid message status %q", status)
}

const mailboxColumns = `m.id,m.sender_agent_id,m.recipient_agent_id,m.kind,m.delivery,m.subject,m.excerpt,COALESCE(m.evidence_ref,''),
	m.status,m.available_at,m.created_at,m.delivered_at,m.delivered_turn_id,m.done_at,COALESCE(LENGTH(m.body_inline),0),COALESCE(m.body_ref,''),COALESCE(r.size,0)`

func scanMailboxRow(rows interface {
	Scan(dest ...any) error
}, message *MailboxMessage) error {
	var availableAt, createdAt, deliveredAt, doneAt string
	var inlineSize, referenceSize int64
	var reference string
	if err := rows.Scan(&message.ID, &message.SenderAgentID, &message.RecipientAgentID, &message.Kind, &message.Delivery, &message.Subject,
		&message.Excerpt, &message.EvidenceReferenceID, &message.Status, &availableAt, &createdAt, &deliveredAt, &message.DeliveredTurnID, &doneAt,
		&inlineSize, &reference, &referenceSize); err != nil {
		return err
	}
	message.AvailableAt, message.CreatedAt = parseTimestamp(availableAt), parseTimestamp(createdAt)
	message.DeliveredAt, message.DoneAt = parseTimestamp(deliveredAt), parseTimestamp(doneAt)
	message.Body = RuntimeValue{Size: inlineSize}
	if reference != "" {
		message.Body.ReferenceID, message.Body.Size = reference, referenceSize
	}
	return nil
}

// ListMailboxMessages lists metadata only; bodies stay behind ReadMailboxMessage.
func (s *Store) ListMailboxMessages(ctx context.Context, rootID, recipientAgentID, status, sender string, limit int) ([]MailboxMessage, error) {
	if rootID == "" || recipientAgentID == "" || limit < 1 || limit > 100 {
		return nil, errors.New("message list requires an agent and limit from 1 to 100")
	}
	status, err := normalizeMessageStatus(status)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + mailboxColumns + ` FROM agent_messages m LEFT JOIN content_references r ON r.id=m.body_ref
		WHERE m.root_id=? AND m.recipient_agent_id=?`
	args := []any{rootID, recipientAgentID}
	if status != "all" {
		query += ` AND m.status=?`
		args = append(args, status)
	}
	if sender != "" {
		query += ` AND m.sender_agent_id=?`
		args = append(args, sender)
	}
	query += ` ORDER BY m.created_at,m.rowid LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []MailboxMessage
	for rows.Next() {
		var message MailboxMessage
		if err := scanMailboxRow(rows, &message); err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, rows.Err()
}

// ReadMailboxMessage returns one message with its body. Marking it delivered
// is the caller's job at turn commit.
func (s *Store) ReadMailboxMessage(ctx context.Context, rootID, recipientAgentID, id string) (MailboxMessage, error) {
	if rootID == "" || recipientAgentID == "" || id == "" {
		return MailboxMessage{}, ErrAgentAccess
	}
	var message MailboxMessage
	row := s.db.QueryRowContext(ctx, `SELECT `+mailboxColumns+` FROM agent_messages m LEFT JOIN content_references r ON r.id=m.body_ref
		WHERE m.id=? AND m.root_id=? AND m.recipient_agent_id=?`, id, rootID, recipientAgentID)
	if err := scanMailboxRow(row, &message); errors.Is(err, sql.ErrNoRows) {
		return MailboxMessage{}, ErrAgentAccess
	} else if err != nil {
		return MailboxMessage{}, err
	}
	if message.Body.ReferenceID == "" {
		var inline []byte
		if err := s.db.QueryRowContext(ctx, `SELECT body_inline FROM agent_messages WHERE id=?`, id).Scan(&inline); err != nil {
			return MailboxMessage{}, err
		}
		message.Body.Inline = inline
	}
	return message, nil
}

// CompleteMailboxMessages terminalizes messages the recipient has handled.
func (s *Store) CompleteMailboxMessages(ctx context.Context, rootID, recipientAgentID string, ids []string) (int64, error) {
	if rootID == "" || recipientAgentID == "" || len(ids) == 0 || len(ids) > 100 {
		return 0, errors.New("message completion requires 1 to 100 ids")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	seen := make(map[string]struct{}, len(ids))
	var changed int64
	for _, id := range ids {
		if id == "" {
			return 0, errors.New("message id is required")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result, err := tx.ExecContext(ctx, `UPDATE agent_messages SET status='done',done_at=?
			WHERE id=? AND root_id=? AND recipient_agent_id=? AND status IN ('pending','delivered')`, stamp, id, rootID, recipientAgentID)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if count == 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_messages WHERE id=? AND root_id=? AND recipient_agent_id=?`, id, rootID, recipientAgentID).Scan(&exists); err != nil {
				return 0, err
			}
			if exists == 0 {
				return 0, ErrAgentAccess
			}
			continue
		}
		changed += count
		if _, err := s.insertActorEventTx(ctx, tx, rootID, "message.done", actorEvent{AgentID: recipientAgentID, MessageID: id, Status: "done"}, stamp); err != nil {
			return 0, err
		}
	}
	return changed, tx.Commit()
}

// DeferMailboxMessage returns a message to pending with a future
// availability, which is the only way a message wakes its recipient twice.
func (s *Store) DeferMailboxMessage(ctx context.Context, rootID, recipientAgentID, id string, until time.Time) error {
	if rootID == "" || recipientAgentID == "" || id == "" {
		return ErrAgentAccess
	}
	if until.IsZero() {
		return errors.New("message deferral requires a time")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	result, err := tx.ExecContext(ctx, `UPDATE agent_messages SET status='pending',available_at=?,delivered_at='',delivered_turn_id=''
		WHERE id=? AND root_id=? AND recipient_agent_id=? AND status IN ('pending','delivered')`, timestampArg(until), id, rootID, recipientAgentID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrAgentAccess
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "message.deferred", actorEvent{
		AgentID: recipientAgentID, MessageID: id, Status: "pending", Slot: timestampArg(until),
	}, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

// markMessagesDeliveredTx records that a committed turn showed these messages.
func (s *Store) markMessagesDeliveredTx(ctx context.Context, tx *sql.Tx, rootID, recipientAgentID, turnID string, ids []string, stamp string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result, err := tx.ExecContext(ctx, `UPDATE agent_messages SET status='delivered',delivered_at=?,delivered_turn_id=?
			WHERE id=? AND root_id=? AND recipient_agent_id=? AND status='pending'`, stamp, turnID, id, rootID, recipientAgentID)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return err
			}
			continue // already delivered, done, or deferred again: nothing to record
		}
		if _, err := s.insertActorEventTx(ctx, tx, rootID, "message.delivered", actorEvent{
			AgentID: recipientAgentID, MessageID: id, Status: "delivered",
		}, stamp); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) MailboxSummary(ctx context.Context, rootID, recipientAgentID string) (MailboxSummary, error) {
	var summary MailboxSummary
	var oldest, newest sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT count(*),MIN(created_at),MAX(created_at) FROM agent_messages
		WHERE root_id=? AND recipient_agent_id=? AND status='pending'`, rootID, recipientAgentID).Scan(&summary.UnreadCount, &oldest, &newest); err != nil {
		return MailboxSummary{}, err
	}
	if oldest.Valid {
		summary.OldestAt = oldest.String
	}
	if newest.Valid {
		summary.NewestAt = newest.String
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT sender_agent_id FROM agent_messages
		WHERE root_id=? AND recipient_agent_id=? AND status='pending' ORDER BY sender_agent_id LIMIT 20`, rootID, recipientAgentID)
	if err != nil {
		return MailboxSummary{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sender string
		if err := rows.Scan(&sender); err != nil {
			return MailboxSummary{}, err
		}
		summary.Senders = append(summary.Senders, sender)
	}
	sort.Strings(summary.Senders)
	return summary, rows.Err()
}

// AgentWorkStatus derives readiness from canonical rows.
func (s *Store) AgentWorkStatus(ctx context.Context, rootID, agentID string, at time.Time) (AgentWork, error) {
	if rootID == "" || agentID == "" {
		return AgentWork{}, ErrAgentAccess
	}
	stamp := timestampArg(at)
	var work AgentWork
	if err := s.db.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM inbox WHERE root_id=? AND agent_id=? AND status='queued'),
		EXISTS(SELECT 1 FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND status='pending' AND available_at<=? AND delivery<>'next_turn')`,
		rootID, agentID, rootID, agentID, stamp).Scan(&work.HasExplicitInput, &work.HasReadyMail); err != nil {
		return AgentWork{}, err
	}
	var deferred string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(available_at),'') FROM agent_messages
		WHERE root_id=? AND recipient_agent_id=? AND status='pending' AND available_at>? AND delivery<>'next_turn'`, rootID, agentID, stamp).Scan(&deferred); err != nil {
		return AgentWork{}, err
	}
	work.NextDeferredAt = parseTimestamp(deferred)
	return work, nil
}

// PendingSteers returns steer-class work for a running turn's boundary hook:
// queued human steer rows and ready steer-class messages. Nothing is marked;
// the turn journal excludes items it already injected and the commit
// consumes them.
func (s *Store) PendingSteers(ctx context.Context, rootID, agentID string, at time.Time) ([]InboxItem, []MailboxMessage, error) {
	if rootID == "" || agentID == "" {
		return nil, nil, ErrAgentAccess
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.seq,i.kind,i.status,substr(i.payload_inline,1,?),COALESCE(i.payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
		FROM inbox i LEFT JOIN content_references r ON r.id=i.payload_ref
		WHERE i.root_id=? AND i.agent_id=? AND i.status='queued' AND i.kind IN ('steer','steer.parts') ORDER BY i.seq LIMIT ?`,
		InlineValueLimit+1, rootID, agentID, MaxInboxBatch)
	if err != nil {
		return nil, nil, err
	}
	items, err := scanInboxRows(rows, rootID, agentID)
	closeErr := rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	mailRows, err := s.db.QueryContext(ctx, `SELECT `+mailboxColumns+` FROM agent_messages m LEFT JOIN content_references r ON r.id=m.body_ref
		WHERE m.root_id=? AND m.recipient_agent_id=? AND m.status='pending' AND m.delivery=? AND m.available_at<=? ORDER BY m.created_at,m.rowid LIMIT ?`,
		rootID, agentID, MessageDeliverySteer, timestampArg(at), MailboxDigestLines)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = mailRows.Close() }()
	var mail []MailboxMessage
	for mailRows.Next() {
		var message MailboxMessage
		if err := scanMailboxRow(mailRows, &message); err != nil {
			return nil, nil, err
		}
		mail = append(mail, message)
	}
	return items, mail, mailRows.Err()
}

// ReadMailboxDigest assembles the bounded mailbox view for a turn.
func (s *Store) ReadMailboxDigest(ctx context.Context, rootID, recipientAgentID string, at time.Time) (MailboxDigest, error) {
	if rootID == "" || recipientAgentID == "" {
		return MailboxDigest{}, ErrAgentAccess
	}
	stamp := timestampArg(at)
	digest := MailboxDigest{SenderNames: make(map[string]string), Relationships: make(map[string]string)}
	var recipientParent string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(parent_id,'') FROM agents WHERE root_id=? AND id=?`, rootID, recipientAgentID).Scan(&recipientParent); err != nil {
		return MailboxDigest{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+mailboxColumns+`,COALESCE(a.name,''),COALESCE(a.parent_id,'')
		FROM agent_messages m LEFT JOIN content_references r ON r.id=m.body_ref
		LEFT JOIN agents a ON a.root_id=m.root_id AND a.id=m.sender_agent_id
		WHERE m.root_id=? AND m.recipient_agent_id=? AND m.status='pending' AND m.available_at<=? ORDER BY m.created_at,m.rowid LIMIT ?`,
		rootID, recipientAgentID, stamp, MailboxDigestLines)
	if err != nil {
		return MailboxDigest{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var message MailboxMessage
		var senderName, senderParent string
		var availableAt, createdAt, deliveredAt, doneAt string
		var inlineSize, referenceSize int64
		var reference string
		if err := rows.Scan(&message.ID, &message.SenderAgentID, &message.RecipientAgentID, &message.Kind, &message.Delivery, &message.Subject,
			&message.Excerpt, &message.EvidenceReferenceID, &message.Status, &availableAt, &createdAt, &deliveredAt, &message.DeliveredTurnID, &doneAt,
			&inlineSize, &reference, &referenceSize, &senderName, &senderParent); err != nil {
			return MailboxDigest{}, err
		}
		message.AvailableAt, message.CreatedAt = parseTimestamp(availableAt), parseTimestamp(createdAt)
		message.Body = RuntimeValue{Size: inlineSize}
		if reference != "" {
			message.Body.ReferenceID, message.Body.Size = reference, referenceSize
		}
		digest.Pending = append(digest.Pending, message)
		digest.SenderNames[message.SenderAgentID] = senderName
		switch {
		case message.SenderAgentID == recipientParent:
			digest.Relationships[message.SenderAgentID] = "parent"
		case senderParent == recipientAgentID:
			digest.Relationships[message.SenderAgentID] = "child"
		default:
			digest.Relationships[message.SenderAgentID] = "sibling"
		}
	}
	if err := rows.Err(); err != nil {
		return MailboxDigest{}, err
	}
	var deferred string
	if err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND status='pending' AND available_at<=?),
		(SELECT count(*) FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND status='delivered'),
		COALESCE((SELECT MIN(available_at) FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND status='pending' AND available_at>?),'')`,
		rootID, recipientAgentID, stamp, rootID, recipientAgentID, rootID, recipientAgentID, stamp).Scan(&digest.PendingTotal, &digest.DeliveredOpen, &deferred); err != nil {
		return MailboxDigest{}, err
	}
	digest.NextDeferredAt = parseTimestamp(deferred)
	return digest, nil
}
