package runtimepath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketDiscoveryIsStableShortAndReadOnly(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, strings.Repeat("long", 40))
	path := Socket(directory)
	if len(path) > 100 || filepath.Dir(path) == directory || Socket(directory+"-other") == path {
		t.Fatal("invalid fallback", path)
	}
	t.Setenv("TMPDIR", filepath.Join(base, strings.Repeat("temp", 40)))
	if Socket(directory) != path {
		t.Fatal("caller environment changed runtime address")
	}
	for _, untouched := range []string{directory, filepath.Dir(path)} {
		if _, err := os.Lstat(untouched); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("discovery created storage", untouched, err)
		}
	}
	if got := Socket("/tmp/short-native"); got != "/tmp/short-native/runtime.sock" {
		t.Fatal(got)
	}
}
