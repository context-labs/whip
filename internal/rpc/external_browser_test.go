package rpc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestExternalBrowserRPCPassiveConfigurationCAS(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	r, c := fixture(t)
	before := call[protocol.ExternalBrowserStatus](t, c, "host.external_browser", protocol.EmptyParams{})
	if before.Configuration.Mode != "disabled" || before.Driver != "rod" || before.DriverPinned {
		t.Fatal(before)
	}
	settings := protocol.ExternalBrowserConfiguration{Mode: "headless", Executable: "/not/probed/by/status"}
	written := call[protocol.ExternalBrowserStatus](t, c, "host.set_external_browser", protocol.ConfigureExternalBrowserParams{ExpectedRevision: before.Revision, Configuration: settings})
	if written.Configuration != settings || written.Revision == before.Revision {
		t.Fatal(written)
	}
	requireHistoryError(t, c, "host.set_external_browser", protocol.ConfigureExternalBrowserParams{ExpectedRevision: before.Revision, Configuration: settings}, "CONFLICT")
	same := call[protocol.ExternalBrowserStatus](t, c, "host.external_browser", protocol.EmptyParams{})
	if same != written {
		t.Fatal(same, written)
	}
	requireHistoryError(t, c, "host.set_external_browser", protocol.ConfigureExternalBrowserParams{ExpectedRevision: written.Revision, Configuration: protocol.ExternalBrowserConfiguration{Mode: "live", LiveEndpoint: "http://not-loopback.example:9222"}}, "INVALID")
	// Configuration/status cannot create a profile or discover an executable.
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.SocketPath()), "browser")); !os.IsNotExist(err) {
		t.Fatal("passive browser control touched profile", err)
	}
}
