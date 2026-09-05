package rlm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
)

func promptFixture(t *testing.T) PromptOptions {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", filepath.Join(home, "whip"))
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return PromptOptions{
		WorkingDirectory: cwd, Now: time.Date(2026, 9, 5, 12, 34, 56, 0, time.FixedZone("PDT", -7*60*60)),
		Platform: "fixture-platform", Username: "fixture-user",
	}
}

func writePromptFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestComposePromptIncludesActualEnvironmentAndOrderedSources(t *testing.T) {
	options := promptFixture(t)
	root := options.WorkingDirectory
	options.WorkingDirectory = filepath.Join(root, "nested")
	options.ProjectRoots = []string{options.WorkingDirectory, root}
	options.Identity = Identity{AgentID: "child-id", Name: "worker", ParentID: "root-id", ParentName: "root", Depth: 1, Report: "inline"}
	files := []struct{ path, content string }{
		{filepath.Join(root, "CLAUDE.md"), "ROOT_CLAUDE_MARKER"},
		{filepath.Join(root, "AGENTS.md"), "# A real heading\nROOT_AGENTS_MARKER"},
		{filepath.Join(options.WorkingDirectory, "CLAUDE.md"), "NESTED_CLAUDE_MARKER"},
		{filepath.Join(options.WorkingDirectory, "AGENTS.md"), "NESTED_AGENTS_MARKER"},
		{filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), "# OMIT_COMMENT\n\n STANDING_USER_MARKER \n"},
	}
	for _, file := range files {
		writePromptFile(t, file.path, file.content)
	}
	snapshot, err := ComposePrompt(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"rlm_exec", "fixture-platform", "fixture-user", "Sat Sep 5, 2026 12:34:56 PDT", "child-id", "root-id", "4 KiB", "Git hygiene", "# A real heading", "Explicit user instructions are authoritative", "AGENTS.md takes precedence over CLAUDE.md"} {
		if !strings.Contains(snapshot.Prompt, marker) {
			t.Errorf("prompt missing %q", marker)
		}
	}
	previous := -1
	for _, marker := range []string{"ROOT_CLAUDE_MARKER", "ROOT_AGENTS_MARKER", "NESTED_CLAUDE_MARKER", "NESTED_AGENTS_MARKER", "STANDING_USER_MARKER"} {
		position := strings.Index(snapshot.Prompt, marker)
		if position <= previous {
			t.Fatalf("precedence order wrong for %s: %d after %d", marker, position, previous)
		}
		previous = position
	}
	if strings.Contains(snapshot.Prompt, "OMIT_COMMENT") {
		t.Fatal("standing instruction comments were included")
	}
	if snapshot.WorkingDirectory != options.WorkingDirectory || !snapshot.AppliedAt.Equal(options.Now) {
		t.Fatalf("snapshot environment mismatch: %#v", snapshot)
	}
	var projectSources []PromptSource
	for _, source := range snapshot.Sources {
		if source.Kind == "project_instructions" {
			projectSources = append(projectSources, source)
		}
	}
	if len(projectSources) != 4 {
		t.Fatalf("applied project sources = %#v", projectSources)
	}
	for i, source := range projectSources {
		if source.Path != files[i].path || source.Scope != filepath.Dir(files[i].path) || source.Bytes != len(files[i].content) {
			t.Errorf("source %d = %#v, expected %+v", i, source, files[i])
		}
	}
}

func TestComposePromptRefreshesStandingRulesAndWorkingDirectory(t *testing.T) {
	options := promptFixture(t)
	firstCWD := options.WorkingDirectory
	secondCWD, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	me := filepath.Join(os.Getenv("WHIP_HOME"), "me.md")
	writePromptFile(t, me, "BEFORE_USER_EDIT")
	writePromptFile(t, filepath.Join(firstCWD, "AGENTS.md"), "FIRST_PROJECT_RULE")
	writePromptFile(t, filepath.Join(secondCWD, "AGENTS.md"), "SECOND_PROJECT_RULE")
	first, err := ComposePrompt(options)
	if err != nil {
		t.Fatal(err)
	}
	writePromptFile(t, me, "AFTER_USER_EDIT")
	options.WorkingDirectory = secondCWD
	options.ProjectRoots = []string{firstCWD}
	second, err := ComposePrompt(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"BEFORE_USER_EDIT", "FIRST_PROJECT_RULE", firstCWD} {
		if !strings.Contains(first.Prompt, marker) || strings.Contains(second.Prompt, marker) {
			t.Errorf("old environment leaked into refreshed prompt: %q", marker)
		}
	}
	for _, marker := range []string{"AFTER_USER_EDIT", "SECOND_PROJECT_RULE", secondCWD} {
		if !strings.Contains(second.Prompt, marker) || strings.Contains(first.Prompt, marker) {
			t.Errorf("new environment not isolated to refreshed prompt: %q", marker)
		}
	}
}

func TestComposePromptLoadsOnlyAuthorizedAncestorChain(t *testing.T) {
	options := promptFixture(t)
	outside := options.WorkingDirectory
	boundary := filepath.Join(outside, "project")
	options.WorkingDirectory = filepath.Join(boundary, "child")
	options.ProjectRoots = []string{boundary, filepath.Join(outside, "sibling")}
	writePromptFile(t, filepath.Join(outside, "AGENTS.md"), strings.Repeat("X", maxProjectInstructionBytes+1))
	writePromptFile(t, filepath.Join(boundary, "AGENTS.md"), "INHERITED_RULE")
	writePromptFile(t, filepath.Join(options.WorkingDirectory, "AGENTS.md"), "CHILD_RULE")
	writePromptFile(t, filepath.Join(outside, "sibling", "AGENTS.md"), strings.Repeat("X", maxProjectInstructionBytes+1))
	writePromptFile(t, filepath.Join(options.WorkingDirectory, "unentered", "AGENTS.md"), strings.Repeat("X", maxProjectInstructionBytes+1))
	snapshot, err := ComposePrompt(options)
	if err != nil {
		t.Fatalf("unrelated rules should not be read: %v", err)
	}
	if !strings.Contains(snapshot.Prompt, "INHERITED_RULE") || !strings.Contains(snapshot.Prompt, "CHILD_RULE") {
		t.Fatal("child lost applicable inherited instructions")
	}
	options.ProjectRoots = nil
	snapshot, err = ComposePrompt(options)
	if err != nil || strings.Contains(snapshot.Prompt, "INHERITED_RULE") {
		t.Fatalf("empty ancestor authorization must remain at cwd: %v", err)
	}
	options.ProjectRoots = []string{outside}
	if snapshot, err := ComposePrompt(options); err == nil || snapshot.Prompt != "" || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("authorized oversized rule must fail explicitly: %v", err)
	}
}

func TestComposePromptSkillsReflectScopedCatalogAndExplicitEntries(t *testing.T) {
	options := promptFixture(t)
	root := options.WorkingDirectory
	options.WorkingDirectory = filepath.Join(root, "child")
	options.ProjectRoots = []string{root}
	if err := os.MkdirAll(options.WorkingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	skills := []struct{ path, description string }{
		{filepath.Join(options.WorkingDirectory, ".agents", "skills", "local", "SKILL.md"), "LOCAL_CATALOG_MARKER"},
		{filepath.Join(root, ".agents", "skills", "ancestor", "SKILL.md"), "ANCESTOR_CATALOG_MARKER"},
		{filepath.Join(os.Getenv("WHIP_HOME"), "skills", "user", "SKILL.md"), "USER_CATALOG_MARKER"},
		{filepath.Join(os.Getenv("HOME"), ".agents", "skills", "shared", "SKILL.md"), "SHARED_CATALOG_MARKER"},
	}
	for _, skill := range skills {
		writePromptFile(t, skill.path, "---\ndescription: "+skill.description+"\n---\nBODY_NOT_IN_CATALOG")
	}
	disabledPath := filepath.Join(root, ".agents", "skills", "explicit", "SKILL.md")
	writePromptFile(t, disabledPath, "---\nname: explicit\ndescription: EXPLICIT_ONLY_MARKER\ndisable-model-invocation: true\n---\n")
	snapshot, err := ComposePrompt(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		if !strings.Contains(snapshot.Prompt, skill.description) || !strings.Contains(snapshot.Prompt, skill.path) {
			t.Errorf("catalog missing %s", skill.path)
		}
	}
	if strings.Contains(snapshot.Prompt, "EXPLICIT_ONLY_MARKER") || strings.Contains(snapshot.Prompt, "BODY_NOT_IN_CATALOG") {
		t.Fatal("invisible skill or body leaked into visible catalog")
	}
	if len(snapshot.Skills) != 5 || snapshot.Skills[0].Name != "local" {
		t.Fatalf("catalog does not preserve local-first discovery or explicit entries: %#v", snapshot.Skills)
	}
	var count int
	for _, source := range snapshot.Sources {
		if source.Kind == "skill" {
			count++
			if source.Path == disabledPath {
				t.Fatal("audit claimed an explicitly invokable-only skill was supplied")
			}
		}
	}
	if count != 4 {
		t.Fatalf("visible skill source count = %d", count)
	}
	options.SkillDirs = []string{}
	snapshot, err = ComposePrompt(options)
	if err != nil || len(snapshot.Skills) != 0 || strings.Contains(snapshot.Prompt, "CATALOG_MARKER") {
		t.Fatalf("explicit empty skill directories must disable catalog reads: %v", err)
	}
}

func TestComposePromptRejectsIncompleteApplicableRules(t *testing.T) {
	for _, failure := range []string{"oversized", "directory", "invalid-utf8", "escaping-symlink", "broken-symlink", "standing-oversized", "standing-directory", "bad-config-home", "bad-cwd", "skill-frontmatter"} {
		t.Run(failure, func(t *testing.T) {
			options := promptFixture(t)
			path := filepath.Join(options.WorkingDirectory, "AGENTS.md")
			switch failure {
			case "oversized":
				writePromptFile(t, path, strings.Repeat("x", maxProjectInstructionBytes+1))
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "invalid-utf8":
				writePromptFile(t, path, "constraint\xff")
			case "escaping-symlink":
				outside := filepath.Join(t.TempDir(), "AGENTS.md")
				writePromptFile(t, outside, "OUTSIDE_AUTHORIZED_ROOT")
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "broken-symlink":
				if err := os.Symlink(filepath.Join(options.WorkingDirectory, "missing"), path); err != nil {
					t.Fatal(err)
				}
			case "standing-oversized":
				writePromptFile(t, filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), strings.Repeat("x", config.MaxMeInstructionBytes+1))
			case "standing-directory":
				if err := os.MkdirAll(filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "bad-config-home":
				writePromptFile(t, os.Getenv("WHIP_HOME"), "not a directory")
			case "bad-cwd":
				options.WorkingDirectory = filepath.Join(options.WorkingDirectory, "missing")
			case "skill-frontmatter":
				writePromptFile(t, filepath.Join(options.WorkingDirectory, ".agents", "skills", "broken", "SKILL.md"), "missing frontmatter")
			}
			snapshot, err := ComposePrompt(options)
			if err == nil || snapshot.Prompt != "" {
				t.Fatalf("must reject partial environment, got error %v, prompt bytes %d", err, len(snapshot.Prompt))
			}
		})
	}
}

func TestComposePromptMissingOptionalFilesAndExactBoundary(t *testing.T) {
	options := promptFixture(t)
	snapshot, err := ComposePrompt(options)
	if err != nil || snapshot.Prompt == "" || len(snapshot.Skills) != 0 {
		t.Fatalf("optional instructions must not be required: %v", err)
	}
	path := filepath.Join(options.WorkingDirectory, "AGENTS.md")
	content := strings.Repeat("x", maxProjectInstructionBytes-6) + "TAIL!!"
	writePromptFile(t, path, content)
	snapshot, err = ComposePrompt(options)
	if err != nil || !strings.Contains(snapshot.Prompt, content) {
		t.Fatalf("exact-boundary instruction must be complete: %v", err)
	}
	writePromptFile(t, path, content+"x")
	if snapshot, err := ComposePrompt(options); err == nil || snapshot.Prompt != "" {
		t.Fatalf("oversized source must not be clipped: %v", err)
	}
}

func TestComposePromptRejectsAggregateRuleOverflow(t *testing.T) {
	options := promptFixture(t)
	root := options.WorkingDirectory
	for i := 0; i < 17; i++ {
		writePromptFile(t, filepath.Join(options.WorkingDirectory, "AGENTS.md"), strings.Repeat("x", maxProjectInstructionBytes))
		options.WorkingDirectory = filepath.Join(options.WorkingDirectory, "child")
	}
	if err := os.MkdirAll(options.WorkingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	options.ProjectRoots = []string{root}
	snapshot, err := ComposePrompt(options)
	if err == nil || snapshot.Prompt != "" || !strings.Contains(err.Error(), "environment prompt exceeds") {
		t.Fatalf("aggregate source limit must fail without a clipped prompt: %v", err)
	}
}

func TestComposePromptCanonicalAliasesKeepAuthorizedAncestorRules(t *testing.T) {
	options := promptFixture(t)
	root := options.WorkingDirectory
	child := filepath.Join(root, "child")
	writePromptFile(t, filepath.Join(root, "AGENTS.md"), "CANONICAL_ANCESTOR_RULE")
	writePromptFile(t, filepath.Join(child, "AGENTS.md"), "CANONICAL_CHILD_RULE")
	alias := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	options.WorkingDirectory = alias
	options.ProjectRoots = []string{root}
	snapshot, err := ComposePrompt(options)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.WorkingDirectory != child || !strings.Contains(snapshot.Prompt, "CANONICAL_ANCESTOR_RULE") || !strings.Contains(snapshot.Prompt, "CANONICAL_CHILD_RULE") {
		t.Fatalf("symlink cwd lost applicable canonical ancestors: cwd=%q, sources=%+v", snapshot.WorkingDirectory, snapshot.Sources)
	}
}
