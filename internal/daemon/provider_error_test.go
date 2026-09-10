package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestProviderValidationErrorGivesSafeRecovery(t *testing.T) {
	for _, test := range []struct{ status, want string }{
		{"401 Unauthorized", "API key"},
		{"402 Payment Required", "billing"},
		{"403 Forbidden", "permissions"},
		{"429 Too Many Requests", "wait"},
		{"500 Server Error", "connection"},
	} {
		t.Run(test.status, func(t *testing.T) {
			err := providerValidationError(&llm.HTTPError{Status: test.status, Body: "private-key-and-account"})
			if !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe or unhelpful error: %v", err)
			}
		})
	}
	if err := providerValidationError(context.DeadlineExceeded); !strings.Contains(err.Error(), "timed out") {
		t.Fatal(err)
	}
}
