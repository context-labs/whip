package rlm

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// Tool exposes the entire RLM runtime as one model-facing operation.
func Tool(kernel *Kernel) tools.Tool {
	return tools.Tool{
		Def: llm.NewTool("rlm_exec", `Execute one bounded Starlark cell. Starlark globals persist across cells and survive worker restarts (closures and self-referential values excepted). Use the context, files, shell, browser, computer, models, agents, messages, mcp, state, artifacts, schedules, and permissions modules for all host access. Module calls accept keyword arguments only.`, `{
  "type": "object",
  "properties": {"code": {"type": "string", "description": "Starlark source code"}},
  "required": ["code"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, arguments json.RawMessage) (string, error) {
			if kernel == nil {
				return "", errors.New("RLM kernel is unavailable")
			}
			var input struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(arguments, &input); err != nil {
				return "", err
			}
			if input.Code == "" {
				return "", errors.New("code is required")
			}
			result, err := kernel.Exec(ctx, input.Code)
			data, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return "", marshalErr
			}
			if err != nil {
				return string(data), err
			}
			return tools.Truncate(string(data)), nil
		},
	}
}
