package localruntime_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/hostcmd"
	"github.com/context-labs/whip/internal/localruntime"
)

func TestLocalRuntimeChild(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--native-runtime-fixture" {
			for j, value := range os.Args {
				if value == "-directory" && j+1 < len(os.Args) {
					if err := os.WriteFile(filepath.Join(filepath.Dir(os.Args[j+1]), "fixture.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := hostcmd.Run(t.Context(), os.Args[i+1:], io.Discard, os.Stderr); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}

func fixturePaths(t *testing.T) localruntime.Paths {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "whip-local-") //nolint:usetesting // Keep macOS Unix socket paths short; cleanup is registered immediately.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if raw, err := os.ReadFile(filepath.Join(directory, "fixture.pid")); err == nil {
			if pid, err := strconv.Atoi(string(raw)); err == nil && pid > 0 {
				// This unreaped test child cannot reuse its PID. Always reap even
				// when a readiness assertion fails before a protocol stop.
				_ = syscall.Kill(pid, syscall.SIGKILL)
				var status syscall.WaitStatus
				_, _ = syscall.Wait4(pid, &status, 0, nil)
			}
		}
		_ = os.RemoveAll(directory)
	})
	paths, err := localruntime.Resolve(directory)
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func fixtureLaunch(t *testing.T) localruntime.Launch {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return localruntime.Launch{Executable: executable, Arguments: []string{"-test.run=^TestLocalRuntimeChild$", "--", "--native-runtime-fixture", "-scripted"}, Build: "fixture"}
}

func TestStartObserveStopAndConcurrentStart(t *testing.T) {
	paths := fixturePaths(t)
	if status := localruntime.Inspect(t.Context(), paths); status.State != "stopped" {
		t.Fatal(status)
	}
	if _, err := os.Stat(paths.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status created storage", err)
	}
	legacy := filepath.Join(filepath.Dir(paths.Directory), "config.json")
	if err := os.WriteFile(legacy, []byte("retired data stays intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	launch := fixtureLaunch(t)
	results := make(chan localruntime.Status, 3)
	var workers sync.WaitGroup
	for range 3 {
		workers.Go(func() {
			status, err := localruntime.Start(t.Context(), paths, launch)
			if err != nil {
				raw, _ := os.ReadFile(paths.Log)
				t.Errorf("%v; log: %s", err, raw)
			}
			results <- status
		})
	}
	workers.Wait()
	close(results)
	var selected localruntime.Status
	for status := range results {
		if status.Process == nil {
			t.Fatal(status)
		}
		if selected.Process != nil && selected.Process.ProcessEpoch != status.Process.ProcessEpoch {
			t.Fatal("concurrent start replaced owner")
		}
		selected = status
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = localruntime.Stop(ctx, paths)
	})
	if selected.State != "running" || selected.Process.Build != "fixture" {
		t.Fatal(selected)
	}
	stopped, err := localruntime.Stop(t.Context(), paths)
	if err != nil || !stopped {
		t.Fatal(stopped, err)
	}
	stopped, err = localruntime.Stop(t.Context(), paths)
	if err != nil || stopped {
		t.Fatal(stopped, err)
	}
	raw, err := os.ReadFile(legacy)
	if err != nil || string(raw) != "retired data stays intact" {
		t.Fatal("retired data changed", string(raw), err)
	}
}

func TestLocalPathsAndLaunchFailClosed(t *testing.T) {
	paths := fixturePaths(t)
	if _, err := localruntime.Resolve(""); err == nil {
		t.Fatal("accepted empty home")
	}
	bad := paths
	bad.Log = filepath.Join(filepath.Dir(paths.Directory), "outside")
	if _, err := localruntime.Start(t.Context(), bad, fixtureLaunch(t)); err == nil {
		t.Fatal("accepted forged log")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := localruntime.Start(ctx, paths, fixtureLaunch(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled start created storage")
	}
	if err := os.Symlink(t.TempDir(), paths.Directory); err != nil {
		t.Fatal(err)
	}
	if _, err := localruntime.Start(t.Context(), paths, fixtureLaunch(t)); err == nil {
		t.Fatal("accepted symlink directory")
	}
	if _, err := localruntime.Stop(t.Context(), paths); err == nil {
		t.Fatal("stopped unverifiable owner")
	}
}

func TestUnsafeLogIsNeverFollowed(t *testing.T) {
	paths := fixturePaths(t)
	if err := os.Mkdir(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, paths.Log); err != nil {
		t.Fatal(err)
	}
	if _, err := localruntime.Start(t.Context(), paths, fixtureLaunch(t)); err == nil {
		t.Fatal("followed log symlink")
	}
	raw, err := os.ReadFile(outside)
	if err != nil || string(raw) != "original" {
		t.Fatal("changed external file", err)
	}
}

func TestMaintenanceExcludesLaunchAndStopsOnlySelectedEpoch(t *testing.T) {
	paths := fixturePaths(t)
	lease, err := localruntime.AcquireMaintenance(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	wait, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := localruntime.Start(wait, paths, fixtureLaunch(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("starter bypassed maintenance", err)
	}
	if status := localruntime.Inspect(t.Context(), paths); status.Process != nil {
		t.Fatal("blocked starter created host", status)
	}
	selected, err := lease.Start(t.Context(), fixtureLaunch(t))
	if err != nil || selected.Process == nil {
		t.Fatal(selected, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = localruntime.Stop(ctx, paths)
	})
	stale := *selected.Process
	stale.ProcessEpoch += "_stale"
	if err := lease.Stop(t.Context(), stale); err == nil {
		t.Fatal("stale selection stopped live host")
	}
	if current := localruntime.Inspect(t.Context(), paths); current.Process == nil || current.Process.ProcessEpoch != selected.Process.ProcessEpoch {
		t.Fatal(current)
	}
	if err := lease.Stop(t.Context(), *selected.Process); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Start(t.Context(), fixtureLaunch(t)); err == nil {
		t.Fatal("closed lease started host")
	}
	if err := lease.Stop(t.Context(), *selected.Process); err == nil {
		t.Fatal("closed lease stopped host")
	}
	reopened, err := localruntime.AcquireMaintenance(t.Context(), paths)
	if err != nil {
		t.Fatal("close retained launch exclusion", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedGatewayReadinessAndFailureIsolation(t *testing.T) {
	for _, mode := range []string{"ready", "occupied", "existing-local"} {
		t.Run(mode, func(t *testing.T) {
			paths := fixturePaths(t)
			launch := fixtureLaunch(t)
			launch.WaitForWeb = true
			address := "127.0.0.1:0"
			if mode == "occupied" {
				listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = listener.Close() }()
				address = listener.Addr().String()
			}
			if mode == "existing-local" {
				local := fixtureLaunch(t)
				if _, err := localruntime.Start(t.Context(), paths, local); err != nil {
					t.Fatal(err)
				}
			}
			launch.Arguments = append(launch.Arguments, "-web", "-web-listen", address)
			status, err := localruntime.Start(t.Context(), paths, launch)
			if status.State != "running" || status.Process == nil {
				t.Fatalf("host unavailable: %+v; %v", status, err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := localruntime.Stop(ctx, paths); err != nil {
					t.Error(err)
				}
			})
			switch mode {
			case "ready":
				if err != nil || status.Process.WebState != "running" || status.Process.WebEndpoint == "" {
					t.Fatalf("premature readiness: %+v; %v", status.Process, err)
				}
			case "occupied":
				if err == nil || !strings.Contains(err.Error(), "browser gateway failed") || status.Process.WebState != "failed" || status.Process.WebError == "" || status.Process.WebEndpoint != "" {
					t.Fatalf("failure hidden: %+v; %v", status.Process, err)
				}
			case "existing-local":
				if err != nil || status.Process.WebState != "" {
					t.Fatalf("existing host changed: %+v; %v", status.Process, err)
				}
			}
			attached, err := localruntime.Start(t.Context(), paths, fixtureLaunch(t))
			if err != nil || attached.Process == nil || attached.Process.ProcessEpoch != status.Process.ProcessEpoch {
				t.Fatalf("local attachment replaced or rejected: %+v; %v", attached, err)
			}
			observed := localruntime.Inspect(t.Context(), paths)
			if observed.Process == nil || observed.Process.ProcessEpoch != status.Process.ProcessEpoch {
				t.Fatal("host lost after gateway result", observed)
			}
		})
	}
}

func TestLongHomeLaunchAndDiscoveryPreserveRuntimeIdentity(t *testing.T) {
	paths := fixturePaths(t)
	home := filepath.Join(filepath.Dir(paths.Directory), strings.Repeat("long", 40))
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	paths, err := localruntime.Resolve(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = localruntime.Stop(ctx, paths)
		_ = os.Remove(filepath.Dir(paths.Socket))
	})
	if filepath.Dir(paths.Socket) == paths.Directory || len(paths.Socket) > 100 {
		t.Fatal(paths)
	}
	if status := localruntime.Inspect(t.Context(), paths); status.State != "stopped" {
		t.Fatal(status)
	}
	if _, err := os.Stat(filepath.Dir(paths.Socket)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created fallback", err)
	}
	first, err := localruntime.Start(t.Context(), paths, fixtureLaunch(t))
	if err != nil || first.Process == nil {
		t.Fatal(first, err)
	}
	for _, file := range []string{"state.db", "host.json", "runtime.lock"} {
		if _, err := os.Stat(filepath.Join(paths.Directory, file)); err != nil {
			t.Fatal("durable storage moved", file, err)
		}
	}
	if stopped, err := localruntime.Stop(t.Context(), paths); err != nil || !stopped {
		t.Fatal(stopped, err)
	}
	second, err := localruntime.Start(t.Context(), paths, fixtureLaunch(t))
	if err != nil || second.Process == nil {
		t.Fatal(second, err)
	}
	if second.Process.RuntimeID != first.Process.RuntimeID || second.Process.ProcessEpoch == first.Process.ProcessEpoch {
		t.Fatal("restart changed durable identity", first, second)
	}
}
