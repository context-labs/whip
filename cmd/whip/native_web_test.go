package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/hostcmd"
	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeWebHost(t *testing.T) localruntime.Paths {
	t.Helper()
	t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:0")
	t.Setenv("WHIPCODE_ALLOWED_HOSTS", "")
	t.Setenv("WHIPCODE_ALLOWED_ORIGINS", "")
	nativeDaemonHome(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- hostcmd.Run(ctx, []string{"-directory", paths.Directory, "-scripted"}, io.Discard, io.Discard)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("native host did not join")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if status := localruntime.Inspect(t.Context(), paths); status.State == "running" {
			return paths
		}
		if time.Now().After(deadline) {
			t.Fatal("native host did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNativeGatewayCancellationLeavesHostAndDiscoveryAlone(t *testing.T) {
	t.Setenv("WHIPCODE_NETWORK", "0")
	t.Setenv("WHIPCODE_LISTEN", "127.0.0.1:0")
	paths := nativeWebHost(t)
	before := localruntime.Inspect(t.Context(), paths)
	oldAssets := gatewayAssetsAvailable
	gatewayAssetsAvailable = func() bool { return true } // Fixture discovery only, not renderer acceptance.
	t.Cleanup(func() { gatewayAssetsAvailable = oldAssets })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() {
		done <- runNativeGateway(ctx, paths, func(endpoint string) error { ready <- endpoint; return nil })
	}()
	var endpoint string
	select {
	case endpoint = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("gateway not ready")
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+"/api/v4/web", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var discovery protocol.GatewayDiscovery
	err = json.NewDecoder(response.Body).Decode(&discovery)
	response.Body.Close()
	if err != nil || discovery.RuntimeID != before.Process.RuntimeID || discovery.ProcessEpoch != before.Process.ProcessEpoch {
		t.Fatal(discovery, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not join")
	}
	after := localruntime.Inspect(t.Context(), paths)
	if after.Process == nil || after.Process.ProcessEpoch != before.Process.ProcessEpoch || after.Process.WebEndpoint != "" {
		t.Fatal("gateway changed host lifecycle", after)
	}
	if response, err := (&http.Client{Timeout: time.Second}).Do(request); err == nil {
		response.Body.Close()
		t.Fatal("gateway still listening")
	}
}

func TestNativeGatewayMissingAssetsAndOccupiedPortLeaveHostAlive(t *testing.T) {
	paths := nativeWebHost(t)
	before := localruntime.Inspect(t.Context(), paths)
	oldAssets := gatewayAssetsAvailable
	t.Cleanup(func() { gatewayAssetsAvailable = oldAssets })
	gatewayAssetsAvailable = func() bool { return false }
	ready := func(string) error { t.Error("failed gateway reported ready"); return nil }
	if err := runNativeGateway(t.Context(), paths, ready); err == nil || !strings.Contains(err.Error(), "without web assets") {
		t.Fatal(err)
	}
	gatewayAssetsAvailable = func() bool { return true }
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("WHIPCODE_LISTEN", listener.Addr().String())
	if err := runNativeGateway(t.Context(), paths, ready); err == nil {
		t.Fatal("occupied port accepted")
	}
	after := localruntime.Inspect(t.Context(), paths)
	if after.Process == nil || after.Process.ProcessEpoch != before.Process.ProcessEpoch {
		t.Fatal("gateway altered host", after)
	}
}
