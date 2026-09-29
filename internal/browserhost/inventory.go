package browserhost

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

type pendingInventory struct {
	provider *provider
	owner    Identity
	targets  map[string]string
	ctx      context.Context
	result   chan InventoryResult
	settled  bool
}

func (h *Host) ListTabs(ctx context.Context, owner Identity) ([]Tab, error) {
	if !owner.valid() {
		return nil, errors.New("invalid browser inventory owner")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h.mu.Lock()
	v, e := h.destinationLocked(owner.RootID)
	if e != nil {
		h.mu.Unlock()
		return nil, e
	}
	if v.offer.Version < 2 {
		h.mu.Unlock()
		return nil, &Failure{Kind: "unsupported_operation", Message: "tab discovery requires a v2 desktop provider"}
	}
	if len(h.inventory) >= MaxPending {
		h.mu.Unlock()
		return nil, ErrBusy
	}
	if e = h.startLocked(v.peer); e != nil {
		h.mu.Unlock()
		return nil, e
	}
	targets := map[string]string{}
	if owner.RootID == owner.AgentID {
		for _, t := range v.offer.Tabs {
			targets[t.TabID] = t.TabGeneration
		}
	}
	for id, t := range v.created {
		if t.owner == owner.AgentID {
			targets[id] = t.tab.TabGeneration
		}
	}
	for _, a := range v.attachments {
		if a.value.Owner == owner && a.live && !a.value.Delegated && a.ctx.Err() == nil {
			targets[a.value.Scope.TabID] = a.value.Scope.TabGeneration
		}
	}
	req := InventoryRequest{RequestID: rand.Text(), Identity: owner, Binding: v.binding, Tabs: []InventoryTarget{}}
	for id, gen := range targets {
		req.Tabs = append(req.Tabs, InventoryTarget{TabID: id, TabGeneration: gen})
	}
	slices.SortFunc(req.Tabs, func(a, b InventoryTarget) int { return stringsCompare(a.TabID, b.TabID) })
	q := &pendingInventory{provider: v, owner: owner, targets: targets, ctx: ctx, result: make(chan InventoryResult, 1)}
	h.inventory[req.RequestID] = q
	sent := v.peer.notifyLocked(Notification{Inventory: &req})
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.inventory, req.RequestID)
		v.peer.queue = slices.DeleteFunc(v.peer.queue, func(n queued) bool {
			if n.value.Inventory != nil && n.value.Inventory.RequestID == req.RequestID {
				v.peer.bytes -= n.bytes
				return true
			}
			return false
		})
		h.mu.Unlock()
		h.finish(v.peer)
	}()
	if !sent {
		return nil, ErrClosed
	}
	var result InventoryResult
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-v.ctx.Done():
		return nil, ErrStale
	case result = <-q.result:
	}
	if result.Error != nil {
		return nil, result.Error
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.currentLocked(v) {
		return nil, ErrStale
	}
	present := map[string]bool{}
	for i := range result.Tabs {
		t := &result.Tabs[i]
		present[t.TabID] = true
		t.AttachmentID = ""
		for _, a := range v.attachments {
			if a.value.Scope.TabID == t.TabID && a.value.Scope.TabGeneration == t.TabGeneration && a.live && !a.value.Delegated && a.ctx.Err() == nil {
				t.State = "busy"
				t.Requestable = false
				if a.value.Owner == owner {
					t.State = "attached"
					t.AttachmentID = a.value.Scope.AttachmentID
				}
			}
		}
	}
	for id := range targets {
		if c, ok := v.created[id]; ok && c.owner == owner.AgentID && !present[id] {
			delete(v.created, id)
		}
	}
	if result.Tabs == nil {
		result.Tabs = []Tab{}
	}
	slices.SortFunc(result.Tabs, func(a, b Tab) int { return stringsCompare(a.TabID, b.TabID) })
	return result.Tabs, nil
}

func (p *Peer) SettleInventory(result InventoryResult) error {
	raw, e := json.Marshal(result)
	if e != nil || len(raw) > 64<<10 || len(result.Tabs) > 72 || !validFailure(result.Error) {
		return errors.New("inventory result exceeds bounds")
	}
	var copyResult InventoryResult
	if json.Unmarshal(raw, &copyResult) != nil {
		return errors.New("invalid inventory result")
	}
	result = copyResult
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.inventory[result.RequestID]
	if p.closed || q == nil || q.settled || q.ctx.Err() != nil || q.provider.peer != p || q.owner.RootID != result.RootID || q.provider.binding.ProviderEpoch != result.ProviderEpoch || !h.currentLocked(q.provider) {
		return ErrStale
	}
	if result.Error != nil && len(result.Tabs) > 0 {
		return errors.New("failed inventory cannot contain tabs")
	}
	seen := map[string]bool{}
	for i := range result.Tabs {
		t := &result.Tabs[i]
		if q.targets[t.TabID] != t.TabGeneration || t.TabGeneration == "" || seen[t.TabID] || !textBound(t.DocumentRevision, 128) || !textBound(t.URL, 8192) || !textBound(t.Title, 512) || (t.State != "available" && t.State != "busy") {
			return errors.New("unrequested or invalid inventory tab")
		}
		seen[t.TabID] = true
		t.AttachmentID = ""
		t.URL = cleanURL(t.URL)
		t.Title = cleanTitle(t.Title)
	}
	q.settled = true
	q.result <- result
	return nil
}
