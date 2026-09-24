//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
)

// TestDesktopCompiledUpdate exercises the actual exec/FD handoff, not a shell
// imitation. Both versions are this release baseline with different build IDs.
func TestDesktopCompiledUpdate(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp("/tmp", "whip-update-")
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(directory, "whipcode")
	newer := filepath.Join(directory, "payload")
	for binary, build := range map[string]string{canonical: "1.0.0-beta.1", newer: "1.0.0-beta.2"} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags",
			"-X main.version="+build+" -X github.com/context-labs/whip/internal/buildinfo.Name=whipcode -X github.com/context-labs/whip/internal/buildinfo.UpdateOwner=desktop",
			"-o", binary, "./cmd/whip")
		cmd.Dir = root
		output, buildErr := cmd.CombinedOutput()
		cancel()
		if buildErr != nil {
			t.Fatalf("build fixture: %v\n%s", buildErr, output)
		}
	}
	home := filepath.Join(directory, "home")
	env := []string{"HOME=" + directory, "WHIPCODE_HOME=" + home, "WHIPCODE_NETWORK=0", "INFERENCE_API_KEY=fixture", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	run := func(binary string, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", filepath.Base(binary), args, err, output)
		}
		return output
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, canonical, "daemon", "stop")
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("cleanup failed; preserved %s: %v\n%s", directory, err, output)
			return
		}
		_ = os.RemoveAll(directory)
	})
	run(canonical, "daemon", "start")
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	connect := func(build string) *daemon.Client {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		client, err := daemon.DialClient(ctx, paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, BuildID: build, ClientID: "update-smoke", ClientKind: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	client := connect("1.0.0-beta.1")
	payload, _ := json.Marshal(map[string]string{"kind": "agent", "cwd": directory, "model": "kimi-k3-fast", "provider": "inference-net"})
	created, err := client.Command(t.Context(), daemon.CommandParams{CommandID: "update-smoke-create", Scope: "daemon", Operation: "session.create", Payload: payload})
	if err != nil || created.Status != "succeeded" {
		t.Fatalf("create session: %+v, %v", created, err)
	}
	_ = client.Close()
	oldPID, _, err := daemon.ActiveOwnerPID(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	oldHash, _ := desktopBinaryDigest(canonical)
	newHash, _ := desktopBinaryDigest(newer)
	args := []string{"_desktop-runtime-sync", "--executable", canonical, "--expected-sha256", oldHash, "--sha256", newHash}
	var result desktopSyncResult
	if err := json.Unmarshal(run(newer, args...), &result); err != nil || result.State != "approval-required" {
		t.Fatalf("unapproved update: %+v, %v", result, err)
	}
	if actual, _ := desktopBinaryDigest(canonical); actual != oldHash {
		t.Fatal("unapproved update replaced the executable")
	}
	if pid, _, _ := daemon.ActiveOwnerPID(paths.Lock); pid != oldPID {
		t.Fatal("unapproved update interrupted the daemon")
	}
	if err := json.Unmarshal(run(newer, append(args, "--interrupt")...), &result); err != nil || result.State != "ready" || result.BuildID != "1.0.0-beta.2" {
		t.Fatalf("approved update: %+v, %v", result, err)
	}
	if actual, _ := desktopBinaryDigest(canonical); actual != newHash {
		t.Fatal("canonical bytes differ from the approved payload")
	}
	newPID, owned, _ := daemon.ActiveOwnerPID(paths.Lock)
	if !owned || newPID == oldPID {
		t.Fatal("replacement daemon did not acquire ownership")
	}
	client = connect("1.0.0-beta.2")
	defer func() { _ = client.Close() }()
	if snapshot, err := client.Snapshot(t.Context(), created.Output); err != nil || snapshot.RootID != created.Output {
		t.Fatalf("session did not survive update: %+v, %v", snapshot, err)
	}
	configAfter, _ := os.ReadFile(filepath.Join(home, "config.json"))
	if string(configBefore) != string(configAfter) {
		t.Fatal("update changed user configuration")
	}
	// A lost success response must not cause another interruption on retry.
	run(newer, args...)
	if pid, _, _ := daemon.ActiveOwnerPID(paths.Lock); pid != newPID {
		t.Fatal("retry restarted the already updated daemon")
	}
	if output := string(run(canonical, "update")); !strings.Contains(output, "Whip desktop") {
		t.Fatalf("desktop-managed CLI used the standalone updater: %s", output)
	}
	t.Log("Verified: approval preserves old PID/bytes; approved replacement has new build/PID/hash; session/config survive; retry does not restart; CLI update defers to desktop")
}
