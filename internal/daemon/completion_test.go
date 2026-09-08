package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completionFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"docs/roadmap.md", "internal/tui/roadmap_notes.txt", "README.md", "cmd/whip/main.go", ".git/config", "vendor/pkg/mod.go"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestHostFileCompletionUsesWorkspaceAndRanksFuzzyMatches(t *testing.T) {
	root := completionFixture(t)
	for _, test := range []struct {
		prefix string
		count  int
		first  string
	}{
		{prefix: "roadmap", count: 2, first: "@docs/roadmap.md"},
		{prefix: "rdmp", count: 2, first: "@docs/roadmap.md"},
		{prefix: "main", count: 1, first: "@cmd/whip/main.go"},
		{prefix: "zzz", count: 0},
		{prefix: "docs/r", count: 1, first: "@docs/roadmap.md"},
	} {
		result, err := completeAt(t.Context(), root, CompletionParams{Kind: "mention", Prefix: test.prefix, Limit: 8})
		if err != nil || len(result.Candidates) != test.count {
			t.Fatalf("prefix=%s result=%+v err=%v", test.prefix, result, err)
		}
		if test.count > 0 && result.Candidates[0].Text != test.first {
			t.Fatalf("prefix=%s result=%+v", test.prefix, result)
		}
	}
	files, _, err := completionFileList(t.Context(), root)
	if err != nil || len(files) != 4 {
		t.Fatalf("hidden/vendor indexing=%v err=%v", files, err)
	}
}

func TestHostPathCompletionPreservesAbsoluteAndRelativePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "alphadir"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, absolute := range []bool{false, true} {
		prefix := "al"
		if absolute {
			prefix = filepath.Join(root, prefix)
		}
		result, err := completeAt(t.Context(), root, CompletionParams{Kind: "path", Prefix: prefix, Limit: 8})
		if err != nil || len(result.Candidates) != 2 {
			t.Fatalf("paths=%+v err=%v", result, err)
		}
		expected := "alphadir/"
		if absolute {
			expected = filepath.ToSlash(filepath.Join(root, "alphadir")) + "/"
		}
		if result.Candidates[1].Text != expected || result.Candidates[1].Description != "dir" {
			t.Fatal("directory completion lost slash or origin")
		}
	}
	result, err := completeAt(t.Context(), root, CompletionParams{Kind: "path", Prefix: "al", Limit: 1})
	if err != nil || len(result.Candidates) != 1 || !result.Truncated {
		t.Fatal("completion response was not explicitly bounded")
	}
}

func TestHostSkillCompletionUsesHostWorkspace(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(root, ".agents", "skills", "go-style")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: go-style\ndescription: Go style guidance\n---\nUse Go."), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := completeAt(t.Context(), root, CompletionParams{Kind: "skill", Prefix: "go", Limit: 8})
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Text != "$go-style" || !strings.Contains(result.Candidates[0].Description, "Go style") {
		t.Fatalf("host skills=%+v err=%v", result, err)
	}
}

func TestHostCompletionCancellation(t *testing.T) {
	root := completionFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := completionFileList(ctx, root); err == nil {
		t.Fatal("cancelled file scan continued")
	}
}
