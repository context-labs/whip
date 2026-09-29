package hostcmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func startLifecycleFixture(t *testing.T, directory string) (*client.Client, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(commandReady, 1)
	result := make(chan error, 1)
	done := make(chan struct{})
	var diagnostics bytes.Buffer
	go func() {
		defer close(done)
		result <- Run(ctx, []string{"-directory", directory, "-scripted", "-build", "fixture"}, ready, &diagnostics)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("lifecycle did not join")
		}
	})
	select {
	case <-ready:
	case err := <-result:
		t.Fatalf("startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	c, err := client.Connect(t.Context(), filepath.Join(directory, "runtime.sock"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, result
}

func TestNativeProcessStopPinsEpochAndJoins(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	c, done := startLifecycleFixture(t, directory)
	var status protocol.HostStatus
	if err := c.Call(t.Context(), "host.status", protocol.EmptyParams{}, &status); err != nil {
		t.Fatal(err)
	}
	if status.RuntimeID != c.Identity() || status.PID != os.Getpid() || status.Build != "fixture" || status.StartedAt == "" || status.ProcessEpoch == "" {
		t.Fatalf("status=%+v", status)
	}
	params := protocol.StopHostParams{RuntimeID: status.RuntimeID, ProcessEpoch: status.ProcessEpoch}
	wrong := params
	wrong.ProcessEpoch = "previous_epoch"
	var accepted protocol.HostStopAccepted
	err := c.Call(t.Context(), "host.stop", wrong, &accepted)
	var remote *client.Error
	if !errors.As(err, &remote) || remote.Kind != "IDENTITY" {
		t.Fatalf("stale stop=%v", err)
	}
	if err := c.Call(t.Context(), "host.stop", params, &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.RuntimeID != params.RuntimeID || accepted.ProcessEpoch != params.ProcessEpoch {
		t.Fatal("wrong acknowledgement")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not finish")
	}
	next, nextDone := startLifecycleFixture(t, directory)
	var nextStatus protocol.HostStatus
	if err := next.Call(t.Context(), "host.status", protocol.EmptyParams{}, &nextStatus); err != nil {
		t.Fatal(err)
	}
	if nextStatus.RuntimeID != status.RuntimeID || nextStatus.ProcessEpoch == status.ProcessEpoch {
		t.Fatalf("restart=%+v", nextStatus)
	}
	if err := next.Call(t.Context(), "host.stop", params, &accepted); !errors.As(err, &remote) || remote.Kind != "IDENTITY" {
		t.Fatalf("late stop=%v", err)
	}
	params.ProcessEpoch = nextStatus.ProcessEpoch
	if err := next.Call(t.Context(), "host.stop", params, &accepted); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-nextDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart stop did not finish")
	}
}

func TestNativeProcessNetworkCannotStop(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	c, _ := startLifecycleFixture(t, directory)
	var status protocol.HostStatus
	if err := c.Call(t.Context(), "host.status", protocol.EmptyParams{}, &status); err != nil {
		t.Fatal(err)
	}
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", filepath.Join(directory, "runtime.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(conn)
	send := func(method string, params any) protocol.Response {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(conn).Encode(protocol.Request{JSONRPC: "2.0", ID: "request", Method: method, Params: raw}); err != nil {
			t.Fatal(err)
		}
		if !scan.Scan() {
			t.Fatal("missing response", scan.Err())
		}
		var response protocol.Response
		if err := json.Unmarshal(scan.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	if result := send("initialize", protocol.InitializeParams{Major: 4, NetworkClient: true}); result.Error != nil {
		t.Fatal(result.Error)
	}
	result := send("host.stop", protocol.StopHostParams{RuntimeID: status.RuntimeID, ProcessEpoch: status.ProcessEpoch})
	if result.Error == nil || result.Error.Kind != "NETWORK_RESTRICTED" {
		t.Fatalf("network stop=%+v", result)
	}
	if err := c.Call(t.Context(), "host.status", protocol.EmptyParams{}, &status); err != nil {
		t.Fatal("network stopped host", err)
	}
}
