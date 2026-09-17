package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/go-rod/rod/lib/cdp"
)

func (p *browserProviders) Execute(ctx context.Context, request browser.DesktopRequest, run func(context.Context, browser.Backend) (string, error)) (browser.DesktopResult, error) {
	args, err := browser.DecodeDesktopArguments(request.Call.Arguments)
	if err != nil {
		return browser.DesktopResult{}, err
	}
	p.mu.Lock()
	a, err := p.findLocked(request.Call)
	if err == nil && a.identity != request.Identity {
		err = browserFailure("permission_denied", "browser attachment owner mismatch")
	}
	p.mu.Unlock()
	if err != nil {
		return browser.DesktopResult{}, err
	}
	timeout := 60 * time.Second
	if args.Timeout > 0 {
		if args.Timeout > 120 {
			return browser.DesktopResult{}, browserFailure("unsupported_operation", "timeout exceeds 120 seconds")
		}
		timeout = time.Duration(args.Timeout * float64(time.Second))
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	release, err := a.tab.acquire(ctx, false)
	if err != nil {
		return browser.DesktopResult{}, err
	}
	defer release()
	p.mu.Lock()
	_, err = p.findLocked(request.Call)
	live := a.live
	delegated := a.delegated
	document := a.result.DocumentRevision
	p.mu.Unlock()
	if err != nil {
		return browser.DesktopResult{}, err
	}
	fresh := request.Operation == "browser.open" || request.Operation == "browser.attach"
	if !fresh {
		if !live || (delegated && request.Operation != "browser.detach") {
			return browser.DesktopResult{}, browserFailure("attachment_revoked", "attachment is not executable")
		}
		if err = p.store.AuthorizeBrowser(ctx, request.Identity.RootID, request.Identity.AgentID, request.Call.Grant, request.Operation, request.Call.Scope); err != nil {
			return browser.DesktopResult{}, err
		}
	}
	if args.ExpectedDocument != "" && args.ExpectedDocument != document {
		return browser.DesktopResult{}, browserFailure("stale_document", "document changed before browser batch")
	}
	if request.Operation == "browser.run" {
		return p.run(ctx, a, request, args, run)
	}
	if fresh {
		if live {
			return browser.DesktopResult{}, browserFailure("attachment_revoked", "creation operation already consumed")
		}
		ref, issueErr := p.store.IssueBrowserCapability(ctx, a.identity.RootID, a.identity.AgentID, "", a.scope, capability.Reference{})
		if issueErr != nil {
			return browser.DesktopResult{}, issueErr
		}
		p.mu.Lock()
		a.grant = ref
		p.mu.Unlock()
	}
	if request.Operation == "browser.allow_preview_port" {
		if a.scope.Preview == nil {
			return browser.DesktopResult{}, browserFailure("unsupported_operation", "not a preview attachment")
		}
		if slices.Contains(a.scope.Preview.Ports, args.Port) {
			p.mu.Lock()
			out := cloneBrowserResult(a.result)
			p.mu.Unlock()
			return out, nil
		}
		if a.parent != nil {
			return browser.DesktopResult{}, browserFailure("permission_denied", "delegated preview expansion requires a fresh user-approved attach")
		}
	}
	result, err := p.command(ctx, a, request.OperationID, strings.TrimPrefix(request.Operation, "browser."), request.Call.Arguments, args.ExpectedDocument)
	if err != nil {
		if fresh {
			p.mu.Lock()
			a.cancel()
			a.live = false
			p.mu.Unlock()
			p.revokeGrant(a)
		}
		return browser.DesktopResult{}, err
	}
	if request.Operation == "browser.detach" {
		p.revokeAttachment(a)
		p.mu.Lock()
		out := cloneBrowserResult(a.result)
		p.mu.Unlock()
		return out, nil
	}
	if request.Operation == "browser.allow_preview_port" {
		scope := cloneBrowserScope(a.scope)
		if !slices.Contains(scope.Preview.Ports, args.Port) {
			scope.Preview.Ports = append(scope.Preview.Ports, args.Port)
			slices.Sort(scope.Preview.Ports)
		}
		ref, issueErr := p.store.IssueBrowserCapability(ctx, a.identity.RootID, a.identity.AgentID, "", scope, capability.Reference{})
		if issueErr != nil {
			p.revokeAttachment(a)
			return browser.DesktopResult{}, issueErr
		}
		old := a.grant
		p.mu.Lock()
		a.scope = scope
		a.grant = ref
		a.result.Network.Ports = slices.Clone(scope.Preview.Ports)
		p.mu.Unlock()
		_, _ = p.store.RevokeCapabilityFor(context.Background(), a.identity.RootID, a.identity.AgentID, old.ID)
	}
	p.mu.Lock()
	if len(result.Result) > 0 {
		var native browser.DesktopResult
		if json.Unmarshal(result.Result, &native) == nil {
			if native.URL != "" {
				a.result.URL = native.URL
			}
			if native.Title != "" {
				a.result.Title = native.Title
			}
			if native.DocumentRevision != "" {
				a.result.DocumentRevision = native.DocumentRevision
			}
			a.result.Error = native.Error
		}
	}
	if result.DocumentRevision != "" {
		a.result.DocumentRevision = result.DocumentRevision
	}
	a.live = true
	var previous []*browserAttachment
	if fresh {
		for _, other := range a.lease.attachments {
			if other != a && other.scope.TabID == a.scope.TabID && other.live && !other.delegated {
				other.cancel()
				other.live = false
				previous = append(previous, other)
			}
		}
	}
	out := cloneBrowserResult(a.result)
	p.mu.Unlock()
	for _, other := range previous {
		p.revokeGrant(other)
	}
	return out, nil
}

func (p *browserProviders) run(ctx context.Context, a *browserAttachment, request browser.DesktopRequest, args browser.DesktopArguments, callback func(context.Context, browser.Backend) (string, error)) (browser.DesktopResult, error) {
	if callback == nil {
		return browser.DesktopResult{}, errors.New("browser run callback is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p.mu.Lock()
	a.batchCancel = cancel
	a.batchOperation = request.OperationID
	a.events = make(chan *cdp.Event, 64)
	events := a.events
	p.mu.Unlock()
	defer func() { p.mu.Lock(); a.batchCancel = nil; a.batchOperation = ""; a.events = nil; p.mu.Unlock() }()
	// Ending a batch is bounded cleanup, never a replay of an uncertain command.
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_, _ = p.command(cleanup, a, request.OperationID, "end", json.RawMessage(`{}`), "")
	}()
	if _, err := p.command(ctx, a, request.OperationID, "begin", request.Call.Arguments, args.ExpectedDocument); err != nil {
		return browser.DesktopResult{}, err
	}
	client := &browserCDP{providers: p, attachment: a, operationID: request.OperationID, events: events}
	backend, err := browser.NewDesktopBackend(ctx, client, a.scope.TabID, func() error { return nil })
	if err != nil {
		return browser.DesktopResult{}, err
	}
	defer func() { _ = backend.Close() }()
	// Rod may tolerate a failed initialization command; never begin helpers after the batch expires.
	if ctx.Err() != nil {
		return browser.DesktopResult{}, browserFailure("outcome_unknown", "browser batch expired during backend construction")
	}
	output, err := callback(ctx, backend)
	p.mu.Lock()
	out := cloneBrowserResult(a.result)
	p.mu.Unlock()
	out.Output = output
	if ctx.Err() != nil {
		return out, browserFailure("outcome_unknown", "browser batch cancelled; delivered effects may have occurred")
	}
	return out, err
}

func (p *browserProviders) revokeAttachment(a *browserAttachment) {
	p.mu.Lock()
	var revoked []*browserAttachment
	for _, candidate := range a.lease.attachments {
		for current := candidate; current != nil; current = current.parent {
			if current == a {
				candidate.cancel()
				candidate.live = false
				revoked = append(revoked, candidate)
				break
			}
		}
	}
	p.mu.Unlock()
	for _, candidate := range revoked {
		p.revokeGrant(candidate)
	}
}
func (p *browserProviders) RevokeAgent(ctx context.Context, identity browser.DesktopIdentity) error {
	p.mu.Lock()
	var revoke []*browserAttachment
	if lease := p.roots[identity.RootID]; lease != nil {
		for _, a := range lease.attachments {
			if a.identity == identity && a.ctx.Err() == nil {
				revoke = append(revoke, a)
			}
		}
	}
	p.mu.Unlock()
	for _, a := range revoke {
		p.revokeAttachment(a)
	}
	for _, a := range revoke {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_, _ = p.command(cleanup, a, browserID(), "detach", json.RawMessage(`{}`), "")
		cancel()
	}
	return nil
}

// Transfer reserves every tab before sending one all-or-nothing native handoff.
// No child attachment becomes executable until native ACK and issuer-only rows.
func (p *browserProviders) Transfer(ctx context.Context, parent, child browser.DesktopIdentity, ids []string) ([]browser.DesktopResult, error) {
	if len(ids) == 0 {
		return []browser.DesktopResult{}, nil
	}
	if len(ids) > maxBrowserAttachments || parent.RootID != child.RootID || parent.AgentID == child.AgentID {
		return nil, browserFailure("permission_denied", "invalid browser transfer")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		return nil, browserFailure("permission_denied", "duplicate browser attachment")
	}
	p.mu.Lock()
	lease := p.roots[parent.RootID]
	parents := make([]*browserAttachment, 0, len(ids))
	for _, id := range ids {
		if lease == nil {
			p.mu.Unlock()
			return nil, browserFailure("desktop_unavailable", "no selected provider")
		}
		a := lease.attachments[id]
		if a == nil || a.identity != parent || !a.live || a.delegated || a.ctx.Err() != nil {
			p.mu.Unlock()
			return nil, browserFailure("attachment_revoked", "parent attachment is unavailable")
		}
		parents = append(parents, a)
	}
	count := 0
	if lease != nil {
		for _, a := range lease.attachments {
			if a.ctx.Err() == nil {
				count++
			}
		}
	}
	if count+len(ids) > maxBrowserAttachmentRecords {
		p.mu.Unlock()
		return nil, browserFailure("browser_busy", "transfer exceeds attachment capacity")
	}
	p.mu.Unlock()
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for _, a := range parents {
		release, err := a.tab.acquire(ctx, true)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	children := make([]*browserAttachment, 0, len(parents))
	committed := false
	nativeMayOwnChildren := false
	defer func() {
		if !committed {
			for _, a := range children {
				p.revokeAttachment(a)
			}
			if !nativeMayOwnChildren {
				return
			}
			for _, a := range children {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				_, _ = p.command(cleanup, a, browserID(), "detach", json.RawMessage(`{}`), "")
				cancel()
			}
		}
	}()
	wire := protocol.BrowserTransferArguments{ChildAgentID: child.AgentID, Attachments: []protocol.BrowserTransferAttachment{}}
	for _, a := range parents {
		if err := p.store.AuthorizeBrowser(ctx, parent.RootID, parent.AgentID, a.grant, "browser.run", a.scope); err != nil {
			return nil, err
		}
		scope := cloneBrowserScope(a.scope)
		scope.AttachmentID = browserID()
		scope.AttachmentGeneration = browserID()
		ref, err := p.store.IssueBrowserCapability(ctx, child.RootID, child.AgentID, parent.AgentID, scope, a.grant)
		if err != nil {
			return nil, err
		}
		actx, cancel := context.WithCancel(lease.ctx)
		p.mu.Lock()
		snapshot := cloneBrowserResult(a.result)
		p.mu.Unlock()
		next := &browserAttachment{lease: lease, identity: child, scope: scope, grant: ref, ctx: actx, cancel: cancel, tab: a.tab, result: snapshot, parent: a}
		next.result.AttachmentID = scope.AttachmentID
		children = append(children, next)
		p.mu.Lock()
		lease.attachments[scope.AttachmentID] = next
		p.mu.Unlock()
		wire.Attachments = append(wire.Attachments, protocol.BrowserTransferAttachment{ParentScope: cloneBrowserScope(a.scope), ChildScope: scope})
	}
	raw, _ := json.Marshal(wire)
	transferCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	reply, err := p.command(transferCtx, parents[0], browserID(), "transfer", raw, "")
	if err != nil {
		if reply.Error != nil && reply.Error.Kind != "outcome_unknown" {
			return nil, err
		}
		nativeMayOwnChildren = true
		// Delivery may have mutated ownership. Revoke all participants, not a partial
		// restoration that could leave a child controller and parent grant executable.
		for _, a := range parents {
			p.revokeAttachment(a)
		}
		return nil, err
	}
	nativeMayOwnChildren = true
	for _, a := range parents {
		if err := p.store.SetBrowserDelegationOnly(ctx, parent.RootID, parent.AgentID, a.grant, true); err != nil {
			for _, owned := range parents {
				p.revokeAttachment(owned)
			}
			return nil, browserFailure("outcome_unknown", "native transfer acknowledged but grant activation failed; all participants revoked")
		}
	}
	p.mu.Lock()
	result := make([]browser.DesktopResult, 0, len(children))
	for i, a := range parents {
		a.delegated = true
		children[i].live = true
		result = append(result, cloneBrowserResult(children[i].result))
	}
	p.mu.Unlock()
	committed = true
	return result, nil
}

// invalidateRevoked connects the existing capability-revoke control path to
// live native work; resource grants and the module/definition grant are distinct.
func (p *browserProviders) invalidateRevoked(ctx context.Context, rootID string) {
	type candidate struct {
		a        *browserAttachment
		call     capability.BrowserCall
		identity browser.DesktopIdentity
	}
	p.mu.Lock()
	var candidates []candidate
	if lease := p.roots[rootID]; lease != nil {
		for _, a := range lease.attachments {
			if a.ctx.Err() == nil {
				candidates = append(candidates, candidate{a: a, identity: a.identity, call: capability.BrowserCall{Scope: cloneBrowserScope(a.scope), Grant: a.grant}})
			}
		}
	}
	p.mu.Unlock()
	var revoked []*browserAttachment
	for _, candidate := range candidates {
		authority, _, err := p.store.LoadAgentAuthority(ctx, rootID, candidate.identity.AgentID)
		if err == nil {
			err = p.store.AuthorizeCapability(ctx, rootID, candidate.identity.AgentID, authority.Shell, "browser.run", "")
		}
		if err == nil && candidate.call.Grant.ID != "" {
			err = p.store.AuthorizeBrowser(ctx, rootID, candidate.identity.AgentID, candidate.call.Grant, "browser.detach", candidate.call.Scope)
		}
		if err != nil {
			revoked = append(revoked, candidate.a)
		}
	}
	for _, a := range revoked {
		p.revokeAttachment(a)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	for _, a := range revoked {
		_, _ = p.command(cleanup, a, browserID(), "detach", json.RawMessage(`{}`), "")
	}
}
