package providerhost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestDisconnectCancelsCatalogAndInvalidatesLateSetup(t *testing.T) {
	f := newFixture(t)
	created, err := f.service.Create(t.Context(), Change{Revision: f.revision(), ID: "custom", Provider: config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "file"}, Key: &KeyPublication{ID: "owned", Key: "private-key"}})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	f.serve(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	})
	done := make(chan error, 1)
	go func() { _, err := f.service.Refresh(t.Context(), "custom"); done <- err }()
	<-entered
	result, err := f.authority.DisconnectProvider(t.Context(), created.Revision, "custom", "", nil)
	if err != nil || result.CredentialState != "cleared" {
		t.Fatal(result, err)
	}
	f.service.ClearDiscovery("custom")
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("refresh was not cancelled", err)
	}
	_, err = f.service.Update(t.Context(), Change{Revision: created.Revision, ID: "custom", Provider: config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "file"}, Key: &KeyPublication{ID: "late", Key: "late-key"}})
	if !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("old setup republished after disconnect", err)
	}
	catalog, err := f.service.Catalog(t.Context(), "custom")
	if err != nil || catalog.State != "missing" {
		t.Fatal("old catalog survived disconnect", catalog, err)
	}
}
