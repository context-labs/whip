package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func validateOperationAttachments(ctx context.Context, q querier, owner session.SessionID, ids []string) (int64, error) {
	if len(ids) > session.MaxOperationAttachments {
		return 0, fmt.Errorf("%w: cell image count exceeds limit", session.ErrInvalid)
	}
	seen := map[string]bool{}
	var total int64
	for _, id := range ids {
		if seen[id] || session.ValidateID(id) != nil {
			return 0, fmt.Errorf("%w: invalid or duplicate host attachment", session.ErrInvalid)
		}
		seen[id] = true
		ref, err := readContent(ctx, q, owner, id)
		if err != nil {
			return 0, fmt.Errorf("%w: host attachment is not owned by the operation session", session.ErrInvalid)
		}
		switch ref.MediaType {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
		default:
			return 0, fmt.Errorf("%w: host attachment must be a supported image", session.ErrInvalid)
		}
		total += ref.Size
		if total > session.MaxOperationAttachmentBytes {
			return 0, fmt.Errorf("%w: cell image bytes exceed limit", session.ErrInvalid)
		}
	}
	return total, nil
}

// Only typed metadata in committed operation outcomes can become model content.
// This query does not examine a JSON value, guest output or kernel checkpoint.
func cellOperationAttachments(ctx context.Context, q querier, cell session.CellID, owner session.SessionID) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT result FROM operations WHERE cell_id=? AND finished_at IS NOT NULL ORDER BY created_at,id", cell)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var outcome session.OperationResult
		if json.Unmarshal([]byte(raw), &outcome) != nil {
			return nil, fmt.Errorf("%w: invalid committed operation result", ErrConflict)
		}
		ids = append(ids, outcome.ContentReferences...)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err := validateOperationAttachments(ctx, q, owner, ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// CellAttachmentAllowance is a bounded observation, not a reservation. Concrete
// host handlers must serialize image-producing operations for the same live
// kernel through settlement; the store also checks the limit atomically.
func (s *Store) CellAttachmentAllowance(ctx context.Context, cell session.CellID) (int, int64, error) {
	value, err := s.Cell(ctx, cell)
	if err != nil {
		return 0, 0, err
	}
	ids, err := cellOperationAttachments(ctx, s.db, cell, value.SessionID)
	if err != nil {
		return 0, 0, err
	}
	size, err := validateOperationAttachments(ctx, s.db, value.SessionID, ids)
	return session.MaxOperationAttachments - len(ids), session.MaxOperationAttachmentBytes - size, err
}

// CellResultParts returns the canonical committed result, never a worker's
// presentation cache. A caller cannot read another session's screenshot refs.
func (s *Store) CellResultParts(ctx context.Context, owner session.SessionID, id session.CellID) ([]session.Part, error) {
	cell, err := s.Cell(ctx, id)
	if err != nil {
		return nil, err
	}
	if cell.SessionID != owner {
		return nil, ErrNotFound
	}
	if cell.ResultMessageID == nil {
		return nil, ErrBusy
	}
	message, err := scanMessage(s.db.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", *cell.ResultMessageID))
	if err != nil {
		return nil, err
	}
	if message.SessionID != owner || message.Role != session.Tool || len(message.Parts) == 0 || message.Parts[0].Result == nil || message.Parts[0].Result.CallID != cell.CallID {
		return nil, ErrConflict
	}
	return message.Parts, nil
}
