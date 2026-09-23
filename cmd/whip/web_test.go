package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"
)

func TestWebEndpointValidation(t *testing.T) {
	for _, test := range []struct{ name, value, want, errorText string }{
		{name: "loopback", value: "http://127.0.0.1:43210", want: "http://127.0.0.1:43210"},
		{name: "proxy", value: "https://whip.example/", want: "https://whip.example"},
		{name: "script rejected", value: "javascript:alert(1)", errorText: "HTTP or HTTPS"},
		{name: "credentials rejected", value: "http://user:secret@localhost:8080", errorText: "without credentials"},
		{name: "path rejected", value: "http://localhost:8080/session", errorText: "without credentials"},
		{name: "query rejected", value: "http://localhost:8080?", errorText: "without credentials"},
		{name: "wildcard", value: "http://[::]:8080", errorText: "wildcard"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateWebEndpoint(test.value)
			if test.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errorText) {
					t.Fatalf("error %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("endpoint %q %v", got, err)
			}
		})
	}
}

func TestWebCLIExplicitURLNeverStartsRuntime(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing-home")
	t.Setenv("WHIP_HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/web" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"available":true,"protocol_major":%d,"websocket_path":"/api/v3/ws","content_path":"/api/v3/content/"}`, daemon.ProtocolMajor)
	}))
	defer server.Close()
	oldOpen, oldLaunch := openWebBrowser, launchManagedDaemon
	t.Cleanup(func() { openWebBrowser, launchManagedDaemon = oldOpen, oldLaunch })
	opens := 0
	openWebBrowser = func(value string) bool {
		opens++
		if value != server.URL+"/" {
			t.Errorf("opened %s", value)
		}
		return true
	}
	launchManagedDaemon = func(daemon.RuntimePaths) error {
		t.Fatal("web command must never launch or replace runtime")
		return nil
	}
	output := captureDaemonOutput(t, func() error { return webCLI([]string{"--url", server.URL, "--no-open"}) })
	if strings.TrimSpace(output) != server.URL+"/" || opens != 0 {
		t.Fatalf("print URL %q opens=%d", output, opens)
	}
	captureDaemonOutput(t, func() error { return webCLI([]string{"--url", server.URL}) })
	if opens != 1 {
		t.Fatal("browser was not opened")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("explicit URL touched home: %v", err)
	}
}

func TestWebDiscoveryRejectsMissingAndIncompatibleAssets(t *testing.T) {
	for _, test := range []struct {
		name            string
		code            int
		body, errorText string
	}{
		{name: "not packaged", code: 200, body: fmt.Sprintf(`{"available":false,"protocol_major":%d,"websocket_path":"/api/v3/ws","content_path":"/api/v3/content/"}`, daemon.ProtocolMajor), errorText: "without web assets"},
		{name: "old endpoint", code: 404, errorText: "check the URL"},
		{name: "host rejected", code: 403, errorText: "WHIP_ALLOWED_HOSTS"},
		{name: "major mismatch", code: 200, body: `{"available":true,"protocol_major":1}`, errorText: "incompatible"},
		{name: "malformed", code: 200, body: `<html>`, errorText: "invalid gateway web discovery"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.code); fmt.Fprint(w, test.body) }))
			defer server.Close()
			if err := checkWebAssets(t.Context(), server.URL); err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestWebStoppedDaemonIsNotStarted(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing")
	t.Setenv("WHIP_HOME", home)
	err := runWeb(t.Context(), []string{"--no-open"})
	if err == nil || !strings.Contains(err.Error(), "whip daemon start") {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("web created home: %v", err)
	}
}

func TestGatewayDialRequiresCapabilityAcknowledgement(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(fmt.Sprintf("ack=%t", ack), func(t *testing.T) {
			paths, err := daemon.Paths(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("unix", paths.Socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := os.Chmod(paths.Socket, 0600); err != nil {
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
					done <- fmt.Errorf("gateway did not request restrictions")
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
	t.Setenv("WHIP_HOME", home)
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
	t.Setenv("WHIP_NETWORK", "0")
	t.Setenv("WHIP_LISTEN", "127.0.0.1:0")
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
	if response, err := httpClient.Get(record.Endpoint + "/api/v3/web"); err == nil {
		response.Body.Close()
		t.Fatal("gateway kept listening")
	}
}

func TestGatewayMissingAssetsAndExplicitPortFailureLeaveDaemonAlive(t *testing.T) {
	t.Setenv("WHIP_NETWORK", "0")
	paths := startWebTestDaemon(t)
	oldAssets := gatewayAssetsAvailable
	t.Cleanup(func() { gatewayAssetsAvailable = oldAssets })
	gatewayAssetsAvailable = func() bool { return false }
	report := func(gatewayReady) error { t.Error("failed gateway reported ready"); return nil }
	if err := runGateway(t.Context(), paths, nil, report); err == nil || !strings.Contains(err.Error(), "without web assets") {
		t.Fatalf("assets error: %v", err)
	}
	gatewayAssetsAvailable = func() bool { return true }
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("WHIP_LISTEN", listener.Addr().String())
	if err := runGateway(t.Context(), paths, nil, report); err == nil {
		t.Fatal("occupied explicit port accepted")
	}
	status, client := probeDaemon(paths, time.Second)
	if client == nil {
		t.Fatalf("daemon stopped: %+v", status)
	}
	client.Close()
}
