package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestV2CreateSessionUsesHostDefaults(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			cfg := config.Default()
			cfg.DefaultModel = "host-alias"
			cfg.DefaultProvider = "host-provider"
			cfg.DefaultPermissionMode = session.PermissionModeAutomatic
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
			if snap.PermissionMode != session.PermissionModeAutomatic {
				t.Fatalf("permission mode = %q", snap.PermissionMode)
			}
			cfg.DefaultPermissionMode = session.PermissionModePrompt
			cfg.DefaultModel = "removed-default"
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			retry, err := client.Command(t.Context(), params)
			if err != nil || string(retry.Result) != string(result.Result) {
				t.Fatalf("retry changed accepted route: %+v %v", retry, err)
			}
			snap, err = client.Snapshot(t.Context(), created.RootID)
			if err != nil || snap.PermissionMode != session.PermissionModeAutomatic {
				t.Fatalf("host update changed existing session: %+v %v", snap, err)
			}
		})
	}
}

func TestV2CreatePermissionDefaultsWithExplicitRouting(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			fixture := newV2Fixture(t, &fakeRunner{})
			client := fixture.dial(transport, "permission-default-client")
			for _, tc := range []struct {
				name, host, override, want string
				kind                       session.SessionKind
			}{
				{"legacy", "", "", "prompt", session.SessionKindAgent},
				{"inherit", "automatic", "", "automatic", session.SessionKindAgent},
				{"ask override", "automatic", "prompt", "prompt", session.SessionKindAgent},
				{"automatic override", "prompt", "automatic", "automatic", session.SessionKindAgent},
				{"tool host", "automatic", "", "prompt", session.SessionKindToolHost},
			} {
				t.Run(tc.name, func(t *testing.T) {
					cfg := config.Default()
					cfg.DefaultPermissionMode = tc.host
					if err := cfg.Save(); err != nil {
						t.Fatal(err)
					}
					create := CreateSession{
						Kind: tc.kind, CWD: t.TempDir(), Model: "explicit-model", Provider: "explicit-provider",
						ExecutionEngine: "starlark", PermissionMode: tc.override,
					}
					if tc.kind == session.SessionKindToolHost {
						create.Model, create.Provider = "", ""
					}
					raw, err := json.Marshal(create)
					if err != nil {
						t.Fatal(err)
					}
					result, err := client.Command(t.Context(), CommandParams{
						CommandID: tc.name, Scope: "daemon", Operation: "session.create", Payload: raw,
					})
					if err != nil || result.Status != "succeeded" {
						t.Fatalf("create: %+v %v", result, err)
					}
					var created protocol.RootIDResult
					if err := json.Unmarshal(result.Result, &created); err != nil {
						t.Fatal(err)
					}
					snap, err := client.Snapshot(t.Context(), created.RootID)
					if err != nil || snap.PermissionMode != tc.want {
						t.Fatalf("permission mode: %+v %v, want %q", snap, err, tc.want)
					}
				})
			}
		})
	}
}

func TestSessionPermissionDefaultRetrySurvivesRestart(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	database := filepath.Join(t.TempDir(), "sessions.db")
	admission := session.CommandAdmission{
		ClientID: "client", CommandID: "create", RequestDigest: "stable",
		Payload: session.RuntimePayload{Data: []byte(`{}`)},
	}
	create := CreateSession{
		Kind: session.SessionKindAgent, CWD: t.TempDir(), Model: "model", Provider: "provider", ExecutionEngine: "starlark",
	}
	var original session.CommandRecord
	for _, mode := range []string{"automatic", "prompt"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Default()
			cfg.DefaultPermissionMode = mode
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			store := openStore(t, database)
			ctx, cancel := context.WithCancel(t.Context())
			control := newControl(ctx, store)
			t.Cleanup(func() {
				cancel()
				<-control.done
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			record, err := control.CreateSession(t.Context(), admission, create)
			if err != nil || record.Status != "succeeded" {
				t.Fatalf("create: %+v %v", record, err)
			}
			if mode == "automatic" {
				original = record
			} else if string(record.Outcome.Inline) != string(original.Outcome.Inline) {
				t.Fatal("retry after restart changed accepted session")
			}
			var created protocol.RootIDResult
			if err := json.Unmarshal(record.Outcome.Inline, &created); err != nil {
				t.Fatal(err)
			}
			got, err := store.PermissionMode(t.Context(), created.RootID)
			if err != nil || got != "automatic" {
				t.Fatalf("saved permission = %q, %v", got, err)
			}
		})
	}
}

func TestSessionDefaultsPermissionMode(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	for _, hostMode := range []string{"", "prompt", "automatic", "invalid"} {
		cfg := config.Default()
		cfg.DefaultPermissionMode = hostMode
		if err := cfg.Save(); err != nil {
			t.Fatal(err)
		}
		for _, override := range []string{"", "prompt", "automatic"} {
			t.Run("host="+hostMode+"/override="+override, func(t *testing.T) {
				got, err := resolveSessionDefaults(t.Context(), nil, CreateSession{
					Kind: session.SessionKindAgent, Model: "explicit-model", Provider: "explicit-provider",
					ExecutionEngine: "starlark", PermissionMode: override,
				})
				if err != nil {
					t.Fatal(err)
				}
				want := override
				if want == "" {
					want = "prompt"
					if hostMode == "automatic" {
						want = "automatic"
					}
				}
				if got.PermissionMode != want {
					t.Fatalf("permission mode = %q, want %q", got.PermissionMode, want)
				}
			})
		}
		got, err := resolveSessionDefaults(t.Context(), nil, CreateSession{Kind: session.SessionKindToolHost})
		if err != nil || got.PermissionMode != "" {
			t.Fatalf("tool host gained permission mode: %+v %v", got, err)
		}
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
	if err := config.SaveCatalogs(map[string]config.Catalog{"override": {BaseURL: "http://localhost:2", Models: []config.ModelInfoLite{{ID: "catalog-only"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, model, provider, wantModel, wantProvider string }{
		{"model only", "alias", "", "alias", "default"},
		{"provider only", "", "override", "alias", "override"},
		{"catalog owner", "catalog-only", "", "catalog-only", "override"},
		{"explicit route", "explicit", "override", "explicit", "override"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSessionDefaults(t.Context(), nil, CreateSession{Kind: "agent", Model: tc.model, Provider: tc.provider})
			if err != nil || got.Model != tc.wantModel || got.Provider != tc.wantProvider {
				t.Fatalf("route=%+v error=%v", got, err)
			}
		})
	}
	got, err := resolveSessionDefaults(t.Context(), nil, CreateSession{Kind: "tool_host"})
	if err != nil || got.Model != "" || got.Provider != "" {
		t.Fatalf("tool host gained model: %+v %v", got, err)
	}
}
