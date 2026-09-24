package quickjs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/rlm/engine"
)

func validArgs(args []byte, byteLimit int) error {
	if err := validJSON(args, byteLimit); err != nil {
		return err
	}
	if bytes.TrimSpace(args)[0] != '{' {
		return errors.New("bridge: tool arguments must be a JSON object")
	}
	return nil
}

// ValidateOutcome is pure, uses this runtime's immutable policy, and never
// observes or settles a guest waiter. Coordinators must call it before recording
// a result (including cancellation), not after an undeliverable journal commit.
func (v *runtime) ValidateOutcome(ctx context.Context, out engine.Outcome) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := v.encodeOutcome(out)
	return err
}

func (v *runtime) encodeOutcome(out engine.Outcome) ([]byte, error) {
	byteLimit := v.factory.options.Limits.MaxResultBytes
	if out.ID == "" || len(out.ID) > 1600 || !utf8.ValidString(out.ID) || strings.ContainsRune(out.ID, 0) {
		return nil, errors.New("bridge: invalid outcome ID")
	}
	if out.OK {
		if out.Error != nil {
			return nil, errors.New("bridge: successful outcome has error")
		}
		if err := validJSON(out.Value, byteLimit); err != nil {
			return nil, err
		}
	} else {
		if out.Error == nil || out.Error.Code == "" || len(out.Value) != 0 {
			return nil, errors.New("bridge: invalid error outcome")
		}
		if len(out.Error.Code) > byteLimit || len(out.Error.Message) > byteLimit {
			return nil, errors.New("bridge: error byte limit")
		}
		if !utf8.ValidString(out.Error.Code) || !utf8.ValidString(out.Error.Message) {
			return nil, errors.New("bridge: invalid UTF-8 error")
		}
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(payload) > byteLimit {
		return nil, errors.New("bridge: outcome byte limit")
	}
	return payload, nil
}

// ValidateCell checks policy and cell identity without evaluating source.
// Syntax errors remain guest outcomes; JavaScript evaluation is not transactional.
func (v *runtime) ValidateCell(ctx context.Context, cellID, source string) error {
	if err := v.enter(ctx); err != nil {
		return err
	}
	defer v.mu.Unlock()
	return v.validateCellLocked(cellID, source)
}

func (v *runtime) validateCellLocked(cellID, source string) error {
	if !validID(cellID) || !utf8.ValidString(source) {
		return errors.New("bridge: invalid cell ID/source")
	}
	if len(source) > v.factory.options.Limits.MaxRequestBytes {
		return errors.New("bridge: source byte limit")
	}

	if _, exists := v.cells[cellID]; exists {
		return errors.New("bridge: cell ID already used; cell IDs must be unique")
	}
	if len(v.pending) != 0 || v.jobsPending || v.cellStatus == "running" {
		return ErrBusy
	}
	return nil
}
