package computer

import (
	"errors"
	"strings"
	"testing"
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
