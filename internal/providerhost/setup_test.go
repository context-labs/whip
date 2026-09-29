package providerhost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestSetupKeyChecksDiscoveryBeforeSavingAndPreservesExplicitState(t *testing.T) {
	f := newFixture(t)
	f.route("other", config.Provider{Kind: "openai-chat", BaseURL: "https://other.invalid", CredentialSource: "none"})
	f.route("openrouter", config.Provider{Kind: "openai-chat", BaseURL: "https://openrouter.ai/api/v1", CredentialSource: "env", CredentialEnv: "OLD_KEY", Models: map[string]config.Model{"explicit-model": {MaxOutputTokens: 1234}}})
	before := f.revision()
	f.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-key" {
			http.Error(w, "private-rejection", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"model","context_length":64000,"max_completion_tokens":4096}]}`)
	})
	if _, err := f.service.SetupKey(t.Context(), before, "openrouter", &KeyPublication{ID: "bad", Key: "bad-key"}, false); !errors.Is(err, ErrDiscovery) || strings.Contains(err.Error(), "private-rejection") {
		t.Fatal("rejected discovery was accepted or disclosed", err)
	}
	if f.revision() != before {
		t.Fatal("rejected discovery changed configuration")
	}
	entries, err := os.ReadDir(f.directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "provider-key-") {
			t.Fatal("rejected key persisted")
		}
	}
	result, err := f.service.SetupKey(t.Context(), before, "openrouter", &KeyPublication{ID: "good", Key: "good-key"}, false)
	if err != nil || len(result.Routes) != 2 || result.Defaults.Name != "" {
		t.Fatal(result, err)
	}
	after, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := after.Host.Providers["openrouter"]
	if p.Models["explicit-model"].MaxOutputTokens != 1234 || p.CredentialSource != "file" || p.CredentialEnv != "" {
		t.Fatal("explicit model settings lost", p)
	}
	key, err := os.ReadFile(p.CredentialFile)
	if err != nil || string(key) != "good-key" {
		t.Fatal("private key missing", err)
	}
	catalog, err := f.service.Catalog(t.Context(), "openrouter")
	if err != nil || catalog.State != "cached" || catalog.Discovery != "authenticated_catalog" || len(catalog.Models) != 1 {
		t.Fatal(catalog, err)
	}
	f.serve(func(http.ResponseWriter, *http.Request) { t.Error("stale setup issued network request") })
	if _, err := f.service.SetupKey(t.Context(), before, "openrouter", &KeyPublication{ID: "good", Key: "good-key"}, false); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal(err)
	}
}

func TestSetupKeyUsesHostEnvironmentAndRejectsRacingCAS(t *testing.T) {
	f := newFixture(t)
	f.env["INFERENCE_API_KEY"] = "host-key"
	initial := f.revision()
	f.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer host-key" {
			t.Error("host reference not resolved")
		}
		if _, err := f.authority.Update(r.Context(), initial, func(h *config.Host) error {
			h.Providers["winner"] = config.Provider{Kind: "openai-chat", BaseURL: "https://winner.invalid", CredentialSource: "none"}
			return nil
		}); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	if _, err := f.service.SetupKey(t.Context(), initial, "inference-net", nil, true); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal(err)
	}
	f.serve(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"data":[]}`) })
	if _, err := f.service.SetupKey(t.Context(), f.revision(), "inference-net", nil, true); err != nil {
		t.Fatal(err)
	}
	after, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := after.Host.Providers["inference-net"]
	if p.CredentialEnv != "INFERENCE_API_KEY" || p.CredentialFile != "" || p.CredentialSource != "env" || len(after.Host.Providers) != 2 {
		t.Fatal(p)
	}
}

func TestSetupKeyRejectsInvalidInputsAndJoinsCancellation(t *testing.T) {
	f := newFixture(t)
	for _, test := range []struct {
		id  string
		key *KeyPublication
		env bool
	}{
		{"other", &KeyPublication{ID: "key", Key: "valid"}, false},
		{"openrouter", nil, false},
		{"openrouter", &KeyPublication{ID: "key", Key: "valid"}, true},
		{"openrouter", &KeyPublication{ID: "key", Key: "bad\nkey"}, false},
	} {
		if _, err := f.service.SetupKey(t.Context(), f.revision(), test.id, test.key, test.env); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	started := make(chan struct{})
	f.serve(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
	finished := make(chan error, 1)
	go func() {
		_, err := f.service.SetupKey(t.Context(), f.revision(), "inference-net", &KeyPublication{ID: "key", Key: "valid"}, false)
		finished <- err
	}()
	<-started
	f.service.Close()
	if err := <-finished; !errors.Is(err, context.Canceled) && !errors.Is(err, ErrDiscovery) {
		t.Fatal(err)
	}
	after, err := f.authority.Snapshot(t.Context())
	if err != nil || len(after.Host.Providers) != 0 {
		t.Fatal("cancelled discovery saved a route", err)
	}
}
