package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/hostcmd"
	"github.com/context-labs/whip/internal/localruntime"
)

func syncFixture(t *testing.T) (string, desktopSyncOptions) {
	t.Helper()
	dir := nativeDaemonHome(t)
	source, target := filepath.Join(dir, "payload"), filepath.Join(dir, "whipcode")
	for path, content := range map[string]string{source: "new backend", target: "old backend"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(text string) string { sum := sha256.Sum256([]byte(text)); return hex.EncodeToString(sum[:]) }
	return source, desktopSyncOptions{executable: target, expected: hash("old backend"), digest: hash("new backend")}
}

func TestDesktopSyncCLIRejectsUnownedOrMalformedUpdates(t *testing.T) {
	previousOwner := buildinfo.UpdateOwner
	defer func() { buildinfo.UpdateOwner = previousOwner }()
	if err := desktopRuntimeSyncCLI(nil, io.Discard); err == nil {
		t.Fatal("ordinary binary acquired desktop update authority")
	}
	buildinfo.UpdateOwner = "desktop"
	for _, args := range [][]string{
		{"--unknown"},
		{"extra"},
		{"--executable", "relative"},
		{"--executable", "/absolute", "--sha256", "invalid"},
		{"--executable", "/absolute", "--sha256", strings.Repeat("A", 64), "--expected-sha256", strings.Repeat("0", 64)},
		{"--executable", "/missing-parent-for-update-fixture/whipcode", "--sha256", strings.Repeat("0", 64), "--expected-sha256", strings.Repeat("0", 64)},
	} {
		if err := desktopRuntimeSyncCLI(args, io.Discard); err == nil {
			t.Fatal("malformed update was accepted")
		}
	}
}

func TestDesktopSyncDaemonHelper(t *testing.T) {
	if os.Getenv("WHIP_DESKTOP_UPDATE_TEST_HOME") == "" {
		return
	}
	for index, arg := range os.Args {
		if arg == "_native-runtime" {
			if err := nativeRuntimeCLI(append(os.Args[index+1:], "-scripted")); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("helper was not launched as a native runtime")
}

func TestDesktopSyncCoordinatesRealOwnerAndReadiness(t *testing.T) {
	source, options := syncFixture(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIPCODE_NETWORK", "0")
	t.Setenv("WHIP_DESKTOP_UPDATE_TEST_HOME", filepath.Dir(paths.Directory))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	launcher := "#!/bin/sh\nexec '" + strings.ReplaceAll(self, "'", "'\\''") + "' -test.run='^TestDesktopSyncDaemonHelper$' -- \"$@\"\n"
	for _, name := range []string{source, options.executable} {
		if err := os.WriteFile(name, []byte(launcher), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	options.expected, _ = desktopBinaryDigest(options.executable)
	if err := os.WriteFile(source, []byte(launcher+"# replacement\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	options.digest, _ = desktopBinaryDigest(source)
	launch := localruntime.Launch{Executable: options.executable, Arguments: []string{"_native-runtime"}, Build: "previous"}
	var ownedPIDs []int
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := localruntime.Stop(ctx, paths); err != nil {
			t.Error(err)
		}
		for _, pid := range ownedPIDs {
			// These are our unreaped fixture children, never a PID from user storage.
			_ = syscall.Kill(pid, syscall.SIGKILL)
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &status, 0, nil)
		}
	})
	initial, err := localruntime.Start(t.Context(), paths, launch)
	if err != nil || initial.Process == nil {
		t.Fatal(initial, err)
	}
	ownedPIDs = append(ownedPIDs, initial.Process.PID)
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := localruntime.Stop(cancelled, paths); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if current := localruntime.Inspect(t.Context(), paths); current.Process == nil || current.Process.ProcessEpoch != initial.Process.ProcessEpoch {
		t.Fatal(current)
	}
	result, err := syncDesktopRuntime(t.Context(), source, options)
	if err != nil || result.State != "approval-required" {
		t.Fatal(result, err)
	}
	if current, _ := desktopBinaryDigest(options.executable); current != options.expected {
		t.Fatal("unapproved update changed executable")
	}
	options.interrupt = true
	result, err = syncDesktopRuntime(t.Context(), source, options)
	if err != nil || result.State != "ready" {
		raw, _ := os.ReadFile(paths.Log)
		t.Fatalf("%+v %v\n%s", result, err, raw)
	}
	current := localruntime.Inspect(t.Context(), paths)
	if current.Process == nil || current.Process.PID == initial.Process.PID || current.Process.ProcessEpoch == initial.Process.ProcessEpoch || current.Process.RuntimeID != initial.Process.RuntimeID {
		t.Fatal(initial, current)
	}
	ownedPIDs = append(ownedPIDs, current.Process.PID)
	if result, err = syncDesktopRuntime(t.Context(), source, options); err != nil || result.State != "ready" {
		t.Fatal(result, err)
	}
	if after := localruntime.Inspect(t.Context(), paths); after.Process == nil || after.Process.ProcessEpoch != current.Process.ProcessEpoch {
		t.Fatal("retry restarted replacement", after)
	}
}

func TestDesktopSyncFailedReadinessKeepsVerifiedReplacement(t *testing.T) {
	source, options := syncFixture(t)
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 4\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	options.digest, _ = desktopBinaryDigest(source)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if _, err := syncDesktopRuntime(ctx, source, options); err == nil || !strings.Contains(err.Error(), "readiness is unconfirmed") {
		t.Fatalf("missing readiness error: %v", err)
	}
	if digest, _ := desktopBinaryDigest(options.executable); digest != options.digest {
		t.Fatal("failed readiness removed or reverted the verified executable")
	}
}

func TestDesktopSyncPreflightPreservesInstalledBinary(t *testing.T) {
	for _, kind := range []string{"digest", "changed", "symlink", "cancelled", "maintenance"} {
		t.Run(kind, func(t *testing.T) {
			source, options := syncFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "digest":
				options.digest = strings.Repeat("0", 64)
			case "changed":
				options.expected = strings.Repeat("0", 64)
			case "symlink":
				original := options.executable
				options.executable += ".link"
				if err := os.Symlink(original, options.executable); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			case "maintenance":
				paths, err := nativeRuntimePaths()
				if err != nil {
					t.Fatal(err)
				}
				lock, err := localruntime.AcquireMaintenance(ctx, paths)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Close() }()
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			}
			if _, err := syncDesktopRuntime(ctx, source, options); err == nil {
				t.Fatal("invalid update succeeded")
			}
			bytes, err := os.ReadFile(options.executable)
			if err != nil || string(bytes) != "old backend" {
				t.Fatalf("changed old binary: %q, %v", bytes, err)
			}
			staged, err := filepath.Glob(filepath.Join(filepath.Dir(source), ".whipcode-update-*"))
			if err != nil || len(staged) != 0 {
				t.Fatalf("leaked staged files: %v, %v", staged, err)
			}
		})
	}
}

func TestDesktopSyncRejectsUnverifiedOwner(t *testing.T) {
	source, options := syncFixture(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := syscall.Flock(int(owner.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	for _, interrupt := range []bool{false, true} {
		options.interrupt = interrupt
		if _, err := syncDesktopRuntime(t.Context(), source, options); err == nil || !strings.Contains(err.Error(), "unverified") {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(options.executable); err != nil || string(data) != "old backend" {
			t.Fatal(string(data), err)
		}
	}
}

func TestDesktopRuntimeRejectsUnsafePayloadsBeforeShutdown(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "directory", "oversized", "unreadable", "stage-directory", "fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			source, options := syncFixture(t)
			switch kind {
			case "missing":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "empty", "oversized":
				size := int64(0)
				if kind == "oversized" {
					size = desktopBinaryLimit + 1
				}
				if err := os.Truncate(source, size); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(source, 0o700); err != nil {
					t.Fatal(err)
				}
			case "fifo", "symlink":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if kind == "fifo" {
					if err := syscall.Mkfifo(source, 0o600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(options.executable, source); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root can read owner-denied files")
				}
				if err := os.Chmod(source, 0); err != nil {
					t.Fatal(err)
				}
			}
			parent := filepath.Dir(options.executable)
			if kind == "stage-directory" {
				parent = options.executable
			}
			if name, err := stageDesktopRuntime(t.Context(), source, parent, options.digest); err == nil || name != "" {
				t.Fatal("unsafe payload was staged")
			}
			if data, err := os.ReadFile(options.executable); err != nil || string(data) != "old backend" {
				t.Fatal("preflight changed the installed backend")
			}
		})
	}
}

func startInProcessSyncOwner(t *testing.T, paths localruntime.Paths) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- hostcmd.Run(ctx, []string{"-directory", paths.Directory, "-scripted", "-build", "previous"}, io.Discard, io.Discard)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("host did not join")
		}
	})
	// Match production readiness and polling bounds, including a cold race-built
	// SQLite schema. This fixture must not impose a shorter startup contract.
	wait, stop := context.WithTimeout(t.Context(), 15*time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	defer stop()
	for localruntime.Inspect(wait, paths).Process == nil {
		select {
		case <-wait.Done():
			t.Fatal(wait.Err())
		case <-ticker.C:
		}
	}
}

func TestDesktopRuntimeNeverStopsItself(t *testing.T) {
	source, options := syncFixture(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	startInProcessSyncOwner(t, paths)
	options.interrupt = true
	if _, err := syncDesktopRuntime(t.Context(), source, options); err == nil || !strings.Contains(err.Error(), "updater itself") {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(options.executable); err != nil || string(data) != "old backend" {
		t.Fatal(string(data), err)
	}
}

func TestDesktopManagedDiagnosticsAndApprovalCLI(t *testing.T) {
	_, options := syncFixture(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	startInProcessSyncOwner(t, paths)
	previousOwner := buildinfo.UpdateOwner
	defer func() { buildinfo.UpdateOwner = previousOwner }()
	buildinfo.UpdateOwner = "desktop"
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := desktopBinaryDigest(self)
	if err != nil {
		t.Fatal(err)
	}
	output := invokeMain(t, "_desktop-runtime-sync", "--executable", options.executable, "--expected-sha256", options.expected, "--sha256", digest)
	if !strings.Contains(output, `"state":"approval-required"`) {
		t.Fatal(output)
	}
	if metadata := invokeMain(t, "_desktop-runtime-info"); !strings.Contains(metadata, `"updateOwner":"desktop"`) {
		t.Fatal(metadata)
	}
	var status nativeDaemonStatus
	if err := json.Unmarshal([]byte(invokeMain(t, "daemon", "status", "--json")), &status); err != nil || status.State != "running" || status.Process == nil {
		t.Fatal(status, err)
	}
	if data, err := os.ReadFile(options.executable); err != nil || string(data) != "old backend" {
		t.Fatal(string(data), err)
	}
}

func TestDesktopSyncUnsafeRuntimeDoesNotReplaceCanonicalBinary(t *testing.T) {
	source, options := syncFixture(t)
	// A file where the daemon home belongs must fail before owner shutdown.
	t.Setenv("WHIPCODE_HOME", source)
	if _, err := syncDesktopRuntime(t.Context(), source, options); err == nil {
		t.Fatal("accepted non-directory runtime home")
	}
	if data, err := os.ReadFile(options.executable); err != nil || string(data) != "old backend" {
		t.Fatal("unsafe runtime changed executable")
	}
}

func TestDesktopDaemonPreflightRejectsUnsafeStartup(t *testing.T) {
	for _, kind := range []string{"flags", "network", "no-home", "home-file", "maintenance", "database"} {
		t.Run(kind, func(t *testing.T) {
			source, _ := syncFixture(t)
			args := []string{}
			switch kind {
			case "flags":
				args = []string{"--invalid"}
			case "network":
				t.Setenv("WHIPCODE_NETWORK", "invalid")
			case "no-home":
				t.Setenv("WHIPCODE_HOME", "")
				t.Setenv("HOME", "")
			case "home-file":
				t.Setenv("WHIPCODE_HOME", source)
			case "maintenance", "database":
				paths, err := daemonRuntimePaths()
				if err != nil {
					t.Fatal(err)
				}
				if kind == "maintenance" {
					if err := os.WriteFile(filepath.Join(paths.Runtime, "maintenance.lock"), nil, 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(filepath.Join(paths.Home, "sessions.db"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := runDaemon(ctx, args); err == nil {
				t.Fatal("unsafe startup was accepted")
			}
		})
	}
}

func TestDesktopDiagnosticsReportUnhealthyOwnerAndRejectMalformedCommands(t *testing.T) {
	for _, run := range []func() error{
		func() error { return daemonStatusCLI([]string{"--invalid"}) },
		func() error { return daemonStartCLI([]string{"--invalid"}) },
		func() error { return daemonLogsCLI([]string{"--invalid"}) },
	} {
		if err := run(); err == nil {
			t.Fatal("invalid daemon command accepted")
		}
	}
	output := captureDaemonOutput(t, func() error {
		printDaemonStatus(daemonStatus{State: "unhealthy", PID: 123, Error: "fixture owner did not respond", NetworkEndpoint: "http://127.0.0.1:8080"})
		return nil
	})
	for _, part := range []string{"unhealthy", "123", "fixture owner did not respond", "http://127.0.0.1:8080"} {
		if !strings.Contains(output, part) {
			t.Fatalf("diagnostic omitted %q", part)
		}
	}
	if printablePID(0) != "unknown" {
		t.Fatal("missing owner PID was reported as a real process")
	}
	_, _ = syncFixture(t)
	t.Setenv("WHIPCODE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := daemonStatusPaths(); err == nil {
		t.Fatal("missing home was accepted")
	}
}

func TestDesktopDaemonLogTailUsesTheRequestedFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(name, []byte("old line\nlatest diagnostic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	output := captureDaemonOutput(t, func() error { return tailDaemonLog(file, 1, false) })
	if output != "latest diagnostic\n" {
		t.Fatal("log tail ignored its file or line bound")
	}
}
