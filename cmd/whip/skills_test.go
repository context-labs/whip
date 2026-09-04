package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
