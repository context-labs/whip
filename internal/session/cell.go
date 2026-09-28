package session

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type (
	CellID    string
	CellState string
)

const (
	CellRunning   CellState = "running"
	CellSucceeded CellState = "succeeded"
	CellFailed    CellState = "failed"
	CellUncertain CellState = "uncertain"
)

// Checkpoint identifies an immutable body. Its engine-owned metadata is opaque
// to persistence; the execution adapter verifies compatibility before restoring.
type Checkpoint struct {
	Digest   string          `json:"digest"`
	Size     int64           `json:"size"`
	Engine   Engine          `json:"engine"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c Checkpoint) Validate() error {
	digest, err := hex.DecodeString(c.Digest)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != c.Digest || c.Size < 1 || c.Size > 40<<20 || c.Engine.Validate() != nil || len(c.Metadata) > 65536 || !json.Valid(c.Metadata) || len(c.Metadata) == 0 || c.Metadata[0] != '{' {
		return fmt.Errorf("%w: invalid checkpoint", ErrInvalid)
	}
	return nil
}

// Cell binds execution to an already committed assistant call. The result
// message and checkpoint commit together. A nil checkpoint on the latest cell
// blocks restoration: an older image must not masquerade as current REPL state.
type Cell struct {
	ID              CellID
	SessionID       SessionID
	TurnID          TurnID
	CallMessageID   MessageID
	CallID          string
	State           CellState
	ResultMessageID *MessageID
	Checkpoint      *Checkpoint
	CreatedAt       time.Time
	FinishedAt      *time.Time
}

type CellSpec struct {
	ID            CellID
	TurnID        TurnID
	CallMessageID MessageID
	CallID        string
}
