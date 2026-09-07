package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type MailboxCursor struct {
	RootID   string `json:"root_id"`
	AgentID  string `json:"agent_id"`
	Status   string `json:"status"`
	Revision int64  `json:"revision,string"`
	Offset   int64  `json:"offset,string"`
}

// MailboxInspection is a human read projection, with decimal wire counters.
// Inspecting it never participates in agent receipt or delivery bookkeeping.
type MailboxInspection struct {
	ID                  string       `json:"id"`
	Revision            int64        `json:"revision,string"`
	SenderAgentID       string       `json:"sender"`
	RecipientAgentID    string       `json:"recipient"`
	Kind                string       `json:"kind"`
	Delivery            string       `json:"delivery"`
	Subject             string       `json:"subject"`
	Excerpt             string       `json:"excerpt"`
	Body                RuntimeValue `json:"body"`
	EvidenceReferenceID string       `json:"evidence_handle,omitempty"`
	Status              string       `json:"status"`
	AvailableAt         time.Time    `json:"available_at"`
	CreatedAt           time.Time    `json:"created_at"`
	DeliveredAt         time.Time    `json:"delivered_at"`
	DeliveredTurnID     string       `json:"delivered_turn_id,omitempty"`
	DoneAt              time.Time    `json:"done_at"`
}

type MailboxPage struct {
	Revision   int64               `json:"revision,string"`
	Items      []MailboxInspection `json:"items"`
	NextCursor *MailboxCursor      `json:"next_cursor,omitempty"`
	HasMore    bool                `json:"has_more"`
}

func inspectMailbox(message MailboxMessage) MailboxInspection {
	return MailboxInspection{
		ID: message.ID, Revision: message.Revision, SenderAgentID: message.SenderAgentID,
		RecipientAgentID: message.RecipientAgentID, Kind: message.Kind, Delivery: message.Delivery,
		Subject: message.Subject, Excerpt: message.Excerpt, Body: message.Body, EvidenceReferenceID: message.EvidenceReferenceID,
		Status: message.Status, AvailableAt: message.AvailableAt, CreatedAt: message.CreatedAt,
		DeliveredAt: message.DeliveredAt, DeliveredTurnID: message.DeliveredTurnID, DoneAt: message.DoneAt,
	}
}

func (s *Store) InspectMailboxPage(ctx context.Context, rootID, agentID, status string, cursor *MailboxCursor, limit, maxBytes int) (MailboxPage, error) {
	page := MailboxPage{Items: []MailboxInspection{}}
	if rootID == "" || agentID == "" || limit < 1 || limit > 128 || maxBytes < 4096 || maxBytes > 512<<10 {
		return page, errors.New("mailbox requires root, agent, limit 1..128 and max_bytes 4096..524288")
	}
	if status == "" {
		status = "all"
	}
	if status != "all" && status != "pending" && status != "delivered" && status != "done" {
		return page, errors.New("invalid mailbox status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	// A newly created root can be inspected before its first actor opens and
	// materializes authority. Child identities must already belong to that root.
	if err := tx.QueryRowContext(ctx, `SELECT s.collection_revision FROM sessions s WHERE s.id=? AND (?=s.id OR EXISTS(SELECT 1 FROM agents a WHERE a.root_id=s.id AND a.id=?))`, rootID, agentID, agentID).Scan(&page.Revision); errors.Is(err, sql.ErrNoRows) {
		return page, ErrAgentAccess
	} else if err != nil {
		return page, err
	}
	var offset int64
	if cursor != nil {
		if cursor.RootID != rootID || cursor.AgentID != agentID || cursor.Status != status || cursor.Revision != page.Revision || cursor.Offset < 0 {
			return page, ErrCollectionChanged
		}
		offset = cursor.Offset
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+mailboxColumns+`,COALESCE(r.digest,''),COALESCE(r.media_type,'text/plain'),COALESCE(r.source,'agent mailbox')
 FROM agent_messages m LEFT JOIN content_references r ON r.id=m.body_ref
 WHERE m.root_id=? AND m.recipient_agent_id=? AND (?='all' OR m.status=?) ORDER BY m.created_at,m.rowid LIMIT ? OFFSET ?`, rootID, agentID, status, status, limit+1, offset)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		var message MailboxMessage
		var digest, mediaType, source string
		if err := scanMailboxRow(rows, &message, &digest, &mediaType, &source); err != nil {
			_ = rows.Close()
			return page, err
		}
		message.Body.Digest, message.Body.MediaType, message.Body.Source = digest, mediaType, source
		page.Items = append(page.Items, inspectMailbox(message))
		page.NextCursor = &MailboxCursor{RootID: rootID, AgentID: agentID, Status: status, Revision: page.Revision, Offset: offset + int64(len(page.Items))}
		raw, err := json.Marshal(page)
		if err != nil {
			_ = rows.Close()
			return page, err
		}
		if len(raw) > maxBytes {
			page.Items = page.Items[:len(page.Items)-1]
			page.HasMore = true
			break
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return page, err
	}
	if page.HasMore && len(page.Items) == 0 {
		return page, errors.New("mailbox metadata exceeds presentation budget")
	}
	if page.HasMore {
		page.NextCursor.Offset = offset + int64(len(page.Items))
	} else {
		page.NextCursor = nil
	}
	return page, tx.Commit()
}

// InspectMailboxMessage returns a bounded inline body or its existing content
// reference. Root/recipient association is checked by the canonical mail read.
func (s *Store) InspectMailboxMessage(ctx context.Context, rootID, agentID, id string) (MailboxInspection, error) {
	if rootID == "" || agentID == "" || id == "" {
		return MailboxInspection{}, ErrAgentAccess
	}
	var message MailboxMessage
	var inline []byte
	var digest, mediaType, source string
	row := s.db.QueryRowContext(ctx, `SELECT `+mailboxColumns+`,substr(m.body_inline,1,?),COALESCE(r.digest,''),COALESCE(r.media_type,'text/plain'),COALESCE(r.source,'agent mailbox')
 FROM agent_messages m LEFT JOIN content_references r ON r.id=m.body_ref WHERE m.id=? AND m.root_id=? AND m.recipient_agent_id=?`, InlineValueLimit+1, id, rootID, agentID)
	if err := scanMailboxRow(row, &message, &inline, &digest, &mediaType, &source); errors.Is(err, sql.ErrNoRows) {
		return MailboxInspection{}, ErrAgentAccess
	} else if err != nil {
		return MailboxInspection{}, err
	}
	if len(inline) > InlineValueLimit {
		return MailboxInspection{}, errors.New("mailbox inline body exceeds storage bound")
	}
	message.Body.Digest, message.Body.MediaType, message.Body.Source = digest, mediaType, source
	if message.Body.ReferenceID == "" {
		message.Body.Inline = inline
	}
	return inspectMailbox(message), nil
}
