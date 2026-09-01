package acp

import (
	"context"
	"os"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// llmTool builds a bare tool def for test-only tools.
func llmTool(name string) llm.Tool {
	return llm.NewTool(name, name, `{"type":"object","properties":{}}`)
}

// checkGateForTest follows the same context-bound permission seam as built-ins.
func checkGateForTest(ctx context.Context, tool, command string) string {
	return tools.CheckGate(ctx, tool, command)
}

type errStringT string

func (e errStringT) Error() string { return string(e) }

func errString(s string) error { return errStringT(s) }
