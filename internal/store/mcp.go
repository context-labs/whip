package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

func validateMCPCatalog(ctx context.Context, q querier, spec session.OperationSpec) error {
	var request session.MCPCatalogRequest
	decoder := json.NewDecoder(bytes.NewReader(spec.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	var owner session.SessionID
	var tree session.TreeID
	var revision session.Revision
	if err := q.QueryRowContext(ctx, `SELECT t.session_id,s.tree_id,t.config_revision FROM cells c JOIN turns t ON t.id=c.turn_id JOIN sessions s ON s.id=t.session_id WHERE c.id=?`, spec.CellID).Scan(&owner, &tree, &revision); err != nil {
		return found(err)
	}
	if request.SessionID != owner || request.TreeID != tree || request.ConfigRevision != revision || spec.Resource != string(tree) {
		return ErrConflict
	}
	config, err := readConfiguration(ctx, q, owner, revision)
	if err != nil {
		return err
	}
	if !slices.Contains(config.Modules, "mcp") || !session.SameContract(config.MCPServers, request.Selection) {
		return ErrConflict
	}
	return nil
}

// MCPStandingGrants observes current standing call/instruction authority in one
// read snapshot. A later revocation may race this observation; dispatch always
// rechecks the chain independently. One-use approvals never publish a catalog.
func (s *Store) MCPStandingGrants(ctx context.Context, id session.SessionID) (result []session.Grant, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure := tx.Rollback(); failure != nil && !errors.Is(failure, sql.ErrTxDone) {
			err = errors.Join(err, failure)
		}
	}()
	if _, err := readSession(ctx, tx, id); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, grantSelect+` WHERE session_id=? AND operation_id IS NULL AND revoked_at IS NULL AND capability IN ('mcp.call','mcp.call.trusted','mcp.instructions') ORDER BY id LIMIT ?`, id, session.MaxGrantsPerSession+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var candidates []session.Grant
	for rows.Next() {
		value, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, value)
	}
	readErr := rows.Err()
	err = rows.Close()
	if err = errors.Join(readErr, err); err != nil {
		return nil, err
	}
	if len(candidates) > session.MaxGrantsPerSession {
		return nil, ErrLimit
	}
	result = []session.Grant{}
	for _, grant := range candidates {
		if err := validateGrantChain(ctx, tx, grant); err == nil {
			result = append(result, grant)
		} else if !errors.Is(err, ErrConflict) {
			return nil, err
		}
	}
	return result, tx.Commit()
}

// Imported/attached connections require an exact grant even in Full Access.
// Only the trusted host preparation can select the separate trusted capability.
func requiresExplicitMCPGrant(capability string) bool {
	return capability == "mcp.call" || capability == "mcp.connect"
}
