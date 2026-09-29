package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/providerhost"
)

func TestMain(m *testing.M) {
	// Gateway process fixtures inherit the already isolated parent home. A
	// SIGKILL fixture cannot run deferred cleanup of a second temporary home.
	if os.Getenv("WHIP_TEST_GATEWAY_HELPER") != "" {
		os.Exit(m.Run())
	}
	code := func() int {
		home, err := os.MkdirTemp("", "whip-cli-test-home-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(home)
		if err := os.Setenv("HOME", home); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.Setenv("WHIPCODE_HOME", filepath.Join(home, "whip")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		// OpenCode discovery prefers XDG_CONFIG_HOME over HOME; keep it off
		// the developer's real files too.
		if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		// A test daemon must never discover the developer's inherited keys.
		names := []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"}
		for _, preset := range providerhost.Presets() {
			names = append(names, preset.Environments...)
		}
		for _, name := range names {
			if err := os.Unsetenv(name); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		return m.Run()
	}()
	os.Exit(code)
}

func invokeMain(t *testing.T, args ...string) string {
	t.Helper()
	previousArgs, previousFlags := os.Args, flag.CommandLine
	previousInput, previousOutput := os.Stdin, os.Stdout
	defer func() {
		os.Args, flag.CommandLine = previousArgs, previousFlags
		os.Stdin, os.Stdout = previousInput, previousOutput
	}()
	os.Args = append([]string{"whipcode"}, args...)
	flag.CommandLine = flag.NewFlagSet("whipcode", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = inW.Close()
	os.Stdin = inR
	defer func() { _ = inR.Close() }()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, outR)
		close(done)
	}()
	main()
	_ = outW.Close()
	<-done
	_ = outR.Close()
	return output.String()
}

func TestMainDispatchesHeadlessCommands(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		if output := invokeMain(t, "-version"); !strings.Contains(output, "whipcode "+version) {
			t.Fatalf("version output = %q", output)
		}
	})
	t.Run("kernel EOF", func(t *testing.T) {
		invokeMain(t, "_kernel")
	})

	t.Run("bench", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WHIPCODE_HOME", home)
		invokeMain(t, "-bench")
		if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
			t.Fatal("read-only benchmark wrote files", entries, err)
		}
		invokeMain(t, "-bench-init")
		if _, err := os.Stat(filepath.Join(home, "runtime-v4", "host.json")); err != nil {
			t.Fatal("explicit benchmark initialization did not publish host.json", err)
		}
	})

	t.Run("browser install", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PATH", t.TempDir())
		if output := invokeMain(t, "browser", "install"); !strings.Contains(output, "Load unpacked") {
			t.Fatalf("browser output = %q", output)
		}
	})

	t.Run("update", func(t *testing.T) {
		home, err := os.MkdirTemp("/tmp", "whip-update-main-") //nolint:usetesting // Native Unix socket paths must fit macOS.
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(home) })
		bin := t.TempDir()
		t.Setenv("WHIPCODE_HOME", home)
		installer := filepath.Join(bin, "sh")
		if err := os.WriteFile(installer, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
		if output := invokeMain(t, "update"); !strings.Contains(output, "whipcode updated") {
			t.Fatalf("update output = %q", output)
		}
	})

	t.Run("run and sessions", func(t *testing.T) {
		runFixture(t, "main reply", nil)
		if output := invokeMain(t, "run", "hello"); !strings.Contains(output, "main reply") {
			t.Fatalf("run output = %q", output)
		}
		if output := invokeMain(t, "sessions"); !strings.Contains(output, "test") {
			t.Fatalf("sessions output = %q", output)
		}
	})
	t.Run("mcp", func(t *testing.T) {
		mcpHome(t, "")
		if output := invokeMain(t, "mcp", "list"); output == "" {
			t.Fatal("mcp list produced no output")
		}
	})
	t.Run("auth", func(t *testing.T) {
		useNativeAuth(t, nil)
		if output := invokeMain(t, "auth", "inference-net", "status"); !strings.Contains(output, "Inference.net") {
			t.Fatalf("auth output = %q", output)
		}
	})
}

// Actual client event loops send original inputs; the native host composes model context.
func TestClientEntryPathsSendAssembledPromptToProvider(t *testing.T) {
	t.Run("acp", testNativeACPPromptContext)
	t.Run("tui", testNativeTUIPromptContext)
}

func writePromptRequestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
