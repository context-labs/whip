package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// TurnOutput projects a successful turn's final assistant message using the
// configuration captured when that turn began. Reading never executes work.
func (s *Store) TurnOutput(ctx context.Context, id session.TurnID) (*session.StructuredOutput, error) {
	return turnOutput(ctx, s.db, id)
}

//nolint:nilnil // A successful turn without a configured output contract has no structured output.
func turnOutput(ctx context.Context, q querier, id session.TurnID) (*session.StructuredOutput, error) {
	var state session.TurnState
	var kind session.InputKind
	var configuration string
	var messageID, rawParts sql.NullString
	err := q.QueryRowContext(ctx, `SELECT t.state,COALESCE(i.kind,'prompt'),c.configuration,m.id,m.parts
		FROM turns t
		LEFT JOIN inputs i ON i.turn_id=t.id
		JOIN session_configurations c ON c.session_id=t.session_id AND c.revision=t.config_revision
		LEFT JOIN messages m ON m.id=(SELECT id FROM messages WHERE turn_id=t.id AND role='assistant' ORDER BY sequence DESC LIMIT 1)
		WHERE t.id=?`, id).Scan(&state, &kind, &configuration, &messageID, &rawParts)
	if err != nil {
		return nil, found(err)
	}
	if !state.Terminal() {
		return nil, ErrBusy
	}
	if state != session.Succeeded || kind == session.CompactInput {
		return nil, nil
	}
	var config session.Configuration
	if err := json.Unmarshal([]byte(configuration), &config); err != nil {
		return nil, fmt.Errorf("decode turn configuration: %w", err)
	}
	var parts []session.Part
	if rawParts.Valid {
		if err := json.Unmarshal([]byte(rawParts.String), &parts); err != nil {
			return nil, fmt.Errorf("decode final assistant message: %w", err)
		}
	}
	value, err := session.ValidateOutput(config.OutputSchema, parts)
	if err != nil {
		return nil, fmt.Errorf("validate stored turn output: %w", err)
	}
	if value == nil {
		return nil, nil
	}
	return &session.StructuredOutput{TurnID: id, MessageID: session.MessageID(messageID.String), Value: value}, nil
}
