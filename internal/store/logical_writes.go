package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func isWriteBudget(kind session.BudgetKind) bool {
	return kind == session.BudgetLogicalWrites || kind == session.BudgetLogicalWriteBytes
}

func readWriteBudgets(ctx context.Context, q querier, owner session.SessionID) ([]session.Budget, error) {
	if _, err := readSession(ctx, q, owner); err != nil {
		return nil, err
	}
	result := []session.Budget{
		{SessionID: owner, Kind: session.BudgetLogicalWrites},
		{SessionID: owner, Kind: session.BudgetLogicalWriteBytes},
	}
	for i := range result {
		err := q.QueryRowContext(ctx, "SELECT revision,limit_value FROM budget_limits WHERE session_id=? AND kind=?", owner, result[i].Kind).Scan(&result[i].Revision, &result[i].Limit)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT w.bytes FROM logical_writes w
 JOIN logical_write_ancestors a ON a.write_id=w.id WHERE a.session_id=?`, owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var size int64
		if err := rows.Scan(&size); err != nil {
			return nil, err
		}
		addBudgetAmount(&result[0], &result[0].Used, new(int64(1)))
		addBudgetAmount(&result[1], &result[1].Used, &size)
	}
	return result, rows.Err()
}

// chargeWrite commits only with its logical action. Stable source identities
// make retries free; captured ancestry keeps spend after source deletion.
func chargeWrite(ctx context.Context, tx *sql.Tx, actor session.SessionID, kind, id string, revision, size int64) error {
	if size < 0 {
		return session.ErrInvalid
	}
	var previousActor session.SessionID
	var previousSize int64
	err := tx.QueryRowContext(ctx, "SELECT author_id,bytes FROM logical_writes WHERE source_kind=? AND source_id=? AND source_revision=?", kind, id, revision).Scan(&previousActor, &previousSize)
	if err == nil {
		if previousActor != actor || previousSize != size {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	owner, err := readSession(ctx, tx, actor)
	if err != nil {
		return err
	}
	ancestors, err := sessionAncestors(ctx, tx, actor)
	if err != nil {
		return err
	}
	for i, ancestor := range ancestors {
		var finite bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM budget_limits WHERE session_id=? AND kind IN ('logical_writes','logical_write_bytes') AND limit_value IS NOT NULL)", ancestor).Scan(&finite); err != nil {
			return err
		}
		if !finite && i < len(ancestors)-1 {
			continue
		}
		budgets, err := readWriteBudgets(ctx, tx, ancestor)
		if err != nil {
			return err
		}
		for j := range budgets {
			budget := &budgets[j]
			amount := int64(1)
			if budget.Kind == session.BudgetLogicalWriteBytes {
				amount = size
			}
			addBudgetAmount(budget, &budget.Used, &amount)
			if budget.Incomplete || (budget.Limit != nil && budget.Used > *budget.Limit) || (i == len(ancestors)-1 && budget.Limit == nil) {
				return fmt.Errorf("%w: %s budget cannot admit write", ErrLimit, budget.Kind)
			}
		}
	}
	writeID := newID("write")
	if _, err := tx.ExecContext(ctx, "INSERT INTO logical_writes VALUES (?,?,?,?,?,?,?,?)", writeID, owner.TreeID, actor, kind, id, revision, size, now()); err != nil {
		return err
	}
	for _, ancestor := range ancestors {
		if _, err := tx.ExecContext(ctx, "INSERT INTO logical_write_ancestors VALUES (?,?)", writeID, ancestor); err != nil {
			return err
		}
	}
	return nil
}

func inputWriteBytes(parts []session.Part) int64 {
	var size int64
	for _, part := range parts {
		if part.Type == "text" {
			size += int64(len(part.Text))
		}
	}
	return size
}
