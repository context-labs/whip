package browserhost

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"time"
)

// Check must verify the already-dispatched operation and its current permission
// chain. It is called before every native command, after queue admission too.
type Check func(context.Context) error

// Commit publishes durable activation only after the native acknowledgement.
// A failed/uncertain commit retires the handle; neither effect is replayed.
type Commit func(context.Context, Attachment) error

type Capture struct {
	host      *Host
	provider  *provider
	owner     Identity
	operation string
	args      Arguments
	scope     Scope
	initial   *attachment
	snapshot  Attachment
	used      bool
}

func (c *Capture) Scope() Scope { return cloneScope(c.scope) }
func (c *Capture) Lifetime() context.Context {
	if c.initial != nil {
		return c.initial.ctx
	}
	return c.provider.ctx
}

// Resolve is inert. Prospective IDs are private to this capture; no shared
// attachment or queue slot is held while a human considers permission.
func (h *Host) Resolve(ctx context.Context, owner Identity, operation string, args Arguments) (*Capture, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !owner.valid() || !textBound(args.URL, 8192) || !textBound(args.ExpectedDocument, 128) {
		return nil, errors.New("invalid browser request")
	}
	if e := validateArguments(operation, args); e != nil {
		return nil, e
	}
	for _, id := range []string{args.TabID, args.AttachmentID, args.PreviewHostID} {
		if id != "" && !token(id) {
			return nil, errors.New("invalid browser identifier")
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	v, e := h.destinationLocked(owner.RootID)
	if e != nil {
		return nil, e
	}
	c := &Capture{host: h, provider: v, owner: owner, operation: operation, args: args}
	switch operation {
	case "run", "detach", "allow_preview_port":
		a := v.attachments[args.AttachmentID]
		if a == nil || a.value.Owner != owner || !a.live || a.ctx.Err() != nil || (a.value.Delegated && operation != "detach") {
			return nil, ErrStale
		}
		c.initial = a
		c.snapshot = cloneAttachment(a.value)
		c.scope = cloneScope(a.value.Scope)
		if operation == "allow_preview_port" {
			if c.scope.Preview == nil || args.Port < 1 || args.Port > 65535 {
				return nil, errors.New("valid preview attachment and port required")
			}
			if a.parent != nil {
				return nil, errors.New("delegated preview expansion requires a fresh human-approved attachment")
			}
			if !slices.Contains(c.scope.Preview.Ports, args.Port) {
				if len(c.scope.Preview.Ports) >= 64 {
					return nil, ErrBusy
				}
				c.scope.Preview.Ports = append(c.scope.Preview.Ports, args.Port)
				slices.Sort(c.scope.Preview.Ports)
			}
		}
	case "open", "attach":
		if e = h.capacityLocked(v); e != nil {
			return nil, e
		}
		scope := Scope{ProviderID: v.binding.ProviderID, ProviderEpoch: v.binding.ProviderEpoch, ControlLineage: rand.Text(), AttachmentID: rand.Text(), AttachmentGeneration: rand.Text()}
		if operation == "open" {
			if !h.openCapacityLocked(v) {
				return nil, ErrBusy
			}
			scope.TabID = rand.Text()
			scope.TabGeneration = rand.Text()
			scope.ProfileID = v.offer.CreateProfileID
			if args.PreviewHostID != "" {
				for _, p := range v.offer.PreviewHosts {
					if p.HostID == args.PreviewHostID {
						scope.Preview = clonePreview(&p)
						break
					}
				}
				if scope.Preview == nil {
					return nil, errors.New("preview host is not offered")
				}
			}
		} else {
			var tab *OfferedTab
			if owner.AgentID == owner.RootID {
				for _, t := range v.offer.Tabs {
					if t.TabID == args.TabID {
						copyTab := t
						tab = &copyTab
						break
					}
				}
			}
			if t, ok := v.created[args.TabID]; ok && t.owner == owner.AgentID {
				copyTab := t.tab
				tab = &copyTab
			}
			if tab == nil {
				return nil, errors.New("tab is not offered to this owner")
			}
			scope.TabID = tab.TabID
			scope.TabGeneration = tab.TabGeneration
			scope.ProfileID = tab.ProfileID
			scope.Preview = clonePreview(tab.Preview)
			c.snapshot = Attachment{DocumentRevision: tab.DocumentRevision, URL: tab.URL, Title: tab.Title}
		}
		if scope.Preview != nil && operation == "open" {
			port, err := previewPort(args.URL, scope.Preview.Loopback)
			if err != nil {
				return nil, err
			}
			scope.Preview.Ports = append(scope.Preview.Ports, port)
			slices.Sort(scope.Preview.Ports)
			scope.Preview.Ports = slices.Compact(scope.Preview.Ports)
			if len(scope.Preview.Ports) > 64 {
				return nil, ErrBusy
			}
		}
		c.scope = scope
	default:
		return nil, errors.New("unsupported browser operation")
	}
	return c, nil
}

type Lease struct {
	capture    *Capture
	attachment *attachment
	ctx        context.Context
	cancel     context.CancelFunc
	stop       func() bool
	release    func()
	mu         sync.Mutex
	used       bool
	closed     bool
	wg         sync.WaitGroup
	once       sync.Once
}

func (c *Capture) Acquire(ctx context.Context) (*Lease, error) {
	h := c.host
	h.mu.Lock()
	if c.used {
		h.mu.Unlock()
		return nil, ErrStale
	}
	c.used = true
	if !h.currentLocked(c.provider) {
		h.mu.Unlock()
		return nil, ErrStale
	}
	a := c.initial
	if a != nil {
		if !h.captureCurrentLocked(c, a) {
			h.mu.Unlock()
			return nil, ErrStale
		}
	} else {
		if e := h.capacityLocked(c.provider); e != nil {
			h.mu.Unlock()
			return nil, e
		}
		if c.operation == "open" && !h.openCapacityLocked(c.provider) {
			h.mu.Unlock()
			return nil, ErrBusy
		}
		t := c.provider.tabs[c.scope.TabID]
		if t == nil {
			t = &tabQueue{}
			c.provider.tabs[c.scope.TabID] = t
		}
		actx, cancel := context.WithCancel(c.provider.ctx)
		value := c.snapshot
		value.Scope = cloneScope(c.scope)
		value.Owner = c.owner
		a = &attachment{provider: c.provider, value: value, ctx: actx, cancel: cancel, tab: t, opening: c.operation == "open"}
		c.provider.attachments[c.scope.AttachmentID] = a
	}
	if e := h.startLocked(c.provider.peer); e != nil {
		if c.initial == nil {
			a.cancel()
		}
		h.mu.Unlock()
		return nil, e
	}
	h.mu.Unlock()
	life, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	release, e := a.tab.acquire(life, false)
	if e != nil {
		stop()
		cancel()
		h.mu.Lock()
		if c.initial == nil {
			a.cancel()
			delete(c.provider.attachments, a.value.Scope.AttachmentID)
		}
		h.mu.Unlock()
		h.finish(c.provider.peer)
		return nil, e
	}
	l := &Lease{capture: c, attachment: a, ctx: life, cancel: cancel, stop: stop, release: release}
	h.mu.Lock()
	current := h.captureCurrentLocked(c, a)
	h.mu.Unlock()
	if !current || life.Err() != nil {
		l.Close()
		return nil, ErrStale
	}
	return l, nil
}

func (h *Host) captureCurrentLocked(c *Capture, a *attachment) bool {
	if !h.currentLocked(c.provider) || a.ctx.Err() != nil || a.value.Owner != c.owner {
		return false
	}
	if c.initial == nil {
		return c.provider.attachments[c.scope.AttachmentID] == a && !a.live
	}
	return a.live && (!a.value.Delegated || c.operation == "detach") && reflect.DeepEqual(a.value.Scope, c.snapshot.Scope)
}
func (l *Lease) Lifetime() context.Context { return l.ctx }
func (l *Lease) Close() {
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.cancel()
		l.mu.Unlock()
		l.wg.Wait()
		l.stop()
		h := l.capture.host
		h.mu.Lock()
		if !l.attachment.live {
			l.attachment.cancel()
		}
		h.mu.Unlock()
		l.release()
		h.finish(l.capture.provider.peer)
	})
}

func (l *Lease) begin(ctx context.Context) (context.Context, context.CancelFunc, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.used {
		return nil, nil, ErrStale
	}
	l.used = true
	l.wg.Add(1)
	run, cancel := context.WithTimeout(ctx, 120*time.Second)
	stop := context.AfterFunc(l.ctx, cancel)
	return run, func() { stop(); cancel(); l.wg.Done() }, nil
}

func (l *Lease) check(ctx context.Context, check Check) error {
	if check == nil {
		return errors.New("dispatched operation authority check is required")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := check(ctx); e != nil {
		return e
	}
	h := l.capture.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if l.ctx.Err() != nil || !h.captureCurrentLocked(l.capture, l.attachment) {
		return ErrStale
	}
	return nil
}

func (l *Lease) Execute(ctx context.Context, operationID string, check Check, commit Commit) (Attachment, error) {
	c := l.capture
	if c.operation == "run" || !token(operationID) || commit == nil {
		return Attachment{}, errors.New("lifecycle operation and commit callback required")
	}
	ctx, done, e := l.begin(ctx)
	if e != nil {
		return Attachment{}, e
	}
	defer done()
	if e = l.check(ctx, check); e != nil {
		return Attachment{}, e
	}
	h := c.host
	h.mu.Lock()
	e = h.claimLocked(c.provider)
	h.mu.Unlock()
	if e != nil {
		return Attachment{}, e
	}
	args, _ := json.Marshal(c.args)
	reply, e := h.command(ctx, l.attachment, operationID, c.operation, args, c.args.ExpectedDocument, check, &c.scope)
	if e != nil {
		if c.initial == nil || errors.Is(e, ErrUnknown) {
			h.mu.Lock()
			h.revokeLocked(l.attachment)
			h.mu.Unlock()
		}
		return Attachment{}, e
	}
	h.mu.Lock()
	next := cloneAttachment(l.attachment.value)
	next.Scope = cloneScope(c.scope)
	applyMetadata(&next, reply.CommandResult)
	h.mu.Unlock()
	// Commit rechecks lifecycle/policy transactionally. It must not call into this
	// lease or transfer native ownership; native success is already irreversible.
	if e = commit(ctx, cloneAttachment(next)); e != nil {
		h.mu.Lock()
		h.revokeLocked(l.attachment)
		h.mu.Unlock()
		return Attachment{}, errors.Join(ErrUnknown, e)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.captureCurrentLocked(c, l.attachment) || ctx.Err() != nil {
		h.revokeLocked(l.attachment)
		return Attachment{}, ErrUnknown
	}
	if c.operation == "detach" {
		h.revokeLocked(l.attachment)
		return next, nil
	}
	if c.operation == "allow_preview_port" {
		// Existing descendants cannot silently acquire an expanded preview route.
		for _, a := range c.provider.attachments {
			if a != l.attachment && a.value.Scope.ControlLineage == c.scope.ControlLineage {
				h.revokeLocked(a)
			}
		}
	}
	if c.initial == nil {
		for _, a := range c.provider.attachments {
			if a != l.attachment && a.value.Scope.TabID == c.scope.TabID && a.live {
				h.revokeLocked(a)
			}
		}
	}
	l.attachment.value = next
	l.attachment.live = true
	l.attachment.opening = false
	if c.operation == "open" {
		c.provider.created[c.scope.TabID] = createdTab{owner: c.owner.AgentID, tab: OfferedTab{TabID: c.scope.TabID, TabGeneration: c.scope.TabGeneration, ProfileID: c.scope.ProfileID, DocumentRevision: next.DocumentRevision, URL: next.URL, Title: next.Title, Preview: clonePreview(c.scope.Preview)}}
	}
	return cloneAttachment(next), nil
}

func applyMetadata(a *Attachment, r CommandResult) {
	if r.DocumentRevision != "" {
		a.DocumentRevision = r.DocumentRevision
	}
	if r.URL != "" {
		a.URL = cleanURL(r.URL)
	}
	if r.Title != "" {
		a.Title = cleanTitle(r.Title)
	}
}
