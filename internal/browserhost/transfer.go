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

type TransferCapture struct {
	host      *Host
	provider  *provider
	parent    Identity
	child     Identity
	parents   []*attachment
	snapshots []Attachment
	children  []Attachment
	used      bool
}

// PrepareTransfer resolves a prospective owner without creating it. The caller
// derives child identity from its admitted operation/receipt, not from this core.
func (h *Host) PrepareTransfer(ctx context.Context, parent, child Identity, ids []string) (*TransferCapture, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !parent.valid() || !child.valid() || parent.RootID != child.RootID || parent.AgentID == child.AgentID || len(ids) == 0 || len(ids) > 4 {
		return nil, errors.New("invalid browser transfer")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		return nil, errors.New("duplicate transfer attachment")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	v, e := h.destinationLocked(parent.RootID)
	if e != nil {
		return nil, e
	}
	capture := &TransferCapture{host: h, provider: v, parent: parent, child: child}
	seenTabs := map[string]bool{}
	for _, id := range ids {
		a := v.attachments[id]
		if a == nil || a.value.Owner != parent || !a.live || a.value.Delegated || a.ctx.Err() != nil || seenTabs[a.value.Scope.TabID] {
			return nil, ErrStale
		}
		seenTabs[a.value.Scope.TabID] = true
		capture.parents = append(capture.parents, a)
		capture.snapshots = append(capture.snapshots, cloneAttachment(a.value))
		next := cloneAttachment(a.value)
		next.Owner = child
		next.Scope.AttachmentID = rand.Text()
		next.Scope.AttachmentGeneration = rand.Text()
		next.Delegated = false
		capture.children = append(capture.children, next)
	}
	return capture, nil
}

// Parents returns immutable source scope for durable permission presentation.
func (c *TransferCapture) Parents() []Attachment {
	out := make([]Attachment, len(c.snapshots))
	for i, a := range c.snapshots {
		out[i] = cloneAttachment(a)
	}
	return out
}

func (c *TransferCapture) Attachments() []Attachment {
	out := make([]Attachment, len(c.children))
	for i, a := range c.children {
		out[i] = cloneAttachment(a)
	}
	return out
}
func (c *TransferCapture) Lifetime() context.Context { return c.provider.ctx }
func (c *TransferCapture) currentLocked() bool {
	if !c.host.currentLocked(c.provider) {
		return false
	}
	for i, a := range c.parents {
		if a.ctx.Err() != nil || !a.live || a.value.Delegated || a.value.Owner != c.parent || !reflect.DeepEqual(a.value.Scope, c.snapshots[i].Scope) {
			return false
		}
	}
	return true
}

type TransferLease struct {
	capture   *TransferCapture
	children  []*attachment
	ctx       context.Context
	cancel    context.CancelFunc
	stop      []func() bool
	releases  []func()
	mu        sync.Mutex
	closed    bool
	used      bool
	wg        sync.WaitGroup
	once      sync.Once
	activated bool // host.mu
}

func (c *TransferCapture) Acquire(ctx context.Context) (*TransferLease, error) {
	h := c.host
	h.mu.Lock()
	if c.used || !c.currentLocked() {
		h.mu.Unlock()
		return nil, ErrStale
	}
	c.used = true
	h.pruneLocked(c.provider)
	if len(c.provider.attachments)+len(c.children) > MaxRecords {
		h.mu.Unlock()
		return nil, ErrBusy
	}
	if e := h.startLocked(c.provider.peer); e != nil {
		h.mu.Unlock()
		return nil, e
	}
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	l := &TransferLease{capture: c, ctx: ctx, cancel: cancel}
	for _, a := range c.parents {
		l.stop = append(l.stop, context.AfterFunc(a.ctx, cancel))
	}
	// Order by tab identity, not by caller-provided attachment order.
	parents := slices.Clone(c.parents)
	slices.SortFunc(parents, func(a, b *attachment) int { return stringsCompare(a.value.Scope.TabID, b.value.Scope.TabID) })
	for _, a := range parents {
		release, e := a.tab.acquire(ctx, true)
		if e != nil {
			l.Close()
			return nil, e
		}
		l.releases = append(l.releases, release)
	}
	h.mu.Lock()
	if !c.currentLocked() || ctx.Err() != nil || len(c.provider.attachments)+len(c.children) > MaxRecords {
		h.mu.Unlock()
		l.Close()
		return nil, ErrStale
	}
	for i, next := range c.children {
		actx, acancel := context.WithCancel(c.provider.ctx)
		a := &attachment{provider: c.provider, value: cloneAttachment(next), ctx: actx, cancel: acancel, tab: c.parents[i].tab, parent: c.parents[i]}
		c.provider.attachments[next.Scope.AttachmentID] = a
		l.children = append(l.children, a)
	}
	h.mu.Unlock()
	return l, nil
}
func (l *TransferLease) Lifetime() context.Context { return l.ctx }
func (l *TransferLease) Close() {
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.cancel()
		l.mu.Unlock()
		l.wg.Wait()
		for _, stop := range l.stop {
			stop()
		}
		h := l.capture.host
		h.mu.Lock()
		if !l.activated {
			for _, a := range l.children {
				a.cancel()
				delete(l.capture.provider.attachments, a.value.Scope.AttachmentID)
			}
		}
		h.mu.Unlock()
		for _, release := range slices.Backward(l.releases) {
			release()
		}
		h.finish(l.capture.provider.peer)
	})
}

// Execute makes one atomic native handoff, then invokes commit while every tab
// remains reserved. Commit must recheck all SQL authority/capacity/lifecycle facts
// while provisional child controls wait behind these same tab reservations.
// A scheduler already polling may capture them, but cannot dispatch yet. Host effects and SQL
// are not atomic: unknown delivery/commit retires every participant, never replays
// ownership or restores the parent. A known native rejection preserves parents.
func (l *TransferLease) Execute(ctx context.Context, operationID string, check Check, commit func(context.Context, []Attachment) error) ([]Attachment, error) {
	if !token(operationID) || check == nil || commit == nil {
		return nil, errors.New("transfer requires dispatched authority and commit callbacks")
	}
	l.mu.Lock()
	if l.closed || l.used {
		l.mu.Unlock()
		return nil, ErrStale
	}
	l.used = true
	l.wg.Add(1)
	l.mu.Unlock()
	defer l.wg.Done()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	stop := context.AfterFunc(l.ctx, cancel)
	defer func() { stop(); cancel() }()
	c := l.capture
	h := c.host
	if e := check(ctx); e != nil {
		return nil, e
	}
	h.mu.Lock()
	current := c.currentLocked()
	h.mu.Unlock()
	if !current || ctx.Err() != nil {
		return nil, ErrStale
	}
	args := TransferArguments{ChildAgentID: c.child.AgentID, Attachments: make([]TransferPair, len(c.parents))}
	for i := range c.parents {
		args.Attachments[i] = TransferPair{Parent: cloneScope(c.snapshots[i].Scope), Child: cloneScope(l.children[i].value.Scope)}
	}
	raw, _ := json.Marshal(args)
	_, e := h.command(ctx, c.parents[0], operationID, "transfer", raw, "", check, nil)
	if e != nil {
		if errors.Is(e, ErrUnknown) {
			h.mu.Lock()
			for _, a := range c.parents {
				h.revokeLocked(a)
			}
			h.mu.Unlock()
		}
		return nil, e
	}
	// Once native acknowledged, no failure path may reinstate parent execution.
	committed := false
	defer func() {
		if !committed {
			h.mu.Lock()
			for _, a := range c.parents {
				h.revokeLocked(a)
			}
			h.mu.Unlock()
		}
	}()
	h.mu.Lock()
	current = c.currentLocked() && ctx.Err() == nil
	if current {
		// Native owns the child now. A committed child can resolve this handle,
		// then waits on the held tab reservation until activation is final.
		for i, a := range c.parents {
			a.value.Delegated = true
			l.children[i].live = true
		}
	}
	h.mu.Unlock()
	if !current {
		return nil, ErrUnknown
	}
	children := c.Attachments()
	if e = commit(ctx, children); e != nil {
		return nil, errors.Join(ErrUnknown, e)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !l.provisionalCurrentLocked() || ctx.Err() != nil {
		return nil, ErrUnknown
	}
	l.activated = true
	committed = true
	return c.Attachments(), nil
}

func (l *TransferLease) provisionalCurrentLocked() bool {
	c := l.capture
	if !c.host.currentLocked(c.provider) {
		return false
	}
	for i, a := range c.parents {
		child := l.children[i]
		if a.ctx.Err() != nil || !a.live || !a.value.Delegated || !reflect.DeepEqual(a.value.Scope, c.snapshots[i].Scope) || child.ctx.Err() != nil || !child.live || child.value.Delegated || !reflect.DeepEqual(child.value.Scope, c.children[i].Scope) {
			return false
		}
	}
	return true
}
