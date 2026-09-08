// ext.go wires the extension-relay backend: Open(ctx, ModeExtension) starts
// (or reuses) the loopback extrelay, waits for the user to pin a tab via the
// extension icon, and hands rod the relay's /cdp endpoint — rod drives the
// real logged-in tab unchanged.

package browser

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/context-labs/whip/internal/buildinfo"

	"github.com/context-labs/whip/internal/browser/extrelay"
	"github.com/go-rod/rod"
)

// sharedRelay is the process-wide extension relay: one per agent run, so the
// extension's single pinned tab and the single auth token stay stable across
// sessions. The token lives for the process's lifetime.
var (
	sharedRelayMu sync.Mutex
	sharedRelay   *extrelay.Relay
)

// relayForExtension returns the shared relay, starting it on first use.
func relayForExtension() (*extrelay.Relay, error) {
	sharedRelayMu.Lock()
	defer sharedRelayMu.Unlock()
	if sharedRelay != nil {
		return sharedRelay, nil
	}
	r, err := extrelay.NewRelay()
	if err != nil {
		return nil, err
	}
	// Make the relay discoverable to the installed extension: relay.json
	// carries the live address + token (0600 — a drive-a-tab credential).
	if home, herr := os.UserHomeDir(); herr == nil {
		if _, werr := extrelay.WriteRelayState(home, r.Addr(), r.Token()); werr != nil {
			// Non-fatal: the relay works for backends, the extension just
			// can't auto-find it until install re-runs.
			_ = werr
		}
	}
	sharedRelay = r
	return r, nil
}

// openExtension connects rod to the pinned tab through the relay.
func openExtension(ctx context.Context) (*Browser, error) {
	r, err := relayForExtension()
	if err != nil {
		return nil, fmt.Errorf("start extension relay: %w", err)
	}
	if err := r.WaitAttached(ctx); err != nil {
		return nil, fmt.Errorf(buildinfo.Text("extension relay waiting for a pinned tab: %w\n(run `whip browser install`, load the extension, then click its icon on the tab to drive)"), err)
	}
	b := &Browser{mode: ModeExtension, obtained: ObtainedLive}
	b.browser = rod.New().ControlURL(r.CDPURL())
	if err := b.browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect to extension relay: %w", err)
	}
	b.browser = b.browser.Context(ctx)
	if err := b.attachPage(); err != nil {
		detach(b.browser)
		return nil, fmt.Errorf("attach to pinned tab: %w", err)
	}
	return b, nil
}
