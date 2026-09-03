package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestRunDaemonPublishesProtocolAndStopsCleanly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	t.Setenv("INFERENCE_API_KEY", "test-key")
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
	badModel, _ := json.Marshal(map[string]string{"cwd": home, "model": "missing", "provider": "inference-net"})
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
		"cwd": home, "model": "kimi-k3-fast", "provider": "inference-net",
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
	if _, err := os.Stat(filepath.Join(home, "sessions.db")); err != nil {
		t.Fatalf("daemon database missing: %v", err)
	}
}

func TestRunDaemonRejectsInvalidArguments(t *testing.T) {
	if err := daemonCLI([]string{"unexpected"}); err == nil {
		t.Fatal("hidden daemon accepted positional arguments")
	}
}

func TestRunDaemonClassicTurnStartsNoKernelProcess(t *testing.T) {
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
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"classic done"},"finish_reason":"stop"}]}`+"\n\n")
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
			ProtocolMajor: daemon.ProtocolMajor, BuildID: version, ClientID: "classic-zero-kernel", ClientKind: "test",
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
			t.Error("Classic daemon did not stop")
		}
	})
	if err != nil {
		t.Fatalf("daemon did not become ready: %v", err)
	}
	createPayload, _ := json.Marshal(map[string]string{"cwd": home, "model": "test-model", "provider": "test-provider"})
	created, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "classic-create", Scope: "daemon", Operation: "session.create", Payload: createPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	turnPayload, _ := json.Marshal(map[string]string{"text": "try the runtime"})
	turn, err := client.Command(context.Background(), daemon.CommandParams{
		CommandID: "classic-turn", Scope: "root", RootID: created.Output, Operation: "submit", Payload: turnPayload,
	})
	if err != nil || turn.Output != "classic done" || calls.Load() != 2 {
		t.Fatalf("Classic turn = %+v, calls=%d, err=%v", turn, calls.Load(), err)
	}
	for range 2 {
		request := <-requests
		if request.Temperature == nil || *request.Temperature != 0.25 || request.TopP == nil || *request.TopP != 0.75 {
			t.Fatalf("daemon sampling parameters = temperature %v, top_p %v", request.Temperature, request.TopP)
		}
	}
	snapshot, err := client.Snapshot(context.Background(), created.Output)
	if err != nil || snapshot.Meta.Mode != session.ModeClassic {
		t.Fatalf("Classic snapshot = %+v, %v", snapshot.Meta, err)
	}
	output, processErr := exec.CommandContext(context.Background(), "pgrep", "-P", strconv.Itoa(os.Getpid()), "-f", "[_]kernel").CombinedOutput()
	if processErr == nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("Classic mode launched a kernel process: %s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(processErr, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("inspect child processes: %v", processErr)
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
