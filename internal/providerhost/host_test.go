package providerhost

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	t         *testing.T
	service   *Service
	authority *config.Authority
	directory string
	mu        sync.Mutex
	env       map[string]string
	requests  []string
	handler   http.HandlerFunc
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Initialize(directory); err != nil {
		t.Fatal(err)
	}
	authority, err := config.NewAuthority(directory)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, authority: authority, directory: directory, env: map[string]string{}}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"model","context_length":64000,"max_completion_tokens":4096}]}`)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.RequestURI())
		handler := f.handler
		f.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		local := r.Clone(r.Context())
		local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(local)
	})}
	f.service, err = New(t.Context(), authority, client, func(name string) (string, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		value, ok := f.env[name]
		return value, ok
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.service.Close)
	return f
}

func (f *fixture) revision() string {
	f.t.Helper()
	value, err := f.authority.Snapshot(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return value.Revision
}

func (f *fixture) route(id string, p config.Provider) {
	f.t.Helper()
	if _, err := f.service.Create(f.t.Context(), Change{Revision: f.revision(), ID: id, Provider: p}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) serve(handler http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
}
func (f *fixture) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.requests) }
func noAuth() config.Provider {
	return config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "none"}
}

func TestRoutesAndDefaultsAreExplicitAtomicAndPreserveCredentials(t *testing.T) {
	f := newFixture(t)
	f.env["OPENAI_API_KEY"] = "ambient-private-key"
	initial, err := f.service.List(t.Context())
	if err != nil || len(initial.Routes) != 0 || f.count() != 0 {
		t.Fatal("inventory discovered or called ambient providers")
	}
	change := Change{Revision: initial.Revision, ID: "custom", Provider: config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "file"}, Key: &KeyPublication{ID: "stable-key", Key: "private-pasted-key"}}
	created, err := f.service.Create(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 0 || created.Routes[0].Credential.State != "available" || created.Defaults.Name != "" {
		t.Fatal("route creation made implicit network/default changes")
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, config.FileName))
	if err != nil || strings.Contains(string(raw), "private-pasted-key") {
		t.Fatal("key persisted in host JSON", err)
	}
	public, _ := json.Marshal(created)
	if strings.Contains(string(public), "private-pasted-key") {
		t.Fatal("key leaked in inventory")
	}
	path := created.Routes[0].Credential.File
	if _, err := f.service.Create(t.Context(), change); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("old CAS replay unexpectedly changed config")
	}
	selection := session.ModelSelection{Provider: "custom", Name: "exact-model", Effort: "high", Temperature: new(0.0)}
	settings := config.Model{Prices: session.ModelPrices{Input: new(int64(0)), Output: new(int64(9007199254740993))}, ContextWindowTokens: new(int64(64000)), MaxOutputTokens: 1000}
	selected, err := f.service.SetDefaults(t.Context(), created.Revision, Defaults{Selection: &selection, Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	if !selected.Defaults.Equal(selection) || *selected.Routes[0].Models[selection.Name].Prices.Output != 9007199254740993 {
		t.Fatal("selection/prices lost atomic exact values")
	}
	before := f.revision()
	if _, err := f.service.Remove(t.Context(), before, "custom", nil); !errors.Is(err, ErrInvalid) || f.revision() != before {
		t.Fatal("dangling default removal mutated config")
	}
	removed, err := f.service.Remove(t.Context(), before, "custom", &Defaults{})
	if err != nil || len(removed.Routes) != 0 || removed.Defaults.Name != "" {
		t.Fatal("atomic clear/remove failed", err)
	}
	if key, err := os.ReadFile(path); err != nil || string(key) != "private-pasted-key" {
		t.Fatal("route removal destroyed private credentials", err)
	}
}

func TestRouteEndpointChangeRequiresExplicitCredentialChoice(t *testing.T) {
	f := newFixture(t)
	f.route("custom", noAuth())
	p := noAuth()
	p.BaseURL = "https://changed.test/v1"
	before := f.revision()
	if _, err := f.service.Update(t.Context(), Change{ID: "custom", Revision: before, Provider: p, KeepCredential: true}); !errors.Is(err, ErrInvalid) || f.revision() != before {
		t.Fatal("kept credentials moved to new destination")
	}
	if _, err := f.service.Update(t.Context(), Change{ID: "custom", Revision: before, Provider: p}); err != nil {
		t.Fatal(err)
	}
	if f.count() != 0 {
		t.Fatal("configuration update contacted provider")
	}
}

func TestInspectionNeverExecutesCredentialCommand(t *testing.T) {
	f := newFixture(t)
	marker := filepath.Join(f.directory, "executions")
	script := filepath.Join(f.directory, "credential-command")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf x >> \"$1\"\nprintf command-private-key\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := noAuth()
	p.CredentialSource = "command"
	p.CredentialCommand = &config.CredentialCommand{Executable: script, Arguments: []string{marker}}
	f.route("command", p)
	for range 2 {
		inventory, err := f.service.List(t.Context())
		if err != nil || inventory.Routes[0].Credential.State != "unchecked" {
			t.Fatal("dishonest command readiness", err)
		}
		if _, err := f.service.Catalog(t.Context(), "command"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection executed command")
	}
	if _, err := f.service.Refresh(t.Context(), "command"); err != nil {
		t.Fatal(err)
	}
	cached, err := f.service.Catalog(t.Context(), "command")
	if err != nil || cached.ScopeState != "unverified" {
		t.Fatal("command cache claimed current authorization")
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "x" {
		t.Fatal("command was resolved more than once", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf x >> \"$1\"\nprintf replacement-private-key\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.serve(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if _, err := f.service.Refresh(t.Context(), "command"); !errors.Is(err, ErrDiscovery) {
		t.Fatal("failed discovery unexpectedly succeeded")
	}
	cached, err = f.service.Catalog(t.Context(), "command")
	if err != nil || cached.State != "scope_changed" || len(cached.Models) != 0 {
		t.Fatal("known command account change exposed prior catalog")
	}
}

func TestPresetCopiesKeepTenAPIRoutesAndSubscriptionDistinct(t *testing.T) {
	first := Presets()
	if len(first) != 11 {
		t.Fatal("retained presets missing")
	}
	want := []string{"inference-net", "openrouter", "openai", "openai-codex", "cerebras", "deepinfra", "deepseek", "fireworks-ai", "groq", "togetherai", "xai"}
	got := []string{}
	for _, preset := range first {
		got = append(got, preset.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("preset identities drifted")
	}
	first[0].Environments[0] = "MUTATED"
	if Presets()[0].Environments[0] != "INFERENCE_API_KEY" {
		t.Fatal("preset caller mutated policy")
	}
}

func TestCompactionSelectionIsExplicitAndBlocksDanglingRemoval(t *testing.T) {
	f := newFixture(t)
	f.route("custom", noAuth())
	selection := session.ModelSelection{Provider: "custom", Name: "summary", Effort: "medium"}
	value, err := f.service.SetCompactionModel(t.Context(), f.revision(), Defaults{Selection: &selection})
	if err != nil || value.CompactionModel == nil || !value.CompactionModel.Equal(selection) || value.Defaults.Name != "" {
		t.Fatal("compaction changed unrelated default", err)
	}
	before := f.revision()
	if _, err := f.service.Remove(t.Context(), before, "custom", &Defaults{}); !errors.Is(err, ErrInvalid) || f.revision() != before {
		t.Fatal("route removal left dangling compaction selection", err)
	}
	if _, err := f.service.SetCompactionModel(t.Context(), before, Defaults{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Remove(t.Context(), f.revision(), "custom", nil); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitUncataloguedDefaultSurvivesCacheRestart(t *testing.T) {
	f := newFixture(t)
	f.route("custom", noAuth())
	selection := session.ModelSelection{Provider: "custom", Name: "explicit-new-model"}
	if _, err := f.service.SetDefaults(t.Context(), f.revision(), Defaults{Selection: &selection}); err != nil {
		t.Fatal(err)
	}
	service, err := New(t.Context(), f.authority, f.service.http, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	value, err := service.List(t.Context())
	if err != nil || !value.Defaults.Equal(selection) {
		t.Fatal("catalog restart substituted explicit selection", err)
	}
	ready, err := service.Readiness(t.Context(), selection)
	if err != nil || ready.ModelState != "unknown" || ready.InferenceState != "not_tested" || f.count() != 0 {
		t.Fatal("unknown explicit model claimed verified availability", ready, err)
	}
}

func TestUnrelatedManagedRecordsAreLazyAndServiceDoesNotOwnManagers(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"inference-net.json", "openai-codex.json"} {
		if err := os.WriteFile(filepath.Join(f.directory, name), []byte("malformed-private-record"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inference, err := inferenceauth.New(t.Context(), f.directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inference.Close() })
	openAI := openaiauth.New(t.Context(), f.directory)
	t.Cleanup(openAI.Close)
	f.service.inference, f.service.openAI = inference, openAI
	f.route("api", noAuth())
	if _, err := f.service.Refresh(t.Context(), "api"); err != nil {
		t.Fatal("unrelated private record blocked API route", err)
	}
	if err := os.Remove(filepath.Join(f.directory, "inference-net.json")); err != nil {
		t.Fatal(err)
	}
	value := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "private-management", UserID: "user"}}
	if err := inference.Install(t.Context(), inference.Generation(), value); err != nil {
		t.Fatal(err)
	}
	f.route("managed", config.Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialSource: "inference-net"})
	if _, err := f.service.Refresh(t.Context(), "managed"); !errors.Is(err, ErrCredentials) || f.count() != 1 {
		t.Fatal("management authority used for catalog", err)
	}
	value.Scope = inferenceauth.Scope{TeamID: "team", ProjectID: "project"}
	value.MachineKey = inferenceauth.MachineKey{ID: "key", Value: "private-machine-key"}
	if err := inference.Install(t.Context(), inference.Generation(), value); err != nil {
		t.Fatal(err)
	}
	f.service.Close()
	if captured, err := inference.Capture(t.Context()); err != nil || captured.Key != value.MachineKey.Value {
		t.Fatal("service closed its borrowed manager", err)
	}
}
