package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/protocol"
)

func availableBrowserOffer(root string) protocol.BrowserProviderBindParams {
	offer := browserOffer(root)
	offer.Version, offer.Availability, offer.OfferedTabs = 2, true, nil
	return offer
}

func readInventory(t *testing.T, w *browserWire) protocol.BrowserInventoryRequest {
	t.Helper()
	for {
		var message rpcMessage
		if len(w.queued) > 0 {
			message, w.queued = w.queued[0], w.queued[1:]
		} else {
			message = w.read()
		}
		if message.Method != "browser.inventory" {
			continue
		}
		var request protocol.BrowserInventoryRequest
		if err := json.Unmarshal(message.Params, &request); err != nil {
			t.Fatal(err)
		}
		return request
	}
}

func listBrowserAsync(ctx context.Context, p *browserProviders, identity browser.DesktopIdentity) <-chan desktopOutcome {
	done := make(chan desktopOutcome, 1)
	go func() { result, err := p.ListTabs(ctx, identity); done <- desktopOutcome{result, err} }()
	return done
}

func inventoryReply(request protocol.BrowserInventoryRequest, tabs ...browser.DesktopTab) protocol.BrowserInventoryResultParams {
	return protocol.BrowserInventoryResultParams{RequestID: request.RequestID, RootID: request.RootID, ProviderEpoch: request.ProviderEpoch, Tabs: tabs}
}

func TestBrowserAvailabilityRequiresUnambiguousApprovedDestination(t *testing.T) {
	d, server, root := browserHarness(t)
	first, second := connectBrowserWire(t, server, true), connectBrowserWire(t, server, true)
	one := bindBrowserWire(t, first, availableBrowserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.browserProviders.roots) != 0 {
		t.Fatal("availability/Resolve selected a destination before approval")
	}
	two := bindBrowserWire(t, second, availableBrowserOffer(root))
	if _, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "https://example.test"}); err == nil {
		t.Fatal("connection order selected an ambiguous window")
	}
	out := awaitBrowser(t, executeBrowserAsync(t.Context(), d.browserProviders, identity, "browser.open", call))
	if out.err == nil {
		t.Fatal("approval raced a new candidate and still executed")
	}
	if reply := second.rpc("browser.provider.unbind", protocol.BrowserProviderUnbindParams{RootID: root, ProviderEpoch: two.ProviderEpoch}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	call, err = d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	done := executeBrowserAsync(t.Context(), d.browserProviders, identity, "browser.open", call)
	command := first.command()
	if command.Kind != "open" || command.ProviderEpoch != one.ProviderEpoch {
		t.Fatalf("wrong destination: %+v", command)
	}
	if reply := first.settle(command, browser.DesktopResult{URL: "https://example.test/"}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	out = awaitBrowser(t, done)
	if out.err != nil || out.result.AttachmentID == "" {
		t.Fatalf("open: %+v", out)
	}
	bindBrowserWire(t, second, availableBrowserOffer(root))
	if _, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: out.result.AttachmentID, Code: "info()"}); err != nil {
		t.Fatalf("new advertiser stole the approved destination: %v", err)
	}
}

func TestBrowserInventoryFreshScopedAndAuthenticated(t *testing.T) {
	d, server, root := browserHarness(t)
	wire, stranger := connectBrowserWire(t, server, true), connectBrowserWire(t, server, true)
	offer := browserOffer(root)
	offer.Version = 2
	bindBrowserWire(t, wire, offer)
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, wire, identity, "tab-1")
	done := listBrowserAsync(t.Context(), d.browserProviders, identity)
	request := readInventory(t, wire)
	if len(request.Tabs) != 2 {
		t.Fatalf("targets: %+v", request.Tabs)
	}
	tab := browser.DesktopTab{TabID: "tab-1", TabGeneration: "tab-gen", DocumentRevision: "new-document", URL: "https://user:password@example.test/new", Title: "New title", State: "busy", AttachmentID: "untrusted-handle"}
	if reply := stranger.rpc("browser.inventory.result", inventoryReply(request, tab)); reply.Error == nil {
		t.Fatal("different connection settled inventory")
	}
	malicious := tab
	malicious.TabID = "unshared"
	if reply := wire.rpc("browser.inventory.result", inventoryReply(request, malicious)); reply.Error == nil {
		t.Fatal("unrequested metadata accepted")
	}
	if reply := wire.rpc("browser.inventory.result", inventoryReply(request, tab)); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	out := awaitBrowser(t, done)
	if out.err != nil || len(out.result.Tabs) != 1 {
		t.Fatalf("list: %+v", out)
	}
	got := out.result.Tabs[0]
	if got.Title != "New title" || got.URL != "https://example.test/new" || got.AttachmentID != attached.AttachmentID || got.State != "attached" || got.Requestable {
		t.Fatalf("metadata: %+v", got)
	}
	child := browser.DesktopIdentity{RootID: root, AgentID: "unrelated-child"}
	done = listBrowserAsync(t.Context(), d.browserProviders, child)
	request = readInventory(t, wire)
	if len(request.Tabs) != 0 {
		t.Fatal("child inherited offered tab metadata")
	}
	if reply := wire.rpc("browser.inventory.result", inventoryReply(request)); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if out = awaitBrowser(t, done); out.err != nil || len(out.result.Tabs) != 0 {
		t.Fatalf("child: %+v", out)
	}
	if _, err := d.browserProviders.Resolve(t.Context(), child, "browser.attach", browser.DesktopArguments{TabID: "tab-2"}); err == nil {
		t.Fatal("child attached root-only offer")
	}
}

func TestBrowserInventoryCancellationAndStaleEpoch(t *testing.T) {
	d, server, root := browserHarness(t)
	wire := connectBrowserWire(t, server, true)
	bindBrowserWire(t, wire, availableBrowserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	ctx, cancel := context.WithCancel(t.Context())
	done := listBrowserAsync(ctx, d.browserProviders, identity)
	request := readInventory(t, wire)
	cancel()
	if out := awaitBrowser(t, done); out.err == nil {
		t.Fatal("cancelled inventory succeeded")
	}
	if reply := wire.rpc("browser.inventory.result", inventoryReply(request)); reply.Error == nil {
		t.Fatal("late result accepted")
	}
	done = listBrowserAsync(t.Context(), d.browserProviders, identity)
	request = readInventory(t, wire)
	if reply := wire.rpc("browser.provider.unbind", protocol.BrowserProviderUnbindParams{RootID: root, ProviderEpoch: request.ProviderEpoch}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if out := awaitBrowser(t, done); out.err == nil {
		t.Fatal("unbound inventory succeeded")
	}
}
