package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestClientValidationFailuresAreDurableAndLeaveSettingsUntouched(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := config.Default()
	cfg.DefaultEffort = "high"
	cfg.Providers["provider"] = config.Provider{BaseURL: "https://example.test", APIKey: "fixture-key"}
	cfg.Models["model"] = config.Model{ID: "api-model", Providers: []string{"provider"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{"provider": {Models: []config.ModelInfoLite{{ID: "api-model", ReasoningEfforts: []string{"low"}}}}}); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &controlSurfaceRunner{fakeRunner: &fakeRunner{}}
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	for i, test := range []struct {
		operation string
		payload   map[string]any
		want      string
	}{
		{"cancel", map[string]any{"target_command_id": "work", "turn_id": "turn"}, "one cancellation target"},
		{"cancel", map[string]any{"target_command_id": 1}, "cannot unmarshal"},
		{"session.model", map[string]any{"model": 1}, "cannot unmarshal"},
		{"session.model", map[string]any{}, "model is required"},
		{"session.effort", map[string]any{}, "effort is required"},
		{"session.effort", map[string]any{"effort": "invented"}, "unknown effort"},
		{"session.effort", map[string]any{"effort": "high", "persist_default": true}, "does not support"},
		{"session.preview", map[string]any{}, "requires an ID"},
		{"mcp.enable", map[string]any{"name": 1}, "cannot unmarshal"},
		{"mcp.enable", map[string]any{"name": "unavailable"}, "unavailable"},
		{"mcp.import.configure", map[string]any{"source": "unknown"}, "claude|codex"},
		{"goal.from-context", map[string]any{}, "does not support"},
	} {
		id := fmt.Sprintf("invalid-%d", i)
		result := clientCommand(t, root, "validation", id, test.operation, test.payload)
		if result.Status != "failed" || !strings.Contains(result.Error, test.want) {
			t.Fatalf("%s result=%+v, want %q", test.operation, result, test.want)
		}
		retry := clientCommand(t, root, "validation", id, test.operation, test.payload)
		if !reflect.DeepEqual(retry, result) {
			t.Fatalf("failed command outcome changed on retry: %+v vs %+v", retry, result)
		}
	}
	saved, err := config.Load()
	if err != nil || !reflect.DeepEqual(saved, cfg) || runner.effort != "" {
		t.Fatalf("failed command changed configuration or runtime effort: %v %q", err, runner.effort)
	}
	accepted := clientCommand(t, root, "validation", "valid-effort", "session.effort", map[string]any{"effort": "low"})
	if accepted.Status != "succeeded" || runner.effort != "low" {
		t.Fatalf("catalog-supported effort did not apply: %+v %q", accepted, runner.effort)
	}
	meta, _, err := store.Load(rootID)
	if err != nil || meta.Effort != "low" {
		t.Fatalf("effort did not persist in session: %+v %v", meta, err)
	}
	if cfg, err := config.Load(); err != nil || cfg.DefaultEffort != "high" {
		t.Fatalf("session effort overwrote global default: %+v %v", cfg, err)
	}
}

func TestClientModelReplacementPersistsDefaultWithoutForgettingExplicitOff(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Providers["provider"] = config.Provider{BaseURL: "https://example.test", APIKey: "fixture-key"}
	cfg.Models["replacement"] = config.Model{ID: "replacement-api", Providers: []string{"provider"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	if err := store.SetEffort(rootID, "off"); err != nil {
		t.Fatal(err)
	}
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	result := clientCommand(t, root, "model-default", "remember-replacement", "session.model", map[string]any{"model": "replacement", "provider": "provider", "persist_default": true})
	if result.Status != "succeeded" {
		t.Fatalf("model replacement=%+v", result)
	}
	meta, _, err := store.Load(rootID)
	if err != nil || meta.Model != "replacement" || meta.Effort != "off" {
		t.Fatalf("replacement forgot explicit off: %+v %v", meta, err)
	}
	saved, err := config.Load()
	if err != nil || saved.DefaultModel != "replacement" || saved.DefaultProvider != "provider" {
		t.Fatalf("replacement did not persist defaults: %+v %v", saved, err)
	}
}
