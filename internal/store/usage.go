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

func readUsage(ctx context.Context, q querier, owner session.SessionID, limit int) (session.Usage, error) {
	rows, err := q.QueryContext(ctx, `SELECT a.state,a.dispatched_at IS NOT NULL,a.finished_at IS NOT NULL,
 a.cost_source,a.cost_nano_usd,json_extract(a.result,'$.usage.input'),json_extract(a.result,'$.usage.output'),
 json_extract(a.result,'$.usage.reasoning'),json_extract(a.result,'$.usage.cached_input'),
 json_extract(a.result,'$.usage.cached_output'),json_extract(a.result,'$.elapsed_millis')
 FROM attempt_budget_ancestors b JOIN model_attempts a ON a.id=b.attempt_id
 WHERE b.session_id=? ORDER BY b.attempt_id LIMIT ?`, owner, limit+1)
	if err != nil {
		return session.Usage{}, err
	}
	defer func() { _ = rows.Close() }()
	result := session.Usage{SessionID: owner}
	count := 0
	for rows.Next() {
		if count == limit {
			return session.Usage{}, fmt.Errorf("%w: usage exceeds the attempt scan limit", ErrLimit)
		}
		count++
		var state, source string
		var dispatched, finished bool
		var cost, input, output, reasoning, cachedInput, cachedOutput, elapsed *int64
		if err := rows.Scan(&state, &dispatched, &finished, &source, &cost, &input, &output, &reasoning, &cachedInput, &cachedOutput, &elapsed); err != nil {
			return session.Usage{}, err
		}
		if !finished {
			if dispatched {
				result.Attempts.InFlight++
			} else {
				result.Attempts.Reserved++
			}
			continue
		}
		if !dispatched {
			result.Attempts.NotDispatched++
			continue
		}
		result.Attempts.Settled++
		if state == string(session.AttemptUncertain) {
			result.Attempts.Uncertain++
		}
		switch {
		case source == "provider" && cost != nil:
			addUsageCost(&result.ReportedCost, *cost)
		case source == "prices" && cost != nil:
			addUsageCost(&result.EstimatedCost, *cost)
		default:
			result.UnknownCost++
		}
		for _, field := range []struct {
			total *session.UsageQuantity
			value *int64
		}{
			{&result.InputTokens, input},
			{&result.OutputTokens, output},
			{&result.ReasoningTokens, reasoning},
			{&result.CachedInput, cachedInput},
			{&result.CachedOutput, cachedOutput},
			{&result.ElapsedMillis, elapsed},
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
		return session.Usage{}, err
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
