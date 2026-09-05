package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeSeedsTemplateAndStripsComments(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())

	// first read seeds the file with the commented template
	if got := MeInstructions(); got != "" {
		t.Fatalf("a fresh seed is all comments — nothing to inject, got %q", got)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("WHIP_HOME"), "me.md"))
	if err != nil || !strings.Contains(string(data), "# Your standing instructions") {
		t.Fatalf("seed file should exist with the template: %v\n%s", err, data)
	}

	// user edits land in the injection; comments and blanks stay out
	os.WriteFile(filepath.Join(os.Getenv("WHIP_HOME"), "me.md"),
		[]byte("# hi\n\n- Always pnpm.\n- Ask before force-push.\n"), 0o644)
	got := MeInstructions()
	if !strings.Contains(got, "- Always pnpm.") || strings.Contains(got, "# hi") {
		t.Fatalf("instructions should carry user lines only:\n%s", got)
	}

	// the built-in operating rules are unaffected — the file APPENDS
	if !strings.Contains(MeSeed, "/me opens this file") {
		t.Fatal("seed should tell the user how to edit")
	}
}

func TestLoadMeInstructionsReportsIncompleteRules(t *testing.T) {
	for _, failure := range []string{"oversized", "directory", "invalid-utf8", "bad-home"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("WHIP_HOME", dir)
			path := filepath.Join(dir, "me.md")
			switch failure {
			case "oversized":
				if err := os.WriteFile(path, []byte(strings.Repeat("#", MaxMeInstructionBytes+1)), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "invalid-utf8":
				if err := os.WriteFile(path, []byte("rule\xff"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "bad-home":
				if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("WHIP_HOME", path)
			}
			if text, err := LoadMeInstructions(); err == nil || text != "" {
				t.Fatalf("invalid standing rules must return an error and no partial text: %q, %v", text, err)
			}
		})
	}
}

func TestLoadMeInstructionsAcceptsExactLimitWithoutClipping(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	content := strings.Repeat("x", MaxMeInstructionBytes-4) + "TAIL"
	if err := os.WriteFile(filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if text, err := LoadMeInstructions(); err != nil || text != content {
		t.Fatalf("exact-boundary standing rules must remain complete: %v, len %d", err, len(text))
	}
}
