package rlm

import (
	"context"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/engine/process"
)

func TestRLMToolValidatesArgumentsAndRunsKernel(t *testing.T) {
	tool := Tool(nil)
	if _, err := tool.Run(context.Background(), []byte(`{"code":"1"}`)); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil kernel error = %v", err)
	}
	kernel := retainedKernel(t, process.KernelOptions{Limits: process.DefaultLimits()})
	tool = Tool(kernel)
	for _, input := range [][]byte{[]byte("{"), []byte(`{"code":""}`)} {
		if _, err := tool.Run(context.Background(), input); err == nil {
			t.Fatalf("invalid arguments %q were accepted", input)
		}
	}
	output, err := tool.Run(context.Background(), []byte(`{"code":"21 * 2"}`))
	if err != nil || !strings.Contains(output, `"value":42`) {
		t.Fatalf("tool output = %q, %v", output, err)
	}
	if _, err := tool.Run(context.Background(), []byte(`{"code":"fail"}`)); err == nil {
		t.Fatal("evaluation error was hidden")
	}
}
