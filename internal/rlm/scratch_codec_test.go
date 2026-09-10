package rlm

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func roundTripScratch(t *testing.T, w *worker) (*worker, SnapshotManifest) {
	t.Helper()
	program, manifest := w.buildSnapshot()
	fresh, _ := newUnitWorker("")
	fresh.installModules(nil)
	report := fresh.applySnapshot(program)
	if len(report.Failed) > 0 {
		t.Fatalf("restore failed: %+v\nsnapshot=%s", report, program)
	}
	return fresh, manifest
}

func TestScratchHelpersRetainStandardLibraryModules(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	result := w.evaluate(`data = json.decode('{"value": 16}')
timestamp = time.from_timestamp(0)
def summarize():
    return json.encode({"root": math.sqrt(data["value"]), "date": time.from_timestamp(0).format("2006-01-02")})
`)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	original := w.evaluate("summarize()")
	if original.Error != "" {
		t.Fatal(original.Error)
	}
	fresh, manifest := roundTripScratch(t, w)
	// Native time objects are outside the scratch data format. Helpers using
	// the module still survive without replaying the original cell.
	if len(manifest.Skipped) != 1 || manifest.Skipped[0].Name != "timestamp" {
		t.Fatalf("manifest=%+v", manifest)
	}
	if result := fresh.evaluate("summarize()"); result.Error != "" || result.Value != original.Value {
		t.Fatalf("restored helper=%+v", result)
	}
	if w.requests != 0 || fresh.requests != 0 {
		t.Fatal("standard library helpers made host requests")
	}
}

func TestScratchNestedAliasesAndUnsupportedContainers(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	result := w.evaluate("shared = [1]\na = {'s': shared}\nb = (shared, a)\ndef helper():\n    return 1\nbad = {'nested': [helper]}\nindependent = 7")
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	fresh, manifest := roundTripScratch(t, w)
	if len(manifest.Skipped) != 1 || manifest.Skipped[0].Name != "bad" {
		t.Fatalf("manifest=%+v", manifest)
	}
	if result := fresh.evaluate("a['s'].append(2)\nb[0][-1] == 2 and b[1]['s'][-1] == 2 and independent == 7"); result.Error != "" || result.Value != true {
		t.Fatalf("alias result=%+v", result)
	}
}

func TestScratchHelpersFailureRedefinitionAndDependencies(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	for _, cell := range []string{
		"def twice(x):\n    return x * 2\ndef recursive(n):\n    return 1 if n == 0 else n * recursive(n-1)\ndef composed(x):\n    return twice(x) + recursive(x)\ndef size(x):\n    return len(x)",
		"def replaced():\n    return 'old'",
	} {
		if result := w.evaluate(cell); result.Error != "" {
			t.Fatal(result.Error)
		}
	}
	if result := w.evaluate("def replaced():\n    return 'new'\nfail('after definition')"); result.Error == "" {
		t.Fatal("expected cell error")
	}
	fresh, manifest := roundTripScratch(t, w)
	if len(manifest.Skipped) > 0 {
		t.Fatalf("skipped=%+v", manifest.Skipped)
	}
	if result := fresh.evaluate("replaced() == 'new' and composed(4) == 32 and size([1]) == 1"); result.Error != "" || result.Value != true {
		t.Fatalf("helpers=%+v", result)
	}
	// Definitions in an errored cell must also be checkpointable again after restore.
	again, _ := roundTripScratch(t, fresh)
	if result := again.evaluate("replaced()"); result.Value != "new" {
		t.Fatalf("second restore=%+v", result)
	}
}

func TestScratchUnsupportedHelperDependencies(t *testing.T) {
	tests := []struct {
		name, initial, later string
		skipped              []string
	}{
		{"mutable default", "def helper(seen=[]):\n    seen.append(1)\n    return seen\ndef dependent():\n    return helper()", "helper()", []string{"helper", "dependent"}},
		{"shadowed literal default", "True = []\ndef helper(seen=True):\n    seen.append(1)\n    return seen\ndef dependent():\n    return helper()", "helper()", []string{"helper", "dependent"}},
		{"rebound literal default", "None = 1\ndef helper(value=None):\n    return value\nNone = 2\nif helper() != 1:\n    fail('default changed before snapshot')", "", []string{"helper"}},
		{"nested shadowed default", "False = []\ndef helper(value=(False,)):\n    return value", "", []string{"helper"}},
		{"evaluated default", "def helper(x=1+2):\n    return x", "", []string{"helper"}},
		{"rebound dependency", "n = 1\ndef helper():\n    return n\ndef dependent():\n    return helper()", "n = 2", []string{"helper", "dependent"}},
		{"type changed", "n = 1\ndef helper():\n    return type(n)", "n = 1.0", []string{"helper"}},
		{"float sign changed", "n = 0.0\ndef helper():\n    return n", "n = -0.0", []string{"helper"}},
		{"universal shadowed", "def helper(x):\n    return len(x)", "len = 2", []string{"helper"}},
		{"unsupported data", "bad = range(2)\ndef helper():\n    return bad", "", []string{"bad", "helper"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w, _ := newUnitWorker("")
			w.installModules(nil)
			for _, cell := range []string{test.initial, test.later} {
				if result := w.evaluate(cell); result.Error != "" {
					t.Fatal(result.Error)
				}
			}
			_, manifest := roundTripScratch(t, w)
			var skipped []string
			for _, entry := range manifest.Skipped {
				skipped = append(skipped, entry.Name)
			}
			slices.Sort(skipped)
			slices.Sort(test.skipped)
			if !slices.Equal(skipped, test.skipped) {
				t.Fatalf("manifest=%+v want=%v", manifest, test.skipped)
			}
		})
	}
}

func TestScratchExactScalarTypesAndBits(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	w.globals["raw"] = starlark.String(string([]byte{0xff, 0, 0xc0}))
	w.globals["bytes_value"] = starlark.Bytes(string([]byte{0xff, 0, 0xc0}))
	w.globals["negative_zero"] = starlark.Float(math.Copysign(0, -1))
	fresh, _ := roundTripScratch(t, w)
	for _, name := range []string{"raw", "bytes_value", "negative_zero"} {
		if !sameScratchBinding(w.globals[name], fresh.globals[name]) {
			t.Fatalf("%s changed: %v -> %v", name, w.globals[name], fresh.globals[name])
		}
	}
}

func TestScratchTraversalBoundsAndCycles(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	var deep starlark.Value = starlark.None
	for range 140 {
		deep = starlark.Tuple{deep}
	}
	w.globals["deep"] = deep
	cycle := starlark.NewList(nil)
	if err := cycle.Append(starlark.Tuple{cycle}); err != nil {
		t.Fatal(err)
	}
	w.globals["cycle"] = cycle
	w.globals["good"] = starlark.MakeInt(3)
	_, manifest := roundTripScratch(t, w)
	if !slices.Equal(manifest.Saved, []string{"good"}) || len(manifest.Skipped) != 2 {
		t.Fatalf("manifest=%+v", manifest)
	}
}

func TestScratchCorruptionAndHostEffectDefaults(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	w.globals["existing"] = starlark.MakeInt(9)
	for _, snapshot := range []scratchSnapshot{
		{Version: 1, Bindings: []scratchBinding{{Name: "cycle", Value: 0}}, Nodes: []scratchNode{{Kind: "list", Items: []int{0}}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "files", Value: 0}}, Nodes: []scratchNode{{Kind: "int", Text: "1"}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 99}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: -1}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 0}}, Nodes: []scratchNode{{Kind: "int", Text: "one"}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 0}}, Nodes: []scratchNode{{Kind: "float", Text: "NaN"}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 0}}, Nodes: []scratchNode{{Kind: "string", Text: "!"}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 0}}, Nodes: []scratchNode{{Kind: "bytes", Text: "!"}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 0}}, Nodes: []scratchNode{{Kind: "unknown"}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "duplicate", Value: 0}, {Name: "duplicate", Value: 0}}, Nodes: []scratchNode{{Kind: "none"}}},
		{Version: 1, Nodes: []scratchNode{{Kind: "none"}}},
		{Version: 1, Nodes: make([]scratchNode, 65537)},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 1}}, Nodes: []scratchNode{{Kind: "none"}, {Kind: "int", Text: "1", Items: []int{0}}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 1}}, Nodes: []scratchNode{{Kind: "none"}, {Kind: "dict", Items: []int{0}}}},
		{Version: 1, Bindings: []scratchBinding{{Name: "invalid", Value: 2}}, Nodes: []scratchNode{{Kind: "string", Text: "a2V5"}, {Kind: "none"}, {Kind: "dict", Items: []int{0, 1, 0, 1}}}},
		{Version: 2},
	} {
		encoded, _ := json.Marshal(snapshot)
		if result := w.restore(string(encoded)); result.Error == "" {
			t.Fatalf("accepted corrupt snapshot: %s", encoded)
		}
		if w.globals["existing"] != starlark.MakeInt(9) {
			t.Fatal("corrupt restore erased current environment")
		}
	}
	snapshot := scratchSnapshot{Version: 1, Helpers: []scratchHelper{{Name: "danger", Source: "def danger(x=files.read(path='x')):\n    return x\n"}, {Name: "dependent", Source: "def dependent():\n    return danger()\n"}, {Name: "okay", Source: "def okay():\n    return files.read(path='x')\n"}}}
	encoded, _ := json.Marshal(snapshot)
	report := w.applySnapshot(string(encoded))
	if len(report.Failed) != 2 || !slices.Equal(report.Restored, []string{"okay"}) {
		t.Fatalf("report=%+v", report)
	}
	if w.requests != 0 {
		t.Fatal("restore issued host request")
	}
}

func TestScratchSameLineFailedRedefinition(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	if result := w.evaluate("f = lambda: 1; fail('stop'); f = lambda: 2"); result.Error == "" {
		t.Fatal("expected failure")
	}
	fresh, manifest := roundTripScratch(t, w)
	if slices.Contains(manifest.Saved, "f") {
		if result := fresh.evaluate("f()"); result.Value != int64(1) {
			t.Fatalf("wrong source restored: %+v", result)
		}
	} else if len(manifest.Skipped) != 1 || !strings.Contains(manifest.Skipped[0].Reason, "source") {
		t.Fatalf("unexpected omission: %+v", manifest)
	}
}

func TestScratchMutualRecursionLambdasAndLiteralDefaults(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	code := "def even(n):\n    return True if n == 0 else odd(n-1)\ndef odd(n):\n    return False if n == 0 else even(n-1)\ndef defaults(x=(None, True, -2, 1.5, b'raw')):\n    return x\nfactor = 3\ntimes = lambda x: x * factor\ndef sizes(n):\n    return len(range(n))"
	if result := w.evaluate(code); result.Error != "" {
		t.Fatal(result.Error)
	}
	fresh, manifest := roundTripScratch(t, w)
	if len(manifest.Skipped) > 0 {
		t.Fatalf("skipped=%+v", manifest.Skipped)
	}
	if result := fresh.evaluate("even(4) and odd(3) and defaults() == (None, True, -2, 1.5, b'raw') and times(2) == 6 and sizes(5) == 5"); result.Error != "" || result.Value != true {
		t.Fatalf("result=%+v", result)
	}
}

func TestScratchUninitializedGlobalDoesNotFallBack(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	if result := w.evaluate("existing = 1"); result.Error != "" {
		t.Fatal(result.Error)
	}
	if result := w.evaluate("def helper():\n    return n\nfail('stop')\nn = 2"); result.Error == "" {
		t.Fatal("expected failure")
	}
	if result := w.evaluate("helper()"); result.Error == "" {
		t.Fatal("expected uninitialized binding")
	}
	_, manifest := roundTripScratch(t, w)
	if len(manifest.Skipped) != 1 || manifest.Skipped[0].Name != "helper" {
		t.Fatalf("manifest=%+v", manifest)
	}
}

func TestScratchSkippedShadowDoesNotFallBackToBuiltinOrModule(t *testing.T) {
	for _, name := range []string{"len", "files"} {
		t.Run(name, func(t *testing.T) {
			w, _ := newUnitWorker("")
			w.installModules(nil)
			if result := w.evaluate(name + " = [lambda: 1]\ndef helper():\n    return " + name + "\ndef dependent():\n    return helper()"); result.Error != "" {
				t.Fatal(result.Error)
			}
			fresh, manifest := roundTripScratch(t, w)
			skipped := map[string]bool{}
			for _, entry := range manifest.Skipped {
				skipped[entry.Name] = true
			}
			if !skipped["helper"] || !skipped["dependent"] {
				t.Fatalf("shadowed helper survived: %+v", manifest)
			}
			if _, exists := fresh.globals["helper"]; exists {
				t.Fatal("restored helper with changed dependency")
			}
		})
	}
}

func TestScratchRestoreRejectsShadowedLiteralDefaults(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	snapshot := scratchSnapshot{
		Version:  1,
		Bindings: []scratchBinding{{Name: "True", Value: 0}, {Name: "independent", Value: 1}},
		Nodes:    []scratchNode{{Kind: "list"}, {Kind: "int", Text: "42"}},
		Helpers: []scratchHelper{
			{Name: "helper", Source: "def helper(value=(True,)):\n    return value\n"},
			{Name: "dependent", Source: "def dependent():\n    return helper()\n"},
			{Name: "okay", Source: "def okay(value=False):\n    return value\n"},
		},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	report := w.applySnapshot(string(encoded))
	if len(report.Failed) != 2 || !slices.Equal(report.Restored, []string{"True", "independent", "okay"}) {
		t.Fatalf("report=%+v", report)
	}
	if result := w.evaluate("independent == 42 and okay() == False"); result.Error != "" || result.Value != true {
		t.Fatalf("independent bindings lost: %+v", result)
	}
}

func TestScratchUnchangedHelpersProduceStableCheckpoint(t *testing.T) {
	w, _ := newUnitWorker("")
	w.installModules(nil)
	if result := w.evaluate("bad = range(1)\ndef first():\n    return bad\ndef second():\n    return bad\ndef dependent():\n    return first() + second()"); result.Error != "" {
		t.Fatal(result.Error)
	}
	snapshot, manifest := w.buildSnapshot()
	initial := scratchHash(snapshot, manifest)
	for range 100 {
		snapshot, manifest := w.buildSnapshot()
		if scratchHash(snapshot, manifest) != initial {
			t.Fatalf("unchanged helper omissions changed checkpoint: %+v", manifest)
		}
	}
}

func FuzzScratchDataRoundTrip(f *testing.F) {
	f.Add([]byte("hello\x00世界"), uint64(0))
	f.Add([]byte{0xff, 0xc0, 0, '\n'}, math.Float64bits(math.Copysign(0, -1)))
	f.Add([]byte("\"\\\n"), math.Float64bits(math.MaxFloat64))
	f.Fuzz(func(t *testing.T, raw []byte, bits uint64) {
		number := math.Float64frombits(bits)
		if len(raw) > 1024 || math.IsNaN(number) || math.IsInf(number, 0) {
			t.Skip()
		}
		w, _ := newUnitWorker("")
		w.installModules(nil)
		shared := starlark.NewList([]starlark.Value{starlark.String(raw), starlark.Bytes(raw), starlark.MakeUint64(bits), starlark.Float(number)})
		w.globals["shared"] = shared
		w.globals["nested"] = starlark.Tuple{shared, starlark.NewList([]starlark.Value{shared})}
		for range 3 {
			fresh, manifest := roundTripScratch(t, w)
			if len(manifest.Skipped) != 0 {
				t.Fatalf("unexpected omission: %+v", manifest)
			}
			actual := fresh.globals["shared"].(*starlark.List)
			for i := range shared.Len() {
				if !sameScratchBinding(shared.Index(i), actual.Index(i)) {
					t.Fatalf("scalar %d changed during restart", i)
				}
			}
			nested := fresh.globals["nested"].(starlark.Tuple)
			if nested[0] != actual || nested[1].(*starlark.List).Index(0) != actual {
				t.Fatal("restart lost shared list identity")
			}
			w = fresh
		}
	})
}
