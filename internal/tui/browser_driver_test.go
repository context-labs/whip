package tui

import (
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/tools"
)

// The ctrl+p "Browser driver" row exists, shows the current driver, and
// switching it updates only this model's browser manager.
func TestBrowserDriverPalette(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	services := tools.NewServices()
	manager := browser.NewManager(browser.ModeHeadless)
	services.SetBrowser(manager, false)
	m := &model{cfg: &config.Config{}, agent: &agent.Agent{Services: services}}
	var found *paletteItem
	for i, it := range m.paletteItems() {
		if it.title == "Browser driver" {
			found = &m.paletteItems()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no Browser driver palette row")
	}
	if got := found.dynDesc(m); got == "" {
		t.Fatal("empty description")
	}
	pp := found.panel(m)
	if pp == nil || pp.kind != panelBrowser {
		t.Fatal("panel must be panelBrowser")
	}
	if len(pp.list) != 2 {
		t.Fatalf("want 2 drivers, got %v", pp.list)
	}
	m.switchBrowserDriver(browser.DriverChromedp)
	if manager.Driver() != browser.DriverChromedp {
		t.Fatalf("switch failed: %q", manager.Driver())
	}
}
