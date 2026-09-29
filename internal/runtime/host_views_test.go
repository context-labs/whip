package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func hostSkill(t *testing.T, path, name, description string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: "+description+"\ndisable-model-invocation: true\n---\nSECRET_BODY_NOT_METADATA"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHostViewsRootlessSkillsUseExplicitRootsAndFreshConfiguration(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	cwd, global := t.TempDir(), t.TempDir()
	hostSkill(t, filepath.Join(global, "duplicate", "SKILL.md"), "duplicate", "host winner")
	hostSkill(t, filepath.Join(cwd, ".agents", "skills", "duplicate", "SKILL.md"), "duplicate", "workspace winner")
	hostSkill(t, filepath.Join(cwd, ".agents", "skills", "local", "SKILL.md"), "local", "project only")
	definition, err := r.store.RegisterDefinition(t.Context(), session.DefinitionDocument{ID: "preview", Name: "Preview", Defaults: session.ConfigPatch{Instructions: &session.Instructions{DiscoverSkills: true, SkillRoots: []string{"global"}}}})
	if err != nil {
		t.Fatal(err)
	}
	host := config.Default()
	host.SkillRoots = map[string]string{"global": global}
	if err := config.Save(r.directory, host); err != nil {
		t.Fatal(err)
	}
	globalResult, err := r.CompleteHostSkills(t.Context(), HostSkillsRequest{Scope: "global", Definition: &definition.Ref, Limit: 1024})
	if err != nil || len(globalResult.Candidates) != 1 || globalResult.Candidates[0].Description != "host winner" {
		t.Fatal(globalResult, err)
	}
	project, err := r.CompleteHostSkills(t.Context(), HostSkillsRequest{Scope: "project", CWD: cwd, Definition: &definition.Ref, Limit: 1024})
	if err != nil || len(project.Candidates) != 2 || project.Candidates[0].Description != "workspace winner" {
		t.Fatal(project, err)
	}
	limited, err := r.CompleteHostSkills(t.Context(), HostSkillsRequest{Scope: "project", CWD: cwd, Definition: &definition.Ref, Limit: 1})
	if err != nil || !limited.Truncated {
		t.Fatal(limited, err)
	}
	if roots, err := r.Trees(t.Context(), store.TreeList{Limit: 10}); err != nil || len(roots.Items) != 0 || len(r.kernels) != 0 {
		t.Fatal("preview admitted session", roots, err)
	}
	fresh := t.TempDir()
	hostSkill(t, filepath.Join(fresh, "fresh", "SKILL.md"), "fresh", "new host publication")
	host.SkillRoots["global"] = fresh
	if err := config.Save(r.directory, host); err != nil {
		t.Fatal(err)
	}
	current, err := r.CompleteHostSkills(t.Context(), HostSkillsRequest{Scope: "global", Definition: &definition.Ref, Limit: 10})
	if err != nil || len(current.Candidates) != 1 || current.Candidates[0].Text != "$fresh" {
		t.Fatal(current, err)
	}
	for _, p := range []HostSkillsRequest{{Scope: "global", CWD: cwd, Limit: 1}, {Scope: "project", Limit: 1}, {Scope: "global", Limit: 0}, {Scope: "global", Prefix: "\x00", Limit: 1}, {Scope: "project", CWD: filepath.Join(cwd, "missing"), Limit: 1}} {
		if _, err := r.CompleteHostSkills(t.Context(), p); err == nil {
			t.Fatal("accepted", p)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.CompleteHostSkills(ctx, HostSkillsRequest{Scope: "global", Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestHostViewsThemesUseDedicatedNamespaceAndNeverWriteImports(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	old := t.TempDir()
	t.Setenv("WHIPCODE_HOME", old)
	if err := os.MkdirAll(filepath.Join(r.directory, "themes"), 0o700); err != nil {
		t.Fatal(err)
	}
	custom := `{"name":"new-runtime","dark":true,"palette":{"primary":"#abcdef"}}`
	if err := os.WriteFile(filepath.Join(r.directory, "themes", "custom.json"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := r.HostThemes(t.Context())
	if err != nil || len(catalog.Errors) != 0 {
		t.Fatal(catalog, err)
	}
	resolved, err := r.ResolveHostTheme(t.Context(), "new-runtime", "")
	if err != nil || resolved.Colors.Primary != "#abcdef" {
		t.Fatal(resolved, err)
	}
	imported, err := r.ResolveHostTheme(t.Context(), "", strings.ReplaceAll(custom, "new-runtime", "imported"))
	if err != nil || imported.ID != "imported" {
		t.Fatal(imported, err)
	}
	if files, err := os.ReadDir(filepath.Join(r.directory, "themes")); err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	for _, request := range [][2]string{{"", ""}, {"../host.json", ""}, {"x", custom}, {"", "not JSON"}} {
		if _, err := r.ResolveHostTheme(t.Context(), request[0], request[1]); err == nil {
			t.Fatal(request)
		}
	}
}

func TestHostViewsRuntimeCloseJoinsDialog(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "started")
	name := "osascript"
	if goruntime.GOOS == "linux" {
		name = "zenity"
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\nprintf started > '"+marker+"'\n/bin/sleep 30 & wait\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	r := openTest(t, t.TempDir(), model.Scripted{})
	done := make(chan error, 1)
	go func() { _, err := r.PickHostDirectory(t.Context(), ""); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dialog did not start")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("runtime close left dialog group running")
	}
	if err := <-done; err == nil {
		t.Fatal("terminated dialog reported success")
	}
}
