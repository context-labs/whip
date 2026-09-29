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
	"slices"
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
	CellSession(context.Context, session.SessionID, session.CellID) (session.Session, error)
}

type Coordination interface {
	PrepareCoordination(context.Context, session.Session, Invocation) (Prepared, error)
}

type Invocation struct {
	SessionID    session.SessionID
	CellID       session.CellID
	DirectTurnID session.TurnID
	RequestID    string
	Module       string
	Name         string
	Arguments    map[string]any
}

// OperationID binds one immutable host request to its actual cell or direct turn.
// Preparation may derive prospective resources from it before admission; it is
// never evidence of permission or successful dispatch.
func (call Invocation) OperationID() session.OperationID {
	owner := string(call.CellID)
	if call.DirectTurnID != "" {
		owner = "host_turn:" + string(call.DirectTurnID)
	}
	digest := sha256.Sum256([]byte(owner + "\x00" + call.RequestID))
	return session.OperationID("operation_" + hex.EncodeToString(digest[:]))
}

type Dispatcher struct {
	ledger       Ledger
	sessions     Sessions
	files        *Files
	coordination Coordination
}

func NewDispatcher(ledger Ledger, sessions Sessions, coordination Coordination) *Dispatcher {
	return &Dispatcher{ledger: ledger, sessions: sessions, files: NewFiles(), coordination: coordination}
}

func (d *Dispatcher) Call(ctx context.Context, call Invocation) (any, session.OperationID, error) {
	current, err := d.invocationSession(ctx, call)
	if err != nil {
		return nil, "", err
	}
	if call.Module == "tools" {
		declaration, enabled := current.Config.Tools[call.Name]
		if !enabled {
			return nil, "", fmt.Errorf("%w: custom tool is not enabled for this turn", session.ErrInvalid)
		}
		if err := declaration.ValidateInput(call.Arguments); err != nil {
			return nil, "", err
		}
	}
	if call.Module != "tools" && !slices.Contains(current.Config.Modules, call.Module) {
		return nil, "", fmt.Errorf("%w: host module is not enabled for this turn", session.ErrInvalid)
	}
	if hooks, ok := d.coordination.(interface {
		BeforeTool(context.Context, session.Session, Invocation) (Invocation, error)
	}); ok {
		call, err = hooks.BeforeTool(ctx, current, call)
		if err != nil {
			return nil, "", err
		}
		if call.Module == "tools" {
			if err := current.Config.Tools[call.Name].ValidateInput(call.Arguments); err != nil {
				return nil, "", err
			}
		}
	}
	var prepared Prepared
	if call.Module == "files" {
		prepared, err = d.files.Prepare(current.WorkingDirectory, call.Module+"."+call.Name, call.Arguments)
	} else if d.coordination != nil {
		prepared, err = d.coordination.PrepareCoordination(ctx, current, call)
	} else if call.Module == "tools" {
		err = errors.New("custom tool executor unavailable")
	} else {
		err = errors.New("unsupported host module")
	}
	if err != nil {
		return nil, "", err
	}
	if call.Module == "files" && call.Name == "diagnostics" {
		prepared, err = d.prepareDiagnostics(ctx, current, prepared)
		if err != nil {
			return nil, "", err
		}
	}
	value, id, err := d.callPrepared(ctx, call, prepared, false)
	if err == nil && call.Module == "files" && fileMutation("files."+call.Name) {
		if output, ok := value.(map[string]any); ok {
			output["diagnostics"] = d.afterFileWrite(ctx, current, call, id, prepared.FileSnapshot())
		}
	}
	return value, id, err
}

func (d *Dispatcher) callPrepared(ctx context.Context, call Invocation, prepared Prepared, standingOnly bool) (any, session.OperationID, error) {
	if prepared.Lifetime != nil {
		lifetimeCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(prepared.Lifetime, cancel)
		defer func() { stop(); cancel() }()
		if err := prepared.Lifetime.Err(); err != nil {
			return nil, "", err
		}
		ctx = lifetimeCtx
	}
	if prepared.ModelTimeouts && (prepared.Apply != nil || (prepared.Capability != "models.call" && prepared.Capability != "models.batch")) {
		return nil, "", fmt.Errorf("%w: model timeouts require models.call or models.batch execution", session.ErrInvalid)
	}

	if prepared.CustomTimeout != 0 && (call.Module != "tools" || prepared.Capability != "tools."+call.Name || prepared.CustomTimeout < 0 || prepared.CustomTimeout > 15*time.Minute || prepared.Timeout != 0 || prepared.ModelTimeouts || prepared.Apply != nil) {
		return nil, "", fmt.Errorf("%w: invalid custom tool timeout", session.ErrInvalid)
	}
	if prepared.Timeout < 0 || prepared.Timeout > 5*time.Minute || prepared.Timeout != 0 && (prepared.ModelTimeouts || prepared.Apply != nil || prepared.Capability == "models.call" || prepared.Capability == "models.batch") {
		return nil, "", fmt.Errorf("%w: invalid host effect timeout", session.ErrInvalid)
	}

	id := call.OperationID()
	spec := session.OperationSpec{ID: id, CellID: call.CellID, DirectTurnID: call.DirectTurnID, RequestID: call.RequestID, Capability: prepared.Capability, Resource: prepared.Resource, Arguments: prepared.Arguments}
	var admitted session.Operation
	var err error
	if standingOnly {
		ledger, ok := d.ledger.(interface {
			AdmitStandingOperation(context.Context, session.OperationSpec) (session.Operation, error)
		})
		if !ok {
			return nil, "", errors.New("standing diagnostic admission unavailable")
		}
		admitted, err = ledger.AdmitStandingOperation(ctx, spec)
		if err == nil && admitted.ID == "" {
			return map[string]any{"state": "skipped", "reason": "no standing lsp.diagnostics authority for this workspace"}, "", nil
		}
	} else {
		admitted, err = d.ledger.AdmitOperation(ctx, spec)
	}
	if err != nil {
		return nil, id, err
	}
	if admitted.SessionID != call.SessionID {
		return nil, id, Fatal(errors.New("operation owner mismatch"))
	}
	if admitted.State == session.OperationDispatched {
		return nil, id, Fatal(errors.New("operation remains dispatched; automatic replay prohibited"))
	}
	if admitted.State != session.OperationWaiting && admitted.State != session.OperationReady {
		if admitted.Result != nil && admitted.Result.Failure != nil {
			return nil, id, errors.New(*admitted.Result.Failure)
		}
		return nil, id, errors.New("operation already admitted; automatic replay prohibited")
	}
	if err := d.awaitPermission(ctx, id); err != nil {
		return nil, id, d.cancelAfterError(ctx, id, err)
	}
	if prepared.Apply != nil {
		value, err := prepared.Apply(ctx, id)
		if err != nil {
			return nil, id, d.cancelAfterError(ctx, id, err)
		}
		return value, id, nil
	}
	release, err := prepared.Acquire(ctx)
	if err != nil {
		return nil, id, d.cancelAfterError(ctx, id, err)
	}
	defer release()
	allowed, err := d.ledger.DispatchOperation(ctx, id)
	if err != nil {
		return nil, id, d.cancelAfterError(ctx, id, err)
	} // Ambiguous admission never grants a handler call.
	if !allowed {
		outcome, err := d.ledger.Operation(ctx, id)
		if err != nil {
			return nil, id, Fatal(err)
		}
		if !outcome.State.Terminal() {
			return nil, id, Fatal(errors.New("operation dispatch outcome is unresolved"))
		}
		if outcome.Result != nil && outcome.Result.Failure != nil {
			return nil, id, errors.New(*outcome.Result.Failure)
		}
		return nil, id, errors.New("operation dispatch was not authorized")
	}
	timeout := prepared.Timeout
	if prepared.CustomTimeout != 0 {
		timeout = prepared.CustomTimeout
	}
	effectCtx, cancel := operationContext(ctx, prepared.ModelTimeouts, timeout)
	value, callErr := prepared.Run(effectCtx, id)
	cancel()
	if isFatal(callErr) {
		// Linked attempt accounting may still be unresolved. Do not fabricate a
		// terminal operation; recovery owns the persisted dispatched evidence.
		return nil, id, callErr
	}
	result := session.OperationResult{State: session.OperationSucceeded}
	if output, ok := value.(Output); ok {
		value = output.Value
		result.ContentReferences = slices.Clone(output.ContentReferences)
	}
	if callErr != nil {
		result.State = session.OperationFailed
		if !isSettledFailure(callErr) && (prepared.Mutating || ctx.Err() != nil || errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded)) {
			result.State = session.OperationUncertain
		}
		result.Failure = new(failureText(callErr))
	}
	if callErr == nil || value != nil {
		result.Value, err = json.Marshal(value)
		if err != nil || len(result.Value) > session.MaxDocumentBytes/2 {
			// Keep the observed effect state even when presentation is unusable.
			result.Value = json.RawMessage(`{"notice":"operation output was unavailable or exceeded the size limit"}`)
			if callErr == nil {
				callErr = errors.New("operation succeeded but its output is unavailable")
			}
			value = nil
		}
	}
	if err := d.settle(ctx, id, result); err != nil {
		return nil, id, Fatal(fmt.Errorf("operation outcome could not be committed; do not repeat the effect: %w", err))
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
				return ctx.Err()
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
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func operationContext(parent context.Context, modelTimeouts bool, timeout time.Duration) (context.Context, context.CancelFunc) {
	if modelTimeouts {
		return context.WithCancel(parent)
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(parent, timeout)
}

func (d *Dispatcher) cancelAfterError(ctx context.Context, id session.OperationID, cause error) error {
	if isFatal(cause) {
		return cause
	}
	if err := d.cancelPending(ctx, id, cause); err != nil {
		return Fatal(errors.Join(cause, err))
	}
	return cause
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

func (d *Dispatcher) invocationSession(ctx context.Context, call Invocation) (session.Session, error) {
	if call.DirectTurnID == "" {
		return d.sessions.CellSession(ctx, call.SessionID, call.CellID)
	}
	if call.CellID != "" {
		return session.Session{}, session.ErrInvalid
	}
	direct, ok := d.sessions.(interface {
		HostTurnSession(context.Context, session.SessionID, session.TurnID) (session.Session, error)
	})
	if !ok {
		return session.Session{}, errors.New("direct host execution unavailable")
	}
	return direct.HostTurnSession(ctx, call.SessionID, call.DirectTurnID)
}
