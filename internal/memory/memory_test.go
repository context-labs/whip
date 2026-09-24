package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The agreed contract: markdown bullets in both scopes, remember appends,
// forget strikes (doesn't delete), prompt block carries only open entries,
// and the cap is enforced.
func TestScopeRoundTrip(t *testing.T) {
	s := Scope{Path: filepath.Join(t.TempDir(), "memory.md"), Name: "installation"}

	if got := s.Entries(); got != nil {
		t.Fatalf("missing file should be an empty list, got %v", got)
	}
	if err := s.Remember("prefers pnpm over npm"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember("deploy with ./scripts/ship.sh"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(s.Path)
	if string(data) != "- [ ] prefers pnpm over npm\n- [ ] deploy with ./scripts/ship.sh\n" {
		t.Fatalf("file must be plain markdown bullets:\n%s", data)
	}

	if err := s.Forget(1); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(s.Path)
	if !strings.Contains(string(data), "- [x] prefers pnpm over npm") {
		t.Fatalf("forget should strike, not delete:\n%s", data)
	}
	if err := s.Forget(1); err == nil {
		t.Fatal("forgetting a done entry should say so")
	}
	if err := s.Forget(9); err == nil {
		t.Fatal("forgetting a missing entry should error")
	}

	// injection carries only open entries, numbered as in the file
	block := PromptBlock(s)
	if strings.Contains(block, "pnpm") {
		t.Fatalf("done entries must not be injected:\n%s", block)
	}
	if !strings.Contains(block, "2. deploy with ./scripts/ship.sh") {
		t.Fatalf("open entry should be injected with its number:\n%s", block)
	}

	// empty scopes inject nothing
	if b := PromptBlock(Scope{}, Scope{}); b != "" {
		t.Fatalf("no scopes should inject nothing, got %q", b)
	}
}

func TestRememberValidationAndCap(t *testing.T) {
	s := Scope{Path: filepath.Join(t.TempDir(), "m.md"), Name: "session"}
	if err := s.Remember("   "); err == nil {
		t.Fatal("blank entries should be rejected")
	}
	if err := s.Remember(strings.Repeat("x", maxEntryLength+1)); err == nil {
		t.Fatal("overlong entries should be rejected")
	}
	for range maxEntries {
		if err := s.Remember("fact"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Remember("one too many"); err == nil {
		t.Fatal("the cap must reject; the model is told to forget first")
	}
	// forgetting one opens a slot again
	if err := s.Forget(1); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember("fits now"); err != nil {
		t.Fatal(err)
	}
}

// Prose the user writes by hand (headers, notes) survives a forget rewrite.
func TestForgetPreservesUserProse(t *testing.T) {
	s := Scope{Path: filepath.Join(t.TempDir(), "m.md"), Name: "session"}
	os.WriteFile(s.Path, []byte("# my notes\nremember to water the plants\n- [ ] prefers dark mode\n"), 0o644)
	if err := s.Forget(1); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.Path)
	if !strings.Contains(string(data), "# my notes") || !strings.Contains(string(data), "water the plants") {
		t.Fatalf("hand-written prose must survive:\n%s", data)
	}
}

// Scope constructors resolve under the whipcode home (WHIPCODE_HOME overrides it).
func TestScopeConstructors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", home)

	inst := Installation()
	if inst.Path != filepath.Join(home, "memory.md") || inst.Name != "installation" {
		t.Fatalf("Installation() = %+v", inst)
	}
	sess := Session("abcd1234")
	if sess.Path != filepath.Join(home, "sessions", "abcd1234.memory.md") || sess.Name != "session" {
		t.Fatalf("Session() = %+v", sess)
	}
	// no session id yet → the zero scope, which rejects writes and reads empty
	zero := Session("")
	if zero != (Scope{}) {
		t.Fatalf("Session(\"\") = %+v, want zero scope", zero)
	}
	if got := zero.Entries(); got != nil {
		t.Fatalf("zero scope should read empty, got %v", got)
	}
	if err := zero.Remember("x"); err == nil {
		t.Fatal("remember on the zero scope should error")
	}
	if err := zero.Forget(1); err == nil {
		t.Fatal("forget on the zero scope should error")
	}
	// constructors and the round-trip compose: remember lands in WHIPCODE_HOME
	if err := inst.Remember("a fact"); err != nil {
		t.Fatal(err)
	}
	if es := Installation().Entries(); len(es) != 1 || es[0].Text != "a fact" {
		t.Fatalf("entries via constructor: %+v", es)
	}
}

// Forget with no file yet reports "no memories" instead of failing oddly.
func TestForgetMissingFile(t *testing.T) {
	s := Scope{Path: filepath.Join(t.TempDir(), "none.md"), Name: "session"}
	if err := s.Forget(1); err == nil {
		t.Fatal("forget with no file should error")
	}
}
