package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

type providerConfigurationClient interface {
	ReadConfiguration(context.Context) (RuntimeConfiguration, error)
	ListProviders(context.Context) (protocol.ProviderList, error)
	ListProvidersFor(context.Context, string, string) (protocol.ProviderList, error)
	ProviderCatalogs(context.Context, bool) (protocol.ProviderCatalogsResult, error)
	ProviderCatalogsFor(context.Context, string, bool) (protocol.ProviderCatalogsResult, error)
	ReadProvider(context.Context, string) (protocol.ProviderConfiguration, error)
	CreateProvider(context.Context, protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error)
	UpdateProvider(context.Context, protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error)
	DisconnectProvider(context.Context, protocol.ProviderDisconnectParams) (ProviderStatus, error)
	RemoveProvider(context.Context, protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error)
}

func TestProviderClientsCustomConnectionLifecycle(t *testing.T) {
	for _, mode := range []string{"client", "root"} {
		t.Run(mode, func(t *testing.T) {
			server, client, root, rootID := providerBehaviorFixture(t)
			var api providerConfigurationClient = client
			if mode == "root" {
				api = root
			}
			server.providers.validate = func(_ context.Context, endpoint, key string) ([]llm.ModelInfo, error) {
				if endpoint != "https://example.test/v1" || key != "private-fixture-key" {
					return nil, errors.New("incorrect discovery credentials")
				}
				return []llm.ModelInfo{{ID: "fixture-chat", SupportsTools: new(true), OutputModalities: []string{"text"}}}, nil
			}
			before, err := api.ReadConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			created, err := api.CreateProvider(t.Context(), protocol.ProviderCreateParams{
				Revision: before.Revision, Provider: "custom-fixture",
				Definition: protocol.ProviderDefinition{Name: "Fixture", BaseURL: "https://example.test/v1", API: "openai-completions"},
				Credential: protocol.ProviderCredential{Mode: "api_key", Key: "private-fixture-key"},
			})
			if err != nil || !created.Custom || created.Discovery == nil || created.Discovery.Status != "loaded" || created.Discovery.ModelCount != 1 {
				t.Fatalf("create result: %+v %v", created, err)
			}
			read, err := api.ReadProvider(t.Context(), created.Provider)
			if err != nil || read.Revision != created.Revision || read.Definition != created.Definition || read.Credential.Mode != "api_key" || !read.Credential.Configured {
				t.Fatalf("read result: %+v %v", read, err)
			}
			listed, err := api.ListProviders(t.Context())
			if err != nil || !slices.ContainsFunc(listed.Providers, func(p protocol.ProviderEntry) bool { return p.ID == created.Provider && p.Custom }) {
				t.Fatalf("custom connection missing from list: %+v %v", listed, err)
			}
			selection, err := api.ListProvidersFor(t.Context(), "fixture-chat", created.Provider)
			if err != nil || selection.Selection == nil || !selection.Selection.Ready || selection.Selection.Provider != created.Provider {
				t.Fatalf("selection is not ready: %+v %v", selection, err)
			}
			catalog, err := api.ProviderCatalogs(t.Context(), false)
			if err != nil || len(catalog.Catalogs[created.Provider].Models) != 1 {
				t.Fatalf("saved discovery absent from catalogs: %+v %v", catalog, err)
			}
			filtered, err := api.ProviderCatalogsFor(t.Context(), created.Provider, false)
			if err != nil || !reflect.DeepEqual(filtered.Catalogs[created.Provider], catalog.Catalogs[created.Provider]) {
				t.Fatalf("provider catalog differs: %+v %v", filtered, err)
			}
			updated, err := api.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Revision: read.Revision, Provider: read.Provider, Name: new("Renamed fixture")})
			if err != nil || updated.Definition.Name != "Renamed fixture" || updated.Revision == read.Revision {
				t.Fatalf("metadata update: %+v %v", updated, err)
			}
			if _, err := api.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Revision: read.Revision, Provider: read.Provider, Name: new("Stale edit")}); err == nil {
				t.Fatal("stale client overwrote the connection")
			}
			disconnected, err := api.DisconnectProvider(t.Context(), protocol.ProviderDisconnectParams{Revision: updated.Revision, Provider: read.Provider})
			if err != nil || !disconnected.Disabled || disconnected.KeySource != "none" || disconnected.Available == nil || *disconnected.Available {
				t.Fatalf("disconnect retained credentials or readiness: %+v %v", disconnected, err)
			}
			after, err := api.ReadConfiguration(t.Context())
			if err != nil || after.DefaultModel != before.DefaultModel || after.DefaultProvider != before.DefaultProvider {
				t.Fatalf("connection edit changed defaults: %+v %v", after, err)
			}
			removed, err := api.RemoveProvider(t.Context(), protocol.ProviderRemoveParams{Revision: after.Revision, Provider: read.Provider})
			if err != nil || removed.Revision == after.Revision {
				t.Fatalf("remove result: %+v %v", removed, err)
			}
			if _, err := api.ReadProvider(t.Context(), read.Provider); err == nil {
				t.Fatal("removed connection remains readable")
			}
			persisted, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, exists := persisted.Providers[read.Provider]; exists {
				t.Fatal("removed connection remains on disk")
			}
			replay, err := client.Replay(t.Context(), ReplayParams{RootID: rootID, Limit: 1000})
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []any{created, read, listed, selection, catalog, filtered, updated, disconnected, after, removed, replay} {
				body, err := json.Marshal(value)
				if err != nil || strings.Contains(string(body), "private-fixture-key") {
					t.Fatalf("secret reached a public response or journal: %v", err)
				}
			}
		})
	}
}
