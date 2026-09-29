package secretref

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCommandOutputIsBoundedAndCancellationJoins(t *testing.T) {
	start := time.Now()
	if _, err := ResolveSecret("!yes confidential-output"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("output bounds: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("overflow did not stop helper")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ResolveSecretContext(ctx, "!sleep 30"); err == nil {
		t.Fatal("cancelled helper started")
	}
}
