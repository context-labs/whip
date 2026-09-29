package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func TestBrowserDriverRPCSharedCASAndPin(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	r, c := fixture(t)
	before := call[protocol.HostBrowserDriver](t, c, "host.browser_driver", protocol.EmptyParams{})
	if before.Driver != "rod" || before.ConfiguredDriver != "rod" || before.Pinned {
		t.Fatal(before)
	}
	written := call[protocol.HostBrowserDriver](t, c, "host.set_browser_driver", protocol.SetBrowserDriverParams{ExpectedRevision: before.Revision, Driver: "chromedp"})
	if written.Driver != "chromedp" || written.ConfiguredDriver != "chromedp" || written.Revision == before.Revision {
		t.Fatal(written)
	}
	requireHistoryError(t, c, "host.set_browser_driver", protocol.SetBrowserDriverParams{ExpectedRevision: before.Revision, Driver: "rod"}, "CONFLICT")
	same := call[protocol.HostBrowserDriver](t, c, "host.set_browser_driver", protocol.SetBrowserDriverParams{ExpectedRevision: written.Revision, Driver: "chromedp"})
	if same != written {
		t.Fatal(same, written)
	}
	if _, err := r.HostConfiguration().Update(t.Context(), written.Revision, func(h *config.Host) error { h.ProjectRoots["root"] = "/tmp/project"; return nil }); err != nil {
		t.Fatal(err)
	}
	requireHistoryError(t, c, "host.set_browser_driver", protocol.SetBrowserDriverParams{ExpectedRevision: written.Revision, Driver: "rod"}, "CONFLICT")
	if err := c.Call(t.Context(), "host.set_browser_driver", protocol.SetBrowserDriverParams{ExpectedRevision: written.Revision, Driver: "other"}, nil); err == nil {
		t.Fatal("invalid driver passed wire validation")
	}
	t.Setenv("WHIP_BROWSER_DRIVER", "chromedp")
	_, pinned := fixture(t)
	pin := call[protocol.HostBrowserDriver](t, pinned, "host.browser_driver", protocol.EmptyParams{})
	if !pin.Pinned || pin.Driver != "chromedp" || pin.ConfiguredDriver != "rod" {
		t.Fatal(pin)
	}
	requireHistoryError(t, pinned, "host.set_browser_driver", protocol.SetBrowserDriverParams{ExpectedRevision: pin.Revision, Driver: "rod"}, "INVALID")
}
