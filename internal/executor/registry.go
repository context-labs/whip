package executor

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/context-labs/whip/internal/session"
)

type Coverage struct{ Tools, Hooks []string }

type Registry struct {
	mu                           sync.Mutex
	epoch                        string
	closed                       bool
	generation                   int64
	peers                        map[*Peer]bool
	leases                       map[session.DefinitionRef]*holder
	calls                        map[string]*Call
	active, waiting, queuedBytes int
	changed                      chan struct{}
}

type holder struct {
	lease Lease
	peer  *Peer
}

// New starts no goroutines. Runtime owns Close; callers own every wait context.
func New() *Registry {
	return &Registry{epoch: "executor_" + rand.Text(), peers: map[*Peer]bool{}, leases: map[session.DefinitionRef]*holder{}, calls: map[string]*Call{}, changed: make(chan struct{})}
}

func (r *Registry) Epoch() string { return r.epoch }

func (r *Registry) Peer() (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	if len(r.peers) >= MaxPeers {
		return nil, ErrCapacity
	}
	peer := &Peer{registry: r, notify: make(chan struct{}, 1), done: make(chan struct{})}
	r.peers[peer] = true
	return peer, nil
}

// Close revokes peers and wakes reserved, invoked and bind-waiting callers. It
// has no independent workers to join; callers join their owning runtime turns.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for peer := range r.peers {
		r.disconnect(peer, ErrClosed)
	}
	r.signal()
}

func (r *Registry) signal() { close(r.changed); r.changed = make(chan struct{}) }

func coverage(declared, offered []string) bool {
	if len(declared) != len(offered) {
		return false
	}
	seen := make(map[string]bool, len(offered))
	for _, name := range offered {
		if seen[name] || !slices.Contains(declared, name) {
			return false
		}
		seen[name] = true
	}
	return true
}

// Bind requires all declared handlers and no extras. Runtime must load document
// from its registered revision before calling this method; no client-supplied
// document or mutable session alias can supply that authority.
func (p *Peer) Bind(ctx context.Context, document session.DefinitionDocument, offered Coverage) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	canonical, _, ref, err := session.CanonicalDefinition(document)
	if err != nil {
		return Lease{}, err
	}
	tools, hooks := slices.Sorted(maps.Keys(canonical.Defaults.Tools)), slices.Sorted(maps.Keys(canonical.Defaults.Hooks))
	if len(tools)+len(hooks) == 0 || !coverage(tools, offered.Tools) || !coverage(hooks, offered.Hooks) {
		return Lease{}, fmt.Errorf("%w: executor handlers must exactly cover declared tools and hooks", session.ErrInvalid)
	}
	r := p.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	if r.closed {
		return Lease{}, ErrClosed
	}
	if !r.peers[p] {
		return Lease{}, ErrDisconnected
	}
	old := r.leases[ref]
	owned := 0
	for _, lease := range r.leases {
		if lease.peer == p {
			owned++
		}
	}
	if old == nil && len(r.leases) >= MaxLeases || (old == nil || old.peer != p) && owned >= MaxPeerLeases || r.generation == math.MaxInt64 {
		return Lease{}, ErrCapacity
	}
	if old != nil {
		for _, call := range r.calls {
			if call.holder == old {
				r.finish(call, Result{}, ErrReplaced, true)
			}
		}
	}
	// Finishing old calls may disconnect a peer whose bounded cancel queue filled.
	if !r.peers[p] {
		return Lease{}, ErrDisconnected
	}
	r.generation++
	lease := Lease{Epoch: r.epoch, Definition: ref, Generation: r.generation, Tools: tools, Hooks: hooks}
	r.leases[ref] = &holder{lease: lease, peer: p}
	r.signal()
	return cloneLease(lease), nil
}

// Acquire reserves a current lease and bounded invocation capacity after any
// permission wait. It publishes nothing. Check immediately before SQL dispatch;
// Invoke rechecks the same lease after dispatch and never borrows its replacement.
func (r *Registry) Acquire(ctx context.Context, ref session.DefinitionRef, kind Kind, name string) (call *Call, err error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if (kind != Tool && kind != Hook) || session.ValidateID(name) != nil {
		return nil, fmt.Errorf("%w: invalid executor invocation", session.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, BindWait)
	defer cancel()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	if r.active >= MaxCalls {
		r.mu.Unlock()
		return nil, ErrCapacity
	}
	r.active++
	waiting := false
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if waiting {
			r.waiting--
		}
		if err != nil {
			r.active--
		}
	}()
	for {
		if err = ctx.Err(); err != nil {
			r.mu.Unlock()
			return nil, err
		}
		if r.closed {
			r.mu.Unlock()
			return nil, ErrClosed
		}
		if waitCtx.Err() != nil {
			r.mu.Unlock()
			return nil, ErrUnavailable
		}
		lease := r.leases[ref]
		if lease != nil {
			names := lease.lease.Tools
			if kind == Hook {
				names = lease.lease.Hooks
			}
			if !slices.Contains(names, name) {
				r.mu.Unlock()
				return nil, fmt.Errorf("%w: handler is not declared", session.ErrInvalid)
			}
			count := 0
			for _, pending := range r.calls {
				if pending.holder.peer == lease.peer {
					count++
				}
			}
			if count >= MaxPeerCalls {
				r.mu.Unlock()
				return nil, ErrCapacity
			}
			call = &Call{registry: r, holder: lease, id: "invocation_" + rand.Text(), kind: kind, name: name, done: make(chan struct{}), progress: make(chan string, 1)}
			r.calls[call.id] = call
			r.mu.Unlock()
			return call, nil
		}
		if !waiting {
			if r.waiting >= MaxBindWaiters {
				r.mu.Unlock()
				return nil, ErrCapacity
			}
			waiting = true
			r.waiting++
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-waitCtx.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, ErrUnavailable
		case <-changed:
		}
		r.mu.Lock()
	}
}

func cloneLease(lease Lease) Lease {
	lease.Tools = slices.Clone(lease.Tools)
	lease.Hooks = slices.Clone(lease.Hooks)
	return lease
}

func cloneRequest(request Request) Request {
	request.Arguments = slices.Clone(request.Arguments)
	request.Spawn = slices.Clone(request.Spawn)
	return request
}

func cloneInvocation(invocation Invocation) Invocation {
	invocation.Lease = cloneLease(invocation.Lease)
	invocation.Request = cloneRequest(invocation.Request)
	return invocation
}

func cloneResult(result Result) Result {
	result.Value = slices.Clone(result.Value)
	result.Arguments = slices.Clone(result.Arguments)
	result.Spawn = slices.Clone(result.Spawn)
	return result
}
