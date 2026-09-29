package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
)

type lostStopAck struct {
	net.Conn
	lose atomic.Bool
}

func (c *lostStopAck) Write(p []byte) (int, error) {
	if c.lose.Load() {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(p)
}

func TestHostStopLostAcknowledgementStillRequestsShutdown(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	stopped := make(chan struct{})
	lifecycle := NewHostLifecycle(protocol.ID(r.Identity()), protocol.ID(r.ProcessEpoch()), os.Getpid(), "fixture", time.Now(), func() { close(stopped) })
	server := &Server{runtime: r, host: HostServices{Lifecycle: lifecycle}}
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	wrapped := &lostStopAck{Conn: b}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	joined := make(chan struct{})
	go func() { defer close(joined); server.connection(ctx, wrapped) }()
	defer func() {
		_ = a.Close()
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Error("connection did not join")
		}
	}()
	send := func(method string, params any) {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(a).Encode(protocol.Request{JSONRPC: "2.0", ID: "request", Method: method, Params: raw}); err != nil {
			t.Fatal(err)
		}
	}
	send("initialize", protocol.InitializeParams{Major: 4})
	var response protocol.Response
	if err := json.NewDecoder(bufio.NewReader(a)).Decode(&response); err != nil || response.Error != nil {
		t.Fatal(err, response.Error)
	}
	wrapped.lose.Store(true)
	send("host.stop", protocol.StopHostParams{RuntimeID: protocol.ID(r.Identity()), ProcessEpoch: protocol.ID(r.ProcessEpoch())})
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("lost acknowledgement abandoned accepted stop")
	}
}
