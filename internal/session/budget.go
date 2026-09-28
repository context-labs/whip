package session

import "fmt"

type BudgetKind string

const (
	BudgetModelCalls         BudgetKind = "model_calls"
	BudgetModelTokens        BudgetKind = "model_tokens"
	BudgetModelCostNanoUSD   BudgetKind = "model_cost_nano_usd"
	BudgetModelElapsedMillis BudgetKind = "model_elapsed_millis"
	BudgetLogicalWrites      BudgetKind = "logical_writes"
	BudgetLogicalWriteBytes  BudgetKind = "logical_write_bytes"
)

// BudgetLimit is a local cap. Nil removes only this cap; every live ancestor's
// cap still applies. Root logical-write caps must remain finite.
type BudgetLimit struct {
	Kind  BudgetKind `json:"kind"`
	Limit *int64     `json:"limit,string"`
}

// DefaultWriteBudgets gives each new root fresh finite storage limits. Imported
// history never copies the source's cumulative usage or remaining allowance.
func DefaultWriteBudgets() []BudgetLimit {
	return []BudgetLimit{
		{Kind: BudgetLogicalWrites, Limit: new(int64(100_000))},
		{Kind: BudgetLogicalWriteBytes, Limit: new(int64(1 << 30))},
	}
}

// Budget projects model attempts or committed logical writes.
// Incomplete means some exposure cannot be quantified within the int64 range.
type Budget struct {
	SessionID  SessionID
	Kind       BudgetKind
	Revision   int64
	Limit      *int64
	Used       int64
	Reserved   int64
	Uncertain  int64
	Incomplete bool
}

func (b BudgetLimit) Validate() error {
	switch b.Kind {
	case BudgetModelCalls, BudgetModelTokens, BudgetModelCostNanoUSD, BudgetModelElapsedMillis,
		BudgetLogicalWrites, BudgetLogicalWriteBytes:
	default:
		return fmt.Errorf("%w: unsupported budget kind", ErrInvalid)
	}
	if b.Limit != nil && *b.Limit < 0 {
		return fmt.Errorf("%w: negative budget limit", ErrInvalid)
	}
	return nil
}
