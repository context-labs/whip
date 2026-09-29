package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func nativeBrowserStatusCLI(t *testing.T) protocol.ExternalBrowserStatus {
	t.Helper()
	var out bytes.Buffer
	if err := nativeBrowserCLI([]string{"status", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	var status protocol.ExternalBrowserStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestNativeBrowserCLIExplicitCASAndLostConnectionAcknowledgement(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "chromedp")
	requests := new(runRequests)
	r := runFixture(t, "unused", requests)
	before := nativeBrowserStatusCLI(t)
	if !before.DriverPinned || before.Driver != "chromedp" || before.Configuration.Mode != "disabled" {
		t.Fatal(before)
	}
	var out bytes.Buffer
	args := []string{"configure", "--revision", before.Revision, "--mode", "headless", "--executable", "/not/probed/Chrome for Testing", "--allow-private-urls"}
	if err := nativeBrowserCLI(args, &out); err != nil {
		t.Fatal(err)
	}
	configured := nativeBrowserStatusCLI(t)
	if configured.Configuration.Executable != args[6] || !configured.Configuration.AllowPrivateURLs || configured.Driver != "chromedp" || !configured.DriverPinned {
		t.Fatal(configured, args)
	}
	if err := nativeBrowserCLI(args, &out); err == nil || !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatal("old host revision rebased", err)
	}
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := r.CreateTree(t.Context(), store.CreateTree{Definition: refs[0], WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{AutomaticTitle: new(false), GoalsEnabled: new(false)}})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := r.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "browser-cli", RequestID: "prepare"}, root.ID, session.HostOperation{Module: "browser", Name: "run", Arguments: json.RawMessage(`{"session":"default","code":"info()"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var original protocol.ExternalBrowserSession
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out.Reset()
		if err := nativeBrowserCLI([]string{"list", "--json", string(root.ID)}, &out); err != nil {
			t.Fatal(err)
		}
		var list protocol.ExternalBrowserSessions
		if err := json.Unmarshal(out.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) == 1 {
			original = list.Items[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if original.Generation == "" || original.State != "prepared" {
		t.Fatal("permissioned direct input did not prepare a connection", original)
	}
	if _, err := r.CancelInput(t.Context(), admission.Input.ID); err != nil {
		t.Fatal(err)
	}
	socket, writes := nativeControlFaultProxy(t, r.SocketPath(), "browser.reconnect_external", true)
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) { return client.Connect(ctx, socket, nil) }
	if err := nativeBrowserCLI([]string{"reconnect", string(root.ID), original.Name, string(original.Generation)}, &out); err == nil || !strings.Contains(err.Error(), "browser list") || writes.Load() != 1 {
		t.Fatal("lost generation acknowledgement was not reported", err, writes.Load())
	}
	rows, err := r.ExternalBrowserSessions(t.Context(), root.ID)
	if err != nil || len(rows) != 1 || rows[0].Generation == string(original.Generation) || rows[0].State != "prepared" {
		t.Fatal(rows, err)
	}
	out.Reset()
	if err := nativeBrowserCLI([]string{"list", "--json", string(root.ID)}, &out); err != nil || !strings.Contains(out.String(), rows[0].Generation) || writes.Load() != 1 {
		t.Fatal("read-only recovery replayed a reconnect", err, out.String(), writes.Load())
	}
	if err := nativeBrowserCLI([]string{"disconnect", string(root.ID), original.Name, string(original.Generation)}, &out); err == nil || !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatal("stale generation disconnected replacement", err)
	}
	if err := nativeBrowserCLI([]string{"disconnect", string(root.ID), rows[0].Name, rows[0].Generation}, &out); err != nil {
		t.Fatal(err)
	}
	rows, err = r.ExternalBrowserSessions(t.Context(), root.ID)
	if err != nil || len(rows) != 1 || rows[0].State != "ended" {
		t.Fatal(rows, err)
	}
	if len(requests.list()) != 0 {
		t.Fatal("human browser controls invoked a provider")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.SocketPath()), "browser")); !os.IsNotExist(err) {
		t.Fatal("passive controls opened browser/profile", err)
	}
}

func TestNativeBrowserCLILostConfigurationAcknowledgementOnlyReadsEvidence(t *testing.T) {
	r := runFixture(t, "unused", nil)
	before := nativeBrowserStatusCLI(t)
	socket, writes := nativeControlFaultProxy(t, r.SocketPath(), "host.set_external_browser", true)
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) { return client.Connect(ctx, socket, nil) }
	args := []string{"configure", "--revision", before.Revision, "--mode", "extension"}
	var out bytes.Buffer
	if err := nativeBrowserCLI(args, &out); err == nil || !strings.Contains(err.Error(), "browser status") || writes.Load() != 1 {
		t.Fatal(err, writes.Load())
	}
	current := nativeBrowserStatusCLI(t)
	if current.Configuration.Mode != "extension" || current.Revision == before.Revision || writes.Load() != 1 {
		t.Fatal("read replayed or lost accepted configuration", current, writes.Load())
	}
	if err := nativeBrowserCLI(args, &out); err == nil || !strings.Contains(err.Error(), "CONFLICT") || writes.Load() != 2 {
		t.Fatal("explicit original edit was rebased", err, writes.Load())
	}
}
