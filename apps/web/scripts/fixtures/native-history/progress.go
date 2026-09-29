//go:build unix

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type seedProgress struct {
	output    io.Writer
	started   time.Time
	stageAt   time.Time
	stage     string
	completed int
	total     int
	records   int
	truncated bool
}

type seedUsage struct {
	UserMillis   int64 `json:"user_ms"`
	SystemMillis int64 `json:"system_ms"`
	BlockOutputs int64 `json:"block_outputs"`
}

func newSeedProgress(output io.Writer) *seedProgress {
	return &seedProgress{output: output, started: time.Now()}
}

func (p *seedProgress) begin(stage string, total int) {
	p.stage, p.total, p.completed, p.stageAt = stage, total, 0, time.Now()
	p.write("progress", "")
}

func (p *seedProgress) advance(completed, interval int) {
	p.completed = completed
	if completed%interval == 0 || completed == p.total {
		p.write("progress", "")
	}
}

func (p *seedProgress) finish(err error) {
	outcome, errorKind := "complete", ""
	if err != nil {
		outcome, errorKind = "failed", "error"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			errorKind = "deadline"
		case errors.Is(err, context.Canceled):
			errorKind = "cancelled"
		}
	}
	p.write(outcome, errorKind)
}

func (p *seedProgress) write(outcome, errorKind string) {
	// Reserve the last of at most 64 records for the terminal outcome. No timer,
	// background observer or extra database read competes with the seed.
	if p.records >= 64 || (p.records >= 63 && outcome == "progress") {
		p.truncated = true
		return
	}
	p.records++
	var usage *seedUsage
	var resources unix.Rusage
	if unix.Getrusage(unix.RUSAGE_SELF, &resources) == nil {
		usage = &seedUsage{
			UserMillis:   resources.Utime.Nano() / int64(time.Millisecond),
			SystemMillis: resources.Stime.Nano() / int64(time.Millisecond),
			BlockOutputs: resources.Oublock,
		}
	}
	value := struct {
		Event        string     `json:"event"`
		PID          int        `json:"pid"`
		Stage        string     `json:"stage"`
		Completed    int        `json:"completed"`
		Total        int        `json:"total"`
		Elapsed      int64      `json:"elapsed_ms"`
		StageElapsed int64      `json:"stage_elapsed_ms"`
		Outcome      string     `json:"outcome"`
		ErrorKind    string     `json:"error_kind,omitempty"`
		Truncated    bool       `json:"truncated,omitempty"`
		Usage        *seedUsage `json:"process_usage,omitempty"`
	}{
		Event: "native-history-seed", PID: os.Getpid(), Stage: p.stage,
		Completed: p.completed, Total: p.total,
		Elapsed: time.Since(p.started).Milliseconds(), StageElapsed: time.Since(p.stageAt).Milliseconds(),
		Outcome: outcome, ErrorKind: errorKind, Truncated: p.truncated, Usage: usage,
	}
	// Diagnostics must not change admission, durability or failure semantics.
	_ = json.NewEncoder(p.output).Encode(value)
}
