package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(content), 0o644)
}

func TestDefaultDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	project := t.TempDir()
	t.Chdir(project)

	want := []string{
		filepath.Join(project, ".agents", "skills"),
		filepath.Join(home, ".whip", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	if got := DefaultDirs(); !slices.Equal(got, want) {
		t.Fatalf("DefaultDirs() = %q, want %q", got, want)
	}

	writeSkill(
		t,
		filepath.Join(home, ".agents", "skills"),
		"global-agent",
		"---\nname: global-agent\ndescription: user-level agent skill\n---\n",
	)
	for _, skill := range Scan(DefaultDirs()...) {
		if skill.Name == "global-agent" {
			return
		}
	}
	t.Fatal("global-agent was not scanned from ~/.agents/skills")
}

func TestScanAndPromptBlock(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "go-style", "---\nname: go-style\ndescription: \"Go style rules\"\n---\nbody")
	writeSkill(t, dir, "unnamed", "---\ndescription: 'no name here'\n---\n")
	writeSkill(t, dir, "no-frontmatter", "just a plain file")
	os.MkdirAll(filepath.Join(dir, "empty-dir"), 0o755) // no SKILL.md

	sk := Scan(dir, filepath.Join(dir, "does-not-exist"))
	if len(sk) != 2 {
		t.Fatalf("expected 2 skills, got %+v", sk)
	}
	byName := map[string]Skill{}
	for _, s := range sk {
		byName[s.Name] = s
	}
	if byName["go-style"].Description != "Go style rules" {
		t.Fatalf("quoted description: %+v", byName["go-style"])
	}
	if _, ok := byName["unnamed"]; !ok { // falls back to dir name
		t.Fatalf("dir-name fallback missing: %+v", sk)
	}

	block := PromptBlock(sk)
	if !strings.Contains(block, "<available_skills>") || !strings.Contains(block, "<name>go-style</name>") || !strings.Contains(block, "<description>Go style rules</description>") {
		t.Fatalf("prompt block: %q", block)
	}
	if PromptBlock(nil) != "" {
		t.Fatal("empty scan must produce no block")
	}

	// descriptions up to the spec's 1024 pass through intact (no truncation —
	// the spec limit is a validity ceiling, not a prompt budget)
	long := Skill{Name: "x", Description: strings.Repeat("d", 400), Path: "p"}
	if b := PromptBlock([]Skill{long}); !strings.Contains(b, strings.Repeat("d", 400)) {
		t.Fatalf("spec-legal description must not be truncated")
	}
}

// claude-code's skill tooling writes long descriptions as YAML folded block
// scalars (description: >- followed by indented lines). Before block-scalar
// support, the catalog rendered the indicator itself as the description —
// the skill loaded but the model saw ">-", so nothing ever triggered it.
func TestBlockScalarDescriptions(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "folded-strip",
		"---\nname: folded-strip\ndescription: >-\n  Explicit-invocation working loop — never\n  auto-triggers; when invoked it replaces the\n  default routing for that task.\n---\nbody")
	writeSkill(t, dir, "folded-clip",
		"---\nname: folded-clip\ndescription: >\n  first line\n  second line\n---\n")
	writeSkill(t, dir, "literal-keep",
		"---\nname: literal-keep\ndescription: |+\n  line one\n  line two\n---\n")
	writeSkill(t, dir, "block-last",
		"---\ndescription: >-\n  nothing follows\n  this block\n---\n")
	writeSkill(t, dir, "after-block",
		"---\ndescription: >-\n  folded text here\ndisable-model-invocation: true\n---\n")

	byName := map[string]Skill{}
	for _, s := range Scan(dir) {
		byName[s.Name] = s
	}

	want := map[string]string{
		"folded-strip": "Explicit-invocation working loop — never auto-triggers; when invoked it replaces the default routing for that task.",
		"folded-clip":  "first line second line",
		"literal-keep": "line one\nline two\n",
		"block-last":   "nothing follows this block",
	}
	for name, desc := range want {
		s, ok := byName[name]
		if !ok {
			t.Fatalf("skill %q not scanned: %+v", name, byName)
		}
		if s.Description != desc {
			t.Errorf("%s description = %q, want %q", name, s.Description, desc)
		}
	}

	// A key following a block scalar is out of scope for the hand parser;
	// pinned so a future real-YAML swap knows the contract changed.
	if _, ok := byName["after-block"]; !ok {
		t.Fatalf("after-block not scanned: %+v", byName)
	}
}
