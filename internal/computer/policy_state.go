package computer

import "slices"

// PolicyState is an immutable host-side view of application access decisions.
type PolicyState struct {
	DefaultDeny                                    bool
	Allowed, Denied, SessionAllowed, SessionDenied []string
}

func (p *Policy) State() PolicyState {
	p.mu.Lock()
	defer p.mu.Unlock()
	names := func(values map[string]bool) []string {
		out := make([]string, 0, len(values))
		for name, enabled := range values {
			if enabled {
				out = append(out, name)
			}
		}
		slices.Sort(out)
		return out
	}
	return PolicyState{DefaultDeny: p.DefaultDeny, Allowed: names(p.allow), Denied: names(p.deny), SessionAllowed: names(p.sessionAllow), SessionDenied: names(p.sessionDeny)}
}
