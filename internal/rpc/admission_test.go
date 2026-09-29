package rpc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

func admissionServer(t *testing.T) (*rpc.Server, string, context.CancelFunc, <-chan error) {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "whip-admission-") //nolint:usetesting // macOS Unix socket paths must remain shorter than 104 bytes.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	server, err := rpc.Listen(r, rpc.HostServices{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); admissionJoined(t, done) })
	return server, r.SocketPath(), cancel, done
}

func admissionJoined(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RPC admission or connection workers did not join")
	}
}

func admissionPeer(t *testing.T, socket string) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":\"initialize\",\"method\":\"initialize\",\"params\":{\"major\":4}}\n")); err != nil {
		t.Fatal(err)
	}
	return conn
}

func admissionResponse(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var response protocol.Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "initialize" || response.Error != nil {
		t.Fatalf("unexpected initialization: %+v", response)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func fillAdmission(t *testing.T, socket string) []net.Conn {
	t.Helper()
	peers := make([]net.Conn, 64)
	for index := range peers {
		peers[index] = admissionPeer(t, socket)
		admissionResponse(t, peers[index])
	}
	return peers
}

func pendingAdmission(t *testing.T, socket string) net.Conn {
	t.Helper()
	conn := admissionPeer(t, socket)
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err := conn.Read(make([]byte, 1))
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("full server must leave the request waiting without a response or disconnect: %v", err)
	}
	return conn
}

func TestRPCAdmissionWaitsAt64WithoutDroppingTheNextConnection(t *testing.T) {
	_, socket, _, _ := admissionServer(t)
	peers := fillAdmission(t, socket)
	pending := pendingAdmission(t, socket)
	if err := peers[0].Close(); err != nil {
		t.Fatal(err)
	}
	admissionResponse(t, pending)
	// The accepted waiter occupies the released slot; a second waiter still waits.
	next := pendingAdmission(t, socket)
	if err := pending.Close(); err != nil {
		t.Fatal(err)
	}
	admissionResponse(t, next)
}

func TestRPCAdmissionShutdownJoinsFullServerAndPendingClients(t *testing.T) {
	for _, stop := range []string{"close", "cancel"} {
		t.Run(stop, func(t *testing.T) {
			server, socket, cancel, done := admissionServer(t)
			peers := fillAdmission(t, socket)
			pending := pendingAdmission(t, socket)
			if stop == "close" {
				if err := server.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			admissionJoined(t, done)
			for _, peer := range append(peers, pending) {
				if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				_, err := peer.Read(make([]byte, 1))
				var timeout net.Error
				if err == nil || errors.As(err, &timeout) && timeout.Timeout() {
					t.Fatalf("shutdown left a live peer: %v", err)
				}
			}
		})
	}
}

func TestRPCAdmissionClientCloseDoesNotKeepCapacityOrNeedAReplay(t *testing.T) {
	_, socket, _, _ := admissionServer(t)
	peers := fillAdmission(t, socket)
	cancelled := pendingAdmission(t, socket)
	if err := cancelled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := peers[0].Close(); err != nil {
		t.Fatal(err)
	}
	// The canceled queued socket is retired, allowing the next exact request in.
	next := admissionPeer(t, socket)
	admissionResponse(t, next)
}

func TestRPCAdmissionCloseCancelsAnActiveRequestBeforeJoining(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"osascript", "zenity"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf started > \"$0.started\"\nexec /bin/sleep 30\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	server, socket, _, done := admissionServer(t)
	conn := admissionPeer(t, socket)
	admissionResponse(t, conn)
	if _, err := conn.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":\"held-request\",\"method\":\"host.directory.pick\",\"params\":{\"start\":\"\"}}\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, macErr := os.Stat(filepath.Join(bin, "osascript.started"))
		_, linuxErr := os.Stat(filepath.Join(bin, "zenity.started"))
		if macErr == nil || linuxErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned fake picker was not dispatched")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	admissionJoined(t, done)
}
