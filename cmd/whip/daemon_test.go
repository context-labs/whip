package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
)

func init() {
	daemonKernelCommand = []string{os.Args[0], "-test.run=TestDaemonKernelWorker", "--"}
}

func TestDaemonKernelWorker(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	if err := rlm.WorkerMain(os.Args[separator+1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func TestRunDaemonPublishesProtocolAndStopsCleanly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	t.Setenv("WHIP_NETWORK", "")
	t.Setenv("WHIP_LISTEN", "")
	t.Setenv("WHIP_ALLOWED_HOSTS", "")
	t.Setenv("WHIP_ALLOWED_ORIGINS", "")
	t.Setenv("WHIP_NETWORK_TERMINALS", "")
	t.Setenv("INFERENCE_API_KEY", "test-key")
	legacyPath := filepath.Join(home, "sessions.db")
	legacyBytes := []byte("legacy store must remain completely untouched")
	if err := os.WriteFile(legacyPath, legacyBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, nil) }()
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	var client *daemon.Client
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err = daemon.DialClient(context.Background(), paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, BuildID: version, ClientID: "daemon-test", ClientKind: "test",
		})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("daemon did not become ready: %v", err)
	}
	if client.InitializeResult().Generation != 1 {
		t.Fatalf("initial generation = %+v", client.InitializeResult())
	}
	endpoint := client.InitializeResult().NetworkEndpoint
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatalf("default listener is not loopback: %q", endpoint)
	}
	httpClient := &http.Client{Timeout: 2 * time.Second}
	for _, test := range []struct {
		name, host, origin string
		status             int
	}{
		{name: "native client", status: http.StatusOK},
		{name: "same origin browser", origin: endpoint, status: http.StatusOK},
		{name: "foreign host", host: "evil.test", status: http.StatusForbidden},
		{name: "foreign origin", origin: "https://evil.test", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+"/api/v3/web", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Host = test.host
			request.Header.Set("Origin", test.origin)
			response, err := httpClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status %d, want %d", response.StatusCode, test.status)
			}
		})
	}
	badModel, _ := json.Marshal(map[string]string{"kind": string(session.SessionKindAgent), "cwd": home, "model": "missing", "provider": "inference-net"})
	badCreated, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "bad-model", Scope: "daemon", Operation: "session.create", Payload: badModel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(context.Background(), badCreated.Output); err == nil {
		t.Fatal("daemon factory accepted an unknown model")
	}
	payload, _ := json.Marshal(map[string]string{
		"kind": string(session.SessionKindAgent), "cwd": home, "model": "kimi-k3-fast", "provider": "inference-net",
	})
	created, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "create", Scope: "daemon", Operation: "session.create", Payload: payload,
	})
	if err != nil || created.Output == "" || created.Status != "succeeded" {
		t.Fatalf("create session = %+v, %v", created, err)
	}
	if snapshot, err := client.Snapshot(context.Background(), created.Output); err != nil || snapshot.RootID != created.Output {
		t.Fatalf("snapshot created session = %+v, %v", snapshot, err)
	}
	_ = client.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("daemon socket remains: %v", err)
	}
	if _, err := os.Stat(runtimeDBPath(home)); err != nil {
		t.Fatalf("daemon database missing: %v", err)
	}
	after, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, legacyBytes) {
		t.Fatalf("legacy database was read or modified: got %q", after)
	}
}

func TestResolvedRuntimeEffortPreservesExplicitOffAndInheritance(t *testing.T) {
	catalogs := map[string]config.Catalog{"provider": {
		Models: []config.ModelInfoLite{{ID: "model", ReasoningEfforts: []string{"low", "high"}}},
	}}
	if got := resolvedRuntimeEffort(catalogs, "provider", "model", "off", "high"); got != "" {
		t.Fatalf("explicit off resolved to %q", got)
	}
	if got := resolvedRuntimeEffort(catalogs, "provider", "model", "", "high"); got != "high" {
		t.Fatalf("inherited effort resolved to %q", got)
	}
	if got := resolvedRuntimeEffort(catalogs, "provider", "model", "low", "high"); got != "low" {
		t.Fatalf("session override resolved to %q", got)
	}
}

func TestRunDaemonRejectsInvalidArguments(t *testing.T) {
	if err := daemonCLI([]string{"unexpected"}); err == nil {
		t.Fatal("hidden daemon accepted positional arguments")
	}
}

func TestRunDaemonAlwaysUsesRLMRuntime(t *testing.T) {
	var calls atomic.Int32
	requests := make(chan llm.Request, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- input
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"kernel-attempt","type":"function","function":{"name":"rlm_exec","arguments":"{\"code\":\"1\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"rlm done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()

	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	configData, err := json.Marshal(map[string]any{
		"defaultModel": "test-model",
		"rlm":          map[string]any{"enabled": false},
		"providers": map[string]any{"test-provider": map[string]any{
			"baseUrl": provider.URL, "api": "openai-completions", "apiKey": "test-key",
		}},
		"models": map[string]any{"test-model": map[string]any{
			"providers": []string{"test-provider"}, "context": 65536, "maxOut": 128,
			"samplingParams": map[string]any{"temperature": 0.25, "top_p": 0.75},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), configData, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, nil) }()
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	var client *daemon.Client
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err = daemon.DialClient(context.Background(), paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, BuildID: version, ClientID: "rlm-only", ClientKind: "test",
		})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() {
		if client != nil {
			_ = client.Close()
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("RLM daemon did not stop")
		}
	})
	if err != nil {
		t.Fatalf("daemon did not become ready: %v", err)
	}
	createPayload, _ := json.Marshal(map[string]string{"kind": string(session.SessionKindAgent), "cwd": home, "model": "test-model", "provider": "test-provider"})
	created, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "rlm-create", Scope: "daemon", Operation: "session.create", Payload: createPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnPayload, _ := json.Marshal(map[string]string{"text": "try the runtime"})
	turn, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "rlm-turn", Scope: "root", RootID: created.Output, Operation: "submit", Payload: turnPayload,
	})
	if err != nil || turn.Output != "rlm done" || calls.Load() != 2 {
		t.Fatalf("RLM turn = %+v, calls=%d, err=%v", turn, calls.Load(), err)
	}
	for range 2 {
		request := <-requests
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "rlm_exec" {
			t.Fatalf("model-facing tools = %+v", request.Tools)
		}
		if request.Temperature == nil || *request.Temperature != 0.25 || request.TopP == nil || *request.TopP != 0.75 {
			t.Fatalf("daemon sampling parameters = temperature %v, top_p %v", request.Temperature, request.TopP)
		}
	}
	snapshot, err := client.Snapshot(context.Background(), created.Output)
	if err != nil {
		t.Fatalf("RLM snapshot = %+v, %v", snapshot.Meta, err)
	}
}

func TestResolveRuntimeModelUsesSelectedProviderPricing(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	alphaPrice := llm.Pricing{Prompt: "0.000002", Completion: "0.000004"}
	betaPrice := llm.Pricing{Prompt: "0.000008", Completion: "0.000012", InputCacheRead: "0"}
	freePrice := llm.Pricing{Prompt: "0", Completion: "0"}
	cfg := &config.Config{
		DefaultProvider: "alpha",
		DefaultModel:    "shared",
		Providers: map[string]config.Provider{
			"alpha": {BaseURL: "https://alpha.invalid", APIKey: "test-alpha"},
			"beta":  {BaseURL: "https://beta.invalid", APIKey: "test-beta"},
		},
		Models: map[string]config.Model{
			"shared": {ID: "shared-api", Providers: []string{"beta"}, Context: 8192},
			"free":   {ID: "free-api", Providers: []string{"beta"}},
		},
	}
	catalogs := map[string]config.Catalog{
		"alpha": {BaseURL: "https://alpha.invalid", Models: []config.ModelInfoLite{{ID: "shared-api", Pricing: alphaPrice}}},
		"beta": {BaseURL: "https://beta.invalid/", Models: []config.ModelInfoLite{
			{ID: "shared-api", Pricing: betaPrice, ContextLength: 32768, MaxCompletionTokens: 4096, InputModalities: []string{"image"}},
			{ID: "free-api", Pricing: freePrice},
			{ID: "catalog-only", Pricing: betaPrice},
			{ID: "unknown"},
		}},
	}
	if err := config.SaveCatalogs(catalogs); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, model, provider, wantProvider, wantModel string
		pricing                                        llm.Pricing
	}{
		{name: "configured default", model: "shared", wantProvider: "alpha", wantModel: "shared-api", pricing: alphaPrice},
		{name: "explicit route", model: "shared", provider: "beta", wantProvider: "beta", wantModel: "shared-api", pricing: betaPrice},
		{name: "catalog overrides default", model: "catalog-only", wantProvider: "beta", wantModel: "catalog-only", pricing: betaPrice},
		{name: "free model", model: "free", provider: "beta", wantProvider: "beta", wantModel: "free-api", pricing: freePrice},
		{name: "unknown price", model: "unknown", wantProvider: "beta", wantModel: "unknown"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			route, _, err := resolveRuntimeModel(cfg, test.model, test.provider)
			if err != nil {
				t.Fatal(err)
			}
			if route.Provider != test.wantProvider || route.Model != test.wantModel || route.Pricing != test.pricing {
				t.Fatalf("route = provider %q model %q pricing %+v", route.Provider, route.Model, route.Pricing)
			}
			if route.Client.BaseURL != cfg.Providers[test.wantProvider].BaseURL {
				t.Fatalf("client endpoint does not match pricing provider: %q", route.Client.BaseURL)
			}
		})
	}
	route, _, err := resolveRuntimeModel(cfg, "shared", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if route.ContextLimit != 32768 || route.MaxTokens != 4096 || !route.Vision {
		t.Fatalf("selected route omitted catalog capabilities: %+v", route)
	}

	catalogs["beta"].Models[0].Pricing = freePrice
	if err := config.SaveCatalogs(catalogs); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := resolveRuntimeModel(cfg, "shared", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Pricing != freePrice || route.Pricing != betaPrice {
		t.Fatalf("reload changed an existing route or retained old pricing: old=%+v new=%+v", route.Pricing, reloaded.Pricing)
	}
	provider := cfg.Providers["beta"]
	provider.BaseURL = "https://replacement.invalid"
	cfg.Providers["beta"] = provider
	replacement, _, err := resolveRuntimeModel(cfg, "shared", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Pricing != (llm.Pricing{}) {
		t.Fatalf("new endpoint reused former endpoint's rates: %+v", replacement.Pricing)
	}
}

func TestRuntimeModelDoesNotRouteBuiltinCompactionToAnotherProvider(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv(config.InferenceNetEnvVar, "")
	cfg := config.Default()
	cfg.DefaultModel, cfg.DefaultProvider = "router-coding", "openrouter"
	cfg.Providers["openrouter"] = config.Provider{BaseURL: "https://router.test/v1", APIKey: "fixture"}
	cfg.Models["router-coding"] = config.Model{Providers: []string{"openrouter"}, Context: 8192}
	if err := config.SaveCatalogs(map[string]config.Catalog{"openrouter": {BaseURL: "https://router.test/v1", Models: []config.ModelInfoLite{{ID: "router-coding"}}}}); err != nil {
		t.Fatal(err)
	}
	main, _, err := resolveRuntimeModel(cfg, "", "")
	if err != nil || main.Model != "router-coding" || main.Provider != "openrouter" {
		t.Fatalf("main route: %+v, %v", main, err)
	}
	if _, _, err := resolveRuntimeModel(cfg, cfg.CompactModel, cfg.CompactProvider); err == nil {
		t.Fatal("built-in Inference compaction model was routed to OpenRouter; factory must retain main-model fallback")
	}
	// A deliberately configured auxiliary model on this provider still works.
	cfg.CompactModel = "router-coding"
	compact, _, err := resolveRuntimeModel(cfg, cfg.CompactModel, cfg.CompactProvider)
	if err != nil || compact.Model != main.Model || compact.Provider != main.Provider {
		t.Fatalf("configured compaction: %+v, %v", compact, err)
	}
}

func TestRunDaemonRejectsOwnedAndInvalidHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := runDaemon(context.Background(), nil); !errors.Is(err, daemon.ErrDaemonOwned) {
		t.Fatalf("second daemon owner = %v", err)
	}
	_ = owner.Close()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runDaemon(context.Background(), nil); err == nil {
		t.Fatal("daemon accepted corrupt config")
	}
}

func TestRunDaemonCompletesCheckpointRestartHandoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	restartErr := errors.New("restart captured")
	previousRestart := restartDaemonBinary
	restartDaemonBinary = func() error { return restartErr }
	t.Cleanup(func() { restartDaemonBinary = previousRestart })
	done := make(chan error, 1)
	go func() { done <- runDaemon(context.Background(), nil) }()
	var client *daemon.Client
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err = daemon.DialClient(context.Background(), paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, ClientID: "restart-test", ClientKind: "automation",
		})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"reason": "test"})
	result, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "checkpoint", Scope: "daemon", Operation: "daemon.checkpoint", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var notice daemon.RestartNotice
	if err := json.Unmarshal([]byte(result.Output), &notice); err != nil {
		t.Fatal(err)
	}
	if err := client.RequestRestart(context.Background(), notice.Generation); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, restartErr) {
		t.Fatalf("restart handoff = %v", err)
	}
}

func TestRunDaemonCompletesCheckpointStop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runDaemon(context.Background(), nil) }()
	var client *daemon.Client
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err = daemon.DialClient(context.Background(), paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, ClientID: "stop-test", ClientKind: "automation",
		})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"reason": "test"})
	result, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "checkpoint-stop", Scope: "daemon", Operation: "daemon.checkpoint", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var notice daemon.RestartNotice
	if err := json.Unmarshal([]byte(result.Output), &notice); err != nil {
		t.Fatal(err)
	}
	if err := client.RequestStop(context.Background(), notice.Generation); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("stop handoff = %v", err)
	}
	if _, err := os.Stat(paths.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("daemon socket remains after stop: %v", err)
	}
}

func TestScreenshotPartsNormalizesOversizedCaptures(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, llm.NormalizeMaxDim+100, 40)), nil); err != nil {
		t.Fatal(err)
	}
	parts := screenshotParts([][]byte{buf.Bytes()})
	if len(parts) != 1 || parts[0].W == 0 || parts[0].W > llm.NormalizeMaxDim {
		t.Fatalf("screenshot parts=%+v", parts)
	}
}

func TestResolveRuntimeModelPreservesCatalogDefaultPair(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{
		DefaultModel: "shared-catalog-model", DefaultProvider: "beta",
		Providers: map[string]config.Provider{
			"alpha": {BaseURL: "https://alpha.example/v1", APIKey: "fixture-alpha"},
			"beta":  {BaseURL: "https://beta.example/v1", APIKey: "fixture-beta"},
		},
		Models: map[string]config.Model{},
	}
	betaPrice := llm.Pricing{Prompt: "0.000002", Completion: "0.000004"}
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"alpha": {BaseURL: "https://alpha.example/v1", Models: []config.ModelInfoLite{{ID: cfg.DefaultModel, ContextLength: 8192}}},
		"beta":  {BaseURL: "https://beta.example/v1", Models: []config.ModelInfoLite{{ID: cfg.DefaultModel, ContextLength: 32768, MaxCompletionTokens: 4096, Pricing: betaPrice}}},
	}); err != nil {
		t.Fatal(err)
	}
	route, _, err := resolveRuntimeModel(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "beta" || route.Model != cfg.DefaultModel || route.ModelName != cfg.DefaultModel || route.Client.BaseURL != "https://beta.example/v1" || route.Client.APIKey != "fixture-beta" || route.ContextLimit != 32768 || route.MaxTokens != 4096 || route.Pricing != betaPrice {
		t.Fatal("runtime client, model, limits or prices did not use the saved default pair")
	}
	if _, _, err := resolveRuntimeModel(cfg, cfg.DefaultModel, ""); err == nil {
		t.Fatal("explicit ambiguous model silently inherited default provider")
	}
	override, _, err := resolveRuntimeModel(cfg, "", "alpha")
	if err != nil || override.Provider != "alpha" || override.ContextLimit != 8192 {
		t.Fatal("explicit provider did not override the default pair")
	}
}

func TestRLMHostConcurrencyConfiguration(t *testing.T) {
	if got := rlmLimits(config.RLMConfig{MaxConcurrentHostCalls: 1}).MaxConcurrentHostCalls; got != 1 {
		t.Fatalf("configured concurrency=%d", got)
	}
	if got := rlmLimits(config.RLMConfig{}).MaxConcurrentHostCalls; got != 16 {
		t.Fatalf("default concurrency=%d", got)
	}
}
