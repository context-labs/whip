package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, name, args string) string {
	t.Helper()
	return Execute(context.Background(), All(), name, json.RawMessage(args))
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

func (m *mockInteractiveRunner) Run(_ context.Context, command string, timeout time.Duration, keys <-chan []byte) string {
	m.gotCommand = command
	m.gotTimeout = timeout
	m.gotKeys = keys
	return m.returnThis
}

// TestBashToolInteractiveHook verifies that bash with interactive:true hands
// off to the installed InteractiveBash runner, passing command+timeout+keys,
// and returns whatever the runner returns. It also confirms the hook is
// consulted only when interactive is true.
func TestBashToolInteractiveHook(t *testing.T) {
	mock := &mockInteractiveRunner{returnThis: "PASSWORD_ACCEPTED\n(exit: 0)"}
	prev := InteractiveBash
	InteractiveBash = mock
	defer func() { InteractiveBash = prev }()

	out := run(t, "bash", `{"command":"sudo apt install -y sl","interactive":true,"timeout":20}`)
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
	out = run(t, "bash", `{"command":"echo nohook"}`)
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
	w := writeTool()
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

// Binary tool output must be replaced with a compact placeholder instead of
// having raw NUL/control bytes or base64 garbage injected into the
// conversation. isBinary drives the read/bash gate; read exercises the full
// path with a real binary file, bash with a synthetic binary stream.
func TestBinaryOutputPlaceholder(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want bool
	}{
		{name: "empty text", in: nil, want: false},
		{name: "plain text", in: []byte("package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"hi\") }\n"), want: false},
		{name: "utf8 text", in: []byte("héllo wörld → ütf8 ✓\n"), want: false},
		{name: "single nul", in: []byte{0x00}, want: true},
		{name: "nul in text", in: append([]byte("abc"), 0x00, 'd', 'e', 'f'), want: true},
		{name: "invalid utf8", in: []byte{0xff, 0xfe, 0x00, 'x'}, want: true}, // BOM-ish, not valid
		{name: "control heavy", in: bytes.Repeat([]byte{0x01}, 100), want: true},
		{name: "whitespace controls ok", in: []byte("line1\n\tline2\rline3\f\v"), want: false},
		// Regression: multi-byte runes anywhere in the buffer (including past a
		// hard 1KB boundary position) must not read as binary — the UTF-8 check
		// covers the whole buffer, so there is no probe cut to straddle.
		{name: "utf8 multi-byte deep in buffer", in: append(bytes.Repeat([]byte("a"), 1023), []byte("é世界")...), want: false},
		// Regression: ANSI-colored output (ls/grep --color) is ESC-heavy but not
		// binary — ESC is excluded from the control-byte count.
		{name: "ansi colored output", in: []byte("\x1b[31mred\x1b[0m \x1b[32mgreen\x1b[0m \x1b[1mBold\x1b[0m normal text here\n"), want: false},
		// Regression: output that starts as clean text but turns binary later
		// must still be caught — the NUL scan covers the whole buffer.
		{name: "text then binary deep in buffer", in: append(bytes.Repeat([]byte("a"), 1124), 0x00, 0x01), want: true},
		// Regression: a Latin-1 file (smart quotes, no NULs) has invalid UTF-8
		// bytes anywhere in the buffer — it must read as binary.
		{name: "latin1 interior bytes stay binary", in: append(bytes.Repeat([]byte("a"), 1023), 0x93, 0x94, 0x92), want: true},
		// Regression (review): an INVALID sequence — E0 80 is an overlong
		// encoding lead with an illegal second byte — must read as binary.
		{name: "invalid overlong stays binary", in: append(bytes.Repeat([]byte("a"), 1022), 0xE0, 0x80), want: true},
		// C1 lead byte (0xC0/0xC1 are never legal UTF-8).
		{name: "c1 lead stays binary", in: append(bytes.Repeat([]byte("a"), 1023), 0xC1, 0xBF), want: true},
		// Regression (review round 4): invalid UTF-8 deep in the buffer with no
		// NULs (a log file with corrupt bytes partway through) must still be
		// binary — the UTF-8 check covers the whole buffer, not just the prefix.
		{name: "invalid utf8 deep in buffer stays binary", in: append(bytes.Repeat([]byte("a"), 1124), 0x93, 0x94), want: true},
		// Regression (review round 5): a NUL-free control-junk tail (text then
		// 4KB of 0x01) passes NUL/UTF-8 — the density check must cover the whole
		// buffer too, not just a prefix.
		{name: "control junk tail in buffer", in: append(bytes.Repeat([]byte("a"), 1024), bytes.Repeat([]byte{0x01}, 4096)...), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinary(tt.in); got != tt.want {
				t.Fatalf("isBinary(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}

	// read: a real binary file straight through the read tool.
	dir := t.TempDir()
	bin := filepath.Join(dir, "blob.bin")
	raw := append(bytes.Repeat([]byte{0x01, 0x02, 0x03}, 40), 0x00, 0x00, 0x00) // ~120 bytes binary
	if err := os.WriteFile(bin, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out := run(t, "read", fmt.Sprintf(`{"path":%q}`, bin))
	want := fmt.Sprintf("[binary: %s, %s]", bin, bytesHuman(len(raw)))
	if out != want {
		t.Fatalf("read placeholder:\n got %q\nwant %q", out, want)
	}

	// bash: a command that streams binary bytes. NULs can't travel through a
	// shell string argument, so write a binary file and cat it.
	if out := run(t, "bash", fmt.Sprintf(`{"command":"cat %s | head -c 200"}`, bin)); !strings.Contains(out, "not shown") {
		t.Fatalf("bash binary output not replaced: %q", out)
	}

	// A plain-text read still returns line-numbered content.
	txt := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(txt, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run(t, "read", fmt.Sprintf(`{"path":%q}`, txt)); !strings.Contains(out, "2\ttwo") {
		t.Fatalf("text read should stay intact: %q", out)
	}
}
