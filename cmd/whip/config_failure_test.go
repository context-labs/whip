package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionMalformedConfigIsNeverReplacedByCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), home)
	path := filepath.Join(home, "config.json")
	broken := []byte(`{"providers": unfinished user edit`)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []struct {
		name string
		run  func() error
	}{
		{"acp", func() error { return acpCLI(nil) }},
		{"mcp list", func() error { return mcpCLI([]string{"list"}, version) }},
		{"mcp test", func() error { return mcpCLI([]string{"test", "fixture"}, version) }},
		{"mcp import", func() error { return mcpCLI([]string{"import"}, version) }},
		{"daemon", func() error { return runDaemon(t.Context(), nil) }},
		{"run", func() error { _, err := runCapture(t, "", "hello"); return err }},
	} {
		t.Run(command.name, func(t *testing.T) {
			if err := command.run(); err == nil {
				t.Fatal("command accepted malformed configuration")
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(broken) {
				t.Fatalf("command replaced user's malformed configuration: %q, %v", after, err)
			}
		})
	}
}
