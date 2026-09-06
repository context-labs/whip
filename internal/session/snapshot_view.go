package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/context-labs/whip/internal/llm"
)

type SnapshotViewOptions struct {
	RecentMessages  int
	CollectionLimit int
	MaxBytes        int
}

// SnapshotRootView captures bounded presentation data and its durable cursor
// in one transaction. Omitted collections require explicit paging; missing
// stream prefixes are reported rather than presented as complete output.
func (s *Store) SnapshotRootView(ctx context.Context, rootID string, opts SnapshotViewOptions) (RootSnapshot, error) {
	if opts.RecentMessages < 1 || opts.RecentMessages > 128 || opts.CollectionLimit < 1 || opts.CollectionLimit > 128 || opts.MaxBytes < 4096 || opts.MaxBytes > 512*1024 {
		return RootSnapshot{}, errors.New("snapshot requires recent_messages and collection_limit 1..128, max_bytes 4096..524288")
	}
	return s.snapshotRoot(ctx, rootID, &opts)
}

func (s *RootSnapshot) collectionLimit() int {
	if s.view == nil {
		return -1
	}
	return s.view.CollectionLimit + 1
}

func readSnapshotRecentMessages(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	budget := snapshot.view.MaxBytes / 2
	rows, err := tx.QueryContext(ctx, `SELECT seq,length(CAST(content AS BLOB)),substr(CAST(content AS BLOB),1,?) FROM messages WHERE session_id=? ORDER BY seq DESC LIMIT ?`, budget, rootID, snapshot.view.RecentMessages+1)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var seq, size int
		var body []byte
		if err := rows.Scan(&seq, &size, &body); err != nil {
			return err
		}
		if len(snapshot.Messages) == snapshot.view.RecentMessages || size > budget {
			snapshot.Omitted["messages"] = true
			break
		}
		var message llm.Message
		if err := json.Unmarshal(body, &message); err != nil {
			return err
		}
		if message.Role == "" {
			return errors.New("snapshot message has no role")
		}
		message.RawSequence = seq
		snapshot.Messages = append(snapshot.Messages, message)
		snapshot.FirstMessageSeq = seq
		budget -= size
	}
	slices.Reverse(snapshot.Messages)
	return rows.Err()
}

func (s *Store) readBoundedPresentation(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	// Read a bounded suffix. Any missing prefix is explicitly unavailable; the
	// durable transcript remains independently pageable.
	limit := snapshot.view.CollectionLimit
	rows, err := tx.QueryContext(ctx, `SELECT seq,kind,substr(payload_inline,1,?),length(payload_inline),COALESCE(payload_ref,'') FROM (SELECT seq,kind,payload_inline,payload_ref FROM events WHERE root_id=? ORDER BY seq DESC LIMIT ?) ORDER BY seq`, snapshot.view.MaxBytes/4, rootID, limit+1)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	snapshot.AgentPresentations = map[string][]SnapshotEvent{}
	snapshot.Omitted["presentation_prefix"] = false
	remaining := snapshot.view.MaxBytes / 4
	first := true
	for rows.Next() {
		var item SnapshotEvent
		var payload []byte
		var size sql.NullInt64
		var ref string
		if err := rows.Scan(&item.Seq, &item.Kind, &payload, &size, &ref); err != nil {
			return err
		}
		item.Payload = payload
		if first {
			snapshot.Omitted["presentation_prefix"] = item.Seq > 1
			first = false
		}
		switch item.Kind {
		case "turn.started", "turn.succeeded", "turn.failed", "turn.cancelled", "turn.interrupted":
			snapshot.Presentation = nil
			continue
		}
		stream := len(item.Kind) >= 7 && item.Kind[:7] == "stream."
		agentLifecycle := len(item.Kind) >= 11 && item.Kind[:11] == "agent.turn."
		if !stream && !agentLifecycle {
			continue
		}
		if ref != "" || size.Int64 > int64(snapshot.view.MaxBytes/4) || (stream && size.Int64 > int64(remaining)) {
			snapshot.Omitted["presentation"] = true
			continue
		}
		if stream {
			remaining -= len(item.Payload)
		}
		var owner struct {
			AgentID string `json:"agent_id"`
		}
		if len(item.Payload) > 0 && json.Unmarshal(item.Payload, &owner) != nil {
			return errors.New("invalid presentation event payload")
		}
		switch item.Kind {
		case "turn.started", "turn.succeeded", "turn.failed", "turn.cancelled", "turn.interrupted":
			snapshot.Presentation = nil
		case "agent.turn.started", "agent.turn.succeeded", "agent.turn.failed", "agent.turn.cancelled", "agent.turn.interrupted":
			delete(snapshot.AgentPresentations, owner.AgentID)
		default:
			if len(item.Kind) < 7 || item.Kind[:7] != "stream." {
				continue
			}
			if owner.AgentID == "" || owner.AgentID == rootID {
				snapshot.Presentation = append(snapshot.Presentation, item)
			} else {
				snapshot.AgentPresentations[owner.AgentID] = append(snapshot.AgentPresentations[owner.AgentID], item)
			}
		}
	}
	for _, agent := range snapshot.Agents {
		if agent.ParentID != "" && agent.LifecyclePhase != "running" {
			delete(snapshot.AgentPresentations, agent.ID)
		}
	}
	if !snapshot.Omitted["presentation_prefix"] {
		delete(snapshot.Omitted, "presentation_prefix")
	}
	return rows.Err()
}

func boundSlice[T any](items *[]T, limit int, key string, omitted map[string]bool) {
	if len(*items) > limit {
		*items = (*items)[:limit]
		omitted[key] = true
	}
}

func boundSnapshot(snapshot *RootSnapshot) error {
	limit := snapshot.view.CollectionLimit
	boundSlice(&snapshot.Agents, limit, "agents", snapshot.Omitted)
	boundSlice(&snapshot.Inbox, limit, "inbox", snapshot.Omitted)
	boundSlice(&snapshot.Blackboard, limit, "blackboard", snapshot.Omitted)
	boundSlice(&snapshot.Budgets, limit, "budgets", snapshot.Omitted)
	boundSlice(&snapshot.Capabilities, limit, "capabilities", snapshot.Omitted)
	boundSlice(&snapshot.Schedules, limit, "schedules", snapshot.Omitted)
	boundSlice(&snapshot.Permissions, limit, "permissions", snapshot.Omitted)
	// Prefer retaining questions/permissions and the active agent catalog when a
	// large individual collection exhausts the aggregate response budget.
	for {
		snapshot.MessageSeqs = snapshotMessageSeqs(snapshot.Messages)
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if len(encoded) <= snapshot.view.MaxBytes {
			return nil
		}
		switch {
		case len(snapshot.Messages) > 0:
			snapshot.Messages = snapshot.Messages[1:]
			if len(snapshot.Messages) > 0 {
				snapshot.FirstMessageSeq = snapshot.Messages[0].RawSequence
			} else {
				snapshot.FirstMessageSeq = 0
			}
			snapshot.Omitted["messages"] = true
		case len(snapshot.Presentation) > 0 || len(snapshot.AgentPresentations) > 0:
			snapshot.Presentation = nil
			snapshot.AgentPresentations = nil
			snapshot.Omitted["presentation"] = true
		case len(snapshot.Blackboard) > 0:
			snapshot.Blackboard = nil
			snapshot.Omitted["blackboard"] = true
		case len(snapshot.Inbox) > 0:
			snapshot.Inbox = nil
			snapshot.Omitted["inbox"] = true
		case len(snapshot.Capabilities) > 0:
			snapshot.Capabilities = nil
			snapshot.Omitted["capabilities"] = true
		case len(snapshot.Schedules) > 0:
			snapshot.Schedules = nil
			snapshot.Omitted["schedules"] = true
		case len(snapshot.Budgets) > 0:
			snapshot.Budgets = nil
			snapshot.Omitted["budgets"] = true
		case len(snapshot.Agents) > 0:
			snapshot.Agents = snapshot.Agents[:len(snapshot.Agents)-1]
			snapshot.Omitted["agents"] = true
		case len(snapshot.Permissions) > 0:
			snapshot.Permissions = snapshot.Permissions[:len(snapshot.Permissions)-1]
			snapshot.Omitted["permissions"] = true
		default:
			return errors.New("snapshot metadata exceeds max_bytes")
		}
	}
}

func snapshotMessageSeqs(messages []llm.Message) []int {
	sequences := make([]int, len(messages))
	for i, message := range messages {
		sequences[i] = message.RawSequence
	}
	return sequences
}
