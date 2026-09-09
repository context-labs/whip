package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
)

func TestResolveSubscriptionRouteUsesHostCredentialsAndLimits(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("WHIP_HOME", directory)
	t.Setenv("OPENAI_API_KEY", "must-not-fallback")
	auth := openaiauth.New(t.Context(), directory)
	if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{
		AccessToken: "fixture-access", RefreshToken: "fixture-refresh", AccountID: "fixture-account", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	auth.Close()
	providers := daemon.NewProviderService(t.Context(), "subscription-route")
	t.Cleanup(providers.Close)
	cfg := config.Default()
	if err := cfg.UpsertOpenAICodex(); err != nil {
		t.Fatal(err)
	}
	cfg.Models["subscription"] = config.Model{ID: "gpt-5.5", Providers: []string{openaiauth.Provider}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{openaiauth.Provider: {
		BaseURL: openaiauth.BaseURL, AccountID: "fixture-account", Models: []config.ModelInfoLite{{
			ID: "gpt-5.5", ContextLength: 380000, InputModalities: []string{"text", "image"},
			Pricing: llm.Pricing{Prompt: "0", Completion: "0"},
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	route, _, err := resolveRuntimeModel(cfg, "subscription", openaiauth.Provider, providers)
	if err != nil || route.MaxTokens != 128000 || route.ContextLimit != 380000 || !route.Vision || route.Pricing != (llm.Pricing{}) {
		t.Fatalf("subscription route limits/pricing: %+v %v", route, err)
	}
	if route.Client.APIKey != "" || route.Client.BaseURL != openaiauth.BaseURL {
		t.Fatal("subscription route imported API billing configuration")
	}
	model := cfg.Models["subscription"]
	model.MaxOut = 100
	cfg.Models["subscription"] = model
	if _, _, err := resolveRuntimeModel(cfg, "subscription", openaiauth.Provider, providers); err == nil {
		t.Fatal("explicit unenforceable maxOut was accepted")
	}
	if _, err := providers.LogoutProvider(t.Context(), openaiauth.Provider); err != nil {
		t.Fatal(err)
	}
	// A client retained across logout must consult the shared host manager.
	_, _, err = route.Client.Complete(t.Context(), llm.Request{Model: "gpt-5.5"})
	if err == nil || !strings.Contains(err.Error(), "sign in") {
		t.Fatalf("retained runtime client ignored logout: %v", err)
	}
}

func TestACPSubscriptionDoesNotRequireAnAPIKey(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := config.Default()
	if err := cfg.UpsertOpenAICodex(); err != nil {
		t.Fatal(err)
	}
	cfg.DefaultModel, cfg.DefaultProvider = "gpt-5.5", openaiauth.Provider
	cfg.Models["gpt-5.5"] = config.Model{Providers: []string{openaiauth.Provider}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close() // EOF ends the stdio bridge after its startup validation.
	previous := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = previous; _ = input.Close() })
	if err := acpCLI(nil); err != nil {
		t.Fatalf("subscription ACP startup required an API key: %v", err)
	}
}
