package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestGeneratedContractChecksDriftWithoutMutation(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "schema")
	if err := generate(dir, false); err != nil {
		t.Fatal(err)
	}
	if err := generate(dir, true); err != nil {
		t.Fatalf("generation is not deterministic: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var generated manifest
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatal(err)
	}
	if generated.Major != protocol.Major || len(generated.Operations) != len(protocol.Operations()) || len(generated.Events) == 0 {
		t.Fatalf("incomplete contract: %+v", generated)
	}
	fixtures, err := os.ReadFile(filepath.Join(dir, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixtures), `"cursor": "9007199254740993"`) || !strings.Contains(string(fixtures), `"size": "9007199254740993"`) {
		t.Fatalf("fixtures must preserve integers beyond JavaScript precision: %s", fixtures)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte("drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generate(dir, true); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("check accepted drift: %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != "drift" {
		t.Fatalf("check mutated drifted file: %q, %v", after, err)
	}
	if err := generate(dir, false); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "removed-operation.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generate(dir, true); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("check accepted stale schema: %v", err)
	}
	if err := generate(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("generation retained stale schema: %v", err)
	}
	if err := generate(dir, true); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateRejectsUnusableOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := generate(filepath.Join(dir, "missing"), true); err == nil {
		t.Fatal("missing output passed check")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generate(filepath.Join(file, "child"), false); err == nil {
		t.Fatal("output under regular file accepted")
	}
}
