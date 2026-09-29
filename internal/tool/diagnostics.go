package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/context-labs/whip/internal/session"
)

// FileSnapshot is the exact bounded content captured by a successful file
// operation. Diagnostics describe this content; other writers are not frozen.
type FileSnapshot struct {
	Path, Text        string
	WorkspaceIdentity os.FileInfo
}

type DiagnosticRun func(context.Context, session.OperationID, FileSnapshot) (any, error)

type DiagnosticProvider interface {
	PrepareDiagnostics(context.Context, session.Session) (DiagnosticRun, error)
}

func (d *Dispatcher) diagnosticRun(ctx context.Context, owner session.Session) (DiagnosticRun, error) {
	provider, ok := d.coordination.(DiagnosticProvider)
	if !ok {
		return nil, errors.New("language server diagnostics unavailable")
	}
	return provider.PrepareDiagnostics(ctx, owner)
}

func (d *Dispatcher) prepareDiagnostics(ctx context.Context, owner session.Session, prepared Prepared) (Prepared, error) {
	diagnostics, err := d.diagnosticRun(ctx, owner)
	if err != nil {
		return Prepared{}, err
	}
	run := prepared.Run
	prepared.Capability = "lsp.diagnostics"
	prepared.Mutating = true // Starting a custom server may have external effects.
	prepared.Run = func(ctx context.Context, id session.OperationID) (any, error) {
		if _, err := run(ctx, id); err != nil {
			return nil, err
		}
		return diagnostics(ctx, id, prepared.FileSnapshot())
	}
	return prepared, nil
}

func (d *Dispatcher) afterFileWrite(ctx context.Context, owner session.Session, call Invocation, source session.OperationID, captured FileSnapshot) any {
	diagnostics, err := d.diagnosticRun(ctx, owner)
	if err != nil {
		return diagnosticObservation("", map[string]any{"state": "unavailable", "reason": failureText(err)})
	}
	digest := sha256.Sum256([]byte(string(source)))
	call.RequestID = "diagnostics_" + hex.EncodeToString(digest[:])
	contentHash := sha256.Sum256([]byte(captured.Text))
	path, err := filepath.Rel(owner.WorkingDirectory, captured.Path)
	if err != nil {
		return diagnosticObservation("", map[string]any{"state": "unavailable", "reason": failureText(err)})
	}
	arguments, _ := json.Marshal(map[string]any{"path": path, "source_operation": source, "content_sha256": hex.EncodeToString(contentHash[:])})
	prepared := Prepared{
		Capability: "lsp.diagnostics", Resource: owner.WorkingDirectory, Arguments: arguments, Mutating: true,
		Acquire: func(context.Context) (func(), error) { return func() {}, nil },
		Run:     func(ctx context.Context, id session.OperationID) (any, error) { return diagnostics(ctx, id, captured) },
	}
	value, id, err := d.callPrepared(ctx, call, prepared, true)
	if err != nil {
		return diagnosticObservation(id, map[string]any{"state": "unavailable", "reason": failureText(err)})
	}
	return diagnosticObservation(id, value)
}

func diagnosticObservation(id session.OperationID, result any) any {
	var operation *session.OperationID
	if id != "" {
		operation = &id
	}
	return map[string]any{"operation_id": operation, "result": result}
}
