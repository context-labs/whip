package browser

import (
	"context"
	"strings"
	"testing"
)

func TestManagerDriver(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	m := NewManager(ModeHeadless)
	m.SwitchDriver(DriverChromedp)
	if m.Driver() != DriverChromedp {
		t.Fatalf("got %q", m.Driver())
	}
	m.SwitchDriver(DriverRod)
	if m.Driver() != DriverRod {
		t.Fatalf("got %q", m.Driver())
	}
	m.SwitchDriver("bogus")
	if m.Driver() != DriverRod {
		t.Fatalf("bogus driver must be ignored, got %q", m.Driver())
	}
}

func TestSetDriverEnvPinWins(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "chromedp")
	m := NewManager(ModeHeadless)
	m.SwitchDriver(DriverRod)
	if m.Driver() != DriverChromedp {
		t.Fatalf("pin must make SwitchDriver a no-op, got %q", m.Driver())
	}
}

func TestChromedpRejectsScopedLaunchEnvironment(t *testing.T) {
	_, err := openChromedp(context.Background(), ModeHeadless, "test", []string{"PATH=/bin"})
	if err == nil || !strings.Contains(err.Error(), "isolated environment") {
		t.Fatalf("scoped chromedp launch error = %v", err)
	}
}
