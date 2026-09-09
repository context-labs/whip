//go:build integration && unix

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/webassets"
)

// TestV2SDKBridge runs only as a subprocess of the shared integration fixture. Its
// temporary home survives deliberate process kills so the SDK can prove recovery
// from committed WAL rather than a graceful in-process simulation.
func TestV2SDKBridge(t *testing.T) {
	directory := os.Getenv("WHIP_SDK_FIXTURE_DIR")
	if directory == "" {
		t.Skip("started by packages/sdk/scripts/fixture.mjs")
	}
	// Keep config/catalog state beside the fixture database across restarts;
	// package TestMain otherwise selects a new disposable home for each process.
	t.Setenv("WHIP_HOME", filepath.Join(directory, "home"))
	lifetime, err := sdkFixtureLifetime(os.Getenv("WHIP_SDK_FIXTURE_LIFETIME"))
	if err != nil {
		t.Fatal(err)
	}
	externalOrigin, err := sdkFixtureExternalOrigin(os.Getenv("WHIP_SDK_FIXTURE_ORIGIN"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := Paths(filepath.Join(directory, "home"))
	if err != nil {
		t.Fatal(err)
	}
	store := openStore(t, filepath.Join(paths.Home, "sessions.db"))
	var previous sdkBridgeInfo
	if data, err := os.ReadFile(filepath.Join(directory, "bridge.json")); err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	rootID := previous.RootID
	if rootID == "" {
		rootID, err = store.Create(session.SessionKindAgent, directory, "model", "provider")
		if err != nil {
			t.Fatal(err)
		}
		if os.Getenv("WHIP_WEB_PERF_FIXTURE") == "1" {
			seedSDKPerformanceHistory(t, store, rootID, directory)
		}
		if os.Getenv("WHIP_WEB_REPL_FIXTURE") == "1" {
			seedSDKREPLHistory(t, store, rootID, directory)
		}
		if os.Getenv("WHIP_WEB_TURN_FAILURE_FIXTURE") == "1" {
			seedSDKTurnFailures(t, store, rootID, directory)
		}
	}
	frontendAddress := "127.0.0.1:0"
	if previous.Frontend != "" {
		frontendAddress = strings.TrimPrefix(previous.Frontend, "http://")
	}
	listener, err := net.Listen("tcp", frontendAddress)
	if err != nil {
		t.Fatal(err)
	}
	frontend := "http://" + listener.Addr().String()
	runner := &sdkRunnerControl{directory: directory, holds: make(map[string]chan struct{})}
	owner, err := New(store, func(_ context.Context, _ session.Meta, history []llm.Message) (Components, error) {
		value := &sdkFixtureRunner{fakeRunner: &fakeRunner{history: history}, services: tools.NewServices()}
		value.services.SetExternalPermissions(true)
		value.fakeRunner.turn = func(ctx context.Context, input string, authored bool) (string, error) {
			if input == "question:single" || input == "question:batch" {
				questions := []session.QuestionSet{{
					Question: "Continue with the fixture?",
					Options: []session.QuestionOption{
						{Label: "Proceed", Description: "Continue this isolated test.", Recommended: true},
						{Label: "Wait", Description: "Choose a different fixture answer."},
					},
				}}
				if input == "question:batch" {
					questions = append(questions,
						session.QuestionSet{
							Question: "Which surfaces should be checked? Add custom text if needed.",
							Multiple: true,
							Options: []session.QuestionOption{
								{Label: "Web", Description: "Check the browser client."},
								{Label: "Mobile", Description: "Check the native client."},
							},
						},
						session.QuestionSet{Question: "Optional note: skip this page to test dismissal."},
					)
				}
				answers, err := value.root.AskUser(ctx, value.root.ID(), questions)
				if err != nil {
					return "", err
				}
				data, err := json.Marshal(answers)
				return string(data), err
			}
			if strings.HasPrefix(input, "permission:") {
				arguments, err := json.Marshal(map[string]string{
					"path": "sdk-permission-" + rand.Text() + ".txt", "content": input,
				})
				if err != nil {
					return "", err
				}
				return value.services.CallTool(ctx, "write", arguments)
			}
			return runner.turn(ctx, input, authored)
		}
		return Components{Runner: value, Bind: func(_ context.Context, root *Session) error {
			value.root = root
			return value.services.BindDispatcher(root.store, root.store.Workspaces(), root.store.Processes(), root.authority)
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	network := NetworkOptions{Enabled: true, AllowedOrigins: []string{frontend}}
	// Multi-host browser fixtures explicitly trust the local fixture's origin.
	// Production network defaults and origin validation remain authoritative.
	if raw := os.Getenv("WHIP_SDK_FIXTURE_ALLOWED_ORIGINS"); raw != "" {
		origins := []string{}
		if err := json.Unmarshal([]byte(raw), &origins); err != nil {
			t.Fatal(err)
		}
		network.AllowedOrigins = append(network.AllowedOrigins, origins...)
	}
	if previous.Endpoint != "" {
		network.Address = strings.TrimSuffix(strings.TrimPrefix(previous.Endpoint, "ws://"), "/api/v3/ws")
	}
	if externalOrigin != "" {
		parsed, err := url.Parse(externalOrigin)
		if err != nil {
			t.Fatal(err)
		}
		// An explicit Host list replaces the production loopback default. Select
		// the test port first so both exact Hosts are known before Serve starts.
		// A competing bind fails fixture startup rather than attaching elsewhere.
		if network.Address == "" {
			reserved, err := network.listen(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			network.Address = reserved.Addr().String()
			if err := reserved.Close(); err != nil {
				t.Fatal(err)
			}
		}
		network.AllowedOrigins = append(network.AllowedOrigins, externalOrigin)
		network.AllowedHosts = []string{network.Address, parsed.Host}
	}
	server, err := NewServer(owner, ServerOptions{Generation: previous.Generation + 1, BuildID: "sdk-fixture", RuntimeDir: paths.Runtime, Network: network})
	if err != nil {
		t.Fatal(err)
	}
	unixListener, err := listenLocal(paths)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(unixListener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	probe, err := DialClient(ctx, paths, InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: "sdk-probe", ClientKind: "automation"})
	if err != nil {
		t.Fatal(err)
	}
	initialized := probe.InitializeResult()
	_ = probe.Close()
	info := sdkBridgeInfo{
		Frontend: frontend, Endpoint: "ws" + strings.TrimPrefix(initialized.NetworkEndpoint, "http") + "/api/v3/ws",
		RootID: rootID, Socket: paths.Socket, RuntimeID: initialized.RuntimeID,
		Generation: previous.Generation + 1, Directory: directory,
	}
	done := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	// Test wrappers, including the actual Safari runner, use the same-origin
	// attachment that production assets use. Keep the browser Origin intact.
	daemonURL, err := url.Parse(initialized.NetworkEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(daemonURL)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		direct(r)
		r.Host = daemonURL.Host
	}
	mux.Handle("/api/", proxy)
	if os.Getenv("WHIP_WEB_PERF_FIXTURE") == "1" {
		registerSDKPerformanceProbes(mux, store, rootID)
	}
	if os.Getenv("WHIP_WEB_REPL_FIXTURE") == "1" {
		registerSDKREPLProbes(mux, store, rootID)
	}
	if os.Getenv("WHIP_WEB_TURN_FAILURE_FIXTURE") == "1" {
		registerSDKTurnOutcomeProbe(mux, store, rootID)
	}
	mux.HandleFunc("POST /done", func(http.ResponseWriter, *http.Request) { once.Do(func() { close(done) }) })
	mux.HandleFunc("GET /bridge.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	})
	mux.HandleFunc("POST /control/release", func(w http.ResponseWriter, r *http.Request) {
		runner.release(r.URL.Query().Get("key"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /control/effects", func(w http.ResponseWriter, _ *http.Request) {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		data, err := os.ReadFile(filepath.Join(directory, "effects.jsonl"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("POST /result/{browser}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("browser")
		if name != "chromium" && name != "firefox" && name != "safari" && name != "webkit" {
			http.Error(w, "invalid browser", http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil || !json.Valid(data) {
			http.Error(w, "invalid result", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(filepath.Join(directory, name+"-result.json"), data, 0o600); err != nil {
			http.Error(w, "cannot save result", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	assets := http.FileServer(http.Dir(filepath.Join(directory, "public")))
	mux.Handle("/", assets)
	frontendServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", webassets.ContentSecurityPolicy)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	}))
	frontendServer.Listener = listener
	frontendServer.Start()
	defer frontendServer.Close()
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bridge.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(lifetime):
		t.Fatal("SDK bridge timed out")
	}
}

func sdkFixtureExternalOrigin(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	invalid := errors.New("WHIP_SDK_FIXTURE_ORIGIN must be an exact HTTPS origin " +
		"without credentials, path, query, fragment or wildcards")
	parsed, err := url.Parse(value)
	if err != nil || len(value) > 2048 {
		return "", invalid
	}
	exactOrigin := parsed.Scheme == "https" && parsed.Hostname() != "" && value == "https://"+parsed.Host
	plainHost := parsed.User == nil && !strings.Contains(parsed.Host, "*")
	if !exactOrigin || !plainHost {
		return "", invalid
	}
	return value, nil
}

func TestSDKFixtureExternalOrigin(t *testing.T) {
	validOrigins := []string{
		"", "https://whip.example.ts.net", "https://whip.example.ts.net:8443", "https://[::1]:8443",
	}
	for _, value := range validOrigins {
		got, err := sdkFixtureExternalOrigin(value)
		if err != nil || got != value {
			t.Fatalf("valid origin %q: got %q, %v", value, got, err)
		}
	}
	for _, value := range []string{
		"http://whip.example.ts.net", "wss://whip.example.ts.net", "https://", "https://*.example.ts.net",
		"https://user:password@whip.example.ts.net", "https://whip.example.ts.net/", "https://whip.example.ts.net/api",
		"https://whip.example.ts.net?", "https://whip.example.ts.net?q=1", "https://whip.example.ts.net#fragment",
	} {
		if _, err := sdkFixtureExternalOrigin(value); err == nil {
			t.Errorf("accepted invalid origin %q", value)
		}
	}
}

func sdkFixtureLifetime(value string) (time.Duration, error) {
	if value == "" {
		return 4 * time.Minute, nil
	}
	lifetime, err := time.ParseDuration(value)
	withinBounds := lifetime > 0 && lifetime <= 30*time.Minute
	if err != nil || !withinBounds {
		return 0, fmt.Errorf("WHIP_SDK_FIXTURE_LIFETIME must be greater than zero and at most 30m: %q", value)
	}
	return lifetime, nil
}

func TestSDKFixtureLifetime(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Duration
	}{
		{value: "", want: 4 * time.Minute},
		{value: "1s", want: time.Second},
		{value: "30m", want: 30 * time.Minute},
		{value: "0"},
		{value: "-1s"},
		{value: "30m1ms"},
		{value: "invalid"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := sdkFixtureLifetime(tt.value)
			if got != tt.want || (err != nil) != (tt.want == 0) {
				t.Fatalf("sdkFixtureLifetime(%q) = %v, %v; want %v", tt.value, got, err, tt.want)
			}
		})
	}
}

type sdkBridgeInfo struct {
	Frontend   string `json:"frontend"`
	Endpoint   string `json:"endpoint"`
	Socket     string `json:"socket"`
	RootID     string `json:"root_id"`
	RuntimeID  string `json:"runtime_id"`
	Directory  string `json:"directory"`
	Generation int64  `json:"generation"`
}

type sdkRunnerControl struct {
	mu        sync.Mutex
	directory string
	holds     map[string]chan struct{}
}

type sdkFixtureRunner struct {
	*fakeRunner
	root     *Session
	services *tools.Services
}

func (r *sdkFixtureRunner) TurnParts(ctx context.Context, input string, parts []llm.ContentPart, started func(), accepted func(string)) (string, error) {
	// Echo the resolved model input so built SDK tests can distinguish an actual
	// host attachment read from an opaque reference appended to the prompt.
	data, err := json.Marshal(SubmitPayload{Text: input, Parts: parts})
	if err != nil {
		return "", err
	}
	output, err := r.Turn(ctx, string(data), true, started, accepted)
	r.mu.Lock()
	for index := len(r.history) - 1; index >= 0; index-- {
		if r.history[index].Role == "user" {
			r.history[index].Content = input
			r.history[index].Parts = parts
			break
		}
	}
	r.mu.Unlock()
	return output, err
}

func (r *sdkFixtureRunner) Turn(ctx context.Context, input string, authored bool, started func(), accepted func(string)) (string, error) {
	if input == "scratch-result" {
		return r.scratchResult(ctx, started)
	}
	return r.fakeRunner.Turn(ctx, input, authored, func() {
		started()
		for _, text := range []string{input[:len(input)/2], input[len(input)/2:]} {
			r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{kind: "stream.text", event: StreamEvent{Text: text}}})
		}
		if os.Getenv("WHIP_WEB_PERF_FIXTURE") == "1" && input == "hold:performance-stream" {
			streamSDKPerformance(ctx, r.root)
		}
		refreshFixture := strings.HasPrefix(input, "hold:tool-stream-refresh-")
		if input == "hold:tool-stream" || refreshFixture {
			// Interleave calls so the supervisor cannot coalesce all updates before
			// they reach the SDK. These payloads are cumulative, not deltas.
			for _, args := range []string{`{"code":"print(`, `{"code":"print(1)"}`} {
				for _, id := range []string{"tool-a", "tool-b"} {
					r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
						kind: "stream.tool.call", event: StreamEvent{ID: id, Name: "rlm_exec", Args: args},
					}})
				}
			}
			for _, output := range []string{"first", "first\nsecond"} {
				for _, id := range []string{"tool-a", "tool-b"} {
					r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
						kind: "stream.tool.output", event: StreamEvent{ID: id, Text: output},
					}})
				}
			}
			if refreshFixture {
				r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
					kind: "stream.tool.completed", event: StreamEvent{ID: "tool-a", Result: "completed before snapshot"},
				}})
				// Push the earlier text and completed tool beyond the snapshot's
				// 128-event window while the same turn remains active.
				for range 160 {
					r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
						kind: "stream.usage", event: StreamEvent{},
					}})
				}
			}
		}
	}, accepted)
}

func (r *sdkFixtureRunner) ReplaceHistory(history []llm.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append([]llm.Message(nil), history...)
}

func (r *sdkFixtureRunner) SetExternalPermissions(enabled bool) {
	r.services.SetExternalPermissions(enabled)
}

func (r *sdkFixtureRunner) ExternalPermissionsEnabled() bool {
	return r.services.ExternalPermissionsEnabled()
}

func (r *sdkFixtureRunner) ResolvePermission(id string, decision capability.Decision) error {
	return r.services.ResolvePermission(id, decision)
}

func (r *sdkFixtureRunner) Close() {
	r.services.Close()
	r.fakeRunner.Close()
}

func (r *sdkRunnerControl) turn(ctx context.Context, input string, _ bool) (string, error) {
	r.mu.Lock()
	file, err := os.OpenFile(filepath.Join(r.directory, "effects.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		err = errors.Join(json.NewEncoder(file).Encode(input), file.Close())
	}
	var hold chan struct{}
	if key, ok := strings.CutPrefix(input, "hold:"); ok {
		hold = r.hold(key)
	}
	r.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("record fake effect: %w", err)
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return input, nil
}

func (r *sdkRunnerControl) hold(key string) chan struct{} {
	hold := r.holds[key]
	if hold == nil {
		hold = make(chan struct{})
		r.holds[key] = hold
	}
	return hold
}

func (r *sdkRunnerControl) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hold := r.hold(key)
	select {
	case <-hold:
	default:
		close(hold)
	}
}
