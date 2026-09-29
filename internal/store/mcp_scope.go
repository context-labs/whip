package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

// MaxChildMCPTools matches the maximum retained host catalog. Capturing scope
// cannot allocate an unbounded per-child copy of external metadata.
const MaxChildMCPTools = 8192

// MCPToolScope is host-captured eligibility, not a grant. It excludes connection
// generations so an unchanged tool remains eligible after root reconnection.
type MCPToolScope struct {
	Capability string `json:"capability"`
	Resource   string `json:"resource"`
}

func validateMCPTools(tools []MCPToolScope) error {
	if len(tools) > MaxChildMCPTools {
		return ErrLimit
	}
	seen := make(map[MCPToolScope]bool, len(tools))
	for _, item := range tools {
		identity := strings.TrimPrefix(item.Resource, "mcp_call_")
		_, err := hex.DecodeString(identity)
		if item.Capability != "mcp.call.trusted" || len(identity) != 64 || identity == item.Resource || err != nil || seen[item] {
			return session.ErrInvalid
		}
		seen[item] = true
	}
	return nil
}

func readMCPTools(ctx context.Context, q querier, id session.SessionID) ([]MCPToolScope, error) {
	rows, err := q.QueryContext(ctx, "SELECT capability,resource FROM child_mcp_tools WHERE session_id=? ORDER BY capability,resource LIMIT ?", id, MaxChildMCPTools+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []MCPToolScope{}
	for rows.Next() {
		var item MCPToolScope
		if err := rows.Scan(&item.Capability, &item.Resource); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := validateMCPTools(result); err != nil {
		return nil, err
	}
	return result, nil
}

// MCPInheritedTools observes the immutable tool ceiling without authorizing an
// effect. A child spawned while permission mode is Ask keeps this scope for a
// later mode change, but its calls still require current authority.
func (s *Store) MCPInheritedTools(ctx context.Context, id session.SessionID) ([]MCPToolScope, error) {
	return readMCPTools(ctx, s.db, id)
}

func insertMCPTools(ctx context.Context, tx *sql.Tx, child session.Session, tools []MCPToolScope) error {
	if err := validateMCPTools(tools); err != nil {
		return err
	}
	if !slices.Contains(child.Config.Modules, "mcp") {
		return nil
	}
	parent, err := readSession(ctx, tx, *child.ParentID)
	if err != nil {
		return err
	}
	if parent.ParentID != nil {
		ceiling, err := readMCPTools(ctx, tx, parent.ID)
		if err != nil {
			return err
		}
		for _, item := range tools {
			if !slices.Contains(ceiling, item) {
				return ErrConflict
			}
		}
	}
	for _, item := range tools {
		if _, err := tx.ExecContext(ctx, "INSERT INTO child_mcp_tools(session_id,capability,resource) VALUES (?,?,?)", child.ID, item.Capability, item.Resource); err != nil {
			return err
		}
	}
	return nil
}

// automaticOperationPermissionRevision is used at admission and again before
// effects. The tree's current policy cannot widen a child's captured MCP tools.
func automaticOperationPermissionRevision(ctx context.Context, q querier, owner session.Session, capability, resource string) (*session.Revision, error) {
	if requiresExplicitHostGrant(capability) {
		return nil, nil //nolint:nilnil // No revision means this operation has no automatic authority.
	}
	if owner.ParentID != nil {
		if capability == "mcp.connect.trusted" {
			return nil, nil //nolint:nilnil // A child has no automatic connection authority.
		}
		if capability == "mcp.call.trusted" {
			var exists bool
			if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM child_mcp_tools WHERE session_id=? AND capability=? AND resource=?)", owner.ID, capability, resource).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, nil //nolint:nilnil // An uncaptured tool has no automatic authority.
			}
		}
	}
	return automaticPermissionRevision(ctx, q, owner)
}

// MCPDelegatedAuthority is a bounded read-only snapshot of the same authority
// dispatch uses. One-use approvals do not publish tools. Discovery grants
// nothing, starts no connections, and never substitutes for dispatch checks.
func (s *Store) MCPDelegatedAuthority(ctx context.Context, id session.SessionID) (result []MCPToolScope, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure := tx.Rollback(); failure != nil && !errors.Is(failure, sql.ErrTxDone) {
			err = errors.Join(err, failure)
		}
	}()
	owner, err := readSession(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	grants, err := mcpStandingGrants(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	result = make([]MCPToolScope, 0, len(grants))
	for _, grant := range grants {
		result = append(result, MCPToolScope{Capability: grant.Capability, Resource: grant.Resource})
	}
	// The immutable list is already the exact scope required by the operation
	// check. Check live policy once for this snapshot, not once per catalog row.
	revision, err := automaticPermissionRevision(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if revision != nil && owner.ParentID != nil {
		tools, err := readMCPTools(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, tools...)
	}
	return result, tx.Commit()
}

// Host capture is private admission evidence; public exact retries are matched
// against the original user request, regardless of later catalog changes.
func childRequestDigest(request ChildRequest) (string, error) {
	request.MCPTools = nil
	return requestDigest("spawn_child", request)
}
