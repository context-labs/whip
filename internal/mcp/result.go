package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/capability"
)

// ErrToolFailure means the server returned a completed isError result. The
// caller retains its returned evidence; a transport failure is a different case.
var ErrToolFailure = errors.New("MCP tool returned an error")

func checkedResult(result *sdkmcp.CallToolResult) (capability.MCPResult, error) {
	if len(result.Content) > 128 {
		return capability.MCPResult{}, errors.New("MCP result has too many content parts")
	}
	size := 0
	for _, part := range result.Content {
		switch value := part.(type) {
		case *sdkmcp.TextContent:
			size += len(value.Text)
		case *sdkmcp.ImageContent:
			size += len(value.Data) + len(value.MIMEType)
		case *sdkmcp.AudioContent:
			size += len(value.Data) + len(value.MIMEType)
		case *sdkmcp.EmbeddedResource:
			if value.Resource != nil {
				size += len(value.Resource.Text) + len(value.Resource.Blob) + len(value.Resource.URI) + len(value.Resource.MIMEType)
			}
		case *sdkmcp.ResourceLink:
			size += len(value.URI) + len(value.Name)
		}
		if size > maxWireBytes {
			return capability.MCPResult{}, errors.New("MCP result content exceeds byte limit")
		}
	}
	if result.StructuredContent != nil {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil || len(raw) > maxWireBytes-size {
			return capability.MCPResult{}, errors.New("MCP structured result exceeds byte limit")
		}
	}
	output := flattenResult(result)
	total := len(output.Text)
	for _, attachment := range output.Attachments {
		total += len(attachment.Data)
	}
	if total > maxWireBytes {
		return capability.MCPResult{}, errors.New("MCP rendered result exceeds byte limit")
	}
	if result.IsError {
		return output, fmt.Errorf("%w: %s", ErrToolFailure, output.Text)
	}
	return output, nil
}

// Small shallow objects retain readable indentation. Compact rendering of
// larger/deeper values prevents indentation from amplifying a bounded response.
func structuredJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 64<<10 {
		return raw, err
	}
	depth, inString, escaped := 0, false, false
	for _, char := range raw {
		if inString {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > 16 {
				return raw, nil
			}
		case '}', ']':
			depth--
		}
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return nil, err
	}
	return pretty.Bytes(), nil
}
