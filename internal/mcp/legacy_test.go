package mcp

import (
	"context"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func legacyTools(handlers []Handler) []tools.Tool {
	result := make([]tools.Tool, 0, len(handlers))
	for _, h := range handlers {
		result = append(result, tools.Tool{Def: llm.Tool(h.Def), Run: h.Run})
	}
	return result
}

type legacyProvider struct{ *tools.Services }

func (p legacyProvider) ToolDefinitions(ctx context.Context) ([]Definition, error) {
	defs, err := p.Services.ToolDefinitions(ctx)
	out := make([]Definition, 0, len(defs))
	for _, d := range defs {
		out = append(out, Definition(d))
	}
	return out, err
}
