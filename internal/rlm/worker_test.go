package rlm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"math"
	"math/big"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func newUnitWorker(input string) (*worker, *bytes.Buffer) {
	output := new(bytes.Buffer)
	return &worker{
		input: bufio.NewReader(strings.NewReader(input)), output: output,
		steps: 10_000, hostRequests: 4, outputBytes: 1 << 10, frameBytes: 1 << 20,
		globals: make(starlark.StringDict), modules: make(starlark.StringDict),
	}, output
}

func TestWorkerEvaluatesCellsAndHostCallsInProcess(t *testing.T) {
	var replies bytes.Buffer
	if err := writeFrame(&replies, 1<<20, frame{Type: "host_response", ID: 1, Value: map[string]any{"text": "hello", "count": float64(2)}}); err != nil {
		t.Fatal(err)
	}
	w, output := newUnitWorker(replies.String())
	w.installModules()
	result := w.evaluate("x = 40\nprint('ready')\nfiles.read(path='note.txt')")
	if result.Error != "" || result.Output != "ready\n" {
		t.Fatalf("evaluation = %+v", result)
	}
	value, ok := result.Value.(map[string]any)
	if !ok || value["text"] != "hello" || value["count"] != int64(2) {
		t.Fatalf("host value = %#v", result.Value)
	}
	request, err := readFrame(bufio.NewReader(output), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if request.Type != "host_request" || request.Module != "files" || request.Operation != "read" || request.Arguments["path"] != "note.txt" {
		t.Fatalf("host request = %+v", request)
	}
	if got := w.evaluate("x + 2"); got.Error != "" || got.Value != int64(42) {
		t.Fatalf("persisted globals = %+v", got)
	}
	if got := w.evaluate("if"); got.Error == "" {
		t.Fatal("parse error was accepted")
	}
	if got := w.evaluate("def f():\n  return f\nf()"); got.Error == "" || !strings.Contains(got.Error, "unsupported Starlark value") {
		t.Fatalf("unsupported result = %+v", got)
	}
}

func TestWorkerRunFramesAndProtocolFailures(t *testing.T) {
	var input bytes.Buffer
	if err := writeFrame(&input, 1<<20, frame{Type: "eval", ID: 7, Code: "6 * 7"}); err != nil {
		t.Fatal(err)
	}
	w, output := newUnitWorker(input.String())
	w.installModules()
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	result, err := readFrame(bufio.NewReader(output), 1<<20)
	if err != nil || result.ID != 7 || result.Value != float64(42) {
		t.Fatalf("result frame = %+v, %v", result, err)
	}

	bad, _ := newUnitWorker("{\"version\":1,\"type\":\"host_response\",\"id\":1}\n")
	if err := bad.run(); err == nil || !strings.Contains(err.Error(), "unexpected RLM frame") {
		t.Fatalf("unexpected frame error = %v", err)
	}
	truncated, _ := newUnitWorker("not-json\n")
	if err := truncated.run(); err == nil || !strings.Contains(err.Error(), "decode RLM frame") {
		t.Fatalf("decode error = %v", err)
	}
	writeFailure, _ := newUnitWorker(input.String())
	writeFailure.output = errorWriter{}
	writeFailure.installModules()
	if err := writeFailure.run(); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("write error = %v", err)
	}
}

func TestWorkerMainParsesLimitsAndRunsProtocol(t *testing.T) {
	oldMemoryLimit := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(oldMemoryLimit) })
	if err := applySoftMemoryLimit(math.MaxUint64); err == nil {
		t.Fatal("oversized soft memory limit succeeded")
	}
	if err := applySoftMemoryLimit(math.MaxInt64); err != nil {
		t.Fatalf("valid soft memory limit: %v", err)
	}
	if err := WorkerMain([]string{"-steps", "bad"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("public worker entrypoint accepted an invalid flag")
	}
	var input bytes.Buffer
	if err := writeFrame(&input, 1<<20, frame{Type: "eval", ID: 4, Code: "6 * 7"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var memory uint64
	args := []string{"-steps", "100", "-host-requests", "2", "-memory-bytes", "1048576", "-output-bytes", "1024", "-frame-bytes", "1048576"}
	if err := workerMain(args, &input, &output, func(value uint64) error { memory = value; return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := readFrame(bufio.NewReader(&output), 1<<20)
	if err != nil || result.ID != 4 || result.Value != float64(42) || memory != 1<<20 {
		t.Fatalf("worker result = %+v, memory=%d, %v", result, memory, err)
	}

	if err := workerMain([]string{"-steps", "bad"}, strings.NewReader(""), &bytes.Buffer{}, func(uint64) error { return nil }); err == nil {
		t.Fatal("invalid worker flag succeeded")
	}
	if err := workerMain([]string{"extra"}, strings.NewReader(""), &bytes.Buffer{}, func(uint64) error { return nil }); err == nil {
		t.Fatal("worker positional argument succeeded")
	}
	if err := workerMain(nil, strings.NewReader(""), &bytes.Buffer{}, func(uint64) error { return errors.New("limit failed") }); err == nil || !strings.Contains(err.Error(), "limit failed") {
		t.Fatalf("memory limit error = %v", err)
	}
}

func TestWorkerEvaluationAndNestedConversionEdges(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules()
	if got := w.evaluate(""); got.Error != "" || got.Value != nil {
		t.Fatalf("empty cell = %+v", got)
	}
	if got := w.evaluate("x = 1"); got.Error != "" || got.Value != nil {
		t.Fatalf("statement cell = %+v", got)
	}
	w.outputBytes = 4
	if got := w.evaluate("'abcdef'"); got.Error != "cell output limit exceeded" {
		t.Fatalf("oversized result = %+v", got)
	}

	badList := starlark.NewList([]starlark.Value{starlark.NewBuiltin("f", nil)})
	if _, err := starlarkToGo(badList); err == nil {
		t.Fatal("nested unsupported Starlark value converted")
	}
	if _, err := goToStarlark([]any{make(chan int)}); err == nil {
		t.Fatal("nested unsupported Go value converted")
	}
}

func TestWorkerHostCallValidatesResponsesAndLimits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		setup func(*worker)
		want  string
	}{
		{name: "unknown operation", setup: func(w *worker) {}, want: "unknown RLM operation"},
		{name: "request limit", setup: func(w *worker) { w.hostRequests = 0 }, want: "host request limit"},
		{name: "mismatch", input: `{"version":1,"type":"host_response","id":99}` + "\n", setup: func(w *worker) {}, want: "mismatched RLM host response"},
		{name: "host error", input: `{"version":1,"type":"host_response","id":1,"error":"denied"}` + "\n", setup: func(w *worker) {}, want: "denied"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w, _ := newUnitWorker(test.input)
			test.setup(w)
			operation := "read"
			if test.name == "unknown operation" {
				operation = "missing"
			}
			if _, err := w.hostCall("files", operation, nil); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("host call error = %v", err)
			}
		})
	}

	w, _ := newUnitWorker("")
	w.installModules()
	if got := w.evaluate("files.read('x')"); got.Error == "" || !strings.Contains(got.Error, "keyword arguments") {
		t.Fatalf("positional module call = %+v", got)
	}
}

func TestStarlarkValueConversionBoundaries(t *testing.T) {
	dict := starlark.NewDict(1)
	if err := dict.SetKey(starlark.MakeInt(1), starlark.String("value")); err != nil {
		t.Fatal(err)
	}
	for _, value := range []starlark.Value{
		starlark.None, starlark.True, starlark.String("text"), starlark.Bytes("bytes"),
		starlark.MakeInt64(9), starlark.MakeBigInt(newBigInt(t, "9223372036854775808")), starlark.Float(1.5),
		starlark.NewList([]starlark.Value{starlark.String("x")}),
		starlark.Tuple{starlark.MakeInt(2)},
	} {
		if _, err := starlarkToGo(value); err != nil {
			t.Fatalf("convert %s: %v", value.Type(), err)
		}
	}
	if _, err := starlarkToGo(dict); err == nil || !strings.Contains(err.Error(), "string keys") {
		t.Fatalf("non-string dictionary key error = %v", err)
	}
	if _, err := starlarkToGo(starlark.NewBuiltin("f", nil)); err == nil {
		t.Fatal("builtin converted unexpectedly")
	}
	for _, value := range []any{nil, true, "text", float64(2), float64(2.5), []any{"x"}, map[string]any{"n": float64(1)}, struct {
		Name string `json:"name"`
	}{Name: "value"}} {
		if _, err := goToStarlark(value); err != nil {
			t.Fatalf("convert %#v: %v", value, err)
		}
	}
	if _, err := goToStarlark(make(chan int)); err == nil {
		t.Fatal("unencodable Go value converted unexpectedly")
	}
}

func newBigInt(t *testing.T, value string) *big.Int {
	t.Helper()
	result, ok := new(big.Int).SetString(value, 10)
	if !ok {
		t.Fatal("invalid test integer")
	}
	return result
}

func TestRLMToolValidatesArgumentsAndRunsKernel(t *testing.T) {
	tool := Tool(nil)
	if _, err := tool.Run(context.Background(), []byte(`{"code":"1"}`)); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil kernel error = %v", err)
	}
	kernel := testKernel(t, DefaultLimits(), nil)
	tool = Tool(kernel)
	for _, input := range [][]byte{[]byte("{"), []byte(`{"code":""}`)} {
		if _, err := tool.Run(context.Background(), input); err == nil {
			t.Fatalf("invalid arguments %q were accepted", input)
		}
	}
	output, err := tool.Run(context.Background(), []byte(`{"code":"21 * 2"}`))
	if err != nil || !strings.Contains(output, `"value":42`) {
		t.Fatalf("tool output = %q, %v", output, err)
	}
	if _, err := tool.Run(context.Background(), []byte(`{"code":"fail"}`)); err == nil {
		t.Fatal("evaluation error was hidden")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWorkerSnapshotAndRestoreScratch(t *testing.T) {
	w, _ := newUnitWorker("")
	w.frameBytes = 8 << 20
	w.installModules()
	cells := []string{
		"n = 42\nbig = 1 << 100\nf = 1.5\ns = \"héllo\\n\"\nb = bytes(\"raw\")\nlst = [1, \"two\", [3.0, None], {\"k\": (1, 2)}]\nalias = lst\ntup = (1, 2, 3)\nd = {\"a\": 1, 7: \"int key\", (1, 2): \"tuple key\"}\ndef add(a, b=2):\n    \"\"\"adds\"\"\"\n    return a + b\ndef make_adder(k):\n    def inner(x):\n        return x + k\n    return inner\nplus3 = make_adder(3)\nsq = lambda x: x * x\n",
		"selfref = [1]\nselfref.append(selfref)\ndef uses_n():\n    return n\n",
	}
	for index, cell := range cells {
		if result := w.evaluate(cell); result.Error != "" {
			t.Fatalf("cell %d: %s", index, result.Error)
		}
	}
	program, manifest := w.buildSnapshot()
	for _, name := range []string{"n", "big", "f", "s", "b", "lst", "alias", "tup", "d", "add", "make_adder", "sq", "uses_n"} {
		if !slices.Contains(manifest.Saved, name) {
			t.Fatalf("%s missing from snapshot: %+v\n%s", name, manifest, program)
		}
	}
	skipped := map[string]string{}
	for _, item := range manifest.Skipped {
		skipped[item.Name] = item.Reason
	}
	if skipped["plus3"] != "closure" || skipped["selfref"] != "self-referential value" || len(skipped) != 2 {
		t.Fatalf("skipped = %+v", manifest.Skipped)
	}
	for module := range moduleRegistry {
		if strings.Contains(program, module+" = ") {
			t.Fatalf("host module %s entered the snapshot:\n%s", module, program)
		}
	}
	if again, _ := w.buildSnapshot(); again != program {
		t.Fatal("snapshot is not deterministic")
	}

	fresh, _ := newUnitWorker("")
	fresh.frameBytes = 8 << 20
	fresh.installModules()
	report := fresh.applySnapshot(program)
	if len(report.Failed) != 0 || len(report.Restored) != len(manifest.Saved) {
		t.Fatalf("restore report = %+v", report)
	}
	check := fresh.evaluate(`n == 42 and big == 1 << 100 and f == 1.5 and s == "héllo\n" and b == bytes("raw") and lst == [1, "two", [3.0, None], {"k": (1, 2)}] and tup == (1, 2, 3) and d[7] == "int key" and d[(1, 2)] == "tuple key" and add(1) == 3 and sq(4) == 16 and uses_n() == 42 and type(tup) == "tuple" and type(b) == "bytes"`)
	if check.Error != "" || check.Value != true {
		t.Fatalf("restored values differ: %+v", check)
	}
	if aliasCheck := fresh.evaluate("lst.append(9)\nalias[-1]"); aliasCheck.Error != "" || aliasCheck.Value != int64(9) {
		t.Fatalf("alias identity lost on restore: %+v", aliasCheck)
	}
	if hostCheck := fresh.evaluate("type(files.read)"); hostCheck.Error != "" || hostCheck.Value != "builtin_function_or_method" {
		t.Fatalf("host modules missing after restore: %+v", hostCheck)
	}
}

func TestWorkerRestoreIsolatesFailuresAndDeniesHostCalls(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules()
	report := w.applySnapshot("good = 1\nbad = undefined_name\ndef f(x=files.read(path=\"a\")):\n    return x\nalso = 2\n")
	if !slices.Equal(report.Restored, []string{"good", "also"}) {
		t.Fatalf("restored = %+v", report)
	}
	if len(report.Failed) != 2 || report.Failed[0].Name != "bad" || report.Failed[1].Name != "f" || !strings.Contains(report.Failed[1].Reason, "host calls are unavailable") {
		t.Fatalf("failed = %+v", report.Failed)
	}
	if check := w.evaluate("good + also"); check.Error != "" || check.Value != int64(3) {
		t.Fatalf("surviving bindings = %+v", check)
	}
	if check := w.evaluate("files.read(path='x')"); check.Error == "" || strings.Contains(check.Error, "unavailable while scratch") {
		t.Fatalf("host calls stayed disabled after restore: %+v", check)
	}
}

func TestWorkerSnapshotCapsAndNonFiniteFloats(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules()
	if result := w.evaluate("huge = 'x' * " + strconv.Itoa(snapshotVariableBytes+1) + "\nnan = float('nan')\nsmall = 1\n"); result.Error != "" {
		t.Fatal(result.Error)
	}
	program, manifest := w.buildSnapshot()
	skipped := map[string]string{}
	for _, item := range manifest.Skipped {
		skipped[item.Name] = item.Reason
	}
	if skipped["huge"] != "exceeds per-variable cap" || skipped["nan"] != "non-finite float" || !slices.Equal(manifest.Saved, []string{"small"}) {
		t.Fatalf("manifest = %+v", manifest)
	}
	if strings.Contains(program, "huge") || manifest.Bytes != len(program) {
		t.Fatalf("program = %q bytes=%d", program, manifest.Bytes)
	}
}

// Each REPL chunk owns its global slots, so a helper keeps the binding it was
// compiled against. A restore runs one program and unifies them.
func TestWorkerCrossChunkBindingQuirkUnifiesOnRestore(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules()
	for _, cell := range []string{"n = 42\ndef get_n():\n    return n\n", "n = 50\n"} {
		if result := w.evaluate(cell); result.Error != "" {
			t.Fatal(result.Error)
		}
	}
	if live := w.evaluate("get_n()"); live.Value != int64(42) {
		t.Fatalf("live binding = %+v (quirk changed; update the doctrine)", live)
	}
	program, _ := w.buildSnapshot()
	fresh, _ := newUnitWorker("")
	fresh.installModules()
	if report := fresh.applySnapshot(program); len(report.Failed) != 0 {
		t.Fatalf("restore = %+v", report)
	}
	if restored := fresh.evaluate("get_n()"); restored.Value != int64(50) {
		t.Fatalf("restored binding = %+v", restored)
	}
}
