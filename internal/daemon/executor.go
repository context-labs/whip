package daemon

import (
	"context"
	"crypto/rand"
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
	hooks      []string
}

// invocationOutcome settles one invocation: a tool's output or a hook's
// decision, or the error that ended the wait.
type invocationOutcome struct {
	output   string
	decision hookDecision
	err      error
}

// pendingInvocation is one in-flight tool call or hook. For tools the durable
// record is the ledger operation row; this is the wait state that connects an
// invocation to an executor.
type pendingInvocation struct {
	lease    *executorLease
	tool     *protocol.ToolInvokeParams
	hook     *protocol.HookInvokeParams
	progress func(string)
	result   chan invocationOutcome
}

// hookInvocation is one hook call the daemon asks the executor to decide.
type hookInvocation struct {
	Definition     string
	Revision       string
	RootID         string
	AgentID        string
	TurnID         string
	Hook           string
	Operation      string
	Arguments      json.RawMessage
	Spawn          *protocol.SpawnPreview
	Input          string
	PermissionMode string
	Timeout        time.Duration
}

// hookDecision is an answered hook. The zero value is allow, unchanged. Failed
// marks a handler error: a deny for a required hook, a skip for an optional
// one. An unanswered hook is an error from InvokeHook, never a decision.
type hookDecision struct {
	InvocationID string
	Deny         bool
	Failed       bool
	// Skipped marks an optional hook that went unanswered or failed; the
	// operation proceeds unchanged.
	Skipped   bool
	Reason    string
	Arguments json.RawMessage
	Spawn     *protocol.SpawnRequest
	Context   string
}

// maxHookContextBytes bounds a turn_start contribution.
const maxHookContextBytes = 4 << 10

// executorRegistry pairs bound executors with tool and hook invocations. One
// lease per definition revision; a later bind replaces the holder and its
// pending calls fail rather than replay.
type executorRegistry struct {
	mu         sync.Mutex
	leases     map[string]*executorLease
	pending    map[string]*pendingInvocation
	generation int64
	bound      chan struct{} // closed and replaced on every bind, waking waiters
	bindWait   time.Duration
}

func newExecutorRegistry() *executorRegistry {
	return &executorRegistry{leases: make(map[string]*executorLease), pending: make(map[string]*pendingInvocation), bound: make(chan struct{}), bindWait: defaultExecutorBindWait}
}

func leaseKey(definition, revision string) string { return definition + "@" + revision }

// bind installs conn as the executor for a definition revision and returns the
// lease generation. Pending invocations of a replaced holder fail.
func (r *executorRegistry) bind(conn executorConn, definition, revision string, toolNames, hookNames []string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := leaseKey(definition, revision)
	if previous := r.leases[key]; previous != nil {
		r.failLeaseLocked(previous, errors.New("executor replaced by a newer bind"))
	}
	r.generation++
	r.leases[key] = &executorLease{key: key, generation: r.generation, conn: conn, tools: slices.Clone(toolNames), hooks: slices.Clone(hookNames)}
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
			invocation.result <- invocationOutcome{err: err}
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

// pendingFor lists the tool and hook invocations awaiting the lease conn
// holds, earliest deadline first.
func (r *executorRegistry) pendingFor(conn executorConn, params protocol.ExecutorPendingParams) (protocol.ExecutorPendingResult, error) {
	lease, err := r.holder(conn, params.Definition, params.Revision, params.Generation)
	if err != nil {
		return protocol.ExecutorPendingResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := protocol.ExecutorPendingResult{Invocations: make([]protocol.ToolInvokeParams, 0)}
	for _, invocation := range r.pending {
		if invocation.lease != lease {
			continue
		}
		if invocation.tool != nil {
			result.Invocations = append(result.Invocations, *invocation.tool)
		} else {
			result.Hooks = append(result.Hooks, *invocation.hook)
		}
	}
	slices.SortFunc(result.Invocations, func(a, b protocol.ToolInvokeParams) int { return int(a.DeadlineMillis - b.DeadlineMillis) })
	slices.SortFunc(result.Hooks, func(a, b protocol.HookInvokeParams) int { return int(a.DeadlineMillis - b.DeadlineMillis) })
	return result, nil
}

// take removes and returns the pending invocation a reply names when the
// reply comes from the lease holder at the right generation; anything else
// is rejected, so late, duplicate, and foreign replies never settle a call.
func (r *executorRegistry) take(conn executorConn, id string, generation int64, hook bool) (*pendingInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invocation := r.pending[id]
	if invocation == nil || invocation.lease.conn != conn || invocation.lease.generation != generation {
		return nil, errors.New("invocation is unknown, settled, or held by another executor")
	}
	if (invocation.hook != nil) != hook {
		return nil, errors.New("invocation kind does not match the reply")
	}
	delete(r.pending, id)
	return invocation, nil
}

// settle accepts a tool result from the lease holder.
func (r *executorRegistry) settle(conn executorConn, params protocol.ToolResultParams) error {
	invocation, err := r.take(conn, params.InvocationID, params.Generation, false)
	if err != nil {
		return err
	}
	outcome := invocationOutcome{output: string(params.Output)}
	if params.Error != "" {
		outcome = invocationOutcome{err: errors.New(params.Error)}
	} else if len(params.Output) == 0 || !json.Valid(params.Output) {
		outcome = invocationOutcome{err: errors.New("tool returned no JSON result")}
	}
	invocation.result <- outcome
	return nil
}

// settleHook accepts a hook reply from the lease holder. Absent fields mean
// allow, unchanged; malformed fields reject the reply so the call keeps
// waiting for a valid one or its deadline.
func (r *executorRegistry) settleHook(conn executorConn, params protocol.HookResultParams) error {
	decision := hookDecision{InvocationID: params.InvocationID, Reason: params.Reason, Spawn: params.Spawn, Context: params.Context}
	switch params.Decision {
	case "", "allow":
	case "deny":
		decision.Deny = true
	default:
		return fmt.Errorf("hook decision must be allow or deny, not %q", params.Decision)
	}
	if len(params.Arguments) > 0 {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(params.Arguments, &object); err != nil || object == nil {
			return errors.New("hook arguments must be a JSON object")
		}
		decision.Arguments = params.Arguments
	}
	if len(decision.Context) > maxHookContextBytes {
		decision.Context = utf8PrefixRuntime(decision.Context, maxHookContextBytes)
	}
	if params.Error != "" {
		decision = hookDecision{InvocationID: params.InvocationID, Deny: true, Failed: true, Reason: params.Error}
	}
	invocation, err := r.take(conn, params.InvocationID, params.Generation, true)
	if err != nil {
		return err
	}
	invocation.result <- invocationOutcome{decision: decision}
	return nil
}

// report forwards progress text from the lease holder.
func (r *executorRegistry) report(conn executorConn, params protocol.ToolProgressParams) error {
	r.mu.Lock()
	invocation := r.pending[params.InvocationID]
	r.mu.Unlock()
	if invocation == nil || invocation.lease.conn != conn || invocation.lease.generation != params.Generation || invocation.tool == nil {
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
	pending := &pendingInvocation{lease: lease, tool: &params, progress: invocation.Progress, result: make(chan invocationOutcome, 1)}
	outcome, err := r.await(ctx, lease, pending, params.InvocationID, "tool.invoke", "tool.cancel", params, deadline, func() error {
		return fmt.Errorf("tool %s timed out after %s", invocation.Tool, timeout)
	})
	if err != nil {
		return "", err
	}
	return outcome.output, outcome.err
}

// InvokeHook asks the bound executor for a hook decision. Like Invoke it
// waits briefly for a lease and settles on the reply, the deadline, turn
// cancellation, or the executor's disconnect; every error means the hook was
// not answered, and the caller applies the hook's required or optional rule.
func (r *executorRegistry) InvokeHook(ctx context.Context, invocation hookInvocation) (hookDecision, error) {
	lease, err := r.awaitLease(ctx, invocation.Definition, invocation.Revision)
	if err != nil {
		return hookDecision{}, fmt.Errorf("hook %s: %w", invocation.Hook, err)
	}
	timeout := invocation.Timeout
	if timeout <= 0 {
		timeout = agentdef.DefaultHookTimeout
	}
	deadline := time.Now().Add(timeout)
	params := protocol.HookInvokeParams{
		InvocationID: "hook-" + rand.Text(), Definition: invocation.Definition, Revision: invocation.Revision, Generation: lease.generation,
		RootID: invocation.RootID, AgentID: invocation.AgentID, TurnID: invocation.TurnID, Hook: invocation.Hook, Operation: invocation.Operation,
		Arguments: invocation.Arguments, Spawn: invocation.Spawn, Input: invocation.Input, PermissionMode: invocation.PermissionMode,
		DeadlineMillis: deadline.UnixMilli(),
	}
	pending := &pendingInvocation{lease: lease, hook: &params, result: make(chan invocationOutcome, 1)}
	outcome, err := r.await(ctx, lease, pending, params.InvocationID, "hook.invoke", "hook.cancel", params, deadline, func() error {
		return fmt.Errorf("hook %s timed out after %s", invocation.Hook, timeout)
	})
	if err != nil {
		return hookDecision{}, err
	}
	if outcome.err != nil {
		return hookDecision{}, fmt.Errorf("hook %s: %w", invocation.Hook, outcome.err)
	}
	return outcome.decision, nil
}

// await records a pending invocation, notifies the executor, and waits for
// one of the four ways it can end. A call that leaves here is never replayed.
func (r *executorRegistry) await(ctx context.Context, lease *executorLease, pending *pendingInvocation, id, invoke, cancelMethod string, params any, deadline time.Time, timedOut func() error) (invocationOutcome, error) {
	r.mu.Lock()
	r.pending[id] = pending
	r.mu.Unlock()
	if !lease.conn.notify(invoke, params) {
		r.abandon(id)
		return invocationOutcome{}, errors.New("executor disconnected")
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	cancel := func(reason string) {
		if r.abandon(id) {
			lease.conn.notify(cancelMethod, protocol.ToolCancelParams{InvocationID: id, Generation: lease.generation, Reason: reason})
		}
	}
	select {
	case outcome := <-pending.result:
		return outcome, nil
	case <-ctx.Done():
		cancel("cancelled")
		return invocationOutcome{}, ctx.Err()
	case <-timer.C:
		cancel("timeout")
		return invocationOutcome{}, timedOut()
	case <-lease.conn.finished():
		r.abandon(id)
		return invocationOutcome{}, errors.New("executor disconnected")
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
