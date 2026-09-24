package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestBashToolSpillOnTruncation: output beyond maxOutput is tail-truncated for
// the model AND spilled to a file the tool result names, so the model can
// read/grep the rest (roadmap: "spill truncated bash output to a temp file").
func TestBashToolSpillOnTruncation(t *testing.T) {
	// Emit maxOutput+1000 bytes: head marker, 60KB of filler, tail marker.
	cmd := `printf 'HEADMARKER\n'; head -c 60000 /dev/zero | tr '\0' 'x'; printf '\nTAILMARKER\n'`
	out := run(t, "bash", fmt.Sprintf(`{"command":%q}`, cmd))

	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation marker:\n%.300s", out)
	}
	// The spill notice names a file holding the FULL output.
	i := strings.Index(out, "[full output (")
	if i < 0 {
		t.Fatalf("missing spill notice:\n%.500s", out)
	}
	pathStart := strings.Index(out[i:], ": ") + 2 + i
	path := strings.TrimSuffix(strings.TrimSpace(out[pathStart:]), "]")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	defer os.Remove(path)
	if !strings.Contains(string(data), "HEADMARKER") || !strings.Contains(string(data), "TAILMARKER") {
		t.Fatalf("spill file lost content: %d bytes, head=%v tail=%v",
			len(data), strings.Contains(string(data), "HEADMARKER"), strings.Contains(string(data), "TAILMARKER"))
	}
	// The truncated result keeps the tail (the interesting end of the log).
	if !strings.Contains(out, "TAILMARKER") {
		t.Errorf("truncated result lost the tail marker")
	}
}

// TestBashToolNoSpillUnderCap: output within maxOutput spills nothing.
func TestBashToolNoSpillUnderCap(t *testing.T) {
	out := run(t, "bash", `{"command":"echo small"}`)
	if strings.Contains(out, "full output") {
		t.Fatalf("small output must not spill: %q", out)
	}
}

// TestBashToolOnUpdateCtx: WithOnUpdate on the ctx reaches bashrun — partial
// snapshots arrive while a staged command is still running.
func TestBashToolOnUpdateCtx(t *testing.T) {
	var mu sync.Mutex
	var snaps []string
	ctx := WithOnUpdate(context.Background(), func(soFar string) {
		mu.Lock()
		snaps = append(snaps, soFar)
		mu.Unlock()
	})
	out := Execute(ctx, directTools(), "bash",
		json.RawMessage(`{"command":"echo early; sleep 0.3; echo late"}`))
	if !strings.Contains(out, "late") {
		t.Fatalf("final result wrong: %q", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snaps) == 0 {
		t.Fatal("no partial snapshots delivered through the ctx callback")
	}
	if !strings.Contains(strings.Join(snaps, ""), "early") {
		t.Fatalf("snapshots missed the early output: %v", snaps)
	}
}
