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
	t.Setenv("INFERENCE_API_KEY", "test-key")
	legacyPath := filepath.Join(home, "sessions.db")
	legacyBytes := []byte("legacy store must remain completely untouched")
	if err := os.WriteFile(legacyPath, legacyBytes, 0o600); err != nil {
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
			"providers": []string{"test-provider"}, "context": 4096, "maxOut": 128,
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
