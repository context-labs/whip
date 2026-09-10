package rlm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryCheckpoints struct {
	mu    sync.Mutex
	value *Checkpoint
	fail  error
}

func (s *memoryCheckpoints) Load(context.Context) (*Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value == nil {
		return nil, nil
	}
	copy := *s.value
	copy.Data = append([]byte{}, copy.Data...)
	return &copy, nil
}
func (s *memoryCheckpoints) Save(_ context.Context, value Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	value.Data = append([]byte{}, value.Data...)
	s.value = &value
	return nil
}
func testQuickJS(t *testing.T, host Host, checkpoints CheckpointStore) *Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Engine: EngineQuickJS, Host: host, Checkpoints: checkpoints})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}
func TestQuickJSCellsAndFullImageRestore(t *testing.T) {
	store := &memoryCheckpoints{}
	kernel := testQuickJS(t, nil, store)
	first, err := kernel.Exec(t.Context(), `let count = 40; const next = () => ++count; const cycle = {}; cycle.self=cycle; class Box { constructor(n) {this.n=n} get(){return this.n} }; const box = new Box(7); print("ready"); next()`)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasValue || first.Value != json.Number("41") || first.Output != "ready\n" {
		t.Fatalf("first: %+v", first)
	}
	if first.Scratch != nil {
		t.Fatalf("checkpoint: %+v", first.Scratch)
	}
	if store.value == nil || len(store.value.Data) < 1<<20 {
		t.Fatalf("missing whole image")
	}
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	result, err := kernel.Exec(t.Context(), `[next(), cycle.self === cycle, box.get()]`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("restored %+v", result)
	values := result.Value.([]any)
	if values[0] != json.Number("42") || values[1] != true || values[2] != json.Number("7") || result.Restored == nil {
		t.Fatalf("restore: %+v", result)
	}
}

func TestQuickJSHostPromisesAndLosslessNumbers(t *testing.T) {
	var calls atomic.Int32
	host := HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
		calls.Add(1)
		if operation == "private_get" {
			return map[string]any{"big": json.Number("9007199254740993"), "decimal": json.Number("1.00000000000000000001"), "exponent": json.Number("1e3"), "zero": json.Number("-0")}, nil
		}
		value := args["value"].(map[string]any)
		for key, want := range map[string]string{"big": "9007199254740993", "decimal": "1.00000000000000000001", "exponent": "1e3", "zero": "-0", "negative": "-0.0"} {
			if value[key] != json.Number(want) {
				t.Errorf("%s = %#v, want %s", key, value[key], want)
			}
		}
		return value, nil
	})
	kernel := testQuickJS(t, host, nil)
	result, err := kernel.Exec(t.Context(), `const payload = await state.private_get({key:"large"}); payload.negative = -0; const saved = await state.private_set({key:"large",value:payload}); [typeof saved.big, saved.big === 9007199254740993n, json.encode(saved), Number(saved.exponent)]`)
	if err != nil {
		t.Fatal(err)
	}
	values := result.Value.([]any)
	if values[0] != "bigint" || values[1] != true || values[3] != json.Number("1000") || calls.Load() != 2 {
		t.Fatalf("result: %+v calls %d", result, calls.Load())
	}
	if !strings.Contains(values[2].(string), `"decimal":1.00000000000000000001`) {
		t.Fatalf("raw decimal lost: %s", values[2])
	}
}

func TestQuickJSPassiveHostArguments(t *testing.T) {
	var calls atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(context.Context, string, string, map[string]any) (any, error) { calls.Add(1); return nil, nil }), nil)
	cases := []string{
		`({value:9007199254740992})`,
		`({value:undefined})`,
		`({value:()=>1})`,
		`({get value(){throw new Error("accessor ran")}})`,
		`(()=>{const x={};x.self=x;return {value:x}})()`,
		`({value:Infinity})`,
	}
	for _, args := range cases {
		result, err := kernel.Exec(t.Context(), `var caught=""; try {await state.private_set(`+args+`)} catch(e) {caught=e.message}; caught`)
		if err != nil || !result.HasValue || result.Value == "" {
			t.Fatalf("%s: %+v %v", args, result, err)
		}
		if strings.Contains(result.Value.(string), "accessor ran") {
			t.Fatal("accessor ran during payload validation")
		}
	}
	result, err := kernel.Exec(t.Context(), `typeof Proxy`)
	if err != nil || result.Value != "undefined" || calls.Load() != 0 {
		t.Fatalf("passive admission: %+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestQuickJSHostQuotaRejectionCanBeCaught(t *testing.T) {
	var calls atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(context.Context, string, string, map[string]any) (any, error) {
		calls.Add(1)
		return "ok", nil
	}), nil)
	kernel.limits.HostRequests = 1
	result, err := kernel.Exec(t.Context(), `var quotaCode = ""; try { await files.read({path:"one"}); await files.read({path:"two"}); } catch (error) { quotaCode = error.code; } quotaCode`)
	if err != nil || result.Value != "E_LIMIT" || calls.Load() != 1 {
		t.Fatalf("quota rejection: result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
	result, err = kernel.Exec(t.Context(), `quotaCode`)
	if err != nil || result.Value != "E_LIMIT" {
		t.Fatalf("state after caught quota rejection: result=%+v err=%v", result, err)
	}
}

func TestQuickJSWaitsForRejectedPromiseSibling(t *testing.T) {
	siblingStarted := make(chan struct{})
	releaseSibling := make(chan struct{})
	host := HostFunc(func(ctx context.Context, _, _ string, args map[string]any) (any, error) {
		if args["path"] == "reject" {
			<-siblingStarted
			return nil, errors.New("expected denial")
		}
		close(siblingStarted)
		select {
		case <-releaseSibling:
			return "settled", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	store := &memoryCheckpoints{}
	kernel := testQuickJS(t, host, store)
	done := make(chan error, 1)
	go func() {
		_, err := kernel.Exec(t.Context(), `await Promise.all([files.read({path:"reject"}),files.read({path:"sibling"})])`)
		done <- err
	}()
	<-siblingStarted
	select {
	case err := <-done:
		t.Fatalf("returned with a live sibling: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseSibling)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "expected denial") {
		t.Fatalf("cell error=%v", err)
	}
	if store.value == nil {
		t.Fatal("settled ordinary error did not checkpoint")
	}
}

func TestQuickJSDrainsForgottenAwaitAndRejectsUnhandled(t *testing.T) {
	var calls atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(_ context.Context, _, _ string, args map[string]any) (any, error) {
		calls.Add(1)
		if args["path"] == "bad" {
			return nil, errors.New("denied")
		}
		return 7, nil
	}), nil)
	result, err := kernel.Exec(t.Context(), `var answer=0; files.read({path:"one"}).then(v => files.read({path:"two"})).then(v => answer=v); 42`)
	if err != nil || result.Value != json.Number("42") || calls.Load() != 2 {
		t.Fatalf("forgotten await: %+v err=%v calls=%d", result, err, calls.Load())
	}
	result, err = kernel.Exec(t.Context(), `answer`)
	if err != nil || result.Value != json.Number("7") {
		t.Fatalf("nested completion: %+v %v", result, err)
	}
	_, err = kernel.Exec(t.Context(), `files.read({path:"bad"}); 1`)
	if err == nil || !strings.Contains(err.Error(), "UNHANDLED_REJECTION") {
		t.Fatalf("forgotten rejection: %v", err)
	}
}

func TestQuickJSCancellationAndStalledCellRestoreCommittedImage(t *testing.T) {
	store := &memoryCheckpoints{}
	kernel := testQuickJS(t, nil, store)
	if _, err := kernel.Exec(t.Context(), `var durableCount=5`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	_, err := kernel.Exec(ctx, `durableCount=99; for(;;){}`)
	cancel()
	if err == nil {
		t.Fatal("infinite cell survived cancellation")
	}
	result, err := kernel.Exec(t.Context(), `durableCount`)
	if err != nil || result.Value != json.Number("5") || result.Restored == nil {
		t.Fatalf("cancel restore: %+v %v", result, err)
	}
	_, err = kernel.Exec(t.Context(), `durableCount=100; await new Promise(()=>{})`)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("stalled promise: %v", err)
	}
	result, err = kernel.Exec(t.Context(), `durableCount`)
	if err != nil || result.Value != json.Number("5") {
		t.Fatalf("stalled restore: %+v %v", result, err)
	}
}

func TestQuickJSLexicalErrorsAndValuePresence(t *testing.T) {
	kernel := testQuickJS(t, nil, nil)
	for _, test := range []struct {
		code  string
		has   bool
		value any
	}{{"let lexical=1", false, nil}, {"null", true, nil}, {"undefined", false, nil}, {"lexical", true, json.Number("1")}} {
		got, err := kernel.Exec(t.Context(), test.code)
		if err != nil || got.HasValue != test.has || got.Value != test.value {
			t.Fatalf("%s: %+v %v", test.code, got, err)
		}
	}
	if _, err := kernel.Exec(t.Context(), `let lexical=2`); err == nil {
		t.Fatal("lexical redeclaration succeeded")
	}
	if _, err := kernel.Exec(t.Context(), `const broken=(()=>{throw new Error("initializer")})()`); err == nil {
		t.Fatal("throwing initializer succeeded")
	}
	if _, err := kernel.Exec(t.Context(), `broken`); err == nil {
		t.Fatal("uninitialized lexical was readable")
	}
	result, err := kernel.Exec(t.Context(), `lexical=3; throw new Error("partial")`)
	if err == nil {
		t.Fatalf("throw succeeded: %+v", result)
	}
	result, err = kernel.Exec(t.Context(), `lexical`)
	if err != nil || result.Value != json.Number("3") {
		t.Fatalf("ordinary error lost partial mutation: %+v %v", result, err)
	}
}

func TestQuickJSCheckpointFailureKeepsPreviousImage(t *testing.T) {
	store := &memoryCheckpoints{}
	kernel := testQuickJS(t, nil, store)
	if _, err := kernel.Exec(t.Context(), `var saved=1`); err != nil {
		t.Fatal(err)
	}
	original := store.value.Envelope.SHA256
	store.fail = errors.New("storage quota")
	got, err := kernel.Exec(t.Context(), `saved=2`)
	if err != nil || got.Scratch == nil || !strings.Contains(got.Scratch.Warning, "storage quota") || store.value.Envelope.SHA256 != original {
		t.Fatalf("save failure: %+v %v", got, err)
	}
	store.fail = nil
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	got, err = kernel.Exec(t.Context(), `saved`)
	if err != nil || got.Value != json.Number("1") {
		t.Fatalf("previous image lost: %+v %v", got, err)
	}
	store.value.Data[len(store.value.Data)-1] ^= 1
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.Exec(t.Context(), `1`); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("corrupt image accepted: %v", err)
	}
}

func TestQuickJSPrototypeMutationCannotRunHostSerializationHooks(t *testing.T) {
	var calls atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(_ context.Context, _, _ string, args map[string]any) (any, error) {
		calls.Add(1)
		return map[string]any{"array": []any{json.Number("9007199254740993")}, "decimal": json.Number("1.00")}, nil
	}), nil)
	got, err := kernel.Exec(t.Context(), `String.prototype.slice=()=>{throw new Error("slice hook")}; String.prototype.includes=()=>{throw new Error("includes hook")}; Object.is=()=>{throw new Error("is hook")}; JSON.parse=()=>{throw new Error("parse hook")}; JSON.stringify=()=>{throw new Error("stringify hook")}; const decoded = await files.read({path:"x",negative:-0}); print(decoded.array[0]); json.encode(decoded)`)
	if err != nil || got.Output != "{\"type\":\"bigint\",\"value\":\"9007199254740993\"}\n" || calls.Load() != 1 {
		t.Fatalf("intrinsic capture: %+v %v", got, err)
	}
	if got.Value != `{"array":[9007199254740993],"decimal":1.00}` {
		t.Fatalf("lossless intrinsic result=%#v", got.Value)
	}
}

func TestQuickJSHostCallCancellationSettlesBeforeReturn(t *testing.T) {
	entered, settled := make(chan struct{}), make(chan struct{})
	kernel := testQuickJS(t, HostFunc(func(ctx context.Context, _, _ string, _ map[string]any) (any, error) {
		close(entered)
		<-ctx.Done()
		close(settled)
		return nil, ctx.Err()
	}), nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := kernel.Exec(ctx, `await user.ask({question:"q"})`); done <- err }()
	<-entered
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled host call succeeded")
	}
	select {
	case <-settled:
	default:
		t.Fatal("kernel returned before host accounting settled")
	}
}

func TestCheckpointPersistsChangedStarlarkOmissions(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryCheckpoints{}
	kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Checkpoints: store})
	if err != nil {
		t.Fatal(err)
	}
	defer kernel.Close()
	if _, err := kernel.Exec(t.Context(), `x=1`); err != nil {
		t.Fatal(err)
	}
	firstImage := append([]byte{}, store.value.Data...)
	got, err := kernel.Exec(t.Context(), `unsupported=[len]`)
	if err != nil || got.Scratch == nil || len(got.Scratch.Skipped) != 1 || got.Scratch.Skipped[0].Name != "unsupported" {
		t.Fatalf("omission notice: %+v %v", got, err)
	}
	if !bytes.Equal(firstImage, store.value.Data) {
		t.Fatal("fixture unexpectedly changed encoded globals")
	}
	if len(store.value.Envelope.Manifest.Skipped) != 1 {
		t.Fatal("changed omission manifest was not persisted")
	}
}

func TestQuickJSConfiguredConcurrencyQueuesRequests(t *testing.T) {
	for _, limit := range []int{1, 3} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			var active, peak atomic.Int32
			host := HostFunc(func(ctx context.Context, _, _ string, _ map[string]any) (any, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				select {
				case <-time.After(20 * time.Millisecond):
					return 7, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})
			kernel := testQuickJS(t, host, nil)
			kernel.limits.MaxConcurrentHostCalls = limit
			got, err := kernel.Exec(t.Context(), `await Promise.all(Array.from({length:8},()=>files.read({path:"fixture"})))`)
			if err != nil || len(got.Value.([]any)) != 8 || peak.Load() != int32(limit) {
				t.Fatalf("concurrency=%d result=%+v err=%v peak=%d", limit, got, err, peak.Load())
			}
		})
	}
}

func TestEngineDescriptorsAndJavaScriptGuide(t *testing.T) {
	descriptor, err := ResolveEngine(EngineQuickJS)
	if err != nil || descriptor.Language != "javascript" || len(descriptor.Build) != 64 || len(descriptor.GuideSHA256) != 64 || len(descriptor.BridgeSHA256) != 64 {
		t.Fatalf("descriptor=%+v %v", descriptor, err)
	}
	if _, err := ResolveEngine("node"); err == nil {
		t.Fatal("unbundled engine accepted")
	}
	guide, err := RuntimeGuide(EngineQuickJS, ModuleNames(), nil, "/workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"await files.read({path:", "BigInt", "Proxy construction is disabled", "complete JavaScript heap", "100,000", "40 MiB"} {
		if !strings.Contains(guide, want) {
			t.Fatalf("guide missing %q", want)
		}
	}
	for _, bad := range []string{"bounded Starlark runtime", "accept keyword arguments only", "context.history(seq=", "include_grants=True"} {
		if strings.Contains(guide, bad) {
			t.Fatalf("JavaScript guide retains %q", bad)
		}
	}
}

func TestQuickJSRejectsUnpairedUnicodeAndPreservesUTF8(t *testing.T) {
	var calls atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(_ context.Context, _, _ string, args map[string]any) (any, error) {
		calls.Add(1)
		return args["value"], nil
	}), nil)
	for _, source := range []string{`"\ud800"`, `"\udfff"`, `{"\ud800":1}`} {
		got, err := kernel.Exec(t.Context(), `var unicodeError="";try {await state.private_set({key:"x",value:`+source+`})}catch(e){unicodeError=e.code};unicodeError`)
		if err != nil || got.Value != "E_JSON" {
			t.Fatalf("invalid Unicode %s: %+v %v", source, got, err)
		}
	}
	got, err := kernel.Exec(t.Context(), `await state.private_set({key:"x",value:"hello 🌏 café"})`)
	if err != nil || got.Value != "hello 🌏 café" || calls.Load() != 1 {
		t.Fatalf("Unicode fidelity: %+v %v calls=%d", got, err, calls.Load())
	}
}
