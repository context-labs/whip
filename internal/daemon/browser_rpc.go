package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/go-rod/rod/lib/cdp"
)

func (s *Server) handleBrowser(c *serverConn, request rpcMessage) (any, *RPCError, bool) {
	switch request.Method {
	case "browser.provider.bind":
		var params protocol.BrowserProviderBindParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		result, err := s.daemon.browserProviders.bind(c, params)
		return result, rpcFromError(err), true
	case "browser.provider.unbind":
		var params protocol.BrowserProviderUnbindParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := s.daemon.browserProviders.unbind(c, params); err != nil {
			return nil, rpcFailure(-32009, err.Error()), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	case "browser.inventory.result":
		var params protocol.BrowserInventoryResultParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := s.daemon.browserProviders.settleInventory(c, params); err != nil {
			return nil, rpcFailure(-32009, err.Error()), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	case "browser.command.result":
		var params protocol.BrowserCommandResultParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := s.daemon.browserProviders.settle(c, params); err != nil {
			return nil, rpcFailure(-32009, err.Error()), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	case "browser.provider.event":
		var params protocol.BrowserProviderEventParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := s.daemon.browserProviders.event(c, params); err != nil {
			failure := rpcFailure(-32009, err.Error())
			if errors.Is(err, errBrowserEventStale) {
				failure.Data.Kind = "browser_event_stale"
			}
			return nil, failure, true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	}
	return nil, nil, false
}

func (p *browserProviders) command(ctx context.Context, a *browserAttachment, operationID, kind string, args json.RawMessage, expected string) (protocol.BrowserCommandResultParams, error) {
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		deadline = time.Now().Add(60 * time.Second)
	}
	p.mu.Lock()
	cleanup := kind == "detach"
	if ctx.Err() != nil || (!cleanup && a.ctx.Err() != nil) || a.lease.ctx.Err() != nil || p.roots[a.identity.RootID] != a.lease {
		p.mu.Unlock()
		return protocol.BrowserCommandResultParams{}, browserFailure("attachment_revoked", "browser command scope expired")
	}
	command := protocol.BrowserCommand{CommandID: browserID(), OperationID: operationID, RootID: a.identity.RootID, AgentID: a.identity.AgentID, ProviderEpoch: a.scope.ProviderEpoch, Scope: cloneBrowserScope(a.scope), ExpectedDocument: expected, DeadlineMillis: deadline.UnixMilli(), Kind: kind, Arguments: args}
	pending := &browserPending{done: ctx.Done(), lease: a.lease, attachment: a, command: command, result: make(chan protocol.BrowserCommandResultParams, 1)}
	p.pending[command.CommandID] = pending
	delivered := a.lease.holder.notify("browser.command", command)
	if !delivered {
		delete(p.pending, command.CommandID)
	}
	p.mu.Unlock()
	if !delivered {
		return protocol.BrowserCommandResultParams{}, browserFailure("desktop_unavailable", "selected provider outbound queue unavailable")
	}
	defer func() { p.mu.Lock(); delete(p.pending, command.CommandID); p.mu.Unlock() }()
	attachmentDone := a.ctx.Done()
	if cleanup {
		attachmentDone = nil
	}
	select {
	case result := <-pending.result:
		if result.Error != nil {
			if result.Error.Kind == "attachment_revoked" || result.Error.Kind == "tab_closed" || result.Error.Kind == "preview_disconnected" {
				p.revokeAttachment(a)
			}
			return result, result.Error
		}
		return result, nil
	case <-ctx.Done():
	case <-attachmentDone:
	}
	a.lease.holder.notify("browser.command.cancel", protocol.BrowserCommandCancel{CommandID: command.CommandID, RootID: command.RootID, ProviderEpoch: command.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, Reason: "cancelled"})
	return protocol.BrowserCommandResultParams{}, browserFailure("outcome_unknown", "delivered browser command cancelled; effects are not replayed")
}

func (p *browserProviders) settle(c *serverConn, result protocol.BrowserCommandResultParams) error {
	if len(result.DocumentRevision) > 256 || len(result.Result) > 512<<10 || (result.Error != nil && (len(result.Error.Message) > 4096 || !knownBrowserError(result.Error.Kind))) {
		return errors.New("browser result exceeds bounds or has an unknown error")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.pending[result.CommandID]
	if pending == nil || pending.cancelled() || pending.lease.holder != c || pending.lease.ctx.Err() != nil || (pending.attachment.ctx.Err() != nil && pending.command.Kind != "detach") || pending.command.RootID != result.RootID || pending.command.ProviderEpoch != result.ProviderEpoch || pending.command.Scope.AttachmentGeneration != result.AttachmentGeneration {
		return errors.New("browser result does not match a live command on this connection")
	}
	if result.Screenshot != nil {
		var args struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(pending.command.Arguments, &args)
		if pending.command.Kind != "cdp" || args.Method != "Page.captureScreenshot" || !c.server.uploads.authorizeBrowserContent(c.id, pending.command.RootID, pending.command.AgentID, *result.Screenshot) {
			return errors.New("browser screenshot requires a bounded same-connection JPEG upload")
		}
	}
	if result.DocumentRevision != "" {
		pending.attachment.result.DocumentRevision = result.DocumentRevision
	}
	delete(p.pending, result.CommandID)
	pending.result <- result
	return nil
}
func knownBrowserError(kind string) bool {
	switch kind {
	case "permission_denied", "desktop_unavailable", "host_not_connected", "browser_busy", "stale_document", "attachment_revoked", "tab_closed", "preview_disconnected", "unsupported_operation", "outcome_unknown":
		return true
	}
	return false
}

// This rejection retires only an obsolete attachment event queue, never the
// selected provider. It is returned only after authenticating that provider.
var errBrowserEventStale = errors.New("browser event attachment is stale")

func (p *browserProviders) event(c *serverConn, event protocol.BrowserProviderEventParams) error {
	if len(event.DocumentRevision) > 256 || (event.Kind == "cdp" && (event.Method == "" || !json.Valid(event.Params))) || len(event.Params) > 256<<10 || len(event.URL) > 8192 || len(event.Title) > 4096 || len(event.Method) > 256 {
		return errors.New("browser event exceeds bounds")
	}
	switch event.Kind {
	case "cdp", "state", "document", "closed", "revoked", "preview_disconnected":
	default:
		return errors.New("unsupported browser event kind")
	}
	if event.Sequence == 0 {
		return errors.New("browser event sequence must be positive")
	}
	p.mu.Lock()
	lease := p.roots[event.RootID]
	if lease == nil || lease.holder != c || lease.epoch != event.ProviderEpoch || lease.ctx.Err() != nil {
		p.mu.Unlock()
		return errors.New("browser event is not from the selected provider")
	}
	a := lease.attachments[event.AttachmentID]
	if a != nil && a.scope.TabID != event.TabID {
		p.mu.Unlock()
		return errors.New("browser event tab does not match attachment")
	}
	if a == nil || a.ctx.Err() != nil || a.scope.TabGeneration != event.TabGeneration || a.scope.AttachmentGeneration != event.AttachmentGeneration || a.delegated {
		p.mu.Unlock()
		return errBrowserEventStale
	}
	if event.Sequence != a.sequence+1 {
		a.cancel()
		a.live = false
		p.mu.Unlock()
		p.revokeGrant(a)
		return errors.New("browser event sequence gap; attachment revoked")
	}
	a.sequence = event.Sequence
	if event.DocumentRevision != "" && event.DocumentRevision != a.result.DocumentRevision {
		a.result.DocumentRevision = event.DocumentRevision
		if a.batchCancel != nil && event.OperationID != a.batchOperation {
			a.batchCancel()
		}
	}
	if event.URL != "" {
		a.result.URL = event.URL
	}
	if event.Title != "" {
		a.result.Title = event.Title
	}
	if event.Kind == "closed" {
		delete(lease.created, event.TabID)
	}
	revoke := event.Kind == "closed" || event.Kind == "revoked" || event.Kind == "preview_disconnected"
	if revoke {
		a.cancel()
		a.live = false
	}
	if event.Kind == "cdp" && a.events != nil {
		select {
		case a.events <- &cdp.Event{SessionID: a.scope.AttachmentID, Method: event.Method, Params: event.Params}:
		default:
			a.cancel()
			a.live = false
			revoke = true
		}
	}
	p.mu.Unlock()
	if revoke {
		p.revokeGrant(a)
	}
	return nil
}

var _ browser.DesktopProvider = (*browserProviders)(nil)

func (pending *browserPending) cancelled() bool {
	select {
	case <-pending.done:
		return true
	default:
		return false
	}
}
