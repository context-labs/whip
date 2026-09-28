package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
)

const provisionalTitleRunes = 64

// TitleInitialization is returned only by the admission that committed the
// initial title. Prompt contains authored text, never expanded attachments.
type TitleInitialization struct {
	Title  string
	Prompt string
}

func provisionalTitleText(text string) string {
	title := strings.Join(strings.Fields(text), " ")
	runes := []rune(title)
	if len(runes) > provisionalTitleRunes {
		return string(runes[:provisionalTitleRunes-1]) + "…"
	}
	return title
}

// ProvisionalTitle derives the initial session title from the first message the
// user authored. The returned title is whitespace-normalized and at most 64
// runes, including the ellipsis.
func ProvisionalTitle(messages []llm.Message) string {
	for _, message := range messages {
		if message.Role != "user" || !message.Authored {
			continue
		}
		return provisionalTitleText(message.TextContent())
	}
	return ""
}

// authoredInputText reads only the submitted text fields. Attachment references,
// image data, and design context are deliberately not naming context.
func authoredInputText(item InboxEnqueue) (string, error) {
	if item.Kind != "submit.parts" && item.Kind != "steer.parts" {
		return string(item.Payload.Data), nil
	}
	var body struct {
		Text  string            `json:"text"`
		Parts []llm.ContentPart `json:"parts"`
	}
	if err := json.Unmarshal(item.Payload.Data, &body); err != nil {
		return "", fmt.Errorf("%w: invalid content-parts submission: %w", ErrInvalidInput, err)
	}
	text := []string{body.Text}
	for _, part := range body.Parts {
		if part.Type == "text" {
			text = append(text, part.Text)
		}
	}
	return strings.TrimSpace(strings.Join(text, "\n")), nil
}

func initializeInputTitleTx(ctx context.Context, tx *sql.Tx, item InboxEnqueue) (*TitleInitialization, error) {
	if item.RootID != item.AgentID || item.Origin != "client" || clientInputOrigin(item.Kind) == "" {
		return nil, nil
	}
	prompt, err := authoredInputText(item)
	if err != nil {
		return nil, err
	}
	title := provisionalTitleText(prompt)
	if title == "" {
		return nil, nil
	}
	var current, forkedFrom string
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT title,forked_from,history_revision
		FROM sessions WHERE id=?`, item.RootID).Scan(&current, &forkedFrom, &revision); err != nil {
		return nil, err
	}
	if current != "" || forkedFrom != "" || revision != 0 {
		return nil, nil
	}
	var firstTurn sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT CASE WHEN json_valid(content) THEN COALESCE(json_extract(content,'$.presentation.turn_id'),'') ELSE '' END
		 FROM messages WHERE session_id=? ORDER BY seq LIMIT 1)`, item.RootID).Scan(&firstTurn); err != nil {
		return nil, err
	}
	if firstTurn.Valid && firstTurn.String == "" {
		return nil, nil
	}
	// Retained inbox metadata proves that previous human input had no usable
	// text. Unknown legacy previews are not evidence of an attachment-only start.
	rows, err := tx.QueryContext(ctx, `SELECT seq,origin,preview FROM inbox
		WHERE root_id=? AND agent_id=? AND kind IN ('submit','submit.parts','steer','steer.parts')
		AND origin<>'internal' ORDER BY seq`, item.RootID, item.AgentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	historyLinked := !firstTurn.Valid
	for rows.Next() {
		var seq int64
		var origin string
		var data []byte
		if err := rows.Scan(&seq, &origin, &data); err != nil {
			return nil, err
		}
		preview := readInboxPreview(data)
		// A legacy preview truncated in whitespace may hide text after its
		// 2048-byte boundary; the UTF-8 truncator can drop up to three bytes.
		if origin != "client" || preview == nil || strings.TrimSpace(preview.Text) != "" ||
			preview.Truncated && len(preview.Text) >= 2048-utf8.UTFMax+1 {
			return nil, nil
		}
		if firstTurn.String == rootTurnID(item.AgentID, seq) {
			historyLinked = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// A history without a known no-text admission is historical, not a new
	// conversation. Reading only its first row also avoids expanded file text.
	if !historyLinked {
		return nil, nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET title=? WHERE id=? AND title=''`, title, item.RootID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return nil, err
	}
	return &TitleInitialization{Title: title, Prompt: prompt}, nil
}
