package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedThemesAreCurrentAndDeterministic(t *testing.T) {
	first, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range first {
		if !bytes.Equal(data, second[name]) {
			t.Fatalf("nondeterministic %s", name)
		}
		path := filepath.Join("..", "..", "packages", "ui", "src", "generated", name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("theme drift in %s: go run ./cmd/themegen", path)
		}
	}
}

func TestDriftCheckRejectsChangedArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := run(dir, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "theme-catalog.ts")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, true); err == nil {
		t.Fatal("accepted stale generated catalog")
	}
}
