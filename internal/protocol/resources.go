package protocol

import "github.com/context-labs/whip/internal/session"

type ResourceLimit struct {
	Kind  string   `json:"kind" enum:"depth,descendants,queued_inputs,active_operations,subscriptions,runnable_descendants,schedules"`
	Limit *Counter `json:"limit"`
}

type ResourceUsage struct {
	SessionID ID       `json:"session_id"`
	Kind      string   `json:"kind" enum:"depth,descendants,queued_inputs,active_operations,subscriptions,runnable_descendants,schedules"`
	Revision  Counter  `json:"revision"`
	Limit     *Counter `json:"limit"`
	Used      Counter  `json:"used"`
}

type ResourcesResult struct {
	Items []ResourceUsage `json:"items"`
}

type SetResourceParams struct {
	SessionID        ID            `json:"session_id"`
	ExpectedRevision Counter       `json:"expected_revision"`
	Resource         ResourceLimit `json:"resource"`
}

func (value ResourceLimit) Domain() session.ResourceLimit {
	result := session.ResourceLimit{Kind: session.ResourceKind(value.Kind)}
	if value.Limit != nil {
		result.Limit = new(int64(*value.Limit))
	}
	return result
}

// ResourceLimitsDomain preserves order and duplicates for domain validation.
func ResourceLimitsDomain(values []ResourceLimit) []session.ResourceLimit {
	result := make([]session.ResourceLimit, len(values))
	for i, value := range values {
		result[i] = value.Domain()
	}
	return result
}

func ResourceUsageFromDomain(value session.ResourceUsage) ResourceUsage {
	return ResourceUsage{SessionID: ID(value.SessionID), Kind: string(value.Kind), Revision: Counter(value.Revision), Limit: counter(value.Limit), Used: Counter(value.Used)}
}
