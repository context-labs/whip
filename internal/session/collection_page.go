package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

var ErrCollectionChanged = errors.New("collection changed; resynchronize its first page")

type CollectionCursor struct {
	RootID     string `json:"root_id"`
	Collection string `json:"collection"`
	Revision   int64  `json:"revision,string"`
	Offset     int64  `json:"offset,string"`
}

type CollectionPageOptions struct {
	Cursor   *CollectionCursor
	Limit    int
	MaxBytes int
}

// CollectionEntry contains exactly one typed item, or a content reference to
// that same JSON item when its complete body exceeds the presentation budget.
type CollectionEntry struct {
	Agent      *RuntimeAgent       `json:"agent,omitempty"`
	Inbox      *InboxItem          `json:"inbox,omitempty"`
	Blackboard *StateValue         `json:"blackboard,omitempty"`
	Budget     *SnapshotBudget     `json:"budget,omitempty"`
	Capability *CapabilityRecord   `json:"capability,omitempty"`
	Schedule   *Schedule           `json:"schedule,omitempty"`
	Permission *PermissionSnapshot `json:"permission,omitempty"`
	Body       *RuntimeValue       `json:"body,omitempty"`
}

type RootCollectionPage struct {
	RootID      string            `json:"root_id"`
	Collection  string            `json:"collection"`
	Revision    int64             `json:"revision,string"`
	EventCursor int64             `json:"event_cursor,string"`
	Items       []CollectionEntry `json:"items"`
	NextCursor  *CollectionCursor `json:"next_cursor,omitempty"`
	HasMore     bool              `json:"has_more"`
}

// RootCollectionPage reads only a bounded set of stable row keys and their
// values. A schema-maintained revision detects updates/deletions between pages,
// including collection mutations that have not yet emitted a command event.
func (s *Store) RootCollectionPage(ctx context.Context, rootID, collection string, opts CollectionPageOptions) (RootCollectionPage, error) {
	if rootID == "" || opts.Limit < 1 || opts.Limit > 128 || opts.MaxBytes < 4096 || opts.MaxBytes > 512<<10 {
		return RootCollectionPage{}, errors.New("collection requires a root, limit 1..128, and max_bytes 4096..524288")
	}
	query, ok := collectionKeyQueries[collection]
	if !ok {
		return RootCollectionPage{}, errors.New("unsupported root collection")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RootCollectionPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	page := RootCollectionPage{RootID: rootID, Collection: collection, Items: []CollectionEntry{}}
	if err := tx.QueryRowContext(ctx, `SELECT collection_revision,COALESCE((SELECT MAX(seq) FROM events WHERE root_id=sessions.id),0) FROM sessions WHERE id=?`, rootID).Scan(&page.Revision, &page.EventCursor); err != nil {
		return page, err
	}
	var offset int64
	if cursor := opts.Cursor; cursor != nil {
		if cursor.RootID != rootID || cursor.Collection != collection || cursor.Offset < 0 || cursor.Revision != page.Revision {
			return page, ErrCollectionChanged
		}
		offset = cursor.Offset
	}
	rows, err := tx.QueryContext(ctx, query, rootID, opts.Limit+1, offset)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	keys := []int64{}
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			return page, err
		}
		keys = append(keys, key)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return page, err
	}
	if len(keys) == 0 && offset > 0 {
		return page, ErrCollectionChanged
	}
	for index, key := range keys {
		if index == opts.Limit {
			page.HasMore = true
			break
		}
		entry, err := s.readCollectionEntry(ctx, tx, rootID, collection, key)
		if err != nil {
			return page, err
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return page, err
		}
		if len(raw) > opts.MaxBytes-1024 {
			if len(page.Items) > 0 {
				page.HasMore = true
				break
			}
			value, err := s.prepareContentReference(RuntimePayload{Data: raw, MediaType: "application/json", Source: "root.collection." + collection}, ContentGrant{RootID: rootID, Scope: ContentGrantRoot})
			if err != nil {
				return page, err
			}
			var existing string
			err = tx.QueryRowContext(ctx, `SELECT r.id FROM content_references r JOIN content_grants g ON g.reference_id=r.id WHERE r.digest=? AND r.source=? AND g.root_id=? AND g.agent_id='' AND g.scope='root' AND g.revoked_at='' LIMIT 1`, value.Digest, value.Source, rootID).Scan(&existing)
			if err == nil {
				value.ReferenceID = existing
			} else if errors.Is(err, sql.ErrNoRows) {
				if err := insertRuntimeValue(ctx, tx, value, now()); err != nil {
					return page, err
				}
			} else {
				return page, err
			}
			entry = CollectionEntry{Body: &value.RuntimeValue}
		}
		page.Items = append(page.Items, entry)
		page.NextCursor = &CollectionCursor{RootID: rootID, Collection: collection, Revision: page.Revision, Offset: offset + int64(len(page.Items))}
		encoded, err := json.Marshal(page)
		if err != nil {
			return page, err
		}
		if len(encoded) > opts.MaxBytes {
			page.Items = page.Items[:len(page.Items)-1]
			page.HasMore = true
			break
		}
	}
	if page.HasMore {
		page.NextCursor = &CollectionCursor{RootID: rootID, Collection: collection, Revision: page.Revision, Offset: offset + int64(len(page.Items))}
	} else {
		page.NextCursor = nil
	}
	if page.HasMore && len(page.Items) == 0 {
		return page, errors.New("collection item exceeds the minimum presentation budget")
	}
	return page, tx.Commit()
}

// Queries are constants: collection names never enter SQL. rowid is stable
// within the revision and avoids offset drift after deletes or inserts.
var collectionKeyQueries = map[string]string{
	"agents":       `SELECT rowid FROM agents WHERE root_id=? ORDER BY rowid LIMIT ? OFFSET ?`,
	"inbox":        `SELECT rowid FROM inbox WHERE root_id=? AND status IN ('queued','running') ORDER BY rowid LIMIT ? OFFSET ?`,
	"blackboard":   `SELECT rowid FROM blackboard WHERE root_id=? ORDER BY rowid LIMIT ? OFFSET ?`,
	"budgets":      `SELECT rowid FROM budgets WHERE root_id=? ORDER BY rowid LIMIT ? OFFSET ?`,
	"capabilities": `SELECT rowid FROM capabilities WHERE root_id=? ORDER BY rowid LIMIT ? OFFSET ?`,
	"schedules":    `SELECT rowid FROM schedules WHERE session_id=? ORDER BY rowid LIMIT ? OFFSET ?`,
	"permissions":  `SELECT rowid FROM permission_requests WHERE root_id=? AND status='pending' ORDER BY rowid LIMIT ? OFFSET ?`,
}

func (s *Store) readCollectionEntry(ctx context.Context, tx *sql.Tx, rootID, collection string, key int64) (CollectionEntry, error) {
	switch collection {
	case "agents":
		var id string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE root_id=? AND rowid=?`, rootID, key).Scan(&id); err != nil {
			return CollectionEntry{}, err
		}
		value, err := loadAgentTx(ctx, tx, rootID, id)
		if err != nil {
			return CollectionEntry{}, err
		}
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND status='pending'`, rootID, id).Scan(&value.PendingMail)
		return CollectionEntry{Agent: &value}, err
	case "capabilities":
		var id string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM capabilities WHERE root_id=? AND rowid=?`, rootID, key).Scan(&id); err != nil {
			return CollectionEntry{}, err
		}
		value, err := loadCapabilityRecordTx(ctx, tx, rootID, id)
		return CollectionEntry{Capability: &value}, err
	case "blackboard":
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT key FROM blackboard WHERE root_id=? AND rowid=?`, rootID, key).Scan(&name); err != nil {
			return CollectionEntry{}, err
		}
		value, err := loadBlackboardTx(ctx, tx, rootID, name)
		return CollectionEntry{Blackboard: &value}, err
	case "budgets":
		var value SnapshotBudget
		var row budgetRow
		err := tx.QueryRowContext(ctx, `SELECT agent_id,kind,limit_value,used_value,reserved_value,uncertain_value,incomplete,model_incomplete FROM budgets WHERE root_id=? AND rowid=?`, rootID, key).Scan(&value.AgentID, &row.kind, &row.limit, &row.used, &row.reserved, &row.uncertain, &row.incomplete, &row.modelIncomplete)
		if err != nil {
			return CollectionEntry{}, err
		}
		if _, valid := budgetRemaining(row); !valid {
			return CollectionEntry{}, capability.ErrDenied
		}
		value.State = budgetStateFromRow(row)
		return CollectionEntry{Budget: &value}, nil
	case "inbox":
		value := InboxItem{RootID: rootID}
		err := tx.QueryRowContext(ctx, `SELECT seq,agent_id,kind,status,substr(payload_inline,1,?),COALESCE(payload_ref,''),COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
   FROM inbox i LEFT JOIN content_references r ON r.id=i.payload_ref WHERE i.root_id=? AND i.rowid=?`, InlineValueLimit+1, rootID, key).Scan(&value.Seq, &value.AgentID, &value.Kind, &value.Status, &value.Payload.Inline, &value.Payload.ReferenceID, &value.Payload.Digest, &value.Payload.Size, &value.Payload.MediaType, &value.Payload.Source)
		if value.Payload.ReferenceID != "" {
			value.Payload.Inline = nil
		} else if len(value.Payload.Inline) > InlineValueLimit {
			return CollectionEntry{}, errors.New("oversized inline inbox value")
		}
		return CollectionEntry{Inbox: &value}, err
	case "schedules":
		var value Schedule
		var anchor, last string
		err := tx.QueryRowContext(ctx, `SELECT id,schedule,prompt,anchor,last_fire FROM schedules WHERE session_id=? AND rowid=?`, rootID, key).Scan(&value.ID, &value.Schedule, &value.Prompt, &anchor, &last)
		if err != nil {
			return CollectionEntry{}, err
		}
		value.Anchor, err = time.Parse(time.RFC3339, anchor)
		if err != nil {
			return CollectionEntry{}, err
		}
		if last != "" {
			value.LastFire, err = time.Parse(time.RFC3339, last)
		}
		return CollectionEntry{Schedule: &value}, err
	case "permissions":
		return s.readCollectionPermission(ctx, tx, rootID, key)
	default:
		return CollectionEntry{}, fmt.Errorf("unsupported collection %q", collection)
	}
}

func (s *Store) readCollectionPermission(ctx context.Context, tx *sql.Tx, rootID string, key int64) (CollectionEntry, error) {
	var value PermissionSnapshot
	var inline []byte
	var reference sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.agent_id,p.operation_id,p.status,o.payload_inline,o.payload_ref
  FROM permission_requests p JOIN operations o ON o.root_id=p.root_id AND o.id=p.operation_id WHERE p.root_id=? AND p.rowid=?`, rootID, key).Scan(&value.ID, &value.AgentID, &value.OperationID, &value.Status, &inline, &reference)
	if err != nil {
		return CollectionEntry{}, err
	}
	raw, err := s.readRuntimeValueTx(ctx, tx, inline, reference)
	if err != nil {
		return CollectionEntry{}, err
	}
	var admission capability.Admission
	if err := json.Unmarshal(raw, &admission); err != nil {
		return CollectionEntry{}, err
	}
	value.Operation = admission.Request.Operation
	value.CanonicalPath = admission.CanonicalPath
	value.RequestDigest = admission.RequestDigest
	value.CapabilityID = admission.Request.CapabilityID
	value.CapabilityGeneration = admission.Request.CapabilityGeneration
	command, rules, _ := capability.PermissionRule(admission.Request.Operation, admission.Request.Arguments, admission.CanonicalPath)
	value.Command, value.Rule = command, capability.RuleLabel(rules)
	return CollectionEntry{Permission: &value}, nil
}
