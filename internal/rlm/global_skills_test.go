package rlm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestLoadGlobalPromptSkillsIsolatedRoots(t *testing.T) {
	home, appHome, project := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(buildinfo.Env("HOME"), appHome)
	t.Chdir(project)
	// A cwd/ancestor scan would fail, not merely return an extra candidate.
	for _, directory := range []string{project, filepath.Dir(project)} {
		writePromptFile(t, filepath.Join(directory, ".agents", "skills", "tripwire", "SKILL.md"), "not frontmatter")
	}
	writePromptFile(t, filepath.Join(appHome, "skills", "app", "SKILL.md"),
		"---\nname: duplicate\ndescription: app\n---\nBODY")
	writePromptFile(t, filepath.Join(home, ".agents", "skills", "user", "SKILL.md"),
		"---\nname: duplicate\ndescription: user\ndisable-model-invocation: true\n---\nBODY")
	roster, err := LoadGlobalPromptSkillsContext(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	var descriptions []string
	for _, skill := range roster {
		descriptions = append(descriptions, skill.Description)
	}
	if !reflect.DeepEqual(descriptions, []string{"app", "user"}) || !roster[1].DisableModelInvocation {
		t.Fatalf("wrong global order/metadata: %+v", roster)
	}
	roster, err = LoadGlobalPromptSkillsContext(t.Context(), false)
	if err != nil || len(roster) != 0 {
		t.Fatalf("disabled discovery: %+v, %v", roster, err)
	}
}

func TestLoadGlobalPromptSkillsDistributionHome(t *testing.T) {
	home, foreign, custom := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", "")
	t.Setenv("WHIPCODE_HOME", "")
	other := "WHIPCODE_HOME"
	if buildinfo.Name == "whipcode" {
		other = "WHIP_HOME"
	}
	t.Setenv(other, foreign)
	writePromptFile(t, filepath.Join(foreign, "skills", "tripwire", "SKILL.md"), "not frontmatter")
	writePromptFile(t, filepath.Join(home, "."+buildinfo.Name, "skills", "default", "SKILL.md"),
		"---\nname: default\ndescription: distribution default\n---\nBODY")
	writePromptFile(t, filepath.Join(home, ".agents", "skills", "shared", "SKILL.md"),
		"---\nname: shared\ndescription: OS home\n---\nBODY")
	roster, err := LoadGlobalPromptSkillsContext(t.Context(), true)
	if err != nil || len(roster) != 2 || roster[0].Name != "default" || roster[1].Name != "shared" {
		t.Fatalf("distribution default: %+v, %v", roster, err)
	}
	t.Setenv(buildinfo.Env("HOME"), custom)
	writePromptFile(t, filepath.Join(custom, "skills", "custom", "SKILL.md"),
		"---\nname: custom\ndescription: distribution override\n---\nBODY")
	roster, err = LoadGlobalPromptSkillsContext(t.Context(), true)
	if err != nil || len(roster) != 2 || roster[0].Name != "custom" || roster[1].Name != "shared" {
		t.Fatalf("distribution override changed OS home or retained default: %+v, %v", roster, err)
	}
}

func TestLoadGlobalPromptSkillsMissingMalformedAndCanceled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	appHome := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), appHome)
	roster, err := LoadGlobalPromptSkillsContext(t.Context(), true)
	if err != nil || len(roster) != 0 {
		t.Fatalf("missing optional roots: %+v, %v", roster, err)
	}
	writePromptFile(t, filepath.Join(appHome, "skills", "invalid", "SKILL.md"), "not frontmatter")
	if _, err := LoadGlobalPromptSkillsContext(t.Context(), true); err == nil {
		t.Fatal("malformed global metadata was ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, discovery := range []bool{false, true} {
		if _, err := LoadGlobalPromptSkillsContext(ctx, discovery); !errors.Is(err, context.Canceled) {
			t.Fatalf("discovery=%v: %v", discovery, err)
		}
	}
}

func TestLoadGlobalPromptSkillsPreservesUserSymlinkTrust(t *testing.T) {
	home, appHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(buildinfo.Env("HOME"), appHome)
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	writePromptFile(t, outside, "---\nname: linked\ndescription: trusted user alias\n---\nBODY")
	link := filepath.Join(home, ".agents", "skills", "linked", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	roster, err := LoadGlobalPromptSkillsContext(t.Context(), true)
	if err != nil || len(roster) != 1 || roster[0].Name != "linked" {
		t.Fatalf("trusted global symlink: %+v, %v", roster, err)
	}
}
