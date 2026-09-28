// Package tool prepares host actions and dispatches them through durable,
// scoped authority. It owns no SQL, scheduler, interpreter or transport.
package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

type Ledger interface {
	AdmitOperation(context.Context, session.OperationSpec) (session.Operation, error)
	Operation(context.Context, session.OperationID) (session.Operation, error)
	DispatchOperation(context.Context, session.OperationID) (bool, error)
	SettleOperation(context.Context, session.OperationID, session.OperationResult) (session.Operation, error)
}

type Sessions interface {
	Session(context.Context, session.SessionID) (session.Session, error)
}

type Invocation struct {
	SessionID session.SessionID
	CellID    session.CellID
	RequestID string
	Module    string
	Name      string
	Arguments map[string]any
}

type Dispatcher struct {
	ledger   Ledger
	sessions Sessions
	files    *Files
}

func NewDispatcher(ledger Ledger, sessions Sessions) *Dispatcher {
	return &Dispatcher{ledger: ledger, sessions: sessions, files: NewFiles()}
}

func (d *Dispatcher) Call(ctx context.Context, call Invocation) (any, session.OperationID, error) {
	if call.Module != "files" {
		return nil, "", errors.New("unsupported host module")
	}
	current, err := d.sessions.Session(ctx, call.SessionID)
	if err != nil {
		return nil, "", err
	}
	prepared, err := d.files.Prepare(current.WorkingDirectory, call.Module+"."+call.Name, call.Arguments)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256([]byte(string(call.CellID) + "\x00" + call.RequestID))
	id := session.OperationID("operation_" + hex.EncodeToString(digest[:]))
	admitted, err := d.ledger.AdmitOperation(ctx, session.OperationSpec{ID: id, CellID: call.CellID, RequestID: call.RequestID, Capability: prepared.Capability, Resource: prepared.Resource, Arguments: prepared.Arguments})
	if err != nil {
		return nil, id, err
	}
	if admitted.SessionID != call.SessionID {
		return nil, id, errors.New("operation owner mismatch")
	}
	if admitted.State != session.OperationWaiting && admitted.State != session.OperationReady {
		return nil, id, errors.New("operation already admitted; automatic replay prohibited")
	}
	if err := d.awaitPermission(ctx, id); err != nil {
		return nil, id, errors.Join(err, d.cancelPending(ctx, id, err))
	}
	release, err := prepared.Acquire(ctx)
	if err != nil {
		return nil, id, errors.Join(err, d.cancelPending(ctx, id, err))
	}
	defer release()
	allowed, err := d.ledger.DispatchOperation(ctx, id)
	if err != nil {
		return nil, id, errors.Join(err, d.cancelPending(ctx, id, err))
	} // Ambiguous admission never grants a handler call.
	if !allowed {
		outcome, err := d.ledger.Operation(ctx, id)
		if err != nil {
			return nil, id, err
		}
		if outcome.Result != nil && outcome.Result.Failure != nil {
			return nil, id, errors.New(*outcome.Result.Failure)
		}
		return nil, id, errors.New("operation dispatch was not authorized")
	}
	effectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	value, callErr := prepared.Run(effectCtx)
	cancel()
	result := session.OperationResult{State: session.OperationSucceeded}
	if callErr != nil {
		result.State = session.OperationFailed
		if prepared.Mutating || ctx.Err() != nil || errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			result.State = session.OperationUncertain
		}
		result.Failure = new(failureText(callErr))
	} else {
		result.Value, err = json.Marshal(value)
		if err != nil || len(result.Value) > session.MaxDocumentBytes/2 {
			// A completed effect remains completed even if its presentation is unusable.
			result.Value = json.RawMessage(`{"notice":"operation succeeded; output was unavailable or exceeded the size limit"}`)
			callErr = errors.New("operation succeeded but its output is unavailable")
			value = nil
		}
	}
	if err := d.settle(ctx, id, result); err != nil {
		return nil, id, fmt.Errorf("operation outcome could not be committed; do not repeat the effect: %w", err)
	}
	if result.State == session.OperationUncertain {
		return nil, id, fmt.Errorf("operation outcome is uncertain; do not automatically repeat the effect: %w", callErr)
	}
	return value, id, callErr
}

func (d *Dispatcher) awaitPermission(ctx context.Context, id session.OperationID) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		operation, err := d.ledger.Operation(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return errors.Join(ctx.Err(), d.cancelPending(ctx, id, ctx.Err()))
			}
			return err
		}
		switch operation.State {
		case session.OperationReady:
			return nil
		case session.OperationWaiting:
		default:
			if operation.Result != nil && operation.Result.Failure != nil {
				return errors.New(*operation.Result.Failure)
			}
			return fmt.Errorf("operation is %s", operation.State)
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), d.cancelPending(ctx, id, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) cancelPending(parent context.Context, id session.OperationID, reason error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	current, err := d.ledger.Operation(ctx, id)
	if err != nil {
		return err
	}
	if current.State.Terminal() {
		return nil
	}
	if current.State == session.OperationDispatched {
		return errors.New("cannot relabel dispatched operation as cancelled")
	}
	return d.settle(ctx, id, session.OperationResult{State: session.OperationCancelled, Failure: new(failureText(reason))})
}

func (d *Dispatcher) settle(parent context.Context, id session.OperationID, result session.OperationResult) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := d.ledger.SettleOperation(ctx, id, result)
		if err == nil || errors.Is(err, session.ErrInvalid) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

func failureText(err error) string {
	text := strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(err.Error(), "�"), "\x00", "�"))
	if text == "" {
		text = "host operation failed"
	}
	if len(text) > 16384 {
		text = text[:16384]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}
