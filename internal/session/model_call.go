package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

var ErrModelCallConflict = errors.New("model call identity conflicts with stored accounting")

type ModelCallReservation struct {
	ID        string
	MaxTokens int
	Timeout   time.Duration
}

type ModelCallSettlement struct {
	Exhausted bool
}

// ModelCallEvent describes one attempt admission or accounting outcome.
// It contains neither provider credentials nor request/response content.
type ModelCallEvent struct {
	ID            string `json:"id"`
	LogicalID     string `json:"logical_id"`
	Number        int    `json:"number"`
	Purpose       string `json:"purpose"`
	UsageSource   string `json:"usage_source"`
	CostSource    string `json:"cost_source"`
	Tokens        int64  `json:"tokens,string"`
	CostMicros    int64  `json:"cost_micros,string"`
	ElapsedMillis int64  `json:"elapsed_millis,string"`
	Exhausted     bool   `json:"exhausted"`
}

// ModelAccounting keeps token-usage completeness separate from cost provenance.
// Reported cost comes from the provider; estimates use the call's saved rates.
// EstimatedCalls counts attempts without complete token usage. Their reserve
// estimates live in BudgetState.Uncertain, not known token or cost totals.
type ModelAccounting struct {
	RootID              string `json:"root_id"`
	AgentID             string `json:"agent_id"`
	Scope               string `json:"scope"`
	Revision            int64  `json:"revision,string"`
	ReportedCostMicros  int64  `json:"reported_cost_micros,string"`
	EstimatedCostMicros int64  `json:"estimated_cost_micros,string"`
	ReportedCostCalls   int64  `json:"reported_cost_calls,string"`
	EstimatedCostCalls  int64  `json:"estimated_cost_calls,string"`
	UnknownCostCalls    int64  `json:"unknown_cost_calls,string"`
	ReportedCalls       int64  `json:"reported_calls,string"`
	EstimatedCalls      int64  `json:"estimated_calls,string"`
	PendingCalls        int64  `json:"pending_calls,string"`
}

type modelBudgetReservation struct {
	AgentID string     `json:"agent_id"`
	Kind    BudgetKind `json:"kind"`
	Amount  int64      `json:"amount,string"`
}

type storedModelCall struct {
	ID, RootID, AgentID, Status string
	Attempt                     llm.ModelAttempt
	Reservation                 ModelCallReservation
	Budgets                     []modelBudgetReservation
	Result                      []byte
	Exhausted                   bool
	Contributions               map[BudgetKind]modelContribution
}

// modelContribution is the amount this call placed in each budget. Unknown
// consumption remains separate from measured usage, including unknown zero cost.
type modelContribution struct {
	Used       int64 `json:"used,string"`
	Uncertain  int64 `json:"uncertain,string"`
	Incomplete bool  `json:"incomplete"`
}

// CheckModelWork stops new host effects when this agent or an ancestor has
// exhausted its model budget. Reservations alone do not stop admitted work.
func (s *Store) CheckModelWork(ctx context.Context, rootID, agentID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, "")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if modelBudgetExhausted(row) {
			return capability.ErrDenied
		}
	}
	return tx.Commit()
}

// Free calls remain possible at an explicitly zero monetary cap. Token and
// elapsed exhaustion still stop work; all arithmetic has been validated first.
func modelBudgetExhausted(row budgetRow) bool {
	if row.limit == nil {
		return false
	}
	spent := row.used + row.uncertain
	switch row.kind {
	case BudgetTokens, BudgetElapsed:
		return spent >= *row.limit
	case BudgetCost:
		return spent > *row.limit || spent == *row.limit && *row.limit > 0
	default:
		return false
	}
}

// AdmitModelCall reserves one wire attempt, including its finite deadline. The
// saved row identifies each ancestor reservation so later cap changes cannot
// release another attempt's capacity during settlement.
func (s *Store) AdmitModelCall(ctx context.Context, rootID, agentID string, attempt llm.ModelAttempt) (ModelCallReservation, error) {
	if attempt.LogicalID == "" || attempt.Number < 1 || attempt.Model == "" || attempt.InputTokens < 0 || attempt.MaxTokens < 1 || attempt.Timeout <= 0 {
		return ModelCallReservation{}, errors.New("model admission requires call identity, model, nonnegative input, positive output, and timeout")
	}
	encoded, err := json.Marshal(attempt)
	if err != nil {
		return ModelCallReservation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelCallReservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	agent, err := loadAgentTx(ctx, tx, rootID, agentID)
	if err != nil {
		return ModelCallReservation{}, err
	}
	if isTerminalAgentStatus(agent.Status) {
		return ModelCallReservation{}, ErrAgentTerminal
	}
	rootAgentID, err := rootAgentIDTx(ctx, tx, rootID)
	if err != nil {
		return ModelCallReservation{}, err
	}
	if rootAgentID != agentID {
		rootAgent, err := loadAgentTx(ctx, tx, rootID, rootAgentID)
		if err != nil {
			return ModelCallReservation{}, err
		}
		if isTerminalAgentStatus(rootAgent.Status) {
			return ModelCallReservation{}, ErrRootTerminal
		}
	}
	var existing ModelCallReservation
	var previous []byte
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id,max_tokens,timeout_nanos,attempt,status FROM model_calls
		WHERE root_id=? AND agent_id=? AND logical_id=? AND attempt_number=?`, rootID, agentID, attempt.LogicalID, attempt.Number).
		Scan(&existing.ID, &existing.MaxTokens, &existing.Timeout, &previous, &status)
	if err == nil {
		if !bytes.Equal(previous, encoded) || status != "running" {
			return ModelCallReservation{}, ErrModelCallConflict
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ModelCallReservation{}, err
	}
	rows, err := loadBudgetRowsTx(ctx, tx, rootID, agentID, "")
	if err != nil {
		return ModelCallReservation{}, err
	}
	remaining := map[BudgetKind]int64{BudgetTokens: math.MaxInt64, BudgetCost: math.MaxInt64, BudgetElapsed: math.MaxInt64, BudgetActiveOperations: math.MaxInt64}
	for _, row := range rows {
		if _, tracked := remaining[row.kind]; !tracked {
			continue
		}
		available, valid := budgetRemaining(row)
		if !valid || row.kind == BudgetCost && row.limit != nil && row.used+row.uncertain > *row.limit {
			return ModelCallReservation{}, capability.ErrDenied
		}
		if row.kind == BudgetCost && row.limit != nil && !attempt.Pricing.Known() {
			return ModelCallReservation{}, fmt.Errorf("%w: model pricing is unavailable for a finite cost budget", capability.ErrDenied)
		}
		remaining[row.kind] = min(remaining[row.kind], available)
	}
	if attempt.InputTokens >= remaining[BudgetTokens] || remaining[BudgetElapsed] == 0 || remaining[BudgetActiveOperations] == 0 {
		return ModelCallReservation{}, capability.ErrDenied
	}
	maxTokens := min(int64(attempt.MaxTokens), remaining[BudgetTokens]-attempt.InputTokens)
	cost := int64(0)
	if attempt.Pricing.Known() {
		// Cost is monotone in output tokens. Binary search also handles fractional
		// microunit rates and free output without lossy division.
		low, high := int64(0), maxTokens
		for low < high {
			mid := low + (high-low)/2 + 1
			estimate, priceErr := attempt.Pricing.ReserveCost(attempt.InputTokens, mid)
			if priceErr != nil || estimate > remaining[BudgetCost] {
				high = mid - 1
			} else {
				low = mid
			}
		}
		maxTokens = low
		if maxTokens == 0 {
			return ModelCallReservation{}, capability.ErrDenied
		}
		cost, err = attempt.Pricing.ReserveCost(attempt.InputTokens, maxTokens)
		if err != nil {
			return ModelCallReservation{}, err
		}
	}
	timeout := attempt.Timeout
	if durationMillis(timeout) > remaining[BudgetElapsed] {
		// This multiplication is safe because remaining is below timeout's
		// representable duration, expressed in whole milliseconds.
		timeout = time.Duration(remaining[BudgetElapsed]) * time.Millisecond
	}
	amounts := map[BudgetKind]int64{BudgetTokens: attempt.InputTokens + maxTokens, BudgetCost: cost, BudgetElapsed: durationMillis(timeout), BudgetActiveOperations: 1}
	var reservations []modelBudgetReservation
	stamp := now()
	for _, row := range rows {
		amount, tracked := amounts[row.kind]
		if !tracked {
			continue
		}
		reserved, err := addModelAmount(row.reserved, amount)
		if err != nil {
			return ModelCallReservation{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE budgets SET reserved_value=?,updated_at=? WHERE root_id=? AND agent_id=? AND kind=?`,
			reserved, stamp, rootID, row.agentID, row.kind); err != nil {
			return ModelCallReservation{}, err
		}
		reservations = append(reservations, modelBudgetReservation{AgentID: row.agentID, Kind: row.kind, Amount: amount})
	}
	id, err := runtimeID()
	if err != nil {
		return ModelCallReservation{}, err
	}
	reservedJSON, err := json.Marshal(reservations)
	if err != nil {
		return ModelCallReservation{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO model_calls
		(id,root_id,agent_id,logical_id,attempt_number,status,attempt,max_tokens,timeout_nanos,reservations,created_at,updated_at)
		VALUES(?,?,?,?,?,'running',?,?,?,?,?,?)`, id, rootID, agentID, attempt.LogicalID, attempt.Number, encoded, maxTokens, timeout, reservedJSON, stamp, stamp); err != nil {
		return ModelCallReservation{}, err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "model.call.started", actorEvent{
		AgentID: agentID, Status: "running", ModelCall: &ModelCallEvent{
			ID: id, LogicalID: attempt.LogicalID, Number: attempt.Number, Purpose: attempt.Purpose,
			UsageSource: "none", CostSource: "none",
		},
	}, stamp); err != nil {
		return ModelCallReservation{}, err
	}
	permit := ModelCallReservation{ID: id, MaxTokens: int(maxTokens), Timeout: timeout}
	return permit, tx.Commit()
}

// SettleModelCall records the completed attempt even when actual usage exceeds
// its estimate or budget. Repeating the same durable ID and result is harmless.
func (s *Store) SettleModelCall(ctx context.Context, rootID, callID string, result llm.ModelAttemptResult) (ModelCallSettlement, error) {
	if result.Elapsed < 0 {
		return ModelCallSettlement{}, errors.New("model elapsed time cannot be negative")
	}
	if !result.Dispatched && (result.Usage.HasUsage() || result.Usage.Cost != nil) {
		return ModelCallSettlement{}, errors.New("undispatched model attempt cannot report usage")
	}
	encoded, err := encodeModelResult(result)
	if err != nil {
		return ModelCallSettlement{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelCallSettlement{}, err
	}
	defer func() { _ = tx.Rollback() }()
	call, err := loadModelCallTx(ctx, tx, rootID, callID)
	if err != nil {
		return ModelCallSettlement{}, err
	}
	if call.Status != "running" && call.Status != "interrupted" {
		if bytes.Equal(encoded, call.Result) {
			return ModelCallSettlement{Exhausted: call.Exhausted}, tx.Commit()
		}
		return ModelCallSettlement{}, ErrModelCallConflict
	}
	// Stop releases capacity before a cancelled provider necessarily returns.
	// Its estimate may be corrected once by that attempt's real outcome.
	status := "succeeded"
	if result.Failed || result.Usage.Validate() != nil {
		status = "failed"
	}
	if !result.Dispatched {
		status = "rejected"
	}
	settled, err := s.settleModelCallTx(ctx, tx, call, result, encoded, status)
	if err != nil {
		return ModelCallSettlement{}, err
	}
	return settled, tx.Commit()
}

func loadModelCallTx(ctx context.Context, tx *sql.Tx, rootID, callID string) (storedModelCall, error) {
	var call storedModelCall
	var attempt, reservations, contributions []byte
	err := tx.QueryRowContext(ctx, `SELECT id,root_id,agent_id,status,attempt,max_tokens,timeout_nanos,reservations,result,exhausted,contributions
		FROM model_calls WHERE root_id=? AND id=?`, rootID, callID).Scan(&call.ID, &call.RootID, &call.AgentID, &call.Status,
		&attempt, &call.Reservation.MaxTokens, &call.Reservation.Timeout, &reservations, &call.Result, &call.Exhausted,
		&contributions)
	if err != nil {
		return storedModelCall{}, err
	}
	call.Reservation.ID = call.ID
	if err := json.Unmarshal(attempt, &call.Attempt); err != nil {
		return storedModelCall{}, err
	}
	if err := json.Unmarshal(reservations, &call.Budgets); err != nil {
		return storedModelCall{}, err
	}
	if err := json.Unmarshal(contributions, &call.Contributions); err != nil {
		return storedModelCall{}, err
	}
	return call, nil
}

func (s *Store) settleModelCallTx(ctx context.Context, tx *sql.Tx, call storedModelCall, result llm.ModelAttemptResult, encoded []byte, status string) (ModelCallSettlement, error) {
	contributions := map[BudgetKind]modelContribution{BudgetElapsed: {Used: durationMillis(result.Elapsed)}}
	allowances := make(map[BudgetKind]int64, 4)
	for _, reservation := range call.Budgets {
		allowances[reservation.Kind] = reservation.Amount
	}
	usageSource, costSource := "none", "none"
	if result.Dispatched {
		usage := validModelUsage(result.Usage)
		usageSource = "estimated"
		contributions[BudgetTokens] = modelContribution{Uncertain: allowances[BudgetTokens], Incomplete: true}
		if usage.HasUsage() {
			usageSource = "reported"
			tokens, err := addModelAmount(int64(usage.PromptTokens), int64(usage.CompletionTokens))
			if err != nil {
				return ModelCallSettlement{}, err
			}
			contributions[BudgetTokens] = modelContribution{Used: tokens}
		}
		cost, known, err := call.Attempt.Pricing.ActualCost(usage)
		if err != nil {
			return ModelCallSettlement{}, err
		}
		costSource = "unknown"
		contributions[BudgetCost] = modelContribution{Uncertain: allowances[BudgetCost], Incomplete: true}
		if known {
			contributions[BudgetCost] = modelContribution{Used: cost}
			costSource = "estimated"
			if usage.Cost != nil {
				costSource = "reported"
			}
		}
	}
	if status == "interrupted" {
		// A crash/stop establishes no measured duration; reserve exposure is an
		// upper bound that the late transport result can replace exactly once.
		contributions[BudgetElapsed] = modelContribution{Uncertain: allowances[BudgetElapsed], Incomplete: true}
	}
	settled := ModelCallSettlement{}
	stamp := now()
	for _, reservation := range call.Budgets {
		var row budgetRow
		row.agentID, row.kind = reservation.AgentID, reservation.Kind
		if err := tx.QueryRowContext(ctx, `SELECT limit_value,used_value,reserved_value,uncertain_value,incomplete,model_incomplete FROM budgets WHERE root_id=? AND agent_id=? AND kind=?`,
			call.RootID, row.agentID, row.kind).Scan(&row.limit, &row.used, &row.reserved, &row.uncertain, &row.incomplete, &row.modelIncomplete); err != nil {
			return ModelCallSettlement{}, err
		}
		if _, valid := budgetRemaining(row); !valid {
			return ModelCallSettlement{}, capability.ErrDenied
		}
		release := reservation.Amount
		previous := modelContribution{}
		if call.Status == "interrupted" {
			release = 0
			previous = call.Contributions[row.kind]
		}
		next := contributions[row.kind]
		if row.reserved < release || release < 0 || row.used < previous.Used || previous.Used < 0 || row.uncertain < previous.Uncertain || previous.Uncertain < 0 {
			return ModelCallSettlement{}, errors.New("model reservation no longer held")
		}
		var err error
		row.used, err = addModelAmount(row.used-previous.Used, next.Used)
		if err != nil {
			return ModelCallSettlement{}, err
		}
		row.uncertain, err = addModelAmount(row.uncertain-previous.Uncertain, next.Uncertain)
		if err != nil {
			return ModelCallSettlement{}, err
		}
		row.reserved -= release
		if previous.Incomplete {
			if row.modelIncomplete == 0 {
				return ModelCallSettlement{}, errors.New("model uncertainty no longer held")
			}
			row.modelIncomplete--
		}
		if next.Incomplete {
			row.modelIncomplete, err = addModelAmount(row.modelIncomplete, 1)
			if err != nil {
				return ModelCallSettlement{}, err
			}
		}
		if _, valid := budgetRemaining(row); !valid {
			return ModelCallSettlement{}, capability.ErrDenied
		}
		if _, err := tx.ExecContext(ctx, `UPDATE budgets SET used_value=?,reserved_value=?,uncertain_value=?,model_incomplete=?,updated_at=? WHERE root_id=? AND agent_id=? AND kind=?`,
			row.used, row.reserved, row.uncertain, row.modelIncomplete, stamp, call.RootID, row.agentID, row.kind); err != nil {
			return ModelCallSettlement{}, err
		}
		settled.Exhausted = settled.Exhausted || modelBudgetExhausted(row)
	}
	storedContributions, err := json.Marshal(contributions)
	if err != nil {
		return ModelCallSettlement{}, err
	}
	tokens, cost, elapsed := contributions[BudgetTokens].Used, contributions[BudgetCost].Used, contributions[BudgetElapsed].Used
	if _, err := tx.ExecContext(ctx, `UPDATE model_calls SET status=?,result=?,usage_source=?,cost_source=?,tokens=?,cost_micros=?,elapsed_millis=?,contributions=?,exhausted=?,updated_at=?
  WHERE root_id=? AND id=? AND status=?`, status, encoded, usageSource, costSource, tokens, cost, elapsed, storedContributions, settled.Exhausted, stamp, call.RootID, call.ID, call.Status); err != nil {
		return ModelCallSettlement{}, err
	}
	eventKind := "model.call.settled"
	if call.Status == "interrupted" {
		eventKind = "model.call.corrected"
	}
	if _, err := s.insertActorEventTx(ctx, tx, call.RootID, eventKind, actorEvent{
		AgentID: call.AgentID, Status: status, ModelCall: &ModelCallEvent{
			ID: call.ID, LogicalID: call.Attempt.LogicalID, Number: call.Attempt.Number, Purpose: call.Attempt.Purpose,
			UsageSource: usageSource, CostSource: costSource, Tokens: tokens, CostMicros: cost,
			ElapsedMillis: elapsed, Exhausted: settled.Exhausted,
		},
	}, stamp); err != nil {
		return ModelCallSettlement{}, err
	}
	return settled, nil
}

// Provider token usage and charges have independent validity. Bad tokens do
// not erase a valid charge, and a bad charge does not erase valid token usage.
func validModelUsage(usage llm.Usage) llm.Usage {
	cost := usage.Cost
	usage.Cost = nil
	if usage.Validate() != nil {
		usage = llm.Usage{}
	}
	if cost != nil && *cost >= 0 && !math.IsNaN(*cost) && !math.IsInf(*cost, 0) {
		usage.Cost = cost
	}
	return usage
}

func encodeModelResult(result llm.ModelAttemptResult) ([]byte, error) {
	if result.Usage.Cost == nil || !math.IsNaN(*result.Usage.Cost) && !math.IsInf(*result.Usage.Cost, 0) {
		return json.Marshal(result)
	}
	// JSON cannot encode these malformed provider values. Preserve their class
	// in the durable identity so a retry is still distinct from absent cost.
	invalidCost := strconv.FormatFloat(*result.Usage.Cost, 'g', -1, 64)
	result.Usage.Cost = nil
	return json.Marshal(struct {
		Result      llm.ModelAttemptResult `json:"result"`
		InvalidCost string                 `json:"invalid_cost"`
	}{Result: result, InvalidCost: invalidCost})
}

func durationMillis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration-1)/time.Millisecond) + 1
}

func addModelAmount(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, errors.New("model accounting exceeds integer range")
	}
	return left + right, nil
}

// ModelAccounting returns either one agent or its complete descendant tree.
// Empty agentID selects the whole root and is always a tree scope.
func (s *Store) ModelAccounting(ctx context.Context, rootID, agentID string, subtree bool) (ModelAccounting, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ModelAccounting{}, err
	}
	defer func() { _ = tx.Rollback() }()
	accounting, err := modelAccountingTx(ctx, tx, rootID, agentID, subtree)
	if err != nil {
		return ModelAccounting{}, err
	}
	return accounting, tx.Commit()
}

func modelAccountingTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, subtree bool) (ModelAccounting, error) {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE root_id=?`, rootID).Scan(&revision); err != nil {
		return ModelAccounting{}, err
	}
	wholeRoot := agentID == ""
	if agentID == "" {
		var err error
		agentID, err = rootAgentIDTx(ctx, tx, rootID)
		if errors.Is(err, sql.ErrNoRows) {
			// A newly created session can be snapshotted before its runtime
			// authority exists. It has no model calls yet.
			if err := tx.QueryRowContext(ctx, `SELECT id FROM sessions WHERE id=?`, rootID).Scan(&agentID); err != nil {
				return ModelAccounting{}, err
			}
			return ModelAccounting{RootID: rootID, AgentID: agentID, Scope: "subtree", Revision: revision}, nil
		}
		if err != nil {
			return ModelAccounting{}, err
		}
		subtree = true
	} else if _, err := loadAgentTx(ctx, tx, rootID, agentID); err != nil {
		return ModelAccounting{}, err
	}
	accounting := ModelAccounting{RootID: rootID, AgentID: agentID, Scope: "agent", Revision: revision}
	query := `SELECT status,usage_source,cost_source,cost_micros FROM model_calls WHERE root_id=? AND agent_id=?`
	args := []any{rootID, agentID}
	if subtree {
		accounting.Scope = "subtree"
		query = subtreeCTE + `SELECT status,usage_source,cost_source,cost_micros FROM model_calls WHERE root_id=? AND agent_id IN (SELECT id FROM subtree)`
		args = []any{rootID, agentID, rootID, rootID}
	}
	if wholeRoot {
		query = `SELECT status,usage_source,cost_source,cost_micros FROM model_calls WHERE root_id=?`
		args = []any{rootID}
	}
	// SUM over integer costs retains exact integer arithmetic and rejects an
	// overflow. Ordinary + expressions in SQLite could instead promote to REAL.
	query = `SELECT
		COALESCE(SUM(CASE WHEN cost_source='reported' THEN cost_micros ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN cost_source='estimated' THEN cost_micros ELSE 0 END),0),
		COUNT(CASE WHEN cost_source='reported' THEN 1 END),
		COUNT(CASE WHEN cost_source='estimated' THEN 1 END),
		COUNT(CASE WHEN cost_source='unknown' THEN 1 END),
		COUNT(CASE WHEN usage_source='reported' THEN 1 END),
		COUNT(CASE WHEN usage_source='estimated' THEN 1 END),
		COUNT(CASE WHEN status='running' THEN 1 END) FROM (` + query + `)`
	err := tx.QueryRowContext(ctx, query, args...).Scan(&accounting.ReportedCostMicros, &accounting.EstimatedCostMicros,
		&accounting.ReportedCostCalls, &accounting.EstimatedCostCalls,
		&accounting.UnknownCostCalls, &accounting.ReportedCalls, &accounting.EstimatedCalls, &accounting.PendingCalls)
	return accounting, err
}

func (s *Store) settleInterruptedModelCallsTx(ctx context.Context, tx *sql.Tx, rootID, targetAgentID string) error {
	query := `SELECT root_id,id FROM model_calls WHERE status='running'`
	var args []any
	if targetAgentID != "" {
		query = subtreeCTE + `SELECT root_id,id FROM model_calls WHERE root_id=? AND agent_id IN (SELECT id FROM subtree) AND status='running'`
		args = []any{rootID, targetAgentID, rootID, rootID}
	} else if rootID != "" {
		query += ` AND root_id=?`
		args = []any{rootID}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type identity struct{ root, id string }
	var calls []identity
	for rows.Next() {
		var call identity
		if err := rows.Scan(&call.root, &call.id); err != nil {
			return err
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range calls {
		call, err := loadModelCallTx(ctx, tx, id.root, id.id)
		if err != nil {
			return err
		}
		result := llm.ModelAttemptResult{Dispatched: true, Failed: true, Elapsed: call.Reservation.Timeout}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := s.settleModelCallTx(ctx, tx, call, result, encoded, "interrupted"); err != nil {
			return fmt.Errorf("recover model call %s: %w", call.ID, err)
		}
		if _, err := s.insertActorEventTx(ctx, tx, call.RootID, "model.call.interrupted", actorEvent{
			AgentID: call.AgentID, Status: "interrupted", Error: "model call interrupted; usage is uncertain until its outcome is known",
		}, now()); err != nil {
			return err
		}
	}
	return nil
}

// Creating a new child cap is safe while model attempts are running because
// they settle only the ancestors recorded at admission. Generic reservations
// still prohibit that change: they do not retain an exact ancestor identity.
func heldModelReservationsTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, kind BudgetKind) (int64, error) {
	if !isModelBudgetKind(kind) {
		return 0, nil
	}
	var held int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(json_extract(reservation.value,'$.amount') AS INTEGER)),0)
 FROM model_calls, json_each(model_calls.reservations) AS reservation
 WHERE root_id=? AND status='running' AND json_extract(reservation.value,'$.agent_id')=? AND json_extract(reservation.value,'$.kind')=?`, rootID, agentID, kind).Scan(&held)
	return held, err
}

// A cap cannot bound an existing budget's spend while an unpriced call remains
// unresolved. New child rows govern subsequent calls, so only saved ancestors
// of each attempt participate in this check.
func requireBoundedModelCostTx(ctx context.Context, tx *sql.Tx, rootID, agentID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT attempt FROM model_calls AS c
 WHERE c.root_id=? AND (c.status='running' OR c.cost_source='unknown')
 AND EXISTS (SELECT 1 FROM json_each(c.reservations) AS r
 WHERE json_extract(r.value,'$.agent_id')=? AND json_extract(r.value,'$.kind')='cost')`, rootID, agentID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return err
		}
		var attempt llm.ModelAttempt
		if err := json.Unmarshal(encoded, &attempt); err != nil {
			return err
		}
		if !attempt.Pricing.Known() {
			return fmt.Errorf("%w: unresolved model cost has no known price", capability.ErrDenied)
		}
	}
	return rows.Err()
}
