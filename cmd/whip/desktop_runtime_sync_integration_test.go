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

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// TestDesktopCompiledUpdate exercises real native process replacement. Both
// binaries and every runtime path belong to this disposable fixture.
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
			"-X main.version="+build+" -X github.com/context-labs/whip/internal/buildinfo.UpdateOwner=desktop",
			"-o", binary, "./cmd/whip")
		cmd.Dir = root
		output, buildErr := cmd.CombinedOutput()
		cancel()
		if buildErr != nil {
			t.Fatalf("build fixture: %v\n%s", buildErr, output)
		}
	}
	home := filepath.Join(directory, "home")
	env := []string{"HOME=" + directory, "WHIPCODE_HOME=" + home, "WHIPCODE_NETWORK=0", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
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
	paths, err := localruntime.Resolve(home)
	if err != nil {
		t.Fatal(err)
	}
	broken := seedNativeUpdateRoot(t, paths.Directory, directory)
	legacy := filepath.Join(home, "config.json")
	if err := os.WriteFile(legacy, []byte("retired user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(paths.Directory, "host.json")
	configBefore, err := os.ReadFile(configuration)
	if err != nil {
		t.Fatal(err)
	}
	run(canonical, "daemon", "start")
	inspect := func() protocol.HostStatus {
		t.Helper()
		status := localruntime.Inspect(t.Context(), paths)
		if status.State != "running" || status.Process == nil {
			t.Fatalf("native runtime not ready: %+v", status)
		}
		return *status.Process
	}
	old := inspect()
	if old.Build != "1.0.0-beta.1" {
		t.Fatal(old)
	}
	connect := func() *client.Client {
		t.Helper()
		c, err := client.Connect(t.Context(), paths.Socket, &old.RuntimeID)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := connect()
	var created protocol.CreateTreeResult
	if err := c.Call(t.Context(), "trees.create", protocol.CreateTreeParams{CreationID: "update_smoke_create", Definition: c.Builtins()[0], WorkingDirectory: directory}, &created); err != nil || created.Root == nil {
		t.Fatalf("create session: %+v %v", created, err)
	}
	_ = c.Close()
	oldHash, err := desktopBinaryDigest(canonical)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := desktopBinaryDigest(newer)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"_desktop-runtime-sync", "--executable", canonical, "--expected-sha256", oldHash, "--sha256", newHash}
	var result desktopSyncResult
	if err := json.Unmarshal(run(newer, args...), &result); err != nil || result.State != "approval-required" {
		t.Fatalf("unapproved update: %+v %v", result, err)
	}
	if actual, err := desktopBinaryDigest(canonical); err != nil || actual != oldHash {
		t.Fatal("unapproved update replaced the executable", err)
	}
	if current := inspect(); current.PID != old.PID || current.ProcessEpoch != old.ProcessEpoch {
		t.Fatal("unapproved update interrupted the runtime", old, current)
	}
	if err := json.Unmarshal(run(newer, append(args, "--interrupt")...), &result); err != nil || result.State != "ready" || result.BuildID != "1.0.0-beta.2" {
		t.Fatalf("approved update: %+v %v", result, err)
	}
	if actual, err := desktopBinaryDigest(canonical); err != nil || actual != newHash {
		t.Fatal("canonical bytes differ from the approved payload", err)
	}
	updated := inspect()
	if updated.PID == old.PID || updated.ProcessEpoch == old.ProcessEpoch || updated.RuntimeID != old.RuntimeID || updated.Build != "1.0.0-beta.2" {
		t.Fatal("replacement did not preserve runtime and replace process identity", old, updated)
	}
	c = connect()
	defer func() { _ = c.Close() }()
	for _, id := range []protocol.ID{created.Root.ID, protocol.ID(broken)} {
		handle, err := c.Session(id)
		if err != nil {
			t.Fatal(err)
		}
		current, err := handle.Get(t.Context())
		if err != nil || current.ID != id {
			t.Fatalf("session did not survive update: %+v %v", current, err)
		}
		if id == protocol.ID(broken) {
			if current.Configuration.Model.Provider != "removed-provider" || current.Configuration.Model.Name != "removed-model" {
				t.Fatal("unconfigured selection changed", current)
			}
			page, err := handle.History(t.Context(), protocol.HistoryPageParams{Direction: "forward", Limit: 10})
			if err != nil || len(page.Messages) != 2 || len(page.Messages[0].Parts) != 1 || page.Messages[0].Parts[0].Text != "preserved conversation" {
				t.Fatalf("unconfigured owner history is not readable: %+v %v", page, err)
			}
			var scheduled protocol.ScheduleResult
			if err := c.Call(t.Context(), "schedules.get", protocol.ScheduleParams{SessionID: id, ScheduleID: "preserved_wake"}, &scheduled); err != nil || scheduled.Schedule.SessionID != id || scheduled.Schedule.Latest != nil || len(scheduled.Parts) != 1 || scheduled.Parts[0].Text != "preserved wake" {
				t.Fatalf("future schedule changed: %+v %v", scheduled, err)
			}
		}
	}
	if configAfter, err := os.ReadFile(configuration); err != nil || string(configBefore) != string(configAfter) {
		t.Fatal("update changed native host configuration", err)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "retired user data" {
		t.Fatal("update touched retired configuration", err)
	}
	// Lost success responses must not cause another process replacement on retry.
	run(newer, args...)
	if current := inspect(); current.PID != updated.PID || current.ProcessEpoch != updated.ProcessEpoch {
		t.Fatal("retry restarted the already updated runtime", updated, current)
	}
	if output := string(run(canonical, "update")); !strings.Contains(output, "Whip desktop") {
		t.Fatalf("desktop-managed CLI used the standalone updater: %s", output)
	}
	_ = c.Close()
	run(canonical, "daemon", "stop")
	db, err := store.Open(t.Context(), filepath.Join(paths.Directory, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	history, err := db.History(t.Context(), broken, 0, 10)
	if err != nil || len(history) != 2 || history[0].Parts[0].Text != "preserved conversation" {
		t.Fatalf("retained history changed after final shutdown: %+v %v", history, err)
	}
	t.Log("Verified: approval preserves process/bytes; replacement preserves runtime, sessions, history, schedule and host configuration; retry does not restart; retired configuration remains untouched")
}

func seedNativeUpdateRoot(t *testing.T, directory, working string) session.SessionID {
	t.Helper()
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := r.CreateTree(t.Context(), store.CreateTree{Definition: refs[0], WorkingDirectory: working, Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	if err != nil {
		t.Fatal(err)
	}
	identity := session.RequestIdentity{ClientID: "update", RequestID: "preserve"}
	if _, err := r.Admit(t.Context(), identity, store.Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "preserved conversation"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		admitted, err := r.Admission(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if admitted.Turn != nil && admitted.Turn.FinishedAt != nil {
			if admitted.Turn.State != session.Succeeded {
				t.Fatal(admitted.Turn)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	future := "@at " + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339)
	if _, err := r.CreateSchedule(t.Context(), root.ID, "preserved_wake", session.ScheduleSpec{Expression: future, Parts: []session.Part{{Type: "text", Text: "preserved wake"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "removed-provider", Name: "removed-model"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return root.ID
}
