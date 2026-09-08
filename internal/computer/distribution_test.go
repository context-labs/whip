//go:build darwin

package computer

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionOwnedPath(t *testing.T) {
	custom := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), custom)
	got, err := helperDest()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(custom, "bin", "whip-computer") {
		t.Fatalf("distribution path = %v", got)
	}
}
