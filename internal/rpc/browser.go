package rpc

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func browserPreviewToDomain(p *protocol.BrowserPreviewScope) *browserhost.Preview {
	if p == nil {
		return nil
	}
	return &browserhost.Preview{HostID: string(p.HostID), HostIdentity: string(p.HostIdentity), ConnectionGeneration: string(p.ConnectionGeneration), EnvironmentID: string(p.EnvironmentID), Loopback: p.Loopback, Ports: slices.Clone(p.Ports)}
}

func browserPreview(p *browserhost.Preview) *protocol.BrowserPreviewScope {
	if p == nil {
		return nil
	}
	return &protocol.BrowserPreviewScope{HostID: protocol.BrowserToken(p.HostID), HostIdentity: protocol.BrowserToken(p.HostIdentity), ConnectionGeneration: protocol.BrowserToken(p.ConnectionGeneration), EnvironmentID: protocol.BrowserToken(p.EnvironmentID), Loopback: p.Loopback, Ports: append([]int{}, p.Ports...)}
}

func browserScope(s browserhost.Scope) protocol.BrowserScope {
	return protocol.BrowserScope{ProviderID: protocol.BrowserToken(s.ProviderID), ProviderEpoch: protocol.BrowserToken(s.ProviderEpoch), TabID: protocol.BrowserToken(s.TabID), TabGeneration: protocol.BrowserToken(s.TabGeneration), ProfileID: protocol.BrowserToken(s.ProfileID), ControlLineage: protocol.BrowserToken(s.ControlLineage), AttachmentID: protocol.BrowserToken(s.AttachmentID), AttachmentGeneration: protocol.BrowserToken(s.AttachmentGeneration), Preview: browserPreview(s.Preview)}
}

func browserFailureToDomain(p *protocol.BrowserFailure) *browserhost.Failure {
	if p == nil {
		return nil
	}
	return &browserhost.Failure{Kind: p.Kind, Message: p.Message}
}

func browserTab(tab browserhost.Tab) protocol.BrowserTab {
	value := protocol.BrowserTab{TabID: protocol.BrowserToken(tab.TabID), TabGeneration: protocol.BrowserToken(tab.TabGeneration), DocumentRevision: tab.DocumentRevision, URL: tab.URL, Title: tab.Title, State: tab.State, Requestable: tab.Requestable}
	if tab.AttachmentID != "" {
		value.AttachmentID = new(protocol.BrowserToken(tab.AttachmentID))
	}
	return value
}

func dispatchBrowserRead(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	return decode(raw, func(p protocol.SessionParams) (any, error) {
		if method == "browser.tabs" {
			tabs, err := r.BrowserTabs(ctx, session.SessionID(p.SessionID))
			result := protocol.BrowserTabsResult{Tabs: []protocol.BrowserTab{}}
			for _, tab := range tabs {
				result.Tabs = append(result.Tabs, browserTab(tab))
			}
			return result, err
		}
		entries, err := r.BrowserAttachments(ctx, session.SessionID(p.SessionID))
		result := protocol.BrowserAttachmentsResult{Attachments: []protocol.BrowserAttachment{}}
		for _, a := range entries {
			result.Attachments = append(result.Attachments, protocol.BrowserAttachment{Scope: browserScope(a.Scope), RootID: protocol.ID(a.Owner.RootID), AgentID: protocol.ID(a.Owner.AgentID), DocumentRevision: a.DocumentRevision, URL: a.URL, Title: a.Title})
		}
		return result, err
	})
}

func browserEvent(n browserhost.Notification) protocol.BrowserEvent {
	event := protocol.BrowserEvent{JSONRPC: "2.0"}
	switch {
	case n.Command != nil:
		c := n.Command
		event.Method = "browser.command"
		event.Command = &protocol.BrowserCommand{CommandID: protocol.BrowserToken(c.CommandID), OperationID: protocol.ID(c.OperationID), RootID: protocol.ID(c.Identity.RootID), AgentID: protocol.ID(c.Identity.AgentID), ProviderEpoch: protocol.BrowserToken(c.Scope.ProviderEpoch), Scope: browserScope(c.Scope), ExpectedDocument: c.ExpectedDocument, DeadlineMillis: protocol.Counter(c.DeadlineMillis), Kind: c.Kind, Arguments: c.Arguments}
	case n.Inventory != nil:
		q := n.Inventory
		event.Method = "browser.inventory"
		event.Inventory = &protocol.BrowserInventoryRequest{RequestID: protocol.BrowserToken(q.RequestID), RootID: protocol.ID(q.Identity.RootID), AgentID: protocol.ID(q.Identity.AgentID), ProviderID: protocol.BrowserToken(q.Binding.ProviderID), ProviderEpoch: protocol.BrowserToken(q.Binding.ProviderEpoch), Tabs: []protocol.BrowserInventoryTarget{}}
		for _, tab := range q.Tabs {
			event.Inventory.Tabs = append(event.Inventory.Tabs, protocol.BrowserInventoryTarget{TabID: protocol.BrowserToken(tab.TabID), TabGeneration: protocol.BrowserToken(tab.TabGeneration)})
		}
	case n.Cancel != nil:
		c := n.Cancel
		event.Method = "browser.command.cancel"
		event.Cancel = &protocol.BrowserCommandCancel{CommandID: protocol.BrowserToken(c.CommandID), RootID: protocol.ID(c.RootID), ProviderEpoch: protocol.BrowserToken(c.ProviderEpoch), AttachmentGeneration: protocol.BrowserToken(c.AttachmentGeneration)}
	case n.Revoked != nil:
		r := n.Revoked
		event.Method = "browser.provider.revoked"
		event.Revoked = &protocol.BrowserProviderRevoked{RootID: protocol.ID(r.RootID), ProviderID: protocol.BrowserToken(r.Binding.ProviderID), ProviderEpoch: protocol.BrowserToken(r.Binding.ProviderEpoch), Reason: r.Reason}
	case n.Retired != nil:
		r := n.Retired
		event.Method = "browser.scopes.retired"
		event.Retired = &protocol.BrowserScopesRetired{RootID: protocol.ID(r.RootID), ProviderID: protocol.BrowserToken(r.Binding.ProviderID), ProviderEpoch: protocol.BrowserToken(r.Binding.ProviderEpoch), Scopes: []protocol.BrowserScope{}}
		for _, scope := range r.Scopes {
			event.Retired.Scopes = append(event.Retired.Scopes, browserScope(scope))
		}
	}
	return event
}
