package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

// Caps, mirrored from runtime.ts. Agent concurrency is min(16, cores-2);
// a run creates at most 1000 agents; one pipeline()/parallel() call processes
// at most 4096 items.
const (
	MaxConcurrency  = 16
	MaxAgentsPerRun = 1000
	MaxFanoutItems  = 4096
	maxNestDepth    = 1
)

// AgentOptions are the per-call opts a script passes to agent().
type AgentOptions struct {
	Label     string          `json:"label,omitempty"`
	Phase     string          `json:"phase,omitempty"`
	Schema    json.RawMessage `json:"schema,omitempty"`
	Model     string          `json:"model,omitempty"`
	Effort    string          `json:"effort,omitempty"`
	TimeoutMs int             `json:"timeoutMs,omitempty"`
}

// AgentRequest is one agent() call handed to the Runner.
type AgentRequest struct {
	Prompt  string
	Options AgentOptions
	Phase   string // resolved phase (opts.phase ?? current phase())
	Model   string // resolved (opts > phase > meta)
	Effort  string // resolved (opts > phase > meta)
	Index   int    // lexical call index (the resume key)
	Workdir string // the run's cwd
}

// Usage is one subagent run's token spend, folded into budget.spent().
type Usage struct{ Total int }

// Runner executes one agent() call (the whip side: a fresh subagent Turn).
// The result is a string (final report) or, with Options.Schema, the
// validated structured-output value.
type Runner func(ctx context.Context, req AgentRequest) (result any, usage Usage, err error)

// Events are live progress callbacks (the manager wires them to snapshots
// and journal persistence). OnAgentStart runs on the worker goroutine; the
// rest run on the scheduler goroutine. All must be cheap and non-blocking.
type Events struct {
	OnAgentStart func(index int, label, phase, model string)
	OnAgentEnd   func(index int, label, phase string, result any, tokens int, err string)
	OnJournal    func(e JournalEntry)
	OnPhase      func(title string)
	OnLog        func(msg string)
}

// Result is what a finished run produced.
type Result struct {
	RunID      string
	Meta       Meta
	Value      any
	AgentCount int
	Tokens     int
	Phases     []string
	Duration   time.Duration
	Journal    []JournalEntry
}

// Options configure one Run.
type Options struct {
	RunID string
	Cwd   string // script-visible cwd (also handed to the Runner)
	Args  any
	Run   Runner // required — the subagent bridge
	// ResumeJournal replays completed agent() calls from a prior run
	// (callIndex → entry). Built by JournalMap(LoadRun(id)).
	ResumeJournal map[int]JournalEntry
	Events        Events
	Concurrency   int // 0 = min(MaxConcurrency, NumCPU-2)
	MaxAgents     int // 0 = MaxAgentsPerRun
	// shared is set for a nested workflow(): caps/budget/limiter are shared
	// with the parent run so the global bounds hold across nesting.
	shared *sharedRuntime
}

// sharedRuntime is shared across a run and any nested workflow() so the
// global caps hold (runtime.ts SharedRuntime). The agent counter is an
// explicit mutex-guarded int — the count check and the reservation must be
// one atomic step or a fan-out could overshoot MaxAgents.
// sharedRuntime is shared across a run and any nested workflow() so the
// global caps hold (runtime.ts SharedRuntime). The counter, mutex, limiter,
// and budget all live behind pointers so a child run shares them; only
// `depth` is per-level.
type sharedRuntime struct {
	state *sharedState
	depth int
}

type sharedState struct {
	mu         sync.Mutex
	agentCount int
	sem        chan struct{} // the concurrency limiter: capacity IS the cap
	spent      atomic.Int64
}

// reserveAgent atomically checks the cap and takes a slot.
func (s *sharedRuntime) reserveAgent(max int) (n int, ok bool) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.agentCount >= max {
		return 0, false
	}
	s.state.agentCount++
	return s.state.agentCount, true
}

func (s *sharedRuntime) count() int {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.agentCount
}

// child returns the shared runtime for one nesting level down: same state,
// deeper depth.
func (s *sharedRuntime) child() *sharedRuntime {
	return &sharedRuntime{state: s.state, depth: s.depth + 1}
}

// runState is one run's mutable bookkeeping (runtime.ts RunState). Mutated
// only on the scheduler goroutine.
type runState struct {
	phases    []string
	phase     string
	callSeq   int
	firstMiss int
	tokens    int
	journal   []JournalEntry
	resume    map[int]JournalEntry
}

// determinismPrelude neuters the nondeterministic builtins inside the VM
// realm (runtime.ts DETERMINISM_PRELUDE — same trick, same realm-locality).
// ES5-safe (goja parses ES5.1 by default).
const determinismPrelude = `"use strict";
Math.random = function() { throw new Error("Math.random() is unavailable in a workflow (it breaks resume); vary by index or pass via args"); };
(function(g) {
  var RealDate = Date;
  var fail = function(w) { throw new Error(w + " is unavailable in a workflow (it breaks resume); pass a timestamp via args"); };
  function SafeDate() {
    if (!(this instanceof SafeDate)) { fail("Date()"); }
    if (arguments.length === 0) { fail("new Date()"); }
    var args = [null].concat(Array.prototype.slice.call(arguments));
    return new (Function.prototype.bind.apply(RealDate, args))();
  }
  SafeDate.UTC = RealDate.UTC;
  SafeDate.parse = RealDate.parse;
  SafeDate.now = function() { fail("Date.now()"); };
  SafeDate.prototype = RealDate.prototype;
  g.Date = SafeDate;
})(this);`

// Run executes one workflow script to completion and returns its result.
// Port of runtime.ts runWorkflow.
func Run(ctx context.Context, script string, opts Options) (*Result, error) {
	started := time.Now()
	meta, body, err := Parse(script)
	if err != nil {
		return nil, err
	}
	if opts.Run == nil {
		return nil, errors.New("workflow: Options.Run (the subagent runner) is required")
	}
	maxAgents := opts.MaxAgents
	if maxAgents <= 0 {
		maxAgents = MaxAgentsPerRun
	}
	cwd := opts.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	shared := opts.shared
	if shared == nil {
		conc := opts.Concurrency
		if conc <= 0 {
			if n := runtime.NumCPU() - 2; n > 0 {
				conc = n
			} else {
				conc = 1
			}
			if conc > MaxConcurrency {
				conc = MaxConcurrency
			}
		}
		shared = &sharedRuntime{state: &sharedState{sem: make(chan struct{}, conc)}}
	}

	st := &runState{firstMiss: 1 << 30, resume: opts.ResumeJournal}
	// Default the current phase to the first declared one so agents created
	// before an explicit phase() call still group under a declared phase.
	if len(meta.Phases) > 0 {
		st.phase = meta.Phases[0].Title
		st.phases = []string{meta.Phases[0].Title}
	}

	vm := goja.New()
	// TagFieldNameMapper so AgentOptions' json tags drive opts decoding.
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	sc := &scheduler{vm: vm, jobs: make(chan func()), done: make(chan struct{})}

	// ── Globals (runtime.ts sandbox) ──
	must(vm.Set("args", opts.Args))
	must(vm.Set("cwd", cwd))
	must(vm.Set("console", map[string]any{
		"log": func(args ...any) { sc.log(opts.Events.OnLog, joinArgs(args)) },
	}))
	must(vm.Set("budget", map[string]any{
		"total": nil,
		"spent": func() int64 { return shared.state.spent.Load() },
		// No token budget enforcement (better-workflows has total: null too).
		"remaining": func() goja.Value { return goja.PositiveInf() },
	}))
	must(vm.Set("log", func(msg any) { sc.log(opts.Events.OnLog, fmt.Sprint(msg)) }))
	must(vm.Set("phase", func(title string) { sc.setPhase(st, title, opts.Events.OnPhase) }))
	must(vm.Set("agent", sc.agentFunc(ctx, st, opts, shared, meta, maxAgents, cwd)))
	must(vm.Set("parallel", sc.parallelFunc(opts.Events.OnLog)))
	must(vm.Set("pipeline", sc.pipelineFunc(opts.Events.OnLog)))
	must(vm.Set("workflow", sc.nestedWorkflowFunc(ctx, opts, shared)))

	if _, err := vm.RunString(determinismPrelude); err != nil {
		return nil, fmt.Errorf("workflow: determinism prelude: %w", err)
	}

	// Unhandled promise rejections are logged, never crash the run. The
	// tracker fires synchronously from goja's async runner ON the scheduler
	// goroutine, so it must NOT enqueue (that self-deadlocks) — the rejection
	// detail is surfaced via the script promise's own reject path; this notes
	// it in the run log from a side goroutine.
	vm.SetPromiseRejectionTracker(func(p *goja.Promise, op goja.PromiseRejectionOperation) {
		if op == goja.PromiseRejectionReject && opts.Events.OnLog != nil {
			msg := fmt.Sprintf("unhandled promise rejection: %v", p.Result())
			go func() { opts.Events.OnLog(msg) }()
		}
	})

	go sc.loop()

	// Kick the script body; its completion (or throw) settles the run. The
	// body runs inside vm.Try so a JS exception thrown from a Go global (the
	// fanout-cap panic, a bad agent() arg) unwinds INTO the script as a real
	// JS throw — rejecting the async function's promise — instead of crashing
	// the scheduler goroutine.
	var runErr error
	var runValue any
	finished := make(chan struct{})
	sc.enqueue(func() {
		var promise *goja.Promise
		if exc := vm.Try(func() {
			v, err := vm.RunString("(async () => {\n" + body + "\n})()")
			if err != nil {
				panic(err)
			}
			p, ok := v.Export().(*goja.Promise)
			if !ok {
				panic(errors.New("workflow body did not evaluate to a promise"))
			}
			promise = p
		}); exc != nil {
			runErr = fmt.Errorf("%v", exc)
			close(finished)
			return
		}
		// .then callbacks are themselves scheduler jobs — the loop drains them
		// until the promise settles.
		awaitValue(vm, vm.ToValue(promise).ToObject(vm),
			func(v goja.Value) { runValue = v.Export(); close(finished) },
			func(e goja.Value) {
				runErr = fmt.Errorf("%v", exportErr(e))
				close(finished)
			})
	})

	// Wait for the script promise to settle or the context to die. A parked
	// script (awaiting an in-flight agent) can't be interrupted — goja's
	// async runner isn't executing VM instructions — so on cancel we settle
	// the run ourselves: interrupt (in case the VM IS mid-instruction) and
	// abandon the run rather than block on a promise that will never fire.
	select {
	case <-finished:
	case <-ctx.Done():
		vm.Interrupt(context.Canceled)
		// Give the interrupt a beat to unwind a mid-instruction script; if
		// the run is parked on an agent, don't wait for it — settle as
		// cancelled and let the worker goroutines drain on their own.
		select {
		case <-finished:
		case <-time.After(100 * time.Millisecond):
		}
		runErr = context.Canceled
	}
	close(sc.done)

	if shared.count() == 0 && runErr == nil {
		runErr = errors.New("workflow scripts must call agent() at least once; this run declared phases but spawned no agents")
	}
	if runErr != nil {
		return nil, runErr
	}
	return &Result{
		RunID:      opts.RunID,
		Meta:       meta,
		Value:      runValue,
		AgentCount: shared.count(),
		Tokens:     st.tokens,
		Phases:     st.phases,
		Duration:   time.Since(started),
		Journal:    st.journal,
	}, nil
}

func must(err error) {
	if err != nil {
		panic(err) // goja Set only fails on unconvertible Go values — a programmer error
	}
}

// scheduler owns the VM: a single goroutine runs every VM-touching job in
// arrival order. Worker goroutines (agent runs, fan-out wrappers) never touch
// the VM — they hand work back through enqueue, and promise resolve/reject
// happens ONLY on this goroutine (goja's NewPromise is not goroutine-safe).
type scheduler struct {
	vm   *goja.Runtime
	jobs chan func()
	done chan struct{}
}

func (s *scheduler) enqueue(job func()) {
	select {
	case s.jobs <- job:
	case <-s.done:
	}
}

func (s *scheduler) loop() {
	for {
		select {
		case job := <-s.jobs:
			job()
			// Pump goja's promise queue: resolve/reject called from Go (a
			// worker goroutine's completion) queues the async continuation,
			// which only runs when the VM next executes — a no-op drains it.
			// Without this, a script parked on `await agent(...)` never wakes
			// when the agent finishes.
			_, _ = s.vm.RunString("0")
		case <-s.done:
			return
		}
	}
}

func (s *scheduler) log(onLog func(string), msg string) {
	// Log lines live on the manager's snapshot (it owns the slice); the
	// runtime only forwards. Runs on the scheduler goroutine.
	if onLog != nil {
		onLog(msg)
	}
}

func (s *scheduler) setPhase(st *runState, title string, onPhase func(string)) {
	st.phase = title
	for _, p := range st.phases {
		if p == title {
			if onPhase != nil {
				onPhase(title)
			}
			return
		}
	}
	st.phases = append(st.phases, title)
	if onPhase != nil {
		onPhase(title)
	}
}

func joinArgs(args []any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = fmt.Sprint(a)
	}
	return strings.Join(parts, " ")
}

// awaitValue wires a JS value's then/fulfill+reject callbacks to Go funcs.
// The callbacks themselves are scheduled jobs, so delivery is serialized.
func awaitValue(vm *goja.Runtime, obj *goja.Object, onOK, onErr func(goja.Value)) {
	if fn, ok := goja.AssertFunction(obj.Get("then")); ok {
		_, _ = fn(obj, vm.ToValue(onOK), vm.ToValue(onErr))
	}
}

// exportErr unwraps a goja error value to a readable string.
func exportErr(v goja.Value) any {
	if e := v.Export(); e != nil {
		if err, ok := e.(error); ok {
			return err
		}
		return e
	}
	return v.String()
}

// agentFunc returns the script's global agent(). Port of runtime.ts agent().
//
// The resume key is assigned at lexical call time (before the limiter), so
// parallel()/pipeline() fan-out is reproducible; the slot reservation is
// atomic with the cap gate (sharedRuntime.mu) so a fan-out can't overshoot.
func (s *scheduler) agentFunc(ctx context.Context, st *runState, opts Options, shared *sharedRuntime, meta Meta, maxAgents int, cwd string) func(goja.FunctionCall) goja.Value {
	vm := s.vm
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.ToValue("agent(prompt, opts): prompt is required"))
		}
		prompt, ok := call.Argument(0).Export().(string)
		if !ok {
			panic(vm.ToValue("agent(prompt, opts): prompt must be a string"))
		}
		var ao AgentOptions
		if len(call.Arguments) > 1 {
			if err := vm.ExportTo(call.Argument(1), &ao); err != nil {
				panic(vm.ToValue("agent(prompt, opts): " + err.Error()))
			}
		}
		if ctx.Err() != nil {
			panic(vm.ToValue("workflow aborted"))
		}

		assignedPhase := ao.Phase
		if assignedPhase == "" {
			assignedPhase = st.phase
		}
		model := ao.Model
		if model == "" {
			model = phaseModel(meta, assignedPhase)
		}
		effort := ao.Effort
		if effort == "" {
			effort = phaseEffort(meta, assignedPhase)
		}

		callIndex := st.callSeq
		st.callSeq++
		callHash := callKey(prompt, model, effort, assignedPhase, ao.Schema)

		n, ok := shared.reserveAgent(maxAgents)
		if !ok {
			panic(vm.ToValue(fmt.Sprintf("agent limit exceeded (%d) — a runaway-loop backstop", maxAgents)))
		}
		label := strings.TrimSpace(ao.Label)
		if label == "" {
			if assignedPhase != "" {
				label = fmt.Sprintf("%s agent %d", assignedPhase, n)
			} else {
				label = fmt.Sprintf("agent %d", n)
			}
		}
		if len(label) > 80 {
			label = label[:80]
		}

		// Longest-unchanged-prefix resume: replay only while this call's index
		// is before the first changed/new call.
		cached, hit := st.resume[callIndex]
		if hit && cached.Hash == callHash && callIndex < st.firstMiss {
			if opts.Events.OnAgentStart != nil {
				opts.Events.OnAgentStart(callIndex, label, assignedPhase, model)
			}
			if opts.Events.OnAgentEnd != nil {
				opts.Events.OnAgentEnd(callIndex, label, assignedPhase, cached.Result, 0, "")
			}
			st.journal = append(st.journal, JournalEntry{Index: callIndex, Hash: callHash, Result: cached.Result})
			return vm.ToValue(cached.Result)
		}
		if !hit || cached.Hash != callHash {
			if callIndex < st.firstMiss {
				st.firstMiss = callIndex
			}
		}

		promise, resolve, reject := vm.NewPromise()
		req := AgentRequest{
			Prompt: prompt, Options: ao, Phase: assignedPhase,
			Model: model, Effort: effort, Index: callIndex, Workdir: cwd,
		}
		go s.runAgent(ctx, st, opts, shared, req, label, callHash, resolve, reject)
		return vm.ToValue(promise)
	}
}

// runAgent is the worker half of one agent() call: acquire the limiter, run
// the subagent, then hand resolution back to the scheduler. Failures resolve
// to null (the CC contract: scripts .filter(Boolean)); they are NOT
// journaled, so a resume retries them.
func (s *scheduler) runAgent(ctx context.Context, st *runState, opts Options, shared *sharedRuntime, req AgentRequest, label, callHash string, resolve, reject func(any) error) {
	select {
	case shared.state.sem <- struct{}{}:
	case <-ctx.Done():
		s.enqueue(func() { _ = reject("workflow aborted") })
		return
	}
	defer func() { <-shared.state.sem }()

	if opts.Events.OnAgentStart != nil {
		opts.Events.OnAgentStart(req.Index, label, req.Phase, req.Model)
	}

	runCtx := ctx
	if req.Options.TimeoutMs > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(req.Options.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	// A runner that panics must not kill the run goroutine — treat it as an
	// agent failure (null) so one bad subagent can't sink the workflow.
	// recover() only works from a deferred call, so the recovery lands on
	// named results via this closure.
	result, usage, err := func() (result any, usage Usage, err error) {
		defer func() {
			if r := recover(); r != nil {
				result, usage, err = nil, Usage{}, fmt.Errorf("agent runner panicked: %v", r)
			}
		}()
		return opts.Run(runCtx, req)
	}()

	s.enqueue(func() {
		st.tokens += usage.Total
		shared.state.spent.Add(int64(usage.Total))
		if err != nil {
			s.log(opts.Events.OnLog, fmt.Sprintf("agent %q failed: %v", label, err))
			if opts.Events.OnAgentEnd != nil {
				opts.Events.OnAgentEnd(req.Index, label, req.Phase, nil, usage.Total, err.Error())
			}
			_ = resolve(nil) // CC contract: a dead agent resolves to null
			return
		}
		entry := JournalEntry{Index: req.Index, Hash: callHash, Result: result}
		st.journal = append(st.journal, entry)
		if opts.Events.OnJournal != nil {
			opts.Events.OnJournal(entry)
		}
		if opts.Events.OnAgentEnd != nil {
			opts.Events.OnAgentEnd(req.Index, label, req.Phase, result, usage.Total, "")
		}
		_ = resolve(result)
	})
}

// callKey is the resume cache key: djb2 of JSON.stringify({prompt, model,
// effort, phase, schema}) — field order matches runtime.ts so journals are
// cross-compatible with the TS extension.
func callKey(prompt, model, effort, phase string, schema json.RawMessage) string {
	var b strings.Builder
	b.WriteString(`{"prompt":`)
	writeJSONString(&b, prompt)
	b.WriteString(`,"model":`)
	writeJSONString(&b, model)
	b.WriteString(`,"effort":`)
	writeJSONString(&b, effort)
	b.WriteString(`,"phase":`)
	writeJSONString(&b, phase)
	b.WriteString(`,"schema":`)
	if len(schema) == 0 {
		b.WriteString("null")
	} else {
		var buf bytes.Buffer
		if err := json.Compact(&buf, schema); err == nil {
			b.WriteString(buf.String())
		} else {
			b.Write(schema)
		}
	}
	b.WriteByte('}')
	return HashString(b.String())
}

// writeJSONString writes s as a JSON string literal, or null when empty —
// mirroring the TS `model: model ?? null` shape of the hash input.
func writeJSONString(b *strings.Builder, s string) {
	if s == "" {
		b.WriteString("null")
		return
	}
	out, _ := json.Marshal(s)
	b.Write(out)
}

// parallelFunc: Promise.all over thunks — a BARRIER. A throwing thunk
// resolves to null; the call never rejects (runtime.ts parallel).
func (s *scheduler) parallelFunc(onLog func(string)) func(goja.FunctionCall) goja.Value {
	vm := s.vm
	return func(call goja.FunctionCall) goja.Value {
		thunks, err := s.thunks(call.Argument(0), "parallel")
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		promise, resolve, _ := vm.NewPromise()
		results := make([]any, len(thunks))
		var wg sync.WaitGroup
		for i, thunk := range thunks {
			wg.Add(1)
			go func(i int, thunk func() (any, error)) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						s.enqueue(func() { s.log(onLog, fmt.Sprintf("parallel[%d] failed: %v", i, r)) })
						results[i] = nil
					}
				}()
				v, err := thunk()
				if err != nil {
					s.enqueue(func() { s.log(onLog, fmt.Sprintf("parallel[%d] failed: %v", i, err)) })
					v = nil
				}
				results[i] = v
			}(i, thunk)
		}
		go func() {
			wg.Wait()
			s.enqueue(func() { _ = resolve(results) })
		}()
		return vm.ToValue(promise)
	}
}

// pipelineFunc: each item flows through all stages independently — NO barrier
// (runtime.ts pipeline). A throwing stage drops the item to null and skips
// its remaining stages.
func (s *scheduler) pipelineFunc(onLog func(string)) func(goja.FunctionCall) goja.Value {
	vm := s.vm
	return func(call goja.FunctionCall) goja.Value {
		var items []any
		if err := vm.ExportTo(call.Argument(0), &items); err != nil {
			panic(vm.ToValue("pipeline() expects an array as the first argument"))
		}
		if len(items) > MaxFanoutItems {
			panic(vm.ToValue(fmt.Sprintf("pipeline() accepts at most %d items (got %d)", MaxFanoutItems, len(items))))
		}
		var stages []func(args ...any) (any, error)
		for _, arg := range call.Arguments[1:] {
			fn, ok := goja.AssertFunction(arg)
			if !ok {
				panic(vm.ToValue("pipeline() stages must be functions"))
			}
			stages = append(stages, s.stage(fn))
		}
		promise, resolve, _ := vm.NewPromise()
		results := make([]any, len(items))
		var wg sync.WaitGroup
		for i, item := range items {
			wg.Add(1)
			go func(i int, item any) {
				defer wg.Done()
				value := item
				for _, stage := range stages {
					v, err := stage(value, item, i)
					if err != nil {
						s.enqueue(func() { s.log(onLog, fmt.Sprintf("pipeline[%d] dropped at a stage: %v", i, err)) })
						results[i] = nil
						return
					}
					value = v
				}
				results[i] = value
			}(i, item)
		}
		go func() {
			wg.Wait()
			s.enqueue(func() { _ = resolve(results) })
		}()
		return vm.ToValue(promise)
	}
}

// thunks converts the parallel() argument into Go callables. Each call is a
// scheduler job; a thenable result is awaited through .then (also a job).
func (s *scheduler) thunks(v goja.Value, name string) ([]func() (any, error), error) {
	vm := s.vm
	obj := v.ToObject(vm)
	if obj == nil || obj.Get("length") == nil {
		return nil, fmt.Errorf("%s() expects an array of functions", name)
	}
	n := int(obj.Get("length").ToInteger())
	if n > MaxFanoutItems {
		return nil, fmt.Errorf("%s() accepts at most %d items (got %d)", name, MaxFanoutItems, n)
	}
	thunks := make([]func() (any, error), 0, n)
	for i := 0; i < n; i++ {
		fn, ok := goja.AssertFunction(obj.Get(fmt.Sprint(i)))
		if !ok {
			return nil, fmt.Errorf("%s() expects functions, not promises — wrap each call: () => agent(...)", name)
		}
		thunks = append(thunks, func() (any, error) {
			return s.callJS(fn)
		})
	}
	return thunks, nil
}

// stage wraps one pipeline stage function the same way, with (prev, original,
// index) arguments.
func (s *scheduler) stage(fn goja.Callable) func(args ...any) (any, error) {
	return func(args ...any) (any, error) {
		return s.callJS(fn, args...)
	}
}

// callJS invokes a JS function from a worker goroutine: the invocation is a
// scheduler job and the (possibly thenable) result is delivered on a
// channel. This is THE bridge between the goroutine fan-out and the
// single-goroutine VM.
func (s *scheduler) callJS(fn goja.Callable, args ...any) (any, error) {
	vm := s.vm
	type out struct {
		v   any
		err error
	}
	done := make(chan out, 1)
	s.enqueue(func() {
		vals := make([]goja.Value, len(args))
		for i, a := range args {
			vals[i] = vm.ToValue(a)
		}
		v, err := fn(goja.Undefined(), vals...)
		if err != nil {
			done <- out{err: err}
			return
		}
		if p, ok := v.Export().(*goja.Promise); ok {
			awaitValue(vm, vm.ToValue(p).ToObject(vm),
				func(r goja.Value) { done <- out{v: r.Export()} },
				func(e goja.Value) { done <- out{err: fmt.Errorf("%v", exportErr(e))} })
			return
		}
		done <- out{v: v.Export()}
	})
	// s.done covers the cancelled-run case: with the scheduler dead, dropped
	// resolve jobs would otherwise leave this worker parked forever.
	select {
	case o := <-done:
		return o.v, o.err
	case <-s.done:
		return nil, context.Canceled
	}
}

// nestedWorkflowFunc runs one child workflow as a sub-step, sharing the
// parent's limiter, counters, and ctx. One level of nesting only
// (runtime.ts workflow()).
func (s *scheduler) nestedWorkflowFunc(ctx context.Context, opts Options, shared *sharedRuntime) func(goja.FunctionCall) goja.Value {
	vm := s.vm
	return func(call goja.FunctionCall) goja.Value {
		if shared.depth >= maxNestDepth {
			panic(vm.ToValue("workflow() nesting is one level only"))
		}
		var scriptPath string
		switch ref := call.Argument(0).Export().(type) {
		case string:
			// ponytail: no saved-workflow registry is ported; a bare string is
			// treated as a path so the {scriptPath} round-trip works by name.
			scriptPath = ref
		case map[string]any:
			scriptPath, _ = ref["scriptPath"].(string)
		}
		if scriptPath == "" {
			panic(vm.ToValue("workflow(nameOrRef): pass { scriptPath } of a persisted workflow script"))
		}
		var childArgs any
		if len(call.Arguments) > 1 {
			childArgs = call.Argument(1).Export()
		}
		promise, resolve, reject := vm.NewPromise()
		go func() {
			data, err := os.ReadFile(filepath.Clean(scriptPath))
			if err != nil {
				s.enqueue(func() { _ = reject("could not read scriptPath: " + err.Error()) })
				return
			}
			child, err := Run(ctx, string(data), Options{
				RunID:  opts.RunID + ".nested",
				Cwd:    opts.Cwd,
				Args:   childArgs,
				Run:    opts.Run,
				Events: opts.Events,
				shared: shared.child(),
			})
			if err != nil {
				s.enqueue(func() { _ = reject(err.Error()) })
				return
			}
			s.enqueue(func() { _ = resolve(child.Value) })
		}()
		return vm.ToValue(promise)
	}
}

// phaseModel / phaseEffort resolve per-phase overrides from meta
// (runtime.ts phaseModel / phaseEffort).
func phaseModel(meta Meta, phase string) string {
	if phase != "" {
		for _, p := range meta.Phases {
			if p.Title == phase && p.Model != "" {
				return p.Model
			}
		}
	}
	return meta.Model
}

func phaseEffort(meta Meta, phase string) string {
	if phase != "" {
		for _, p := range meta.Phases {
			if p.Title == phase && p.Effort != "" {
				return p.Effort
			}
		}
	}
	return meta.Effort
}
