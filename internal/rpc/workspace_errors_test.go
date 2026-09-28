package rpc

import (
	"fmt"
	"testing"

	"github.com/context-labs/whip/internal/runtime"
)

func TestWorkspaceWireErrorsKeepPrivateDetailsOffWire(t *testing.T) {
	for _, test := range []struct {
		err  error
		kind string
	}{{runtime.ErrWorkspaceChanged, "CONFLICT"}, {runtime.ErrWorkspaceLimit, "LIMIT"}} {
		value := wireError(fmt.Errorf("%w: /private/repository", test.err))
		if value.Kind != test.kind || value.Message != test.err.Error() {
			t.Fatal(value)
		}
	}
}
