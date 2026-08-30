package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

func run(t *testing.T, name, args string) string {
	t.Helper()
	return Execute(context.Background(), directTools(), name, json.RawMessage(args))
}

func TestToolRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "sub", "a.txt")

	out := run(t, "write", fmt.Sprintf(`{"path":%q,"content":"one\ntwo\nthree\n"}`, f))
	if strings.HasPrefix(out, "Error") {
		t.Fatal(out)
	}
	out = run(t, "read", fmt.Sprintf(`{"path":%q}`, f))
	if !strings.Contains(out, "2\ttwo") {
		t.Fatalf("read missing line numbers: %q", out)
	}
	out = run(t, "edit", fmt.Sprintf(`{"path":%q,"old_string":"two","new_string":"2"}`, f))
	if strings.HasPrefix(out, "Error") {
		t.Fatal(out)
	}
	out = run(t, "read", fmt.Sprintf(`{"path":%q,"offset":2,"limit":1}`, f))
	if strings.TrimSpace(out) != "2\t2" {
		t.Fatalf("edit not applied: %q", out)
	}
	// ambiguous edit must fail without replace_all
	run(t, "write", fmt.Sprintf(`{"path":%q,"content":"x x"}`, f))
	out = run(t, "edit", fmt.Sprintf(`{"path":%q,"old_string":"x","new_string":"y"}`, f))
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("expected ambiguity error, got %q", out)
	}
	out = run(t, "bash", `{"command":"echo hi; echo err >&2; exit 3"}`)
	if !strings.Contains(out, "hi") || !strings.Contains(out, "err") || !strings.Contains(out, "exit") {
		t.Fatalf("bash output wrong: %q", out)
	}
	out = run(t, "nope", `{}`)
	if !strings.Contains(out, "unknown tool") {
		t.Fatalf("expected unknown tool error, got %q", out)
	}
}

func TestHelpersAndEdgeCases(t *testing.T) {
	if len(Defs(All())) != 4 {
		t.Fatal("expected 4 tool defs")
	}
	long := strings.Repeat("x", maxOutput+10)
	out := truncate(long)
	if !strings.Contains(out, "10 bytes elided from the middle") {
		t.Fatalf("truncate: %q", out[len(out)-60:])
	}
	// head and tail both survive the middle elision, and the spill marker
	// points at a recoverable full copy
	if !strings.HasPrefix(out, strings.Repeat("x", 100)) || !strings.HasSuffix(out, strings.Repeat("x", 100)) {
		t.Fatal("middle elision must keep head and tail")
	}
	if !strings.Contains(out, "full output") {
		t.Fatal("truncation should spill the full output and point at it")
	}
	if out2 := TruncateTail(long); !strings.HasPrefix(out2, "[... first 10 bytes truncated]") {
		t.Fatalf("truncateTail: %q", out2[:40])
	}
	// short strings pass through untouched
	if truncate("ok") != "ok" || TruncateTail("ok") != "ok" {
		t.Fatal("short strings must not be modified")
	}

	// bad args json hits every tool's unmarshal error branch
	for _, name := range []string{"bash", "read", "write", "edit"} {
		if out := run(t, name, `{bad`); !strings.HasPrefix(out, "Error") {
			t.Fatalf("%s: expected error, got %q", name, out)
		}
	}

	// empty output branch
	if out := run(t, "bash", `{"command":"true"}`); out != "(no output)" {
		t.Fatalf("empty output: %q", out)
	}
	// timeout branch
	if out := run(t, "bash", `{"command":"sleep 5","timeout":0.1}`); !strings.Contains(out, "timed out") {
		t.Fatalf("timeout: %q", out)
	}

	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	// read: missing file, offset past EOF, default limit
	if out := run(t, "read", fmt.Sprintf(`{"path":%q}`, f)); !strings.HasPrefix(out, "Error") {
		t.Fatalf("missing file: %q", out)
	}
	run(t, "write", fmt.Sprintf(`{"path":%q,"content":"a\nb"}`, f))
	if out := run(t, "read", fmt.Sprintf(`{"path":%q,"offset":99}`, f)); !strings.Contains(out, "past end") {
		t.Fatalf("offset past EOF: %q", out)
	}
	// write: MkdirAll fails when a parent is a file
	if out := run(t, "write", fmt.Sprintf(`{"path":%q,"content":"x"}`, f+"/child.txt")); !strings.HasPrefix(out, "Error") {
		t.Fatalf("bad parent: %q", out)
	}
	// edit: missing file, not-found old_string, replace_all
	if out := run(t, "edit", fmt.Sprintf(`{"path":%q,"old_string":"x","new_string":"y"}`, filepath.Join(dir, "nope"))); !strings.HasPrefix(out, "Error") {
		t.Fatalf("edit missing file: %q", out)
	}
	if out := run(t, "edit", fmt.Sprintf(`{"path":%q,"old_string":"zzz","new_string":"y"}`, f)); !strings.Contains(out, "not found") {
		t.Fatalf("edit not found: %q", out)
	}
	run(t, "write", fmt.Sprintf(`{"path":%q,"content":"x x x"}`, f))
	if out := run(t, "edit", fmt.Sprintf(`{"path":%q,"old_string":"x","new_string":"y","replace_all":true}`, f)); !strings.Contains(out, "3 occurrence") {
		t.Fatalf("replace_all: %q", out)
	}
}

func TestBashToolFastFailOnTTYRead(t *testing.T) {
	// Regression: a command that reads from /dev/tty (as sudo does for a
	// password) must NOT hang the tool. pre-fix the tool used CombinedOutput
	// with the child sharing whip's controlling terminal, so the read
	// blocked until the 120s bash timeout. post-fix the child runs in a new
	// session with no controlling tty and stdin tied to /dev/null, so the
	// read fails immediately. We assert it returns well under the cap and
	// surfaces the tty failure rather than silently succeeding.
	start := time.Now()
	out := run(t, "bash", `{"command":"read -r p < /dev/tty; echo got $p","timeout":5}`)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("bash tool hung %s on /dev/tty read — fast-fail regressed: %q", elapsed, out)
	}
	if strings.Contains(out, "timed out") {
		t.Fatalf("bash tool timed out on /dev/tty read — fast-fail regressed: %q", out)
	}
	// The /dev/tty open must fail (no controlling terminal under Setsid);
	// bash reports "No such device or address" or similar. The crucial bit is
	// that $p is EMPTY — no password was read — and we did not hang.
	if !strings.Contains(out, "/dev/tty") {
		t.Fatalf("expected a /dev/tty error in output: %q", out)
	}
}

// mockInteractiveRunner is a fake tools.InteractiveRunner used to verify the
// bash tool's interactive hook wiring without spinning up a PTY.
type mockInteractiveRunner struct {
	gotCommand string
	gotTimeout time.Duration
	gotKeys    <-chan []byte
	returnThis string
}

func (m *mockInteractiveRunner) Run(_ context.Context, opts bashrun.Options) string {
	m.gotCommand = opts.Command
	m.gotTimeout = opts.Timeout
	m.gotKeys = opts.Keys
	return m.returnThis
}

// TestBashToolInteractiveHook verifies that bash with interactive:true hands
// off to the injected runner, passing command+timeout+keys,
// and returns whatever the runner returns. It also confirms the hook is
// consulted only when interactive is true.
func TestBashToolInteractiveHook(t *testing.T) {
	mock := &mockInteractiveRunner{returnThis: "PASSWORD_ACCEPTED\n(exit: 0)"}
	services := NewServices()
	services.SetInteractive(mock)

	out := Execute(context.Background(), []Tool{bashTool(services)}, "bash", json.RawMessage(`{"command":"sudo apt install -y sl","interactive":true,"timeout":20}`))
	if out != "PASSWORD_ACCEPTED\n(exit: 0)" {
		t.Fatalf("interactive bash should return runner output verbatim: %q", out)
	}
	if mock.gotCommand != "sudo apt install -y sl" {
		t.Fatalf("runner got wrong command: %q", mock.gotCommand)
	}
	if mock.gotTimeout != 20*time.Second {
		t.Fatalf("runner got wrong timeout: %v", mock.gotTimeout)
	}
	if mock.gotKeys == nil {
		t.Fatalf("runner must receive a keys channel")
	}

	// interactive:false must NOT call the runner even when it's installed
	mock.gotCommand = ""
	out = Execute(context.Background(), []Tool{bashTool(services)}, "bash", json.RawMessage(`{"command":"echo nohook"}`))
	if mock.gotCommand != "" {
		t.Fatalf("non-interactive call should not reach the runner: %q", mock.gotCommand)
	}
	if !strings.Contains(out, "nohook") {
		t.Fatalf("non-interactive output wrong: %q", out)
	}
}

// editDiff numbers rows from the file's absolute line when startLine > 0,
// renders unnumbered rows at 0, and caps runaway diffs.
func TestEditDiffLineNumbers(t *testing.T) {
	d := editDiff("ctx\nold\ntail", "ctx\nnew\ntail", 10)
	want := "10   ctx\n11 - old\n11 + new\n12   tail"
	if d != want {
		t.Fatalf("numbered diff:\n%s\nwant:\n%s", d, want)
	}
	if d := editDiff("old", "new", 0); d != "- old\n+ new" {
		t.Fatalf("unnumbered diff: %q", d)
	}
	if editDiff("same", "same", 5) != "" {
		t.Fatal("identical strings should yield no diff")
	}
	big := strings.Repeat("x\n", editDiffMaxLines+50)
	if d := editDiff("", big, 1); !strings.Contains(d, "more lines") {
		t.Fatal("oversized diff should carry the cap marker")
	}
}

// An overwrite carries an absolute-numbered diff; a fresh file does not.
func TestWriteToolDiffOnOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	w := writeTool(NewServices())
	out, err := w.Run(context.Background(), json.RawMessage(`{"path":"`+p+`","content":"a\nb\n"}`))
	if err != nil || strings.Contains(out, "```diff") {
		t.Fatalf("fresh write should carry no diff: %q, %v", out, err)
	}
	out, err = w.Run(context.Background(), json.RawMessage(`{"path":"`+p+`","content":"a\nc\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "```diff") || !strings.Contains(out, "2 - b") || !strings.Contains(out, "2 + c") {
		t.Fatalf("overwrite should diff with absolute line numbers: %q", out)
	}
}

func TestServicesKeepHostHooksIsolated(t *testing.T) {
	one, two := NewServices(), NewServices()
	browserOne, browserTwo := browser.NewManager(browser.ModeHeadless), browser.NewManager(browser.ModeLive)
	one.SetBrowser(browserOne, true)
	two.SetBrowser(browserTwo, false)
	one.SetComputerPolicy(computer.NewPolicy([]string{"One"}, nil, true))
	two.SetComputerPolicy(computer.NewPolicy([]string{"Two"}, nil, true))
	oneCtx, twoCtx := WithServices(t.Context(), one), WithServices(t.Context(), two)

	if one.Browser() != browserOne || two.Browser() != browserTwo {
		t.Fatal("browser managers crossed service boundaries")
	}
	if err := gateApp(oneCtx, "One"); err != nil {
		t.Fatalf("first policy denied its app: %v", err)
	}
	if err := gateApp(twoCtx, "One"); err == nil {
		t.Fatal("second policy inherited the first service's approval")
	}

	noteGeneration(oneCtx, "App", &computer.AppState{Generation: 7})
	if genFor(oneCtx, "App") != 7 || genFor(twoCtx, "App") != 0 {
		t.Fatal("computer generations crossed service boundaries")
	}

	var oneShots, twoShots int
	one.SetScreenshotSink(func(shots [][]byte) { oneShots += len(shots) })
	two.SetScreenshotSink(func(shots [][]byte) { twoShots += len(shots) })
	one.screenshots()([][]byte{{1}})
	if oneShots != 1 || twoShots != 0 {
		t.Fatal("screenshot callback crossed service boundaries")
	}
}

func TestServicesKeepProcessScopesIsolated(t *testing.T) {
	processes := capability.NewProcessManager()
	defer processes.Close()
	one, two := NewServices(), NewServices()
	one.processes, one.processCwd, one.processEnv, one.authority.RootID = processes, canonicalDir(t, t.TempDir()), map[string]string{"WHIP_SESSION_ID": "one"}, "one"
	two.processes, two.processCwd, two.processEnv, two.authority.RootID = processes, canonicalDir(t, t.TempDir()), map[string]string{"WHIP_SESSION_ID": "two"}, "two"

	type result struct{ output string }
	results := make(chan result, 2)
	for _, services := range []*Services{one, two} {
		go func() {
			out, err := services.computerAutomation(t.Context()).Run(t.Context(), "sh", "-c", `printf '%s:%s' "$WHIP_SESSION_ID" "$PWD"`)
			if err != nil {
				results <- result{"error: " + err.Error()}
				return
			}
			results <- result{string(out)}
		}()
	}
	got := map[string]bool{(<-results).output: true, (<-results).output: true}
	if !got["one:"+one.processCwd] || !got["two:"+two.processCwd] {
		t.Fatalf("process scopes crossed: %v", got)
	}
}

func canonicalDir(t *testing.T, dir string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
