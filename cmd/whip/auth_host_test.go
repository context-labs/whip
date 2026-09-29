package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/hostcmd"
)

type authReady chan []byte

func (r authReady) Write(raw []byte) (int, error) { r <- bytes.Clone(raw); return len(raw), nil }

// Real composition root, managers, authority and socket. Only host selection is
// replaced; every fixture uses private disposable v4 storage and no installed daemon.
func useNativeAuth(t *testing.T, prepare func(string)) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "whip-auth-") //nolint:usetesting // Unix socket paths must fit macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("WHIPCODE_HOME", home)
	retired := filepath.Join(home, "config.json")
	if err := os.WriteFile(retired, []byte("unrelated retired configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		raw, err := os.ReadFile(retired)
		if err != nil || string(raw) != "unrelated retired configuration" {
			t.Error("native auth changed retired user configuration", err)
		}
	})
	directory := filepath.Join(home, "runtime-v4")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(directory)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(authReady, 1)
	var diagnostics bytes.Buffer
	finished := make(chan error, 1)
	go func() {
		finished <- hostcmd.Run(ctx, []string{"-directory", directory, "-scripted"}, ready, &diagnostics)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Errorf("host shutdown: %v %s", err, diagnostics.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("host did not join")
		}
	})
	var initial struct {
		Socket string `json:"socket"`
	}
	select {
	case raw := <-ready:
		if err := json.Unmarshal(raw, &initial); err != nil {
			t.Fatal(err)
		}
	case err := <-finished:
		finished <- err
		t.Fatal("host failed before readiness", err)
	case <-time.After(10 * time.Second):
		t.Fatal("host readiness deadline")
	}
	previous := connectNativeRuntime
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) { return client.Connect(ctx, initial.Socket, nil) }
	t.Cleanup(func() { connectNativeRuntime = previous })
	return directory
}
