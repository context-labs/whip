package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/go-rod/rod/lib/cdp"
)

const (
	maxBrowserAttachments = 8
	// Delegation-only ancestors retain revoke authority but do not consume tab capacity.
	maxBrowserAttachmentRecords = 256
	maxBrowserReleases          = 128
)

type browserProviders struct {
	mu           sync.Mutex
	store        *session.Store
	roots        map[string]*browserLease
	available    map[browserReleaseKey]*browserLease
	inventory    map[string]*browserInventoryPending
	pending      map[string]*browserPending
	closed       bool
	released     map[browserReleaseKey]string
	releaseOrder []browserReleaseKey
}
type browserReleaseKey struct {
	holder *serverConn
	rootID string
}
type browserLease struct {
	holder            *serverConn
	rootID, id, epoch string
	offer             protocol.BrowserProviderBindParams
	ctx               context.Context
	cancel            context.CancelFunc
	attachments       map[string]*browserAttachment
	tabs              map[string]*browserTab
	created           map[string]browserCreatedTab
}
type browserAttachment struct {
	lease           *browserLease
	identity        browser.DesktopIdentity
	scope           capability.BrowserScope
	grant           capability.Reference
	ctx             context.Context
	cancel          context.CancelFunc
	tab             *browserTab
	result          browser.DesktopResult
	live, delegated bool
	sequence        uint64
	events          chan *cdp.Event
	batchCancel     context.CancelFunc
	batchOperation  string
	parent          *browserAttachment
}
type browserTab struct {
	mu     sync.Mutex
	active bool
	queue  []chan struct{}
}
type browserPending struct {
	done       <-chan struct{}
	lease      *browserLease
	attachment *browserAttachment
	command    protocol.BrowserCommand
	result     chan protocol.BrowserCommandResultParams
}

func newBrowserProviders(store *session.Store) *browserProviders {
	return &browserProviders{store: store, roots: map[string]*browserLease{}, available: map[browserReleaseKey]*browserLease{}, inventory: map[string]*browserInventoryPending{}, pending: map[string]*browserPending{}, released: map[browserReleaseKey]string{}}
}
func browserID() string { return rand.Text() }
func browserFailure(kind, message string) error {
	return &browser.DesktopError{Kind: kind, Message: message}
}
func cloneBrowserScope(scope capability.BrowserScope) capability.BrowserScope {
	scope.Rights = slices.Clone(scope.Rights)
	if scope.Preview != nil {
		p := *scope.Preview
		p.Ports = slices.Clone(p.Ports)
		scope.Preview = &p
	}
	return scope
}

func (p *browserProviders) bind(c *serverConn, params protocol.BrowserProviderBindParams) (protocol.BrowserProviderBindResult, error) {
	if !slices.Contains(c.client.Capabilities, "desktop-browser-v1") || (params.Version != 1 && params.Version != 2) || params.RootID == "" || params.DesktopID == "" || params.WindowID == "" || params.OfferRevision == "" || params.CreateProfileID == "" {
		return protocol.BrowserProviderBindResult{}, errors.New("browser provider requires desktop-browser-v1 and an explicit versioned root/window offer")
	}
	if params.Version == 2 && !slices.Contains(c.client.Capabilities, "desktop-browser-v2") {
		return protocol.BrowserProviderBindResult{}, errors.New("browser discovery requires desktop-browser-v2")
	}
	if params.Availability && (params.Version != 2 || len(params.OfferedTabs) != 0 || len(params.OfferedPreviewHosts) != 0) {
		return protocol.BrowserProviderBindResult{}, errors.New("availability is a v2 create-only candidate, not an inventory offer")
	}
	if _, _, err := p.store.Load(params.RootID); err != nil {
		return protocol.BrowserProviderBindResult{}, err
	}
	if len(params.OfferedTabs) > 32 || len(params.OfferedPreviewHosts) > 16 {
		return protocol.BrowserProviderBindResult{}, errors.New("browser offer exceeds capacity")
	}
	seen := map[string]bool{}
	for _, tab := range params.OfferedTabs {
		if tab.TabID == "" || tab.TabGeneration == "" || tab.ProfileID == "" || seen[tab.TabID] {
			return protocol.BrowserProviderBindResult{}, errors.New("invalid or duplicate offered tab")
		}
		seen[tab.TabID] = true
		if tab.Preview != nil {
			if err := validateBrowserPreview(*tab.Preview); err != nil {
				return protocol.BrowserProviderBindResult{}, err
			}
		}
	}
	seen = map[string]bool{}
	for _, host := range params.OfferedPreviewHosts {
		if seen[host.HostID] {
			return protocol.BrowserProviderBindResult{}, errors.New("duplicate offered preview host")
		}
		seen[host.HostID] = true
		if err := validateBrowserPreview(host); err != nil {
			return protocol.BrowserProviderBindResult{}, err
		}
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return protocol.BrowserProviderBindResult{}, errors.New("browser provider broker is closed")
	}
	key := browserReleaseKey{holder: c, rootID: params.RootID}
	old := p.roots[params.RootID]
	if params.Availability {
		old = p.available[key]
		if selected := p.roots[params.RootID]; selected != nil && selected.holder == c {
			p.mu.Unlock()
			return protocol.BrowserProviderBindResult{}, errors.New("this connection already has a selected Browser destination")
		}
	}
	count := 0
	for _, lease := range p.leasesLocked() {
		if lease.holder == c {
			count++
		}
	}
	if old == nil && count >= 64 {
		p.mu.Unlock()
		return protocol.BrowserProviderBindResult{}, errors.New("browser provider root binding capacity reached")
	}
	if old != nil && old.ctx.Err() == nil && params.ExpectedProviderEpoch != old.epoch {
		p.mu.Unlock()
		return protocol.BrowserProviderBindResult{}, errors.New("browser association exists; replacement requires its current epoch")
	}
	if (old == nil || old.ctx.Err() != nil) && params.ExpectedProviderEpoch != "" {
		p.mu.Unlock()
		return protocol.BrowserProviderBindResult{}, errors.New("browser association epoch is stale")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	lease := &browserLease{holder: c, rootID: params.RootID, id: browserID(), epoch: browserID(), offer: params, ctx: ctx, cancel: cancel, attachments: map[string]*browserAttachment{}, tabs: map[string]*browserTab{}, created: map[string]browserCreatedTab{}}
	if old != nil {
		old.cancel()
	}
	if params.Availability {
		p.available[key] = lease
	} else {
		p.roots[params.RootID] = lease
	}
	p.mu.Unlock()
	if old != nil {
		p.revokeLease(old, "replaced")
	}
	return protocol.BrowserProviderBindResult{Version: params.Version, ProviderID: lease.id, ProviderEpoch: lease.epoch}, nil
}
func validateBrowserPreview(s capability.BrowserPreviewScope) error {
	if s.HostID == "" || s.HostIdentity == "" || s.ConnectionGeneration == "" || s.EnvironmentID == "" || (s.Loopback != "127.0.0.1" && s.Loopback != "::1") || len(s.Ports) > 64 {
		return errors.New("invalid verified preview offer")
	}
	previous := 0
	for _, port := range s.Ports {
		if port <= previous || port > 65535 {
			return errors.New("preview ports must be canonical")
		}
		previous = port
	}
	return nil
}
func (p *browserProviders) disconnect(c *serverConn) {
	p.mu.Lock()
	for key := range p.released {
		if key.holder == c {
			delete(p.released, key)
		}
	}
	p.releaseOrder = slices.DeleteFunc(p.releaseOrder, func(key browserReleaseKey) bool { return key.holder == c })
	var leases []*browserLease
	for _, lease := range p.leasesLocked() {
		if lease.holder == c {
			lease.cancel()
			p.removeLeaseLocked(lease)
			leases = append(leases, lease)
		}
	}
	p.mu.Unlock()
	for _, lease := range leases {
		p.revokeLease(lease, "disconnected")
	}
}
func (p *browserProviders) unbind(c *serverConn, params protocol.BrowserProviderUnbindParams) error {
	p.mu.Lock()
	key := browserReleaseKey{holder: c, rootID: params.RootID}
	if epoch, ok := p.released[key]; ok && epoch == params.ProviderEpoch {
		p.mu.Unlock()
		return nil
	}
	lease := p.roots[params.RootID]
	if candidate := p.available[key]; candidate != nil && candidate.epoch == params.ProviderEpoch {
		lease = candidate
	}
	if lease == nil || lease.holder != c || lease.epoch != params.ProviderEpoch || lease.ctx.Err() != nil {
		p.mu.Unlock()
		return errors.New("browser unbind does not match the current lease on this connection")
	}
	lease.cancel()
	p.removeLeaseLocked(lease)
	p.rememberReleaseLocked(key, params.ProviderEpoch)
	p.mu.Unlock()
	p.revokeLease(lease, "unbound")
	return nil
}

func (p *browserProviders) revokeLease(lease *browserLease, reason string) {
	// Standalone teardown cannot depend on a live command or the cancelled lease.
	// On disconnect this is best-effort; clients also release on transport loss.
	lease.holder.notify("browser.provider.revoked", protocol.BrowserProviderRevoked{RootID: lease.rootID, ProviderID: lease.id, ProviderEpoch: lease.epoch, Reason: reason})
	p.mu.Lock()
	var attachments []*browserAttachment
	for _, a := range lease.attachments {
		a.cancel()
		a.live = false
		attachments = append(attachments, a)
	}
	p.mu.Unlock()
	for _, a := range attachments {
		p.revokeGrant(a)
	}
}
func (p *browserProviders) revokeGrant(a *browserAttachment) {
	p.mu.Lock()
	ref := a.grant
	identity := a.identity
	p.mu.Unlock()
	if ref.ID != "" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 2*time.Second)
		defer cancel()
		_, _ = p.store.RevokeCapabilityFor(ctx, identity.RootID, identity.AgentID, ref.ID)
	}
}

func (p *browserProviders) Resolve(ctx context.Context, identity browser.DesktopIdentity, operation string, args browser.DesktopArguments) (capability.BrowserCall, error) {
	if err := ctx.Err(); err != nil {
		return capability.BrowserCall{}, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return capability.BrowserCall{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, err := p.destinationLocked(identity.RootID)
	if err != nil {
		return capability.BrowserCall{}, err
	}
	if operation != "browser.open" && operation != "browser.attach" {
		a := lease.attachments[args.AttachmentID]
		if a == nil || a.identity != identity || a.ctx.Err() != nil || !a.live || (a.delegated && operation != "browser.detach") {
			return capability.BrowserCall{}, browserFailure("attachment_revoked", "attachment does not belong to this agent")
		}
		if operation != "browser.run" && operation != "browser.detach" && operation != "browser.allow_preview_port" {
			return capability.BrowserCall{}, browserFailure("unsupported_operation", "unknown browser operation")
		}
		if operation == "browser.allow_preview_port" && (a.scope.Preview == nil || args.Port < 1 || args.Port > 65535) {
			return capability.BrowserCall{}, browserFailure("unsupported_operation", "a preview attachment and valid port are required")
		}
		return capability.BrowserCall{Scope: cloneBrowserScope(a.scope), Grant: a.grant, Arguments: raw}, nil
	}
	if operation == "browser.open" && len(lease.created) >= 32 {
		return capability.BrowserCall{}, browserFailure("browser_busy", "Created tab discovery capacity reached; list tabs to refresh closed pages")
	}
	count := 0
	for id, a := range lease.attachments {
		if a.ctx.Err() != nil {
			delete(lease.attachments, id)
		} else if !a.delegated {
			count++
		}
	}
	for tabID := range lease.tabs {
		referenced := false
		for _, a := range lease.attachments {
			if a.scope.TabID == tabID {
				referenced = true
				break
			}
		}
		if !referenced {
			delete(lease.tabs, tabID)
		}
	}
	if count >= maxBrowserAttachments || len(lease.attachments) >= maxBrowserAttachmentRecords {
		return capability.BrowserCall{}, browserFailure("browser_busy", "attachment capacity reached")
	}
	scope := capability.BrowserScope{ProviderID: lease.id, ProviderEpoch: lease.epoch, AttachmentID: browserID(), AttachmentGeneration: browserID(), Rights: []string{"control"}}
	result := browser.DesktopResult{Network: browser.DesktopNetwork{Kind: "mac", Ports: []int{}}, SupportedOperations: []string{"run", "detach"}, Media: []string{}}
	if operation == "browser.attach" {
		found := false
		offered := []protocol.BrowserOfferedTab{}
		if identity.AgentID == identity.RootID {
			offered = append(offered, lease.offer.OfferedTabs...)
		}
		for _, created := range lease.created {
			if created.owner == identity.AgentID {
				offered = append(offered, created.offer)
			}
		}
		for _, tab := range offered {
			if tab.TabID == args.TabID {
				found = true
				scope.TabID = tab.TabID
				scope.TabGeneration = tab.TabGeneration
				scope.ProfileID = tab.ProfileID
				scope.Preview = tab.Preview
				result.DocumentRevision = tab.DocumentRevision
				result.URL = tab.URL
				result.Title = tab.Title
				break
			}
		}
		if !found {
			return capability.BrowserCall{}, browserFailure("permission_denied", "tab is not in the selected desktop offer")
		}
	} else {
		scope.TabID = browserID()
		scope.TabGeneration = browserID()
		scope.ProfileID = lease.offer.CreateProfileID
		scope.Rights = []string{"create", "control"}
		if args.PreviewHostID != "" {
			for _, host := range lease.offer.OfferedPreviewHosts {
				if host.HostID == args.PreviewHostID {
					h := host
					scope.Preview = &h
					break
				}
			}
			if scope.Preview == nil {
				return capability.BrowserCall{}, browserFailure("host_not_connected", "preview host is not offered")
			}
		}
	}
	scope = cloneBrowserScope(scope)
	if scope.Preview != nil {
		if operation == "browser.open" {
			port, err := browserPreviewURLPort(args.URL, scope.Preview.Loopback)
			if err != nil {
				return capability.BrowserCall{}, err
			}
			scope.Preview.Ports = append(scope.Preview.Ports, port)
			slices.Sort(scope.Preview.Ports)
			scope.Preview.Ports = slices.Compact(scope.Preview.Ports)
			if len(scope.Preview.Ports) > 64 {
				return capability.BrowserCall{}, browserFailure("browser_busy", "preview port capacity reached")
			}
		}
		scope.Rights = append(scope.Rights, "route")
		result.Network = browser.DesktopNetwork{Kind: "ssh", HostID: scope.Preview.HostID, Ports: slices.Clone(scope.Preview.Ports)}
		result.SupportedOperations = append(result.SupportedOperations, "allow_preview_port")
	}
	result.AttachmentID = scope.AttachmentID
	result.TabID = scope.TabID
	tab := lease.tabs[scope.TabID]
	if tab == nil {
		tab = &browserTab{}
		lease.tabs[scope.TabID] = tab
	}
	actx, cancel := context.WithCancel(lease.ctx)
	a := &browserAttachment{lease: lease, identity: identity, scope: scope, ctx: actx, cancel: cancel, tab: tab, result: result}
	lease.attachments[scope.AttachmentID] = a
	context.AfterFunc(ctx, func() {
		p.mu.Lock()
		if !a.live {
			a.cancel()
		}
		p.mu.Unlock()
	})
	return capability.BrowserCall{Scope: cloneBrowserScope(scope), Arguments: raw}, nil
}
func (p *browserProviders) findLocked(call capability.BrowserCall) (*browserAttachment, error) {
	for _, lease := range p.leasesLocked() {
		if lease.id != call.Scope.ProviderID || lease.epoch != call.Scope.ProviderEpoch {
			continue
		}
		current, err := p.destinationLocked(lease.rootID)
		if err != nil {
			return nil, err
		}
		if current != lease {
			continue
		}
		a := lease.attachments[call.Scope.AttachmentID]
		if a != nil && a.ctx.Err() == nil && a.grant == call.Grant && reflect.DeepEqual(a.scope, call.Scope) {
			return a, nil
		}
	}
	return nil, browserFailure("attachment_revoked", "browser scope is no longer current")
}
func (p *browserProviders) CallContext(call capability.BrowserCall) (context.Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, err := p.findLocked(call)
	if err != nil {
		return nil, err
	}
	return a.ctx, nil
}
func (p *browserProviders) Attachments(ctx context.Context, identity browser.DesktopIdentity) []browser.DesktopResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := []browser.DesktopResult{}
	if lease := p.roots[identity.RootID]; lease != nil {
		for _, a := range lease.attachments {
			if a.identity == identity && a.live && !a.delegated && a.ctx.Err() == nil {
				result = append(result, cloneBrowserResult(a.result))
			}
		}
	}
	slices.SortFunc(result, func(a, b browser.DesktopResult) int { return stringsCompare(a.AttachmentID, b.AttachmentID) })
	return result
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
func cloneBrowserResult(v browser.DesktopResult) browser.DesktopResult {
	v.Network.Ports = slices.Clone(v.Network.Ports)
	v.SupportedOperations = slices.Clone(v.SupportedOperations)
	v.Media = slices.Clone(v.Media)
	v.Tabs = slices.Clone(v.Tabs)
	return v
}

// acquire serializes an entire helper run, not individual CDP requests.
func (t *browserTab) acquire(ctx context.Context, immediate bool) (func(), error) {
	t.mu.Lock()
	if !t.active {
		t.active = true
		t.mu.Unlock()
		return t.release, nil
	}
	if immediate || len(t.queue) >= 4 {
		t.mu.Unlock()
		return nil, browserFailure("browser_busy", "one batch active and four queued per tab")
	}
	ready := make(chan struct{})
	t.queue = append(t.queue, ready)
	t.mu.Unlock()
	select {
	case <-ready:
		if err := ctx.Err(); err != nil {
			t.release()
			return nil, err
		}
		return t.release, nil
	case <-ctx.Done():
		t.mu.Lock()
		index := slices.Index(t.queue, ready)
		if index >= 0 {
			t.queue = slices.Delete(t.queue, index, index+1)
			t.mu.Unlock()
		} else {
			t.mu.Unlock()
			t.release()
		}
		return nil, ctx.Err()
	}
}
func (t *browserTab) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.queue) == 0 {
		t.active = false
		return
	}
	next := t.queue[0]
	t.queue = t.queue[1:]
	close(next)
}

func (s *Session) desktopBrowserProvider() browser.DesktopProvider {
	if s.browserProviders == nil {
		return nil
	}
	return s.browserProviders
}

// The initial route is included in the immutable permission envelope, never
// acquired while resolving an offer. Only the offered literal remote loopback
// may select it; DNS names, credentials and alternate address spellings cannot.
func browserPreviewURLPort(raw, loopback string) (int, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() != loopback {
		return 0, browserFailure("permission_denied", "preview URL must use the selected literal remote loopback over HTTP or HTTPS without credentials")
	}
	port := 80
	if parsed.Scheme == "https" {
		port = 443
	}
	if value := parsed.Port(); value != "" {
		port, err = strconv.Atoi(value)
	}
	if err != nil || port < 1 || port > 65535 {
		return 0, browserFailure("permission_denied", "preview URL port must be between 1 and 65535")
	}
	return port, nil
}

func (p *browserProviders) rememberReleaseLocked(key browserReleaseKey, epoch string) {
	if index := slices.Index(p.releaseOrder, key); index >= 0 {
		p.releaseOrder = slices.Delete(p.releaseOrder, index, index+1)
	}
	if len(p.releaseOrder) == maxBrowserReleases {
		delete(p.released, p.releaseOrder[0])
		p.releaseOrder = slices.Delete(p.releaseOrder, 0, 1)
	}
	p.released[key] = epoch
	p.releaseOrder = append(p.releaseOrder, key)
}

func (p *browserProviders) shutdown() {
	p.mu.Lock()
	p.closed = true
	leases := make([]*browserLease, 0, len(p.roots))
	for _, lease := range p.leasesLocked() {
		lease.cancel()
		leases = append(leases, lease)
	}
	clear(p.roots)
	clear(p.available)
	clear(p.released)
	p.releaseOrder = nil
	p.mu.Unlock()
	for _, lease := range leases {
		p.revokeLease(lease, "shutdown")
	}
}
