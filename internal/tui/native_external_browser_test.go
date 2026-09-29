package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeExternalConfigure(revision string, value protocol.ExternalBrowserConfiguration) string {
	raw, _ := json.Marshal(value)
	return "/browser external configure " + revision + " " + string(raw)
}

func TestNativeExternalBrowserControlsPreserveFullCASAndChildReadOnly(t *testing.T) {
	m := nativeIntegrationFixture(t)
	before := nativeMenuRPC[protocol.ExternalBrowserStatus](t, m.connection, "host.external_browser", protocol.EmptyParams{})
	if result := nativeUIControl(t, m, "/browser external status"); result.err != nil || !strings.Contains(m.notice, before.Revision) || !strings.Contains(m.notice, `"allow_private_urls":false`) {
		t.Fatal(result.err, m.notice)
	}
	settings := protocol.ExternalBrowserConfiguration{Mode: "headless", Executable: "/host/Chrome with spaces", AllowPrivateURLs: true}
	command := nativeExternalConfigure(before.Revision, settings)
	if result := nativeUIControl(t, m, command); result.err != nil || result.retry != nil || m.retryControl != nil {
		t.Fatal(result.err, m.status)
	}
	current := nativeMenuRPC[protocol.ExternalBrowserStatus](t, m.connection, "host.external_browser", protocol.EmptyParams{})
	if current.Configuration != settings {
		t.Fatal("complete declaration not retained", current)
	}
	if result := nativeUIControl(t, m, command); result.err == nil || !strings.Contains(m.status, "CONFLICT") || m.retryControl != nil {
		t.Fatal("old revision rebased or became retry", result, m.status)
	}
	for _, raw := range []string{`{"mode":"disabled"}`, `{"mode":"disabled","executable":"","live_endpoint":"","live_profile":"","allow_private_urls":false,"unexpected":true}`, strings.Repeat("x", 32<<10)} {
		if next := m.command("/browser external configure " + current.Revision + " " + raw); next != nil {
			t.Fatal("incomplete, unknown or oversized declaration sent", raw[:min(len(raw), 40)])
		}
	}
	spawned := nativeMenuRPC[protocol.SpawnSessionResult](t, m.connection, "sessions.spawn", protocol.SpawnSessionParams{ParentID: m.owner.ID, Identity: protocol.RequestIdentity{ClientID: "browser-controls", RequestID: "child"}, GrantIDs: []protocol.ID{}, Parts: []protocol.Part{{Type: "text", Text: "child"}}})
	nativeNavigate(t, m, string(spawned.Session.ID))
	for _, text := range []string{"/browser external status", "/browser external list"} {
		if result := nativeUIControl(t, m, text); result.err != nil || !strings.Contains(m.notice, "read-only") {
			t.Fatal(text, result.err, m.notice)
		}
	}
	for _, text := range []string{command, "/browser external reconnect default captured", "/browser external disconnect default captured", "/browser driver chromedp"} {
		if next := m.command(text); next != nil || !strings.Contains(m.status, "read-only") {
			t.Fatal("child offered mutation", text, m.status)
		}
	}
}

func TestNativeExternalBrowserLostAcknowledgementsNeverOfferReplay(t *testing.T) {
	for _, action := range []string{"configure", "reconnect", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			var mu sync.Mutex
			writes, reads := 0, 0
			m := nativeShellPeer(t, func(_ context.Context, request protocol.Request) any {
				mu.Lock()
				defer mu.Unlock()
				switch request.Method {
				case "host.set_external_browser":
					var params protocol.ConfigureExternalBrowserParams
					if err := json.Unmarshal(request.Params, &params); err != nil || params.ExpectedRevision != strings.Repeat("a", 64) || params.Configuration.Mode != "extension" {
						t.Error(params, err)
					}
				case "browser.reconnect_external", "browser.disconnect_external":
					var params protocol.ExternalBrowserConnectionParams
					if err := json.Unmarshal(request.Params, &params); err != nil || params.RootID != "owner" || params.Name != "default" || params.Generation != "captured" {
						t.Error(params, err)
					}
				case "host.external_browser":
					reads++
					return protocol.ExternalBrowserStatus{Revision: strings.Repeat("b", 64), Configuration: protocol.ExternalBrowserConfiguration{Mode: "extension"}, Driver: "rod"}
				case "browser.external_sessions":
					reads++
					return protocol.ExternalBrowserSessions{Items: []protocol.ExternalBrowserSession{}}
				default:
					t.Error("unexpected method", request.Method)
				}
				writes++
				return nil // Mutation accepted; only the transport acknowledgement is lost.
			})
			command := fmt.Sprintf("/browser external %s default captured", action)
			if action == "configure" {
				command = nativeExternalConfigure(strings.Repeat("a", 64), protocol.ExternalBrowserConfiguration{Mode: "extension"})
			}
			result := nativeUIControl(t, m, command)
			if result.err == nil || result.retry != nil || m.retryControl != nil || !strings.Contains(m.status, "not confirmed") {
				t.Fatal(result, m.status)
			}
			if retry := m.command("/retry"); retry != nil {
				t.Fatal("uncertain browser control was replayable")
			}
			if result := nativeUIControl(t, m, "/browser external status"); result.err != nil {
				t.Fatal(result.err)
			}
			if result := nativeUIControl(t, m, "/browser external list"); result.err != nil {
				t.Fatal(result.err)
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != 1 || reads != 2 {
				t.Fatal("passive recovery wrote again", writes, reads)
			}
		})
	}
}

func TestNativeExternalBrowserControlKeepsCapturedOwnerAcrossNavigation(t *testing.T) {
	received := make(chan protocol.ExternalBrowserConnectionParams, 1)
	m := nativeShellPeer(t, func(_ context.Context, request protocol.Request) any {
		if request.Method != "browser.reconnect_external" {
			t.Error("unexpected method", request.Method)
			return nil
		}
		var params protocol.ExternalBrowserConnectionParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Error(err)
			return nil
		}
		received <- params
		return protocol.ExternalBrowserSession{RootID: params.RootID, Name: params.Name, Generation: "replacement-generation", Resource: "browser-external:" + strings.Repeat("a", 64), Mode: "headless", Driver: "rod", State: "prepared"}
	})
	command := m.command("/browser external reconnect default captured")
	if command == nil {
		t.Fatal(m.status)
	}
	// Navigation can happen after the control captures its payload but before
	// its owned request returns. It must not retarget the request or its notice.
	m.owner.ID = "other-owner"
	m.generation++
	m.Update(command())
	params := <-received
	if params.RootID != "owner" || params.Name != "default" || params.Generation != "captured" {
		t.Fatal("control retargeted after navigation", params)
	}
	if m.notice != "" || m.owner.ID != "other-owner" || m.retryControl != nil {
		t.Fatal("old owner acknowledgement changed current view", m.notice, m.owner.ID)
	}
}
