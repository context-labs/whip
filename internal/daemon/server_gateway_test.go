package daemon

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func gatewayPolicyClient(t *testing.T, server *Server, directNetwork bool, params InitializeParams) *Client {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	served := make(chan struct{})
	go func() {
		defer close(served)
		server.serveTransport(newUnixMessageTransport(serverSide), directNetwork)
	}()
	t.Cleanup(func() {
		_ = clientSide.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("policy connection did not stop")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, clientSide, params)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestNetworkClientCapabilityRestrictsBeforeRegistration(t *testing.T) {
	for _, test := range []struct {
		name, kind                           string
		capabilities                         []string
		directNetwork, restricted, terminals bool
	}{
		{name: "unchanged local desktop", kind: "desktop"},
		{name: "identity alone is not policy", kind: "gateway"},
		{name: "restricted desktop identity", kind: "desktop", capabilities: []string{protocol.NetworkClientCapability}, restricted: true},
		{name: "restricted human identity", kind: "human", capabilities: []string{protocol.NetworkClientCapability, protocol.NetworkClientCapability}, restricted: true},
		{name: "direct network cannot request local", kind: "desktop", directNetwork: true, restricted: true},
		{name: "daemon policy permits terminals", kind: "desktop", capabilities: []string{protocol.NetworkClientCapability}, restricted: true, terminals: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := terminalTestServer(t, test.terminals)
			params := InitializeParams{ProtocolMajor: ProtocolMajor, ClientKind: test.kind, ClientID: "same-identity", Capabilities: test.capabilities}
			client := gatewayPolicyClient(t, server, test.directNetwork, params)
			initialized := client.InitializeResult()
			if initialized.ConnectionID == "" {
				t.Fatal("connection identity was lost")
			}
			count := 0
			for _, feature := range initialized.NegotiatedCapabilities {
				if feature == protocol.NetworkClientCapability {
					count++
				}
			}
			want := 0
			if slices.Contains(test.capabilities, protocol.NetworkClientCapability) {
				want = 1
			}
			if count != want {
				t.Fatalf("network acknowledgement count=%d, want %d", count, want)
			}
			server.mu.Lock()
			var registered *serverConn
			for connection := range server.clients {
				if connection.id == initialized.ConnectionID {
					registered = connection
				}
			}
			server.mu.Unlock()
			if registered == nil || registered.network != test.restricted {
				t.Fatalf("registered connection restriction: %+v", registered)
			}
			// A second initialize cannot change either identity or classification.
			params.ClientID = "replacement-identity"
			params.Capabilities = nil
			if err := client.Call(t.Context(), "initialize", params, nil); err == nil {
				t.Fatal("re-initialization accepted")
			}
			if registered.client.ClientID != "same-identity" || registered.network != test.restricted {
				t.Fatal("re-initialization changed registered connection policy")
			}
			// A missing terminal distinguishes authorization from resource lookup
			// without opening a shell in the policy test.
			err := client.Call(t.Context(), "terminal.close", protocol.TerminalIDParams{ID: "missing"}, nil)
			var failure *RPCError
			if !errors.As(err, &failure) {
				t.Fatalf("terminal policy returned %v", err)
			}
			if denied := failure.Code == -32012; denied != (test.restricted && !test.terminals) {
				t.Fatalf("terminal policy: %+v", failure)
			}
		})
	}
}

func TestGatewayStatusDiscoveryAndConcurrentUpdates(t *testing.T) {
	server := terminalTestServer(t, false)
	params := InitializeParams{ProtocolMajor: ProtocolMajor, ClientKind: "local", ClientID: "discovery"}
	client := gatewayPolicyClient(t, server, false, params)
	if got := client.InitializeResult().NetworkEndpoint; got != "" {
		t.Fatalf("ordinary daemon advertised endpoint %q", got)
	}
	var status protocol.GatewayStatus
	if err := client.Call(t.Context(), "gateway.status", protocol.Empty{}, &status); err != nil || status.State != "disabled" {
		t.Fatalf("default gateway status: %+v %v", status, err)
	}
	for _, update := range []protocol.GatewayStatus{
		{State: "starting", Endpoint: "http://stale"},
		{State: "ready", Endpoint: "http://127.0.0.1:4444"},
		{State: "failed", Endpoint: "http://stale", Error: "child exited"},
		{State: "disabled", Endpoint: "http://stale"},
	} {
		server.SetGatewayStatus(update)
		want := update
		if want.State != "ready" {
			want.Endpoint = ""
		}
		status = protocol.GatewayStatus{}
		if err := client.Call(t.Context(), "gateway.status", protocol.Empty{}, &status); err != nil || status != want {
			t.Fatalf("gateway status: %+v, want %+v (%v)", status, want, err)
		}
		probe := gatewayPolicyClient(t, server, false, params)
		if got := probe.InitializeResult().NetworkEndpoint; got != want.Endpoint {
			t.Fatalf("initialize endpoint=%q, want %q", got, want.Endpoint)
		}
		_ = probe.Close()
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			server.SetGatewayStatus(protocol.GatewayStatus{State: "ready", Endpoint: "http://127.0.0.1:4444"})
			server.SetGatewayStatus(protocol.GatewayStatus{State: "failed", Error: "stopped"})
		}
	})
	for range 100 {
		status = protocol.GatewayStatus{}
		if err := client.Call(t.Context(), "gateway.status", protocol.Empty{}, &status); err != nil {
			t.Fatal(err)
		}
		if status.State != "ready" && status.Endpoint != "" {
			t.Fatalf("stale endpoint in %+v", status)
		}
	}
	workers.Wait()
}

func TestGatewayUploadBeginAfterDisconnectDoesNotLeak(t *testing.T) {
	server := terminalTestServer(t, false)
	server.uploads.dir = t.TempDir()
	connection := terminalTestConn(t, server, true)
	connection.close()
	// Models an admitted request goroutine scheduled after transport teardown.
	failure := terminalCall(t, server, connection, "upload.begin", UploadBeginParams{
		UploadID: "late-upload", RootID: "root", Size: 4,
		ExpectedDigest: "230d8358dc8e8890b4c58deeb62912ee2f20357ae92a5cc861b98e68fe31acb5",
	}, nil)
	if failure == nil {
		t.Fatal("upload began after its owner disconnected")
	}
	waitGatewayUploads(t, server.uploads, 0)
}
