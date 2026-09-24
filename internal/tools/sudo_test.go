package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestBashSudoFastFail is an end-to-end regression for the original bug: running
// sudo (which opens /dev/tty for a password) used to hang whip until the 120s
// bash timeout because the child shared whip's controlling terminal. With the
// Setsid isolation the child has no controlling tty, the /dev/tty open fails
// immediately, and sudo returns at once. Skip when sudo isn't installed.
func TestBashSudoFastFail(t *testing.T) {
	if out := run(t, "bash", `{"command":"command -v sudo >/dev/null || echo MISSING","timeout":5}`); strings.Contains(out, "MISSING") {
		t.Skip("sudo not installed; skipping end-to-end sudo hang test")
	}

	start := time.Now()
	out := Execute(context.Background(), directTools(), "bash", json.RawMessage(`{"command":"sudo true 2>&1; echo RC=$?","timeout":5}`))
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Fatalf("sudo invocation hung %s — the /dev/tty hang regressed: %q", elapsed, out)
	}
	// We don't assert a specific RC (sudo may or may not be passwordless here),
	// only that it returned fast and the tool did not time out.
	if strings.Contains(out, "command timed out") {
		t.Fatalf("sudo call timed out — fast-fail regressed: %q", out)
	}
}
