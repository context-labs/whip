package session

// UsageQuantity sums only fields actually reported by settled dispatched
// attempts. Overflow marks Value as a saturated lower bound, never an exact sum.
type UsageQuantity struct {
	Value           int64
	KnownAttempts   int64
	MissingAttempts int64
	Overflow        bool
}

type UsageCost struct {
	Value    int64
	Attempts int64
	Overflow bool
}

type UsageAttempts struct {
	Reserved      int64
	InFlight      int64
	Settled       int64
	NotDispatched int64
	Uncertain     int64
}

// Usage projects immutable captured budget ancestry. A root includes every
// descendant's original attempts, even after that descendant has been deleted.
// It neither copies fork-source usage nor charges imported message provenance.
type Usage struct {
	SessionID       SessionID
	Attempts        UsageAttempts
	ReportedCost    UsageCost
	EstimatedCost   UsageCost
	UnknownCost     int64
	InputTokens     UsageQuantity
	OutputTokens    UsageQuantity
	ReasoningTokens UsageQuantity
	CachedInput     UsageQuantity
	CachedOutput    UsageQuantity
	ElapsedMillis   UsageQuantity
}

// TurnUsage covers only this turn's actual attempts, including its helpers.
// Compactions counts committed derived summaries, even after context undo.
type TurnUsage struct {
	TurnID             TurnID
	Usage              Usage
	CompactionAttempts UsageAttempts
	Compactions        int64
}
