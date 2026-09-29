package session

// ModelContextScope identifies canonical context at a preparation boundary.
// It is evidence, not permission to reconstruct or dispatch a request.
type ModelContextScope struct {
	SessionID       SessionID `json:"session_id"`
	ConfigRevision  Revision  `json:"config_revision"`
	HistoryRevision Revision  `json:"history_revision"`
	ContextRevision int64     `json:"context_revision"`
	ThroughSequence int64     `json:"through_sequence"`
}

// ModelContextEvidence captures the actual planning heuristic and effective
// route capacity before dispatch. Neither is a token reservation or charge.
type ModelContextEvidence struct {
	ModelContextScope
	EstimatedTokens     int64  `json:"estimated_tokens"`
	ContextWindowTokens *int64 `json:"context_window_tokens"`
}

func (v ModelContextEvidence) Validate() error {
	if ValidateID(string(v.SessionID)) != nil || v.ConfigRevision < 1 || v.HistoryRevision < 1 || v.ContextRevision < 0 || v.ThroughSequence < 0 || v.EstimatedTokens < 0 {
		return ErrInvalid
	}
	if v.ContextWindowTokens != nil && (*v.ContextWindowTokens < 1 || *v.ContextWindowTokens > 1000000000) {
		return ErrInvalid
	}
	return nil
}

func (v ModelContextEvidence) Clone() ModelContextEvidence {
	if v.ContextWindowTokens != nil {
		v.ContextWindowTokens = new(*v.ContextWindowTokens)
	}
	return v
}

type ContextPrefill struct {
	AttemptID           ModelAttemptID
	TurnID              TurnID
	Model               ModelSelection
	ThroughSequence     int64
	InputTokens         int64
	InputSource         string
	ContextWindowTokens *int64
	Stale               bool
}

// ContextUsage is the latest ordinary prefill at a captured tail, never a
// cumulative cost or a claim about tokens appended after that request.
type ContextUsage struct {
	ModelContextScope
	UnavailableReason string
	Prefill           *ContextPrefill
}
