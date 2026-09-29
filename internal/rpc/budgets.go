package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchBudget(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "usage.get":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.Usage(ctx, session.SessionID(p.SessionID))
			return protocol.UsageFromDomain(value), err
		})
	case "budgets.list":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			values, err := r.Budgets(ctx, session.SessionID(p.SessionID))
			if err != nil {
				return nil, err
			}
			result := protocol.BudgetsResult{Items: []protocol.Budget{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.BudgetFromDomain(value))
			}
			return result, nil
		})
	case "budgets.set":
		return decode(raw, func(p protocol.SetBudgetParams) (any, error) {
			value, err := r.SetBudget(ctx, session.SessionID(p.SessionID), int64(p.ExpectedRevision), p.Budget.Domain())
			return protocol.BudgetFromDomain(value), err
		})
	default:
		return nil, ErrMethod
	}
}
