package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/protocol"
)

type browserInventoryPending struct {
	lease    *browserLease
	identity browser.DesktopIdentity
	targets  map[string]string
	done     <-chan struct{}
	result   chan protocol.BrowserInventoryResultParams
}

func (p *browserProviders) ListTabs(ctx context.Context, identity browser.DesktopIdentity) (browser.DesktopResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p.mu.Lock()
	lease, err := p.destinationLocked(identity.RootID)
	if err != nil {
		p.mu.Unlock()
		return browser.DesktopResult{}, err
	}
	if lease.offer.Version < 2 {
		p.mu.Unlock()
		return browser.DesktopResult{}, browserFailure("unsupported_operation", "Tab discovery requires a current Desktop provider. Release and select it again after updating.")
	}
	if len(p.inventory) >= 64 {
		p.mu.Unlock()
		return browser.DesktopResult{}, browserFailure("browser_busy", "Browser discovery capacity reached")
	}
	targets := map[string]string{}
	if identity.AgentID == identity.RootID {
		for _, tab := range lease.offer.OfferedTabs {
			targets[tab.TabID] = tab.TabGeneration
		}
	}
	for id, tab := range lease.created {
		if tab.owner == identity.AgentID {
			targets[id] = tab.offer.TabGeneration
		}
	}
	for _, a := range lease.attachments {
		if a.identity == identity && a.live && !a.delegated && a.ctx.Err() == nil {
			targets[a.scope.TabID] = a.scope.TabGeneration
		}
	}
	request := protocol.BrowserInventoryRequest{RequestID: browserID(), RootID: identity.RootID, AgentID: identity.AgentID, ProviderID: lease.id, ProviderEpoch: lease.epoch, Tabs: []protocol.BrowserInventoryTarget{}}
	for id, generation := range targets {
		request.Tabs = append(request.Tabs, protocol.BrowserInventoryTarget{TabID: id, TabGeneration: generation})
	}
	slices.SortFunc(request.Tabs, func(a, b protocol.BrowserInventoryTarget) int { return strings.Compare(a.TabID, b.TabID) })
	pending := &browserInventoryPending{lease: lease, identity: identity, targets: targets, done: ctx.Done(), result: make(chan protocol.BrowserInventoryResultParams, 1)}
	p.inventory[request.RequestID] = pending
	delivered := lease.holder.notify("browser.inventory", request)
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.inventory, request.RequestID); p.mu.Unlock() }()
	if !delivered {
		return browser.DesktopResult{}, browserFailure("desktop_unavailable", "Desktop inventory transport unavailable")
	}
	select {
	case <-ctx.Done():
		return browser.DesktopResult{}, ctx.Err()
	case <-lease.ctx.Done():
		return browser.DesktopResult{}, browserFailure("desktop_unavailable", "Desktop provider disconnected")
	case result := <-pending.result:
		if result.Error != nil {
			return browser.DesktopResult{}, result.Error
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		current, err := p.destinationLocked(identity.RootID)
		if err != nil {
			return browser.DesktopResult{}, err
		}
		if current != lease {
			return browser.DesktopResult{}, browserFailure("desktop_unavailable", "Desktop destination changed during discovery")
		}
		if result.Tabs == nil {
			result.Tabs = []browser.DesktopTab{}
		}
		present := map[string]bool{}
		for i := range result.Tabs {
			tab := &result.Tabs[i]
			present[tab.TabID] = true
			tab.AttachmentID = ""
			for _, a := range lease.attachments {
				if a.scope.TabID == tab.TabID && a.scope.TabGeneration == tab.TabGeneration && a.live && !a.delegated && a.ctx.Err() == nil {
					tab.Requestable = false
					tab.State = "busy"
					if a.identity == identity {
						tab.AttachmentID = a.scope.AttachmentID
						tab.State = "attached"
					}
				}
			}
		}
		for id := range targets {
			if created, ok := lease.created[id]; ok && created.owner == identity.AgentID && !present[id] {
				delete(lease.created, id)
			}
		}
		return browser.DesktopResult{Availability: "available", Tabs: result.Tabs, SupportedOperations: []string{"list_tabs", "open", "attach"}, Media: []string{}}, nil
	}
}

func (p *browserProviders) settleInventory(c *serverConn, result protocol.BrowserInventoryResultParams) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.inventory[result.RequestID]
	if pending == nil || pending.lease.holder != c || pending.lease.epoch != result.ProviderEpoch || pending.identity.RootID != result.RootID || pending.lease.ctx.Err() != nil {
		return errors.New("inventory result does not match a live provider request")
	}
	select {
	case <-pending.done:
		return errors.New("inventory request expired")
	default:
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > 64<<10 || len(result.Tabs) > 72 {
		return errors.New("inventory result exceeds its limit")
	}
	seen := map[string]bool{}
	for i := range result.Tabs {
		tab := &result.Tabs[i]
		if pending.targets[tab.TabID] != tab.TabGeneration || tab.TabGeneration == "" || seen[tab.TabID] || len(tab.URL) > 8192 || len(tab.Title) > 512 || len(tab.DocumentRevision) > 128 {
			return errors.New("inventory result contains an unrequested or invalid tab")
		}
		if tab.State != "available" && tab.State != "busy" {
			return errors.New("invalid native inventory state")
		}
		seen[tab.TabID] = true
		tab.AttachmentID = "" // Only the broker can return the calling agent's handle.
		tab.Title = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, tab.Title)
		if address, err := url.Parse(tab.URL); err == nil {
			address.User = nil
			tab.URL = address.String()
		} else {
			tab.URL = ""
		}
	}
	select {
	case pending.result <- result:
		return nil
	default:
		return errors.New("inventory request already settled")
	}
}
