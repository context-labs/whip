package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func TestV2CreateSessionUsesHostDefaults(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			cfg := config.Default()
			cfg.DefaultModel = "host-alias"
			cfg.DefaultProvider = "host-provider"
			cfg.Models["host-alias"] = config.Model{ID: "provider-api-id", Providers: []string{"host-provider"}}
			cfg.Providers["host-provider"] = config.Provider{BaseURL: "http://localhost:1"}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			fixture := newV2Fixture(t, &fakeRunner{})
			client := fixture.dial(transport, "default-client")
			raw, _ := json.Marshal(CreateSession{Kind: "agent", CWD: t.TempDir()})
			params := CommandParams{CommandID: "default-create", Scope: "daemon", Operation: "session.create", Payload: raw}
			result, err := client.Command(t.Context(), params)
			if err != nil || result.Status != "succeeded" {
				t.Fatalf("create: %+v %v", result, err)
			}
			var created protocol.RootIDResult
			if err := json.Unmarshal(result.Result, &created); err != nil {
				t.Fatal(err)
			}
			snap, err := client.Snapshot(context.Background(), created.RootID)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Meta.Model != "host-alias" || snap.Meta.Provider != "host-provider" {
				t.Fatalf("route=%s/%s", snap.Meta.Model, snap.Meta.Provider)
			}
			cfg.DefaultModel = "removed-default"
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			retry, err := client.Command(t.Context(), params)
			if err != nil || string(retry.Result) != string(result.Result) {
				t.Fatalf("retry changed accepted route: %+v %v", retry, err)
			}
		})
	}
}

func TestSessionDefaultsPreserveRoutingChoices(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := config.Default()
	cfg.DefaultModel = "alias"
	cfg.DefaultProvider = "default"
	cfg.Models["alias"] = config.Model{Providers: []string{"default"}}
	cfg.Providers["default"] = config.Provider{BaseURL: "http://localhost:1"}
	cfg.Providers["override"] = config.Provider{BaseURL: "http://localhost:2"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{"override": {Models: []config.ModelInfoLite{{ID: "catalog-only"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, model, provider, wantModel, wantProvider string }{
		{"model only", "alias", "", "alias", "default"},
		{"provider only", "", "override", "alias", "override"},
		{"catalog owner", "catalog-only", "", "catalog-only", "override"},
		{"explicit route", "explicit", "override", "explicit", "override"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSessionDefaults(CreateSession{Kind: "agent", Model: tc.model, Provider: tc.provider})
			if err != nil || got.Model != tc.wantModel || got.Provider != tc.wantProvider {
				t.Fatalf("route=%+v error=%v", got, err)
			}
		})
	}
	got, err := resolveSessionDefaults(CreateSession{Kind: "tool_host"})
	if err != nil || got.Model != "" || got.Provider != "" {
		t.Fatalf("tool host gained model: %+v %v", got, err)
	}
}
