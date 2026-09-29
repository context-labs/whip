//go:build unix

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSeedProgressFailureKeepsLastCompletedCount(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	progress := newSeedProgress(&output)
	progress.begin("root_pairs", 4998)
	progress.advance(501, 500)
	progress.finish(errors.Join(context.DeadlineExceeded, errors.New("private fixture body")))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || strings.Contains(output.String(), "private fixture body") {
		t.Fatalf("unexpected diagnostic output: %s", output.String())
	}
	var last struct {
		Stage     string `json:"stage"`
		Completed int    `json:"completed"`
		Total     int    `json:"total"`
		Outcome   string `json:"outcome"`
		ErrorKind string `json:"error_kind"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Stage != "root_pairs" || last.Completed != 501 || last.Total != 4998 ||
		last.Outcome != "failed" || last.ErrorKind != "deadline" {
		t.Fatalf("failure lost stage/count: %+v", last)
	}
}

func TestSeedProgressBoundsReserveFinalOutcome(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	progress := newSeedProgress(&output)
	progress.begin("root_pairs", 4998)
	for completed := range 1000 {
		progress.advance(completed, 1)
	}
	progress.finish(nil)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 64 || output.Len() > 64<<10 {
		t.Fatalf("unbounded diagnostics: records=%d bytes=%d", len(lines), output.Len())
	}
	var last struct {
		Outcome   string `json:"outcome"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(lines[63]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Outcome != "complete" || !last.Truncated {
		t.Fatalf("terminal outcome was not retained: %+v", last)
	}
}
