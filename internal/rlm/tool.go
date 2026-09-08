package rlm

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// Tool exposes the entire RLM runtime as one model-facing operation.
func Tool(kernel *Kernel) tools.Tool {
	return tools.Tool{
		Def: llm.NewTool("rlm_exec", `Execute one bounded Starlark cell. Supported data and top-level helpers survive worker eviction and restart; unsupported bindings and checkpoint failures are reported in scratch notices. Checkpoint failure does not undo completed effects. Use the context, files, shell, browser, computer, models, agents, messages, mcp, state, artifacts, schedules, and permissions modules for host access with keyword arguments. Local json.encode(value) and json.decode(text) accept positional arguments.`, `{
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
			data, marshalErr := marshalToolResult(result)
			if marshalErr != nil {
				return "", marshalErr
			}
			// Keep the result available even when the cell failed. The tool
			// dispatcher adds its ordinary error prefix without discarding it.
			return string(data), err
		},
	}
}

// Keep one valid JSON payload for model context, durable history and SDK replay.
// Bound individual fields by encoded bytes before marshaling: truncating the
// serialized document would lose the result and its checkpoint notices.
func marshalToolResult(result Result) ([]byte, error) {
	payload := boundedScratchNotices(result)
	payload["steps"] = result.Steps
	payload["value"] = result.Value
	if result.Output != "" {
		output, truncated := boundedResultText(result.Output, 16*1024)
		payload["output"] = output
		if truncated {
			payload["truncated"] = true
		}
	}
	value, err := json.Marshal(result.Value)
	if err != nil {
		return nil, err
	}
	if len(value) > 16*1024 {
		payload["value"] = nil
		payload["value_preview"], _ = boundedResultText(string(value), 16*1024)
		payload["truncated"] = true
	}
	return json.Marshal(payload)
}

func boundedResultText(value string, encodedLimit int) (string, bool) {
	data, _ := json.Marshal(value)
	if len(data) <= encodedLimit {
		return value, false
	}
	low, high := 3, len(value)
	for low < high {
		mid := low + (high-low+1)/2
		data, _ := json.Marshal(noticeText(value, mid))
		if len(data) <= encodedLimit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return noticeText(value, low), true
}

// Bound presentation independently of the durable manifest. Counts preserve
// visibility of omissions without putting an unbounded name list in context.
func boundedScratchNotices(result Result) map[string]any {
	notices := make(map[string]any)
	if report := result.Scratch; report != nil {
		skipped, omitted := boundedScratchSkips(report.Skipped)
		notices["scratch"] = map[string]any{"warning": noticeText(report.Warning, 1024), "skipped": skipped, "skipped_omitted": omitted}
	}
	if report := result.Restored; report != nil {
		skipped, omitted := boundedScratchSkips(report.Failed)
		names := make([]string, 0)
		bytes := 0
		for _, name := range report.Restored {
			if len(names) == 30 {
				break
			}
			name = noticeText(name, 128)
			data, _ := json.Marshal(name)
			if bytes+len(data) > 2048 {
				break
			}
			names = append(names, name)
			bytes += len(data)
		}
		notices["restored"] = map[string]any{"restored": names, "restored_omitted": len(report.Restored) - len(names), "failed": skipped, "failed_omitted": omitted}
	}
	return notices
}

func boundedScratchSkips(items []SkippedName) ([]SkippedName, int) {
	result := make([]SkippedName, 0)
	bytes := 0
	for _, item := range items {
		if len(result) == 30 {
			break
		}
		item.Name, item.Reason = noticeText(item.Name, 128), noticeText(item.Reason, 256)
		data, _ := json.Marshal(item)
		if bytes+len(data) > 3072 {
			break
		}
		result = append(result, item)
		bytes += len(data)
	}
	return result, len(items) - len(result)
}

func noticeText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit - 3
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "..."
}
