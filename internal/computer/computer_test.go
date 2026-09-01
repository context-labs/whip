package computer

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuote(t *testing.T) {
	// mack's flaw: unescaped quotes break the script. Ours escapes.
	if got := quote(`a"b`); got != `"a\"b"` {
		t.Errorf("quote: %q", got)
	}
	if got := quote(`back\slash`); got != `"back\\slash"` {
		t.Errorf("quote backslash: %q", got)
	}
	if got := quote("plain"); got != `"plain"` {
		t.Errorf("quote plain: %q", got)
	}
}

func TestPolicyCheck(t *testing.T) {
	p := NewPolicy([]string{"Google Chrome"}, []string{"Finder"}, true)
	if err := p.Check("Google Chrome"); err != nil {
		t.Errorf("allowed app blocked: %v", err)
	}
	if err := p.Check("google chrome"); err != nil { // case-insensitive
		t.Errorf("case-normalized allow blocked: %v", err)
	}
	if err := p.Check("Finder"); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("denied app must fail with policy error, got %v", err)
	}
	err := p.Check("Safari")
	if err == nil {
		t.Fatal("unlisted app under default-deny must need approval")
	}
	approvalNeeded := &ApprovalNeeded{}
	if !errors.As(err, &approvalNeeded) {
		t.Fatalf("want ApprovalNeeded, got %T", err)
	}
	p.Approve("Safari")
	if err := p.Check("Safari"); err != nil {
		t.Errorf("session approval must unblock: %v", err)
	}
}

func TestPolicyDefaultAllow(t *testing.T) {
	p := NewPolicy(nil, []string{"Finder"}, false)
	if err := p.Check("Safari"); err != nil {
		t.Errorf("default-allow must pass unlisted apps: %v", err)
	}
	if err := p.Check("Finder"); err == nil {
		t.Error("deny list wins even under default-allow")
	}
}

func TestChromeTabsParse(t *testing.T) {
	// The ￨ separator survives titles containing | or newlines in fields.
	p := &Policy{}
	_ = p
	// (parse logic lives in ChromeTabs; here we pin the separator choice)
	line := "1￨2￨https://example.com￨a title with | pipe"
	f := strings.SplitN(line, "￨", 4)
	if len(f) != 4 || f[3] != "a title with | pipe" {
		t.Fatalf("separator parse: %v", f)
	}
}

// On non-macOS every osascript-tier entry point must fail with
// ErrUnsupportedPlatform (the platform gate fires before any exec).
func TestUnsupportedPlatform(t *testing.T) {
	if Available() {
		t.Skip("darwin: osascript tier would drive the real desktop")
	}
	automation := Automation{}
	if _, err := automation.AppleScript(`return "x"`); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("osascript: %v", err)
	}
	if _, err := Tell("Finder", "activate"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("Tell: %v", err)
	}
	calls := map[string]error{}
	_, _, calls["ChromeActive"] = ChromeActive(automation)
	_, calls["ChromeTabs"] = ChromeTabs(automation)
	calls["ChromeGoto"] = ChromeGoto("https://example.com", automation)
	calls["ChromeNewTab"] = ChromeNewTab("https://example.com", automation)
	calls["ChromeActivateTab"] = ChromeActivateTab(1, 2, automation)
	calls["ChromeCloseTab"] = ChromeCloseTab(1, 2, automation)
	calls["ChromeBack"] = ChromeBack(automation)
	calls["ChromeReload"] = ChromeReload(automation)
	_, calls["ChromeFindTab"] = ChromeFindTab("example", automation)
	_, calls["ChromeState"] = ChromeState(automation)
	for name, err := range calls {
		if !errors.Is(err, ErrUnsupportedPlatform) {
			t.Errorf("%s: want ErrUnsupportedPlatform, got %v", name, err)
		}
	}
	// The platform error must NOT be rewritten to the Chrome-toggle guidance.
	if _, err := ChromeJS("1+1", automation); !errors.Is(err, ErrUnsupportedPlatform) || errors.Is(err, ErrJSFromAppleEvents) {
		t.Errorf("ChromeJS: %v", err)
	}
	if _, err := ensureHelperBinary(); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("ensureHelperBinary: %v", err)
	}
}

func TestPolicyDenyAndSummary(t *testing.T) {
	p := NewPolicy([]string{"Google Chrome"}, nil, false)
	if got := p.Summary(); got != "google chrome" {
		t.Errorf("Summary: %q", got)
	}
	p.Approve("Safari")
	if got := p.Summary(); !strings.Contains(got, "google chrome") || !strings.Contains(got, "safari (session)") {
		t.Errorf("Summary with session approval: %q", got)
	}
	// Deny blocks for the session and revokes a session approval.
	p.Deny("Safari")
	if err := p.Check("Safari"); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Errorf("session-denied app must fail: %v", err)
	}
	// Approve clears a session deny again.
	p.Approve("Safari")
	if err := p.Check("Safari"); err != nil {
		t.Errorf("re-approval must unblock: %v", err)
	}
	if got := NewPolicy(nil, nil, true).Summary(); got != "none" {
		t.Errorf("empty Summary: %q", got)
	}
}

func TestApprovalNeededError(t *testing.T) {
	e := &ApprovalNeeded{App: "Safari"}
	if !strings.Contains(e.Error(), `"Safari"`) || !strings.Contains(e.Error(), "computer.allow") {
		t.Errorf("ApprovalNeeded.Error: %q", e.Error())
	}
}

func TestAutomationUsesInjectedRunner(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	a := Automation{Context: ctx, Run: func(gotCtx context.Context, name string, args ...string) ([]byte, error) {
		if gotCtx != ctx || name != "command" || len(args) != 2 || args[0] != "one" || args[1] != "two" {
			t.Fatalf("runner call = (%v, %q, %q)", gotCtx, name, args)
		}
		return []byte("output"), nil
	}}
	if out, err := a.run("command", "one", "two"); err != nil || string(out) != "output" {
		t.Fatalf("run = %q, %v", out, err)
	}
}

func TestChromeOperationsWithFakeAutomation(t *testing.T) {
	if !Available() {
		t.Skip("darwin-only AppleScript entry points")
	}
	var scripts []string
	automation := Automation{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "osascript" || len(args) != 2 || args[0] != "-e" {
			t.Fatalf("runner call = %q %q", name, args)
		}
		script := args[1]
		scripts = append(scripts, script)
		switch {
		case strings.Contains(script, "set theUrl"):
			return []byte("https://active.example\nActive title\n"), nil
		case strings.Contains(script, "set wCount"):
			return []byte("1￨2￨https://one.example￨One | title\ninvalid\n2￨1￨https://two.example￨Two\n"), nil
		default:
			return []byte("ok\n"), nil
		}
	}}

	url, title, err := ChromeActive(automation)
	if err != nil || url != "https://active.example" || title != "Active title" {
		t.Fatalf("ChromeActive = %q, %q, %v", url, title, err)
	}
	tabs, err := ChromeTabs(automation)
	if err != nil || len(tabs) != 2 || tabs[0].Window != 1 || tabs[0].Index != 2 || tabs[0].Title != "One | title" {
		t.Fatalf("ChromeTabs = %+v, %v", tabs, err)
	}
	if tab, err := ChromeFindTab("two.example", automation); err != nil || tab == nil || tab.Window != 2 {
		t.Fatalf("ChromeFindTab = %+v, %v", tab, err)
	}
	if state, err := ChromeState(automation); err != nil || !strings.Contains(state, `"active"`) || !strings.Contains(state, "https://two.example") {
		t.Fatalf("ChromeState = %q, %v", state, err)
	}

	for _, tc := range []struct {
		name string
		want string
		run  func() error
	}{
		{"goto", `set URL of active tab of front window to "https://example.com"`, func() error { return ChromeGoto("https://example.com", automation) }},
		{"new tab", "make new tab", func() error { return ChromeNewTab("https://example.com", automation) }},
		{"activate", "set active tab index of window 1 to 2", func() error { return ChromeActivateTab(1, 2, automation) }},
		{"close", "close tab 2 of window 1", func() error { return ChromeCloseTab(1, 2, automation) }},
		{"back", "go back", func() error { return ChromeBack(automation) }},
		{"reload", "reload", func() error { return ChromeReload(automation) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(scripts)
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			if len(scripts) != before+1 || !strings.Contains(scripts[before], tc.want) {
				t.Fatalf("script = %q, want %q", scripts[before], tc.want)
			}
		})
	}

	canceled := Automation{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("User canceled.\n"), errors.New("exit status 1")
	}}
	if out, err := canceled.AppleScript("display dialog"); err != nil || out != "User canceled." {
		t.Fatalf("canceled AppleScript = %q, %v", out, err)
	}
	jsBlocked := Automation{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("execute javascript is not allowed"), errors.New("exit status 1")
	}}
	if _, err := ChromeJS("1+1", jsBlocked); !errors.Is(err, ErrJSFromAppleEvents) {
		t.Fatalf("ChromeJS error = %v, want ErrJSFromAppleEvents", err)
	}
}

type testWriteCloser struct{ io.Writer }

func (testWriteCloser) Close() error { return nil }

func TestHelperStartupFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script, want string
	}{
		{"announcement", "#!/bin/sh\nexit 0\n", "did not announce"},
		{"handshake", "#!/bin/sh\nprintf 'whip-computer/1\\n'\nread request\nprintf '{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":1,\"message\":\"no\"}}\\n'\n", "handshake"},
		{"handshake version", "#!/bin/sh\nprintf 'whip-computer/1\\n'\nread request\nprintf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"version\":\"wrong\"}}\\n'\n", "handshake version mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "helper")
			if err := os.WriteFile(bin, []byte(tc.script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WHIP_COMPUTER_BIN", bin)
			h := &Helper{}
			if err := h.spawn(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("spawn error = %v, want %q", err, tc.want)
			}
		})
	}

	t.Setenv("WHIP_COMPUTER_BIN", filepath.Join(t.TempDir(), "missing"))
	if _, err := NewManagedHelper(nil, "root", t.TempDir(), nil); err == nil {
		t.Fatal("NewManagedHelper accepted a missing binary")
	}
}

func TestHelperFrameFailures(t *testing.T) {
	if err := (&Helper{}).callLocked(t.Context(), "test", nil, nil); err == nil {
		t.Fatal("an unstarted helper accepted a call")
	}
	h := &Helper{cmd: &exec.Cmd{}, stdin: testWriteCloser{io.Discard}, reader: bufio.NewReader(strings.NewReader(""))}
	if err := h.callLocked(t.Context(), "test", map[string]any{"bad": make(chan int)}, nil); err == nil {
		t.Fatal("callLocked accepted non-JSON params")
	}
	h.reader = bufio.NewReader(strings.NewReader("bad frame\n"))
	if err := h.callLocked(t.Context(), "test", nil, nil); err == nil || !strings.Contains(err.Error(), "bad frame") {
		t.Fatalf("bad frame error = %v", err)
	}
	h.reader = bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","id":1}` + "\n"))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := h.callLocked(ctx, "test", nil, nil); err != nil {
		t.Fatalf("empty response = %v", err)
	}

	r, w := io.Pipe()
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	h.reader = bufio.NewReader(r)
	if _, err := h.readLineTimeout(time.Millisecond); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("read timeout error = %v", err)
	}

	closedR, closedW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = closedR.Close()
	_ = closedW.Close()
	t.Setenv("WHIP_COMPUTER_BIN", filepath.Join(t.TempDir(), "missing"))
	h = &Helper{cmd: &exec.Cmd{}, stdin: closedW, reader: bufio.NewReader(strings.NewReader(""))}
	if err := h.Call(t.Context(), "test", nil, nil); err == nil || !strings.Contains(err.Error(), "restart failed") {
		t.Fatalf("restart failure = %v", err)
	}
}
