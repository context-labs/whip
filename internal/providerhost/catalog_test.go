package providerhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

func TestCatalogFailureEmptyMembershipAndReadiness(t *testing.T) {
	f := newFixture(t)
	f.route("custom", noAuth())
	first, err := f.service.Refresh(t.Context(), "custom")
	if err != nil || len(first.Models) != 1 || first.Discovery != "public_catalog" {
		t.Fatal("initial discovery", err)
	}
	*first.Models[0].ContextWindowTokens = 1
	*first.FetchedAt = time.Time{}
	f.serve(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "private-server-diagnostic")
	})
	failed, err := f.service.Refresh(t.Context(), "custom")
	if !errors.Is(err, ErrDiscovery) || failed.Models[0].ID != "model" || *failed.Models[0].ContextWindowTokens != 64000 || failed.Failure == "" || failed.FetchedAt.IsZero() {
		t.Fatal("failure lost/aliased the same-scope cache", err)
	}
	ready, err := f.service.Readiness(t.Context(), session.ModelSelection{Provider: "custom", Name: "model"})
	if err != nil || !ready.Configured || ready.ModelState != "catalogued" || ready.InferenceState != "not_tested" {
		t.Fatal("catalog claimed inference readiness", ready, err)
	}
	public, _ := json.Marshal(failed)
	if strings.Contains(string(public), "private-server-diagnostic") {
		t.Fatal("provider response leaked into public failure")
	}
	f.serve(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"data":[]}`) })
	empty, err := f.service.Refresh(t.Context(), "custom")
	if err != nil || empty.Models == nil || len(empty.Models) != 0 || empty.Failure != "" {
		t.Fatal("empty success did not replace membership", err)
	}
	cached, err := f.service.Catalog(t.Context(), "custom")
	if err != nil || len(cached.Models) != 0 || cached.State != "cached" || f.count() != 3 {
		t.Fatal("cache read triggered discovery or retained stale membership", err)
	}
}

func TestCatalogRejectsChangedSourceAndRouteDuringDiscovery(t *testing.T) {
	for _, change := range []string{"environment", "file", "route", "unrelated route"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			p := noAuth()
			p.CredentialSource, p.CredentialEnv = "env", "EXPLICIT_KEY"
			f.env[p.CredentialEnv] = "first-private-key"
			path := filepath.Join(f.directory, "explicit-key")
			if change == "file" {
				p.CredentialSource, p.CredentialEnv, p.CredentialFile = "file", "", path
				if err := os.WriteFile(path, []byte("first-private-key"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			f.route("custom", p)
			if _, err := f.service.Refresh(t.Context(), "custom"); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			f.serve(func(w http.ResponseWriter, _ *http.Request) {
				close(entered)
				<-release
				_, _ = io.WriteString(w, `{"data":[{"id":"late-model"}]}`)
			})
			done := make(chan error, 1)
			go func() { _, err := f.service.Refresh(t.Context(), "custom"); done <- err }()
			<-entered
			switch change {
			case "environment":
				f.mu.Lock()
				f.env["EXPLICIT_KEY"] = "second-private-key"
				f.mu.Unlock()
			case "file":
				if err := os.WriteFile(path, []byte("second-private-key"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "route":
				p.BaseURL = "https://changed.test/v1"
				if _, err := f.service.Update(t.Context(), Change{Revision: f.revision(), ID: "custom", Provider: p}); err != nil {
					t.Fatal(err)
				}
			case "unrelated route":
				f.route("another", noAuth())
			}
			close(release)
			err := <-done
			if change == "unrelated route" {
				if err != nil {
					t.Fatal("unrelated edit invalidated discovery", err)
				}
			} else {
				if !errors.Is(err, ErrStale) {
					t.Fatal("late response accepted", err)
				}
				cached, err := f.service.Catalog(t.Context(), "custom")
				if err != nil || cached.State != "scope_changed" || len(cached.Models) != 0 {
					t.Fatal("old authority catalog exposed", err)
				}
			}
		})
	}
}

func TestManagedCatalogUsesMachineKeyAndRejectsAccountReplacement(t *testing.T) {
	for _, kind := range []string{"inference-net", "openai-codex"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			var replace func()
			var closeManager func()
			var machineKey, account, residency string
			if kind == "inference-net" {
				manager, err := inferenceauth.New(t.Context(), f.directory)
				if err != nil {
					t.Fatal(err)
				}
				f.service.inference = manager
				closeManager = func() {
					if err := manager.Close(); err != nil {
						t.Error(err)
					}
				}
				value := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "private-management", UserID: "user", ExpiresAt: new(time.Now().Add(-time.Hour))}, Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "key", Value: "private-machine-key"}}
				if err := manager.Install(t.Context(), manager.Generation(), value); err != nil {
					t.Fatal(err)
				}
				machineKey = value.MachineKey.Value
				replace = func() {
					value.MachineKey.Value = "replacement-private-key"
					if err := manager.Install(t.Context(), manager.Generation(), value); err != nil {
						t.Fatal(err)
					}
				}
				f.route(kind, config.Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialSource: kind})
			} else {
				manager := openaiauth.New(t.Context(), f.directory)
				f.service.openAI = manager
				closeManager = manager.Close
				value := openaiauth.Credentials{AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresAt: time.Now().Add(time.Hour), AccountID: "account", ComputeResidency: "us"}
				if err := manager.Install(t.Context(), manager.Generation(), value); err != nil {
					t.Fatal(err)
				}
				machineKey, account, residency = value.AccessToken, value.AccountID, value.ComputeResidency
				replace = func() {
					value.AccountID = "replacement-account"
					if err := manager.Install(t.Context(), manager.Generation(), value); err != nil {
						t.Fatal(err)
					}
				}
				f.route(kind, config.Provider{Kind: kind})
			}
			t.Cleanup(func() { f.service.Close(); closeManager() })
			entered, release := make(chan struct{}), make(chan struct{})
			f.serve(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+machineKey || r.Header.Get("Chatgpt-Account-Id") != account || r.Header.Get("X-Openai-Internal-Codex-Residency") != residency {
					t.Error("wrong authority in provider request")
				}
				if kind == "openai-codex" && r.URL.RequestURI() != "/backend-api/codex/models?client_version=0.153.4" {
					t.Error("subscription endpoint drifted")
				}
				close(entered)
				<-release
				if kind == "openai-codex" {
					_, _ = io.WriteString(w, `{"models":[{"slug":"gpt-5.3-codex","visibility":"list","context_window":100000}]}`)
				} else {
					_, _ = io.WriteString(w, `{"data":[{"id":"model"}]}`)
				}
			})
			done := make(chan error, 1)
			go func() { _, err := f.service.Refresh(t.Context(), kind); done <- err }()
			<-entered
			replace()
			close(release)
			if err := <-done; !errors.Is(err, ErrStale) {
				t.Fatal("replacement login accepted old response", err)
			}
			if value, err := f.service.Catalog(t.Context(), kind); err != nil || len(value.Models) != 0 {
				t.Fatal("old account catalog published", err)
			}
		})
	}
}

func TestOpenRouterValidatesKeyBeforeCatalogAndNeverFallsBack(t *testing.T) {
	f := newFixture(t)
	p := config.Provider{Kind: "openai-chat", BaseURL: "https://openrouter.ai/api/v1", CredentialSource: "env", CredentialEnv: "ROUTER_KEY"}
	f.env[p.CredentialEnv] = "private-router-key"
	f.route("openrouter", p)
	f.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/key" {
			t.Error("catalog called before key validation")
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	if value, err := f.service.Refresh(t.Context(), "openrouter"); !errors.Is(err, ErrDiscovery) || len(value.Models) != 0 || f.count() != 1 {
		t.Fatal("auth failure used catalog fallback", err)
	}
	f.serve(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/key") {
			_, _ = io.WriteString(w, `{"data":{}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	if value, err := f.service.Refresh(t.Context(), "openrouter"); err != nil || len(value.Models) != 0 || value.Discovery != "authenticated_catalog" || f.count() != 3 {
		t.Fatal("validated empty catalog not authoritative", err)
	}
}

func TestCatalogCloseCancelsJoinsAndBoundsActiveRefresh(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"one", "two", "three", "four", "five"} {
		f.route(id, noAuth())
	}
	entered := make(chan struct{}, 4)
	f.service.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	var wg sync.WaitGroup
	for _, id := range []string{"one", "two", "three", "four"} {
		wg.Go(func() {
			if _, err := f.service.Refresh(t.Context(), id); !errors.Is(err, context.Canceled) {
				t.Error("shutdown failed to cancel discovery", err)
			}
		})
	}
	for range 4 {
		<-entered
	}
	for _, id := range []string{"one", "five"} {
		if _, err := f.service.Refresh(t.Context(), id); !errors.Is(err, ErrBusy) {
			t.Fatal("unbounded/duplicate discovery admitted", err)
		}
	}
	f.service.Close()
	wg.Wait()
	if _, err := f.service.Refresh(t.Context(), "one"); !errors.Is(err, ErrClosed) {
		t.Fatal("refresh after close accepted")
	}
}

func TestCatalogHTTPBoundsRedirectAndSingleCapturedRefresh(t *testing.T) {
	f := newFixture(t)
	f.route("custom", noAuth())
	for _, bad := range []string{"redirect", "oversized", "malformed"} {
		f.serve(func(w http.ResponseWriter, _ *http.Request) {
			switch bad {
			case "redirect":
				w.Header().Set("Location", "https://different-account.test/private")
				w.WriteHeader(http.StatusFound)
			case "oversized":
				_, _ = io.WriteString(w, strings.Repeat("x", (8<<20)+1))
			case "malformed":
				_, _ = io.WriteString(w, `{"data":null}`)
			}
		})
		if _, err := f.service.Refresh(t.Context(), "custom"); !errors.Is(err, ErrDiscovery) {
			t.Fatal("invalid response accepted", bad, err)
		}
	}
	if f.count() != 3 {
		t.Fatal("redirect followed")
	}
	refreshed := 0
	auth := authorization{key: "old-key", check: func(context.Context) error { return nil }, refresh: func(context.Context) (string, error) { refreshed++; return "new-key", nil }}
	f.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer old-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	if _, err := f.service.request(t.Context(), "https://example.test/models", auth); err != nil || refreshed != 1 || f.count() != 5 {
		t.Fatal("captured auth refresh did not retry exactly once", err)
	}
	f.serve(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if _, err := f.service.request(t.Context(), "https://example.test/models", auth); !errors.Is(err, ErrDiscovery) || refreshed != 2 || f.count() != 7 {
		t.Fatal("rejected refreshed token replayed", err)
	}
}

func TestCatalogEncodedBudgetRetainsPreviousMembership(t *testing.T) {
	f := newFixture(t)
	f.route("custom", noAuth())
	if _, err := f.service.Refresh(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("m-%d", i), "name": strings.Repeat("<", 512)}
	}
	raw, err := json.Marshal(map[string]any{"data": rows})
	if err != nil {
		t.Fatal(err)
	}
	f.serve(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) })
	value, err := f.service.Refresh(t.Context(), "custom")
	if !errors.Is(err, ErrDiscovery) || len(value.Models) != 1 || value.Models[0].ID != "model" {
		t.Fatal("oversized public catalog replaced usable cache", err)
	}
}
