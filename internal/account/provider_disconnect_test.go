package account

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestProviderDisconnectCASPrecedesLoginCancellationAndSharedPreservation(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, func(_ *fixture, _ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	})
	disconnect := func(revision string) (Status, config.ProviderDisconnect, error) {
		var result config.ProviderDisconnect
		status, err := f.service.LogoutGuarded(t.Context(), func(clear func() error) error {
			var guardErr error
			result, guardErr = f.authority.DisconnectProvider(t.Context(), revision, "openai-codex", "openai-codex", clear)
			return guardErr
		})
		return status, result, err
	}
	before, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	configured, err := f.authority.Update(t.Context(), before.Revision, (*config.Host).EnsureSubscription)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := f.service.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, _, err := disconnect(before.Revision); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("stale disconnect accepted", err)
	}
	if view, err := f.service.Get(t.Context(), flow.ID); err != nil || view.State != Authorizing {
		t.Fatal("stale disconnect interrupted login", view, err)
	}
	status, result, err := disconnect(configured.Revision)
	if err != nil || result.CredentialState != "cleared" || status.AuthState != "signed_out" {
		t.Fatal(status, result, err)
	}
	if view, err := f.service.Get(t.Context(), flow.ID); err != nil || view.State != Interrupted {
		t.Fatal("disconnect did not interrupt login", view, err)
	}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), savedCredentials("shared-account")); err != nil {
		t.Fatal(err)
	}
	shared, err := f.authority.Update(t.Context(), result.Snapshot.Revision, func(h *config.Host) error { h.Providers["other"] = h.Providers["openai-codex"]; return nil })
	if err != nil {
		t.Fatal(err)
	}
	status, result, err = disconnect(shared.Revision)
	if err != nil || result.CredentialState != "preserved_shared" || status.AuthState != "stored" || !result.Snapshot.Host.Providers["openai-codex"].Disabled {
		t.Fatal("shared account authorization removed", status, result, err)
	}
}
