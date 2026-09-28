package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

type diagnosticFixture struct {
	calls    atomic.Int32
	snapshot FileSnapshot
	fail     bool
}

func (*diagnosticFixture) PrepareCoordination(context.Context, session.Session, Invocation) (Prepared, error) {
	return Prepared{}, errors.New("unsupported")
}

func (f *diagnosticFixture) PrepareDiagnostics(context.Context, session.Session) (DiagnosticRun, error) {
	return func(_ context.Context, _ session.OperationID, snapshot FileSnapshot) (any, error) {
		f.calls.Add(1)
		f.snapshot = snapshot
		if f.fail {
			return nil, errors.New("server interrupted")
		}
		return map[string]any{"state": "ready", "output": "evidence"}, nil
	}, nil
}

func TestPostWriteDiagnosticsHaveSeparateAuthorityAndOutcome(t *testing.T) {
	db, d, owner, turn, _ := dispatchFixture(t)
	fixture := &diagnosticFixture{}
	d.coordination = fixture
	for _, capability := range []string{"files.write", "files.patch"} {
		if _, err := db.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(capability), SessionID: owner.ID, Capability: capability, Resource: owner.WorkingDirectory}); err != nil {
			t.Fatal(err)
		}
	}
	value, _, err := d.Call(t.Context(), invokeWrite(owner, "write-only"))
	if err != nil || fixture.calls.Load() != 0 {
		t.Fatal("write-only grant started language server", value, err)
	}
	observation := value.(map[string]any)["diagnostics"].(map[string]any)
	if observation["operation_id"].(*session.OperationID) != nil || observation["result"].(map[string]any)["state"] != "skipped" {
		t.Fatal("skipped diagnostic claims an operation", observation)
	}
	permissions, err := db.Permissions(t.Context(), owner.ID, "", 10)
	if err != nil || len(permissions) != 0 {
		t.Fatal("automatic diagnostics prompted", permissions, err)
	}
	if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "diagnostics", SessionID: owner.ID, Capability: "lsp.diagnostics", Resource: owner.WorkingDirectory}); err != nil {
		t.Fatal(err)
	}
	fixture.fail = true
	call := Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "patch", Module: "files", Name: "patch", Arguments: map[string]any{"path": "note.txt", "old_text": "once", "new_text": "twice"}}
	value, id, err := d.Call(t.Context(), call)
	if err != nil || fixture.calls.Load() != 1 || fixture.snapshot.Text != "twice" {
		t.Fatal("diagnostic failure changed successful patch", value, fixture.snapshot, err)
	}
	op, err := db.Operation(t.Context(), id)
	if err != nil || op.State != session.OperationSucceeded {
		t.Fatal(op, err)
	}
	operations, err := db.Operations(t.Context(), turn.ID, "", 10)
	if err != nil || len(operations) != 3 {
		t.Fatal(operations, err)
	}
	for _, op := range operations {
		if op.Capability == "lsp.diagnostics" {
			var args map[string]string
			if err := json.Unmarshal(op.Arguments, &args); err != nil || args["source_operation"] != string(id) || len(args["content_sha256"]) != 64 || op.State != session.OperationUncertain {
				t.Fatal("diagnostic provenance/outcome", op, args, err)
			}
		}
	}
	content, err := os.ReadFile(filepath.Join(owner.WorkingDirectory, "note.txt"))
	if err != nil || string(content) != "twice" {
		t.Fatal(string(content), err)
	}
}

func TestExplicitDiagnosticsOneUseAndSnapshot(t *testing.T) {
	db, d, owner, turn, _ := dispatchFixture(t)
	fixture := &diagnosticFixture{}
	d.coordination = fixture
	if err := os.WriteFile(filepath.Join(owner.WorkingDirectory, "note.txt"), []byte("captured"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "explicit", Module: "files", Name: "diagnostics", Arguments: map[string]any{"path": "note.txt"}}
	done := make(chan error, 1)
	go func() { _, _, err := d.Call(t.Context(), call); done <- err }()
	pending := awaitOperation(t, db, turn.ID, "explicit", session.OperationWaiting)
	if pending.Capability != "lsp.diagnostics" || fixture.calls.Load() != 0 {
		t.Fatal("server started before approval", pending)
	}
	if _, err := db.ResolvePermission(t.Context(), pending.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || fixture.snapshot.Text != "captured" {
		t.Fatal(fixture.snapshot, err)
	}
}
