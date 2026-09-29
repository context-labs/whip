package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/session"
)

func TestNativeBenchmarkFreshReadAndExplicitInitialization(t *testing.T) {
	home := filepath.Join(t.TempDir(), strings.Repeat("long-home-", 16))
	t.Setenv(buildinfo.Env("HOME"), home)
	if err := benchCLI(false, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only benchmark created product files", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, "config.json")
	if err := os.WriteFile(legacy, []byte("malformed retired configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := benchCLI(true, "", ""); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(home, localruntime.Namespace)
	path := filepath.Join(directory, config.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("host publication permissions", info, err)
	}
	for _, initialize := range []bool{false, true} {
		if err := benchCLI(initialize, "", ""); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(before) != string(after) {
			t.Fatal("benchmark rewrote existing configuration", err)
		}
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 1 || entries[0].Name() != config.FileName {
		t.Fatal("benchmark created a database, runtime, or lock", entries, err)
	}
	if old, err := os.ReadFile(legacy); err != nil || string(old) != "malformed retired configuration" {
		t.Fatal("retired user file changed", err)
	}
}

func TestNativeBenchmarkSelectionDoesNotResolveCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), home)
	marker, executable := filepath.Join(home, "credential-ran"), filepath.Join(home, "credential")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'ran' > \"$1\"\nprintf 'secret'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	host := config.Default()
	host.Defaults.Model = session.ModelSelection{Provider: "fixture", Name: "default-model"}
	host.Providers["fixture"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: "command", CredentialCommand: &config.CredentialCommand{Executable: executable, Arguments: []string{marker}}}
	host.Providers["file"] = config.Provider{Kind: "openai-responses", BaseURL: "http://127.0.0.1:1", CredentialSource: "file", CredentialFile: filepath.Join(home, "missing-private-key")}
	host.Providers["env"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialEnv: "WHIP_TEST_BENCH_ABSENT_KEY"}
	t.Setenv("WHIP_TEST_BENCH_ABSENT_KEY", "")
	if err := config.Save(filepath.Join(home, localruntime.Namespace), host); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, model, provider string
		wantError             bool
	}{
		{name: "saved default"},
		{name: "explicit model without optional metadata", model: "provider-model"},
		{name: "file is not read", provider: "file"},
		{name: "environment key is not resolved", provider: "env"},
		{name: "unknown provider", model: "model", provider: "missing", wantError: true},
		{name: "invalid selector", model: "bad\x00model", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := benchCLI(false, test.model, test.provider); (err != nil) != test.wantError {
				t.Fatal(err)
			}
		})
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("benchmark executed credential command", err)
	}
	t.Setenv(buildinfo.Env("HOME"), filepath.Join(home, "fresh"))
	if err := benchCLI(false, "model", ""); err == nil {
		t.Fatal("partial selection accepted on an unconfigured host")
	}
	if err := benchCLI(false, "", "fixture"); err == nil {
		t.Fatal("provider without a model accepted on an unconfigured host")
	}
}

func TestNativeBenchmarkMalformedOrSymlinkConfigurationIsNeverReplaced(t *testing.T) {
	for _, kind := range []string{"malformed", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv(buildinfo.Env("HOME"), home)
			directory := filepath.Join(home, localruntime.Namespace)
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, config.FileName)
			target := path
			if kind == "symlink" {
				target = filepath.Join(home, "outside.json")
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(target, []byte("unfinished user edit"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, initialize := range []bool{false, true} {
				if err := benchCLI(initialize, "", ""); err == nil {
					t.Fatal("unsafe configuration accepted")
				}
				if after, err := os.ReadFile(target); err != nil || string(after) != "unfinished user edit" {
					t.Fatal("user file replaced", err)
				}
			}
		})
	}
}
