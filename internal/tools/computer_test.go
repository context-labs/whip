package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/computer"
)

// computer_exec refuses cleanly when no policy is installed (never drives
// an app ungated), and refuses unknown helpers with guidance.
func TestComputerExecGates(t *testing.T) {
	// On Linux the platform check fires first.
	out := Execute(t.Context(), []Tool{ComputerExec(NewServices())}, "computer_exec", []byte(`{"code":"print(chrome_state())"}`))
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("want error, got %q", out[:80])
	}
}

func TestComputerExecDirect(t *testing.T) {
	tool := computerExec(NewServices())
	if !computer.Available() {
		if _, err := tool.Run(t.Context(), []byte(`{"code":"print(1)"}`)); err == nil || !strings.Contains(err.Error(), "macOS-only") {
			t.Fatalf("direct computerExec error = %v", err)
		}
		return
	}
	for _, args := range []string{`{bad`, `{}`, `{"code":"   "}`} {
		if _, err := tool.Run(t.Context(), []byte(args)); err == nil {
			t.Fatalf("computerExec accepted %q", args)
		}
	}
	if out, err := tool.Run(t.Context(), []byte(`{"code":"print(1)"}`)); err != nil || out != "1\n" {
		t.Fatalf("computerExec print = %q, %v", out, err)
	}
}

// The policy gate blocks denied apps and surfaces ApprovalNeeded for
// unlisted ones; an approver granting consent unblocks.
func TestGateApp(t *testing.T) {
	ctx, services := withComputerPolicy(t, computer.NewPolicy([]string{"Google Chrome"}, []string{"Finder"}, true))

	if err := gateApp(ctx, "Google Chrome"); err != nil {
		t.Errorf("allowed app blocked: %v", err)
	}
	if err := gateApp(ctx, "Finder"); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("denied app must fail: %v", err)
	}
	err := gateApp(ctx, "Safari")
	if err == nil {
		t.Fatal("unlisted app must need approval")
	}
	services.SetComputerApprover(func(app string) bool { return app == "Safari" })
	if err := gateApp(ctx, "Safari"); err != nil {
		t.Errorf("approver-consent must unblock: %v", err)
	}
	// persisted for the session
	services.SetComputerApprover(nil)
	if err := gateApp(ctx, "Safari"); err != nil {
		t.Errorf("approval must persist for the session: %v", err)
	}
}

func TestIsStale(t *testing.T) {
	if !IsStale(&computer.StaleError{Msg: "state changed"}) {
		t.Error("StaleError must be stale")
	}
	if IsStale(errors.New("other")) || IsStale(nil) {
		t.Error("non-stale errors must not be stale")
	}
}

func TestShorten(t *testing.T) {
	if got := shorten("short", 10); got != "short" {
		t.Errorf("shorten under limit: %q", got)
	}
	if got := shorten("abcdefghij", 5); got != "abcd…" {
		t.Errorf("shorten over limit: %q", got)
	}
}

func TestGenerations(t *testing.T) {
	ctx := WithServices(t.Context(), NewServices())
	noteGeneration(ctx, "GenApp", &computer.AppState{Generation: 7})
	if got := genFor(ctx, "genapp"); got != 7 { // case-insensitive
		t.Errorf("genFor: %d", got)
	}
	noteGeneration(ctx, "GenApp", nil) // nil state is a no-op
	if got := genFor(ctx, "GenApp"); got != 7 {
		t.Errorf("genFor after nil note: %d", got)
	}
	if got := genFor(ctx, "NeverSeen"); got != 0 {
		t.Errorf("unknown app gen: %d", got)
	}
}

func TestSummarize(t *testing.T) {
	st := &computer.AppState{
		Generation: 3,
		App:        "TextEdit",
		Elements: []computer.AXElement{{
			Index: 0, Role: "AXButton", Title: "OK", Value: "v", Desc: "d",
			Position: []float64{10, 20}, Size: []float64{30, 40}, Focused: true,
		}},
		Screenshot: &computer.Screenshot{Bytes: 123},
	}
	out := summarize(st)
	for _, want := range []string{
		"app=TextEdit generation=3 elements=1",
		"screenshot: 123 bytes jpeg attached",
		`[0] AXButton title="OK" value="v" desc="d" at(10,20 30x40) focused`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summarize missing %q in:\n%s", want, out)
		}
	}
	st.Screenshot = &computer.Screenshot{Err: "no grant"}
	if out := summarize(st); !strings.Contains(out, "screenshot: unavailable (no grant)") {
		t.Errorf("summarize screenshot error: %s", out)
	}
}

func withComputerPolicy(t *testing.T, policy *computer.Policy) (context.Context, *Services) {
	t.Helper()
	services := NewServices()
	services.SetComputerPolicy(policy)
	return WithServices(t.Context(), services), services
}

// runComputerCode error and gate paths that are portable (no osascript, no
// helper needed — they fail before touching the platform).
func TestRunComputerCodePortablePaths(t *testing.T) {
	ctx, _ := withComputerPolicy(t, computer.NewPolicy([]string{"Google Chrome"}, []string{"Finder"}, true))

	if out, err := runComputerCode(ctx, "# label\nprint(\"hi\")"); err != nil || out != "hi\n" {
		t.Errorf("print: %q %v", out, err)
	}
	if out, err := runComputerCode(ctx, `print(42)`); err != nil || out != "42\n" {
		t.Errorf("print number: %q %v", out, err)
	}
	if _, err := runComputerCode(ctx, `frobnicate("x")`); err == nil || !strings.Contains(err.Error(), "unknown helper") {
		t.Errorf("unknown helper: %v", err)
	}
	if _, err := runComputerCode(ctx, `goto("x" `); err == nil {
		t.Error("parse error must surface")
	}
	// Policy: denied app blocks tell() before any AppleScript runs.
	if _, err := runComputerCode(ctx, `tell("Finder", "activate")`); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("denied app: %v", err)
	}
	// SSRF floor: chrome_goto to a metadata endpoint is blocked pre-navigation.
	if _, err := runComputerCode(ctx, `chrome_goto("http://169.254.169.254/latest")`); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("ssrf block: %v", err)
	}
	// Arg validation fires before gates or platform calls.
	if _, err := runComputerCode(ctx, `chrome_js()`); err == nil || !strings.Contains(err.Error(), "missing arg") {
		t.Errorf("missing arg: %v", err)
	}
	if _, err := runComputerCode(ctx, `click("Google Chrome")`); err == nil || !strings.Contains(err.Error(), "click needs") {
		t.Errorf("click arity: %v", err)
	}
	if _, err := runComputerCode(ctx, `chrome_activate("w", 1)`); err == nil || !strings.Contains(err.Error(), "must be a number") {
		t.Errorf("argNum type check: %v", err)
	}
}

// The osascript tier propagates the platform error on non-macOS.
func TestRunComputerCodeOsascriptTierUnsupported(t *testing.T) {
	if computer.Available() {
		t.Skip("darwin: would drive the real Chrome")
	}
	ctx, _ := withComputerPolicy(t, computer.NewPolicy([]string{"Google Chrome"}, nil, true))
	for _, code := range []string{
		`chrome_state()`, `chrome_tabs()`, `chrome_back()`, `chrome_reload()`,
		`chrome_js("1+1")`, `chrome_find("example")`,
		`chrome_activate(1, 2)`, `chrome_close(1, 2)`,
		`chrome_new_tab("http://93.184.216.34/")`, `chrome_goto("http://93.184.216.34/")`,
		`tell("Google Chrome", "activate")`,
	} {
		if _, err := runComputerCode(ctx, code); err == nil || !errors.Is(err, computer.ErrUnsupportedPlatform) {
			t.Errorf("%s: want ErrUnsupportedPlatform, got %v", code, err)
		}
	}
}

func TestRunComputerCodeWithFakeOSAScript(t *testing.T) {
	if !computer.Available() {
		t.Skip("darwin-only AppleScript entry points")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "osascript")
	script := `#!/bin/sh
case "$2" in
  *"set theUrl to URL of active tab"*) printf 'https://active.example\nActive title\n' ;;
  *"set wCount to count windows"*) printf '1￨2￨https://two.example￨Two\n' ;;
  *"execute javascript"*) printf 'js-result\n' ;;
  *) printf 'ok\n' ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, _ := withComputerPolicy(t, computer.NewPolicy([]string{"Finder", "Google Chrome"}, nil, true))

	out, err := runComputerCode(ctx, `tell("Finder", "activate")
chrome_state()
chrome_tabs()
chrome_goto("https://93.184.216.34/")
chrome_new_tab("https://93.184.216.34/")
chrome_activate(1, 2)
chrome_close(1, 2)
chrome_back()
chrome_reload()
chrome_js("1+1")
chrome_find("two.example")
chrome_find("missing")`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ok", "active.example", "two.example", "js-result", "null"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Without a helper binary, native helpers fail with enable-the-driver guidance.
func TestHelperUnavailable(t *testing.T) {
	if computer.Available() {
		t.Skip("darwin: an embedded helper may exist")
	}
	t.Setenv("WHIP_COMPUTER_BIN", "")
	computer.ResetShared()
	t.Cleanup(computer.ResetShared)
	if _, err := helper(context.Background()); err == nil || !strings.Contains(err.Error(), "whip-computer driver") {
		t.Errorf("helper: %v", err)
	}
	ctx, _ := withComputerPolicy(t, computer.NewPolicy([]string{"TestApp"}, nil, true))
	if _, err := runComputerCode(ctx, `apps()`); err == nil || !strings.Contains(err.Error(), "whip-computer driver") {
		t.Errorf("apps without helper: %v", err)
	}
}

// fakeNativeHelper builds a stub whip-computer binary (Go, stdlib-only) that
// speaks the stdio JSON-RPC protocol with canned responses, and points
// WHIP_COMPUTER_BIN at it — the same seam internal/computer's tests use.
func fakeNativeHelper(t *testing.T) {
	t.Helper()
	const src = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func reply(enc *json.Encoder, id, result any) {
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func main() {
	fmt.Println("whip-computer/1")
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	state := map[string]any{
		"generation": 2, "app": "TestApp",
		"elements": []map[string]any{{"index": 0, "role": "AXButton", "title": "OK", "enabled": true}},
		"screenshot": map[string]any{"jpegBase64": "aGVsbG8=", "bytes": 5},
	}
	for sc.Scan() {
		var req map[string]any
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		id := req["id"]
		params, _ := req["params"].(map[string]any)
		switch req["method"] {
		case "handshake":
			reply(enc, id, map[string]any{"version": "whip-computer/1"})
		case "apps":
			reply(enc, id, []map[string]any{{"name": "Finder", "bundleId": "com.apple.finder", "pid": 1, "active": true}})
		case "permissions.request":
			reply(enc, id, map[string]any{"accessibility": true, "screenRecording": true})
		case "state", "ax":
			reply(enc, id, state)
		case "click":
			// The tool must pass back the generation from the last state read.
			if g, _ := params["gen"].(float64); g != 2 {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": 4, "message": "state changed since generation"}})
				continue
			}
			next := map[string]any{}
			for k, v := range state {
				next[k] = v
			}
			next["generation"] = 3
			reply(enc, id, next)
		case "type":
			reply(enc, id, map[string]any{"action": "typed", "stateUnavailable": "no AX grant", "hint": "grant it"})
		case "press", "scroll", "set", "select", "menu":
			// press("...", "BADJSON") answers with a non-AppState result so the
			// tool's fold-in unmarshal fails.
			if k, _ := params["key"].(string); k == "BADJSON" {
				reply(enc, id, "not a state object")
				continue
			}
			// Echo every param back as an AX row so the test can assert what
			// the tool actually sent over the wire.
			var rows []map[string]any
			i := 0
			for k, v := range params {
				rows = append(rows, map[string]any{"index": i, "role": "AXParam", "title": k, "value": fmt.Sprint(v)})
				i++
			}
			reply(enc, id, map[string]any{"generation": 2, "app": "TestApp", "elements": rows})
		case "screenshot":
			reply(enc, id, map[string]any{"jpegBase64": "aGVsbG8=", "bytes": 5})
		default:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "unknown method"}})
		}
	}
}
`
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-computer")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fakecomputer\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goBin := "go"
	if out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output(); err == nil {
		if cand := filepath.Join(strings.TrimSpace(string(out)), "bin", "go"); fileExists(cand) {
			goBin = cand
		}
	}
	cmd := exec.CommandContext(t.Context(), goBin, "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake helper: %v\n%s", err, out)
	}
	t.Setenv("WHIP_COMPUTER_BIN", bin)
	computer.ResetShared()
	t.Cleanup(computer.ResetShared)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// The native tier end to end against the fake helper: apps/state read,
// generation-guarded mutation with folded-in state, the acknowledgement
// path, screenshots reaching the sink, and the stale-generation error.
func TestNativeTierWithFakeHelper(t *testing.T) {
	fakeNativeHelper(t)
	ctx, services := withComputerPolicy(t, computer.NewPolicy([]string{"TestApp"}, nil, true))
	var sunk [][]byte
	services.SetScreenshotSink(func(jpegs [][]byte) { sunk = jpegs })

	out, err := runComputerCode(ctx, `print(apps())`)
	if err != nil || !strings.Contains(out, "com.apple.finder") {
		t.Fatalf("apps: %q %v", out, err)
	}
	out, err = runComputerCode(ctx, `print(permissions())`)
	if err != nil || !strings.Contains(out, `"accessibility":true`) {
		t.Fatalf("permissions: %q %v", out, err)
	}

	// A mutation before any state() read has no generation: the fake rejects
	// it as stale, and the error surfaces as a StaleError.
	if _, err := runComputerCode(ctx, `click("TestApp", 0)`); !IsStale(err) {
		t.Fatalf("pre-state click: want stale error, got %v", err)
	}

	// state() then click(): the tool passes gen=2, gets generation 3 back,
	// and both calls attach their screenshots.
	out, err = runComputerCode(ctx, "state(\"TestApp\")\nclick(\"TestApp\", 0)")
	if err != nil {
		t.Fatalf("state+click: %v", err)
	}
	if !strings.Contains(out, "generation=2") || !strings.Contains(out, "generation=3") {
		t.Errorf("want fresh state folded into the mutation:\n%s", out)
	}
	if !strings.Contains(out, "2 screenshot(s) attached") || len(sunk) != 2 || string(sunk[0]) != "hello" {
		t.Errorf("screenshot sink: %q, %d shots", out, len(sunk))
	}
	if g := genFor(ctx, "TestApp"); g != 3 {
		t.Errorf("generation after mutation: %d", g)
	}

	// Acknowledgement path: the helper can't re-read state, the action's
	// outcome surfaces verbatim instead of a summary.
	out, err = runComputerCode(ctx, `type("TestApp", "hi")`)
	if err != nil || !strings.Contains(out, "typed") || !strings.Contains(out, "no AX grant") {
		t.Fatalf("ack path: %q %v", out, err)
	}

	out, err = runComputerCode(ctx, `screenshot("TestApp")`)
	if err != nil || !strings.Contains(out, "screenshot captured: 5 bytes") {
		t.Fatalf("screenshot: %q %v", out, err)
	}
	h, err := computer.Shared()
	if err != nil {
		t.Fatal(err)
	}
	services.computerHelper = h
	if cached, err := services.nativeComputerHelper(); err != nil || cached != h {
		t.Fatalf("cached helper = %p, %v", cached, err)
	}
	services.Close()
}

// Every remaining native mutation forwards its arguments to the helper under
// the app + generation the tool tracks (the fake echoes the params back).
func TestNativeMutationParams(t *testing.T) {
	fakeNativeHelper(t)
	ctx, services := withComputerPolicy(t, computer.NewPolicy([]string{"TestApp"}, nil, true))

	// state() first so the generation guard has something to send.
	if _, err := runComputerCode(ctx, `state("TestApp")`); err != nil {
		t.Fatalf("state: %v", err)
	}
	for _, tc := range []struct {
		code string
		want []string
	}{
		{`press("TestApp", "super+c")`, []string{`title="key" value="super+c"`, `title="gen" value="2"`, `title="app" value="TestApp"`}},
		{`scroll("TestApp", 4, "down", 3)`, []string{`title="index" value="4"`, `title="dir" value="down"`, `title="clicks" value="3"`}},
		{`scroll("TestApp", 4)`, []string{`title="index" value="4"`}}, // optional dir/clicks omitted
		{`set("TestApp", 2, "hello")`, []string{`title="index" value="2"`, `title="value" value="hello"`}},
		{`select("TestApp", 2, "word")`, []string{`title="index" value="2"`, `title="target" value="word"`}},
		{`menu("TestApp", 2, "AXShowMenu")`, []string{`title="index" value="2"`, `title="action" value="AXShowMenu"`}},
	} {
		out, err := runComputerCode(ctx, tc.code)
		if err != nil {
			t.Errorf("%s: %v", tc.code, err)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %s in:\n%s", tc.code, w, out)
			}
		}
	}

	// scroll's optional dir/clicks are dropped when absent.
	if out, err := runComputerCode(ctx, `scroll("TestApp", 4)`); err != nil || strings.Contains(out, `title="dir"`) {
		t.Errorf("scroll without dir: %q %v", out, err)
	}

	// Pixel-coordinate click (the fallback arity) reaches the helper as x/y.
	if _, err := runComputerCode(ctx, `state("TestApp")`); err != nil {
		t.Fatal(err)
	}
	out, err := runComputerCode(ctx, `click("TestApp", 10, 20)`)
	if err != nil {
		t.Fatalf("pixel click: %v", err)
	}
	if !strings.Contains(out, "generation=3") {
		t.Errorf("pixel click state fold-in: %s", out)
	}

	// ax() reads the tree without capturing a screenshot (unlike state()).
	if out, err := runComputerCode(ctx, `ax("TestApp")`); err != nil || strings.Contains(out, "screenshot(s) attached") {
		t.Errorf("ax: %q %v", out, err)
	}

	// A helper reply that isn't an AppState surfaces as an unmarshal error
	// rather than a silent success.
	if _, err := runComputerCode(ctx, `press("TestApp", "BADJSON")`); err == nil || !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Errorf("non-state reply: %v", err)
	}

	// A denied app never reaches the helper.
	services.SetComputerPolicy(computer.NewPolicy(nil, []string{"TestApp"}, true))
	if _, err := runComputerCode(ctx, `press("TestApp", "Return")`); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("denied mutation: %v", err)
	}
}

// Argument validation and the policy gate fire before any platform call, for
// every helper that takes arguments.
func TestComputerArgAndGateErrors(t *testing.T) {
	ctx, _ := withComputerPolicy(t, computer.NewPolicy(nil, []string{"Google Chrome", "Denied"}, true))
	for _, tc := range []struct{ code, want string }{
		// missing/ill-typed args
		{`state()`, "missing arg 1"},
		{`screenshot()`, "missing arg 1"},
		{`click()`, "missing arg 1"},
		{`click("A", "x")`, "must be a number"},
		{`click("A", "x", "y")`, "must be a number"},
		{`click("A", 1, "y")`, "must be a number"},
		{`type()`, "missing arg 1"},
		{`type("A")`, "missing arg 2"},
		{`press()`, "missing arg 1"},
		{`press("A")`, "missing arg 2"},
		{`scroll()`, "missing arg 1"},
		{`scroll("A")`, "missing arg 2"},
		{`set()`, "missing arg 1"},
		{`set("A")`, "missing arg 2"},
		{`set("A", 1)`, "missing arg 3"},
		{`select()`, "missing arg 1"},
		{`select("A")`, "missing arg 2"},
		{`menu()`, "missing arg 1"},
		{`menu("A", "x")`, "must be a number"},
		{`menu("A", 1)`, "missing arg 3"},
		{`tell()`, "missing arg 1"},
		{`tell("A")`, "missing arg 2"},
		{`chrome_goto()`, "missing arg 1"},
		{`chrome_activate("w", 1)`, "must be a number"},
		{`chrome_activate(1, "i")`, "must be a number"},
		{`chrome_close(1, "i")`, "must be a number"},
		{`chrome_find()`, "missing arg 1"},
		// policy gate
		{`state("Denied")`, "policy"},
		{`screenshot("Denied")`, "policy"},
		{`chrome_state()`, "policy"},
		{`chrome_tabs()`, "policy"},
		{`chrome_back()`, "policy"},
		{`chrome_reload()`, "policy"},
		{`chrome_js("1+1")`, "policy"},
		{`chrome_find("x")`, "policy"},
		{`chrome_goto("http://93.184.216.34/")`, "policy"},
		{`chrome_activate(1, 2)`, "policy"},
		{`chrome_close(1, 2)`, "policy"},
	} {
		_, err := runComputerCode(ctx, tc.code)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want %q, got %v", tc.code, tc.want, err)
		}
	}

	// Non-string args are coerced for string parameters: numbers formatted,
	// anything else JSON-encoded.
	if _, err := runComputerCode(ctx, `tell("Denied", 42)`); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("number arg coercion: %v", err)
	}
	if _, err := runComputerCode(ctx, `tell("Denied", true)`); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("bool arg coercion: %v", err)
	}
}

// With no policy installed at all the tool refuses rather than defaulting to
// allow.
func TestGateAppNoPolicy(t *testing.T) {
	ctx, _ := withComputerPolicy(t, nil)
	if err := gateApp(ctx, "Anything"); err == nil || !strings.Contains(err.Error(), "no policy installed") {
		t.Errorf("nil policy: %v", err)
	}
}

// Native helpers that need the driver report the missing-driver guidance
// instead of a confusing RPC error.
func TestNativeHelpersWithoutDriver(t *testing.T) {
	if computer.Available() {
		t.Skip("darwin: an embedded helper may exist")
	}
	t.Setenv("WHIP_COMPUTER_BIN", "")
	computer.ResetShared()
	t.Cleanup(computer.ResetShared)
	ctx, _ := withComputerPolicy(t, computer.NewPolicy([]string{"TestApp"}, nil, true))
	for _, code := range []string{
		`permissions()`, `state("TestApp")`, `ax("TestApp")`, `screenshot("TestApp")`,
		`click("TestApp", 0)`,
	} {
		if _, err := runComputerCode(ctx, code); err == nil || !strings.Contains(err.Error(), "whip-computer driver") {
			t.Errorf("%s: %v", code, err)
		}
	}
}
