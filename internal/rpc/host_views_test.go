package rpc_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestHostViewsRPCRootlessContracts(t *testing.T) {
	_, c := fixture(t)
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	directories := call[protocol.HostDirectoriesResult](t, c, "host.directories.list", protocol.HostDirectoriesParams{Path: cwd, Limit: 1})
	if len(directories.Entries) != 1 || directories.Entries[0].Name != "child" || directories.HasMore {
		t.Fatal(directories)
	}
	created := call[protocol.HostDirectoryCreateResult](t, c, "host.directory.create", protocol.HostDirectoryCreateParams{Parent: cwd, Name: "new project"})
	if created.Path != filepath.Join(cwd, "new project") {
		t.Fatal(created)
	}
	if info, err := os.Stat(created.Path); err != nil || !info.IsDir() {
		t.Fatalf("created folder = %v, %v", info, err)
	}
	if trees := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Limit: 1}); len(trees.Items) != 0 {
		t.Fatal("human directory creation created a session")
	}
	skills := call[protocol.HostSkillsResult](t, c, "host.skills.complete", protocol.HostSkillsParams{Scope: "project", CWD: cwd, Limit: 32})
	if skills.Candidates == nil || len(skills.Candidates) != 0 {
		t.Fatal(skills)
	}
	catalog := call[protocol.HostThemesResult](t, c, "host.themes.list", protocol.EmptyParams{})
	if len(catalog.Themes) == 0 || len(catalog.Errors) != 0 {
		t.Fatal(catalog)
	}
	for _, item := range catalog.Themes {
		resolved := call[protocol.HostThemeResolved](t, c, "host.themes.resolve", protocol.HostThemeResolveParams{Name: item.ID})
		if resolved.ID != item.ID || len(resolved.Colors.Primary) != 7 || resolved.Code.Tokens == nil {
			t.Fatal(resolved)
		}
	}
	imported := call[protocol.HostThemeResolved](t, c, "host.themes.resolve", protocol.HostThemeResolveParams{JSON: `{"name":"imported","dark":true,"palette":{"primary":"#123456"}}`})
	if imported.Colors.Primary != "#123456" {
		t.Fatal(imported)
	}
	for _, test := range []struct {
		method string
		params any
	}{
		{"host.themes.resolve", protocol.HostThemeResolveParams{}},
		{"host.themes.resolve", protocol.HostThemeResolveParams{Name: "dark", JSON: "{}"}},
		{"host.directory.pick", protocol.HostDirectoryPickParams{Start: "relative"}},
		{"host.skills.complete", protocol.HostSkillsParams{Scope: "global", CWD: cwd, Limit: 1}},
		{"host.directories.list", []string{"invalid"}},
	} {
		err := c.Call(t.Context(), test.method, test.params, new(any))
		var failure *client.Error
		if err == nil || (errors.As(err, &failure) && failure.Kind != "INVALID") {
			t.Fatalf("%s: %v", test.method, err)
		}
	}
	trees := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Limit: 10})
	if len(trees.Items) != 0 {
		t.Fatal("host preview created roots", trees)
	}
}
