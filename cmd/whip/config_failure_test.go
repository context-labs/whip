package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/hostcmd"
)

func TestDistributionMalformedConfigIsNeverReplacedByCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), home)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Directory, "host.json")
	broken := []byte(`{"providers": unfinished user edit`)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	// The production launcher reexecutes os.Executable. In a test binary that
	// would rerun this suite instead of entering main. Run the actual native
	// composition root directly, with the same malformed private host file.
	previous := connectNativeRuntime
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) {
		return nil, hostcmd.Run(ctx, []string{"-directory", paths.Directory}, io.Discard, io.Discard)
	}
	t.Cleanup(func() { connectNativeRuntime = previous })
	for _, command := range []struct {
		name string
		run  func() error
	}{
		{"acp", func() error { return acpCLI(nil) }},
		{"mcp list", func() error { return mcpCLI([]string{"list"}, version) }},
		{"mcp test", func() error { return mcpCLI([]string{"test", "fixture"}, version) }},
		{"mcp import", func() error { return mcpCLI([]string{"import"}, version) }},
		{"runtime", func() error { return nativeRuntimeCLI([]string{"-directory", paths.Directory}) }},
	} {
		t.Run(command.name, func(t *testing.T) {
			if err := command.run(); err == nil || !strings.Contains(err.Error(), "decode host configuration") {
				t.Fatal("command did not report the malformed native configuration", err)
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(broken) {
				t.Fatalf("command replaced user's malformed configuration: %q, %v", after, err)
			}
		})
	}
}

func TestNativeRunMalformedConfigurationIsNeverReplaced(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "whip-config-") //nolint:usetesting // Unix socket paths must fit macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv(buildinfo.Env("HOME"), home)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Directory, "host.json")
	broken := []byte(`{"providers": unfinished user edit`)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := connectNativeRuntime
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) {
		return nil, hostcmd.Run(ctx, []string{"-directory", paths.Directory}, io.Discard, io.Discard)
	}
	t.Cleanup(func() { connectNativeRuntime = previous })
	if _, err := runCapture(t, "", "-timeout", "1s", "hello"); err == nil {
		t.Fatal("native command accepted malformed configuration")
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(broken) {
		t.Fatal("malformed host file changed", err)
	}
}
