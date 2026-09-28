package session

import "fmt"

type ResourceKind string

const (
	ResourceDepth            ResourceKind = "depth"
	ResourceDescendants      ResourceKind = "descendants"
	ResourceQueuedInputs     ResourceKind = "queued_inputs"
	ResourceActiveOperations ResourceKind = "active_operations"
	ResourceSubscriptions    ResourceKind = "subscriptions"
	// MaxSessionDepth bounds ancestry traversal, including delegated authority.
	MaxSessionDepth = 128
)

// ResourceLimit caps reusable capacity in the owner's subtree. Nil removes a
// local cap; every ancestor still applies. A root must have a finite cap.
type ResourceLimit struct {
	Kind  ResourceKind `json:"kind"`
	Limit *int64       `json:"limit,string"`
}

// ResourceUsage is derived from the owning rows, never a mutable counter.
// Resources returns every scope from the requested session through its root.
type ResourceUsage struct {
	SessionID SessionID
	Kind      ResourceKind
	Revision  int64
	Limit     *int64
	Used      int64
}

func ResourceKinds() []ResourceKind {
	return []ResourceKind{ResourceDepth, ResourceDescendants, ResourceQueuedInputs, ResourceActiveOperations, ResourceSubscriptions}
}

func DefaultResourceLimits() []ResourceLimit {
	return []ResourceLimit{
		{ResourceDepth, new(int64(8))},
		{ResourceDescendants, new(int64(127))},
		{ResourceQueuedInputs, new(int64(256))},
		{ResourceActiveOperations, new(int64(64))},
		{ResourceSubscriptions, new(int64(1000))},
	}
}

func (limit ResourceLimit) Validate() error {
	switch limit.Kind {
	case ResourceDepth, ResourceDescendants, ResourceQueuedInputs, ResourceActiveOperations, ResourceSubscriptions:
	default:
		return fmt.Errorf("%w: unsupported resource kind", ErrInvalid)
	}
	if limit.Limit != nil && (*limit.Limit < 0 || (limit.Kind == ResourceDepth && *limit.Limit > MaxSessionDepth)) {
		return fmt.Errorf("%w: resource limit outside supported bounds", ErrInvalid)
	}
	return nil
}

func ValidateResourceLimits(limits []ResourceLimit) error {
	seen := make(map[ResourceKind]bool, len(limits))
	for _, limit := range limits {
		if err := limit.Validate(); err != nil {
			return err
		}
		if seen[limit.Kind] {
			return fmt.Errorf("%w: duplicate resource kind", ErrInvalid)
		}
		seen[limit.Kind] = true
	}
	return nil
}

// ResolveResourceLimits copies finite defaults and explicit overrides for a new
// root. The resulting records are persisted once; host changes do not alter them.
func ResolveResourceLimits(defaults, overrides []ResourceLimit) ([]ResourceLimit, error) {
	for _, limits := range [][]ResourceLimit{defaults, overrides} {
		if err := ValidateResourceLimits(limits); err != nil {
			return nil, err
		}
		for _, limit := range limits {
			if limit.Limit == nil {
				return nil, fmt.Errorf("%w: root resource limits must be finite", ErrInvalid)
			}
		}
	}
	result := DefaultResourceLimits()
	for _, limits := range [][]ResourceLimit{defaults, overrides} {
		for _, limit := range limits {
			for i := range result {
				if result[i].Kind == limit.Kind {
					result[i].Limit = new(*limit.Limit)
				}
			}
		}
	}
	return result, nil
}
