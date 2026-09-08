package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestClientCompactionRetryRestoresHistoryOnceAndKeepsEarlierSummary(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	history := []llm.Message{{Role: "user", Content: "first", Authored: true}, {Role: "assistant", Content: "answer"}, {Role: "user", Content: "second", Authored: true}, {Role: "assistant", Content: "latest answer"}}
	if err := store.Save(rootID, 1, history, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for _, compact := range []struct {
		cutoff int
		text   string
	}{{1, "earlier summary"}, {3, "latest summary"}} {
		if err := store.RecordCompaction(rootID, compact.cutoff, compact.text); err != nil {
			t.Fatal(err)
		}
	}
	runner := &compactingRunner{fakeRunner: &fakeRunner{}}
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
	result := clientCommand(t, root, "recovery", "undo-latest", "history.compact.retry", struct{}{})
	var outcome protocol.CompactionRetryResult
	if err := json.Unmarshal([]byte(result.Output), &outcome); err != nil || result.Status != "succeeded" || !outcome.Undone {
		t.Fatalf("retry compaction=%+v %v", result, err)
	}
	remaining := store.Compactions(rootID)
	if len(remaining) != 1 || remaining[0].Summary != "earlier summary" {
		t.Fatalf("retry removed wrong compaction: %+v", remaining)
	}
	_, restored, err := store.Load(rootID)
	if err != nil || !reflect.DeepEqual(runner.replaced, restored) {
		t.Fatalf("runtime history differs from durable history: %+v %v", runner.replaced, err)
	}
	retry := clientCommand(t, root, "recovery", "undo-latest", "history.compact.retry", struct{}{})
	if !reflect.DeepEqual(retry, result) || len(store.Compactions(rootID)) != 1 {
		t.Fatalf("command retry removed an earlier summary: %+v", retry)
	}
	userHistory := clientCommand(t, root, "recovery", "history", "history.user.list", struct{}{})
	if userHistory.Status != "succeeded" || !strings.Contains(userHistory.Output, "second") {
		t.Fatalf("authored history lost after undo: %+v", userHistory)
	}
}

func TestClientModelDefaultPersistenceAndDeferredReload(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runtime := &reloadTestRuntime{}
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}, Runtime: runtime}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	result := clientCommand(t, root, "settings", "remember-current-model", "session.model", map[string]any{"model": "model", "persist_default": true})
	if result.Status != "succeeded" {
		t.Fatalf("remember current model=%+v", result)
	}
	cfg, err := config.Load()
	if err != nil || cfg.DefaultModel != "model" || cfg.DefaultProvider != "provider" {
		t.Fatalf("default model was not saved: %+v %v", cfg, err)
	}
	beforeCompaction := cfg
	badCompaction := clientCommand(t, root, "settings", "invalid-compaction", "compaction.configure", map[string]any{"model": "summary", "provider": "missing"})
	if badCompaction.Status != "failed" {
		t.Fatalf("unresolvable compaction model accepted: %+v", badCompaction)
	}
	cfg, err = config.Load()
	if err != nil || !reflect.DeepEqual(cfg, beforeCompaction) {
		t.Fatalf("failed compaction setup altered configuration: %+v %v", cfg, err)
	}
	runtime.running.Store(true)
	deferred := clientCommand(t, root, "settings", "reload-busy", "session.reload", struct{}{})
	if deferred.Status != "succeeded" || !strings.Contains(string(deferred.Result), `"reload_pending":true`) {
		t.Fatalf("active child prevented deferred reload: %+v", deferred)
	}
	mode := clientCommand(t, root, "settings", "mode-busy", "permission.mode", map[string]bool{"external_permissions": true})
	if mode.Status != "failed" || !strings.Contains(mode.Error, "running") {
		t.Fatalf("permission policy changed during active child: %+v", mode)
	}
}
