package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/context-labs/whip/internal/capability"
)

type BudgetKind string

const (
	BudgetTokens                 BudgetKind = "tokens"
	BudgetCost                   BudgetKind = "cost"
	BudgetElapsed                BudgetKind = "elapsed"
	BudgetDurableBytes           BudgetKind = "durable_bytes"
	BudgetRecordCount            BudgetKind = "record_count"
	BudgetSchedulesSubscriptions BudgetKind = "schedules_subscriptions"
	BudgetActiveOperations       BudgetKind = "active_operations"
	BudgetActiveChildren         BudgetKind = "active_children"
	BudgetConcurrentChildTurns   BudgetKind = "concurrent_child_turns"
	BudgetDepth                  BudgetKind = "depth"

	DefaultRootDurableBytes           int64 = 1 << 30
	DefaultRootRecords                int64 = 100_000
	DefaultRootSchedulesSubscriptions int64 = 1_000
	DefaultRootActiveOperations       int64 = 64
	DefaultTreeActiveChildren         int64 = 8
	DefaultTreeConcurrentChildTurns   int64 = 4
	DefaultTreeDepth                  int64 = 2
)

type BudgetLimit struct {
	Kind  BudgetKind
	Limit int64
}

type BudgetState struct {
	Kind       BudgetKind `json:"kind"`
	Limit      *int64     `json:"limit,string"`
	Used       int64      `json:"used,string"`
	Reserved   int64      `json:"reserved,string"`
	Remaining  *int64     `json:"remaining,string"`
	Uncertain  int64      `json:"uncertain,string"`
	Incomplete bool       `json:"incomplete"`
}

var defaultRootBudgets = []BudgetLimit{
	{BudgetTokens, 0},
	{BudgetCost, 0},
	{BudgetElapsed, 0},
	{BudgetDurableBytes, DefaultRootDurableBytes},
	{BudgetRecordCount, DefaultRootRecords},
	{BudgetSchedulesSubscriptions, DefaultRootSchedulesSubscriptions},
	{BudgetActiveOperations, DefaultRootActiveOperations},
	{BudgetActiveChildren, DefaultTreeActiveChildren},
	{BudgetConcurrentChildTurns, DefaultTreeConcurrentChildTurns},
	{BudgetDepth, DefaultTreeDepth},
}

type budgetRow struct {
	agentID    string
	kind       BudgetKind
	limit      *int64
	used       int64
	reserved   int64
	uncertain  int64
	incomplete bool
}

func insertDefaultRootBudgets(ctx context.Context, tx *sql.Tx, rootID, stamp string) error {
	for _, budget := range defaultRootBudgets {
		var limit *int64
		if !isModelBudgetKind(budget.Kind) {
			limit = &budget.Limit
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budgets(root_id,agent_id,kind,limit_value,updated_at)
			VALUES(?,'',?,?,?) ON CONFLICT(root_id,agent_id,kind) DO NOTHING`, rootID, budget.Kind, limit, stamp); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetBudgetLimit(ctx context.Context, rootID, agentID string, kind BudgetKind, limit int64) error {
	if rootID == "" || !validBudgetKind(kind) || limit < 0 {
		return errors.New("budget requires a root, kind, and nonnegative limit")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if agentID != "" {
		if _, err := loadAgentTx(ctx, tx, rootID, agentID); err != nil {
			return err
		}
	} else {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, rootID).Scan(&exists); err != nil || exists != 1 {
			if err != nil {
				return err
			}
			return capability.ErrDenied
		}
	}
	if isChildLiveBudgetKind(kind) {
		if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
			return err
		}
	}
	var result sql.Result
	if agentID == "" {
		result, err = tx.ExecContext(ctx, `INSERT INTO budgets(root_id,agent_id,kind,limit_value,updated_at)
			VALUES(?,?,?,?,?) ON CONFLICT(root_id,agent_id,kind) DO UPDATE SET limit_value=excluded.limit_value,updated_at=excluded.updated_at
			WHERE budgets.used_value<=excluded.limit_value AND budgets.reserved_value<=excluded.limit_value-budgets.used_value AND budgets.uncertain_value<=excluded.limit_value-budgets.used_value-budgets.reserved_value`, rootID, agentID, kind, limit, now())
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE budgets SET limit_value=?,updated_at=?
			WHERE root_id=? AND agent_id=? AND kind=? AND used_value<=? AND reserved_value<=?-used_value AND uncertain_value<=?-used_value-reserved_value`, limit, now(), rootID, agentID, kind, limit, limit, limit)
	}
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return capability.ErrDenied
	}
	if isChildLiveBudgetKind(kind) {
		if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
			return err
		}
		var used, reserved int64
		if err := tx.QueryRowContext(ctx, `SELECT used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id=? AND kind=?`,
			rootID, agentID, kind).Scan(&used, &reserved); err != nil {
			return err
		}
		if used > limit || reserved > limit-used {
			return capability.ErrDenied
		}
	}
	return tx.Commit()
}

func (s *Store) SetCapabilityBudget(ctx context.Context, rootID, kind string, limit int64) error {
	return s.SetBudgetLimit(ctx, rootID, "", BudgetKind(kind), limit)
}

func (s *Store) InspectBudgets(ctx context.Context, rootID, agentID string) ([]BudgetState, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, "")
	if err != nil {
		return nil, err
	}
	states := inspectBudgetRows(rows)
	return states, tx.Commit()
}

func (s *Store) InspectBudgetsFor(ctx context.Context, rootID, callerAgentID, targetAgentID string) ([]BudgetState, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := loadAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return nil, err
	}
	if _, err := loadAgentTx(ctx, tx, rootID, targetAgentID); err != nil {
		return nil, err
	}
	allowed, err := agentInSubtreeTx(ctx, tx, rootID, callerAgentID, targetAgentID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAgentAccess
	}
	rows, err := loadBudgetRowsTx(ctx, tx, rootID, targetAgentID, "")
	if err != nil {
		return nil, err
	}
	states := inspectBudgetRows(rows)
	return states, tx.Commit()
}

func inspectBudgetRows(rows []budgetRow) []BudgetState {
	effective := make(map[BudgetKind]BudgetState, len(rows))
	for _, row := range rows {
		state := budgetStateFromRow(row)
		current, ok := effective[row.kind]
		state.Incomplete = state.Incomplete || current.Incomplete
		if !ok || state.Remaining != nil && (current.Remaining == nil || *state.Remaining < *current.Remaining || *state.Remaining == *current.Remaining && *state.Limit < *current.Limit) {
			effective[row.kind] = state
		} else if state.Incomplete {
			current.Incomplete = true
			effective[row.kind] = current
		}
	}
	states := make([]BudgetState, 0, len(effective))
	for _, state := range effective {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Kind < states[j].Kind })
	return states
}

func (s *Store) ReserveBudget(ctx context.Context, rootID, agentID string, reservations []capability.Reservation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reserveCapabilityBudgets(ctx, tx, rootID, agentID, reservations); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReconcileBudget(ctx context.Context, rootID, agentID string, reservations []capability.Reservation, actual []capability.Usage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := settleCapabilityBudgets(ctx, tx, rootID, agentID, reservations, actual); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReleaseBudget(ctx context.Context, rootID, agentID string, reservations []capability.Reservation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := releaseCapabilityBudgets(ctx, tx, rootID, agentID, reservations); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CapBudget(ctx context.Context, rootID, callerAgentID, targetAgentID string, kind BudgetKind, limit int64) (BudgetState, error) {
	if rootID == "" || callerAgentID == "" || targetAgentID == "" || !validBudgetKind(kind) || limit < 0 {
		return BudgetState{}, ErrAgentAccess
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BudgetState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	caller, err := loadAgentTx(ctx, tx, rootID, callerAgentID)
	if err != nil {
		return BudgetState{}, err
	}
	target, err := loadAgentTx(ctx, tx, rootID, targetAgentID)
	if err != nil {
		return BudgetState{}, err
	}
	allowed, err := agentInSubtreeTx(ctx, tx, rootID, callerAgentID, targetAgentID)
	if err != nil {
		return BudgetState{}, err
	}
	if !allowed || isTerminalAgentStatus(caller.Status) && callerAgentID != targetAgentID {
		return BudgetState{}, ErrAgentAccess
	}
	rowAgentID := targetAgentID
	if target.ParentID == "" {
		rowAgentID = ""
	}
	var row budgetRow
	row.agentID, row.kind = rowAgentID, kind
	insert := false
	if err := tx.QueryRowContext(ctx, `SELECT limit_value,used_value,reserved_value,uncertain_value,incomplete FROM budgets WHERE root_id=? AND agent_id=? AND kind=?`,
		rootID, rowAgentID, kind).Scan(&row.limit, &row.used, &row.reserved, &row.uncertain, &row.incomplete); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return BudgetState{}, err
		}
		if rowAgentID == "" {
			return BudgetState{}, capability.ErrDenied
		}
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, targetAgentID, kind)
		if err != nil {
			return BudgetState{}, err
		}
		row.limit = &limit
		for _, inherited := range rows {
			remaining, valid := budgetRemaining(inherited)
			if !valid || inherited.reserved > 0 || inherited.limit != nil && inherited.used > *inherited.limit {
				return BudgetState{}, capability.ErrDenied
			}
			if inherited.limit != nil && remaining < limit {
				limit = remaining
			}
		}
		insert = true
	}
	if row.limit != nil && limit > *row.limit || row.used > limit || row.reserved > limit-row.used || row.uncertain > limit-row.used-row.reserved {
		return BudgetState{}, capability.ErrDenied
	}
	if insert {
		if _, err := tx.ExecContext(ctx, `INSERT INTO budgets(root_id,agent_id,kind,limit_value,used_value,reserved_value,updated_at) VALUES(?,?,?,?,?,?,?)`,
			rootID, rowAgentID, kind, limit, row.used, row.reserved, now()); err != nil {
			return BudgetState{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE budgets SET limit_value=?,updated_at=? WHERE root_id=? AND agent_id=? AND kind=?`,
			limit, now(), rootID, rowAgentID, kind); err != nil {
			return BudgetState{}, err
		}
	}
	row.limit = &limit
	state := budgetStateFromRow(row)
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "budget.capped", actorEvent{
		AgentID: targetAgentID, BudgetKind: string(kind), Limit: limit, Used: state.Used, Reserved: state.Reserved,
	}, now()); err != nil {
		return BudgetState{}, err
	}
	if err := tx.Commit(); err != nil {
		return BudgetState{}, err
	}
	return state, nil
}

func budgetStateFromRow(row budgetRow) BudgetState {
	remaining, _ := budgetRemaining(row)
	var value *int64
	if row.limit != nil {
		value = &remaining
	}
	return BudgetState{Kind: row.kind, Limit: row.limit, Used: row.used, Reserved: row.reserved, Remaining: value, Uncertain: row.uncertain, Incomplete: row.incomplete}
}

func isModelBudgetKind(kind BudgetKind) bool {
	return kind == BudgetTokens || kind == BudgetCost || kind == BudgetElapsed
}

func validBudgetKind(kind BudgetKind) bool {
	for _, budget := range defaultRootBudgets {
		if budget.Kind == kind {
			return true
		}
	}
	return false
}

func budgetRemaining(row budgetRow) (int64, bool) {
	if !validBudgetKind(row.kind) || row.used < 0 || row.reserved < 0 || row.uncertain < 0 || row.used > math.MaxInt64-row.reserved || row.uncertain > math.MaxInt64-row.used-row.reserved {
		return 0, false
	}
	total := row.used + row.reserved + row.uncertain
	if row.limit == nil {
		return math.MaxInt64 - total, isModelBudgetKind(row.kind)
	}
	if *row.limit < 0 || !isModelBudgetKind(row.kind) && (total > *row.limit || row.uncertain != 0 || row.incomplete) {
		return 0, false
	}
	return max(*row.limit-total, 0), true
}

func loadBudgetRowsTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, kind BudgetKind) ([]budgetRow, error) {
	if rootID == "" || agentID == "" {
		return nil, ErrAgentAccess
	}
	query := `WITH RECURSIVE ancestors(id,parent_id) AS (
		SELECT id,parent_id FROM agents WHERE root_id=? AND id=?
		UNION
		SELECT a.id,a.parent_id FROM agents a JOIN ancestors p ON a.id=p.parent_id WHERE a.root_id=?
	) SELECT b.agent_id,b.kind,b.limit_value,b.used_value,b.reserved_value,b.uncertain_value,b.incomplete FROM budgets b
	WHERE b.root_id=? AND EXISTS(SELECT 1 FROM ancestors)
	AND (b.agent_id='' OR b.agent_id IN (SELECT id FROM ancestors))`
	args := []any{rootID, agentID, rootID, rootID}
	if kind != "" {
		query += ` AND b.kind=?`
		args = append(args, kind)
	}
	query += ` ORDER BY b.kind,b.agent_id`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var budgets []budgetRow
	for rows.Next() {
		var row budgetRow
		if err := rows.Scan(&row.agentID, &row.kind, &row.limit, &row.used, &row.reserved, &row.uncertain, &row.incomplete); err != nil {
			return nil, err
		}
		if _, valid := budgetRemaining(row); !valid {
			return nil, capability.ErrDenied
		}
		budgets = append(budgets, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	roots := make(map[BudgetKind]bool)
	for _, row := range budgets {
		if row.agentID == "" {
			roots[row.kind] = true
		}
	}
	for _, row := range budgets {
		if !roots[row.kind] {
			return nil, capability.ErrDenied
		}
	}
	if len(budgets) == 0 || kind == "" && len(roots) != len(defaultRootBudgets) {
		return nil, capability.ErrDenied
	}
	return budgets, nil
}

func validateBudgetReservations(reservations []capability.Reservation) error {
	return validateReservations(reservations, false)
}

func validateReservations(reservations []capability.Reservation, model bool) error {
	seen := make(map[string]struct{}, len(reservations))
	for _, reservation := range reservations {
		if !validBudgetKind(BudgetKind(reservation.Kind)) || reservation.Amount < 0 || reservation.Amount == 0 && (!model || !isModelBudgetKind(BudgetKind(reservation.Kind))) {
			return capability.ErrDenied
		}
		if _, duplicate := seen[reservation.Kind]; duplicate {
			return capability.ErrDenied
		}
		seen[reservation.Kind] = struct{}{}
	}
	return nil
}

func validateCapabilityBudgets(ctx context.Context, tx *sql.Tx, rootID, agentID string, reservations []capability.Reservation, reserved bool) error {
	if err := validateBudgetReservations(reservations); err != nil {
		return err
	}
	for _, reservation := range reservations {
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, BudgetKind(reservation.Kind))
		if err != nil {
			return err
		}
		for _, row := range rows {
			remaining, valid := budgetRemaining(row)
			if !valid {
				return capability.ErrDenied
			}
			if reserved {
				if row.reserved < reservation.Amount {
					return capability.ErrDenied
				}
			} else if reservation.Amount > remaining {
				return capability.ErrDenied
			}
		}
	}
	return nil
}

func reserveCapabilityBudgets(ctx context.Context, tx *sql.Tx, rootID, agentID string, reservations []capability.Reservation) error {
	return reserveBudgetRows(ctx, tx, rootID, agentID, reservations, false)
}

func reserveBudgetRows(ctx context.Context, tx *sql.Tx, rootID, agentID string, reservations []capability.Reservation, model bool) error {
	if err := validateReservations(reservations, model); err != nil {
		return err
	}
	stamp := now()
	for _, reservation := range reservations {
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, BudgetKind(reservation.Kind))
		if err != nil {
			return err
		}
		for _, row := range rows {
			if remaining, valid := budgetRemaining(row); !valid || reservation.Amount > remaining {
				scope := row.agentID
				if scope == "" {
					scope = "root tree"
				}
				return fmt.Errorf("%w: %s %s budget needs %s, remaining %s", capability.ErrDenied, scope, row.kind, FormatBudgetAmount(row.kind, reservation.Amount), FormatBudgetAmount(row.kind, remaining))
			}
			result, err := tx.ExecContext(ctx, `UPDATE budgets SET reserved_value=reserved_value+?,updated_at=?
				WHERE root_id=? AND agent_id=? AND kind=? AND ?<=?-used_value-reserved_value-uncertain_value`,
				reservation.Amount, stamp, rootID, row.agentID, reservation.Kind, reservation.Amount, math.MaxInt64)
			if err != nil {
				return err
			}
			if changed, err := result.RowsAffected(); err != nil || changed != 1 {
				if err != nil {
					return err
				}
				return capability.ErrDenied
			}
		}
	}
	return nil
}

func settleCapabilityBudgets(ctx context.Context, tx *sql.Tx, rootID, agentID string, reservations []capability.Reservation, actual []capability.Usage) error {
	return settleBudgetRows(ctx, tx, rootID, agentID, reservations, actual, false)
}

func settleBudgetRows(ctx context.Context, tx *sql.Tx, rootID, agentID string, reservations []capability.Reservation, actual []capability.Usage, model bool) error {
	if err := validateReservations(reservations, model); err != nil {
		return err
	}
	actualByKind := make(map[string]int64, len(actual))
	for _, usage := range actual {
		if usage.Kind == "" || usage.Amount < 0 {
			return capability.ErrDenied
		}
		if _, duplicate := actualByKind[usage.Kind]; duplicate {
			return capability.ErrDenied
		}
		actualByKind[usage.Kind] = usage.Amount
	}
	stamp := now()
	for _, reservation := range reservations {
		used, known := actualByKind[reservation.Kind]
		uncertain := int64(0)
		incomplete := model && isModelBudgetKind(BudgetKind(reservation.Kind)) && !known
		if incomplete {
			uncertain = reservation.Amount
		}
		if !known && !incomplete && (reservation.Consume || !isLiveBudgetKind(BudgetKind(reservation.Kind))) {
			used = reservation.Amount
		}
		if known && used > reservation.Amount && (!model || !isModelBudgetKind(BudgetKind(reservation.Kind))) {
			return capability.ErrDenied
		}
		delete(actualByKind, reservation.Kind)
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, BudgetKind(reservation.Kind))
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.reserved < reservation.Amount || used > math.MaxInt64-row.used || uncertain > math.MaxInt64-row.uncertain {
				return capability.ErrDenied
			}
			next := row
			next.reserved -= reservation.Amount
			next.used += used
			next.uncertain += uncertain
			if _, valid := budgetRemaining(next); !valid {
				return capability.ErrDenied
			}
			result, err := tx.ExecContext(ctx, `UPDATE budgets SET reserved_value=?,used_value=?,uncertain_value=?,incomplete=?,updated_at=?
    WHERE root_id=? AND agent_id=? AND kind=?`,
				next.reserved, next.used, next.uncertain, row.incomplete || incomplete, stamp, rootID, row.agentID, reservation.Kind)
			if err != nil {
				return err
			}
			if changed, err := result.RowsAffected(); err != nil || changed != 1 {
				if err != nil {
					return err
				}
				return capability.ErrDenied
			}
		}
	}
	if len(actualByKind) != 0 {
		return capability.ErrDenied
	}
	return nil
}

func isLiveBudgetKind(kind BudgetKind) bool {
	return kind == BudgetActiveOperations || isChildLiveBudgetKind(kind)
}

func isChildLiveBudgetKind(kind BudgetKind) bool {
	return kind == BudgetActiveChildren || kind == BudgetConcurrentChildTurns
}

func releaseCapabilityBudgets(ctx context.Context, tx *sql.Tx, rootID, agentID string, reservations []capability.Reservation) error {
	if err := validateBudgetReservations(reservations); err != nil {
		return err
	}
	stamp := now()
	for _, reservation := range reservations {
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, BudgetKind(reservation.Kind))
		if err != nil {
			return err
		}
		for _, row := range rows {
			result, err := tx.ExecContext(ctx, `UPDATE budgets SET reserved_value=reserved_value-?,updated_at=?
				WHERE root_id=? AND agent_id=? AND kind=? AND reserved_value>=?`,
				reservation.Amount, stamp, rootID, row.agentID, reservation.Kind, reservation.Amount)
			if err != nil {
				return err
			}
			if changed, err := result.RowsAffected(); err != nil || changed != 1 {
				if err != nil {
					return err
				}
				return capability.ErrDenied
			}
		}
	}
	return nil
}

// ReserveModelBudget admits a transport attempt. Zero model estimates are valid
// for known-free or unpriced calls; finite monetary caps require known pricing.
func (s *Store) ReserveModelBudget(ctx context.Context, rootID, agentID string, reservations []capability.Reservation, unpriced bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if unpriced {
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, BudgetCost)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.limit != nil {
				return fmt.Errorf("%w: model pricing is unavailable for a finite cost budget", capability.ErrDenied)
			}
		}
	}
	if err := reserveBudgetRows(ctx, tx, rootID, agentID, reservations, true); err != nil {
		return err
	}
	if unpriced {
		rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, BudgetCost)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, `UPDATE budgets SET incomplete=1 WHERE root_id=? AND agent_id=? AND kind=?`, rootID, row.agentID, BudgetCost); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ReconcileModelBudget records known usage, including estimate overages, and
// retains unknown usage as conservative exposure rather than known spend.
func (s *Store) ReconcileModelBudget(ctx context.Context, rootID, agentID string, reservations []capability.Reservation, actual []capability.Usage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := settleBudgetRows(ctx, tx, rootID, agentID, reservations, actual, true); err != nil {
		return err
	}
	return tx.Commit()
}

// FormatBudgetAmount gives budget diagnostics units without losing precision.
func FormatBudgetAmount(kind BudgetKind, amount int64) string {
	switch kind {
	case BudgetCost:
		return fmt.Sprintf("$%d.%06d", amount/1_000_000, amount%1_000_000)
	case BudgetElapsed:
		return fmt.Sprintf("%d ms", amount)
	case BudgetTokens:
		return fmt.Sprintf("%d tokens", amount)
	default:
		return strconv.FormatInt(amount, 10)
	}
}
