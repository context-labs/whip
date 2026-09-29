package rpc_test

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
)

func TestNetworkMarkerCannotBeDowngradedOnInitializedSocket(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		r, _ := fixtureHost(t, rpc.HostServices{NetworkTerminals: allowed})
		for _, network := range []bool{false, true} {
			conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", r.SocketPath())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			decoder := json.NewDecoder(bufio.NewReader(conn))
			invoke := func(method string, params any) protocol.Response {
				raw, _ := json.Marshal(params)
				if err := json.NewEncoder(conn).Encode(protocol.Request{JSONRPC: "2.0", ID: "id", Method: method, Params: raw}); err != nil {
					t.Fatal(err)
				}
				var result protocol.Response
				if err := decoder.Decode(&result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			initial := invoke("initialize", protocol.InitializeParams{Major: 4, NetworkClient: network, ExpectedProcessEpoch: new(protocol.ID(r.ProcessEpoch()))})
			if initial.Error != nil {
				t.Fatal(initial.Error)
			}
			var result protocol.InitializeResult
			if err := json.Unmarshal(initial.Result, &result); err != nil {
				t.Fatal(err)
			}
			if result.NetworkClient != network || result.ProcessEpoch != protocol.ID(r.ProcessEpoch()) {
				t.Fatalf("initialize=%+v", result)
			}
			for _, method := range []string{"terminal.open", "terminals.open", "shell.input", "shell.interaction", "host.stop"} {
				response := invoke(method, map[string]any{})
				if response.Error == nil {
					t.Fatal("unimplemented method unexpectedly available")
				}
				restricted := network && (method == "host.stop" || !allowed && method != "shell.interaction")
				if (response.Error.Kind == "NETWORK_RESTRICTED") != restricted {
					t.Fatalf("network=%v method=%s error=%+v", network, method, response.Error)
				}
			}
			response := invoke("initialize", protocol.InitializeParams{Major: 4, NetworkClient: !network})
			if response.Error == nil {
				t.Fatal("reinitialized socket")
			}
		}
	}
}
