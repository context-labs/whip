package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func upgradeV13(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin filesystem access upgrade: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`) }()
	var version int
	var identity string
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read filesystem access upgrade version: %w", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil {
		return fmt.Errorf("read filesystem access upgrade identity: %w", err)
	}
	// Another opener may have completed the migration while this one waited.
	if version == 14 && identity == "whip-recursive-runtime-v14" ||
		version == currentSchemaVersion && identity == schemaIdentity {
		return nil
	}
	if version != 13 || identity != "whip-recursive-runtime-v13" {
		return fmt.Errorf("filesystem access upgrade requires version 13, found %d", version)
	}

	// Only the root bootstrap identity and shape reliably identify a default
	// grant. Legacy child creation events contain neither scopes nor their source;
	// equal paths cannot distinguish inheritance from an explicit restriction.
	var after string
	for {
		var id, operations, scopes string
		err := conn.QueryRowContext(ctx, `SELECT id,operations,scopes FROM capabilities
			WHERE id>? AND id='files:'||root_id AND agent_id=root_id AND issuer_agent_id=''
			ORDER BY id LIMIT 1`, after).Scan(&id, &operations, &scopes)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return fmt.Errorf("read legacy filesystem grant: %w", err)
		}
		after = id
		updated, ok := legacyRootFileScope(operations, scopes)
		if !ok {
			continue
		}
		if _, err := conn.ExecContext(ctx, `UPDATE capabilities SET scopes=? WHERE id=?`, updated, id); err != nil {
			return fmt.Errorf("upgrade legacy filesystem grant %q: %w", id, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `UPDATE runtime_schema SET identity='whip-recursive-runtime-v14' WHERE id=1;
		PRAGMA user_version=14`); err != nil {
		return fmt.Errorf("upgrade filesystem access identity: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit filesystem access upgrade: %w", err)
	}
	return nil
}

// legacyRootFileScope preserves the original canonical path as the Ask baseline.
// It does not inspect today's filesystem: renamed or deleted projects must still
// open, and a changed symlink must not rewrite a persisted authorization boundary.
func legacyRootFileScope(operations, raw string) (string, bool) {
	var ops []string
	if err := json.Unmarshal([]byte(operations), &ops); err != nil {
		return "", false
	}
	slices.Sort(ops)
	if !slices.Equal(ops, []string{"edit", "read", "workspace.write", "write"}) {
		return "", false
	}
	var scopes storedCapabilityScopes
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil || len(scopes.Paths) != 1 {
		return "", false
	}
	path := scopes.Paths[0]
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, '\x00') {
		return "", false
	}
	// Preserve expiry and any unrelated fields; never rewrite status, generation,
	// timestamps, budgets, or event history while tagging the scope semantics.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return "", false
	}
	for _, name := range []string{"file_scope", "file_issuer_id", "file_issuer_generation"} {
		if _, exists := fields[name]; exists {
			return "", false
		}
	}
	fields["file_scope"] = json.RawMessage(`"session"`)
	updated, err := json.Marshal(fields)
	return string(updated), err == nil
}
