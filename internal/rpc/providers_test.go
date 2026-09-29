package rpc_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/providerhost"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

type providerFixture struct {
	runtime   *runtime.Runtime
	client    *client.Client
	directory string
	url       string
	requests  atomic.Int32
	body      atomic.Value
	status    atomic.Int32
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	temporary, err := os.MkdirTemp("/tmp", "whip-provider-") //nolint:usetesting // The Unix socket limit is shorter than long t.TempDir test names.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(temporary); err != nil {
			t.Error(err)
		}
	})
	directory, err := filepath.EvalSymlinks(temporary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &providerFixture{directory: directory}
	f.body.Store(`{"data":[{"id":"model","name":"Exact model","context_length":64000,"max_completion_tokens":4096,"reasoning_efforts":[],"pricing":{"prompt":"9.007199254740993","completion":"0"}}]}`)
	f.status.Store(http.StatusOK)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer private-pasted-key" {
			t.Error("explicit credential did not reach intended endpoint")
		}
		w.WriteHeader(int(f.status.Load()))
		_, _ = io.WriteString(w, f.body.Load().(string))
	}))
	t.Cleanup(upstream.Close)
	f.url = upstream.URL + "/v1"
	f.runtime, err = runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	service, err := providerhost.New(t.Context(), f.runtime.HostConfiguration(), upstream.Client(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	server, err := rpc.Listen(f.runtime, rpc.HostServices{Config: f.runtime.HostConfiguration(), ProviderHost: service})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	f.client, err = client.Connect(t.Context(), f.runtime.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func createProviderFixture(t *testing.T, f *providerFixture) protocol.ProviderInventory {
	t.Helper()
	before := call[protocol.ProviderInventory](t, f.client, "providers.list", protocol.EmptyParams{})
	return call[protocol.ProviderInventory](t, f.client, "providers.create", protocol.ChangeProviderParams{Revision: before.Revision, Provider: "custom", Declaration: protocol.ProviderDeclaration{Kind: "openai-chat", BaseURL: f.url, Credential: &protocol.ProviderCredentialInput{Source: "file"}}, Key: &protocol.ProviderKeyPublication{ID: "stable-key", Key: "private-pasted-key"}})
}

func TestProviderSocketSetupCatalogDefaultsAndCredentialPrivacy(t *testing.T) {
	f := newProviderFixture(t)
	presets := call[protocol.ProviderPresetsResult](t, f.client, "providers.presets", protocol.EmptyParams{})
	if len(presets.Items) != 11 {
		t.Fatal("preset coverage lost")
	}
	bundled := call[protocol.ProviderModelsResult](t, f.client, "providers.bundled", protocol.ProviderParams{Provider: "inference-net"})
	if len(bundled.Items) == 0 || bundled.Items[0].MetadataSource != "bundled" {
		t.Fatal("offline metadata lost")
	}
	created := createProviderFixture(t, f)
	if created.Defaults != nil || len(created.Routes) != 1 || created.Routes[0].Credential.State != "available" || f.requests.Load() != 0 {
		t.Fatal("setup triggered network/default selection")
	}
	path := created.Routes[0].Credential.File
	local := call[protocol.ProviderCatalog](t, f.client, "providers.catalog", protocol.ProviderParams{Provider: "custom"})
	if local.State != "missing" || f.requests.Load() != 0 {
		t.Fatal("cache miss triggered discovery")
	}
	live := call[protocol.ProviderCatalog](t, f.client, "providers.refresh", protocol.ProviderParams{Provider: "custom"})
	if len(live.Models) != 1 || *live.Models[0].Prices.Input != 9007199254740993 || *live.Models[0].Prices.Output != 0 || live.Models[0].Prices.CachedInput != nil || live.Models[0].ReasoningEfforts == nil {
		t.Fatal("catalog lost exact metadata")
	}
	selected := protocol.ModelSelection{Provider: "custom", Name: "explicit-uncatalogued", Temperature: new(0.0)}
	settings := protocol.ProviderModelSettings{Prices: live.Models[0].Prices, ContextWindowTokens: new(protocol.Counter(64000)), MaxOutputTokens: 4096}
	configured := call[protocol.ProviderInventory](t, f.client, "providers.defaults", protocol.ProviderDefaultsParams{Revision: created.Revision, Defaults: protocol.ProviderDefaults{Selection: &selected, Settings: &settings}})
	if *configured.Routes[0].Models[selected.Name].Prices.Input != 9007199254740993 {
		t.Fatal("exact persisted price changed")
	}
	root := call[protocol.CreateTreeResult](t, f.client, "trees.create", protocol.CreateTreeParams{CreationID: protocol.ID(rand.Text()), Engine: "starlark", Definition: f.client.Builtins()[0], WorkingDirectory: t.TempDir()})
	if root.Root.Configuration.Model.Name != selected.Name || root.Root.Configuration.Model.Temperature == nil || *root.Root.Configuration.Model.Temperature != 0 {
		t.Fatal("new root missed current provider default")
	}
	ready := call[protocol.ProviderReadiness](t, f.client, "providers.readiness", protocol.ProviderReadinessParams{Selection: selected})
	if ready.ModelState != "configured" || ready.InferenceState != "not_tested" || f.requests.Load() != 1 {
		t.Fatal("readiness probed or overstated execution")
	}
	f.status.Store(http.StatusUnauthorized)
	f.body.Store("private-server-diagnostic")
	failed := call[protocol.ProviderCatalog](t, f.client, "providers.refresh", protocol.ProviderParams{Provider: "custom"})
	if len(failed.Models) != 1 || failed.Failure == nil || failed.State != "cached" {
		t.Fatal("failed discovery erased useful same-scope metadata")
	}
	var ignored protocol.ProviderInventory
	if err := f.client.Call(t.Context(), "providers.defaults", protocol.ProviderDefaultsParams{Revision: created.Revision, Defaults: protocol.ProviderDefaults{}}, &ignored); err == nil || !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatal("stale CAS succeeded", err)
	}
	compaction := call[protocol.ProviderInventory](t, f.client, "providers.compaction", protocol.ProviderDefaultsParams{Revision: configured.Revision, Defaults: protocol.ProviderDefaults{Selection: &selected}})
	cleared := call[protocol.ProviderInventory](t, f.client, "providers.compaction", protocol.ProviderDefaultsParams{Revision: compaction.Revision, Defaults: protocol.ProviderDefaults{}})
	removed := call[protocol.ProviderInventory](t, f.client, "providers.remove", protocol.RemoveProviderParams{Revision: cleared.Revision, Provider: "custom", Replacement: &protocol.ProviderDefaults{}})
	if removed.Defaults != nil || len(removed.Routes) != 0 {
		t.Fatal("explicit remove/clear failed")
	}
	if key, err := os.ReadFile(path); err != nil || string(key) != "private-pasted-key" {
		t.Fatal("route removal destroyed credential", err)
	}
	raw, _ := json.Marshal([]any{created, live, configured, ready, failed, removed})
	if strings.Contains(string(raw), "private-pasted-key") || strings.Contains(string(raw), "private-server-diagnostic") {
		t.Fatal("secret escaped safe public projections")
	}
	host, err := os.ReadFile(filepath.Join(f.directory, config.FileName))
	if err != nil || strings.Contains(string(host), "private-pasted-key") {
		t.Fatal("key entered host JSON", err)
	}
}

func TestProviderSocketBoundsCatalogBytesWithoutDroppingConnection(t *testing.T) {
	f := newProviderFixture(t)
	createProviderFixture(t, f)
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("model-%d", i), "name": strings.Repeat("<", 240), "pricing": map[string]string{"prompt": "9.007199254740993", "completion": "0"}}
	}
	raw, err := json.Marshal(map[string]any{"data": rows})
	if err != nil {
		t.Fatal(err)
	}
	f.body.Store(string(raw))
	live := call[protocol.ProviderCatalog](t, f.client, "providers.refresh", protocol.ProviderParams{Provider: "custom"})
	encoded, _ := json.Marshal(live)
	if len(live.Models) != 1024 || len(encoded) < 3<<19 || len(encoded) >= protocol.MaxFrameBytes-1024 || *live.Models[1023].Prices.Input != 9007199254740993 || *live.Models[1023].Prices.Output != 0 || live.Models[1023].Prices.CachedInput != nil {
		t.Fatal("bounded large catalog lost exact metadata or members")
	}
	for _, row := range rows {
		row["name"] = strings.Repeat("<", 512)
	}
	raw, _ = json.Marshal(map[string]any{"data": rows})
	f.body.Store(string(raw))
	failed := call[protocol.ProviderCatalog](t, f.client, "providers.refresh", protocol.ProviderParams{Provider: "custom"})
	encoded, _ = json.Marshal(failed)
	if failed.Failure == nil || len(failed.Models) != 1024 || len(encoded) >= protocol.MaxFrameBytes-1024 {
		t.Fatal("oversized catalog was silently truncated or exceeded wire capacity")
	}
	if value := call[protocol.ProviderInventory](t, f.client, "providers.list", protocol.EmptyParams{}); len(value.Routes) != 1 {
		t.Fatal("catalog size fault broke socket")
	}
}

func TestProviderInventoryNearHostByteLimitRetainsEveryRouteAndExactPrice(t *testing.T) {
	f := newProviderFixture(t)
	authority := f.runtime.HostConfiguration()
	before, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Update(t.Context(), before.Revision, func(host *config.Host) error {
		// Fill the actual host encoding near its publication limit, including
		// escaped model names, rather than assuming a per-route wire size.
		for i := range 128 {
			host.Providers[fmt.Sprintf("route-%03d", i)] = config.Provider{Kind: "openai-chat", BaseURL: f.url, CredentialSource: "none", Models: map[string]config.Model{}}
		}
		for i := range 1024 {
			provider := host.Providers[fmt.Sprintf("route-%03d", i%128)]
			name := fmt.Sprintf("model-%04d-%s", i, strings.Repeat("<", 128))
			provider.Models[name] = config.Model{Prices: session.ModelPrices{Input: new(int64(9007199254740993)), Output: new(int64(0))}, MaxOutputTokens: 4096}
			raw, err := json.MarshalIndent(host, "", "  ")
			if err != nil {
				return err
			}
			if len(raw) > session.MaxDocumentBytes-2048 {
				delete(provider.Models, name)
				break
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	host, err := os.ReadFile(filepath.Join(f.directory, config.FileName))
	if err != nil || len(host) < session.MaxDocumentBytes-4096 {
		t.Fatal("fixture did not reach publication byte boundary", len(host), err)
	}
	inventory := call[protocol.ProviderInventory](t, f.client, "providers.list", protocol.EmptyParams{})
	encoded, _ := json.Marshal(inventory)
	if len(inventory.Routes) != 128 || len(encoded) >= protocol.MaxFrameBytes-1024 || f.requests.Load() != 0 {
		t.Fatal("inventory truncated routes, exceeded wire capacity, or performed discovery")
	}
	for _, route := range inventory.Routes {
		if len(route.Models) == 0 {
			t.Fatal("inventory lost models")
		}
		for _, settings := range route.Models {
			if settings.Prices.Input == nil || *settings.Prices.Input != 9007199254740993 || settings.Prices.Output == nil || *settings.Prices.Output != 0 || settings.Prices.CachedInput != nil {
				t.Fatal("large inventory lost exact price presence")
			}
		}
	}
}

func TestProviderSocketReadsNeverExecuteCredentialCommandAndUpdateKeepsItPrivate(t *testing.T) {
	f := newProviderFixture(t)
	marker := filepath.Join(f.directory, "command-ran")
	executable := filepath.Join(f.directory, "credential-command")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf called > \"$1\"\nprintf private-pasted-key\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	before := call[protocol.ProviderInventory](t, f.client, "providers.list", protocol.EmptyParams{})
	created := call[protocol.ProviderInventory](t, f.client, "providers.create", protocol.ChangeProviderParams{
		Revision: before.Revision, Provider: "custom",
		Declaration: protocol.ProviderDeclaration{Kind: "openai-chat", BaseURL: f.url, Credential: &protocol.ProviderCredentialInput{Source: "command", Command: &protocol.ProviderCredentialCommand{Executable: executable, Arguments: []string{marker, "private-command-argument"}, Environment: []string{}}}},
	})
	updated := call[protocol.ProviderInventory](t, f.client, "providers.update", protocol.ChangeProviderParams{
		Revision: created.Revision, Provider: "custom", KeepCredential: true,
		Declaration: protocol.ProviderDeclaration{Kind: "openai-chat", BaseURL: f.url, Models: map[string]protocol.ProviderModelSettings{"model": {MaxOutputTokens: 100}}},
	})
	ready := call[protocol.ProviderReadiness](t, f.client, "providers.readiness", protocol.ProviderReadinessParams{Selection: protocol.ModelSelection{Provider: "custom", Name: "model"}})
	cached := call[protocol.ProviderCatalog](t, f.client, "providers.catalog", protocol.ProviderParams{Provider: "custom"})
	if _, err := os.Stat(marker); !os.IsNotExist(err) || f.requests.Load() != 0 || updated.Routes[0].Credential.State != "unchecked" || ready.CredentialState != "unchecked" || cached.ScopeState != "unverified" {
		t.Fatal("read-only setup inspected credentials by execution or claimed verification", err)
	}
	live := call[protocol.ProviderCatalog](t, f.client, "providers.refresh", protocol.ProviderParams{Provider: "custom"})
	if _, err := os.Stat(marker); err != nil || len(live.Models) != 1 || f.requests.Load() != 1 || live.ScopeState != "unverified" {
		t.Fatal("explicit refresh did not preserve command source and uncertainty", err)
	}
	public, _ := json.Marshal([]any{created, updated, ready, cached, live})
	for _, secret := range []string{executable, marker, "private-command-argument", "private-pasted-key"} {
		if strings.Contains(string(public), secret) {
			t.Fatal("credential command escaped public projection")
		}
	}
}

func TestProviderPreferenceFormsAndEnableStateOverSocket(t *testing.T) {
	f := newProviderFixture(t)
	created := createProviderFixture(t, f)
	selected := protocol.ModelSelection{Provider: "custom", Name: "chat"}
	saved := call[protocol.ProviderInventory](t, f.client, "providers.set_preferences", protocol.ProviderPreferencesParams{Revision: created.Revision, Defaults: protocol.ProviderDefaults{Selection: &selected}, PermissionMode: "automatic"})
	if saved.PermissionMode != "automatic" || saved.Defaults.Name != "chat" {
		t.Fatal("provider form fields did not publish together", saved)
	}
	values := protocol.ExecutionPreferences{Engine: "quickjs", CompactionPercent: 60, CompactionModel: protocol.ProviderDefaults{Selection: &selected}, MaxAttempts: 8, ImportClaude: false, ImportCodex: true}
	written := call[protocol.HostExecutionDefaults](t, f.client, "host.set_execution_preferences", protocol.SetExecutionPreferencesParams{ExpectedRevision: saved.Revision, Preferences: values})
	if written.MaxAttempts != 8 || written.GoalMaxContinuations != 100 || written.Preferences.GoalMaxContinuations != nil || written.Preferences.CompactionModel.Selection.Name != "chat" || written.Preferences.ImportClaude {
		t.Fatal("execution form lost values or default intent", written)
	}
	requireHistoryError(t, f.client, "providers.set_preferences", protocol.ProviderPreferencesParams{Revision: saved.Revision, Defaults: protocol.ProviderDefaults{}, PermissionMode: "prompt"}, "CONFLICT")
	disabled := call[protocol.ProviderInventory](t, f.client, "providers.set_enabled", protocol.SetProviderEnabledParams{Revision: written.Revision, Provider: "custom", Enabled: false})
	ready := call[protocol.ProviderReadiness](t, f.client, "providers.readiness", protocol.ProviderReadinessParams{Selection: selected})
	if !disabled.Routes[0].Disabled || !ready.Disabled || ready.CredentialState != "available" || f.requests.Load() != 0 {
		t.Fatal("disable erased credentials or contacted provider", ready)
	}
}

func TestProviderDisconnectSocketClearsOwnedKeyAndCatalog(t *testing.T) {
	f := newProviderFixture(t)
	created := createProviderFixture(t, f)
	call[protocol.ProviderCatalog](t, f.client, "providers.refresh", protocol.ProviderParams{Provider: "custom"})
	result := call[protocol.ProviderDisconnectResult](t, f.client, "providers.disconnect", protocol.DisconnectProviderParams{Revision: created.Revision, Provider: "custom"})
	if result.CredentialState != "cleared" || result.LocalFailure != nil || result.Inventory.Routes[0].Credential.State == "available" {
		t.Fatal("disconnect did not clear owned source", result)
	}
	if _, err := os.Stat(created.Routes[0].Credential.File); !os.IsNotExist(err) {
		t.Fatal("owned file survived", err)
	}
	catalog := call[protocol.ProviderCatalog](t, f.client, "providers.catalog", protocol.ProviderParams{Provider: "custom"})
	if catalog.State != "missing" || len(catalog.Models) != 0 || f.requests.Load() != 1 {
		t.Fatal("disconnect retained/refreshed catalog", catalog)
	}
	requireHistoryError(t, f.client, "providers.disconnect", protocol.DisconnectProviderParams{Revision: created.Revision, Provider: "custom"}, "CONFLICT")
}
