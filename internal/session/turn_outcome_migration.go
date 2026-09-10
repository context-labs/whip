package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	contentstore "github.com/context-labs/whip/internal/content"
)

func upgradeV11(ctx context.Context, conn *sql.Conn, path string) error {
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`) }()
	var version int
	var identity string
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil {
		return err
	}
	if version == 14 && identity == "whip-recursive-runtime-v14" ||
		version == currentSchemaVersion && identity == schemaIdentity ||
		version == 13 && identity == "whip-recursive-runtime-v13" ||
		version == 12 && identity == "whip-recursive-runtime-v12" {
		return nil
	}
	if version != 11 || identity != "whip-recursive-runtime-v11" {
		return fmt.Errorf("turn outcome upgrade requires version 11, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE agents ADD COLUMN last_turn BLOB`); err != nil {
		return err
	}
	content, err := contentstore.New(filepath.Dir(path))
	if err != nil {
		return err
	}
	store := &Store{content: content}
	if err := store.backfillTurnOutcomes(ctx, conn); err != nil {
		return fmt.Errorf("backfill turn outcomes: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE runtime_schema SET identity='whip-recursive-runtime-v12' WHERE id=1; PRAGMA user_version=12`); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	return err
}

// Backfill visits one retained lifecycle event at a time, without holding an
// open cursor during writes. Missing/pruned evidence stays unknown. Legacy root
// history edits have no reliable event boundary, so do not resurrect their old
// outcomes; child history is independent and can still be recovered.
func (s *Store) backfillTurnOutcomes(ctx context.Context, conn *sql.Conn) error {
	var root string
	var seq int64
	for {
		var kind, stamp, digest string
		var inline []byte
		var size, revision int64
		err := conn.QueryRowContext(ctx, `SELECT e.root_id,e.seq,e.kind,e.created_at,substr(e.payload_inline,1,8388609),
			COALESCE(r.digest,''),COALESCE(r.size,0),s.history_revision
			FROM events e JOIN sessions s ON s.id=e.root_id LEFT JOIN content_references r ON r.id=e.payload_ref
			WHERE (e.root_id,e.seq)>(?,?) AND e.kind IN (
			'turn.started','turn.succeeded','turn.failed','turn.cancelled','turn.interrupted',
			'agent.turn.started','agent.turn.succeeded','agent.turn.failed','agent.turn.cancelled','agent.turn.interrupted')
			ORDER BY e.root_id,e.seq LIMIT 1`, root, seq).Scan(&root, &seq, &kind, &stamp, &inline, &digest, &size, &revision)
		if errors.Is(err, sql.ErrNoRows) {
			// Legacy subtree stops did not emit per-turn terminal events. A
			// missing error body must not leave a durably finished turn running.
			_, err := conn.ExecContext(ctx, `UPDATE agents SET last_turn=json_set(last_turn,
				'$.status',(SELECT status FROM turns WHERE id=json_extract(agents.last_turn,'$.turn_id')),
				'$.finished_at',(SELECT updated_at FROM turns WHERE id=json_extract(agents.last_turn,'$.turn_id')))
				WHERE json_extract(last_turn,'$.status')='running' AND EXISTS(
				SELECT 1 FROM turns WHERE id=json_extract(agents.last_turn,'$.turn_id')
				AND root_id=agents.root_id AND agent_id=agents.id AND status IN ('succeeded','failed','cancelled','interrupted'))`)
			return err
		}
		if err != nil {
			return err
		}
		if revision > 0 && !strings.HasPrefix(kind, "agent.") {
			continue
		}
		// Bound migration memory even for externally authored legacy events.
		// Provider error bodies are much smaller; unavailable evidence is never
		// replaced by a guessed error or synthetic execution.
		if size > 8<<20 || len(inline) > 8<<20 {
			continue
		}
		if digest != "" {
			inline, err = s.readContentBody(digest, size)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
		}
		if err := s.projectTurnEvent(ctx, conn, root, kind, inline, seq, stamp); err != nil {
			return err
		}
	}
}
