package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
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
