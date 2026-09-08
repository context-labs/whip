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
	"testing"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
)

func syncFixture(t *testing.T) (string, desktopSyncOptions) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WHIP_HOME", filepath.Join(dir, "home"))
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
	previousName, previousOwner := buildinfo.Name, buildinfo.UpdateOwner
	defer func() { buildinfo.Name, buildinfo.UpdateOwner = previousName, previousOwner }()
	if err := desktopRuntimeSyncCLI(nil, io.Discard); err == nil {
		t.Fatal("ordinary binary acquired desktop update authority")
	}
	buildinfo.Name, buildinfo.UpdateOwner = "whipcode", "desktop"
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
	home := os.Getenv("WHIP_DESKTOP_UPDATE_TEST_HOME")
	if home == "" {
		return
	}
	t.Setenv("WHIP_HOME", home)
	for index, arg := range os.Args {
		if arg == "_daemon" {
			if err := daemonCLI(os.Args[index+1:]); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("helper was not launched as a daemon")
}

func TestDesktopSyncCoordinatesRealOwnerAndReadiness(t *testing.T) {
	source, options := syncFixture(t)
	paths, err := daemonRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_NETWORK", "0")
	t.Setenv("WHIP_DESKTOP_UPDATE_TEST_HOME", filepath.Dir(paths.Home))
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
	options.interrupt = true
	if err := daemon.LaunchInstalledDaemon(paths, options.executable, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := stopDesktopOwner(ctx, paths); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var initialPID int
	for ctx.Err() == nil {
		status, client := probeDaemon(paths, 50*time.Millisecond)
		if client != nil {
			_ = client.Close()
			initialPID = status.PID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if initialPID == 0 {
		t.Fatal("initial daemon did not become ready")
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if err := stopDesktopOwner(cancelled, paths); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stop: %v", err)
	}
	if pid, owned, err := daemon.ActiveOwnerPID(paths.Lock); err != nil || !owned || pid != initialPID {
		t.Fatal("cancelled update disturbed the running owner")
	}
	result, err := syncDesktopRuntime(ctx, source, options)
	if err != nil || result.State != "ready" {
		log, _ := os.ReadFile(filepath.Join(paths.Home, "daemon.log"))
		t.Fatalf("sync: %+v, %v\n%s", result, err, log)
	}
	pid, owned, err := daemon.ActiveOwnerPID(paths.Lock)
	if err != nil || !owned || pid == initialPID {
		t.Fatalf("replacement ownership: %d, %v, %v", pid, owned, err)
	}
	if result, err = syncDesktopRuntime(ctx, source, options); err != nil || result.State != "ready" {
		t.Fatalf("idempotent retry: %+v, %v", result, err)
	}
	if after, _, _ := daemon.ActiveOwnerPID(paths.Lock); after != pid {
		t.Fatal("retry restarted the replacement owner")
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
	if _, err := syncDesktopRuntime(ctx, source, options); err == nil || !strings.Contains(err.Error(), "did not become ready") {
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
				paths, err := daemonRuntimePaths()
				if err != nil {
					t.Fatal(err)
				}
				lock, err := daemon.AcquireMaintenance(paths)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Close() }()
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

func TestDesktopSyncRequiresApprovalForAnOwnedDaemon(t *testing.T) {
	source, options := syncFixture(t)
	paths, err := daemonRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	result, err := syncDesktopRuntime(t.Context(), source, options)
	if err != nil || result.State != "approval-required" {
		t.Fatalf("result: %+v, %v", result, err)
	}
	bytes, err := os.ReadFile(options.executable)
	if err != nil || string(bytes) != "old backend" {
		t.Fatalf("changed binary without approval: %q, %v", bytes, err)
	}
}

func TestDesktopRuntimeRejectsUnsafePayloadsBeforeShutdown(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "directory", "oversized", "unreadable", "stage-directory"} {
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

func TestDesktopRuntimeNeverStopsItself(t *testing.T) {
	_, _ = syncFixture(t)
	paths, err := daemonRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := stopDesktopOwner(t.Context(), paths); err == nil || !strings.Contains(err.Error(), "updater itself") {
		t.Fatalf("self-stop was not refused: %v", err)
	}
}

func TestDesktopManagedDiagnosticsAndApprovalCLI(t *testing.T) {
	// The hidden commands must remain read-only until restart approval, even
	// when invoked through the executable's actual dispatch path.
	_, options := syncFixture(t)
	paths, err := daemonRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	previousName, previousOwner := buildinfo.Name, buildinfo.UpdateOwner
	defer func() { buildinfo.Name, buildinfo.UpdateOwner = previousName, previousOwner }()
	buildinfo.Name, buildinfo.UpdateOwner = "whipcode", "desktop"
	// Point the distribution-specific environment at the same isolated home.
	t.Setenv("WHIPCODE_HOME", filepath.Dir(paths.Home))
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
		t.Fatal("dispatch bypassed restart approval")
	}
	metadata := invokeMain(t, "_desktop-runtime-info")
	if !strings.Contains(metadata, `"updateOwner":"desktop"`) {
		t.Fatal("diagnostics omitted desktop update ownership")
	}
	var status struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(invokeMain(t, "daemon", "status", "--json")), &status); err != nil || status.State != "unhealthy" {
		t.Fatalf("owned daemon without transport: %s, %v", status.State, err)
	}
	if data, err := os.ReadFile(options.executable); err != nil || string(data) != "old backend" {
		t.Fatal("CLI diagnostics changed the installed backend")
	}
}

func TestDesktopSyncUnsafeRuntimeDoesNotReplaceCanonicalBinary(t *testing.T) {
	source, options := syncFixture(t)
	// A file where the daemon home belongs must fail before owner shutdown.
	t.Setenv("WHIP_HOME", source)
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
				t.Setenv("WHIP_NETWORK", "invalid")
			case "no-home":
				t.Setenv("WHIP_HOME", "")
				t.Setenv("HOME", "")
			case "home-file":
				t.Setenv("WHIP_HOME", source)
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
	t.Setenv("WHIP_HOME", "")
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
	output := captureDaemonOutput(t, func() error { return tailDaemonLog(name, 1, false) })
	if output != "latest diagnostic\n" {
		t.Fatal("log tail ignored its file or line bound")
	}
}
