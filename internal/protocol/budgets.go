package protocol

import "github.com/context-labs/whip/internal/session"

type BudgetLimit struct {
	Kind  string   `json:"kind" enum:"model_calls,model_tokens,model_cost_nano_usd,model_elapsed_millis"`
	Limit *Counter `json:"limit"`
}

type Budget struct {
	SessionID  ID       `json:"session_id"`
	Kind       string   `json:"kind" enum:"model_calls,model_tokens,model_cost_nano_usd,model_elapsed_millis"`
	Revision   Counter  `json:"revision"`
	Limit      *Counter `json:"limit"`
	Used       Counter  `json:"used"`
	Reserved   Counter  `json:"reserved"`
	Uncertain  Counter  `json:"uncertain"`
	Incomplete bool     `json:"incomplete"`
}

type BudgetsResult struct {
	Items []Budget `json:"items"`
}

type SetBudgetParams struct {
	SessionID        ID          `json:"session_id"`
	ExpectedRevision Counter     `json:"expected_revision"`
	Budget           BudgetLimit `json:"budget"`
}

func (value BudgetLimit) Domain() session.BudgetLimit {
	result := session.BudgetLimit{Kind: session.BudgetKind(value.Kind)}
	if value.Limit != nil {
		result.Limit = new(int64(*value.Limit))
	}
	return result
}

func BudgetFromDomain(value session.Budget) Budget {
	return Budget{SessionID: ID(value.SessionID), Kind: string(value.Kind), Revision: Counter(value.Revision), Limit: counter(value.Limit), Used: Counter(value.Used), Reserved: Counter(value.Reserved), Uncertain: Counter(value.Uncertain), Incomplete: value.Incomplete}
}
