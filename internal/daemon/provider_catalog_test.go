package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func TestProviderCatalogQueryReloadsConfigurationAfterDiscovery(t *testing.T) {
	service := providerConnectionsFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	resume := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
			_, _ = w.Write([]byte(`{"data":[{"id":"late-model"}]}`))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(resume)
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers = map[string]config.Provider{
			"custom": {BaseURL: upstream.URL, APIKey: "fixture"},
			"other":  {BaseURL: "https://other.example/v1", APIKey: "fixture"},
		}
		cfg.Models = map[string]config.Model{"old-alias": {ID: "old-model", Providers: []string{"custom"}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		body string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		body, err := queryProviderCatalogs(t.Context(), service, json.RawMessage(`{"provider":"custom","refresh":true}`))
		done <- response{body, err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("catalog discovery did not start")
	}
	_, _, err = config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.DisabledProviders = []string{"custom"}
		cfg.Models = map[string]config.Model{"new-alias": {ID: "new-model", Providers: []string{"other"}}}
		return nil
	})
	resume()
	if err != nil {
		t.Fatal(err)
	}
	var received response
	select {
	case received = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("catalog query did not finish")
	}
	if received.err != nil {
		t.Fatal(received.err)
	}
	var result protocol.ProviderCatalogsResult
	if err := json.Unmarshal([]byte(received.body), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Models) != 1 || result.Models["new-alias"].ID != "new-model" {
		t.Fatalf("query retained models from before discovery: %+v", result.Models)
	}
	other := result.Providers["other"]
	if len(result.Providers) != 1 || other.Available == nil || !*other.Available || other.BaseURL != "https://other.example/v1" {
		t.Fatalf("query retained providers from before discovery: %+v", result.Providers)
	}
	if len(result.Catalogs) != 0 || len(result.Errors) != 1 || result.Errors["custom"] != "Model discovery failed; check the provider connection and retry." {
		t.Fatalf("late disabled-provider discovery was not rejected: %+v", result)
	}
}
