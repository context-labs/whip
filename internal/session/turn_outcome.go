package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const turnErrorPreviewBytes = 4 << 10

// TurnOutcome describes the latest turn independently of the retained agent's
// lifecycle. An idle agent may have failed its last turn and still accept work.
type TurnOutcome struct {
	TurnID         string        `json:"turn_id,omitempty"`
	Status         string        `json:"status"`
	StartedAt      string        `json:"started_at,omitempty"`
	FinishedAt     string        `json:"finished_at,omitempty"`
	EventSeq       int64         `json:"event_seq,string"`
	Error          string        `json:"error,omitempty"`
	ErrorTruncated bool          `json:"error_truncated,omitempty"`
	ErrorDetails   *RuntimeValue `json:"error_details,omitempty"`
}

// Scan decodes the nullable JSON projection using database/sql's pointer scan.
func (outcome *TurnOutcome) Scan(value any) error {
	switch data := value.(type) {
	case []byte:
		return json.Unmarshal(data, outcome)
	case string:
		return json.Unmarshal([]byte(data), outcome)
	default:
		return fmt.Errorf("invalid turn outcome storage %T", value)
	}
}

type runtimeValueWriter interface {
	commandQueryer
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func turnEventStatus(kind string) string {
	if !strings.HasPrefix(kind, "turn.") && !strings.HasPrefix(kind, "agent.turn.") {
		return ""
	}
	switch status := kind[strings.LastIndexByte(kind, '.')+1:]; status {
	case "started":
		return "running"
	case "succeeded", "failed", "cancelled", "interrupted":
		return status
	default:
		return ""
	}
}

func (s *Store) projectTurnEvent(ctx context.Context, q runtimeValueWriter, rootID, kind string, payload []byte, seq int64, stamp string) error {
	status := turnEventStatus(kind)
	if status == "" || len(payload) == 0 {
		return nil
	}
	var event LifecycleEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode turn outcome: %w", err)
	}
	agentID := event.AgentID
	if agentID == "" {
		if strings.HasPrefix(kind, "agent.") {
			return nil
		}
		agentID = rootID
	}
	var prior *TurnOutcome
	err := q.QueryRowContext(ctx, `SELECT last_turn FROM agents WHERE root_id=? AND id=?`, rootID, agentID).Scan(&prior)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if prior != nil && seq <= prior.EventSeq {
		return nil
	}
	outcome := TurnOutcome{TurnID: event.TurnID, Status: status, EventSeq: seq}
	if status == "running" {
		outcome.StartedAt = stamp
	} else {
		// Older child completion events omitted turn_id. Only pair with an
		// observed running turn; never infer a turn from timestamps alone.
		if prior != nil {
			if prior.TurnID != "" && event.TurnID != "" && event.TurnID != prior.TurnID {
				return nil
			}
			if prior.Status == "running" && outcome.TurnID == "" {
				outcome.TurnID = prior.TurnID
			}
			if outcome.TurnID != "" && outcome.TurnID == prior.TurnID {
				outcome.StartedAt = prior.StartedAt
			}
		}
		outcome.FinishedAt = stamp
		outcome.Error = event.Error
		if len(event.Error) > turnErrorPreviewBytes {
			end := turnErrorPreviewBytes
			for !utf8.ValidString(event.Error[:end]) {
				end--
			}
			outcome.Error = event.Error[:end]
			outcome.ErrorTruncated = true
			details, err := s.prepareContentReference(RuntimePayload{Data: []byte(event.Error), MediaType: "text/plain", Source: "turn failure"}, ContentGrant{RootID: rootID, Scope: ContentGrantRoot})
			if err != nil {
				return err
			}
			if err := insertRuntimeValue(ctx, q, details, stamp); err != nil {
				return err
			}
			outcome.ErrorDetails = &details.RuntimeValue
		}
	}
	data, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE agents SET last_turn=? WHERE root_id=? AND id=?`, data, rootID, agentID)
	return err
}

func clearRootTurnOutcome(ctx context.Context, q runtimeValueWriter, rootID string) error {
	_, err := q.ExecContext(ctx, `UPDATE agents SET last_turn=NULL WHERE root_id=? AND id=?`, rootID, rootID)
	return err
}
