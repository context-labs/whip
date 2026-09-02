package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func invokeMain(t *testing.T, args ...string) string {
	t.Helper()
	previousArgs, previousFlags := os.Args, flag.CommandLine
	previousInput, previousOutput := os.Stdin, os.Stdout
	defer func() {
		os.Args, flag.CommandLine = previousArgs, previousFlags
		os.Stdin, os.Stdout = previousInput, previousOutput
	}()
	os.Args = append([]string{"whip"}, args...)
	flag.CommandLine = flag.NewFlagSet("whip", flag.ContinueOnError)
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

// The system prompt always carries the built-in operating rules (the safety
// rails); ~/.whip/me.md appends the user's standing instructions after them.
func TestSystemPromptAppendsUserMe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	p := systemPrompt(t.TempDir(), time.Now())
	if !strings.Contains(p, "never force-push") {
		t.Fatal("built-in operating rules must always be present")
	}
	if strings.Contains(p, "Standing instructions") {
		t.Fatal("a fresh install (all-comments me.md) appends nothing")
	}

	os.WriteFile(filepath.Join(home, "me.md"), []byte("- Always pnpm, never npm.\n"), 0o644)
	p = systemPrompt(t.TempDir(), time.Now())
	if !strings.Contains(p, "never force-push") {
		t.Fatal("built-in rules survive a user me.md")
	}
	if !strings.Contains(p, "Standing instructions from the user") || !strings.Contains(p, "Always pnpm") {
		t.Fatalf("user instructions should append:\n%s", p)
	}
}

// The env block tells the model where and when it is: the working directory,
// the platform, the current date/time with the local timezone, and the OS
// username — so relative dates ("tomorrow", "last Tuesday") resolve against
// the user's clock, and the model knows who it's working with.
func TestSystemPromptEnvBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	now := time.Date(2026, 8, 28, 19, 21, 11, 0, time.Local)
	p := systemPrompt("/tmp/work", now)

	for _, want := range []string{
		"<env>",
		"Working directory: /tmp/work",
		"Current date/time: Fri Aug 28, 2026 19:21:11",
		"User: ",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("env block should contain %q:\n%s", want, p)
		}
	}
	if !strings.Contains(p, " (UTC") {
		t.Fatalf("date/time should carry a UTC offset:\n%s", p)
	}
}

func TestMainDispatchesHeadlessCommands(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		if output := invokeMain(t, "-version"); !strings.Contains(output, "whip "+version) {
			t.Fatalf("version output = %q", output)
		}
	})
	t.Run("kernel EOF", func(t *testing.T) {
		invokeMain(t, "_kernel")
	})

	t.Run("bench", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WHIP_HOME", home)
		writeConfig(t, home, `{
			"defaultModel":"test",
			"providers":{"testprov":{"baseUrl":"http://127.0.0.1:1","api":"openai-completions","apiKey":"k"}},
			"models":{"test":{"providers":["testprov"],"maxOut":100}}
		}`)
		invokeMain(t, "-bench")
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
		home, bin := t.TempDir(), t.TempDir()
		t.Setenv("WHIP_HOME", home)
		installer := filepath.Join(bin, "sh")
		if err := os.WriteFile(installer, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
		if output := invokeMain(t, "update"); !strings.Contains(output, "whip updated") {
			t.Fatalf("update output = %q", output)
		}
	})

	t.Run("run sessions mcp and auth", func(t *testing.T) {
		runFixture(t, "main reply", nil)
		if output := invokeMain(t, "run", "hello"); !strings.Contains(output, "main reply") {
			t.Fatalf("run output = %q", output)
		}
		if output := invokeMain(t, "sessions"); !strings.Contains(output, "test") {
			t.Fatalf("sessions output = %q", output)
		}
		if output := invokeMain(t, "mcp", "list"); output == "" {
			t.Fatal("mcp list produced no output")
		}
		if output := invokeMain(t, "auth", "inference-net", "status"); !strings.Contains(output, "Inference.net") {
			t.Fatalf("auth output = %q", output)
		}
	})
}
