package hostview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func completionFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"docs/roadmap.md", "internal/tui/roadmap_notes.txt", "README.md", "cmd/whip/main.go", ".git/config", "vendor/pkg/mod.go", "node_modules/package/index.js", "nested/.cache/cached.go"} {
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
		result, err := CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "mention", Prefix: test.prefix, Limit: 8})
		if err != nil || len(result.Candidates) != test.count {
			t.Fatalf("prefix=%s result=%+v err=%v", test.prefix, result, err)
		}
		if test.count > 0 && result.Candidates[0].Text != test.first {
			t.Fatalf("prefix=%s result=%+v", test.prefix, result)
		}
	}
	files, _, err := completionFileList(t.Context(), root, time.Now().Add(time.Second))
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
		result, err := CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "path", Prefix: prefix, Limit: 8})
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
	result, err := CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "path", Prefix: "al", Limit: 1})
	if err != nil || len(result.Candidates) != 1 || !result.Truncated {
		t.Fatal("completion response was not explicitly bounded")
	}
}

func TestHostCompletionCancellation(t *testing.T) {
	root := completionFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := completionFileList(ctx, root, time.Now().Add(time.Second)); err == nil {
		t.Fatal("cancelled file scan continued")
	}
}

func TestCompletionExplicitPathsSymlinksAndLiteralBodies(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	t.Setenv("HOME", outside)
	if err := os.Mkdir(filepath.Join(outside, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("BODY_MUST_NOT_APPEAR"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"~/", filepath.ToSlash(outside) + "/", "link/"} {
		result, err := CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "mention", Prefix: prefix, Limit: 64})
		if err != nil || len(result.Candidates) != 2 || result.Candidates[0].Description != "dir" {
			t.Fatal(prefix, result, err)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "BODY_MUST_NOT_APPEAR") {
			t.Fatal("read file contents")
		}
	}
	result, err := CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "mention", Prefix: "secret", Limit: 64})
	if err != nil || len(result.Candidates) != 0 {
		t.Fatal("recursive symlink was followed", result, err)
	}
	result, err = CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "mention", Prefix: "missing/", Limit: 64})
	if err != nil || result.Candidates == nil || len(result.Candidates) != 0 {
		t.Fatal(result, err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "mention", Prefix: "pipe/", Limit: 64}); err == nil {
		t.Fatal("FIFO accepted as directory")
	}
}

func TestCompletionCancellationValidationAndElapsedBudget(t *testing.T) {
	root := completionFixture(t)
	for _, p := range []CompletionParams{{Kind: "mention", Limit: 0}, {Kind: "mention", Limit: 65}, {Kind: "skill", Limit: 1}, {Kind: "path", Prefix: "\x00", Limit: 1}, {Kind: "mention", Prefix: string([]byte{255}), Limit: 1}, {Kind: "mention", Prefix: strings.Repeat("é", 2049), Limit: 1}} {
		if _, err := CompleteWorkspace(t.Context(), root, p); err == nil {
			t.Fatal("accepted", p)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, kind := range []string{"mention", "path"} {
		if _, err := CompleteWorkspace(ctx, root, CompletionParams{Kind: kind, Limit: 64}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	files, truncated, err := completionFileList(t.Context(), root, time.Now().Add(-time.Second))
	if err != nil || !truncated || len(files) != 0 {
		t.Fatal(files, truncated, err)
	}
	result, err := completePaths(t.Context(), root, CompletionParams{Kind: "mention", Limit: 64}, time.Now().Add(-time.Second))
	if err != nil || !result.Truncated {
		t.Fatal(result, err)
	}
}

func TestCompletionEncodedOutputBudgetIsExplicit(t *testing.T) {
	root := t.TempDir()
	directory := root
	for range 4 {
		directory = filepath.Join(directory, strings.Repeat("\t", 200))
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 64 {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("file-%02d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := CompleteWorkspace(t.Context(), root, CompletionParams{Kind: "path", Prefix: filepath.ToSlash(directory) + "/", Limit: 64})
	if err != nil || !result.Truncated || len(result.Candidates) == 0 || len(result.Candidates) >= 64 {
		t.Fatal(len(result.Candidates), result.Truncated, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > completionOutputBytes {
		t.Fatal(len(encoded), err)
	}
}

func TestCompletionIndexStopsAtFileCountBudget(t *testing.T) {
	root := t.TempDir()
	for i := range completionPathCount + 1 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%05d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, truncated, err := completionFileList(t.Context(), root, time.Now().Add(time.Minute))
	if err != nil || !truncated || len(files) != completionPathCount {
		t.Fatal(len(files), truncated, err)
	}
}
