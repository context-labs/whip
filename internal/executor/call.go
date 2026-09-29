package executor

import "context"

// Call owns one reserved invocation slot. The caller must Close it on all paths,
// including a SQL dispatch rejection. Invoke is one-shot and never retries.
type Call struct {
	registry                     *Registry
	holder                       *holder
	id                           string
	kind                         Kind
	name                         string
	done                         chan struct{}
	progress                     chan string
	invocation                   *Invocation
	delivered, finished, invoked bool
	result                       Result
	err                          error
}

func (c *Call) ID() string   { return c.id }
func (c *Call) Lease() Lease { return cloneLease(c.holder.lease) }

// Check rejects a revoked reservation immediately before durable dispatch.
// Invoke repeats this check, closing the race without holding a lock across SQL.
func (c *Call) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	return c.check()
}

func (c *Call) check() error {
	if c.finished {
		if c.err != nil {
			return c.err
		}
		return ErrConflict
	}
	r := c.registry
	if r.closed {
		return ErrClosed
	}
	if !r.peers[c.holder.peer] {
		return ErrDisconnected
	}
	if r.leases[c.holder.lease.Definition] != c.holder {
		return ErrReplaced
	}
	return nil
}

func (c *Call) Close() {
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finish(c, Result{}, context.Canceled, true)
}

// Invoke may be called only after the operation's durable dispatch (tools), or
// from the captured hook's owned turn/cell lifetime. Publication and generation
// checking are atomic; an unavailable captured peer never selects a newer lease.
// Progress is synchronous in this caller and must not perform blocking I/O.
func (c *Call) Invoke(ctx context.Context, request Request, progress func(string)) (Result, error) {
	r := c.registry
	r.mu.Lock()
	if err := c.check(); err != nil {
		r.mu.Unlock()
		return Result{}, err
	}
	if c.invoked {
		r.mu.Unlock()
		return Result{}, ErrConflict
	}
	c.invoked = true
	r.mu.Unlock()
	request, err := request.validate(c.kind, c.name)
	if err != nil {
		c.Close()
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		c.Close()
		return Result{}, err
	}
	deadline, _ := ctx.Deadline()
	r.mu.Lock()
	if err := ctx.Err(); err != nil {
		r.finish(c, Result{}, err, false)
		r.mu.Unlock()
		return Result{}, err
	}
	if err := c.check(); err != nil {
		r.mu.Unlock()
		return Result{}, err
	}
	c.invocation = &Invocation{ID: c.id, Lease: cloneLease(c.holder.lease), Kind: c.kind, Name: c.name, Request: cloneRequest(request), Deadline: deadline.UTC()}
	event := Event{Type: "invoke", Invocation: c.invocation, ID: c.id, Epoch: r.epoch, Generation: c.holder.lease.Generation}
	if err := r.enqueue(c.holder.peer, event, c); err != nil {
		r.finish(c, Result{}, err, false)
		r.mu.Unlock()
		return Result{}, err
	}
	r.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.finish(c, Result{}, ctx.Err(), true)
			result, err := cloneResult(c.result), c.err
			r.mu.Unlock()
			return result, err
		case <-c.done:
			r.mu.Lock()
			result, err := cloneResult(c.result), c.err
			r.mu.Unlock()
			return result, err
		case text := <-c.progress:
			if progress != nil && ctx.Err() == nil {
				progress(text)
			}
		}
	}
}

func (r *Registry) finish(call *Call, result Result, err error, cancel bool) {
	if call.finished {
		return
	}
	call.finished = true
	call.result, call.err = result, err
	delete(r.calls, call.id)
	r.active--
	peer := call.holder.peer
	kept := peer.queue[:0]
	for _, item := range peer.queue {
		if item.call == call {
			r.queuedBytes -= item.bytes
			continue
		}
		kept = append(kept, item)
	}
	clear(peer.queue[len(kept):])
	peer.queue = kept
	call.invocation = nil
	close(call.done)
	if cancel && call.delivered && r.peers[peer] {
		event := Event{Type: "cancel", ID: call.id, Epoch: r.epoch, Generation: call.holder.lease.Generation}
		if r.enqueue(peer, event, nil) != nil {
			r.disconnect(peer, ErrDisconnected)
		}
	}
}
