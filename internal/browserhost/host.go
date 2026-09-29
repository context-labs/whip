package browserhost

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"sync"
)

type Host struct {
	mu          sync.Mutex
	closed      bool
	peers       map[*Peer]struct{}
	selected    map[string]*provider
	available   map[bindingKey]*provider
	pending     map[string]*pendingCommand
	inventory   map[string]*pendingInventory
	uploadBytes int
	eventBytes  int
	jobs        int
	wg          sync.WaitGroup
}
type bindingKey struct {
	peer *Peer
	root string
}
type Peer struct {
	host         *Host
	ctx          context.Context
	cancel       context.CancelFunc
	closed       bool
	queue        []queued
	bytes        int
	ready        chan struct{}
	released     map[string]string
	releaseOrder []string
	wg           sync.WaitGroup
}
type queued struct {
	value Notification
	bytes int
}
type provider struct {
	peer        *Peer
	offer       Offer
	binding     Binding
	ctx         context.Context
	cancel      context.CancelFunc
	attachments map[string]*attachment
	tabs        map[string]*tabQueue
	created     map[string]createdTab
}
type createdTab struct {
	owner string
	tab   OfferedTab
}
type attachment struct {
	provider *provider
	value    Attachment
	ctx      context.Context
	cancel   context.CancelFunc
	tab      *tabQueue
	live     bool
	opening  bool
	parent   *attachment
	sequence uint64
	batch    *Batch
}

func New() *Host {
	return &Host{peers: map[*Peer]struct{}{}, selected: map[string]*provider{}, available: map[bindingKey]*provider{}, pending: map[string]*pendingCommand{}, inventory: map[string]*pendingInventory{}}
}

// OpenPeer creates no provider association. The caller owns Close on disconnect.
func (h *Host) OpenPeer() (*Peer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	if len(h.peers) >= MaxPeers {
		return nil, ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Peer{host: h, ctx: ctx, cancel: cancel, ready: make(chan struct{}, 1), released: map[string]string{}}
	h.peers[p] = struct{}{}
	return p, nil
}
func (p *Peer) Lifetime() context.Context { return p.ctx }
func (p *Peer) Close()                    { h := p.host; h.mu.Lock(); h.retirePeerLocked(p); h.mu.Unlock(); p.wg.Wait() }

func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	for p := range h.peers {
		h.retirePeerLocked(p)
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *Host) retirePeerLocked(p *Peer) {
	if p.closed {
		return
	}
	p.closed = true
	p.cancel()
	delete(h.peers, p)
	p.queue = nil
	p.bytes = 0
	for _, v := range h.providersLocked() {
		if v.peer == p {
			h.retireProviderLocked(v, "disconnected", false)
		}
	}
	clear(p.released)
	p.releaseOrder = nil
}

func (h *Host) providersLocked() []*provider {
	out := make([]*provider, 0, len(h.selected)+len(h.available))
	for _, v := range h.selected {
		out = append(out, v)
	}
	for _, v := range h.available {
		out = append(out, v)
	}
	return out
}

func (h *Host) destinationLocked(root string) (*provider, error) {
	if p := h.selected[root]; p != nil && p.ctx.Err() == nil {
		return p, nil
	}
	var candidate *provider
	for k, v := range h.available {
		if k.root != root || v.ctx.Err() != nil {
			continue
		}
		if candidate != nil {
			return nil, errors.New("explicit browser destination selection required")
		}
		candidate = v
	}
	if candidate == nil {
		return nil, ErrClosed
	}
	return candidate, nil
}

func (h *Host) currentLocked(p *provider) bool {
	v, e := h.destinationLocked(p.offer.RootID)
	return e == nil && v == p && p.ctx.Err() == nil
}

func (h *Host) claimLocked(p *provider) error {
	if !h.currentLocked(p) {
		return ErrStale
	}
	h.selected[p.offer.RootID] = p
	delete(h.available, bindingKey{p.peer, p.offer.RootID})
	return nil
}

func (h *Host) retireProviderLocked(v *provider, reason string, notify bool) {
	v.cancel()
	if h.selected[v.offer.RootID] == v {
		delete(h.selected, v.offer.RootID)
	}
	key := bindingKey{v.peer, v.offer.RootID}
	if h.available[key] == v {
		delete(h.available, key)
	}
	for _, a := range v.attachments {
		a.cancel()
		a.live = false
	}
	if notify {
		v.peer.notifyLocked(Notification{Revoked: &Revoked{RootID: v.offer.RootID, Binding: v.binding, Reason: reason}})
	}
}

// Bind is an explicit human association. Runtime must validate that RootID is an
// existing root; this connection core does not infer or access session storage.
func (p *Peer) Bind(offer Offer) (Binding, error) {
	if e := validateOffer(offer); e != nil {
		return Binding{}, e
	}
	offer = cloneOffer(offer)
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.closed || h.closed {
		return Binding{}, ErrClosed
	}
	key := bindingKey{p, offer.RootID}
	old := h.selected[offer.RootID]
	ownCandidate := h.available[key]
	if !offer.Availability && old == nil {
		old = ownCandidate
	}
	if offer.Availability {
		old = h.available[key]
		if selected := h.selected[offer.RootID]; selected != nil && selected.peer == p {
			return Binding{}, errors.New("connection already has a selected browser destination")
		}
	}
	if old != nil && offer.ExpectedProviderEpoch != old.binding.ProviderEpoch {
		return Binding{}, ErrStale
	}
	if old == nil && offer.ExpectedProviderEpoch != "" {
		return Binding{}, ErrStale
	}
	count := 0
	for _, v := range h.providersLocked() {
		if v.peer == p {
			count++
		}
	}
	if old == nil && (count >= 64 || len(h.selected)+len(h.available) >= MaxBindings) {
		return Binding{}, ErrBusy
	}
	ctx, cancel := context.WithCancel(p.ctx)
	v := &provider{peer: p, offer: offer, binding: Binding{Version: offer.Version, ProviderID: rand.Text(), ProviderEpoch: rand.Text()}, ctx: ctx, cancel: cancel, attachments: map[string]*attachment{}, tabs: map[string]*tabQueue{}, created: map[string]createdTab{}}
	if old != nil {
		h.retireProviderLocked(old, "replaced", true)
	}
	if !offer.Availability && ownCandidate != nil && ownCandidate != old {
		h.retireProviderLocked(ownCandidate, "replaced", true)
	}
	// A revocation notification overflowing the old peer may retire this same peer.
	if p.closed {
		cancel()
		return Binding{}, ErrClosed
	}
	if offer.Availability {
		h.available[key] = v
	} else {
		h.selected[offer.RootID] = v
	}
	return v.binding, nil
}

func (p *Peer) Unbind(root, epoch string) error {
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if p.released[root] == epoch && epoch != "" {
		return nil
	}
	v := h.selected[root]
	if candidate := h.available[bindingKey{p, root}]; candidate != nil && candidate.binding.ProviderEpoch == epoch {
		v = candidate
	}
	if v == nil || v.peer != p || v.binding.ProviderEpoch != epoch {
		return ErrStale
	}
	h.retireProviderLocked(v, "unbound", true)
	if p.closed {
		return ErrClosed
	}
	if i := slices.Index(p.releaseOrder, root); i >= 0 {
		p.releaseOrder = slices.Delete(p.releaseOrder, i, i+1)
	}
	if len(p.releaseOrder) >= 128 {
		delete(p.released, p.releaseOrder[0])
		p.releaseOrder = p.releaseOrder[1:]
	}
	p.released[root] = epoch
	p.releaseOrder = append(p.releaseOrder, root)
	return nil
}

func (p *Peer) notifyLocked(n Notification) bool {
	if p.closed {
		return false
	}
	raw, e := json.Marshal(n)
	if e != nil || len(p.queue) >= 64 || len(raw) > maxOutboundBytes-p.bytes {
		p.host.retirePeerLocked(p)
		return false
	}
	// Copy through JSON so a public consumer cannot mutate captured authority.
	var copyValue Notification
	if json.Unmarshal(raw, &copyValue) != nil {
		return false
	}
	p.queue = append(p.queue, queued{copyValue, len(raw)})
	p.bytes += len(raw)
	select {
	case p.ready <- struct{}{}:
	default:
	}
	return true
}

func (p *Peer) Next(ctx context.Context) (Notification, error) {
	for {
		p.host.mu.Lock()
		if p.closed {
			p.host.mu.Unlock()
			return Notification{}, ErrClosed
		}
		if len(p.queue) > 0 {
			q := p.queue[0]
			p.queue[0] = queued{}
			p.queue = p.queue[1:]
			p.bytes -= q.bytes
			p.host.mu.Unlock()
			return q.value, nil
		}
		p.host.mu.Unlock()
		select {
		case <-ctx.Done():
			return Notification{}, ctx.Err()
		case <-p.ctx.Done():
			return Notification{}, ErrClosed
		case <-p.ready:
		}
	}
}

func (h *Host) startLocked(p *Peer) error {
	if h.closed || p.closed {
		return ErrClosed
	}
	if h.jobs >= MaxPending {
		return ErrBusy
	}
	h.jobs++
	h.wg.Add(1)
	p.wg.Add(1)
	return nil
}
func (h *Host) finish(p *Peer) { h.mu.Lock(); h.jobs--; h.mu.Unlock(); p.wg.Done(); h.wg.Done() }
func (h *Host) revokeLocked(a *attachment) {
	retired := []Scope{}
	for _, candidate := range a.provider.attachments {
		if candidate.ctx.Err() != nil {
			continue
		}
		for current := candidate; current != nil; current = current.parent {
			if current == a {
				candidate.cancel()
				candidate.live = false
				retired = append(retired, cloneScope(candidate.value.Scope))
				break
			}
		}
	}
	if len(retired) > 0 {
		slices.SortFunc(retired, func(a, b Scope) int { return stringsCompare(a.AttachmentID, b.AttachmentID) })
		a.provider.peer.notifyLocked(Notification{Retired: &Retired{RootID: a.value.Owner.RootID, Binding: a.provider.binding, Scopes: retired}})
	}
}

// RevokeLineage retires every owner in the lineage, including active descendants.
// It never closes the human page or removes its preview route.
func (h *Host) RevokeLineage(root, resource string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range h.providersLocked() {
		if v.offer.RootID != root {
			continue
		}
		for _, a := range v.attachments {
			if a.value.Scope.Resource() == resource {
				h.revokeLocked(a)
			}
		}
	}
}

func (h *Host) RevokeOwner(owner Identity) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range h.providersLocked() {
		if v.offer.RootID != owner.RootID {
			continue
		}
		for _, a := range v.attachments {
			if a.value.Owner == owner {
				h.revokeLocked(a)
			}
		}
	}
}

func (h *Host) Attachments(owner Identity) []Attachment {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Attachment{}
	if v := h.selected[owner.RootID]; v != nil {
		for _, a := range v.attachments {
			if a.value.Owner == owner && a.live && !a.value.Delegated && a.ctx.Err() == nil {
				out = append(out, cloneAttachment(a.value))
			}
		}
	}
	slices.SortFunc(out, func(a, b Attachment) int { return stringsCompare(a.Scope.AttachmentID, b.Scope.AttachmentID) })
	return out
}

func stringsCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (h *Host) pruneLocked(v *provider) {
	for id, a := range v.attachments {
		if a.ctx.Err() != nil {
			delete(v.attachments, id)
		}
	}
	for id, t := range v.tabs {
		used := false
		for _, a := range v.attachments {
			if a.tab == t {
				used = true
				break
			}
		}
		if !used && t.idle() {
			delete(v.tabs, id)
		}
	}
}

func (h *Host) capacityLocked(v *provider) error {
	h.pruneLocked(v)
	count := 0
	for _, a := range v.attachments {
		if !a.value.Delegated {
			count++
		}
	}
	if count >= MaxAttachments || len(v.attachments) >= MaxRecords {
		return ErrBusy
	}
	return nil
}

func (h *Host) openCapacityLocked(v *provider) bool {
	count := len(v.created)
	for _, a := range v.attachments {
		if a.opening && a.ctx.Err() == nil {
			count++
		}
	}
	return count < 32
}
