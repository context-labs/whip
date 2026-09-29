package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

const (
	maxRecallInputBytes = 1 << 20
	maxRecallScanBytes  = 4 << 20
)

// RecentInputText scans a bounded ordinal window across all sessions. Limiting
// before filtering bounds even a long interval containing only injected work.
// The cursor advances through omitted/empty inputs; callers must not infer EOF
// from an empty item list while next_cursor is present.
func (s *Store) RecentInputText(ctx context.Context, before int64, limit int) (session.InputTextPage, error) {
	page := session.InputTextPage{Items: []session.InputText{}}
	if before < 0 || limit < 1 || limit > 500 {
		return page, session.ErrInvalid
	}
	query := `SELECT session_id,id,ordinal,
 source='user' AND kind='prompt',
 CASE WHEN source='user' AND kind='prompt' THEN length(CAST(parts AS BLOB)) ELSE 0 END,
 CASE WHEN source='user' AND kind='prompt' AND length(CAST(parts AS BLOB))<=? THEN parts END
 FROM inputs`
	args := []any{maxRecallInputBytes}
	if before != 0 {
		query += " WHERE ordinal<?"
		args = append(args, before)
	}
	query += " ORDER BY ordinal DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	var last int64
	var readBytes, textBytes int
	for rows.Next() {
		var item session.InputText
		var eligible bool
		var size int64
		var raw []byte
		if err := rows.Scan(&item.SessionID, &item.InputID, &item.Ordinal, &eligible, &size, &raw); err != nil {
			return page, err
		}
		if page.ScannedCount == limit || readBytes+len(raw) > maxRecallScanBytes {
			page.NextCursor = new(last)
			break
		}
		readBytes += len(raw)
		if eligible && size <= maxRecallInputBytes {
			var parts []session.Part
			if err := json.Unmarshal(raw, &parts); err != nil {
				return page, err
			}
			// Match the retained editor's text projection. Original multipart
			// redraft remains an exact-owner history operation, not this query.
			for _, part := range parts {
				if part.Type == "text" {
					item.Text = part.Text
					break
				}
			}
			if len(item.Text) > session.MaxInputRecallBytes {
				page.SkippedCount++
			} else if strings.TrimSpace(item.Text) != "" {
				if textBytes+len(item.Text) > session.MaxInputRecallBytes {
					page.NextCursor = new(last)
					break
				}
				textBytes += len(item.Text)
				page.Items = append(page.Items, item)
			}
		} else if eligible {
			page.SkippedCount++
		}
		last = item.Ordinal
		page.ScannedCount++
	}
	return page, rows.Err()
}
