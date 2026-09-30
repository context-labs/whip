package tui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

func nativeSetupMenu(t *testing.T, f *nativeMenuFixture) *nativeMenu {
	t.Helper()
	m := newNativeMenu(nativeMenuWork(t), f.connection, nativeMenuOptions{Kind: "setup", Owner: &f.owner})
	t.Cleanup(m.Close)
	nativeMenuRun(t, m, m.Init())
	return m
}

func nativeSetupInput(t *testing.T, m *nativeMenu, value string) tea.Cmd {
	t.Helper()
	m.input.SetValue(value)
	return m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestNativeSetupPresetsKeyValidationMaskAndExplicitDefaults(t *testing.T) {
	f := newNativeMenuFixture(t)
	m := nativeSetupMenu(t, f)
	if len(m.setup.presets) != 11 || f.requests.Load() != 0 || !strings.Contains(m.View(100, 30), "Recommended") {
		t.Fatal("first run lost catalog or performed network", len(m.setup.presets), f.requests.Load())
	}
	nativeMenuSelect(t, m, "preset:openrouter")
	nativeMenuSelect(t, m, "key")
	m.input.SetValue("fixture-secret-key")
	if strings.Contains(m.View(100, 30), "fixture-secret-key") {
		t.Fatal("pasted key displayed")
	}
	f.status.Store(http.StatusUnauthorized)
	before := m.inventory.Revision
	nativeMenuRun(t, m, m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if m.input.Value() != "" || strings.Contains(m.View(100, 30), "fixture-secret-key") {
		t.Fatal("failed key retained in dialog")
	}
	inventory := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.Revision != before || inventory.Defaults != nil {
		t.Fatal("failed discovery published route/default", inventory)
	}
	f.status.Store(0)
	nativeMenuRun(t, m, nativeSetupInput(t, m, "fixture-secret-key"))
	if f.requests.Load() != 3 || f.authorization.Load() != "Bearer fixture-secret-key" || m.inventory.Defaults != nil {
		t.Fatal("validation or explicit-default boundary", f.requests.Load(), m.inventory.Defaults)
	}
	if !strings.Contains(m.message, "Inference has not been tested") {
		t.Fatal("catalog success exaggerated readiness", m.message)
	}
	nativeMenuSelect(t, m, "preset:openrouter")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "env:OPENROUTER_API_KEY"))
	if f.authorization.Load() != "Bearer fixture-environment-key" {
		t.Fatal("environment resolved outside configured host")
	}
}

func TestNativeSetupCustomRouteLimitsEndpointScopeAndRemovalGuard(t *testing.T) {
	f := newNativeMenuFixture(t)
	m := nativeSetupMenu(t, f)
	nativeMenuSelect(t, m, "custom")
	nativeSetupInput(t, m, "new-provider")
	nativeMenuSelect(t, m, "openai-chat")
	nativeSetupInput(t, m, "http://127.0.0.1:1/v1")
	nativeMenuSelect(t, m, "none")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "save"))
	if f.requests.Load() != 0 {
		t.Fatal("custom save secretly contacted provider")
	}
	nativeMenuSelect(t, m, "route:new-provider")
	nativeMenuSelect(t, m, "model-settings")
	nativeSetupInput(t, m, "exact-model")
	nativeSetupInput(t, m, "64000")
	nativeSetupInput(t, m, "8192")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "save"))
	var route protocol.ProviderRoute
	for _, candidate := range m.inventory.Routes {
		if candidate.ID == "new-provider" {
			route = candidate
		}
	}
	if route.Models["exact-model"].ContextWindowTokens == nil || *route.Models["exact-model"].ContextWindowTokens != 64000 || route.Models["exact-model"].MaxOutputTokens != 8192 {
		t.Fatal("model limits lost exactness", route.Models)
	}
	nativeMenuSelect(t, m, "route:new-provider")
	nativeMenuSelect(t, m, "edit")
	nativeSetupInput(t, m, "http://127.0.0.1:2/v1")
	for _, choice := range m.choices {
		if choice.id == "keep" {
			t.Fatal("old credential allowed on changed endpoint")
		}
	}
	if m.setup.keepCredential {
		t.Fatal("changed endpoint retained secret binding")
	}
	// Refused removal stays inside the confirmation with the selected route intact.
	nativeMenuRun(t, m, m.readSetup())
	defaults := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.defaults", protocol.ProviderDefaultsParams{Revision: m.inventory.Revision, Defaults: protocol.ProviderDefaults{Selection: &protocol.ModelSelection{Provider: "new-provider", Name: "exact-model"}}})
	m.inventory = defaults
	nativeMenuSelect(t, m, "route:new-provider")
	nativeMenuSelect(t, m, "remove")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "remove"))
	if m.mode != "setup-remove" || m.message == "" || m.setup.id != "new-provider" {
		t.Fatal("removal failure escaped active dialog", m.mode, m.message)
	}
	if actual := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{}); actual.Revision != defaults.Revision {
		t.Fatal("guard failed to preserve route", actual.Routes)
	}
}

func TestNativeSetupUncertainActionOnlyInspectsAndIgnoresSecretClipboard(t *testing.T) {
	f := newNativeMenuFixture(t)
	m := nativeSetupMenu(t, f)
	nativeMenuSelect(t, m, "preset:openrouter")
	nativeMenuSelect(t, m, "key")
	late := m.inputCommand(func() tea.Msg { return tea.PasteMsg{Content: "stale-secret"} })
	m.input.SetValue("fixture-secret")
	send := m.saveProvider(m.input.Value(), false)
	reply := send().(nativeMenuReply)
	if reply.err != nil {
		t.Fatal(reply.err)
	}
	reply.inventory = nil
	reply.err = errors.New("acknowledgement lost")
	m.Update(reply)
	m.Update(late())
	if m.mode != "setup-unknown" || len(m.choices) != 1 || m.input.Value() != "" || !strings.Contains(m.message, "Outcome unknown") {
		t.Fatal("uncertainty did not stop repeat effects", m.mode, m.message)
	}
	beforeInspect := f.requests.Load()
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "inspect"))
	if f.requests.Load() != beforeInspect || len(m.inventory.Routes) != 2 {
		t.Fatal("inspection replayed effect", f.requests.Load(), m.inventory.Routes)
	}
}

func nativeAccountFixture(t *testing.T) (*nativeMenuFixture, *atomic.Int32) {
	t.Helper()
	var minted atomic.Int32
	f := newNativeMenuFixture(t, func(host *runtime.Runtime) rpc.HostServices {
		auth, err := inferenceauth.New(t.Context(), filepath.Dir(host.SocketPath()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := auth.Close(); err != nil {
				t.Error(err)
			}
		})
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/auth/device/code":
				_, _ = io.WriteString(w, `{"device_code":"private-device","user_code":"PUBLIC-CODE","expires_in":900,"interval":1}`)
			case "/api/auth/device/token":
				_, _ = io.WriteString(w, `{"access_token":"private-management"}`)
			case "/api/auth/get-session":
				_, _ = io.WriteString(w, `{"user":{"id":"user","email":"fixture@example.test"}}`)
			case "/api/auth/organization/list":
				_, _ = io.WriteString(w, `[{"id":"team-a","name":"First team","slug":"first"},{"id":"team-b","name":"Second team","slug":"second"}]`)
			case "/api/auth/organization/set-active":
				_, _ = io.WriteString(w, `{}`)
			case "/api/rest/projects":
				_, _ = io.WriteString(w, `[{"id":"project-a","name":"First project"},{"id":"project-b","name":"Second project"}]`)
			case "/api/rest/api-keys":
				minted.Add(1)
				_, _ = io.WriteString(w, `{"id":"key-id","key":"private-machine"}`)
			case "/api/rest/api-keys/key-id", "/api/auth/sign-out":
				_, _ = io.WriteString(w, `{}`)
			default:
				t.Errorf("unexpected account path %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(upstream.Close)
		target, err := url.Parse(upstream.URL)
		if err != nil {
			t.Fatal(err)
		}
		transport := nativeMenuTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Scheme+"://"+request.URL.Host != "https://observability-api.inference.net" {
				return nil, errors.New("unexpected account origin")
			}
			local := request.Clone(request.Context())
			local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
			return upstream.Client().Transport.RoundTrip(local)
		})
		service, err := inferenceaccount.New(t.Context(), auth, &http.Client{Transport: transport}, func(ctx context.Context) error {
			current, err := host.HostConfiguration().Snapshot(ctx)
			if err != nil {
				return err
			}
			_, err = host.HostConfiguration().Update(ctx, current.Revision, func(settings *config.Host) error { return settings.EnsureInference() })
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(service.Close)
		openAI := openaiauth.New(t.Context(), filepath.Dir(host.SocketPath()))
		t.Cleanup(openAI.Close)
		accounts, err := account.New(t.Context(), openAI, host.HostConfiguration())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(accounts.Close)
		return rpc.HostServices{Inference: service, OpenAI: accounts}
	})
	return f, &minted
}

func nativeAccountWait(t *testing.T, m *nativeMenu, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		nativeMenuRun(t, m, m.refreshAccount())
		if m.setup.inference != nil && m.setup.inference.State == state {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("account flow did not reach", state, m.message)
		case <-ticker.C:
		}
	}
}

func TestNativeSetupAccountInspectionChoiceRecoveryAndNoAutomaticMint(t *testing.T) {
	f, minted := nativeAccountFixture(t)
	m := nativeSetupMenu(t, f)
	nativeMenuSelect(t, m, "preset:openai-codex")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "account"))
	if m.setup.openAIStatus.AuthState != "signed_out" || len(m.setup.openAIFlows) != 0 {
		t.Fatal("opening subscription started login")
	}
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "back"))
	nativeMenuSelect(t, m, "preset:inference-net")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "account"))
	if len(m.setup.inferenceFlows) != 0 || minted.Load() != 0 {
		t.Fatal("opening account caused effects")
	}
	nativeMenuSelect(t, m, "begin")
	accepted := nativeMenuSelect(t, m, "begin")().(nativeMenuReply)
	if accepted.err != nil {
		t.Fatal(accepted.err)
	}
	flowID := accepted.inference.ID
	accepted.inference = nil
	accepted.err = errors.New("lost begin acknowledgement")
	m.Update(accepted)
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "inspect"))
	if len(m.setup.inferenceFlows) != 1 || m.setup.inferenceFlows[0].ID != flowID {
		t.Fatal("lost begin not recovered by inspection", m.setup.inferenceFlows)
	}
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "flow:"+flowID))
	nativeAccountWait(t, m, "choose_team")
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "team:team-b"))
	nativeAccountWait(t, m, "choose_project")
	if minted.Load() != 0 {
		t.Fatal("key provisioned before project choice")
	}
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "project:project-b"))
	nativeAccountWait(t, m, "succeeded")
	if minted.Load() != 1 || strings.Contains(m.View(140, 40), "private-machine") {
		t.Fatal("mint repeated or secret exposed", minted.Load())
	}
	nativeMenuRun(t, m, m.readAccount())
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "setup"))
	if minted.Load() != 1 || m.setup.inferenceStatus.RouteState != "configured" {
		t.Fatal("known-key setup reprovisioned", minted.Load(), m.setup.inferenceStatus)
	}
}

func TestNativeSetupAccountStatesNeverOfferProvisioningRetryForUnknown(t *testing.T) {
	m := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{Kind: "setup"})
	defer m.Close()
	m.setup.account = "inference"
	for _, state := range []string{"uncertain", "interrupted", "failed", "cancelled", "expired"} {
		m.setup.inference = &protocol.InferenceFlow{ID: "flow", State: state}
		m.showAccountFlow()
		for _, choice := range m.choices {
			if choice.id == "retry-flow" || choice.id == "begin" || choice.id == "rotate" {
				t.Fatal("unsafe automatic replacement offered", state, choice)
			}
		}
	}
	for _, state := range []string{"persistence_required", "setup_required", "cleanup_required"} {
		m.setup.inference = &protocol.InferenceFlow{ID: "flow", State: state}
		m.showAccountFlow()
		found := false
		for _, choice := range m.choices {
			found = found || choice.id == "retry-flow"
		}
		if !found {
			t.Fatal("known retained step cannot recover", state)
		}
	}
	m.setup.inference = &protocol.InferenceFlow{ID: "flow", State: "authorizing"}
	m.showAccountFlow()
	stale := nativeMenuPoll{menu: m, generation: m.generation}
	m.Close()
	if command := m.Update(stale); command != nil {
		t.Fatal("closed account menu kept polling")
	}
}
