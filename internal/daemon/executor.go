package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tools"
)

// defaultExecutorBindWait bounds how long an invocation waits for an executor
// to bind before failing closed with an error the model reads.
const defaultExecutorBindWait = 5 * time.Second

// executorConn is the connection an executor lease lives on. *serverConn
// implements it; tests supply fakes.
type executorConn interface {
	notify(method string, params any) bool
	finished() <-chan struct{}
}

// executorLease is one bound executor for a definition revision.
type executorLease struct {
	key        string
	generation int64
	conn       executorConn
	tools      []string
}

type toolOutcome struct {
	output string
	err    error
}

// toolInvocation is one in-flight call. The durable record is the ledger
// operation row; this is the wait state that connects it to an executor.
type toolInvocation struct {
	lease    *executorLease
	params   protocol.ToolInvokeParams
	progress func(string)
	result   chan toolOutcome
}

// executorRegistry pairs bound executors with tool invocations. One lease per
// definition revision; a later bind replaces the holder and its pending calls
// fail rather than replay.
type executorRegistry struct {
	mu         sync.Mutex
	leases     map[string]*executorLease
	pending    map[string]*toolInvocation
	generation int64
	bound      chan struct{} // closed and replaced on every bind, waking waiters
	bindWait   time.Duration
}

func newExecutorRegistry() *executorRegistry {
	return &executorRegistry{leases: make(map[string]*executorLease), pending: make(map[string]*toolInvocation), bound: make(chan struct{}), bindWait: defaultExecutorBindWait}
}

func leaseKey(definition, revision string) string { return definition + "@" + revision }

// bind installs conn as the executor for a definition revision and returns the
// lease generation. Pending invocations of a replaced holder fail.
func (r *executorRegistry) bind(conn executorConn, definition, revision string, toolNames []string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := leaseKey(definition, revision)
	if previous := r.leases[key]; previous != nil {
		r.failLeaseLocked(previous, errors.New("executor replaced by a newer bind"))
	}
	r.generation++
	r.leases[key] = &executorLease{key: key, generation: r.generation, conn: conn, tools: slices.Clone(toolNames)}
	close(r.bound)
	r.bound = make(chan struct{})
	return r.generation
}

// disconnect drops every lease held by conn and fails their pending calls. A
// disconnected executor never sees those invocations again.
func (r *executorRegistry) disconnect(conn executorConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, lease := range r.leases {
		if lease.conn == conn {
			r.failLeaseLocked(lease, errors.New("executor disconnected"))
			delete(r.leases, key)
		}
	}
}

func (r *executorRegistry) failLeaseLocked(lease *executorLease, err error) {
	for id, invocation := range r.pending {
		if invocation.lease == lease {
			delete(r.pending, id)
			invocation.result <- toolOutcome{err: err}
		}
	}
}

// holder returns the lease for key when conn holds it at generation.
func (r *executorRegistry) holder(conn executorConn, definition, revision string, generation int64) (*executorLease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lease := r.leases[leaseKey(definition, revision)]
	if lease == nil || lease.conn != conn || lease.generation != generation {
		return nil, errors.New("executor lease is not held by this connection")
	}
	return lease, nil
}

// pendingFor lists invocations awaiting the lease conn holds, oldest first.
func (r *executorRegistry) pendingFor(conn executorConn, params protocol.ExecutorPendingParams) ([]protocol.ToolInvokeParams, error) {
	lease, err := r.holder(conn, params.Definition, params.Revision, params.Generation)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]protocol.ToolInvokeParams, 0)
	for _, invocation := range r.pending {
		if invocation.lease == lease {
			result = append(result, invocation.params)
		}
	}
	slices.SortFunc(result, func(a, b protocol.ToolInvokeParams) int { return int(a.DeadlineMillis - b.DeadlineMillis) })
	return result, nil
}

// settle accepts a result from the lease holder; anything else is rejected.
func (r *executorRegistry) settle(conn executorConn, params protocol.ToolResultParams) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	invocation := r.pending[params.InvocationID]
	if invocation == nil || invocation.lease.conn != conn || invocation.lease.generation != params.Generation {
		return errors.New("invocation is unknown, settled, or held by another executor")
	}
	delete(r.pending, params.InvocationID)
	outcome := toolOutcome{output: string(params.Output)}
	if params.Error != "" {
		outcome = toolOutcome{err: errors.New(params.Error)}
	} else if len(params.Output) == 0 || !json.Valid(params.Output) {
		outcome = toolOutcome{err: errors.New("tool returned no JSON result")}
	}
	invocation.result <- outcome
	return nil
}

// report forwards progress text from the lease holder.
func (r *executorRegistry) report(conn executorConn, params protocol.ToolProgressParams) error {
	r.mu.Lock()
	invocation := r.pending[params.InvocationID]
	r.mu.Unlock()
	if invocation == nil || invocation.lease.conn != conn || invocation.lease.generation != params.Generation {
		return errors.New("invocation is unknown, settled, or held by another executor")
	}
	if invocation.progress != nil {
		invocation.progress(params.Text)
	}
	return nil
}

// Invoke implements tools.ToolExecutor: it waits briefly for a bound executor,
// notifies it, and settles on the result, the deadline, cancellation, or the
// executor's disconnect. A call that leaves this method is never replayed.
func (r *executorRegistry) Invoke(ctx context.Context, invocation tools.ToolInvocation) (string, error) {
	lease, err := r.awaitLease(ctx, invocation.Definition, invocation.Revision)
	if err != nil {
		return "", err
	}
	timeout := invocation.Timeout
	if timeout <= 0 {
		timeout = agentdef.DefaultToolTimeout
	}
	deadline := time.Now().Add(timeout)
	params := protocol.ToolInvokeParams{
		InvocationID: invocation.OperationID, Definition: invocation.Definition, Revision: invocation.Revision, Generation: lease.generation,
		RootID: invocation.RootID, AgentID: invocation.AgentID, TurnID: invocation.TurnID, Tool: invocation.Tool, Input: invocation.Arguments,
		DeadlineMillis: deadline.UnixMilli(),
	}
	pending := &toolInvocation{lease: lease, params: params, progress: invocation.Progress, result: make(chan toolOutcome, 1)}
	r.mu.Lock()
	r.pending[params.InvocationID] = pending
	r.mu.Unlock()
	if !lease.conn.notify("tool.invoke", params) {
		r.abandon(params.InvocationID)
		return "", errors.New("executor disconnected")
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	cancel := func(reason string) {
		if r.abandon(params.InvocationID) {
			lease.conn.notify("tool.cancel", protocol.ToolCancelParams{InvocationID: params.InvocationID, Generation: lease.generation, Reason: reason})
		}
	}
	select {
	case outcome := <-pending.result:
		return outcome.output, outcome.err
	case <-ctx.Done():
		cancel("cancelled")
		return "", ctx.Err()
	case <-timer.C:
		cancel("timeout")
		return "", fmt.Errorf("tool %s timed out after %s", invocation.Tool, timeout)
	case <-lease.conn.finished():
		r.abandon(params.InvocationID)
		return "", errors.New("executor disconnected")
	}
}

// abandon removes an invocation so late results are rejected; it reports
// whether the call was still pending.
func (r *executorRegistry) abandon(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.pending[id]; !ok {
		return false
	}
	delete(r.pending, id)
	return true
}

// awaitLease returns the current lease for the definition revision, waiting up
// to bindWait for one to appear.
func (r *executorRegistry) awaitLease(ctx context.Context, definition, revision string) (*executorLease, error) {
	deadline := time.NewTimer(r.bindWait)
	defer deadline.Stop()
	for {
		r.mu.Lock()
		lease, bound := r.leases[leaseKey(definition, revision)], r.bound
		r.mu.Unlock()
		if lease != nil {
			return lease, nil
		}
		select {
		case <-bound:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("no executor is bound for agent definition %s revision %s; custom tools run in the process that called client.agents.serve", definition, shortRevision(revision))
		}
	}
}

func shortRevision(revision string) string {
	if len(revision) > 12 {
		return revision[:12]
	}
	if revision == "" {
		return "(built-in)"
	}
	return revision
}
