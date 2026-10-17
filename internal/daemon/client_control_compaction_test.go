package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestClientCompactionConfigurationUsesProviderValidation(t *testing.T) {
	for _, test := range []struct {
		name         string
		model        string
		provider     string
		legacy       bool
		wantProvider string
		wantErr      bool
	}{
		{name: "automatic", provider: "stale"},
		{name: "alias", model: "summary", wantProvider: "ready"},
		{name: "catalog", model: "catalog-only", wantProvider: "ready"},
		{name: "missing credentials", model: "offline-summary", wantErr: true},
		{name: "wrong provider", model: "summary", provider: "other", wantErr: true},
		{name: "disabled provider", model: "disabled-summary", wantErr: true},
		{name: "ambiguous route", model: "ambiguous", wantErr: true},
		{name: "unchanged legacy", model: "deepseek-v4-flash-0731", provider: "missing", legacy: true, wantProvider: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			providers, cfg := compactionConfigurationFixture(t)
			if test.legacy {
				cfg.CompactModel, cfg.CompactProvider = test.model, test.provider
				if err := cfg.Save(); err != nil {
					t.Fatal(err)
				}
			}
			before, err := providers.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			rootID := createRoot(t, store)
			processes := newTestProcesses(t)
			owner, err := New(store, processes, func(context.Context, session.Meta, []llm.Message) (Components, error) {
				return Components{Runner: &fakeRunner{}}, nil
			}, providers)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })
			root, err := owner.Open(rootID)
			if err != nil {
				t.Fatal(err)
			}
			result := clientCommand(t, root, "tui", "configure", "compaction.configure", map[string]string{
				"model": test.model, "provider": test.provider,
			})
			if (result.Status == "failed") != test.wantErr {
				t.Fatalf("configuration command = %+v, wantErr %t", result, test.wantErr)
			}
			after, err := providers.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			if test.wantErr {
				if after.Revision != before.Revision {
					t.Fatal("failed command changed configuration")
				}
				return
			}
			if result.Status != "succeeded" {
				t.Fatalf("configuration command = %+v", result)
			}
			var settings protocol.CompactionSettingsResult
			if err := json.Unmarshal([]byte(result.Output), &settings); err != nil {
				t.Fatal(err)
			}
			if settings.Model != test.model || settings.Provider != test.wantProvider || settings.BuiltinDefault != (test.model == "") {
				t.Fatalf("command settings = %+v", settings)
			}
			if after.CompactModel != settings.Model || after.CompactProvider != settings.Provider {
				t.Fatal("command result did not match saved route")
			}
		})
	}
}
