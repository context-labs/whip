package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestGatewayPreservesConnectionAndProviderOwnership(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	connect := func() *Client {
		t.Helper()
		client, err := DialWebSocketClient(ctx, f.endpoint, InitializeParams{
			ProtocolMajor: ProtocolMajor, ClientID: "same-browser-id", ClientKind: "desktop",
			Capabilities: []string{"desktop-browser-v1", "events"},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		if !slices.Contains(client.InitializeResult().NegotiatedCapabilities, protocol.NetworkClientCapability) {
			t.Fatal("gateway did not force network restrictions")
		}
		return client
	}
	first, second := connect(), connect()
	if first.InitializeResult().ConnectionID == second.InitializeResult().ConnectionID {
		t.Fatal("gateway multiplexed browser ownership onto one connection")
	}
	var failure *RPCError
	if err := first.Call(ctx, "terminal.close", protocol.TerminalIDParams{ID: "missing"}, nil); !errors.As(err, &failure) || failure.Code != -32012 {
		t.Fatalf("spoofed desktop identity bypassed terminal policy: %v", err)
	}
	var provider protocol.BrowserProviderBindResult
	if err := first.Call(ctx, "browser.provider.bind", browserOffer(f.rootID), &provider); err != nil {
		t.Fatalf("provider capability advertisement lost: %v", err)
	}
	if err := second.Call(ctx, "browser.provider.unbind", protocol.BrowserProviderUnbindParams{RootID: f.rootID, ProviderEpoch: provider.ProviderEpoch}, nil); err == nil {
		t.Fatal("second browser unbound the first browser's provider")
	}
	data := []byte("connection-owned upload")
	digest := sha256.Sum256(data)
	begin := UploadBeginParams{
		UploadID: "same-upload-id", RootID: f.rootID, Size: int64(len(data)),
		ExpectedDigest: hex.EncodeToString(digest[:]), MediaType: "text/plain",
	}
	if err := first.Call(ctx, "upload.begin", begin, nil); err != nil {
		t.Fatal(err)
	}
	if err := second.Call(ctx, "upload.chunk", UploadChunkParams{UploadID: begin.UploadID, Data: data}, nil); err == nil {
		t.Fatal("second browser modified the first browser's upload")
	}
	if err := second.Call(ctx, "upload.begin", begin, nil); err != nil {
		t.Fatal(err)
	}
	waitGatewayUploads(t, f.server.uploads, 2)
	_ = first.Close()
	waitGatewayUploads(t, f.server.uploads, 1)
	if _, err := second.Upload(ctx, begin, data); err != nil {
		t.Fatalf("other browser disconnect destroyed this upload: %v", err)
	}
	waitGatewayUploads(t, f.server.uploads, 0)
	// Disconnect releases the first connection's lease without requiring a
	// provider-side cleanup message or trusting the reused browser client ID.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		f.server.daemon.browserProviders.mu.Lock()
		lease := f.server.daemon.browserProviders.roots[f.rootID]
		f.server.daemon.browserProviders.mu.Unlock()
		if lease == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("disconnected browser provider lease remains")
		case <-tick.C:
		}
	}
	if err := second.Call(ctx, "browser.provider.bind", browserOffer(f.rootID), &provider); err != nil {
		t.Fatalf("provider ownership was not released on disconnect: %v", err)
	}
}

func TestGatewayShutdownLeavesAcceptedDaemonWorkRunning(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	runner := &fakeRunner{turn: func(ctx context.Context, input string, _ bool) (string, error) {
		close(started)
		select {
		case <-release:
			return input, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	f := newV2Fixture(t, runner)
	client := f.dial("websocket", "durable-gateway-client")
	params := CommandParams{
		CommandID: "gateway-lifetime", Scope: "root", RootID: f.rootID,
		Operation: "submit", Payload: json.RawMessage(`{"text":"survives gateway exit"}`),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := client.Submit(ctx, params); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := f.gateway.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.Done():
	case <-ctx.Done():
		t.Fatal("gateway shutdown left browser connected")
	}
	close(release)
	local := f.dial("unix", "durable-gateway-client")
	result, err := local.SubmitAndWait(ctx, params)
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("gateway shutdown cancelled accepted work: %+v %v", result, err)
	}
}
