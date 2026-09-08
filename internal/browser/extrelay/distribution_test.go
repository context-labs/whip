package extrelay

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionOwnedPath(t *testing.T) {
	custom := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), custom)
	got := ExtensionDir(t.TempDir())
	if got != filepath.Join(custom, "browser", "extension") {
		t.Fatalf("distribution path = %v", got)
	}
}
