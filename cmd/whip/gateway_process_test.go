package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
)

// The test executable dispatches the same private runner without requiring a
// release build or installed executable. Production always executes itself.
func TestGatewayProcessHelper(t *testing.T) {
	mode := os.Getenv("WHIP_TEST_GATEWAY_HELPER")
	if mode == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	if home := os.Getenv("WHIP_TEST_GATEWAY_HOME"); home != "" {
		t.Setenv(buildinfo.Env("HOME"), home)
	}
	if mode == "child" {
		if path := os.Getenv("WHIP_TEST_GATEWAY_PID"); path != "" {
			if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		gatewayAssetsAvailable = func() bool { return true }
		if err := gatewayChildCLI(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if mode == "parent" {
		paths, err := daemonStatusPaths()
		if err != nil {
			t.Fatal(err)
		}
		client, err := dialGatewayClient(t.Context(), paths)
		if err != nil {
			t.Fatal(err)
		}
		init := client.InitializeResult()
		expected := gatewayReady{RuntimeID: init.RuntimeID, Generation: init.Generation}
		command := gatewayTestCommand(t, "child")
		child, ready, err := startGatewayProcess(t.Context(), command, expected, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(os.Stdout).Encode(struct {
			gatewayReady
			PID int `json:"pid"`
		}{ready, child.command.Process.Pid})
		<-child.done
		os.Exit(0)
	}
	fs := flag.NewFlagSet("helper", flag.ContinueOnError)
	parentFD := fs.Int("parent-fd", 0, "")
	readyFD := fs.Int("ready-fd", 0, "")
	runtimeID := fs.String("runtime-id", "", "")
	generation := fs.Int64("generation", 0, "")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	syscall.CloseOnExec(*parentFD)
	syscall.CloseOnExec(*readyFD)
	parent := os.NewFile(uintptr(*parentFD), "parent")
	ready := os.NewFile(uintptr(*readyFD), "ready")
	if mode == "fail" {
		json.NewEncoder(ready).Encode(gatewayReady{Error: "fixture bind failed"})
		os.Exit(1)
	}
	if mode == "oversize" {
		io.WriteString(ready, `{"error":"`+strings.Repeat("x", gatewayReadyLimit+100)+`"}`)
		os.Exit(1)
	}
	if mode == "exit" {
		os.Exit(7)
	}
	if mode == "stall" {
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
		os.Exit(2)
	}
	record := gatewayReady{Endpoint: "http://127.0.0.1:12345", RuntimeID: *runtimeID, Generation: *generation}
	if mode == "wrong" {
		record.Generation++
	}
	if mode == "invalid-endpoint" {
		record.Endpoint = "file:///secret"
	}
	json.NewEncoder(ready).Encode(record)
	ready.Close()
	if mode == "stubborn" {
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
		os.Exit(2)
	}
	_, _ = io.Copy(io.Discard, parent)
	os.Exit(0)
}

func gatewayTestCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestGatewayProcessHelper$", "--")
	command.Env = append(os.Environ(), "WHIP_TEST_GATEWAY_HELPER="+mode)
	return command
}

func TestManagedGatewayReadinessAndBoundedFailures(t *testing.T) {
	for _, test := range []struct{ name, mode, want string }{
		{name: "ready", mode: "ready"},
		{name: "reported failure", mode: "fail", want: "fixture bind failed"},
		{name: "wrong generation", mode: "wrong", want: "different daemon"},
		{name: "invalid endpoint", mode: "invalid-endpoint", want: "invalid readiness"},
		{name: "oversized readiness", mode: "oversize", want: "gateway startup"},
		{name: "child exit", mode: "exit", want: "gateway startup"},
		{name: "startup timeout", mode: "stall", want: "did not become ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := gatewayTestCommand(t, test.mode)
			expected := gatewayReady{RuntimeID: "fixture-runtime", Generation: 41}
			timeout := 2 * time.Second
			if test.mode == "stall" {
				timeout = 250 * time.Millisecond
			}
			child, ready, err := startGatewayProcess(t.Context(), command, expected, timeout)
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					if child != nil {
						child.stop(time.Second)
					}
					t.Fatalf("error %v", err)
				}
				if command.ProcessState == nil {
					t.Fatal("failed child was not reaped")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			child.stop(time.Second)
			if ready.RuntimeID != expected.RuntimeID || ready.Generation != expected.Generation || command.ProcessState == nil {
				t.Fatalf("readiness %+v, state %v", ready, command.ProcessState)
			}
		})
	}
}

func TestManagedGatewayStopEscalatesAndReaps(t *testing.T) {
	command := gatewayTestCommand(t, "stubborn")
	child, _, err := startGatewayProcess(t.Context(), command, gatewayReady{RuntimeID: "fixture", Generation: 1}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	child.stop(50 * time.Millisecond)
	if time.Since(start) > time.Second || command.ProcessState == nil {
		t.Fatal("stubborn child was not promptly reaped")
	}
	child.stop(time.Second) // shutdown is safe after observing an already exited child
}

func TestManagedGatewayCancelledStartupReaps(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command := gatewayTestCommand(t, "stall")
	child, _, err := startGatewayProcess(ctx, command, gatewayReady{RuntimeID: "fixture", Generation: 1}, time.Second)
	if child != nil {
		child.stop(time.Second)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	if command.Process != nil {
		t.Fatal("cancelled startup launched a child")
	}
}

func TestGatewayChildRejectsNonPrivateInvocations(t *testing.T) {
	for _, args := range [][]string{nil, {"--parent-fd=3"}, {"--parent-fd=3", "--ready-fd=3", "--runtime-id=test", "--generation=1"}} {
		if err := gatewayChildCLI(args); err == nil {
			t.Fatalf("accepted args %v", args)
		}
	}
}

func TestManagedGatewayParentDeathClosesListener(t *testing.T) {
	t.Setenv("WHIPCODE_NETWORK", "0")
	t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:0")
	paths := startWebTestDaemon(t)
	t.Setenv("WHIP_TEST_GATEWAY_HOME", filepath.Dir(paths.Home))
	command := gatewayTestCommand(t, "parent")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			command.Wait()
		}
	}()
	type owned struct {
		gatewayReady
		PID int `json:"pid"`
	}
	records := make(chan owned, 1)
	readErr := make(chan error, 1)
	go func() {
		var record owned
		err := json.NewDecoder(bufio.NewReader(stdout)).Decode(&record)
		if err != nil {
			readErr <- err
		} else {
			records <- record
		}
	}()
	var record owned
	select {
	case record = <-records:
	case err := <-readErr:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("parent did not report child")
	}
	defer func() {
		if record.PID > 0 {
			process, _ := os.FindProcess(record.PID)
			_ = process.Kill()
		}
	}()
	if record.Endpoint == "" || record.PID == 0 {
		t.Fatalf("record %+v", record)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	waited = true
	httpClient := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, record.Endpoint+"/api/v3/web", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			status, client := probeDaemon(paths, time.Second)
			if client == nil {
				t.Fatalf("parent death stopped daemon %+v", status)
			}
			client.Close()
			record.PID = 0 // EOF shutdown succeeded; do not signal a potentially recycled PID.
			return
		}
		response.Body.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("gateway survived parent SIGKILL")
}

func TestManagedGatewayStatusFailureAndRepeatedStart(t *testing.T) {
	t.Setenv("WHIPCODE_NETWORK", "1")
	t.Setenv("WHIP_TEST_GATEWAY_HELPER", "fail")
	previousCommand := gatewayCommand
	gatewayCommand = func(executable string) *exec.Cmd {
		want, _ := os.Executable()
		if executable != want {
			t.Errorf("gateway uses %s, want own executable %s", executable, want)
		}
		return exec.CommandContext(t.Context(), executable, "-test.run=^TestGatewayProcessHelper$", "--")
	}
	t.Cleanup(func() { gatewayCommand = previousCommand })
	previousLaunch := launchManagedDaemon
	t.Cleanup(func() { launchManagedDaemon = previousLaunch })
	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	launches := 0
	launchManagedDaemon = func(daemon.RuntimePaths) error { launches++; go func() { done <- runDaemon(ctx, nil) }(); return nil }
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	}()
	status, err := startManagedDaemon(paths, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "daemon is running") || status.State != "running" || status.Gateway.State != "failed" {
		t.Fatalf("start = %+v %v", status, err)
	}
	if status.NetworkEndpoint != "" {
		t.Fatalf("failed gateway advertised: %+v", status)
	}
	status, err = startManagedDaemon(paths, time.Second)
	if err != nil || status.State != "running" || launches != 1 {
		t.Fatalf("repeated start = %+v %v launches=%d", status, err, launches)
	}
	oldTail := tailDaemonLog
	t.Cleanup(func() { tailDaemonLog = oldTail })
	tailDaemonLog = func(path string, lines int, follow bool) error {
		if path != filepath.Join(paths.Home, "web.log") {
			t.Fatalf("wrong gateway log: %s", path)
		}
		return nil
	}
	if err := daemonLogsCLI([]string{"--web"}); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayReadinessRejectsWrongRuntimeAndAllowsTrustedBind(t *testing.T) {
	expected := gatewayReady{RuntimeID: "one", Generation: 2}
	for _, endpoint := range []string{"http://127.0.0.1:1234", "http://0.0.0.0:4444", "http://[::]:4444"} {
		record := expected
		record.Endpoint = endpoint
		if err := validateGatewayReady(record, expected); err != nil {
			t.Fatal(err)
		}
		record.RuntimeID = "two"
		if err := validateGatewayReady(record, expected); err == nil {
			t.Fatal("wrong runtime accepted")
		}
	}
}

func TestGatewayEnvironmentDistributionAndTerminalIndependence(t *testing.T) {
	t.Setenv("WHIP_NETWORK", "1")
	t.Setenv("WHIP_LISTEN", "127.0.0.1:1234")
	t.Setenv("WHIPCODE_NETWORK", "0")
	t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:4567")
	t.Setenv("WHIPCODE_NETWORK_TERMINALS", "1")
	options, err := daemonNetworkEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if options.Enabled || !options.Terminals || options.Address != "127.0.0.1:4567" {
		t.Fatalf("options %+v", options)
	}
	t.Setenv("WHIPCODE_NETWORK_TERMINALS", "invalid")
	if _, err := daemonNetworkEnvironment(); err == nil || !strings.Contains(err.Error(), "WHIPCODE_NETWORK_TERMINALS") {
		t.Fatalf("error %v", err)
	}
}

func TestGatewayChildSignalsDoNotStopDaemon(t *testing.T) {
	t.Setenv("WHIPCODE_NETWORK", "0")
	t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:0")
	paths := startWebTestDaemon(t)
	t.Setenv("WHIP_TEST_GATEWAY_HOME", filepath.Dir(paths.Home))
	client, err := dialGatewayClient(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	initialized := client.InitializeResult()
	client.Close()
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			command := gatewayTestCommand(t, "child")
			child, _, err := startGatewayProcess(t.Context(), command, gatewayReady{RuntimeID: initialized.RuntimeID, Generation: initialized.Generation}, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer child.stop(time.Second)
			if err := command.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-child.done:
				if child.waitErr != nil {
					t.Fatalf("signal shutdown: %v", child.waitErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("signal failed to settle lifetime reader and gateway")
			}
			status, client := probeDaemon(paths, time.Second)
			if client == nil {
				t.Fatalf("signal stopped daemon: %+v", status)
			}
			client.Close()
		})
	}
}

func TestManagedGatewayRealChildReadyCrashAndDaemonStop(t *testing.T) {
	for _, action := range []string{"crash", "daemon stop"} {
		t.Run(action, func(t *testing.T) {
			t.Setenv("WHIPCODE_NETWORK", "1")
			t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:0")
			t.Setenv("WHIP_TEST_GATEWAY_HELPER", "child")
			pidPath := filepath.Join(t.TempDir(), "gateway.pid")
			t.Setenv("WHIP_TEST_GATEWAY_PID", pidPath)
			previous := gatewayCommand
			var launches atomic.Int32
			gatewayCommand = func(executable string) *exec.Cmd {
				launches.Add(1)
				return exec.CommandContext(t.Context(), executable, "-test.run=^TestGatewayProcessHelper$", "--")
			}
			t.Cleanup(func() { gatewayCommand = previous })
			paths := startWebTestDaemon(t)
			deadline := time.Now().Add(5 * time.Second)
			var ready daemonStatus
			for time.Now().Before(deadline) {
				status, client := probeDaemon(paths, time.Second)
				if client != nil {
					client.Close()
				}
				if status.Gateway.State == "failed" {
					t.Fatalf("gateway failed: %+v", status)
				}
				if status.Gateway.State == "ready" {
					ready = status
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if ready.NetworkEndpoint == "" || ready.NetworkEndpoint != ready.Gateway.Endpoint {
				t.Fatalf("no ready discovery: %+v", ready)
			}
			data, err := os.ReadFile(pidPath)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil || pid <= 0 {
				t.Fatalf("child PID %q %v", data, err)
			}
			process, err := os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			defer process.Release()
			if action == "crash" {
				if err := process.Kill(); err != nil {
					t.Fatal(err)
				}
				deadline = time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					status, client := probeDaemon(paths, time.Second)
					if client == nil {
						t.Fatalf("child crash stopped daemon: %+v", status)
					}
					client.Close()
					if status.Gateway.State == "failed" {
						// Status can be newer than the initialize snapshot on that same
						// connection; a fresh initialize must now omit the dead endpoint.
						fresh, err := dialGatewayClient(t.Context(), paths)
						if err != nil {
							t.Fatal(err)
						}
						initialized := fresh.InitializeResult()
						fresh.Close()
						if status.NetworkEndpoint != "" || initialized.NetworkEndpoint != "" {
							t.Fatalf("dead endpoint advertised: %+v", status)
						}
						if launches.Load() != 1 {
							t.Fatalf("gateway restarted %d times", launches.Load())
						}
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("child crash was not reported")
			}
			if _, err := stopManagedDaemon(paths, 5*time.Second, false); err != nil {
				t.Fatal(err)
			}
			httpClient := &http.Client{Timeout: time.Second}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ready.NetworkEndpoint+"/api/v3/web", nil)
			if err != nil {
				t.Fatal(err)
			}
			if response, err := httpClient.Do(request); err == nil {
				response.Body.Close()
				t.Fatal("daemon stop retained gateway listener")
			}
			if err := process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("owned child was not reaped: %v", err)
			}
		})
	}
}
