package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/creack/pty"
)

// fakeOpenRouter serves GET /models: 200 with a two-model list for the good
// key, 401 for anything else — mirroring OpenRouter's auth behavior.
func fakeOpenRouter(t *testing.T, goodKey string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id": "openai/gpt-5", "context_length": 400000, "input_modalities": []string{"text", "image"},
					"pricing": map[string]string{"prompt": "0.00000125", "completion": "0.00001"},
				},
				{"id": "anthropic/claude-sonnet-4.5", "context_length": 1000000, "input_modalities": []string{"text"}},
			},
		})
	}))
	redirectAuthRequests(t, server.URL, "openrouter.ai", "/api/v1")
	useTestDaemon(t)
	return server
}

func TestAuthOpenRouterGoodKey(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	srv := fakeOpenRouter(t, "sk-or-good")
	defer srv.Close()

	if err := authOpenRouter("sk-or-good", false); err != nil {
		t.Fatalf("auth failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Providers["openrouter"]
	if !ok {
		t.Fatal("openrouter provider not saved")
	}
	if p.APIKey != "sk-or-good" || p.BaseURL != config.OpenRouterBaseURL {
		t.Errorf("unexpected provider: %+v", p)
	}

	cats := config.LoadCatalogs()
	cat, ok := cats["openrouter"]
	if !ok || len(cat.Models) != 2 {
		t.Fatalf("catalog not prefetched: %+v", cats)
	}
	if got := cat.ContextLength("openai/gpt-5"); got != 400000 {
		t.Errorf("context length not carried into catalog: %d", got)
	}
	if pricing := cat.ModelPricing("openai/gpt-5"); pricing.Prompt != "0.00000125" || pricing.Completion != "0.00001" {
		t.Errorf("pricing not carried into catalog: %+v", pricing)
	}
	if vis, found := cat.SupportsVision("openai/gpt-5"); !found || !vis {
		t.Errorf("vision modality not carried into catalog: %v %v", vis, found)
	}

	// The prefetched catalog makes catalog-only models resolvable with no
	// config entry — the "access all openrouter models easily" promise.
	_, m, _, err := cfg.Resolve("anthropic/claude-sonnet-4.5", "")
	if err != nil {
		t.Fatalf("catalog model should resolve: %v", err)
	}
	if len(m.Providers) != 1 || m.Providers[0] != "openrouter" {
		t.Errorf("catalog model should route to openrouter: %+v", m)
	}
}

func TestAuthOpenRouterBadKeyWritesNothing(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	srv := fakeOpenRouter(t, "sk-or-good")
	defer srv.Close()

	err := authOpenRouter("sk-or-bad", false)
	if err == nil {
		t.Fatal("expected rejection for a bad key")
	}

	cfg, lerr := config.Load()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if _, ok := cfg.Providers["openrouter"]; ok {
		t.Error("a rejected key must not leave a provider entry behind")
	}
	if cats := config.LoadCatalogs(); len(cats) != 0 {
		t.Errorf("a rejected key must not write the catalog: %+v", cats)
	}
}

func TestAuthOpenRouterReauthKeepsOtherState(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	srv := fakeOpenRouter(t, "sk-or-new")
	defer srv.Close()

	cfg, _ := config.Load() // first run: default inference.net config
	cfg.UpsertOpenRouter("sk-or-old", false)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := authOpenRouter("sk-or-new", false); err != nil {
		t.Fatalf("re-auth failed: %v", err)
	}
	cfg, _ = config.Load()
	if cfg.Providers["openrouter"].APIKey != "sk-or-new" {
		t.Error("re-auth should replace the key")
	}
	if _, ok := cfg.Providers["inference-net"]; !ok {
		t.Error("re-auth clobbered the default provider")
	}
	if len(cfg.Models) == 0 {
		t.Error("re-auth clobbered the model routes")
	}
}

func TestAuthCLIDispatch(t *testing.T) {
	if err := authCLI(nil); err == nil {
		t.Error("bare `whip auth` should print usage")
	}
	if err := authCLI([]string{"anthropic", "sk-x"}); err == nil {
		t.Error("unknown provider should be rejected")
	}
	// openrouter with no key anywhere errors cleanly (no prompt in tests:
	// stdin isn't a terminal, so the piped read hits EOF).
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv(config.OpenRouterEnvVar, "")
	if err := authCLI([]string{"openrouter"}); err == nil {
		t.Error("openrouter with no key should error, not hang or write config")
	}
}

func TestTerminalKeyPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	peer, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = terminal
	savedStdin, err := syscall.Dup(syscall.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Dup2(int(terminal.Fd()), syscall.Stdin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Dup2(savedStdin, syscall.Stdin)
		_ = syscall.Close(savedStdin)
		os.Stdin = oldStdin
		_ = terminal.Close()
		_ = peer.Close()
	})
	if _, err := peer.WriteString("  sk-or-terminal  \n"); err != nil {
		t.Fatal(err)
	}
	key, err := promptKey("key: ")
	if err != nil || key != "sk-or-terminal" {
		t.Fatalf("terminal key=%q err=%v", key, err)
	}
}

// withStdin replaces os.Stdin with a pipe holding data for the test.
func withStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(data); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
}

// An unparseable flag fails before anything is read or written, and an empty
// answer at the prompt reports the missing key rather than calling the API.
func TestAuthOpenRouterCLIArgs(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv(config.OpenRouterEnvVar, "")

	if err := authOpenRouterCLI([]string{"-nosuchflag"}); err == nil {
		t.Error("an unknown flag should error")
	}

	withStdin(t, "\n") // prompt answered with a bare newline
	err := authCLI([]string{"openrouter"})
	if err == nil || !strings.Contains(err.Error(), "no API key provided") {
		t.Errorf("an empty key should be reported, got %v", err)
	}
	if cfg, lerr := config.Load(); lerr == nil {
		if _, ok := cfg.Providers["openrouter"]; ok {
			t.Error("an empty key must not write a provider entry")
		}
	}
}

// A valid key still fails cleanly when the config can't be read, and nothing
// is written.
func TestAuthOpenRouterUnreadableConfig(t *testing.T) {
	srv := fakeOpenRouter(t, "sk-or-good")
	defer srv.Close()
	unusableHome(t)

	if err := authOpenRouter("sk-or-good", false); err == nil {
		t.Error("an unusable config dir should surface as an error")
	}
}

// A validated key that can't be persisted is an error, not a silent no-op.
func TestAuthOpenRouterUnwritableConfig(t *testing.T) {
	srv := fakeOpenRouter(t, "sk-or-good")
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	if _, err := config.Load(); err != nil { // materialize the default config
		t.Fatal(err)
	}
	freezeHome(t, home)

	if err := authOpenRouter("sk-or-good", false); err == nil {
		t.Error("an unwritable config dir should surface as an error")
	}
}

// Redirect only the provider gateway; daemon RPC and provider control-plane
// requests continue through their real transport paths.
type authRedirectTransport struct {
	next         http.RoundTripper
	endpoint     *url.URL
	host, prefix string
}

func (r authRedirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != r.host {
		return r.next.RoundTrip(request)
	}
	request = request.Clone(request.Context())
	target := *request.URL
	target.Scheme, target.Host = r.endpoint.Scheme, r.endpoint.Host
	target.Path = strings.TrimPrefix(target.Path, r.prefix)
	request.URL = &target
	return r.next.RoundTrip(request)
}

func redirectAuthRequests(t *testing.T, endpoint, host, prefix string) {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = authRedirectTransport{next: previous, endpoint: parsed, host: host, prefix: prefix}
	t.Cleanup(func() { http.DefaultTransport = previous })
}
