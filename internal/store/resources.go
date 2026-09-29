package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

const resourceSubtree = `WITH RECURSIVE scope(id,depth) AS (
 SELECT id,0 FROM sessions WHERE id=? UNION ALL
 SELECT s.id,p.depth+1 FROM sessions s JOIN scope p ON s.parent_id=p.id
) `

// Resources includes every enforcing scope in one consistent snapshot. An absent
// local limit is inheritance, not unlimited capacity or a second usage ledger.
func (s *Store) Resources(ctx context.Context, owner session.SessionID) (result []session.ResourceUsage, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		ancestors, err := resourceAncestors(ctx, tx, owner)
		if err != nil {
			return err
		}
		result = make([]session.ResourceUsage, 0, len(ancestors)*len(session.ResourceKinds()))
		for _, id := range ancestors {
			for _, kind := range session.ResourceKinds() {
				value, err := readResource(ctx, tx, id, kind)
				if err != nil {
					return err
				}
				result = append(result, value)
			}
		}
		return nil
	})
	return
}

func resourceAncestors(ctx context.Context, q querier, owner session.SessionID) ([]session.SessionID, error) {
	ancestors, err := sessionAncestors(ctx, q, owner)
	if err != nil {
		return nil, err
	}
	if len(ancestors) == 0 {
		return nil, ErrNotFound
	}
	if len(ancestors) > session.MaxSessionDepth+1 {
		return nil, ErrLimit
	}
	return ancestors, nil
}

func readResource(ctx context.Context, q querier, owner session.SessionID, kind session.ResourceKind) (session.ResourceUsage, error) {
	result := session.ResourceUsage{SessionID: owner, Kind: kind}
	err := q.QueryRowContext(ctx, "SELECT revision,limit_value FROM resource_limits WHERE session_id=? AND kind=?", owner, kind).
		Scan(&result.Revision, &result.Limit)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	result.Used, err = resourceUsed(ctx, q, owner, kind)
	return result, err
}

func resourceUsed(ctx context.Context, q querier, owner session.SessionID, kind session.ResourceKind) (used int64, err error) {
	var query string
	switch kind {
	case session.ResourceDepth:
		query = "SELECT coalesce(max(depth),0) FROM scope"
	case session.ResourceDescendants:
		query = "SELECT count(*) FROM scope WHERE depth>0"
	case session.ResourceQueuedInputs:
		query = "SELECT count(*) FROM inputs i JOIN scope s ON s.id=i.session_id WHERE i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL"
	case session.ResourceActiveOperations:
		query = `SELECT count(*) FROM operations o LEFT JOIN cells c ON c.id=o.cell_id
 JOIN turns t ON t.id=COALESCE(c.turn_id,o.direct_turn_id) JOIN scope s ON s.id=t.session_id WHERE o.finished_at IS NULL`
	case session.ResourceRunnableDescendants:
		query = "SELECT count(*) FROM turn_permits p JOIN turns t ON t.id=p.turn_id JOIN scope s ON s.id=t.session_id WHERE s.depth>0"
	case session.ResourceSchedules:
		query = "SELECT count(*) FROM schedules v JOIN scope s ON s.id=v.session_id WHERE v.next_due IS NOT NULL AND v.cancelled_at IS NULL AND v.deleted_at IS NULL"
	case session.ResourceSubscriptions:
		query = "SELECT count(*) FROM state_subscriptions v JOIN scope s ON s.id=v.session_id WHERE v.cancelled_at IS NULL"
	default:
		return 0, session.ErrInvalid
	}
	err = q.QueryRowContext(ctx, resourceSubtree+query, owner).Scan(&used)
	return used, err
}

// checkResources runs after proposed admission in the same immediate transaction.
// Rollback removes both the work and its capacity; release is the owning row's
// normal lifecycle transition. No counter reconciliation is necessary on restart.
func checkResources(ctx context.Context, tx *sql.Tx, owner session.SessionID, kinds ...session.ResourceKind) error {
	ancestors, err := resourceAncestors(ctx, tx, owner)
	if err != nil {
		return err
	}
	caps, err := resourceCaps(ctx, tx, owner, kinds)
	if err != nil {
		return err
	}
	rootCaps := make(map[session.ResourceKind]bool, len(kinds))
	for _, limit := range caps {
		if limit.SessionID == ancestors[len(ancestors)-1] {
			rootCaps[limit.Kind] = true
		}
		used, err := resourceUsed(ctx, tx, limit.SessionID, limit.Kind)
		if err != nil {
			return err
		}
		if used > *limit.Limit {
			return fmt.Errorf("%w: %s capacity of session %s", ErrLimit, limit.Kind, limit.SessionID)
		}
	}
	for _, kind := range kinds {
		if !rootCaps[kind] {
			return fmt.Errorf("%w: root resource limit is absent", ErrSchema)
		}
	}
	return nil
}

// Load applicable finite caps together and close the cursor before counting.
// Inherited scopes require no repeated subtree aggregation during admission.
func resourceCaps(ctx context.Context, q querier, owner session.SessionID, kinds []session.ResourceKind) ([]session.ResourceUsage, error) {
	rows, err := q.QueryContext(ctx, sessionAncestry+`SELECT l.session_id,l.kind,l.limit_value
 FROM ancestors a JOIN resource_limits l ON l.session_id=a.id
 WHERE l.limit_value IS NOT NULL ORDER BY a.depth,l.kind`, owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []session.ResourceUsage
	for rows.Next() {
		var limit session.ResourceUsage
		if err := rows.Scan(&limit.SessionID, &limit.Kind, &limit.Limit); err != nil {
			return nil, err
		}
		if slices.Contains(kinds, limit.Kind) {
			result = append(result, limit)
		}
	}
	return result, rows.Err()
}

func (s *Store) SetResource(ctx context.Context, owner session.SessionID, expectedRevision int64, limit session.ResourceLimit) (result session.ResourceUsage, err error) {
	if err := limit.Validate(); err != nil {
		return result, err
	}
	if expectedRevision < 0 {
		return result, session.ErrInvalid
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = setResource(ctx, tx, owner, expectedRevision, limit)
		return err
	})
	return
}

func setResource(ctx context.Context, tx *sql.Tx, owner session.SessionID, expectedRevision int64, limit session.ResourceLimit) (session.ResourceUsage, error) {
	ancestors, err := resourceAncestors(ctx, tx, owner)
	if err != nil {
		return session.ResourceUsage{}, err
	}
	result, err := readResource(ctx, tx, owner, limit.Kind)
	if err != nil {
		return result, err
	}
	if result.Revision != expectedRevision {
		return result, ErrConflict
	}
	if result.Revision == math.MaxInt64 {
		return result, ErrLimit
	}
	if len(ancestors) == 1 && limit.Limit == nil {
		return result, fmt.Errorf("%w: root resource limits must be finite", session.ErrInvalid)
	}
	if limit.Limit != nil && result.Used > *limit.Limit {
		return result, fmt.Errorf("%w: resource limit is below current usage", ErrLimit)
	}
	// Nil removes only the local limit. For explicit depth/descendant allowances,
	// the chain leading to this owner already consumes ancestor capacity.
	for index, id := range ancestors[1:] {
		var parentLimit *int64
		err := tx.QueryRowContext(ctx, "SELECT limit_value FROM resource_limits WHERE session_id=? AND kind=?", id, limit.Kind).Scan(&parentLimit)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if parentLimit == nil || limit.Limit == nil {
			continue
		}
		allowance := *parentLimit
		if limit.Kind == session.ResourceDepth || limit.Kind == session.ResourceDescendants {
			allowance -= int64(index + 1)
		}
		if *limit.Limit > allowance {
			return result, fmt.Errorf("%w: child resource limit exceeds ancestor allowance", ErrLimit)
		}
	}
	result.Revision++
	result.Limit = limit.Limit
	_, err = tx.ExecContext(ctx, `INSERT INTO resource_limits (session_id,kind,revision,limit_value) VALUES (?,?,?,?)
 ON CONFLICT(session_id,kind) DO UPDATE SET revision=excluded.revision,limit_value=excluded.limit_value`, owner, limit.Kind, result.Revision, limit.Limit)
	return result, err
}
