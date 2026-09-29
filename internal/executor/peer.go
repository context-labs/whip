package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type queued struct {
	event Event
	bytes int
	call  *Call
}

// Peer is an opaque connection identity. The transport owns Close and exactly
// one Next loop. It never outlives the connection and is never reused on reconnect.
type Peer struct {
	registry *Registry
	notify   chan struct{}
	done     chan struct{}
	queue    []queued
}

func (p *Peer) Close() {
	r := p.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disconnect(p, ErrDisconnected)
}

func (r *Registry) disconnect(peer *Peer, reason error) {
	if !r.peers[peer] {
		return
	}
	delete(r.peers, peer)
	close(peer.done)
	for _, item := range peer.queue {
		r.queuedBytes -= item.bytes
	}
	peer.queue = nil
	for ref, lease := range r.leases {
		if lease.peer == peer {
			delete(r.leases, ref)
		}
	}
	for _, call := range r.calls {
		if call.holder.peer == peer {
			r.finish(call, Result{}, reason, false)
		}
	}
	r.signal()
}

func (r *Registry) enqueue(peer *Peer, event Event, call *Call) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("invalid executor event: %w", err)
	}
	if len(encoded) > MaxRequestBytes {
		return fmt.Errorf("executor event exceeds %d bytes", MaxRequestBytes)
	}
	if !r.peers[peer] {
		return ErrDisconnected
	}
	if len(peer.queue) >= MaxQueuedEvents || len(encoded) > MaxQueuedBytes-r.queuedBytes {
		return ErrCapacity
	}
	peer.queue = append(peer.queue, queued{event: event, bytes: len(encoded), call: call})
	r.queuedBytes += len(encoded)
	select {
	case peer.notify <- struct{}{}:
	default:
	}
	return nil
}

// Next transfers one independently owned event. The registry starts no pump
// goroutine and never blocks while holding a lock or writing to a transport.
func (p *Peer) Next(ctx context.Context) (Event, error) {
	r := p.registry
	for {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		r.mu.Lock()
		if !r.peers[p] {
			r.mu.Unlock()
			return Event{}, ErrDisconnected
		}
		if len(p.queue) > 0 {
			item := p.queue[0]
			p.queue[0] = queued{}
			p.queue = p.queue[1:]
			r.queuedBytes -= item.bytes
			event := item.event
			if item.call != nil {
				item.call.delivered = true
			}
			if event.Invocation != nil {
				event.Invocation = new(cloneInvocation(*event.Invocation))
			}
			r.mu.Unlock()
			return event, nil
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-p.done:
			return Event{}, ErrDisconnected
		case <-p.notify:
		}
	}
}

// Pending observes only this connection's published calls at the exact current
// generation. It does not publish, claim, acknowledge, or replay any invocation.
// At most four independently owned invocations fit each bounded page.
func (p *Peer) Pending(lease Lease, after string) (Page, error) {
	r := p.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	holder := r.leases[lease.Definition]
	if !r.peers[p] || lease.Epoch != r.epoch || holder == nil || holder.peer != p || holder.lease.Generation != lease.Generation {
		return Page{}, ErrConflict
	}
	ids := make([]string, 0, MaxPeerCalls)
	for id, call := range r.calls {
		if call.holder == holder && call.invocation != nil && id > after {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	page := Page{Items: []Invocation{}}
	bytes := 1024
	for _, id := range ids {
		invocation := r.calls[id].invocation
		encoded, err := json.Marshal(invocation)
		if err != nil {
			return Page{}, err
		}
		if len(page.Items) == 4 || len(encoded) > 4*MaxRequestBytes-bytes {
			page.Next = page.Items[len(page.Items)-1].ID
			break
		}
		bytes += len(encoded)
		page.Items = append(page.Items, cloneInvocation(*invocation))
	}
	return page, nil
}

func (p *Peer) owned(epoch string, generation int64, id string, kind Kind) (*Call, error) {
	r := p.registry
	call := r.calls[id]
	if !r.peers[p] || epoch != r.epoch || call == nil || call.invocation == nil || call.holder.peer != p || call.holder.lease.Generation != generation || call.kind != kind {
		return nil, ErrConflict
	}
	return call, nil
}

// Settle accepts one bounded, kind-correct result from its exact holder.
// Invalid replies leave the invocation pending; completed or late replies fail.
func (p *Peer) Settle(epoch string, generation int64, id string, kind Kind, result Result) error {
	r := p.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	call, err := p.owned(epoch, generation, id, kind)
	if err != nil {
		return err
	}
	if err := result.validate(kind, call.name); err != nil {
		return err
	}
	r.finish(call, cloneResult(result), nil, false)
	return nil
}

// Progress coalesces one bounded latest notice, never an append-only queue.
func (p *Peer) Progress(epoch string, generation int64, id, text string) error {
	if !boundedText(text, MaxProgressBytes) {
		return errors.New("executor progress exceeds bounds")
	}
	r := p.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	call, err := p.owned(epoch, generation, id, Tool)
	if err != nil {
		return err
	}
	select {
	case <-call.progress:
	default:
	}
	call.progress <- text
	return nil
}
