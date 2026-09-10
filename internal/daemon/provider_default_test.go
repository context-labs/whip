package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestModelSelectionAppliesAndPersistsEffortTogether(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := config.Default()
	cfg.DefaultEffort = "high"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	var mu sync.Mutex
	var built []session.Meta
	owner, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		mu.Lock()
		built = append(built, meta)
		mu.Unlock()
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = owner.Close()
		}
	})
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	for i, effort := range []string{"medium", "high"} {
		result := clientCommand(t, root, "tui", "selection-"+effort, "session.model", map[string]any{"model": "gpt-6-astra", "provider": "openai", "effort": effort})
		meta, _, loadErr := store.Load(rootID)
		if result.Status != "succeeded" || loadErr != nil || meta.Model != "gpt-6-astra" || meta.Provider != "openai" || meta.Effort != effort {
			t.Fatalf("selection=%+v meta=%+v err=%v", result, meta, loadErr)
		}
		mu.Lock()
		if len(built) != i+2 || built[len(built)-1].Effort != effort {
			t.Errorf("replacement did not receive effort before construction: %+v", built)
		}
		mu.Unlock()
	}
	result := clientCommand(t, root, "tui", "invalid-effort", "session.model", map[string]any{"model": "other", "provider": "openai", "effort": "invalid"})
	meta, _, err := store.Load(rootID)
	if result.Status != "failed" || err != nil || meta.Model != "gpt-6-astra" || meta.Effort != "high" {
		t.Fatalf("invalid choice mutated persisted state: %+v %+v %v", result, meta, err)
	}
	saved, err := config.Load()
	if err != nil || saved.DefaultModel != cfg.DefaultModel || saved.DefaultEffort != cfg.DefaultEffort {
		t.Fatal("session-only selection rewrote global defaults")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	store = openStore(t, path)
	resumed, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		if meta.Model != "gpt-6-astra" || meta.Provider != "openai" || meta.Effort != "high" {
			t.Errorf("resume lost selection: %+v", meta)
		}
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if _, err := resumed.Open(rootID); err != nil {
		t.Fatal(err)
	}
}
