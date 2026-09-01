package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubLSP records calls and returns a canned diagnostics block.
type stubLSP struct {
	calls []string
	block string
}

func (s *stubLSP) WaitDiagnostics(ctx context.Context, path string) string {
	s.calls = append(s.calls, path)
	return s.block
}

func TestWriteEditAppendLSPDiagnostics(t *testing.T) {
	stub := &stubLSP{block: "\n\n<diagnostics file=\"x.go\">\nERROR [1:1] boom\n</diagnostics>"}
	services := NewServices()
	services.SetDiagnostics(stub)

	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")

	args, _ := json.Marshal(map[string]any{"path": p, "content": "package main\n"})
	out := Execute(context.Background(), []Tool{writeTool(services)}, "write", args)
	if !strings.Contains(out, "<diagnostics") || !strings.Contains(out, "ERROR [1:1] boom") {
		t.Fatalf("write output missing diagnostics: %q", out)
	}
	if len(stub.calls) != 1 || stub.calls[0] != p {
		t.Fatalf("hook calls: %v", stub.calls)
	}

	args, _ = json.Marshal(map[string]any{"path": p, "old_string": "main", "new_string": "main2"})
	out = Execute(context.Background(), []Tool{editTool(services)}, "edit", args)
	if !strings.Contains(out, "<diagnostics") {
		t.Fatalf("edit output missing diagnostics: %q", out)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("hook calls: %v", stub.calls)
	}
}

func TestLSPNilUnchangedOutput(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	args, _ := json.Marshal(map[string]any{"path": p, "content": "hi"})
	out := Execute(context.Background(), directTools(), "write", args)
	if strings.Contains(out, "<diagnostics") {
		t.Fatalf("nil hook must not alter output: %q", out)
	}
}

func TestLSPFailureNeverFailsTool(t *testing.T) {
	services := NewServices()
	services.SetDiagnostics(&stubLSP{block: ""}) // server slow/absent: empty block
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	args, _ := json.Marshal(map[string]any{"path": p, "content": "hi"})
	out := Execute(context.Background(), []Tool{writeTool(services)}, "write", args)
	if !strings.HasPrefix(out, "Wrote") {
		t.Fatalf("tool result should be the success message, got %q", out)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("write still happened")
	}
}
