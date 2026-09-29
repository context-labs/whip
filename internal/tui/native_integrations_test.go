package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeIntegrationFixture(t *testing.T) *nativeModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	m, _ := nativeUIFixture(t)
	return m
}

func TestNativeIntegrationStatusAndPolicyNeverStartHelpers(t *testing.T) {
	m := nativeIntegrationFixture(t)
	marker := filepath.Join(t.TempDir(), "must-not-start")
	config := nativeMenuRPC[protocol.MCPConfiguration](t, m.connection, "mcp.configuration", protocol.EmptyParams{})
	nativeMenuRPC[protocol.MCPConfiguration](t, m.connection, "mcp.configure", protocol.ConfigureMCPParams{Revision: config.Revision, Name: "fixture", Server: &protocol.MCPServerInput{Command: []string{"/bin/sh", "-c", "echo started > \"$1\"", "fixture", marker}, Enabled: new(false), Env: map[string]string{}, Headers: map[string]string{}}})
	for _, text := range []string{"/lsp", "/lsp status", "/browser status", "/computer status", "/mcp", "/mcp import status"} {
		if result := nativeUIControl(t, m, text); result.err != nil || m.notice == "" {
			t.Fatal(text, result.err, m.status)
		}
	}
	if unavailable := nativeUIControl(t, m, "/browser tabs"); unavailable.err == nil || !strings.Contains(m.status, "inventory unavailable") {
		t.Fatal("absent desktop inventory was presented as available", unavailable, m.status)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("status started configured executable", err)
	}
	if result := nativeUIControl(t, m, "/computer allow Test App"); result.err != nil {
		t.Fatal(result.err)
	}
	allowed := nativeMenuRPC[protocol.ComputerStatus](t, m.connection, "computer.status", protocol.EmptyParams{})
	if allowed.State == "connected" || allowed.Configuration.Enabled || allowed.NativeConfigured || !slices.Contains(allowed.Configuration.Allow, "test app") {
		t.Fatal("policy edit started/enabled computer", allowed)
	}
	if result := nativeUIControl(t, m, "/computer deny TEST APP"); result.err != nil {
		t.Fatal(result.err)
	}
	denied := nativeMenuRPC[protocol.ComputerStatus](t, m.connection, "computer.status", protocol.EmptyParams{})
	if slices.Contains(denied.Configuration.Allow, "test app") || !slices.Contains(denied.Configuration.Deny, "test app") {
		t.Fatal("canonical app moved incorrectly", denied)
	}
	changed := nativeUIControl(t, m, "/browser driver chromedp")
	if changed.err != nil || !strings.Contains(m.notice, "chromedp") {
		t.Fatal(changed.err, m.notice)
	}
	if result := nativeUIControl(t, m, "/browser driver rod"); result.err != nil {
		t.Fatal(result.err)
	}
	// Host revisions are content hashes: returning to identical bytes is not a
	// conflict. Keep an unrelated change so the original captured revision is stale.
	if result := nativeUIControl(t, m, "/computer allow Another App"); result.err != nil {
		t.Fatal(result.err)
	}
	if result := changed.retry().(nativeControlResult); result.err == nil {
		t.Fatal("old driver CAS was rebased")
	}
	driver := nativeMenuRPC[protocol.HostBrowserDriver](t, m.connection, "host.browser_driver", protocol.EmptyParams{})
	if driver.Driver != "rod" {
		t.Fatal(driver)
	}
}

func TestNativeMCPImportPolicyPreservesFiltersAndChildConnectionFailsWithoutReplay(t *testing.T) {
	m := nativeIntegrationFixture(t)
	config := nativeMenuRPC[protocol.MCPConfiguration](t, m.connection, "mcp.configuration", protocol.EmptyParams{})
	policy := config.Imports
	policy.Claude = &protocol.MCPImportSource{Only: []string{"permitted"}, Exclude: []string{"blocked"}, Enabled: new(false)}
	nativeMenuRPC[protocol.MCPConfiguration](t, m.connection, "mcp.configure", protocol.ConfigureMCPParams{Revision: config.Revision, Imports: &policy})
	changed := nativeUIControl(t, m, "/mcp import claude on")
	if changed.err != nil {
		t.Fatal(changed.err)
	}
	updated := nativeMenuRPC[protocol.MCPConfiguration](t, m.connection, "mcp.configuration", protocol.EmptyParams{})
	if updated.Imports.Claude == nil || updated.Imports.Claude.Enabled == nil || !*updated.Imports.Claude.Enabled || !slices.Equal(updated.Imports.Claude.Only, policy.Claude.Only) || !slices.Equal(updated.Imports.Claude.Exclude, policy.Claude.Exclude) {
		t.Fatal("source policy lost existing restrictions", updated)
	}
	if result := nativeUIControl(t, m, "/mcp import claude off"); result.err != nil {
		t.Fatal(result.err)
	}
	if result := nativeUIControl(t, m, "/computer allow Another App"); result.err != nil {
		t.Fatal(result.err)
	}
	if repeated := changed.retry().(nativeControlResult); repeated.err == nil {
		t.Fatal("old host policy CAS was rebased")
	}
	spawned := nativeMenuRPC[protocol.SpawnSessionResult](t, m.connection, "sessions.spawn", protocol.SpawnSessionParams{ParentID: m.owner.ID, Identity: protocol.RequestIdentity{ClientID: "integrations", RequestID: "child"}, GrantIDs: []protocol.ID{}, Parts: []protocol.Part{{Type: "text", Text: "child input"}}})
	nativeNavigate(t, m, string(spawned.Session.ID))
	if result := nativeUIControl(t, m, "/mcp status"); result.err != nil {
		t.Fatal(result.err)
	}
	refused := nativeUIControl(t, m, "/mcp missing reconnect")
	if refused.err == nil || refused.retry != nil || m.retryControl != nil || !strings.Contains(m.status, "/mcp status") {
		t.Fatal("connection action offered unbound replay", refused, m.status)
	}
}

func TestNativeComputerTaskUsesOrdinaryInputAndPermissions(t *testing.T) {
	m := nativeIntegrationFixture(t)
	m.input.SetValue("/computer-use arrange the test window")
	command := m.submit()
	if command == nil {
		t.Fatal(m.status)
	}
	value := command().(nativeSubmission)
	m.Update(value)
	if value.err != nil || value.admission.Input == nil || value.admission.Input.Kind != "prompt" || value.admission.Input.Source != "user" || !strings.Contains(value.admission.Input.Parts[0].Text, "arrange the test window") || strings.Contains(value.admission.Input.Parts[0].Text, "computer_exec") {
		t.Fatal(value)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := value.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	status := nativeMenuRPC[protocol.ComputerStatus](t, m.connection, "computer.status", protocol.EmptyParams{})
	if status.State == "connected" || status.Configuration.Enabled {
		t.Fatal("task bypassed explicit host availability", status)
	}
}
