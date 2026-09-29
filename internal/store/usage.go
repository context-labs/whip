package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/context-labs/whip/internal/session"
)

const usageAttemptLimit = 1_000_000

// Usage reads scalar accounting evidence in constant memory at one SQL snapshot.
// Both time and scan work are bounded; exceeding either returns no partial sum.
func (s *Store) Usage(ctx context.Context, owner session.SessionID) (session.Usage, error) {
	if err := session.ValidateID(string(owner)); err != nil {
		return session.Usage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return session.Usage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)", owner).Scan(&exists); err != nil {
		return session.Usage{}, err
	}
	if !exists {
		return session.Usage{}, ErrNotFound
	}
	result, err := readUsage(ctx, tx, owner, usageAttemptLimit)
	if err != nil {
		return session.Usage{}, err
	}
	if err := tx.Commit(); err != nil {
		return session.Usage{}, err
	}
	return result, nil
}

const usageColumns = `a.state,a.dispatched_at IS NOT NULL,a.finished_at IS NOT NULL,
 a.cost_source,a.cost_nano_usd,json_extract(a.result,'$.usage.input'),json_extract(a.result,'$.usage.output'),
 json_extract(a.result,'$.usage.reasoning'),json_extract(a.result,'$.usage.cached_input'),
 json_extract(a.result,'$.usage.cached_output'),json_extract(a.result,'$.elapsed_millis')`

func readUsage(ctx context.Context, q querier, owner session.SessionID, limit int) (session.Usage, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+usageColumns+`,0,0
 FROM attempt_budget_ancestors b JOIN model_attempts a ON a.id=b.attempt_id
 WHERE b.session_id=? ORDER BY b.attempt_id LIMIT ?`, owner, limit+1)
	if err != nil {
		return session.Usage{}, err
	}
	value, err := readUsageRows(rows, owner, limit)
	return value.Usage, err
}

func readUsageRows(rows *sql.Rows, owner session.SessionID, limit int) (session.TurnUsage, error) {
	defer func() { _ = rows.Close() }()
	result := session.TurnUsage{Usage: session.Usage{SessionID: owner}}
	usage := &result.Usage
	count := 0
	for rows.Next() {
		if count == limit {
			return session.TurnUsage{}, fmt.Errorf("%w: usage exceeds the attempt scan limit", ErrLimit)
		}
		count++
		var state, source string
		var dispatched, finished, compaction, committed bool
		var cost, input, output, reasoning, cachedInput, cachedOutput, elapsed *int64
		if err := rows.Scan(&state, &dispatched, &finished, &source, &cost, &input, &output, &reasoning, &cachedInput, &cachedOutput, &elapsed, &compaction, &committed); err != nil {
			return session.TurnUsage{}, err
		}
		if compaction {
			addAttemptState(&result.CompactionAttempts, state, dispatched, finished)
		}
		if committed {
			result.Compactions++
		}
		if !addAttemptState(&usage.Attempts, state, dispatched, finished) {
			continue
		}
		switch {
		case source == "provider" && cost != nil:
			addUsageCost(&usage.ReportedCost, *cost)
		case source == "prices" && cost != nil:
			addUsageCost(&usage.EstimatedCost, *cost)
		default:
			usage.UnknownCost++
		}
		for _, field := range []struct {
			total *session.UsageQuantity
			value *int64
		}{
			{&usage.InputTokens, input},
			{&usage.OutputTokens, output},
			{&usage.ReasoningTokens, reasoning},
			{&usage.CachedInput, cachedInput},
			{&usage.CachedOutput, cachedOutput},
			{&usage.ElapsedMillis, elapsed},
		} {
			if field.value == nil {
				field.total.MissingAttempts++
			} else {
				field.total.KnownAttempts++
				addUsageValue(&field.total.Value, &field.total.Overflow, *field.value)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return session.TurnUsage{}, err
	}
	return result, nil
}

func addUsageCost(total *session.UsageCost, value int64) {
	total.Attempts++
	addUsageValue(&total.Value, &total.Overflow, value)
}

func addUsageValue(total *int64, overflow *bool, value int64) {
	if value < 0 || value > math.MaxInt64-*total {
		*total, *overflow = math.MaxInt64, true
	} else {
		*total += value
	}
}

func addAttemptState(count *session.UsageAttempts, state string, dispatched, finished bool) bool {
	if !finished {
		if dispatched {
			count.InFlight++
		} else {
			count.Reserved++
		}
		return false
	}
	if !dispatched {
		count.NotDispatched++
		return false
	}
	count.Settled++
	if state == string(session.AttemptUncertain) {
		count.Uncertain++
	}
	return true
}

// TurnUsage takes the same bounded scalar snapshot as Usage, but exact local
// turn provenance excludes child attempts and imported transcript history.
func (s *Store) TurnUsage(ctx context.Context, owner session.SessionID, turn session.TurnID) (session.TurnUsage, error) {
	if session.ValidateID(string(owner)) != nil || session.ValidateID(string(turn)) != nil {
		return session.TurnUsage{}, session.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return session.TurnUsage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM turns WHERE id=? AND session_id=?)", turn, owner).Scan(&exists); err != nil {
		return session.TurnUsage{}, err
	}
	if !exists {
		return session.TurnUsage{}, ErrNotFound
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+usageColumns+`,json_extract(a.request,'$.purpose')='compaction',EXISTS(SELECT 1 FROM compactions c WHERE c.attempt_id=a.id)
 FROM model_attempts a WHERE a.turn_id=? ORDER BY a.id LIMIT ?`, turn, usageAttemptLimit+1)
	if err != nil {
		return session.TurnUsage{}, err
	}
	result, err := readUsageRows(rows, owner, usageAttemptLimit)
	if err != nil {
		return session.TurnUsage{}, err
	}
	result.TurnID = turn
	if err = tx.Commit(); err != nil {
		return session.TurnUsage{}, err
	}
	return result, nil
}
