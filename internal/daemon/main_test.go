package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/config"
	providersvc "github.com/context-labs/whip/internal/provider"
)

// Model requests and discovery in tests must not depend on, or expose, the
// developer's credentials, standing instructions or installed skills. Tests may
// override these fixtures.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "whip-daemon-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = os.Setenv("HOME", home); err == nil {
		err = os.Setenv("WHIPCODE_HOME", filepath.Join(home, ".whipcode"))
	}
	if err == nil {
		err = os.Unsetenv("XDG_CONFIG_HOME") // OpenCode discovery would read it before HOME
	}
	if err == nil {
		for _, name := range providerTestEnvironment() {
			if err = os.Unsetenv(name); err != nil {
				break
			}
		}
	}
	if err != nil {
		os.RemoveAll(home)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func providerTestEnvironment() []string {
	names := []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"}
	for _, preset := range config.ProviderPresets() {
		names = append(names, preset.EnvironmentVariables...)
	}
	return names
}

func TestDaemonTestEnvironment(t *testing.T) {
	const child = "WHIP_TEST_PROVIDER_ENV_CHILD"
	if os.Getenv(child) == "" {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDaemonTestEnvironment$")
		command.Env = append(os.Environ(), child+"=1")
		for _, name := range providerTestEnvironment() {
			command.Env = append(command.Env, name+"=inherited-provider-fixture")
		}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("inherited provider environment was not isolated: %v\n%s", err, output)
		}
		return
	}

	// Exercise TestMain in a fresh process with harmless inherited keys. Trap
	// every HTTP request so a regression cannot reach a real provider.
	var requests atomic.Int64
	previous := http.DefaultTransport
	http.DefaultTransport = catalogTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected provider request in isolated tests")
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, name := range providerTestEnvironment() {
		if _, exists := os.LookupEnv(name); exists {
			t.Errorf("inherited %s survived TestMain", name)
		}
	}
	cfg := &config.Config{Providers: map[string]config.Provider{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveProviders(); len(got) != 0 {
		t.Errorf("empty fixture discovered %d inherited providers", len(got))
	}
	service := providersvc.NewProviderService(t.Context(), "isolated-test-environment")
	t.Cleanup(service.Close)
	if _, err := queryProviderCatalogs(t.Context(), service, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("empty fixture made %d provider requests", got)
	}

	// Tests can still opt into discovery explicitly after suite isolation.
	t.Setenv(config.OpenRouterEnvVar, "explicit-provider-fixture")
	providers := cfg.EffectiveProviders()
	if provider, ok := providers["openrouter"]; !ok || len(providers) != 1 || !provider.KeyStatus(cfg).Available {
		t.Fatal("explicit test key did not discover exactly its provider")
	}
}
