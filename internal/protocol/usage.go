package protocol

import "github.com/context-labs/whip/internal/session"

type UsageQuantity struct {
	Value           Counter `json:"value"`
	KnownAttempts   Counter `json:"known_attempts"`
	MissingAttempts Counter `json:"missing_attempts"`
	Overflow        bool    `json:"overflow"`
}

type UsageCost struct {
	Value    Counter `json:"value"`
	Attempts Counter `json:"attempts"`
	Overflow bool    `json:"overflow"`
}

type UsageAttempts struct {
	Reserved      Counter `json:"reserved"`
	InFlight      Counter `json:"in_flight"`
	Settled       Counter `json:"settled"`
	NotDispatched Counter `json:"not_dispatched"`
	Uncertain     Counter `json:"uncertain"`
}

type Usage struct {
	SessionID       ID            `json:"session_id"`
	Attempts        UsageAttempts `json:"attempts"`
	ReportedCost    UsageCost     `json:"reported_cost"`
	EstimatedCost   UsageCost     `json:"estimated_cost"`
	UnknownCost     Counter       `json:"unknown_cost"`
	InputTokens     UsageQuantity `json:"input_tokens"`
	OutputTokens    UsageQuantity `json:"output_tokens"`
	ReasoningTokens UsageQuantity `json:"reasoning_tokens"`
	CachedInput     UsageQuantity `json:"cached_input"`
	CachedOutput    UsageQuantity `json:"cached_output"`
	ElapsedMillis   UsageQuantity `json:"elapsed_millis"`
}

func UsageFromDomain(value session.Usage) Usage {
	quantity := func(v session.UsageQuantity) UsageQuantity {
		return UsageQuantity{Value: Counter(v.Value), KnownAttempts: Counter(v.KnownAttempts), MissingAttempts: Counter(v.MissingAttempts), Overflow: v.Overflow}
	}
	cost := func(v session.UsageCost) UsageCost {
		return UsageCost{Value: Counter(v.Value), Attempts: Counter(v.Attempts), Overflow: v.Overflow}
	}
	return Usage{
		SessionID: ID(value.SessionID), Attempts: usageAttempts(value.Attempts),
		ReportedCost: cost(value.ReportedCost), EstimatedCost: cost(value.EstimatedCost), UnknownCost: Counter(value.UnknownCost),
		InputTokens: quantity(value.InputTokens), OutputTokens: quantity(value.OutputTokens), ReasoningTokens: quantity(value.ReasoningTokens),
		CachedInput: quantity(value.CachedInput), CachedOutput: quantity(value.CachedOutput), ElapsedMillis: quantity(value.ElapsedMillis),
	}
}

type TurnUsageParams struct {
	SessionID ID `json:"session_id"`
	TurnID    ID `json:"turn_id"`
}
type TurnUsage struct {
	TurnID             ID            `json:"turn_id"`
	Usage              Usage         `json:"usage"`
	CompactionAttempts UsageAttempts `json:"compaction_attempts"`
	Compactions        Counter       `json:"compactions"`
}

func TurnUsageFromDomain(value session.TurnUsage) TurnUsage {
	return TurnUsage{TurnID: ID(value.TurnID), Usage: UsageFromDomain(value.Usage), CompactionAttempts: usageAttempts(value.CompactionAttempts), Compactions: Counter(value.Compactions)}
}

func usageAttempts(a session.UsageAttempts) UsageAttempts {
	return UsageAttempts{Reserved: Counter(a.Reserved), InFlight: Counter(a.InFlight), Settled: Counter(a.Settled), NotDispatched: Counter(a.NotDispatched), Uncertain: Counter(a.Uncertain)}
}
