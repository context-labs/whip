package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
)

// InboxPreview is display-only. The original payload remains the model input.
// Previews are bounded before storage and count toward snapshot/page budgets.
type InboxPreview struct {
	Text            string            `json:"text"`
	Truncated       bool              `json:"truncated,omitempty"`
	Attachments     []InboxAttachment `json:"attachments,omitempty"`
	AttachmentCount int               `json:"attachment_count,omitempty"`
}

type InboxAttachment struct {
	Kind    string       `json:"kind"`
	Name    string       `json:"name,omitempty"`
	Content RuntimeValue `json:"content"`
}

func clientInputOrigin(kind string) string {
	switch kind {
	case "submit", "submit.parts", "steer", "steer.parts":
		return "client"
	}
	return ""
}

func inboxPreviewJSON(item InboxEnqueue) []byte {
	if item.Origin != "client" {
		return []byte(`{}`)
	}
	preview := InboxPreview{Text: string(item.Payload.Data)}
	if strings.HasSuffix(item.Kind, ".parts") {
		var body struct {
			Text        string            `json:"text"`
			Parts       []llm.ContentPart `json:"parts"`
			Attachments []InboxAttachment `json:"attachments"`
		}
		if json.Unmarshal(item.Payload.Data, &body) != nil {
			return []byte(`{}`)
		}
		preview.Text = body.Text
		preview.AttachmentCount = len(body.Attachments)
		for _, part := range body.Parts {
			if part.Type == "text" {
				preview.Text += part.Text
			}
			if part.Type == "image_url" {
				preview.AttachmentCount++
			}
		}
		for _, file := range body.Attachments[:min(len(body.Attachments), 16)] {
			file.Name = boundedUTF8(file.Name, 256)
			file.Content.Inline = nil
			file.Content.Source = ""
			preview.Attachments = append(preview.Attachments, file)
		}
	}
	preview.Truncated = len(preview.Text) > 2048 || preview.AttachmentCount > len(preview.Attachments)
	preview.Text = boundedUTF8(preview.Text, 2048)
	body, _ := json.Marshal(preview)
	// Unusual host-supplied metadata must not make the preview unbounded.
	if len(body) > 16<<10 {
		preview.Attachments = nil
		preview.Truncated = true
		body, _ = json.Marshal(preview)
	}
	return body
}

func readInboxPreview(body []byte) *InboxPreview {
	if len(body) == 0 || string(body) == "{}" {
		return nil
	}
	var value InboxPreview
	if json.Unmarshal(body, &value) != nil {
		return nil
	}
	return &value
}

type InboxControlResult struct {
	AgentID  string `json:"agent_id"`
	InboxSeq int64  `json:"inbox_seq,string"`
	Status   string `json:"status"`
}

// ControlInbox only changes waiting client input. It never cancels a turn or
// recreates a submission. The caller's control command owns retry/recovery.
func (s *Store) ControlInbox(ctx context.Context, rootID, agentID string, seq int64, turnID string, remove bool, cancelledOutcome []byte) (InboxControlResult, error) {
	result := InboxControlResult{AgentID: agentID, InboxSeq: seq}
	if rootID == "" || agentID == "" || seq <= 0 || !remove && turnID == "" {
		return result, errors.New("queue control requires an exact input and steering turn")
	}
	if len(cancelledOutcome) > InlineValueLimit {
		return result, errors.New("queue outcome exceeds inline limit")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	var status, origin, clientID, commandID string
	err = tx.QueryRowContext(ctx, `SELECT status,origin,command_client_id,command_id FROM inbox WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, seq).Scan(&status, &origin, &clientID, &commandID)
	if err != nil {
		return result, err
	}
	if origin != "client" {
		return result, errors.New("queue controls require client-authored input")
	}
	if status != "queued" {
		result.Status = "already_started"
		if status == "cancelled" {
			result.Status = "already_removed"
		}
		return result, nil
	}
	stamp := now()
	if remove {
		if _, err = tx.ExecContext(ctx, `UPDATE inbox SET status='cancelled',steer_turn_id='' WHERE root_id=? AND agent_id=? AND seq=? AND status='queued'`, rootID, agentID, seq); err != nil {
			return result, err
		}
		// Child admission commands are already complete; only root input
		// commands live for the duration of model execution.
		if agentID == rootID && commandID != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE commands SET status='cancelled',outcome_inline=?,outcome_ref=NULL,updated_at=? WHERE root_id=? AND scope='root' AND ingress_seq=? AND client_id=? AND command_id=? AND status='queued'`, cancelledOutcome, stamp, rootID, seq, clientID, commandID); err != nil {
				return result, err
			}
		}
		result.Status = "removed"
	} else {
		var active string
		err = tx.QueryRowContext(ctx, `SELECT id FROM turns WHERE root_id=? AND agent_id=? AND status='running'`, rootID, agentID).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) || err == nil && active != turnID {
			result.Status = "turn_ended"
			return result, nil
		}
		if err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE inbox SET steer_turn_id=? WHERE root_id=? AND agent_id=? AND seq=? AND status='queued'`, turnID, rootID, agentID, seq); err != nil {
			return result, err
		}
		result.Status = "steering"
	}
	if _, err = s.insertActorEventTx(ctx, tx, rootID, "inbox."+result.Status, actorEvent{AgentID: agentID, InboxSeq: seq, TurnID: turnID, Status: result.Status, CommandClientID: clientID, CommandID: commandID}, stamp); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func boundedUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit]
}
