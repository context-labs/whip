package inferenceaccount

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestProviderDisconnectGuardsAndJoinsInferenceLogin(t *testing.T) {
	f := newFixture(t)
	if _, err := config.Initialize(f.directory); err != nil {
		t.Fatal(err)
	}
	a, err := config.NewAuthority(f.directory)
	if err != nil {
		t.Fatal(err)
	}
	disconnect := func(revision string) (LogoutResult, config.ProviderDisconnect, error) {
		var result config.ProviderDisconnect
		status, err := f.service.LogoutGuarded(t.Context(), func(clear func() error) error {
			var guardErr error
			result, guardErr = a.DisconnectProvider(t.Context(), revision, "inference-net", "inference-net", clear)
			return guardErr
		})
		return status, result, err
	}
	before, err := a.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	configured, err := a.Update(t.Context(), before.Revision, (*config.Host).EnsureInference)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	f.hook("/api/auth/device/code", func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	})
	flow, err := f.service.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, _, err := disconnect(before.Revision); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("stale disconnect accepted", err)
	}
	if value, err := f.service.Get(t.Context(), flow.ID); err != nil || value.State != Authorizing {
		t.Fatal("stale disconnect cancelled login", value, err)
	}
	status, result, err := disconnect(configured.Revision)
	if err != nil || result.CredentialState != "cleared" || status.LocalFailure != "" {
		t.Fatal(status, result, err)
	}
	f.waitState(flow.ID, Interrupted)
	if f.count("/api/auth/device/token") != 0 {
		t.Fatal("old login advanced after disconnect")
	}
}
