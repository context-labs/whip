package tui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/client"
	hostmodel "github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/providerhost"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

type nativeMenuFixture struct {
	connection    *client.Client
	owner         protocol.Session
	requests      atomic.Int32
	status        atomic.Int32
	authorization atomic.Value
}

func newNativeMenuFixture(t *testing.T, services ...func(*runtime.Runtime) rpc.HostServices) *nativeMenuFixture {
	t.Helper()
	f := &nativeMenuFixture{}
	directory, err := os.MkdirTemp("/tmp", "whip-menu-") //nolint:usetesting // Bounded macOS Unix socket path.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.authorization.Store(r.Header.Get("Authorization"))
		if f.status.Load() != 0 {
			w.WriteHeader(int(f.status.Load()))
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"discovered","name":"Discovered model","context_length":64000,"reasoning_efforts":["low","high"]}]}`)
	}))
	t.Cleanup(upstream.Close)
	host, err := runtime.Open(t.Context(), directory, hostmodel.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	fixtureHTTP := &http.Client{Transport: nativeMenuTransport(func(request *http.Request) (*http.Response, error) {
		origin := request.URL.Scheme + "://" + request.URL.Host
		if origin != upstream.URL && origin != "https://openrouter.ai" && origin != "https://api.inference.net" {
			return nil, errors.New("unexpected fixture target")
		}
		local := request.Clone(request.Context())
		local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
		return upstream.Client().Transport.RoundTrip(local)
	})}
	providers, err := providerhost.New(t.Context(), host.HostConfiguration(), fixtureHTTP, func(name string) (string, bool) {
		if name == "OPENROUTER_API_KEY" {
			return "fixture-environment-key", true
		}
		return "", false
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(providers.Close)
	hostServices := rpc.HostServices{Config: host.HostConfiguration(), ProviderHost: providers}
	if len(services) > 0 {
		hostServices = services[0](host)
		hostServices.Config = host.HostConfiguration()
		hostServices.ProviderHost = providers
	}
	server, err := rpc.Listen(host, hostServices)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	f.connection, err = client.Connect(t.Context(), host.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.connection.Close(); err != nil {
			t.Error(err)
		}
	})
	before := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.create", protocol.ChangeProviderParams{Revision: before.Revision, Provider: "fixture", Declaration: protocol.ProviderDeclaration{Kind: "openai-chat", BaseURL: upstream.URL + "/v1", Credential: &protocol.ProviderCredentialInput{Source: "none"}, Models: map[string]protocol.ProviderModelSettings{"configured": {}}}})
	root := nativeMenuRPC[protocol.CreateTreeResult](t, f.connection, "trees.create", protocol.CreateTreeParams{CreationID: "menu-root", Definition: f.connection.Builtins()[0], Engine: "starlark", WorkingDirectory: directory, Overrides: protocol.ConfigPatch{AutomaticTitle: new(false), Model: &protocol.ModelSelection{Provider: "fixture", Name: "configured", Effort: "old", Temperature: new(0.25)}}})
	f.owner = *root.Root
	return f
}

func nativeMenuRPC[T any](t *testing.T, c *client.Client, method string, params any) T {
	t.Helper()
	var result T
	if err := c.Call(t.Context(), method, params, &result); err != nil {
		t.Fatal(method, err)
	}
	return result
}

func nativeMenuRun(t *testing.T, m *nativeMenu, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected menu work", m.mode, m.message)
	}
	m.Update(cmd())
}

func nativeMenuWork(t *testing.T) *nativeWork {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	work := &nativeWork{ctx: ctx, stop: cancel}
	t.Cleanup(work.close)
	return work
}

func nativeMenuSelect(t *testing.T, m *nativeMenu, id string) tea.Cmd {
	t.Helper()
	for _, choice := range m.choices {
		if choice.id == id {
			return m.choose(choice)
		}
	}
	t.Fatalf("choice %q absent in %s: %+v", id, m.mode, m.choices)
	return nil
}

func TestNativeMenuModelReadRefreshAndSessionScope(t *testing.T) {
	f := newNativeMenuFixture(t)
	m := newNativeMenu(nativeMenuWork(t), f.connection, nativeMenuOptions{Kind: "model-for-session", Owner: &f.owner})
	defer m.Close()
	nativeMenuRun(t, m, m.Init())
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "fixture"))
	if f.requests.Load() != 0 || !strings.Contains(m.View(80, 24), "Price unknown") {
		t.Fatal("opening menu performed discovery or invented price", f.requests.Load(), m.View(80, 24))
	}
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "refresh"))
	if f.requests.Load() != 1 {
		t.Fatal("explicit refresh missing", f.requests.Load())
	}
	nativeMenuSelect(t, m, "model:discovered")
	// The provider's native catalog determines whether effort choices exist.
	if m.mode == "model-effort" {
		nativeMenuSelect(t, m, "")
	}
	if len(m.choices) != 1 || m.choices[0].id != "session" {
		t.Fatal("session-only picker offered host changes", m.choices)
	}
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "session"))
	owner := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.get", protocol.SessionParams{SessionID: f.owner.ID})
	inventory := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if owner.Configuration.Model.Name != "discovered" || owner.Configuration.Model.Effort != "" || owner.Configuration.Model.Temperature == nil || *owner.Configuration.Model.Temperature != 0.25 || inventory.Defaults != nil || m.Owner().ID != f.owner.ID {
		t.Fatal("model scope or sampling changed", owner.Configuration.Model, inventory.Defaults)
	}
}

func TestNativeMenuModelCASConflictKeepsCurrentSelection(t *testing.T) {
	f := newNativeMenuFixture(t)
	m := newNativeMenu(nativeMenuWork(t), f.connection, nativeMenuOptions{Kind: "model", Owner: &f.owner})
	defer m.Close()
	nativeMenuRun(t, m, m.Init())
	m.provider = "fixture"
	m.selectModel("new-choice")
	changed := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: f.owner.ID, ExpectedRevision: f.owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "fixture", Name: "other-client"}}})
	nativeMenuRun(t, m, m.saveModel(false))
	if !strings.Contains(strings.ToLower(m.message), "revision") && !strings.Contains(strings.ToLower(m.message), "conflict") {
		t.Fatal("conflict hidden", m.message)
	}
	if m.Owner().ConfigRevision != f.owner.ConfigRevision {
		t.Fatal("unacknowledged menu selection claimed current")
	}
	nativeMenuRun(t, m, m.refreshMenu())
	if m.Owner().Configuration.Model.Name != "other-client" || m.Owner().ConfigRevision != changed.ConfigRevision {
		t.Fatal("refresh did not read canonical owner")
	}
	inventory := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.Defaults != nil {
		t.Fatal("session failure changed host defaults")
	}
}

func TestNativeMenuFirstRunExplicitHostDefaultAndPartialFailure(t *testing.T) {
	f := newNativeMenuFixture(t)
	m := newNativeMenu(nativeMenuWork(t), f.connection, nativeMenuOptions{Kind: "model"})
	defer m.Close()
	nativeMenuRun(t, m, m.Init())
	m.provider = "fixture"
	m.selectModel("manual-exact")
	if len(m.choices) != 1 || m.choices[0].id != "default" {
		t.Fatal("first run fabricated a session", m.choices)
	}
	nativeMenuRun(t, m, m.saveModel(true))
	if m.Owner() != nil || m.inventory.Defaults == nil || m.inventory.Defaults.Name != "manual-exact" {
		t.Fatal("host default not acknowledged", m.message)
	}
	m2 := newNativeMenu(nativeMenuWork(t), f.connection, nativeMenuOptions{Kind: "model", Owner: &f.owner})
	defer m2.Close()
	nativeMenuRun(t, m2, m2.Init())
	m2.provider = "fixture"
	m2.selectModel("second-exact")
	nativeMenuRPC[protocol.Session](t, f.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: f.owner.ID, ExpectedRevision: f.owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "fixture", Name: "concurrent"}}})
	nativeMenuRun(t, m2, m2.saveModel(true))
	if !strings.Contains(m2.message, "host default was saved") || m2.inventory.Defaults.Name != "second-exact" {
		t.Fatal("partial host success hidden", m2.message, m2.inventory)
	}
}

func TestNativeMenuCancellationJoinsAndRejectsLateReplies(t *testing.T) {
	work := nativeMenuWork(t)
	m := newNativeMenu(work, nil, nativeMenuOptions{})
	entered := make(chan struct{})
	joined := make(chan struct{})
	replies := make(chan tea.Msg, 1)
	command := m.call("hold", true, func(ctx context.Context) nativeMenuReply {
		close(entered)
		<-ctx.Done()
		close(joined)
		return nativeMenuReply{err: ctx.Err()}
	})
	go func() { replies <- command() }()
	<-entered
	m.Close()
	work.close()
	select {
	case <-joined:
	default:
		t.Fatal("work close failed to join menu request")
	}
	m.Update(<-replies)
	if m.message != "" || !m.Done() {
		t.Fatal("late reply changed closed menu")
	}
	m2 := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{})
	defer m2.Close()
	nativeMenuRun(t, m2, m2.call("unknown", true, func(context.Context) nativeMenuReply { return nativeMenuReply{err: errors.New("delivery ended")} }))
	if !strings.Contains(m2.message, "Outcome unknown") {
		t.Fatal("transport failure claimed rejection", m2.message)
	}
	if m2.Handles(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) {
		t.Fatal("menu captured global detach")
	}
}

func TestNativeMenuThemePreviewCancelCommitAndPreferencePreservation(t *testing.T) {
	mdMu.Lock()
	before := mdScheme
	mdMu.Unlock()
	defer setSchemeOverride(before)
	directory := t.TempDir()
	if err := saveNativePreferences(directory, nativePreferences{Theme: "dark", Mouse: new(false), Sidebar: new(false)}); err != nil {
		t.Fatal(err)
	}
	setSchemeOverride("dark")
	m := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{Kind: "theme", PreferencesDirectory: directory})
	m.Init()
	if len(m.choices) < 60 {
		t.Fatal("retained theme catalog missing", len(m.choices))
	}
	m.input.SetValue("light")
	m.previewTheme()
	if CurrentTheme() == "dark" {
		t.Fatal("filter did not preview")
	}
	m.Close()
	if CurrentTheme() != "dark" {
		t.Fatal("escape did not restore")
	}
	m2 := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{Kind: "theme", PreferencesDirectory: directory})
	m2.Init()
	nativeMenuSelect(t, m2, "light")
	prefs, err := readNativePreferences(directory)
	if err != nil || prefs.Theme != "light" || prefs.Mouse == nil || *prefs.Mouse || prefs.Sidebar == nil || *prefs.Sidebar {
		t.Fatal("theme save overwrote other settings", prefs, err)
	}
	if !m2.Done() || CurrentTheme() != "light" {
		t.Fatal("theme did not commit")
	}
	m3 := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{Kind: "settings", PreferencesDirectory: directory})
	defer m3.Close()
	m3.Init()
	nativeMenuSelect(t, m3, "mouse")
	prefs, err = readNativePreferences(directory)
	if err != nil || !*prefs.Mouse || prefs.Theme != "light" {
		t.Fatal("local toggle failed", prefs, err)
	}
	for _, size := range [][2]int{{80, 24}, {20, 8}, {8, 4}} {
		view := m3.View(size[0], size[1])
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("menu exceeds height")
		}
		for line := range strings.SplitSeq(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("menu exceeds width", line)
			}
		}
	}
}

func TestNativeMenuInputGenerationDoesNotCrossDialogs(t *testing.T) {
	m := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{})
	defer m.Close()
	command := m.inputCommand(func() tea.Msg { return tea.PasteMsg{Content: "late clipboard"} })
	m.resetInput()
	m.Update(command())
	if m.input.Value() != "" {
		t.Fatal("late clipboard crossed menu stage")
	}
}

func TestNativeMenuExactPricesAndFuzzyFilter(t *testing.T) {
	if actual := nativeModelPrice(protocol.Counter(9007199254740993)); actual != "$9007199.254740993" {
		t.Fatal(actual)
	}
	if actual := nativeModelPrice(0); actual != "$0" {
		t.Fatal(actual)
	}
	m := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{})
	defer m.Close()
	m.choices = []nativeMenuChoice{{id: "other", label: "Other"}, {id: "model", label: "Claude Sonnet"}}
	m.input.SetValue("cldsnt")
	if choices := m.visibleChoices(); len(choices) != 1 || choices[0].id != "model" {
		t.Fatal("fuzzy filter lost matching model", choices)
	}
	if m.Preferences().Theme != "" {
		t.Fatal("unexpected implicit theme")
	}
}

type nativeMenuTransport func(*http.Request) (*http.Response, error)

func (transport nativeMenuTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
