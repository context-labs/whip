package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Hook discovery uses the platform home directory. Isolate the whole test
// binary so no CLI test can load or execute a developer's real plugins.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "whip-cli-test-home")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// The system prompt always carries the built-in operating rules (the safety
// rails); ~/.whip/me.md appends the user's standing instructions after them.
func TestSystemPromptAppendsUserMe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	p := systemPrompt(t.TempDir(), time.Now())
	if !strings.Contains(p, "never force-push") {
		t.Fatal("built-in operating rules must always be present")
	}
	if strings.Contains(p, "Standing instructions") {
		t.Fatal("a fresh install (all-comments me.md) appends nothing")
	}

	os.WriteFile(filepath.Join(home, "me.md"), []byte("- Always pnpm, never npm.\n"), 0o644)
	p = systemPrompt(t.TempDir(), time.Now())
	if !strings.Contains(p, "never force-push") {
		t.Fatal("built-in rules survive a user me.md")
	}
	if !strings.Contains(p, "Standing instructions from the user") || !strings.Contains(p, "Always pnpm") {
		t.Fatalf("user instructions should append:\n%s", p)
	}
}

// The env block tells the model where and when it is: the working directory,
// the platform, the current date/time with the local timezone, and the OS
// username — so relative dates ("tomorrow", "last Tuesday") resolve against
// the user's clock, and the model knows who it's working with.
func TestSystemPromptEnvBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	now := time.Date(2026, 8, 28, 19, 21, 11, 0, time.Local)
	p := systemPrompt("/tmp/work", now)

	for _, want := range []string{
		"<env>",
		"Working directory: /tmp/work",
		"Current date/time: Fri Aug 28, 2026 19:21:11",
		"User: ",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("env block should contain %q:\n%s", want, p)
		}
	}
	if !strings.Contains(p, " (UTC") {
		t.Fatalf("date/time should carry a UTC offset:\n%s", p)
	}
}
