package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/skills"
)

// writeSkill plants a minimal valid skill at <root>/<name>/SKILL.md.
func writeSkill(t *testing.T, root, name, desc string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSkillsImportDedup pins the core contract: a skill name whip already
// loads (project or user dir) is never overwritten, a name duplicated across
// the foreign sources imports once (codex wins over claude), and a genuinely
// new skill copies into ~/.agents/skills with its contents intact.
func TestSkillsImportDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd := t.TempDir()
	t.Chdir(wd)

	// whip already has "linear" at user level.
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "linear", "whip's copy")
	// codex has "linear" (dup) and "codex-only" (new).
	writeSkill(t, filepath.Join(home, ".codex", "skills"), "linear", "codex copy")
	writeSkill(t, filepath.Join(home, ".codex", "skills"), "codex-only", "only in codex")
	// claude has "linear" (dup), "shared" (dup of codex), and "cloudflare" (new).
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "linear", "claude copy")
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "codex-only", "claude's codex-only")
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "cloudflare", "only in claude")

	if err := skillsCLI([]string{"import"}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(home, ".agents", "skills")

	// The dedup'd name keeps whip's original copy.
	body, err := os.ReadFile(filepath.Join(dest, "linear", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "whip's copy") {
		t.Errorf("linear was overwritten: %q", body)
	}
	// Both new skills arrived.
	for _, name := range []string{"codex-only", "cloudflare"} {
		if _, err := os.Stat(filepath.Join(dest, name, "SKILL.md")); err != nil {
			t.Errorf("%s not imported: %v", name, err)
		}
	}
	// The cross-source dup came from codex (first source), not claude.
	body, err = os.ReadFile(filepath.Join(dest, "codex-only", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "only in codex") {
		t.Errorf("codex-only should be codex's copy, got %q", body)
	}
	// A second run imports nothing — the command is idempotent.
	if err := skillsCLI([]string{"import"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("dest has %d skills after re-import, want 3", len(entries))
	}
}

// TestSkillsImportDryRun: --dry-run writes nothing.
func TestSkillsImportDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "cloudflare", "cf")

	if err := skillsCLI([]string{"import", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills")); !os.IsNotExist(err) {
		t.Errorf("--dry-run created the dest dir: %v", err)
	}
}

// TestSkillsImportNothingToDo: no foreign skills at all is a clean no-op.
func TestSkillsImportNothingToDo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	if err := skillsCLI([]string{"import"}); err != nil {
		t.Fatal(err)
	}
}

// TestSkillsImportContinuesPastFailure: one unreadable skill must not abort
// the rest of the import — the good skill lands, the error names only the
// failed one.
func TestSkillsImportContinuesPastFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	// A good skill and a bad one (unreadable SKILL.md) in claude's dir.
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "good", "fine")
	badDir := filepath.Join(home, ".claude", "skills", "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badFile := filepath.Join(badDir, "SKILL.md")
	if err := os.WriteFile(badFile, []byte("---\nname: bad\ndescription: x\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A subdirectory with an unreadable file inside the otherwise-parseable
	// skill: Scan succeeds, copyFile fails.
	sub := filepath.Join(badDir, "refs")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(sub, "secret.md")
	if err := os.WriteFile(unreadable, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o600) }) // let TempDir clean up
	if _, err := os.Open(unreadable); err == nil {
		t.Skip("running with permission to read 0o000 files (root)")
	}

	err := skillsCLI([]string{"import"})
	if err == nil {
		t.Fatal("import should report the failed skill")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error should name the failed skill, got %v", err)
	}
	// The good skill still imported.
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("good skill not imported despite the failure: %v", err)
	}
	// The bad skill must not linger as a half-copied dir.
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "bad")); err == nil {
		// copyDir creates the dest dir before failing — that's fine as long as
		// it's empty (no partial SKILL.md shipped).
		entries, _ := os.ReadDir(filepath.Join(home, ".agents", "skills", "bad"))
		for _, e := range entries {
			if e.Name() == "SKILL.md" {
				t.Error("partial import: bad skill's SKILL.md copied despite the failure")
			}
		}
	}
}

// TestSkillsListCLI: `whip skills list` renders loaded skills and their
// source dirs without error.
func TestSkillsListCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd := t.TempDir()
	t.Chdir(wd)
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "linear", "whip's copy")
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "cloudflare", "claude copy")

	if err := skillsCLI([]string{"list"}); err != nil {
		t.Fatal(err)
	}
}

// TestSkillsCLIUnknownSubcommand: an unknown subcommand errors clearly.
func TestSkillsCLIUnknownSubcommand(t *testing.T) {
	if err := skillsCLI([]string{"nope"}); err == nil {
		t.Error("expected error for unknown subcommand")
	}
}

// TestSkillsForeignDirs: the foreign skill dirs point at codex and claude.
func TestSkillsForeignDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dirs := skills.ForeignDirs()
	if len(dirs) != 2 {
		t.Fatalf("got %d foreign dirs, want 2", len(dirs))
	}
	if !strings.HasSuffix(dirs[0], ".codex/skills") {
		t.Errorf("first foreign dir = %q, want codex", dirs[0])
	}
	if !strings.HasSuffix(dirs[1], ".claude/skills") {
		t.Errorf("second foreign dir = %q, want claude", dirs[1])
	}
}

// TestCopyDirErrorPaths: copyDir fails on a non-directory source and a
// destination that already exists.
func TestCopyDirErrorPaths(t *testing.T) {
	src := filepath.Join(t.TempDir(), "not-a-dir")
	if err := copyDir(src, filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Error("copyDir of a non-directory should fail")
	}
	dst := filepath.Join(t.TempDir(), "exists")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(t.TempDir(), dst); err == nil {
		t.Error("copyDir to an existing destination should fail")
	}
}

// TestCopyFileErrorPaths: copyFile fails on a missing source.
func TestCopyFileErrorPaths(t *testing.T) {
	if err := copyFile(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Error("copyFile of a missing source should fail")
	}
}

// TestSkillsImportPathTraversal: a skill whose frontmatter name contains path
// separators (../, ../../pwned) must be rejected — the name becomes the
// destination directory, and without the guard it escapes ~/.agents/skills.
func TestSkillsImportPathTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	// Plant a malicious skill with a traversal name in claude's dir.
	malDir := filepath.Join(home, ".claude", "skills", "pwn")
	if err := os.MkdirAll(malDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malDir, "SKILL.md"), []byte("---\nname: ../../pwned\ndescription: x\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// And a good one to prove the import continues past the rejected name.
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "good", "fine")

	err := skillsCLI([]string{"import"})
	if err == nil {
		t.Fatal("import should reject the traversal name")
	}
	if !strings.Contains(err.Error(), "../../pwned") {
		t.Errorf("error should name the rejected skill, got %v", err)
	}
	// The traversal destination must not exist (no escape from ~/.agents/skills).
	if _, err := os.Stat(filepath.Join(home, "pwned")); !os.IsNotExist(err) {
		t.Errorf("path traversal succeeded: %v", err)
	}
	// The good skill still imported.
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("good skill not imported despite the traversal rejection: %v", err)
	}
}
