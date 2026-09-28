package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
	"golang.org/x/sys/unix"
)

func TestCredentialDeclarationRejectsAmbiguousAndUnboundedSources(t *testing.T) {
	validCommand := &CredentialCommand{Executable: "/usr/bin/credential", Arguments: []string{"read", "named-key"}, Environment: []string{"HOME"}}
	for _, test := range []struct {
		name     string
		provider Provider
	}{
		{"env", Provider{CredentialSource: "env", CredentialEnv: "NAMED_KEY"}},
		{"file", Provider{CredentialSource: "file", CredentialFile: "/private/key"}},
		{"command", Provider{CredentialSource: "command", CredentialCommand: validCommand}},
		{"none", Provider{CredentialSource: "none"}},
		{"environment shorthand", Provider{CredentialEnv: "NAMED_KEY"}},
		{"no-auth shorthand", Provider{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := test.provider
			p.Kind, p.BaseURL = "openai-chat", "https://example.test/v1"
			host := Default()
			host.Providers["fixture"] = p
			if err := host.Validate(); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"openai-codex", "managed"} {
				unsafe := p
				unsafe.Kind, unsafe.BaseURL = "openai-codex", ""
				if kind == "managed" {
					unsafe.Kind, unsafe.BaseURL, unsafe.CredentialSource = "openai-chat", "https://api.inference.net/v1", "inference-net"
				}
				if p.CredentialSource == "" && p.CredentialEnv == "" || kind == "managed" && p.CredentialEnv == "" && p.CredentialFile == "" && p.CredentialCommand == nil {
					continue
				}
				if _, err := unsafe.Credential(t.Context(), nil); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("%s accepted generic credentials: %v", kind, err)
				}
			}
		})
	}
	for _, p := range []Provider{
		{CredentialSource: "unknown"},
		{CredentialSource: "env"},
		{CredentialEnv: "bad-key"},
		{CredentialEnv: strings.Repeat("x", 257)},
		{CredentialSource: "env", CredentialEnv: "KEY", CredentialFile: "/key"},
		{CredentialSource: "env", CredentialEnv: "KEY", CredentialCommand: validCommand},
		{CredentialSource: "none", CredentialEnv: "KEY"},
		{CredentialSource: "none", CredentialFile: "/key"},
		{CredentialSource: "none", CredentialCommand: validCommand},
		{CredentialFile: "/key"},
		{CredentialCommand: validCommand},
		{CredentialSource: "file", CredentialFile: "relative"},
		{CredentialSource: "file", CredentialFile: "~/key"},
		{CredentialSource: "file", CredentialFile: "/a/../key"},
		{CredentialSource: "file", CredentialFile: "/"},
		{CredentialSource: "file", CredentialFile: "/key\x00"},
		{CredentialSource: "file", CredentialFile: "/key", CredentialEnv: "KEY"},
		{CredentialSource: "file", CredentialFile: "/key", CredentialCommand: validCommand},
		{CredentialSource: "command"},
		{CredentialSource: "command", CredentialCommand: validCommand, CredentialFile: "/key"},
		{CredentialSource: "command", CredentialCommand: validCommand, CredentialEnv: "KEY"},
	} {
		if _, err := p.Credential(t.Context(), nil); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid source accepted: %+v error=%v", p, err)
		}
	}
	for _, command := range []CredentialCommand{
		{Executable: "credential"},
		{Executable: "/credential", Arguments: make([]string, 65)},
		{Executable: "/credential", Arguments: []string{"bad\x00argument"}},
		{Executable: "/credential", Arguments: []string{strings.Repeat("a", 4097)}},
		{Executable: "/credential", Arguments: slices.Repeat([]string{strings.Repeat("a", 4096)}, 16)},
		{Executable: "/credential", Environment: []string{"BAD-NAME"}},
		{Executable: "/credential", Environment: []string{"HOME", "HOME"}},
		{Executable: "/credential", Environment: make([]string, 65)},
	} {
		if err := command.validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid command accepted: %v", err)
		}
	}
}

func TestCredentialEnvironmentAndNoAuthHaveNoDiscovery(t *testing.T) {
	oldHome := t.TempDir()
	if err := os.Mkdir(filepath.Join(oldHome, ".inf"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldHome, ".inf", "config"), []byte(`{"api_key":"private-old-home"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", oldHome)
	t.Setenv("INFERENCE_API_KEY", "private-implicit")
	lookups := 0
	lookup := func(name string) (string, bool) { lookups++; return "private-implicit", true }
	for _, source := range []string{"", "none", "inference-net"} {
		p := Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialSource: source}
		if key, err := p.Credential(t.Context(), lookup); err != nil || key != "" || lookups != 0 {
			t.Fatal("source discovered undeclared credentials", err)
		}
	}
	p := Provider{CredentialSource: "env", CredentialEnv: "EXPLICIT_KEY"}
	for _, value := range []string{"", " ", "private\nsecond", "private\x00key", "private\tkey", "private key", "private\x7f", "private\xff", strings.Repeat("p", maxCredentialBytes+1)} {
		if key, err := p.Credential(t.Context(), func(string) (string, bool) { return value, true }); err == nil || key != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe credential or error accepted", err)
		}
	}
	if key, err := p.Credential(t.Context(), func(string) (string, bool) { return " private-fixture\n", true }); err != nil || key != "private-fixture" {
		t.Fatal("raw key whitespace failed", err)
	}
	if _, err := p.Credential(t.Context(), func(string) (string, bool) { return "private-unset", false }); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("missing declared environment fell back", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Credential(cancelled, lookup); !errors.Is(err, context.Canceled) || lookups != 0 {
		t.Fatal("cancelled preparation resolved credentials", err)
	}
}

func privateCredentialDirectory(t *testing.T) string {
	t.Helper()
	// macOS exposes its temporary root through /var; declarations deliberately
	// require the canonical path so no component is a symlink.
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestCredentialFilePrivateBoundsAndRotation(t *testing.T) {
	directory := privateCredentialDirectory(t)
	path := filepath.Join(directory, "key")
	p := Provider{CredentialSource: "file", CredentialFile: path}
	for _, value := range []string{"private-first\n", "private-second"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		key, err := p.Credential(t.Context(), nil)
		if err != nil || key != strings.TrimSpace(value) {
			t.Fatal("fresh file credential failed", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Credential(t.Context(), nil); err == nil || strings.Contains(err.Error(), path) {
		t.Fatal("removed file reused a cached key or leaked path", err)
	}
	for _, value := range []string{"", "first\nsecond", strings.Repeat("x", maxCredentialBytes+1)} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Credential(t.Context(), nil); err == nil {
			t.Fatal("invalid private credential file accepted")
		}
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("k", maxCredentialBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if key, err := p.Credential(t.Context(), nil); err != nil || len(key) != maxCredentialBytes {
		t.Fatal("exact file bound rejected", err)
	}
}

func TestCredentialFileRejectsUnsafeObjectsWithoutFollowingLinks(t *testing.T) {
	directory := privateCredentialDirectory(t)
	file := filepath.Join(directory, "private-file")
	if err := os.WriteFile(file, []byte("private-file-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(directory, "linked-directory")
	if err := os.Symlink(directory, parentLink); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(directory, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "key"), []byte("private-child-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(directory, "pipe")
	if err := unix.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(parentLink, "private-file"), filepath.Join(parentLink, "child", "key"), child, pipe} {
		if _, err := (Provider{CredentialSource: "file", CredentialFile: path}).Credential(t.Context(), nil); err == nil || strings.Contains(err.Error(), "private-file-key") || strings.Contains(err.Error(), path) {
			t.Fatal("unsafe file accepted or diagnostic leaked path", err)
		}
	}
	p := Provider{CredentialSource: "file", CredentialFile: file}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o606} {
		if err := os.Chmod(file, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Credential(t.Context(), nil); err == nil {
			t.Fatal("nonprivate file accepted")
		}
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0o700)
	if _, err := p.Credential(t.Context(), nil); err == nil {
		t.Fatal("writable parent accepted")
	}
}

func credentialHelper(t *testing.T, mode string, args ...string) Provider {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Provider{CredentialSource: "command", CredentialCommand: &CredentialCommand{
		Executable:  executable,
		Arguments:   append([]string{"-test.run=^TestCredentialCommandHelper$", "--", mode}, args...),
		Environment: []string{"WHIP_CREDENTIAL_HELPER", "GORACE"},
	}}
}

func helperEnvironment(name string) (string, bool) {
	switch name {
	case "WHIP_CREDENTIAL_HELPER":
		return "1", true
	case "GORACE":
		return "atexit_sleep_ms=0", true
	case "EXPLICIT_VALUE":
		return "private-explicit", true
	default:
		return "", false
	}
}

func TestCredentialCommandHelper(t *testing.T) {
	if os.Getenv("WHIP_CREDENTIAL_HELPER") != "1" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	switch args[0] {
	case "echo":
		fmt.Print(args[1])
	case "environment":
		stdin, _ := io.ReadAll(os.Stdin)
		directory, _ := os.Getwd()
		if os.Getenv("UNDECLARED_SECRET") != "" || os.Getenv("HOME") != "" || os.Getenv("PATH") != "" || directory != "/" || len(stdin) != 0 {
			os.Exit(20)
		}
		fmt.Print(os.Getenv("EXPLICIT_VALUE"))
	case "fail":
		fmt.Fprint(os.Stdout, "private-stdout")
		fmt.Fprint(os.Stderr, "private-stderr")
		os.Exit(21)
	case "overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("k", maxCredentialBytes/2))
		fmt.Fprint(os.Stderr, strings.Repeat("x", maxCredentialBytes/2+1))
	case "wait":
		time.Sleep(time.Minute)
	case "descendant", "orphan":
		child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCredentialCommandHelper$", "--", "marker", args[1])
		child.Env = os.Environ()
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(22)
		}
		if err := os.WriteFile(args[2], []byte("ready"), 0o600); err != nil {
			os.Exit(23)
		}
		if args[0] == "descendant" {
			time.Sleep(time.Minute)
		}
	case "marker":
		time.Sleep(800 * time.Millisecond)
		if err := os.WriteFile(args[1], []byte("survived"), 0o600); err != nil {
			os.Exit(24)
		}
	default:
		os.Exit(25)
	}
	os.Exit(0)
}

func TestCredentialCommandExplicitEnvironmentAndLiteralArguments(t *testing.T) {
	t.Setenv("UNDECLARED_SECRET", "private-daemon")
	p := credentialHelper(t, "environment")
	p.CredentialCommand.Environment = append(p.CredentialCommand.Environment, "EXPLICIT_VALUE")
	if key, err := p.Credential(t.Context(), helperEnvironment); err != nil || key != "private-explicit" {
		t.Fatal("command inherited undeclared environment or lost declared value", err)
	}
	p = credentialHelper(t, "echo", "literal$KEY$(ignored);value")
	if key, err := p.Credential(t.Context(), helperEnvironment); err != nil || key != "literal$KEY$(ignored);value" {
		t.Fatal("command evaluated argument text", err)
	}
	for _, mode := range []string{"fail", "overflow"} {
		p = credentialHelper(t, mode)
		if key, err := p.Credential(t.Context(), helperEnvironment); err == nil || key != "" || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), p.CredentialCommand.Executable) {
			t.Fatal("failed command returned credentials or unsafe diagnostics", err)
		}
	}
	p = credentialHelper(t, "echo", "private-unused")
	p.CredentialCommand.Environment = append(p.CredentialCommand.Environment, "MISSING")
	if key, err := p.Credential(t.Context(), helperEnvironment); err == nil || key != "" {
		t.Fatal("missing explicit command environment accepted")
	}
	p.CredentialCommand.Executable = "/missing/private-executable"
	p.CredentialCommand.Environment = nil
	if _, err := p.Credential(t.Context(), nil); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("launch failure exposed command details", err)
	}
	p = credentialHelper(t, "echo", "private-unused")
	p.CredentialCommand.Environment = []string{"EXPLICIT_VALUE"}
	for _, value := range []string{"private\x00environment", strings.Repeat("x", maxCredentialBytes)} {
		if _, err := p.Credential(t.Context(), func(string) (string, bool) { return value, true }); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid command environment accepted or leaked", err)
		}
	}
}

func TestCredentialCommandCancellationAndLeaderExitKillDescendants(t *testing.T) {
	for _, mode := range []string{"descendant", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			marker, ready := filepath.Join(directory, "marker"), filepath.Join(directory, "ready")
			p := credentialHelper(t, mode, marker, ready)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := p.Credential(ctx, helperEnvironment); result <- err }()
			deadline := time.Now().Add(4 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					cancel()
					<-result
					t.Fatal("helper did not start")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if mode == "descendant" {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil || mode == "descendant" && !errors.Is(err, context.Canceled) {
					t.Fatal("unfinished command did not fail safely", err)
				}
			case <-time.After(time.Second):
				cancel()
				<-result
				t.Fatal("command did not kill and join promptly")
			}
			time.Sleep(time.Second)
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("credential command descendant survived return")
			}
		})
	}
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		if _, err := credentialHelper(t, "wait").Credential(ctx, helperEnvironment); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
			t.Fatal("command did not respect caller deadline and join promptly", err)
		}
	})
}

func TestCredentialDeclarationReadsNeverExecuteCommandsOrReadKeys(t *testing.T) {
	marker, ready := filepath.Join(t.TempDir(), "marker"), filepath.Join(t.TempDir(), "ready")
	host := Default()
	command := credentialHelper(t, "orphan", marker, ready)
	command.Kind, command.BaseURL = "openai-chat", "https://example.test/v1"
	host.Providers["command"] = command
	host.Providers["missing-file"] = Provider{Kind: "openai-responses", BaseURL: "https://example.test/v1", CredentialSource: "file", CredentialFile: "/missing/private-file"}
	directory := t.TempDir()
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	authority, err := NewAuthority(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ready); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("declaration validation executed credential command")
	}
	raw, err := os.ReadFile(filepath.Join(directory, FileName))
	if err != nil || strings.Contains(string(raw), "private-explicit") {
		t.Fatal("configuration retained a credential")
	}
}
