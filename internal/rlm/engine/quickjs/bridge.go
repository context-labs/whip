// Package quickjs hosts the pinned QuickJS WASM without filesystem, network, or process authority.
package quickjs

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/rlm/engine"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const WasmSHA256 = "b006d95d9475edf7c6648cc3eb391d3b780efdd99022fbfbb470f2359da460ff"
const callbackName = "whip.quickjs.submit.v1"
const operationTimeout = 30 * time.Second

var (
	ErrBusy           = errors.New("bridge: runtime busy")
	ErrClosed         = errors.New("bridge: runtime closed")
	ErrQueuedRequests = errors.New("bridge: checkpoint requires settled owned requests and jobs")
	ErrJobBudget      = engine.ErrJobBudget
	toolPattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*$`)
)

//go:embed guest.js
var guestSource string

//go:embed quickjs.wasm
var bundledWASM []byte

type factory struct {
	mu       sync.Mutex
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	vms      sync.Map
	next     uint64
	closed   bool
	options  engine.Options
	identity string
	allowed  map[string]bool
}

type runtime struct {
	mu                          sync.Mutex
	factory                     *factory
	module                      api.Module
	session                     string
	control                     uint64
	initialized                 bool
	closed                      bool
	queue                       []engine.Request
	pending                     map[string]engine.Request
	cells                       map[string]string
	cellID                      string
	cellStatus                  string
	jobsPending                 bool
	prefix                      string
	ordinal                     uint64
	boxes, allocations, strings int
}

type failure struct{ err error }

func check(ok bool, message string) {
	if !ok {
		panic(failure{errors.New("bridge: " + message)})
	}
}
func must(err error) {
	if err != nil {
		panic(failure{err})
	}
}
func recoverError(err *error) {
	if p := recover(); p != nil {
		if f, ok := p.(failure); ok {
			*err = f.err
		} else {
			*err = fmt.Errorf("bridge internal failure: %v", p)
		}
	}
}
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, operationTimeout)
}

func normalize(options engine.Options) (engine.Options, error) {
	l := &options.Limits
	if l.MemoryBytes == 0 {
		l.MemoryBytes = 16 << 20
	}
	if l.MemoryPages == 0 {
		l.MemoryPages = 512
	}
	if l.MaxQueuedRequests == 0 {
		l.MaxQueuedRequests = 128
	}
	if l.MaxRequestBytes == 0 {
		l.MaxRequestBytes = 64 << 10
	}
	if l.MaxResultBytes == 0 {
		l.MaxResultBytes = 64 << 10
	}
	if l.MaxOutputBytes == 0 {
		l.MaxOutputBytes = 64 << 10
	}
	if l.MaxSnapshotBytes == 0 {
		l.MaxSnapshotBytes = 40 << 20
	}
	if l.MemoryPages > 4096 || l.MemoryPages < 32 || l.MemoryBytes < 256<<10 || l.MemoryBytes > uint64(l.MemoryPages)*65536 || l.MaxQueuedRequests < 1 || l.MaxQueuedRequests > 4096 || l.MaxRequestBytes < 1 || l.MaxRequestBytes > 4<<20 || l.MaxResultBytes < 1 || l.MaxResultBytes > 4<<20 || l.MaxOutputBytes < 128 || l.MaxOutputBytes > 4<<20 || l.MaxSnapshotBytes < 65536 || l.MaxSnapshotBytes > 512<<20 {
		return options, errors.New("bridge: invalid resource limits")
	}
	options.AllowedTools = append([]string(nil), options.AllowedTools...)
	sort.Strings(options.AllowedTools)
	for i, name := range options.AllowedTools {
		if len(name) > 128 || !toolPattern.MatchString(name) {
			return options, fmt.Errorf("bridge: invalid module-shaped tool %q", name)
		}
		for _, part := range strings.Split(name, ".") {
			if part == "prototype" || part == "constructor" || part == "__proto__" {
				return options, errors.New("bridge: reserved tool name")
			}
		}
		if i > 0 && options.AllowedTools[i-1] == name {
			return options, errors.New("bridge: duplicate tool")
		}
	}
	return options, nil
}

// NewFactory compiles only the pinned package WASM. No filesystem, network,
// provider, subprocess, or dynamic-module capability is installed in the guest.
func NewFactory(ctx context.Context, options engine.Options) (_ engine.Factory, err error) {
	options, err = normalize(options)
	if err != nil {
		return nil, err
	}
	wasm := bundledWASM
	if hash(wasm) != WasmSHA256 {
		return nil, errors.New("bridge: unrecognized WASM hash")
	}
	f := &factory{options: options, allowed: make(map[string]bool)}
	for _, name := range options.AllowedTools {
		f.allowed[name] = true
	}
	policy, _ := json.Marshal(struct {
		Limits engine.Limits
		Tools  []string
		Bridge string
	}{options.Limits, options.AllowedTools, BridgeSHA256()})
	f.identity = hash(policy)
	config := wazero.NewRuntimeConfigCompiler().WithMemoryLimitPages(options.Limits.MemoryPages).WithCloseOnContextDone(true)
	f.runtime = wazero.NewRuntimeWithConfig(ctx, config)
	defer func() {
		if err != nil {
			_ = f.runtime.Close(context.WithoutCancel(ctx))
		}
	}()
	f.compiled, err = f.runtime.CompileModule(ctx, wasm)
	if err != nil {
		return nil, err
	}
	builders := make(map[string]wazero.HostModuleBuilder)
	for _, d := range f.compiled.ImportedFunctions() {
		mod, name, _ := d.Import()
		if !supportedImport(mod, name) {
			return nil, fmt.Errorf("bridge: unsupported import %s.%s", mod, name)
		}
		b := builders[mod]
		if b == nil {
			b = f.runtime.NewHostModuleBuilder(mod)
			builders[mod] = b
		}
		b.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) { f.host(ctx, m, mod, name, stack) }), d.ParamTypes(), d.ResultTypes()).Export(name)
	}
	for _, b := range builders {
		if _, err = b.Instantiate(ctx); err != nil {
			return nil, err
		}
	}
	return f, nil
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func validID(id string) bool {
	return id != "" && len(id) <= 512 && utf8.ValidString(id) && !strings.ContainsRune(id, 0)
}
func scope(session, cell string) string {
	return "op:" + base64.RawURLEncoding.EncodeToString([]byte(session)) + ":" + base64.RawURLEncoding.EncodeToString([]byte(cell)) + ":"
}

func (f *factory) instantiate(ctx context.Context, session string) *runtime {
	check(!f.closed, "factory closed")
	f.next++
	m, err := f.runtime.InstantiateModule(ctx, f.compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("whip-quickjs-%d", f.next)).WithStartFunctions())
	must(err)
	v := &runtime{factory: f, module: m, session: session, pending: make(map[string]engine.Request), cells: make(map[string]string)}
	f.vms.Store(m.Name(), v)
	return v
}
func (f *factory) New(ctx context.Context, session string) (_ engine.Runtime, err error) {
	if !validID(session) {
		return nil, errors.New("bridge: invalid session ID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	defer recoverError(&err)
	ctx, cancel := bounded(ctx)
	defer cancel()
	v := f.instantiate(ctx, session)
	defer func() {
		if p := recover(); p != nil {
			_ = v.closeLocked(context.WithoutCancel(ctx))
			panic(p)
		}
	}()
	v.call(ctx, "_initialize")
	check(int32(v.call(ctx, "qjs_init")) == 0, "qjs_init failed")
	v.initialized = true
	v.configure(ctx)
	v.installHost(ctx)
	limits, _ := json.Marshal(f.options.Limits)
	tools, _ := json.Marshal(f.options.AllowedTools)
	if len(f.options.AllowedTools) == 0 {
		tools = []byte("[]")
	}
	code := strings.ReplaceAll(strings.ReplaceAll(guestSource, "__WHIP_LIMITS__", string(limits)), "__WHIP_TOOLS__", string(tools))
	v.control = v.eval(ctx, code, 0)
	v.refreshAdmission(ctx)
	return v, nil
}
func (v *runtime) configure(ctx context.Context) {
	v.call(ctx, "qjs_set_memory_limit", v.factory.options.Limits.MemoryBytes)
	v.call(ctx, "qjs_set_max_stack_size", 256<<10)
	v.call(ctx, "qjs_set_interrupt_handler", 1)
	v.call(ctx, "qjs_set_promise_rejection_handler", 1)
	v.call(ctx, "qjs_set_module_loader", 0)
}
func (v *runtime) enter(ctx context.Context) error {
	if !v.mu.TryLock() {
		return ErrBusy
	}
	if v.closed || v.module.IsClosed() {
		v.mu.Unlock()
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		v.mu.Unlock()
		return err
	}
	return nil
}
func (v *runtime) RunCell(ctx context.Context, cellID, source string) (err error) {
	if err = v.enter(ctx); err != nil {
		return err
	}
	defer v.mu.Unlock()
	defer recoverError(&err)
	ctx, cancel := bounded(ctx)
	defer cancel()
	if err = v.validateCellLocked(cellID, source); err != nil {
		return err
	}
	if len(v.pending) != 0 || v.call(ctx, "qjs_is_job_pending") != 0 {
		return ErrBusy
	}
	defer v.refreshAdmission(ctx)
	id := v.newString(ctx, cellID)
	defer v.freeValue(ctx, id)
	prefix := scope(v.session, cellID)
	p := v.newString(ctx, prefix)
	defer v.freeValue(ctx, p)
	result := v.controlCall(ctx, "begin", id, p)
	v.freeValue(ctx, result)
	v.cellID = cellID
	v.prefix = prefix
	v.ordinal = 0
	v.cells = map[string]string{cellID: hash([]byte(source))}
	// ASYNC global eval retains top-level lexical bindings; no async-IIFE rewrite.
	promise := v.evalRaw(ctx, source, 128)
	defer v.freeValue(ctx, promise)
	if v.call(ctx, "qjs_is_exception", promise) != 0 {
		e := v.valueCall(ctx, "qjs_get_exception")
		defer v.freeValue(ctx, e)
		r := v.controlCall(ctx, "rejected", e)
		v.freeValue(ctx, r)
		return errors.New("bridge: cell evaluation failed; inspect rejection")
	}
	fulfilled := v.property(ctx, v.control, "fulfilled")
	defer v.freeValue(ctx, fulfilled)
	rejected := v.property(ctx, v.control, "rejected")
	defer v.freeValue(ctx, rejected)
	watched := v.valueCall(ctx, "qjs_promise_then", promise, fulfilled, rejected)
	defer v.freeValue(ctx, watched)
	v.checkException(ctx, watched)
	return nil
}
func (v *runtime) Drain(ctx context.Context, maxJobs int) (n int, err error) {
	if maxJobs < 1 || maxJobs > 100000 {
		return 0, errors.New("bridge: maxJobs must be 1..100000")
	}
	if err = v.enter(ctx); err != nil {
		return 0, err
	}
	defer v.mu.Unlock()
	defer recoverError(&err)
	ctx, cancel := bounded(ctx)
	defer cancel()
	defer v.refreshAdmission(ctx)
	for n < maxJobs && v.call(ctx, "qjs_is_job_pending") != 0 {
		if int32(v.call(ctx, "qjs_execute_pending_job")) < 0 {
			e := v.valueCall(ctx, "qjs_get_exception")
			defer v.freeValue(ctx, e)
			return n, fmt.Errorf("bridge: pending job failed: %s", v.text(ctx, e, v.factory.options.Limits.MaxOutputBytes))
		}
		n++
	}
	if v.call(ctx, "qjs_is_job_pending") != 0 {
		return n, ErrJobBudget
	}
	return n, nil
}
func cloneRequest(r engine.Request) engine.Request {
	r.Args = append(json.RawMessage(nil), r.Args...)
	return r
}
func (v *runtime) TakeRequests() []engine.Request {
	v.mu.Lock()
	defer v.mu.Unlock()
	result := make([]engine.Request, len(v.queue))
	for i, r := range v.queue {
		result[i] = cloneRequest(r)
	}
	v.queue = nil
	return result
}
func (v *runtime) Deliver(ctx context.Context, out engine.Outcome) (accepted bool, err error) {
	if err = v.enter(ctx); err != nil {
		return false, err
	}
	defer v.mu.Unlock()
	defer recoverError(&err)
	if _, ok := v.pending[out.ID]; !ok {
		return false, nil
	}
	payload, err := v.encodeOutcome(out)
	if err != nil {
		return false, err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	defer v.refreshAdmission(ctx)
	text := v.newString(ctx, string(payload))
	defer v.freeValue(ctx, text)
	result := v.controlCall(ctx, "deliver", text)
	defer v.freeValue(ctx, result)
	accepted = v.call(ctx, "qjs_get_bool", result) != 0
	check(accepted, "guest/host pending state mismatch")
	delete(v.pending, out.ID)
	return true, nil
}
func (v *runtime) inspect(ctx context.Context) engine.View {
	result := v.controlCall(ctx, "inspect")
	defer v.freeValue(ctx, result)
	text := v.text(ctx, result, v.viewLimit())
	var view engine.View
	must(json.Unmarshal([]byte(text), &view))
	v.cellStatus = view.Status
	v.jobsPending = v.call(ctx, "qjs_is_job_pending") != 0
	return view
}

// Cache admission state only at owned boundaries; pure ValidateCell never enters WASM.
func (v *runtime) refreshAdmission(ctx context.Context) {
	if !v.closed && !v.module.IsClosed() {
		_ = v.inspect(ctx)
	}
}
func (v *runtime) viewLimit() int {
	return v.factory.options.Limits.MaxOutputBytes + v.factory.options.Limits.MaxQueuedRequests*1600 + 4096
}
func (v *runtime) Inspect(ctx context.Context) (view engine.View, err error) {
	if err = v.enter(ctx); err != nil {
		return view, err
	}
	defer v.mu.Unlock()
	defer recoverError(&err)
	ctx, cancel := bounded(ctx)
	defer cancel()
	view = v.inspect(ctx)
	return view, nil
}
func (v *runtime) closeLocked(ctx context.Context) (err error) {
	if v.closed {
		return nil
	}
	v.closed = true
	v.factory.vms.Delete(v.module.Name())
	defer func() { err = errors.Join(err, v.module.Close(context.WithoutCancel(ctx))) }()
	defer recoverError(&err)
	if !v.module.IsClosed() && v.initialized {
		if v.control != 0 {
			v.freeValue(ctx, v.control)
			v.control = 0
		}
		v.call(ctx, "qjs_destroy")
	}
	v.queue = nil
	v.pending = nil
	return nil
}
func (v *runtime) Close(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	ctx, cancel := bounded(ctx)
	defer cancel()
	return v.closeLocked(ctx)
}
func (f *factory) Close(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	var errs []error
	f.vms.Range(func(_, value interface{}) bool { errs = append(errs, value.(*runtime).Close(ctx)); return true })
	errs = append(errs, f.runtime.Close(ctx))
	return errors.Join(errs...)
}

// Finish closes cell authority only after all owned requests and jobs settle.
func (v *runtime) Finish(ctx context.Context) (err error) {
	if err = v.enter(ctx); err != nil {
		return err
	}
	defer v.mu.Unlock()
	defer recoverError(&err)
	ctx, cancel := bounded(ctx)
	defer cancel()
	if len(v.pending) != 0 || len(v.queue) != 0 || v.call(ctx, "qjs_is_job_pending") != 0 {
		return ErrBusy
	}
	result := v.controlCall(ctx, "finish")
	v.freeValue(ctx, result)
	v.refreshAdmission(ctx)
	return nil
}

// BridgeSHA256 pins the guest host codec and lifecycle controller in checkpoint profiles.
func BridgeSHA256() string { return hash([]byte(guestSource)) }
