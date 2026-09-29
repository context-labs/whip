package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/context-labs/whip/internal/client"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/creack/pty"
)

// fakeOpenRouter mirrors the authenticated /key and public /models endpoints.
func fakeOpenRouter(t *testing.T, goodKey string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			if r.Header.Get("Authorization") != "Bearer "+goodKey {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"label":"redacted"}}`))
			return
		}
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id": "openai/gpt-5", "context_length": 400000, "input_modalities": []string{"text", "image"},
					"output_modalities": []string{"text"}, "supported_parameters": []string{"tools", "tool_choice"},
					"pricing": map[string]string{"prompt": "0.00000125", "completion": "0.00001"},
				},
				{
					"id": "anthropic/claude-sonnet-4.5", "context_length": 1000000, "input_modalities": []string{"text"},
					"output_modalities": []string{"text"}, "supported_parameters": []string{"tools", "tool_choice"},
				},
			},
		})
	}))
	redirectAuthRequests(t, server.URL, "openrouter.ai", "/api/v1")
	return server
}

func TestAuthOpenRouterGoodKey(t *testing.T) {
	srv := fakeOpenRouter(t, "sk-or-good")
	defer srv.Close()
	directory := useNativeAuth(t, nil)
	if err := authOpenRouter("sk-or-good", false); err != nil {
		t.Fatal(err)
	}
	host, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := host.Providers["openrouter"]
	if !ok || p.CredentialSource != "file" || p.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatal("provider not published", p)
	}
	raw, err := os.ReadFile(p.CredentialFile)
	if err != nil || string(raw) != "sk-or-good" {
		t.Fatal("private credential not saved", err)
	}
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var cat protocol.ProviderCatalog
	if err := c.Call(t.Context(), "providers.catalog", protocol.ProviderParams{Provider: "openrouter"}, &cat); err != nil {
		t.Fatal(err)
	}
	if len(cat.Models) != 2 || cat.Discovery != "authenticated_catalog" {
		t.Fatal(cat)
	}
	var found bool
	for _, m := range cat.Models {
		if m.ID == "openai/gpt-5" {
			found = true
			if m.ContextWindowTokens == nil || *m.ContextWindowTokens != 400000 || m.Prices.Input == nil || *m.Prices.Input != 1250000000 {
				t.Fatal(m)
			}
		}
	}
	if !found {
		t.Fatal("model metadata lost")
	}
	if host.Defaults.Model.Name != "" {
		t.Fatal("setup silently selected a model")
	}
}

func TestAuthOpenRouterBadKeyWritesNothing(t *testing.T) {
	srv := fakeOpenRouter(t, "sk-or-good")
	defer srv.Close()
	directory := useNativeAuth(t, nil)
	if err := authOpenRouter("sk-or-bad", false); err == nil {
		t.Fatal("bad key accepted")
	}
	host, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.Providers) != 0 {
		t.Fatal("bad key changed provider routes")
	}
	paths, err := filepath.Glob(filepath.Join(directory, "provider-key-*"))
	if err != nil || len(paths) != 0 {
		t.Fatal("bad key published", err)
	}
}

func TestAuthOpenRouterEnvironmentModeUsesHostReferenceWithoutPrompt(t *testing.T) {
	t.Setenv(openRouterEnvironment, "sk-or-env")
	srv := fakeOpenRouter(t, "sk-or-env")
	defer srv.Close()
	directory := useNativeAuth(t, nil)
	if err := authOpenRouterCLI([]string{"--env"}); err != nil {
		t.Fatal(err)
	}
	host, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	p := host.Providers["openrouter"]
	if p.CredentialEnv != openRouterEnvironment || p.CredentialSource != "env" || p.CredentialFile != "" {
		t.Fatal("host environment copied", p)
	}
}

func TestAuthOpenRouterReauthKeepsOtherState(t *testing.T) {
	srv := fakeOpenRouter(t, "sk-or-new")
	defer srv.Close()
	directory := useNativeAuth(t, func(directory string) {
		host := config.Default()
		host.Providers["other"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: "none"}
		host.Providers["openrouter"] = config.Provider{Kind: "openai-chat", BaseURL: "https://openrouter.ai/api/v1", CredentialSource: "env", CredentialEnv: "OLD", Models: map[string]config.Model{"explicit": {MaxOutputTokens: 1234}}}
		if err := config.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})
	if err := authOpenRouter("sk-or-new", false); err != nil {
		t.Fatal(err)
	}
	host, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.Providers) != 2 || host.Providers["openrouter"].Models["explicit"].MaxOutputTokens != 1234 {
		t.Fatal("reauth changed unrelated state")
	}
}

func TestAuthCLIDispatch(t *testing.T) {
	if err := authCLI(nil); err == nil {
		t.Error("bare `whipcode auth` should print usage")
	}
	if err := authCLI([]string{"anthropic", "sk-x"}); err == nil {
		t.Error("unknown provider should be rejected")
	}
	// openrouter with no key anywhere errors cleanly (no prompt in tests:
	// stdin isn't a terminal, so the piped read hits EOF).
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	t.Setenv(openRouterEnvironment, "")
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
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	t.Setenv(openRouterEnvironment, "")

	if err := authOpenRouterCLI([]string{"-nosuchflag"}); err == nil {
		t.Error("an unknown flag should error")
	}

	withStdin(t, "\n") // prompt answered with a bare newline
	err := authCLI([]string{"openrouter"})
	if err == nil || !strings.Contains(err.Error(), "no API key provided") {
		t.Errorf("an empty key should be reported, got %v", err)
	}
}

func TestAuthOpenRouterUnavailableHostDoesNotFallback(t *testing.T) {
	previous := connectNativeRuntime
	connectNativeRuntime = func(context.Context) (*client.Client, error) { return nil, errors.New("host unavailable") }
	t.Cleanup(func() { connectNativeRuntime = previous })
	if err := authOpenRouter("sk-or-good", false); err == nil {
		t.Fatal("host failure ignored")
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

func TestAuthOpenRouterUnreadableConfig(t *testing.T) {
	server := fakeOpenRouter(t, "sk-or-good")
	defer server.Close()
	directory := useNativeAuth(t, nil)
	path := filepath.Join(directory, config.FileName)
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := authOpenRouter("sk-or-good", false); err == nil {
		t.Fatal("unsafe configuration accepted")
	}
}

func TestAuthOpenRouterUnwritableConfig(t *testing.T) {
	server := fakeOpenRouter(t, "sk-or-good")
	defer server.Close()
	directory := useNativeAuth(t, nil)
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	if err := authOpenRouter("sk-or-good", false); err == nil {
		t.Fatal("private credential publication failure ignored")
	}
	host, err := config.Load(directory)
	if err != nil || len(host.Providers) != 0 {
		t.Fatal("failed publication changed route", err)
	}
}
