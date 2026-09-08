package skills

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionOwnedPath(t *testing.T) {
	custom := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), custom)
	got := DirsFor("")
	if got[0] != filepath.Join(custom, "skills") {
		t.Fatalf("distribution path = %v", got)
	}
}
