package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// Effort is one vocabulary, "off" or a level, and every session stores a
// concrete value from creation: the request, else the definition's default,
// else the configured default, resolved against the model's catalog entry.
func TestSessionCreationStoresConcreteEffort(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	create := func(t *testing.T, commandID, effort string) (string, error) {
		t.Helper()
		admission := session.CommandAdmission{
			ClientID: "client", CommandID: commandID, RequestDigest: "digest-" + commandID,
			Payload: session.RuntimePayload{Data: []byte(`{}`)},
		}
		record, err := value.control.CreateSession(t.Context(), admission, CreateSession{
			Kind: session.SessionKindAgent, CWD: t.TempDir(), Model: "m", Provider: "p", Effort: effort,
		})
		if err != nil {
			return "", err
		}
		if record.Status != "succeeded" {
			return "", fmt.Errorf("create %s: %+v", commandID, record)
		}
		var result protocol.RootIDResult
		if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
			return "", err
		}
		return result.RootID, nil
	}
	for _, test := range []struct{ name, requested, want string }{
		{"resolved default", "", "low"},
		{"explicit off", "off", "off"},
		{"explicit level", "high", "high"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rootID, err := create(t, "create-"+test.requested, test.requested)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := store.LoadMeta(rootID)
			if err != nil || meta.Effort != test.want {
				t.Fatalf("saved effort = %q, want %q (%v)", meta.Effort, test.want, err)
			}
		})
	}
	if _, err := create(t, "create-bogus", "bogus"); err == nil {
		t.Fatal("unknown effort level accepted at creation")
	}
}

// Rows saved before efforts were resolved at creation are resolved the same
// way the first time the daemon opens them, so the runner and every reader
// share one concrete value.
func TestOpenResolvesLegacyBlankEffort(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store) // store.Create leaves effort blank
	if meta, err := store.LoadMeta(rootID); err != nil || meta.Effort != "" {
		t.Fatalf("fixture effort = %q %v", meta.Effort, err)
	}
	constructed := ""
	value, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		constructed = meta.Effort
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.LoadMeta(rootID)
	if err != nil || meta.Effort != "low" || constructed != "low" {
		t.Fatalf("legacy effort saved=%q constructed=%q %v", meta.Effort, constructed, err)
	}
	if get := clientCommand(t, root, "tui", "effort-get", "session.effort.get", map[string]any{}); get.Output != "low" {
		t.Fatalf("effort.get = %+v", get)
	}
}
