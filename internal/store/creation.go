package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// CreationRetry runs before callers read mutable host configuration. CreateRoot
// repeats this lookup under the transaction admitting a previously unseen ID.
func (s *Store) CreationRetry(ctx context.Context, request session.TreeCreationRequest) (result session.TreeCreationResult, exists bool, err error) {
	if err := session.ValidateID(string(request.ID)); err != nil {
		return result, false, err
	}
	digest, err := requestDigest("tree_creation", request)
	if err != nil {
		return result, false, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, exists, err = creationRetry(ctx, tx, request.ID, digest)
		return err
	})
	return
}

func creationRetry(ctx context.Context, q querier, id session.CreationID, digest string) (session.TreeCreationResult, bool, error) {
	var previous string
	err := q.QueryRowContext(ctx, "SELECT digest FROM tree_creations WHERE id=?", id).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return session.TreeCreationResult{}, false, nil
	}
	if err != nil {
		return session.TreeCreationResult{}, false, err
	}
	if previous != digest {
		return session.TreeCreationResult{}, true, ErrConflict
	}
	result, err := readCreation(ctx, q, id)
	return result, true, err
}

func (s *Store) CreateRoot(ctx context.Context, request session.TreeCreationRequest, defaults session.TreeCreationDefaults) (result session.TreeCreationResult, err error) {
	if err := session.ValidateID(string(request.ID)); err != nil {
		return result, err
	}
	digest, err := requestDigest("tree_creation", request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var exists bool
		result, exists, err = creationRetry(ctx, tx, request.ID, digest)
		if exists || err != nil {
			return err
		}
		engine := request.Engine
		if engine == "" {
			engine = defaults.Engine
		}
		if err := engine.Validate(); err != nil {
			return err
		}
		if err := validMetadata(request.Metadata); err != nil {
			return err
		}
		limits, err := session.ResolveResourceLimits(defaults.Resources, request.Resources)
		if err != nil {
			return err
		}
		mode, err := session.ResolvePermissionMode(defaults.PermissionMode)
		if err != nil {
			return err
		}
		if request.PermissionMode != nil {
			if err := request.PermissionMode.Validate(); err != nil {
				return err
			}
			mode = *request.PermissionMode
		}
		def, err := definition(ctx, tx, request.Definition)
		if err != nil {
			return err
		}
		base := defaults.Configuration.Clone()
		// Host defaults cannot claim an immutable definition's executor identity.
		base.ToolsDefinition, base.HooksDefinition = nil, nil
		config, err := session.Resolve(base, def.Document, request.Overrides)
		if err != nil {
			return err
		}
		metadata, err := encode(request.Metadata)
		if err != nil {
			return err
		}
		treeID := session.TreeID(newID("tree"))
		created := now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_trees VALUES (?,?,?,1,?)", treeID, metadata, engine, created); err != nil {
			return err
		}
		root, err := insertSession(ctx, tx, treeID, nil, request.Definition, config, request.WorkingDirectory, session.ExplicitReloadOverrides(request.Overrides))
		if err != nil {
			return err
		}
		if err := insertPermissionPolicy(ctx, tx, treeID, mode); err != nil {
			return err
		}
		if request.Metadata.Title != nil {
			if err := initializeTitle(ctx, tx, root, nil, "manual"); err != nil {
				return err
			}
		}
		for _, limit := range limits {
			if _, err := setResource(ctx, tx, root.ID, 0, limit); err != nil {
				return err
			}
		}
		for _, limit := range session.DefaultWriteBudgets() {
			if _, err := setBudget(ctx, tx, root.ID, 0, limit); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO tree_creations VALUES (?,?,?,?,?)", request.ID, digest, treeID, root.ID, created); err != nil {
			return err
		}
		if err := bumpTreeCatalog(ctx, tx); err != nil {
			return err
		}
		result, err = readCreation(ctx, tx, request.ID)
		return err
	})
	if err != nil {
		return session.TreeCreationResult{}, err
	}
	return result, nil
}

// TreeCreation reads original admission evidence and the current destination in
// one snapshot. It never recreates a deleted root or starts a worker.
func (s *Store) TreeCreation(ctx context.Context, id session.CreationID) (result session.TreeCreationResult, err error) {
	if err := session.ValidateID(string(id)); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = readCreation(ctx, tx, id); return err })
	return
}

func readCreation(ctx context.Context, q querier, id session.CreationID) (result session.TreeCreationResult, err error) {
	var created int64
	c := &result.Creation
	err = q.QueryRowContext(ctx, "SELECT id,tree_id,root_id,created_at FROM tree_creations WHERE id=?", id).Scan(&c.ID, &c.TreeID, &c.RootID, &created)
	if err != nil {
		return result, found(err)
	}
	c.CreatedAt = timestamp(created)
	root, err := readSession(ctx, q, c.RootID)
	if errors.Is(err, ErrNotFound) {
		result.Deleted = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	tree, err := readTree(ctx, q, c.TreeID)
	if err != nil {
		return result, err
	}
	result.Tree, result.Root = &tree, &root
	return result, nil
}
