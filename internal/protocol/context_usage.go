package protocol

import "github.com/context-labs/whip/internal/session"

type ModelContextEvidence struct {
	SessionID           ID       `json:"session_id"`
	ConfigRevision      Counter  `json:"config_revision"`
	HistoryRevision     Counter  `json:"history_revision"`
	ContextRevision     Counter  `json:"context_revision"`
	ThroughSequence     Counter  `json:"through_sequence"`
	EstimatedTokens     Counter  `json:"estimated_tokens"`
	ContextWindowTokens *Counter `json:"context_window_tokens"`
}

type ContextPrefill struct {
	AttemptID           ID             `json:"attempt_id"`
	TurnID              ID             `json:"turn_id"`
	Model               ModelSelection `json:"model"`
	ThroughSequence     Counter        `json:"through_sequence"`
	InputTokens         Counter        `json:"input_tokens"`
	InputSource         string         `json:"input_source" enum:"reported,estimated"`
	ContextWindowTokens *Counter       `json:"context_window_tokens"`
	Stale               bool           `json:"stale"`
}

type ContextUsage struct {
	SessionID         ID              `json:"session_id"`
	ConfigRevision    Counter         `json:"config_revision"`
	HistoryRevision   Counter         `json:"history_revision"`
	ContextRevision   Counter         `json:"context_revision"`
	ThroughSequence   Counter         `json:"through_sequence"`
	Basis             string          `json:"basis" enum:"latest_prefill"`
	UnavailableReason string          `json:"unavailable_reason" enum:",no_evidence,configuration_changed,history_changed,selection_changed"`
	Prefill           *ContextPrefill `json:"prefill"`
}

func modelContextEvidence(v *session.ModelContextEvidence) *ModelContextEvidence {
	if v == nil {
		return nil
	}
	return &ModelContextEvidence{SessionID: ID(v.SessionID), ConfigRevision: Counter(v.ConfigRevision), HistoryRevision: Counter(v.HistoryRevision), ContextRevision: Counter(v.ContextRevision), ThroughSequence: Counter(v.ThroughSequence), EstimatedTokens: Counter(v.EstimatedTokens), ContextWindowTokens: counter(v.ContextWindowTokens)}
}

func ContextUsageFromDomain(v session.ContextUsage) ContextUsage {
	result := ContextUsage{SessionID: ID(v.SessionID), ConfigRevision: Counter(v.ConfigRevision), HistoryRevision: Counter(v.HistoryRevision), ContextRevision: Counter(v.ContextRevision), ThroughSequence: Counter(v.ThroughSequence), Basis: "latest_prefill", UnavailableReason: v.UnavailableReason}
	if p := v.Prefill; p != nil {
		selection := p.Model.Clone()
		result.Prefill = &ContextPrefill{AttemptID: ID(p.AttemptID), TurnID: ID(p.TurnID), Model: ModelSelection{Provider: ID(selection.Provider), Name: selection.Name, Effort: selection.Effort, Temperature: selection.Temperature, TopP: selection.TopP}, ThroughSequence: Counter(p.ThroughSequence), InputTokens: Counter(p.InputTokens), InputSource: p.InputSource, ContextWindowTokens: counter(p.ContextWindowTokens), Stale: p.Stale}
	}
	return result
}
