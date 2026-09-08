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

// TestV2SDKBridge runs only as a subprocess of the SDK acceptance scripts. Its
// temporary home survives deliberate process kills so the SDK can prove recovery
// from committed WAL rather than a graceful in-process simulation.
func TestV2SDKBridge(t *testing.T) {
	directory := os.Getenv("WHIP_SDK_FIXTURE_DIR")
	if directory == "" {
		t.Skip("started by packages/sdk/scripts/fixture.mjs")
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
	if previous.Endpoint != "" {
		network.Address = strings.TrimSuffix(strings.TrimPrefix(previous.Endpoint, "ws://"), "/api/v3/ws")
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
	if os.Getenv("WHIP_WEB_PERF_FIXTURE") == "1" {
		registerSDKPerformanceProbes(mux, store, rootID)
	}
	if os.Getenv("WHIP_WEB_REPL_FIXTURE") == "1" {
		registerSDKREPLProbes(mux, store, rootID)
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
	mux.Handle("GET /", assets)
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
	case <-time.After(4 * time.Minute):
		t.Fatal("SDK bridge timed out")
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
		if input == "hold:tool-stream" {
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
