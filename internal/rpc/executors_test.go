package rpc_test

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

type executorSocket struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
}

func openExecutorSocket(t *testing.T, path string) *executorSocket {
	t.Helper()
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	socket := &executorSocket{conn: conn, encoder: json.NewEncoder(conn), decoder: json.NewDecoder(conn)}
	if response := socket.call(t, "initialize", protocol.InitializeParams{Major: 4}); response.Error != nil {
		t.Fatal(response.Error)
	}
	return socket
}

func (s *executorSocket) call(t *testing.T, method string, params any) protocol.Response {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.encoder.Encode(protocol.Request{JSONRPC: "2.0", ID: protocol.ID(method), Method: method, Params: raw}); err != nil {
		t.Fatal(err)
	}
	var response protocol.Response
	if err := s.decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestExecutorSocketBindsOnlyRegisteredExactCoverageAndRevokesOldHolder(t *testing.T) {
	r, c := fixture(t)
	definition := call[protocol.Definition](t, c, "definitions.register", protocol.DefinitionDocument{ID: "executor-socket", Name: "Socket", Defaults: protocol.ConfigPatch{Tools: map[string]protocol.ToolDeclaration{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}})
	first := openExecutorSocket(t, r.SocketPath())
	bind := protocol.ExecutorBindParams{Definition: definition.Ref, Tools: []protocol.ID{"lookup"}, Hooks: []protocol.ID{}}
	response := first.call(t, "executor.bind", bind)
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	var lease protocol.ExecutorLease
	if err := json.Unmarshal(response.Result, &lease); err != nil {
		t.Fatal(err)
	}
	params := protocol.ExecutorPendingParams{Epoch: lease.Epoch, Definition: lease.Definition, Generation: lease.Generation}
	if response := first.call(t, "executor.pending", params); response.Error != nil {
		t.Fatal(response.Error)
	}
	second := openExecutorSocket(t, r.SocketPath())
	if response := second.call(t, "executor.bind", bind); response.Error != nil {
		t.Fatal(response.Error)
	}
	for _, socket := range []*executorSocket{first, second} {
		if response := socket.call(t, "executor.pending", params); response.Error == nil || response.Error.Kind != "CONFLICT" {
			t.Fatal("foreign/stale lease accepted", response)
		}
	}
	if response := second.call(t, "initialize", protocol.InitializeParams{Major: 4}); response.Error == nil {
		t.Fatal("executor connection reinitialized")
	}
	if response := second.call(t, "tool.result", protocol.ExecutorToolResultParams{Epoch: lease.Epoch, Generation: lease.Generation, InvocationID: "invented", OutputBase64: new("bnVsbA==")}); response.Error == nil || response.Error.Kind != "CONFLICT" {
		t.Fatal("invented invocation accepted", response)
	}
	invalid := openExecutorSocket(t, r.SocketPath())
	bind.Tools = []protocol.ID{}
	if response := invalid.call(t, "executor.bind", bind); response.Error == nil {
		t.Fatal("missing declared coverage accepted")
	}
	var extra protocol.Response
	if err := invalid.decoder.Decode(&extra); err == nil {
		t.Fatal("failed first bind retained a peer")
	}
}
