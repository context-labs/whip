package bashrun

import (
	"strings"
	"testing"
	"time"
)

func TestCommandOutputRetainsBoundedTailAndTotal(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		name := "pipe"
		if interactive {
			name = "pty"
		}
		t.Run(name, func(t *testing.T) {
			result := Run(t.Context(), Options{Command: "head -c 3000000 /dev/zero | tr '\\0' x; printf tail", Cwd: t.TempDir(), Timeout: 10 * time.Second, Interactive: interactive})
			if result.Exit != "" || result.Killed || !result.Truncated || result.TotalBytes != 3000004 || len(result.Output) != outputBytes || !strings.HasSuffix(result.Output, "tail") {
				t.Fatalf("bounded output: bytes=%d kept=%d truncated=%v exit=%q killed=%v", result.TotalBytes, len(result.Output), result.Truncated, result.Exit, result.Killed)
			}
		})
	}
}

func TestOutputBoundAcrossLargeAndSmallWrites(t *testing.T) {
	var output outputBuffer
	_, _ = output.Write([]byte(strings.Repeat("a", 2*outputBytes)))
	_, _ = output.Write([]byte("tail"))
	if output.total != 2*outputBytes+4 || len(output.data) != outputBytes || !strings.HasSuffix(output.String(), "tail") {
		t.Fatal("output retained more than its bound or lost its tail")
	}
}
