package tui

import (
	"encoding/json"
	"strings"
)

// decodeResult reads legacy Starlark history and the engine-qualified result
// contract without converting exact JSON numbers through float64.
func (cell *replCell) decodeResult(raw string) {
	cell.resultUnavailable = true
	if rest, failed := strings.CutPrefix(raw, "Error:"); failed {
		cell.errText = strings.TrimSpace(rest)
		boundary := strings.LastIndex(raw, "\n{")
		if boundary < 0 {
			return
		}
		cell.errText = strings.TrimSpace(raw[len("Error:"):boundary])
		raw = raw[boundary+1:]
	}
	var result struct {
		FormatVersion *int            `json:"format_version"`
		Engine        string          `json:"execution_engine"`
		Language      string          `json:"language"`
		HasValue      *bool           `json:"has_value"`
		Value         json.RawMessage `json:"value"`
		ValuePreview  *string         `json:"value_preview"`
		Output        string          `json:"output"`
		Steps         *uint64         `json:"steps"`
		Metrics       struct {
			StarlarkSteps *uint64 `json:"starlark_steps"`
			QuickJSJobs   *uint64 `json:"quickjs_jobs"`
		} `json:"metrics"`
	}
	if json.Unmarshal([]byte(raw), &result) != nil {
		return
	}
	hasValue := len(result.Value) > 0 && string(result.Value) != "null" || result.ValuePreview != nil
	if result.FormatVersion == nil {
		if len(result.Value) == 0 || result.Steps == nil {
			return
		}
		cell.engine, cell.steps = "starlark", *result.Steps
	} else {
		validEngine := result.Engine == "starlark" && result.Language == "starlark" ||
			result.Engine == "quickjs" && result.Language == "javascript"
		if *result.FormatVersion != 2 || result.HasValue == nil || !validEngine {
			return
		}
		hasValue = *result.HasValue
		if hasValue && len(result.Value) == 0 {
			return
		}
		cell.engine = result.Engine
		if cell.engine == "starlark" && result.Metrics.StarlarkSteps != nil {
			cell.steps = *result.Metrics.StarlarkSteps
		}
		if cell.engine == "quickjs" {
			cell.steps, cell.jobs = 0, result.Metrics.QuickJSJobs
		}
	}
	cell.output, cell.value = result.Output, ""
	cell.resultUnavailable = false
	if hasValue {
		cell.value = string(result.Value)
		if result.ValuePreview != nil {
			cell.value = *result.ValuePreview
		}
	}
}
