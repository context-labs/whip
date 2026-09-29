package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/legacy/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"
)

func TestGatewayDialRequiresCapabilityAcknowledgement(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(fmt.Sprintf("ack=%t", ack), func(t *testing.T) {
			paths, err := daemon.Paths(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", paths.Socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := os.Chmod(paths.Socket, 0o600); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				frame, err := protocoltransport.ReadFrame(bufio.NewReader(conn))
				if err != nil {
					done <- err
					return
				}
				message, err := protocoltransport.DecodeFrame(frame)
				if err != nil {
					done <- err
					return
				}
				var params protocol.InitializeParams
				if err = json.Unmarshal(message.Params, &params); err != nil {
					done <- err
					return
				}
				if !slices.Contains(params.Capabilities, protocol.NetworkClientCapability) {
					done <- errors.New("gateway did not request restrictions")
					return
				}
				result := protocol.InitializeResult{ProtocolMajor: protocol.Major, RuntimeID: "old-runtime", Generation: 1}
				if ack {
					result.NegotiatedCapabilities = []string{protocol.NetworkClientCapability}
				}
				response, err := protocoltransport.MarshalFrame(protocoltransport.Message{ID: message.ID, Result: result})
				if err == nil {
					_, err = conn.Write(response)
				}
				if err == nil {
					_, err = io.Copy(io.Discard, conn)
				}
				done <- err
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			client, err := dialGatewayClient(ctx, paths)
			if ack {
				if err != nil {
					t.Fatal(err)
				}
				client.Close()
			} else if err == nil || !strings.Contains(err.Error(), "does not support") {
				t.Fatalf("unsupported daemon error: %v", err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("gateway retained old connection")
			}
		})
	}
}

func startWebTestDaemon(t *testing.T) daemon.RuntimePaths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon failed to stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, client := probeDaemon(paths, 100*time.Millisecond)
		if client != nil {
			client.Close()
			if status.State != "running" {
				t.Fatalf("status %+v", status)
			}
			return paths
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon did not become ready")
	return paths
}

func TestForegroundGatewayCancellationLeavesDaemonAndDiscoveryAlone(t *testing.T) {
	t.Setenv("WHIPCODE_NETWORK", "0")
	t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:0")
	paths := startWebTestDaemon(t)
	oldAssets := gatewayAssetsAvailable
	gatewayAssetsAvailable = func() bool { return true }
	t.Cleanup(func() { gatewayAssetsAvailable = oldAssets })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan gatewayReady, 1)
	done := make(chan error, 1)
	go func() {
		done <- runGateway(ctx, paths, nil, func(record gatewayReady) error { ready <- record; return nil })
	}()
	var record gatewayReady
	select {
	case record = <-ready:
	case err := <-done:
		t.Fatalf("gateway startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("gateway not ready")
	}
	if record.RuntimeID == "" || record.Generation == 0 {
		t.Fatalf("readiness: %+v", record)
	}
	status, client := probeDaemon(paths, time.Second)
	if client == nil {
		t.Fatalf("daemon unavailable: %+v", status)
	}
	client.Close()
	if status.NetworkEndpoint != "" || status.Gateway.State != "disabled" {
		t.Fatalf("foreground overwrote managed discovery: %+v", status)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway failed to stop")
	}
	status, client = probeDaemon(paths, time.Second)
	if client == nil {
		t.Fatalf("gateway stopped daemon: %+v", status)
	}
	client.Close()
	httpClient := &http.Client{Timeout: time.Second}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, record.Endpoint+"/api/v3/web", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := httpClient.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("gateway kept listening")
	}
}

func TestGatewayMissingAssetsAndExplicitPortFailureLeaveDaemonAlive(t *testing.T) {
	t.Setenv("WHIPCODE_NETWORK", "0")
	paths := startWebTestDaemon(t)
	oldAssets := gatewayAssetsAvailable
	t.Cleanup(func() { gatewayAssetsAvailable = oldAssets })
	gatewayAssetsAvailable = func() bool { return false }
	report := func(gatewayReady) error { t.Error("failed gateway reported ready"); return nil }
	if err := runGateway(t.Context(), paths, nil, report); err == nil || !strings.Contains(err.Error(), "without web assets") {
		t.Fatalf("assets error: %v", err)
	}
	gatewayAssetsAvailable = func() bool { return true }
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("WHIPCODE_LISTEN", listener.Addr().String())
	if err := runGateway(t.Context(), paths, nil, report); err == nil {
		t.Fatal("occupied explicit port accepted")
	}
	status, client := probeDaemon(paths, time.Second)
	if client == nil {
		t.Fatalf("daemon stopped: %+v", status)
	}
	client.Close()
}
