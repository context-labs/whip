package daemon

import (
	"errors"

	"github.com/context-labs/whip/internal/protocol"
)

// Available providers are inert, connection-bound candidates. A durable approved
// operation promotes exactly one; connection order and focus never select it.
func (p *browserProviders) leasesLocked() []*browserLease {
	leases := make([]*browserLease, 0, len(p.roots)+len(p.available))
	for _, lease := range p.roots {
		leases = append(leases, lease)
	}
	for _, lease := range p.available {
		leases = append(leases, lease)
	}
	return leases
}

func (p *browserProviders) destinationLocked(rootID string) (*browserLease, error) {
	if lease := p.roots[rootID]; lease != nil && lease.ctx.Err() == nil {
		return lease, nil
	}
	var candidate *browserLease
	for key, lease := range p.available {
		if key.rootID != rootID || lease.ctx.Err() != nil {
			continue
		}
		if candidate != nil {
			return nil, browserFailure("desktop_selection_required", "Multiple Desktop windows serve this conversation. Explicitly offer a Browser tab from the intended window, then retry.")
		}
		candidate = lease
	}
	if candidate == nil {
		return nil, browserFailure("desktop_unavailable", "No connected Desktop is serving this conversation")
	}
	return candidate, nil
}

func (p *browserProviders) claimLocked(lease *browserLease) error {
	current, err := p.destinationLocked(lease.rootID)
	if err != nil {
		return err
	}
	if current != lease {
		return errors.New("browser destination changed before approval")
	}
	p.roots[lease.rootID] = lease
	delete(p.available, browserReleaseKey{holder: lease.holder, rootID: lease.rootID})
	return nil
}

func (p *browserProviders) removeLeaseLocked(lease *browserLease) {
	if p.roots[lease.rootID] == lease {
		delete(p.roots, lease.rootID)
	}
	key := browserReleaseKey{holder: lease.holder, rootID: lease.rootID}
	if p.available[key] == lease {
		delete(p.available, key)
	}
}

type browserCreatedTab struct {
	owner string
	offer protocol.BrowserOfferedTab
}
