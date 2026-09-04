package session

import (
	"context"
	"database/sql"
	"errors"
	"slices"
)

// PermissionRule is an "always allow" decision remembered for one session tree.
type PermissionRule struct {
	ID, RootID, Operation, Rule, PrincipalID, CreatedAt string
}

// AddPermissionRule installs a tree rule; re-adding an existing
// (operation, rule) returns the row already there.
func (s *Store) AddPermissionRule(ctx context.Context, rootID, operation, rule, principalID string) (PermissionRule, error) {
	if rootID == "" || operation == "" || rule == "" {
		return PermissionRule{}, errors.New("permission rule requires a root, operation, and rule")
	}
	id, err := runtimeID()
	if err != nil {
		return PermissionRule{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO permission_rules(id,root_id,operation,rule,principal_id,created_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(root_id,operation,rule) DO NOTHING`, id, rootID, operation, rule, principalID, now()); err != nil {
		return PermissionRule{}, err
	}
	var row PermissionRule
	err = s.db.QueryRowContext(ctx, `SELECT id,root_id,operation,rule,principal_id,created_at FROM permission_rules WHERE root_id=? AND operation=? AND rule=?`,
		rootID, operation, rule).Scan(&row.ID, &row.RootID, &row.Operation, &row.Rule, &row.PrincipalID, &row.CreatedAt)
	return row, err
}

func (s *Store) ListPermissionRules(ctx context.Context, rootID string) ([]PermissionRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,root_id,operation,rule,principal_id,created_at FROM permission_rules WHERE root_id=? ORDER BY created_at,id`, rootID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var rules []PermissionRule
	for rows.Next() {
		var row PermissionRule
		if err := rows.Scan(&row.ID, &row.RootID, &row.Operation, &row.Rule, &row.PrincipalID, &row.CreatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, row)
	}
	return rules, rows.Err()
}

func (s *Store) DeletePermissionRule(ctx context.Context, rootID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM permission_rules WHERE root_id=? AND id=?`, rootID, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return err
		}
		return errors.New("no permission rule " + id)
	}
	return nil
}

// SetGlobalPermissionRules replaces the config allowlist ("operation:rule"
// entries) the store consults before prompting.
func (s *Store) SetGlobalPermissionRules(entries []string) {
	entries = slices.Clone(entries)
	s.globalRules.Store(&entries)
}

func (s *Store) GlobalPermissionRules() []string {
	if entries := s.globalRules.Load(); entries != nil {
		return *entries
	}
	return nil
}

// ListPendingPermissions returns the root's open prompts, the same rows the
// snapshot carries.
func (s *Store) ListPendingPermissions(ctx context.Context, rootID string) ([]PermissionSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	return s.pendingPermissions(ctx, tx, rootID)
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// PermissionRuleSource reports what covers a prompt with these rules: "global"
// when the config allowlist covers every rule, "tree" when session rules
// complete the cover, "" when any rule is uncovered.
func (s *Store) PermissionRuleSource(ctx context.Context, rootID, operation string, rules []string) (string, error) {
	return s.permissionRuleSource(ctx, s.db, rootID, operation, rules)
}

func (s *Store) permissionRuleSource(ctx context.Context, db rowQuerier, rootID, operation string, rules []string) (string, error) {
	if len(rules) == 0 {
		return "", nil
	}
	source := "global"
	global := s.GlobalPermissionRules()
	for _, rule := range rules {
		if slices.Contains(global, operation+":"+rule) {
			continue
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM permission_rules WHERE root_id=? AND operation=? AND rule=?`, rootID, operation, rule).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", nil
		}
		source = "tree"
	}
	return source, nil
}
