package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func disconnectFixture(t *testing.T) (*Authority, Snapshot, string) {
	t.Helper()
	a, before := configAuthority(t, Default())
	path, err := a.PublishKey(t.Context(), "private-key", "private-secret")
	if err != nil {
		t.Fatal(err)
	}
	value, err := a.Update(t.Context(), before.Revision, func(h *Host) error {
		h.Providers["owned"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "file", CredentialFile: path}
		h.Defaults.Model = session.ModelSelection{Provider: "owned", Name: "chat"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, value, path
}

func TestDisconnectOwnedKeyRevisionSharingAndDefaults(t *testing.T) {
	a, before, path := disconnectFixture(t)
	shared, err := a.Update(t.Context(), before.Revision, func(h *Host) error { h.Providers["other"] = h.Providers["owned"]; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DisconnectProvider(t.Context(), before.Revision, "owned", "", nil); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale disconnect accepted", err)
	}
	result, err := a.DisconnectProvider(t.Context(), shared.Revision, "owned", "", nil)
	if err != nil || result.CredentialState != "preserved_shared" || !result.Snapshot.Host.Providers["owned"].Disabled {
		t.Fatal("shared source was not preserved", result, err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "private-secret" {
		t.Fatal("shared key removed", err)
	}
	alone, err := a.Update(t.Context(), result.Snapshot.Revision, func(h *Host) error { delete(h.Providers, "other"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err = a.DisconnectProvider(t.Context(), alone.Revision, "owned", "", nil)
	if err != nil || result.CredentialState != "cleared" || result.Snapshot.Host.Providers["owned"].Disabled || result.Snapshot.Host.Defaults.Model.Name != "chat" || result.Snapshot.Host.Providers["owned"].CredentialFile != path {
		t.Fatal("disconnect lost route/default/recovery identity", result, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned key survived", err)
	}
	if result.Snapshot.Revision == alone.Revision {
		t.Fatal("old setup publication can still use its revision")
	}
}

func TestDisconnectCleanupFaultIsExplicitAndRetryable(t *testing.T) {
	a, before, path := disconnectFixture(t)
	calls := 0
	a.syncDirectory = func(directory *os.File) error {
		calls++
		if calls == 2 {
			return errors.New("private sync failure")
		}
		return directory.Sync()
	}
	result, err := a.DisconnectProvider(t.Context(), before.Revision, "owned", "", nil)
	if err != nil || result.CredentialState != "pending" || result.LocalFailure == "" {
		t.Fatal("cleanup uncertainty hidden", result, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected unlink before failed sync", err)
	}
	a.syncDirectory = (*os.File).Sync
	retried, err := a.DisconnectProvider(t.Context(), result.Snapshot.Revision, "owned", "", nil)
	if err != nil || retried.CredentialState != "cleared" || retried.LocalFailure != "" {
		t.Fatal("cleanup retry did not confirm absent key", retried, err)
	}
}

func TestDisconnectPreservesExternalAndSymlinkTargets(t *testing.T) {
	a, before, path := disconnectFixture(t)
	external := filepath.Join(t.TempDir(), "external-key")
	if err := os.WriteFile(external, []byte("external-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := a.Update(t.Context(), before.Revision, func(h *Host) error {
		p := h.Providers["owned"]
		p.CredentialFile = external
		h.Providers["external"] = p
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.DisconnectProvider(t.Context(), value.Revision, "external", "", nil)
	if err != nil || result.CredentialState != "preserved_external" || result.Snapshot.Revision != value.Revision {
		t.Fatal("external source was changed", result, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	result, err = a.DisconnectProvider(t.Context(), value.Revision, "owned", "", nil)
	// Alias references are shared and retained; without another route the
	// no-follow unlink check still refuses a symlink in the reserved namespace.
	if err != nil || result.CredentialState != "preserved_shared" {
		t.Fatal(result, err)
	}
	value, err = a.Update(t.Context(), result.Snapshot.Revision, func(h *Host) error { delete(h.Providers, "external"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err = a.DisconnectProvider(t.Context(), value.Revision, "owned", "", nil)
	if err != nil || result.CredentialState != "pending" {
		t.Fatal("symlink accepted as owned key", result, err)
	}
	if raw, err := os.ReadFile(external); err != nil || string(raw) != "external-secret" {
		t.Fatal("external target changed", err)
	}
}
