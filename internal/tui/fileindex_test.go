package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// chdir points the file index at a fresh temp tree (the package test binary
// runs with the package dir as cwd, so completions must not follow it) and
// resets the shared index so each test builds its own listing.
func chdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := currentRoot
	currentRoot = func() (string, error) { return dir, nil }
	t.Cleanup(func() { currentRoot = old })
	fileIndex.Lock()
	fileIndex.root, fileIndex.files, fileIndex.builtAt = "", nil, time.Time{}
	fileIndex.Unlock()
	return dir
}

func write(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFuzzyFiles(t *testing.T) {
	dir := chdir(t)
	write(t, dir, "docs/roadmap.md")
	write(t, dir, "internal/tui/roadmap_notes.txt")
	write(t, dir, "README.md")
	write(t, dir, "cmd/whip/main.go")

	// bare word finds a nested file
	hits := fuzzyFiles("roadmap", 8)
	if len(hits) != 2 || hits[0] != "docs/roadmap.md" {
		t.Fatalf("roadmap: %v", hits)
	}
	// base-name substring beats full-path match
	hits = fuzzyFiles("main", 8)
	if len(hits) != 1 || hits[0] != "cmd/whip/main.go" {
		t.Fatalf("main: %v", hits)
	}
	// subsequence match
	hits = fuzzyFiles("rdmp", 8)
	if len(hits) != 2 {
		t.Fatalf("rdmp subsequence: %v", hits)
	}
	// empty query lists everything, sorted
	hits = fuzzyFiles("", 8)
	if len(hits) != 4 || hits[0] != "README.md" {
		t.Fatalf("empty: %v", hits)
	}
	// no match
	if hits = fuzzyFiles("zzz", 8); len(hits) != 0 {
		t.Fatalf("zzz: %v", hits)
	}
	// hidden and vendor dirs are skipped
	write(t, dir, ".git/config")
	write(t, dir, "vendor/pkg/mod.go")
	fileIndex.Lock()
	fileIndex.builtAt = time.Time{} // force a rebuild
	fileIndex.Unlock()
	if hits = fuzzyFiles("", 32); len(hits) != 4 {
		t.Fatalf("hidden/vendor should be skipped: %v", hits)
	}
}

func TestAtMentionFuzzyCompletion(t *testing.T) {
	dir := chdir(t)
	write(t, dir, "docs/roadmap.md")
	write(t, dir, "alpha.txt")

	_, cs := completions("fix @road", nil, nil, nil, nil)
	if len(cs) != 1 || cs[0].Text != "@docs/roadmap.md" {
		t.Fatalf("fuzzy @ completion: %v", texts(cs))
	}
	// path-like queries still glob
	_, cs = completions("fix @al", nil, nil, nil, nil)
	if len(cs) != 1 || cs[0].Text != "@alpha.txt" {
		t.Fatalf("glob @ completion: %v", texts(cs))
	}
	_, cs = completions("fix @docs/r", nil, nil, nil, nil)
	if len(cs) != 1 || cs[0].Text != "@docs/roadmap.md" {
		t.Fatalf("slash query: %v", texts(cs))
	}
}

// The completion path must work from a package test binary too: the mention
// root is the model's working dir, not os.Getwd of the test process.
func TestCompletionUsesRootNotTestCwd(t *testing.T) {
	dir := chdir(t)
	write(t, dir, "docs/roadmap.md")
	// the real cwd (package dir) also has files; they must not leak in
	_, cs := completions("fix @roadmap", nil, nil, nil, nil)
	if len(cs) != 1 || cs[0].Text != "@docs/roadmap.md" {
		t.Fatalf("rooted completion: %v", texts(cs))
	}
}
