package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/context-labs/whip/internal/capability"
)

// fileAccess is effective authority, not project context or a working directory.
type fileAccess struct {
	all   bool
	paths []string
}

func (a fileAccess) contains(path string) bool { return a.all || scopeContains(a.paths, path) }

func (a fileAccess) intersect(b fileAccess) fileAccess {
	if a.all {
		return b
	}
	if b.all {
		return a
	}
	var paths []string
	for _, path := range a.paths {
		if b.contains(path) {
			paths = append(paths, path)
		}
	}
	for _, path := range b.paths {
		if a.contains(path) && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	return fileAccess{paths: paths}
}

// sessionFileAccessTx keeps the bootstrap boundary stable across cwd changes.
// A root without bootstrap authority uses its initial cwd until bootstrap.
func sessionFileAccessTx(ctx context.Context, tx *sql.Tx, rootID string) (fileAccess, []string, error) {
	var mode, cwd string
	if err := tx.QueryRowContext(ctx, `SELECT permission_mode,cwd FROM sessions WHERE id=?`, rootID).Scan(&mode, &cwd); err != nil {
		return fileAccess{}, nil, err
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT scopes FROM capabilities WHERE id=? AND root_id=? AND agent_id=?`, "files:"+rootID, rootID, rootID).Scan(&raw)
	var scopes storedCapabilityScopes
	if errors.Is(err, sql.ErrNoRows) {
		path, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			return fileAccess{}, nil, err
		}
		scopes.Paths = []string{path}
	} else if err != nil {
		return fileAccess{}, nil, err
	} else if err := json.Unmarshal(raw, &scopes); err != nil {
		return fileAccess{}, nil, err
	}
	if len(scopes.Paths) == 0 {
		return fileAccess{}, nil, capability.ErrDenied
	}
	for _, path := range scopes.Paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fileAccess{}, nil, capability.ErrDenied
		}
	}
	switch mode {
	case PermissionModeAutomatic:
		return fileAccess{all: true}, scopes.Paths, nil
	case PermissionModePrompt:
		return fileAccess{paths: scopes.Paths}, scopes.Paths, nil
	default:
		return fileAccess{}, nil, capability.ErrDenied
	}
}

// loadFileAccessTx validates every recorded issuer and intersects all explicit
// ceilings with the current session policy. Legacy grants remain explicit.
func loadFileAccessTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, ref capability.Reference) (fileAccess, error) {
	policy, _, err := sessionFileAccessTx(ctx, tx, rootID)
	if err != nil {
		return fileAccess{}, err
	}
	access := policy
	seen := make(map[string]bool)
	var childOperations []string
	for {
		if seen[ref.ID] {
			return fileAccess{}, capability.ErrDenied
		}
		seen[ref.ID] = true
		if err := validateCapabilityAgent(ctx, tx, rootID, agentID); err != nil {
			return fileAccess{}, err
		}
		grant, err := loadCapabilityGrant(ctx, tx, rootID, agentID, ref.ID, ref.Generation)
		if err != nil {
			return fileAccess{}, err
		}
		for _, operation := range childOperations {
			if !slices.Contains(grant.operations, operation) {
				return fileAccess{}, capability.ErrDenied
			}
		}
		switch grant.scopes.FileScope {
		case "session":
			if agentID != rootID || ref.ID != "files:"+rootID || grant.issuerAgentID != "" || grant.scopes.FileIssuerID != "" {
				return fileAccess{}, capability.ErrDenied
			}
			return access, nil
		case "inherit":
			if len(grant.scopes.Paths) != 0 || grant.scopes.FileIssuerID == "" {
				return fileAccess{}, capability.ErrDenied
			}
		case "":
			access = access.intersect(fileAccess{paths: grant.scopes.Paths})
		default:
			return fileAccess{}, capability.ErrDenied
		}
		if grant.scopes.FileIssuerID == "" {
			return access, nil
		}
		if grant.issuerAgentID == "" || grant.issuerAgentID == agentID {
			return fileAccess{}, capability.ErrDenied
		}
		allowed, err := agentInSubtreeTx(ctx, tx, rootID, grant.issuerAgentID, agentID)
		if err != nil {
			return fileAccess{}, err
		}
		if !allowed {
			return fileAccess{}, capability.ErrDenied
		}
		childOperations = grant.operations
		agentID = grant.issuerAgentID
		ref = capability.Reference{ID: grant.scopes.FileIssuerID, Generation: grant.scopes.FileIssuerGeneration}
	}
}

func authorizeFilePathTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, ref capability.Reference, path string) error {
	access, err := loadFileAccessTx(ctx, tx, rootID, agentID, ref)
	if err != nil {
		return err
	}
	if !access.contains(path) {
		return fmt.Errorf("path %q is outside this agent's allowed filesystem scope: %w", path, capability.ErrDenied)
	}
	return nil
}
