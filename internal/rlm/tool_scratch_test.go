package rlm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/tools"
)

func TestScratchWarningSurvivesToolOutputTruncation(t *testing.T) {
	store := &failingScratch{saveErr: errors.New("offline")}
	limits := process.DefaultLimits()
	limits.OutputBytes = 200000
	kernel := retainedKernel(t, process.KernelOptions{Scratch: store, Manager: process.NewManager(1), Limits: limits})
	output, err := Tool(kernel).Run(t.Context(), []byte(`{"code":"saved = 1\nprint('x' * 100000)"}`))
	if err != nil || !json.Valid([]byte(output)) || !strings.Contains(output, "Scratch checkpoint failed") || !strings.Contains(output, "Do not replay") {
		t.Fatalf("warning missing, err=%v", err)
	}
}

func TestScratchFailedCellKeepsStructuredToolResult(t *testing.T) {
	store := &failingScratch{saveErr: errors.New("offline")}
	kernel := retainedKernel(t, process.KernelOptions{Scratch: store, Manager: process.NewManager(1)})
	output := tools.Execute(t.Context(), []tools.Tool{Tool(kernel)}, "rlm_exec", []byte(`{"code":"saved = 1\nprint('before failure')\nfail('cell failed')"}`))
	prefix, payload, found := strings.Cut(output, "\n{")
	if !found || !strings.HasPrefix(prefix, "Error:") || !strings.Contains(prefix, "cell failed") {
		t.Fatalf("missing failed tool boundary: %s", output)
	}
	var result struct {
		Output  string                `json:"output"`
		Steps   uint64                `json:"steps"`
		Scratch process.ScratchReport `json:"scratch"`
	}
	if err := json.Unmarshal([]byte("{"+payload), &result); err != nil {
		t.Fatal(err)
	}
	if result.Output != "before failure\n" || result.Steps == 0 || !strings.Contains(result.Scratch.Warning, "Do not replay") {
		t.Fatalf("partial result lost: %+v", result)
	}
}

func TestScratchToolResultBoundsEncodedFieldsWithoutBreakingJSON(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 100000), strings.Repeat("\x00", 100000), strings.Repeat("👋", 50000)} {
		value := process.Result{
			Value: []any{text}, Output: text, Steps: 42,
			Scratch:  &process.ScratchReport{Warning: strings.Repeat("\x00", 1024)},
			Restored: &process.RestoreReport{Restored: []string{"saved"}},
		}
		data, err := marshalToolResult(value)
		if err != nil || !json.Valid(data) || len(data) > 50000 {
			t.Fatalf("invalid bounded result: bytes=%d err=%v", len(data), err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["truncated"] != true || decoded["value_preview"] == nil || decoded["scratch"] == nil || decoded["restored"] == nil || decoded["steps"] != float64(42) {
			t.Fatalf("bounded result lost metadata: %v", decoded)
		}
	}
}

func TestScratchToolNoticesBoundNamesAndBytes(t *testing.T) {
	report := &process.ScratchReport{Warning: strings.Repeat("\x00", 5000)}
	restored := &process.RestoreReport{}
	for range 1000 {
		item := process.SkippedName{Name: strings.Repeat("\x00", 1000), Reason: strings.Repeat("\x00", 1000)}
		report.Skipped = append(report.Skipped, item)
		restored.Failed = append(restored.Failed, item)
		restored.Restored = append(restored.Restored, item.Name)
	}
	notice := boundedScratchNotices(process.Result{Scratch: report, Restored: restored})
	data, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 16<<10 {
		t.Fatalf("notice bytes=%d", len(data))
	}
	scratch := notice["scratch"].(map[string]any)
	shown := scratch["skipped"].([]process.SkippedName)
	if len(shown) > 30 || len(shown)+scratch["skipped_omitted"].(int) != 1000 {
		t.Fatal("skip omission count incorrect")
	}
	revival := notice["restored"].(map[string]any)
	names := revival["restored"].([]string)
	if len(names) > 30 || len(names)+revival["restored_omitted"].(int) != 1000 {
		t.Fatal("restore omission count incorrect")
	}
	if len(report.Skipped) != 1000 || len(report.Skipped[0].Name) != 1000 {
		t.Fatal("presentation mutated durable report")
	}
}
