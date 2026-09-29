package runtime

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const maxCellOutputBytes = 64 << 10

// CellOutputPreview is disposable stdout from one admitted, running cell.
// The cell's committed result remains authoritative after settlement.
type CellOutputPreview struct {
	SessionID       session.SessionID
	TurnID          session.TurnID
	CellID          session.CellID
	CallMessageID   session.MessageID
	CallID          string
	HistoryRevision session.Revision
	Revision        int64
	Text            string
	Truncated       bool
}

func (r *Runtime) beginCellOutput(turn session.Turn, id session.CellID, message session.MessageID, call string) (func(string), func()) {
	live := &CellOutputPreview{SessionID: turn.SessionID, TurnID: turn.ID, CellID: id, CallMessageID: message, CallID: call, HistoryRevision: turn.HistoryRevision}
	r.previewMu.Lock()
	if r.cellOutputs[turn.SessionID] == nil && len(r.cellOutputs) >= 64 {
		r.previewMu.Unlock()
		return func(string) {}, func() {}
	}
	r.cellOutputs[turn.SessionID] = live
	r.previewMu.Unlock()
	return func(output string) {
			r.previewMu.Lock()
			defer r.previewMu.Unlock()
			if r.cellOutputs[turn.SessionID] != live || live.Truncated || live.Text == output {
				return
			}
			limit := min(len(output), maxCellOutputBytes)
			count := 0
			for count < limit {
				char, width := utf8.DecodeRuneInString(output[count:limit])
				if char == utf8.RuneError && width == 1 {
					break
				}
				count += width
			}
			live.Text = strings.Clone(output[:count])
			live.Truncated = count < len(output)
			live.Revision++
		}, func() {
			r.previewMu.Lock()
			defer r.previewMu.Unlock()
			if r.cellOutputs[turn.SessionID] == live {
				delete(r.cellOutputs, turn.SessionID)
			}
		}
}

// CellOutput reads without starting a kernel or waiting for a live execution lock.
// Read the live value first, then suppress settled cells and retired history.
//
//nolint:nilnil // The absence of a live preview is a valid observation, not a missing session.
func (r *Runtime) CellOutput(ctx context.Context, owner session.SessionID) (*CellOutputPreview, error) {
	r.previewMu.Lock()
	var result *CellOutputPreview
	if live := r.cellOutputs[owner]; live != nil {
		value := *live
		result = &value
	}
	r.previewMu.Unlock()
	snapshot, err := r.store.HistorySnapshot(ctx, owner)
	if err != nil || result == nil {
		return nil, err
	}
	if result.HistoryRevision != snapshot.Revision {
		return nil, nil
	}
	cell, err := r.store.Cell(ctx, result.CellID)
	if err != nil {
		return nil, err
	}
	if cell.SessionID != owner || cell.TurnID != result.TurnID || cell.CallMessageID != result.CallMessageID || cell.CallID != result.CallID || cell.State != session.CellRunning {
		return nil, nil
	}
	return result, nil
}
