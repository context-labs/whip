package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/context-labs/whip/internal/session"
)

var modelBudgetKinds = [...]session.BudgetKind{
	session.BudgetModelCalls, session.BudgetModelTokens, session.BudgetModelCostNanoUSD, session.BudgetModelElapsedMillis,
}

func (s *Store) Budgets(ctx context.Context, owner session.SessionID) (result []session.Budget, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = readBudgets(ctx, tx, owner)
		return err
	})
	return
}

func readBudgets(ctx context.Context, q querier, owner session.SessionID) ([]session.Budget, error) {
	if _, err := readSession(ctx, q, owner); err != nil {
		return nil, err
	}
	result := make([]session.Budget, len(modelBudgetKinds))
	for i, kind := range modelBudgetKinds {
		result[i] = session.Budget{SessionID: owner, Kind: kind}
		err := q.QueryRowContext(ctx, "SELECT revision,limit_value FROM budget_limits WHERE session_id=? AND kind=?", owner, kind).Scan(&result[i].Revision, &result[i].Limit)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	rows, err := q.QueryContext(ctx, attemptSelect+` JOIN attempt_budget_ancestors b ON b.attempt_id=model_attempts.id WHERE b.session_id=? ORDER BY id`, owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		addAttemptBudgets(result, attempt)
	}
	return result, rows.Err()
}

// Totals saturate only when the exact value cannot fit the public int64 range.
// The original per-attempt evidence remains intact and Incomplete blocks a cap.
func addBudgetAmount(budget *session.Budget, target, amount *int64) {
	if amount == nil {
		budget.Incomplete = true
		return
	}
	if *amount < 0 || *amount > math.MaxInt64-*target {
		*target = math.MaxInt64
		budget.Incomplete = true
		return
	}
	*target += *amount
}

func maximumPrice(rates ...*int64) *int64 {
	var result int64
	for _, rate := range rates {
		if rate == nil {
			return nil
		}
		result = max(result, *rate)
	}
	return &result
}

// All categories can occur, so the reservation uses each side's highest known
// price. Cost performs exact integer multiplication and one final upward round.
func reservationCost(request session.ModelRequestSnapshot) *int64 {
	input := maximumPrice(request.Prices.Input, request.Prices.CachedInput)
	output := maximumPrice(request.Prices.Output, request.Prices.Reasoning, request.Prices.CachedOutput)
	prices := session.ModelPrices{Input: input, CachedInput: input, Output: output, Reasoning: output, CachedOutput: output}
	cost, err := prices.Cost(session.ModelUsage{Input: request.InputTokenBound, Output: &request.MaxOutputTokens})
	if err != nil {
		return nil
	}
	return cost
}

func detailLowerBound(details ...*int64) (int64, bool) {
	var total int64
	for _, detail := range details {
		if detail == nil {
			continue
		}
		if *detail > math.MaxInt64-total {
			return math.MaxInt64, true
		}
		total += *detail
	}
	return total, false
}

func addTokenComponent(budget *session.Budget, total, bound *int64, details ...*int64) {
	if total != nil {
		addBudgetAmount(budget, &budget.Used, total)
		return
	}
	lower, overflow := detailLowerBound(details...)
	addBudgetAmount(budget, &budget.Used, &lower)
	if overflow || bound == nil || lower > *bound {
		budget.Incomplete = true
		return
	}
	remaining := *bound - lower
	addBudgetAmount(budget, &budget.Uncertain, &remaining)
}

// Missing cost cannot discard usage that already exceeds the original request
// bounds. A missing total whose detail disproves its bound is still incomplete.
func unresolvedCost(attempt session.ModelAttempt) (*int64, bool) {
	request := attempt.Request
	incomplete := false
	raiseBound := func(total, bound *int64, details ...*int64) *int64 {
		if total != nil {
			if bound == nil {
				return total
			}
			return new(max(*bound, *total))
		}
		lower, overflow := detailLowerBound(details...)
		if bound == nil {
			incomplete = true
			return nil
		}
		if overflow || lower > *bound {
			incomplete = true
		}
		return new(max(*bound, lower))
	}
	request.InputTokenBound = raiseBound(attempt.Result.Usage.Input, request.InputTokenBound, attempt.Result.Usage.CachedInput)
	request.MaxOutputTokens = *raiseBound(attempt.Result.Usage.Output, &request.MaxOutputTokens, attempt.Result.Usage.Reasoning, attempt.Result.Usage.CachedOutput)
	cost := reservationCost(request)
	if cost != nil && *cost == 0 {
		return cost, false
	}
	return cost, incomplete
}

func addAttemptBudgets(budgets []session.Budget, attempt session.ModelAttempt) {
	if attempt.State == session.AttemptCancelled && attempt.DispatchedAt == nil {
		return
	}
	for i := range budgets {
		budget := &budgets[i]
		if attempt.FinishedAt == nil {
			switch budget.Kind {
			case session.BudgetModelCalls:
				if attempt.DispatchedAt != nil {
					addBudgetAmount(budget, &budget.Used, new(int64(1)))
				} else {
					addBudgetAmount(budget, &budget.Reserved, new(int64(1)))
				}
			case session.BudgetModelTokens:
				addBudgetAmount(budget, &budget.Reserved, attempt.Request.InputTokenBound)
				addBudgetAmount(budget, &budget.Reserved, &attempt.Request.MaxOutputTokens)
			case session.BudgetModelCostNanoUSD:
				addBudgetAmount(budget, &budget.Reserved, reservationCost(attempt.Request))
			case session.BudgetModelElapsedMillis:
				addBudgetAmount(budget, &budget.Reserved, &attempt.Request.TimeoutMillis)
			}
			continue
		}
		if attempt.Result == nil {
			budget.Incomplete = true
			continue
		}
		switch budget.Kind {
		case session.BudgetModelCalls:
			addBudgetAmount(budget, &budget.Used, new(int64(1)))
		case session.BudgetModelTokens:
			addTokenComponent(budget, attempt.Result.Usage.Input, attempt.Request.InputTokenBound, attempt.Result.Usage.CachedInput)
			addTokenComponent(budget, attempt.Result.Usage.Output, &attempt.Request.MaxOutputTokens, attempt.Result.Usage.Reasoning, attempt.Result.Usage.CachedOutput)
		case session.BudgetModelCostNanoUSD:
			if attempt.CostNanoUSD != nil {
				addBudgetAmount(budget, &budget.Used, attempt.CostNanoUSD)
			} else if attempt.CostNote != nil && *attempt.CostNote == session.ErrCostOverflow.Error() {
				addBudgetAmount(budget, &budget.Used, new(int64(math.MaxInt64)))
				budget.Incomplete = true
			} else {
				cost, incomplete := unresolvedCost(attempt)
				addBudgetAmount(budget, &budget.Uncertain, cost)
				budget.Incomplete = budget.Incomplete || incomplete
			}
		case session.BudgetModelElapsedMillis:
			if attempt.Result.ElapsedMillis != nil {
				addBudgetAmount(budget, &budget.Used, attempt.Result.ElapsedMillis)
			} else {
				addBudgetAmount(budget, &budget.Uncertain, &attempt.Request.TimeoutMillis)
			}
		}
	}
}

func budgetExceeds(budget session.Budget, limit int64) bool {
	if budget.Incomplete || budget.Used > limit {
		return true
	}
	remaining := limit - budget.Used
	if budget.Reserved > remaining {
		return true
	}
	return budget.Uncertain > remaining-budget.Reserved
}

func budgetAncestors(ctx context.Context, q querier, owner session.SessionID) ([]session.SessionID, error) {
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE ancestors(id,parent_id,depth) AS (
 SELECT id,parent_id,0 FROM sessions WHERE id=? UNION ALL
 SELECT s.id,s.parent_id,a.depth+1 FROM sessions s JOIN ancestors a ON a.parent_id=s.id
 ) SELECT id FROM ancestors ORDER BY depth`, owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []session.SessionID
	for rows.Next() {
		var id session.SessionID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func reserveBudgets(ctx context.Context, tx *sql.Tx, owner session.SessionID, request session.ModelRequestSnapshot) ([]session.SessionID, error) {
	ancestors, err := budgetAncestors(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	for _, ancestor := range ancestors {
		var finite bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM budget_limits WHERE session_id=? AND limit_value IS NOT NULL)", ancestor).Scan(&finite); err != nil {
			return nil, err
		}
		if !finite {
			continue
		}
		budgets, err := readBudgets(ctx, tx, ancestor)
		if err != nil {
			return nil, err
		}
		addAttemptBudgets(budgets, session.ModelAttempt{Request: request, State: session.AttemptReserved})
		for _, budget := range budgets {
			if budget.Limit != nil && budgetExceeds(budget, *budget.Limit) {
				return nil, fmt.Errorf("%w: %s budget cannot reserve request", ErrLimit, budget.Kind)
			}
		}
	}
	return ancestors, nil
}

func (s *Store) SetBudget(ctx context.Context, owner session.SessionID, expectedRevision int64, limit session.BudgetLimit) (result session.Budget, err error) {
	if err := limit.Validate(); err != nil {
		return result, err
	}
	if expectedRevision < 0 {
		return result, session.ErrInvalid
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = setBudget(ctx, tx, owner, expectedRevision, limit)
		return err
	})
	return
}

func setBudget(ctx context.Context, tx *sql.Tx, owner session.SessionID, expectedRevision int64, limit session.BudgetLimit) (session.Budget, error) {
	budgets, err := readBudgets(ctx, tx, owner)
	if err != nil {
		return session.Budget{}, err
	}
	var result session.Budget
	for _, budget := range budgets {
		if budget.Kind == limit.Kind {
			result = budget
		}
	}
	if result.Revision != expectedRevision {
		return result, ErrConflict
	}
	if result.Revision == math.MaxInt64 {
		return result, ErrLimit
	}
	if limit.Limit != nil && budgetExceeds(result, *limit.Limit) {
		return result, fmt.Errorf("%w: budget limit is below allocated exposure", ErrLimit)
	}
	ancestors, err := budgetAncestors(ctx, tx, owner)
	if err != nil {
		return result, err
	}
	for _, ancestor := range ancestors[1:] {
		var parentLimit *int64
		err := tx.QueryRowContext(ctx, "SELECT limit_value FROM budget_limits WHERE session_id=? AND kind=?", ancestor, limit.Kind).Scan(&parentLimit)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if parentLimit != nil && (limit.Limit == nil || *limit.Limit > *parentLimit) {
			return result, fmt.Errorf("%w: child budget exceeds ancestor limit", ErrLimit)
		}
	}
	result.Revision++
	result.Limit = limit.Limit
	_, err = tx.ExecContext(ctx, `INSERT INTO budget_limits (session_id,kind,revision,limit_value) VALUES (?,?,?,?)
 ON CONFLICT(session_id,kind) DO UPDATE SET revision=excluded.revision,limit_value=excluded.limit_value`, owner, limit.Kind, result.Revision, limit.Limit)
	return result, err
}
