package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/hostcmd"
	"github.com/context-labs/whip/internal/localruntime"
	"golang.org/x/sys/unix"
)

// The fixture executable only serves a scripted host in a test-owned directory.
func TestNativeUpdateRuntimeChild(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "_native-runtime" {
			continue
		}
		for j, flag := range os.Args {
			if flag == "-directory" && j+1 < len(os.Args) {
				p := filepath.Join(filepath.Dir(os.Args[j+1]), "children")
				f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = fmt.Fprintln(f, os.Getpid())
				if err := errors.Join(err, f.Close()); err != nil {
					t.Fatal(err)
				}
			}
		}
		args := append([]string{"-scripted"}, os.Args[i+1:]...)
		if err := hostcmd.Run(t.Context(), args, io.Discard, os.Stderr); err != nil {
			t.Fatal(err)
		}
		return
	}
}

func nativeUpdateExecutable(t *testing.T, metadata string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "whipcode")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nif [ \"$1\" = _desktop-runtime-info ]; then\nprintf '%s' " + quote(metadata) + "\nelse\nexec " + quote(self) + " '-test.run=^TestNativeUpdateRuntimeChild$' -- \"$@\"\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

const updatedNativeMetadata = `{"distribution":"whipcode","buildId":"updated-fixture","updateOwner":"standalone","protocolMajor":4}`

func TestNativeUpdateRestartsOnlyOwnedHostOnInstalledBuild(t *testing.T) {
	home := nativeDaemonHome(t)
	t.Setenv("WHIPCODE_NETWORK", "false")
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	// Reap every owned child, even when readiness or an assertion fails. These
	// unreaped children cannot have had their PIDs reused by another process.
	t.Cleanup(func() {
		raw, _ := os.ReadFile(filepath.Join(home, "children"))
		for field := range strings.FieldsSeq(string(raw)) {
			pid, err := strconv.Atoi(field)
			if err != nil || pid <= 0 {
				t.Error("invalid fixture child", field)
				continue
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &status, 0, nil)
		}
	})
	legacy := filepath.Join(home, "config.json")
	if err := os.WriteFile(legacy, []byte("untouched retired configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := nativeUpdateExecutable(t, updatedNativeMetadata)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	first, err := localruntime.Start(ctx, paths, localruntime.Launch{Executable: executable, Arguments: []string{"_native-runtime"}, Build: "previous-fixture"})
	if err != nil || first.Process == nil {
		t.Fatal(first, err)
	}
	bad := nativeUpdateExecutable(t, `{"distribution":"wrong","buildId":"wrong","updateOwner":"standalone","protocolMajor":4}`)
	if err := restartUpdatedNativeRuntime(bad); err == nil {
		t.Fatal("invalid replacement metadata accepted")
	}
	if after := localruntime.Inspect(ctx, paths); after.Process == nil || after.Process.ProcessEpoch != first.Process.ProcessEpoch {
		t.Fatal("invalid replacement stopped existing host", after)
	}
	if err := restartUpdatedNativeRuntime(executable); err != nil {
		t.Fatal(err)
	}
	after := localruntime.Inspect(ctx, paths)
	if after.Process == nil || after.Process.RuntimeID != first.Process.RuntimeID || after.Process.ProcessEpoch == first.Process.ProcessEpoch || after.Process.Build != "updated-fixture" {
		t.Fatal("restart identity/build", first, after)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "untouched retired configuration" {
		t.Fatal(string(data), err)
	}
	if _, err := localruntime.Stop(ctx, paths); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(home, "children"))
	if err != nil {
		t.Fatal(err)
	}
	if err := restartUpdatedNativeRuntime("/missing/whipcode"); err != nil {
		t.Fatal("stopped host inspected replacement or launched", err)
	}
	if data, err := os.ReadFile(filepath.Join(home, "children")); err != nil || string(data) != string(before) {
		t.Fatal("update launched absent host", err)
	}
}

func TestNativeUpdateAbsentAndUnverifiedOwners(t *testing.T) {
	home := nativeDaemonHome(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := restartUpdatedNativeRuntime("/absent/executable"); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatal("absent host initialized", entries, err)
	}
	if err := os.Mkdir(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if err := restartUpdatedNativeRuntime("/absent/executable"); err == nil || !strings.Contains(err.Error(), "unverified") {
		t.Fatal(err)
	}
}

func TestNativeUpdateMetadataBoundsAndIdentity(t *testing.T) {
	for name, metadata := range map[string]string{
		"large":    strings.Repeat("x", 4097),
		"invalid":  `{`,
		"empty":    `{}`,
		"desktop":  strings.ReplaceAll(updatedNativeMetadata, "standalone", "desktop"),
		"protocol": strings.ReplaceAll(updatedNativeMetadata, `"protocolMajor":4`, `"protocolMajor":3`),
		"build":    strings.ReplaceAll(updatedNativeMetadata, "updated-fixture", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := updatedRuntimeLaunch(t.Context(), nativeUpdateExecutable(t, metadata)); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestUpdateCLIRestartFailureDoesNotPromiseReconnection(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	stubShell(t, "0")
	previous := restartDaemonAfterUpdate
	restartDaemonAfterUpdate = func(string) error { return errors.New("unverified owner") }
	t.Cleanup(func() { restartDaemonAfterUpdate = previous })
	var err error
	output := captureStdout(t, func() { err = updateCLI() })
	if err == nil || !strings.Contains(err.Error(), "updated, but native runtime restart was not confirmed") || strings.Contains(output, "new connections use") {
		t.Fatal(output, err)
	}
}
