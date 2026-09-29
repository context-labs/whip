package mcp

import (
	"errors"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfirmedRemoteFailureKeepsBoundedEvidence(t *testing.T) {
	result := &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "partial evidence"}, &sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte{1, 2, 3}}}, StructuredContent: map[string]any{"status": "failed"}}
	output, err := checkedResult(result)
	if !errors.Is(err, ErrToolFailure) || !strings.Contains(output.Text, "partial evidence") || !strings.Contains(output.Text, `"status": "failed"`) || len(output.Attachments) != 1 {
		t.Fatalf("lost completed failure evidence: %+v %v", output, err)
	}
	if _, err := checkedResult(&sdkmcp.CallToolResult{Content: make([]sdkmcp.Content, 129)}); err == nil {
		t.Fatal("unbounded result parts")
	}
}

func TestStructuredFormattingCannotAmplifyDeepPayload(t *testing.T) {
	var value any = "leaf"
	for range 1000 {
		value = []any{value}
	}
	encoded, err := structuredJSON(value)
	if err != nil || len(encoded) > 2100 {
		t.Fatalf("deep result amplified: bytes=%d error=%v", len(encoded), err)
	}
}
