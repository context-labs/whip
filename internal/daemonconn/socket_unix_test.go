//go:build unix

package daemonconn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathsDoesNotCreateRuntime(t *testing.T) {
	t.Parallel()
	root, err := os.MkdirTemp("/tmp", "whip-paths-") //nolint:usetesting // Exercise both short and hashed Unix socket paths on macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for _, test := range []struct {
		name     string
		home     string
		fallback bool
	}{
		{name: "short path", home: filepath.Join(root, "home")},
		{name: "long path", home: filepath.Join(root, strings.Repeat("long", 40)), fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			paths, err := ResolvePaths(test.home)
			if err != nil {
				t.Fatal(err)
			}
			if (paths.Runtime != paths.Home) != test.fallback {
				t.Fatalf("runtime fallback = %+v", paths)
			}
			for _, path := range []string{test.home, paths.Home, paths.Runtime} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("resolving paths touched %s: %v", path, err)
				}
			}
			created, err := Paths(test.home)
			if test.fallback {
				t.Cleanup(func() { _ = os.RemoveAll(paths.Runtime) })
			}
			if err != nil || created != paths {
				t.Fatalf("startup and discovery resolved different paths: %+v, %+v, %v", paths, created, err)
			}
		})
	}
}
