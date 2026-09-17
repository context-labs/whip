package capability

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func permissionBrowserCall() BrowserCall {
	return BrowserCall{
		Grant: Reference{ID: "private-grant-id", Generation: 7},
		Scope: BrowserScope{
			ProviderID: "desktop", ProviderEpoch: "epoch-1", TabID: "tab-1", TabGeneration: "tab-gen-1",
			ProfileID: "profile-1", AttachmentID: "attachment-1", AttachmentGeneration: "attachment-gen-1",
			Rights: []string{"create", "control", "route"},
			Preview: &BrowserPreviewScope{HostID: "saved-host", HostIdentity: "verified-runtime", ConnectionGeneration: "connection-1",
				EnvironmentID: "environment-1", Loopback: "127.0.0.1", Ports: []int{3000, 8080}},
		},
		Arguments: json.RawMessage(`{"url":"https://user:credential-secret@example.com/app?token=query-secret#fragment-secret","port":9000,"code":"private-code-secret","provider_id":"forged-provider","socket":"/private/transport.sock"}`),
	}
}

func permissionBrowserSummary(t *testing.T, operation string, call BrowserCall) string {
	t.Helper()
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	command, rules, ok := PermissionRule(operation, raw, "ignored-local-path")
	if ok || rules != nil {
		t.Fatalf("browser consent became rememberable: %v %v", rules, ok)
	}
	return command
}

func TestBrowserPermissionSummaryResourcesAndLifetimes(t *testing.T) {
	for _, operation := range []string{"browser.open", "browser.attach", "browser.run", "browser.detach", "browser.allow_preview_port"} {
		t.Run(operation, func(t *testing.T) {
			command := permissionBrowserSummary(t, operation, permissionBrowserCall())
			for _, want := range []string{
				"Agent control", operation, `"desktop"`, `"epoch-1"`, `"tab-1"`, `"tab-gen-1"`, `"profile-1"`,
				`"attachment-1"`, `"attachment-gen-1"`, "Once", "no remembered rule", "SSH preview network — independent lifetime",
				`"saved-host"`, `"verified-runtime"`, "not SSH fingerprint", `"connection-1"`, `"environment-1"`,
				`"127.0.0.1"`, "[3000 8080]", "tab/environment lifetime", "detaching agent control does not",
			} {
				if !strings.Contains(command, want) {
					t.Errorf("missing %q:\n%s", want, command)
				}
			}
			if operation == "browser.open" && !strings.Contains(command, `Requested initial URL: "https://example.com/app"`) {
				t.Errorf("missing URL:\n%s", command)
			}
			if operation == "browser.allow_preview_port" && !strings.Contains(command, "Requested additional preview port: 9000") {
				t.Errorf("missing expansion:\n%s", command)
			}
			if operation == "browser.open" || operation == "browser.attach" {
				if !strings.Contains(command, "persists scoped to this agent until detach, revoke, or disconnect") {
					t.Errorf("missing control lifetime:\n%s", command)
				}
			}
			for _, secret := range []string{"private-grant-id", "credential-secret", "query-secret", "fragment-secret", "private-code-secret", "forged-provider", "/private/transport.sock", "ignored-local-path"} {
				if strings.Contains(command, secret) {
					t.Errorf("leaked %q:\n%s", secret, command)
				}
			}
		})
	}
}

func TestBrowserOpenPermissionDescribesProposedInitialNetworkScope(t *testing.T) {
	for _, ports := range [][]int{{5173}, {3000, 5173, 8080}} {
		call := permissionBrowserCall()
		call.Grant = Reference{}
		// The broker's resolved open scope includes 5173, even when the
		// earlier offered ports were empty or only [3000 8080].
		call.Scope.Preview.Ports = ports
		call.Arguments = json.RawMessage(`{"url":"http://127.0.0.1:5173/"}`)
		command := permissionBrowserSummary(t, "browser.open", call)
		for _, want := range []string{
			"Proposed preview ports (not yet approved for this new tab):", "5173", `"http://127.0.0.1:5173/"`,
			"This approval also authorizes the described SSH preview-network scope, including the initial URL port",
			"network access is not limited to one call", "detaching agent control does not",
		} {
			if !strings.Contains(command, want) {
				t.Errorf("missing %q:\n%s", want, command)
			}
		}
		for _, forbidden := range []string{"approved ports:", "Currently approved", "from this Once decision"} {
			if strings.Contains(command, forbidden) {
				t.Errorf("pending initial route falsely approved by %q:\n%s", forbidden, command)
			}
		}
	}
}

func TestBrowserPreviewExpansionPermissionSeparatesApprovedAndRequestedPorts(t *testing.T) {
	call := permissionBrowserCall()
	command := permissionBrowserSummary(t, "browser.allow_preview_port", call)
	for _, want := range []string{
		"Currently approved preview ports: [3000 8080]",
		"Requested additional preview port: 9000 (proposed; not yet approved)",
		"This approval also authorizes adding the requested port to the described SSH preview-network scope",
		"independent of the agent-control attachment", "detaching agent control does not",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("missing %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, "approved preview ports: [3000 8080 9000]") || strings.Contains(command, "from this Once decision") {
		t.Fatalf("expansion mislabeled already authorized:\n%s", command)
	}
	call.Arguments = json.RawMessage(`{"port":3000}`)
	command = permissionBrowserSummary(t, "browser.allow_preview_port", call)
	if !strings.Contains(command, "Requested preview port: 3000 (already in the approved scope; no expansion needed)") || strings.Contains(command, "proposed; not yet approved") {
		t.Fatalf("already approved port mislabeled as a new expansion:\n%s", command)
	}
}

func TestBrowserAttachPermissionDescribesExistingOfferedNetworkScope(t *testing.T) {
	call := permissionBrowserCall()
	call.Grant = Reference{}
	call.Arguments = json.RawMessage(`{"tab_id":"tab-1"}`)
	command := permissionBrowserSummary(t, "browser.attach", call)
	for _, want := range []string{
		"Offered existing-tab preview ports: [3000 8080]",
		"This approval also authorizes the agent to use the described existing SSH preview-network scope on the offered tab",
		"no additional ports are requested", "detaching agent control does not",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("missing %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, "Proposed preview ports") || strings.Contains(command, "Requested additional") || strings.Contains(command, "from this Once decision") {
		t.Fatalf("attach invented a new network expansion:\n%s", command)
	}
}

func TestBrowserPermissionSummaryRejectsUnresolvedEnvelopes(t *testing.T) {
	for _, raw := range []string{`{`, `{}`, `null`, `[]`, `{"url":"https://secret.example"}`, `{"scope":{"provider_id":"raw-secret"},"arguments":{}}`} {
		command, rules, ok := PermissionRule("browser.open", json.RawMessage(raw), "")
		if !strings.Contains(command, "unresolved or invalid") || strings.Contains(command, "secret") || rules != nil || ok {
			t.Fatalf("raw=%s command=%s rules=%v ok=%v", raw, command, rules, ok)
		}
	}
	for _, mutate := range []func(*BrowserCall){
		func(call *BrowserCall) { call.Scope.Preview.Ports = []int{8080, 3000} },
		func(call *BrowserCall) { call.Scope.Preview.Ports = make([]int, 65) },
		func(call *BrowserCall) { call.Scope.AttachmentGeneration = "" },
		func(call *BrowserCall) { call.Scope.Preview.HostIdentity = "" },
		func(call *BrowserCall) { call.Arguments = json.RawMessage(`null`) },
	} {
		call := permissionBrowserCall()
		mutate(&call)
		if command := permissionBrowserSummary(t, "browser.open", call); !strings.Contains(command, "unresolved or invalid") {
			t.Fatal(command)
		}
	}
}

func TestBrowserPermissionSummarySanitizesURLs(t *testing.T) {
	for _, raw := range []string{
		"file:///private/socket-secret", "unix:/private/socket-secret", "https://example.com/\ncontrol-secret", "https://example.com/\u202ebidi-secret",
		"not-a-url/path-secret", "http://%host/invalid-secret", "javascript:secret()",
	} {
		call := permissionBrowserCall()
		call.Arguments, _ = json.Marshal(map[string]any{"url": raw})
		command := permissionBrowserSummary(t, "browser.open", call)
		if strings.Contains(command, "secret") || strings.Contains(command, "/private/") {
			t.Fatalf("unsafe URL %q leaked:\n%s", raw, command)
		}
		if !strings.Contains(command, "invalid") {
			t.Fatalf("unsafe URL wasn't identified:\n%s", command)
		}
	}
	call := permissionBrowserCall()
	call.Scope.Preview = nil
	command := permissionBrowserSummary(t, "browser.open", call)
	if !strings.Contains(command, "grants no SSH preview-network authority") || strings.Contains(command, "saved-host") {
		t.Fatal(command)
	}
}

func TestBrowserPermissionSummaryQuotesBoundsAndRedactsLocalIdentities(t *testing.T) {
	call := permissionBrowserCall()
	call.Scope.ProviderID = "/private/provider.sock"
	call.Scope.TabID = "tab\nForged section\u202e\x1b"
	call.Scope.ProfileID = strings.Repeat("界", 3000) + "hidden-tail"
	call.Scope.Preview.HostID = "host\r\nspoof"
	call.Scope.Preview.EnvironmentID = "C:\\private\\socket-secret"
	call.Arguments, _ = json.Marshal(map[string]any{"url": "https://user:credential-secret@example.com/" + strings.Repeat("x", 9000) + "hidden-url-tail?token=query-secret#fragment-secret"})
	command := permissionBrowserSummary(t, "browser.open", call)
	for _, want := range []string{`tab\nForged section\u202e\x1b`, `host\r\nspoof`, "[truncated]", "non-opaque identity redacted"} {
		if !strings.Contains(command, want) {
			t.Errorf("missing quoted/bounded %q:\n%s", want, command)
		}
	}
	for _, forbidden := range []string{"/private", "socket-secret", "credential-secret", "query-secret", "fragment-secret", "hidden-tail", "hidden-url-tail", "\u202e", "\x1b", "tab\nForged", "host\r\nspoof"} {
		if strings.Contains(command, forbidden) {
			t.Errorf("unsafe %q in summary", forbidden)
		}
	}
	if len(command) > 12<<10 || !utf8.ValidString(command) {
		t.Fatalf("summary not bounded UTF-8: %d bytes", len(command))
	}
}
