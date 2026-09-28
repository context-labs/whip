package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type subscriptionTransport func(*http.Request) (*http.Response, error)

func (f subscriptionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func subscriptionHost(t *testing.T, directory string, settings config.Model) {
	t.Helper()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	host := config.Default()
	host.Providers["subscription"] = config.Provider{Kind: "openai-codex", Models: map[string]config.Model{"gpt-6-astra": settings}}
	host.Defaults.Model = session.ModelSelection{Provider: "subscription", Name: "gpt-6-astra"}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredSubscriptionNaturalBoundsAndLazyCredentials(t *testing.T) {
	directory := t.TempDir()
	subscriptionHost(t, directory, config.Model{})
	manager := openaiauth.New(t.Context(), directory)
	t.Cleanup(manager.Close)
	credentials := openaiauth.Credentials{AccessToken: "private-access", RefreshToken: "private-refresh", AccountID: "private-account", ExpiresAt: time.Now().Add(time.Hour)}
	if err := manager.Install(t.Context(), manager.Generation(), credentials); err != nil {
		t.Fatal(err)
	}
	provider := configuredProvider(directory, manager, nil)
	request := model.Request{Selection: session.ModelSelection{Provider: "subscription", Name: "gpt-6-astra"}, Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}}}
	calls := 0
	provider.Client = &http.Client{Transport: subscriptionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if r.URL.String() != openaiauth.BaseURL+"/responses" || r.Header.Get("Authorization") != "Bearer private-access" || r.Header.Get("Chatgpt-Account-Id") != "private-account" || strings.Contains(string(raw), "max_output_tokens") {
			t.Error("incorrect fixed subscription wire")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))}, nil
	})}
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Snapshot.MaxOutputTokens != 128000 || prepared.Snapshot.Prices.Input != nil || prepared.Snapshot.Prices.Output != nil {
		t.Fatal("generic output cap or price leaked into subscription")
	}
	if _, err := prepared.Execute(t.Context(), nil); err != nil || calls != 1 {
		t.Fatalf("configured execution: %v calls=%d", err, calls)
	}
	// Invalid private storage proves invalid model/cap decisions happen before a
	// manager capture, while unrelated API routes remain lazy about this file.
	manager.Close()
	if err := os.WriteFile(filepath.Join(directory, "openai-codex.json"), []byte("malformed-private-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	unread := openaiauth.New(t.Context(), directory)
	t.Cleanup(unread.Close)
	provider = configuredProvider(directory, unread, nil)
	for _, tc := range []struct {
		name, model string
		settings    config.Model
	}{
		{"unknown model", "unverified", config.Model{}},
		{"small cap", "gpt-6-astra", config.Model{MaxOutputTokens: 4096}},
		{"small context", "gpt-6-astra", config.Model{ContextWindowTokens: new(int64(1000))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subscriptionHost(t, directory, tc.settings)
			request.Selection.Name = tc.model
			if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("did not reject before credential capture: %v", err)
			}
		})
	}
	host := config.Default()
	host.Providers["api"] = config.Provider{Kind: "openai-responses", BaseURL: "https://example.test/v1"}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	request.Selection = session.ModelSelection{Provider: "api", Name: "model"}
	if _, err := provider.Prepare(t.Context(), request); err != nil {
		t.Fatalf("API preparation read unrelated subscription credentials: %v", err)
	}
}

func TestCommandShutdownJoinsSubscriptionRefresh(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp") // Keep the real Unix socket within its platform bound.
	directory := t.TempDir()
	subscriptionHost(t, directory, config.Model{})
	manager := openaiauth.New(t.Context(), directory)
	if err := manager.Install(t.Context(), manager.Generation(), openaiauth.Credentials{AccessToken: "expired", RefreshToken: "private-refresh", AccountID: "account", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, owner, err := r.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: refs[0], WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "shutdown"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "hello"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	started, cancelled, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	original := http.DefaultTransport
	http.DefaultTransport = subscriptionTransport(func(r *http.Request) (*http.Response, error) {
		defer close(exited)
		if r.URL.Host != "auth.openai.com" {
			t.Error("unexpected authentication endpoint")
		}
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-release
		return nil, r.Context().Err()
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, diagnostics bytes.Buffer
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); result <- run(ctx, []string{"-directory", directory}, &out, &diagnostics) }()
	t.Cleanup(func() {
		cancel()
		unblock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("command failed to join after test cancellation")
		}
	})
	select {
	case <-started:
	case err := <-result:
		t.Fatalf("command exited before refresh: %v output=%s diagnostics=%s", err, out.String(), diagnostics.String())
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never started")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh was not cancelled")
	}
	select {
	case err := <-result:
		t.Fatalf("command returned before owned refresh exited: %v", err)
	default:
	}
	unblock()
	select {
	case <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not join shutdown")
	}
	select {
	case <-exited:
	default:
		t.Fatal("command left refresh work alive")
	}
	if strings.Contains(out.String()+diagnostics.String(), "private-refresh") {
		t.Fatal("credential leaked to command output")
	}
}
