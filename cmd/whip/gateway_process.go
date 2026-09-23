package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/webgateway"
)

const gatewayStartupTimeout = 5 * time.Second
const gatewayShutdownTimeout = 2 * time.Second
const gatewayReadyLimit = 8192

// Cancellation is supervised below rather than using CommandContext's immediate
// kill: the lifetime pipe and SIGTERM get a bounded graceful shutdown first.
var gatewayCommand = func(executable string) *exec.Cmd { return exec.Command(executable, "_web-gateway") }

type gatewayReady struct {
	Endpoint   string `json:"endpoint,omitempty"`
	RuntimeID  string `json:"runtime_id,omitempty"`
	Generation int64  `json:"generation,omitempty"`
	Error      string `json:"error,omitempty"`
}

// gatewayEnvironment does not consult NETWORK: that variable controls automatic
// child launch only, never the explicit foreground command.
func gatewayEnvironment() webgateway.Options {
	parseList := func(value string) []string {
		var result []string
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
		return result
	}
	return webgateway.Options{
		Address:        strings.TrimSpace(os.Getenv(buildinfo.Env("LISTEN"))),
		AllowedOrigins: parseList(os.Getenv(buildinfo.Env("ALLOWED_ORIGINS"))),
		AllowedHosts:   parseList(os.Getenv(buildinfo.Env("ALLOWED_HOSTS"))),
	}
}

// managedGateway owns precisely one child. done publishes waitErr, and only the
// supervising goroutine calls stop; os/exec.Wait is called exactly once.
type managedGateway struct {
	command  *exec.Cmd
	lifetime *os.File
	done     chan struct{}
	waitErr  error
}

func startGatewayProcess(ctx context.Context, command *exec.Cmd, expected gatewayReady, timeout time.Duration) (*managedGateway, gatewayReady, error) {
	if err := ctx.Err(); err != nil {
		return nil, gatewayReady{}, err
	}
	lifetimeRead, lifetimeWrite, err := os.Pipe()
	if err != nil {
		return nil, gatewayReady{}, err
	}
	defer func() { _ = lifetimeRead.Close() }()
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		_ = lifetimeWrite.Close()
		return nil, gatewayReady{}, err
	}
	defer func() { _ = readyRead.Close(); _ = readyWrite.Close() }()
	command.ExtraFiles = []*os.File{lifetimeRead, readyWrite}
	command.Args = append(command.Args, "--parent-fd=3", "--ready-fd=4", "--runtime-id="+expected.RuntimeID, "--generation="+strconv.FormatInt(expected.Generation, 10))
	if err := command.Start(); err != nil {
		_ = lifetimeWrite.Close()
		return nil, gatewayReady{}, err
	}
	_ = lifetimeRead.Close()
	_ = readyWrite.Close()
	child := &managedGateway{command: command, lifetime: lifetimeWrite, done: make(chan struct{})}
	go func() { child.waitErr = command.Wait(); close(child.done) }()
	type result struct {
		record gatewayReady
		err    error
	}
	read := make(chan result, 1)
	readDone := make(chan struct{})
	defer func() { _ = readyRead.Close(); <-readDone }()
	go func() {
		defer close(readDone)
		var record gatewayReady
		err := json.NewDecoder(io.LimitReader(readyRead, gatewayReadyLimit)).Decode(&record)
		read <- result{record, err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var record gatewayReady
	select {
	case outcome := <-read:
		record, err = outcome.record, outcome.err
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = fmt.Errorf("gateway did not become ready within %s", timeout)
	}
	// Closing the read end unblocks the bounded decoder, including stalled children.
	if err != nil {
		_ = readyRead.Close()
		child.stop(gatewayShutdownTimeout)
		return nil, gatewayReady{}, fmt.Errorf("gateway startup: %w", err)
	}
	if record.Error != "" {
		err = errors.New(record.Error)
	} else {
		err = validateGatewayReady(record, expected)
	}
	if err == nil {
		select {
		case <-child.done:
			err = fmt.Errorf("gateway exited during startup: %v", child.waitErr)
		default:
		}
	}
	if err != nil {
		child.stop(gatewayShutdownTimeout)
		return nil, gatewayReady{}, err
	}
	return child, record, nil
}

func validateGatewayReady(record, expected gatewayReady) error {
	if record.RuntimeID == "" || record.RuntimeID != expected.RuntimeID || record.Generation != expected.Generation {
		return errors.New("gateway readiness belongs to a different daemon runtime/generation")
	}
	// Wildcard binds are valid listeners, although the user must select a real
	// host when opening them. Readiness is not browser URL selection.
	parsed, err := parseGatewayEndpoint(record.Endpoint)
	if err != nil || parsed == "" {
		return errors.New("gateway returned an invalid readiness endpoint")
	}
	return nil
}

func parseGatewayEndpoint(endpoint string) (string, error) {
	// A wildcard is allowed for managed discovery; use the ordinary validator
	// after substituting a loopback host solely for validation.
	validation := strings.ReplaceAll(strings.ReplaceAll(endpoint, "[::]", "[::1]"), "//0.0.0.0:", "//127.0.0.1:")
	return validateWebEndpoint(validation)
}

func (child *managedGateway) stop(grace time.Duration) {
	_ = child.lifetime.Close()
	select {
	case <-child.done:
		return
	default:
	}
	_ = child.command.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-child.done:
	case <-timer.C:
		_ = child.command.Process.Kill()
		<-child.done
	}
}

func manageGateway(ctx context.Context, paths daemon.RuntimePaths, generation int64, setStatus func(protocol.GatewayStatus)) {
	fail := func(err error) {
		if ctx.Err() != nil {
			setStatus(protocol.GatewayStatus{State: "stopped"})
			return
		}
		setStatus(protocol.GatewayStatus{State: "failed", Error: err.Error()})
		fmt.Fprintln(os.Stderr, "managed gateway:", err, "(daemon remains available; inspect", filepath.Join(paths.Home, "web.log")+")")
	}
	startupCtx, cancel := context.WithTimeout(ctx, gatewayStartupTimeout)
	defer cancel()
	// Socket readiness cannot wait for gateway readiness: the gateway itself
	// needs to initialize against this socket. Only CLI managed-start waits for both.
	var client *daemon.Client
	var err error
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		client, err = dialGatewayClient(startupCtx, paths)
		if err == nil {
			break
		}
		select {
		case <-startupCtx.Done():
			fail(fmt.Errorf("daemon socket not ready for gateway: %w", err))
			return
		case <-ticker.C:
		}
	}
	defer func() { _ = client.Close() }()
	initialized := client.InitializeResult()
	if initialized.Generation != generation {
		fail(errors.New("daemon generation changed before gateway startup"))
		return
	}
	expected := gatewayReady{RuntimeID: initialized.RuntimeID, Generation: initialized.Generation}
	executable, err := os.Executable()
	if err != nil {
		fail(err)
		return
	}
	log, err := os.OpenFile(filepath.Join(paths.Home, "web.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		fail(err)
		return
	}
	defer func() { _ = log.Close() }()
	if err := log.Chmod(0600); err != nil {
		fail(err)
		return
	}
	command := gatewayCommand(executable)
	command.Env = append(os.Environ(), buildinfo.Env("HOME")+"="+filepath.Dir(paths.Home))
	command.Stdout, command.Stderr = log, log
	child, ready, err := startGatewayProcess(startupCtx, command, expected, gatewayStartupTimeout)
	if err != nil {
		fail(err)
		return
	}
	cancel() // Readiness is complete; the live child is owned by ctx, not this deadline.
	defer child.stop(gatewayShutdownTimeout)
	select {
	case <-client.Done():
		fail(errors.New("daemon connection lost during gateway startup"))
		return
	default:
	}
	setStatus(protocol.GatewayStatus{State: "ready", Endpoint: ready.Endpoint})
	select {
	case <-ctx.Done():
		setStatus(protocol.GatewayStatus{State: "stopped"})
	case <-client.Done():
		fail(errors.New("daemon connection lost"))
	case <-child.done:
		fail(fmt.Errorf("gateway exited: %v; run `"+buildinfo.Name+" web` for foreground recovery", child.waitErr))
	}
}

func gatewayChildCLI(args []string) error {
	flags := flag.NewFlagSet("_web-gateway", flag.ContinueOnError)
	parentFD := flags.Int("parent-fd", 0, "parent lifetime descriptor")
	readyFD := flags.Int("ready-fd", 0, "readiness descriptor")
	runtimeID := flags.String("runtime-id", "", "owning daemon runtime")
	generation := flags.Int64("generation", 0, "owning daemon generation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *parentFD < 3 || *readyFD < 3 || *parentFD == *readyFD || *runtimeID == "" || *generation <= 0 {
		return errors.New("invalid private gateway invocation")
	}
	// ExtraFiles are deliberately inherited once, then made close-on-exec before
	// any gateway/browser/subprocess work can create descendants.
	syscall.CloseOnExec(*parentFD)
	syscall.CloseOnExec(*readyFD)
	// Make the inherited pipe pollable so Close interrupts the lifetime reader
	// on SIGINT/SIGTERM as well as on actual parent EOF.
	if err := syscall.SetNonblock(*parentFD, true); err != nil {
		return fmt.Errorf("configure parent lifetime pipe: %w", err)
	}
	lifetime := os.NewFile(uintptr(*parentFD), "gateway-parent")
	ready := os.NewFile(uintptr(*readyFD), "gateway-ready")
	defer func() { _ = ready.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watched := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, lifetime); cancel(); close(watched) }()
	defer func() { _ = lifetime.Close(); <-watched }()
	published := false
	report := func(record gatewayReady) error {
		if err := json.NewEncoder(ready).Encode(record); err != nil {
			return err
		}
		published = true
		return ready.Close()
	}
	paths, err := daemonStatusPaths()
	if err == nil {
		err = runGateway(ctx, paths, &gatewayReady{RuntimeID: *runtimeID, Generation: *generation}, report)
	}
	if err != nil && !published {
		text := err.Error()
		if len(text) > 2048 {
			text = text[:2048]
		}
		_ = report(gatewayReady{Error: text})
	}
	return err
}
