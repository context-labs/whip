package tui

import (
	"testing"

	"github.com/context-labs/whip/internal/legacy/config"
)

// Mouse capture defaults ON (wheel scroll + clicks work) using click/wheel-only
// reporting (?1000, no motion) so native drag-to-copy still works. A config
// "mouse": false opts back into no capture.
func TestMouseDefaultsOn(t *testing.T) {
	cfg := config.Default()
	if cfg.Mouse != nil {
		t.Fatalf("default config must not set mouse (nil = on), got %v", *cfg.Mouse)
	}
	b := false
	cfg2 := &config.Config{Mouse: &b}
	if cfg2.Mouse == nil || *cfg2.Mouse {
		t.Fatal("explicit false should stay off")
	}
}
