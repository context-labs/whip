package engine

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrJobBudget is a quiescent slice boundary with queued jobs still remaining.
var ErrJobBudget = errors.New("quickjs: job budget exhausted; pending jobs remain")

type Request struct {
	ID   string          `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Outcome struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error *RemoteError    `json:"error,omitempty"`
}

// CancellationOutcome rejects a local waiter; it does not undo a remote effect.
func CancellationOutcome(id string) Outcome {
	return Outcome{ID: id, OK: false, Error: &RemoteError{Code: "CANCELLED", Message: "cancelled"}}
}

type View struct {
	CellID   string          `json:"cellId"`
	HasValue bool            `json:"hasValue"`
	Output   string          `json:"output"`
	Status   string          `json:"status"`
	Value    json.RawMessage `json:"value,omitempty"`
	Error    *RemoteError    `json:"error,omitempty"`
	Pending  []string        `json:"pending"`
}

type Limits struct {
	MemoryBytes       uint64
	MemoryPages       uint32
	MaxQueuedRequests int
	MaxRequestBytes   int
	MaxResultBytes    int
	MaxOutputBytes    int
	MaxSnapshotBytes  uint64
}

type Options struct {
	AllowedTools []string
	Limits       Limits
}

type Runtime interface {
	// ValidateCell checks admission without evaluating source or consuming a cell ID.
	ValidateCell(ctx context.Context, cellID, source string) error
	RunCell(ctx context.Context, cellID, source string) error
	// ErrJobBudget requires another drain slice before the cell can settle.
	Drain(ctx context.Context, maxJobs int) (int, error)
	TakeRequests() []Request
	// ValidateOutcome checks schema and runtime-specific quotas without running guest code or mutating state.
	ValidateOutcome(ctx context.Context, outcome Outcome) error
	Deliver(ctx context.Context, outcome Outcome) (bool, error)
	Checkpoint(ctx context.Context) ([]byte, error)
	Finish(ctx context.Context) error
	Inspect(ctx context.Context) (View, error)
	Close(ctx context.Context) error
}

type Factory interface {
	New(ctx context.Context, sessionID string) (Runtime, error)
	Restore(ctx context.Context, sessionID string, image []byte) (Runtime, error)
	Close(ctx context.Context) error
}
