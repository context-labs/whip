package bashrun

import (
	"path/filepath"
	"testing"
)

func TestStartFailureIsDistinctFromCommandExit(t *testing.T) {
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	result := Run(t.Context(), Options{Command: "echo should-not-launch", Cwd: t.TempDir()})
	if result.StartError == nil || result.TotalBytes != 0 || result.Output != "" {
		t.Fatal(result)
	}
}
