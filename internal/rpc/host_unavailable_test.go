package rpc

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/hostview"
	"github.com/context-labs/whip/internal/protocol"
)

func TestUnavailableHostServicesProduceValidWireErrors(t *testing.T) {
	for _, cause := range []error{hostview.ErrUnavailable, computer.ErrBundledUnavailable} {
		response := protocol.Response{JSONRPC: "2.0", ID: "unavailable", Error: wireError(fmt.Errorf("private host detail: %w", cause))}
		if response.Error.Kind != "HOST_UNAVAILABLE" || response.Error.Message != cause.Error() {
			t.Fatalf("unavailable host error lost its safe meaning: %+v", response.Error)
		}
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.Validate("Response", raw); err != nil {
			t.Fatal(err)
		}
	}
}
