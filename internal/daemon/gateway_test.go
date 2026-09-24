package daemon

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/webgateway"
)

func startTestGateway(t *testing.T, paths RuntimePaths, options webgateway.Options) *webgateway.Server {
	t.Helper()
	if options.Address == "" {
		options.Address = "127.0.0.1:0"
	}
	options.SocketPath = paths.Socket
	options.Open = func(ctx context.Context) (webgateway.Client, error) {
		return DialClient(ctx, paths, InitializeParams{
			ProtocolMajor: ProtocolMajor, ClientKind: "gateway", ClientID: "gateway-" + rand.Text(),
			Capabilities: []string{protocol.NetworkClientCapability},
		})
	}
	gateway, err := webgateway.Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := gateway.Close(); err != nil {
			t.Error(err)
		}
	})
	return gateway
}
