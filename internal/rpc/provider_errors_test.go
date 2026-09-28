package rpc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestProviderKeyDurabilityHasDistinctSafeWireOutcome(t *testing.T) {
	for _, test := range []struct {
		err  error
		kind string
	}{{config.ErrKeyStoragePending, "PROVIDER_KEY_PENDING"}, {config.ErrKeyStorage, "PROVIDER_KEY_STORAGE"}, {config.ErrKeyConflict, "CONFLICT"}, {config.ErrRevisionConflict, "CONFLICT"}} {
		value := wireError(fmt.Errorf("private-key-and-command-argument: %w", test.err))
		if value.Kind != test.kind || strings.Contains(value.Message, "private-key") {
			t.Fatal("credential diagnostic leaked or durability distinction lost", value)
		}
	}
}
