package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// Fork atomically imports a terminal raw prefix into a fresh root. A later source
// turn may be active: the exact observed tail, history and configuration must
// still match. Receipt lookup precedes all mutable source/default validation.
func (s *Store) Fork(ctx context.Context, request session.ForkRequest, defaults session.ForkDefaults) (result session.ForkResult, err error) {
	if err := session.ValidateID(string(request.ID)); err != nil {
		return result, err
	}
	digest, err := requestDigest("session_fork", request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT digest FROM forks WHERE id=?", request.ID).Scan(&previous)
		if err == nil {
			if previous != digest {
				return ErrConflict
			}
			result, err = readFork(ctx, tx, request.ID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := request.Validate(); err != nil {
			return err
		}
		resources, err := session.ResolveResourceLimits(nil, defaults.Resources)
		if err != nil {
			return err
		}
		if err := validateForkBudgets(defaults.Budgets); err != nil {
			return err
		}
		source, err := validateForkSource(ctx, tx, request)
		if err != nil {
			return err
		}
		tree, err := readTree(ctx, tx, source.TreeID)
		if err != nil {
			return err
		}
		if err := preflightFork(ctx, tx, request); err != nil {
			return err
		}
		metadata, err := encode(session.TreeMetadata{Title: request.Title})
		if err != nil {
			return err
		}
		treeID := session.TreeID(newID("tree"))
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_trees VALUES (?,?,?,1,?)", treeID, metadata, tree.Engine, now()); err != nil {
			return err
		}
		root, err := insertSession(ctx, tx, treeID, nil, source.Definition, source.Config, source.WorkingDirectory)
		if err != nil {
			return err
		}
		for _, limit := range resources {
			if _, err := setResource(ctx, tx, root.ID, 0, limit); err != nil {
				return err
			}
		}
		for _, limit := range defaults.Budgets {
			if _, err := setBudget(ctx, tx, root.ID, 0, limit); err != nil {
				return err
			}
		}
		messages, err := importForkHistory(ctx, tx, request, root.ID)
		if err != nil {
			return err
		}
		if err := importForkCompactions(ctx, tx, request, root.ID, messages); err != nil {
			return err
		}
		// Opaque handles can occur in arbitrary text. Preserve every owner-scoped
		// reference without rewriting text or granting access by digest alone.
		if _, err := tx.ExecContext(ctx, `INSERT INTO content_references(reference_id,owner_session_id,digest,media_type,created_at)
 SELECT reference_id,?,digest,media_type,? FROM content_references WHERE owner_session_id=?`, root.ID, now(), source.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO forks(id,digest,source_session_id,history_revision,config_revision,observed_through,keep_through,title,tree_id,root_id,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, request.ID, digest, source.ID, request.ExpectedHistoryRevision, request.ExpectedConfigRevision, request.ObservedThrough, request.KeepThrough, request.Title, treeID, root.ID, now()); err != nil {
			return err
		}
		if err := initializeTitle(ctx, tx, root, nil, "fork"); err != nil {
			return err
		}
		result, err = readFork(ctx, tx, request.ID)
		return err
	})
	if err != nil {
		return session.ForkResult{}, err
	}
	return result, nil
}

func validateForkBudgets(limits []session.BudgetLimit) error {
	seen := map[session.BudgetKind]bool{}
	for _, limit := range limits {
		if err := limit.Validate(); err != nil {
			return err
		}
		if seen[limit.Kind] {
			return fmt.Errorf("%w: duplicate fork budget", session.ErrInvalid)
		}
		if isWriteBudget(limit.Kind) && limit.Limit == nil {
			return fmt.Errorf("%w: fork write budgets must be finite", session.ErrInvalid)
		}
		seen[limit.Kind] = true
	}
	if !seen[session.BudgetLogicalWrites] || !seen[session.BudgetLogicalWriteBytes] {
		return fmt.Errorf("%w: fork requires fresh logical-write budgets", session.ErrInvalid)
	}
	return nil
}

func readFork(ctx context.Context, q querier, id session.ForkID) (result session.ForkResult, err error) {
	var created int64
	f := &result.Fork
	err = q.QueryRowContext(ctx, `SELECT id,source_session_id,history_revision,config_revision,observed_through,keep_through,title,tree_id,root_id,created_at
 FROM forks WHERE id=?`, id).Scan(&f.ID, &f.SessionID, &f.ExpectedHistoryRevision, &f.ExpectedConfigRevision, &f.ObservedThrough, &f.KeepThrough, &f.Title, &f.TreeID, &f.RootID, &created)
	if err != nil {
		return result, found(err)
	}
	f.CreatedAt = timestamp(created)
	root, err := readSession(ctx, q, f.RootID)
	if errors.Is(err, ErrNotFound) {
		result.Deleted = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	tree, err := readTree(ctx, q, f.TreeID)
	if err != nil {
		return result, err
	}
	result.Root, result.Tree = &root, &tree
	return result, nil
}

func validateForkSource(ctx context.Context, tx *sql.Tx, request session.ForkRequest) (session.Session, error) {
	source, err := readSession(ctx, tx, request.SessionID)
	if err != nil {
		return source, err
	}
	var through int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence),0) FROM messages WHERE session_id=? AND retired_revision IS NULL", source.ID).Scan(&through); err != nil {
		return source, err
	}
	if source.HistoryRevision != request.ExpectedHistoryRevision || source.ConfigRevision != request.ExpectedConfigRevision || through != request.ObservedThrough {
		return source, ErrConflict
	}
	if request.KeepThrough == 0 {
		return source, nil
	}
	var exists, active, split bool
	err = tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM messages WHERE session_id=? AND sequence=? AND retired_revision IS NULL),
 EXISTS(SELECT 1 FROM messages m JOIN turns t ON t.id=m.turn_id WHERE m.session_id=? AND m.sequence<=? AND m.retired_revision IS NULL AND t.finished_at IS NULL),
 EXISTS(SELECT 1 FROM messages later WHERE later.session_id=? AND later.retired_revision IS NULL AND later.sequence>? AND EXISTS(
 SELECT 1 FROM messages earlier WHERE earlier.group_id=later.group_id AND earlier.retired_revision IS NULL AND earlier.sequence<=?))`,
		source.ID, request.KeepThrough, source.ID, request.KeepThrough, source.ID, request.KeepThrough, request.KeepThrough).Scan(&exists, &active, &split)
	if err != nil {
		return source, err
	}
	if !exists || active || split {
		return source, fmt.Errorf("%w: fork requires a whole terminal history group boundary", session.ErrInvalid)
	}
	return source, nil
}
