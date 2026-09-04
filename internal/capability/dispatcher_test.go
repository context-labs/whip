package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testLedger struct {
	root      string
	pending   Admission
	events    []string
	completed Completion
	onDecide  func() error
}

type permissionApprover func(context.Context, PermissionPrompt) (Decision, error)

type immediateLedger struct{ root string }

func (l immediateLedger) WorkspaceRoot(context.Context, string) (string, error) { return l.root, nil }

func (immediateLedger) Begin(_ context.Context, admission Admission) (Ticket, error) {
	return Ticket{OperationID: admission.Request.OperationID, LeaseID: "lease-" + admission.Request.OperationID}, nil
}

func (immediateLedger) Pending(context.Context, string) (Admission, error) {
	return Admission{}, errors.New("no pending permission")
}

func (immediateLedger) Decide(context.Context, Admission, string, Decision) (Ticket, error) {
	return Ticket{}, errors.New("no pending permission")
}
func (immediateLedger) Finish(context.Context, Completion) error { return nil }

func (f permissionApprover) Decide(ctx context.Context, prompt PermissionPrompt) (Decision, error) {
	return f(ctx, prompt)
}

func (l *testLedger) WorkspaceRoot(context.Context, string) (string, error) { return l.root, nil }

func (l *testLedger) Begin(_ context.Context, admission Admission) (Ticket, error) {
	l.events = append(l.events, "begin")
	if admission.RequirePermission {
		l.pending = admission
		return Ticket{OperationID: admission.Request.OperationID, PermissionID: "permission"}, nil
	}
	return Ticket{OperationID: admission.Request.OperationID, LeaseID: "lease"}, nil
}

func (l *testLedger) Pending(context.Context, string) (Admission, error) {
	l.events = append(l.events, "pending")
	return l.pending, nil
}

func (l *testLedger) Decide(_ context.Context, admission Admission, _ string, decision Decision) (Ticket, error) {
	l.events = append(l.events, "decide")
	if !decision.Allow {
		return Ticket{}, ErrDenied
	}
	if l.onDecide != nil {
		if err := l.onDecide(); err != nil {
			return Ticket{}, err
		}
	}
	return Ticket{OperationID: admission.Request.OperationID, LeaseID: "lease"}, nil
}

func (l *testLedger) Finish(_ context.Context, completion Completion) error {
	l.events = append(l.events, "finish")
	l.completed = completion
	return nil
}

func TestDispatcherRunsDirectAndPersistedPermissionThroughOnePath(t *testing.T) {
	root := t.TempDir()
	workingDir := filepath.Join(root, "sub")
	if err := os.Mkdir(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	workingDir, _ = filepath.EvalSymlinks(workingDir)
	ledger := &testLedger{root: root}
	dispatcher := NewDispatcher(ledger, NewWorkspaces(), nil)
	if err := dispatcher.Register(Registration{
		Operation:  "write",
		Mutation:   MutationPath,
		Permission: true,
		Path: func(arguments json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(arguments, &args); err != nil {
				return "", err
			}
			return args.Path, nil
		},
		Handler: func(_ context.Context, call Call) (string, error) {
			ledger.events = append(ledger.events, "handler")
			if call.WorkingDir != workingDir {
				return "", fmt.Errorf("working directory = %q, want %q", call.WorkingDir, workingDir)
			}
			return "wrote", os.WriteFile(call.CanonicalPath, []byte("ok"), 0o600)
		},
	}); err != nil {
		t.Fatal(err)
	}
	request := Request{
		RootID: "root", AgentID: "agent", CapabilityID: "writer", CapabilityGeneration: 1,
		OperationID: "operation", TraceID: "trace", Operation: "write",
		WorkingDirectory: workingDir, Arguments: json.RawMessage(`{"path":"../file.txt"}`),
	}
	_, err := dispatcher.Dispatch(context.Background(), request)
	var pending *PermissionPendingError
	if !errors.As(err, &pending) || pending.PermissionID != "permission" {
		t.Fatalf("pending dispatch error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("handler ran before permission: %v", err)
	}
	response, err := dispatcher.Decide(context.Background(), "permission", Decision{Allow: true, PrincipalID: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Output != "wrote" || ledger.completed.Status != StatusSucceeded {
		t.Fatalf("response=%+v completion=%+v", response, ledger.completed)
	}
	want := []string{"begin", "pending", "decide", "handler", "finish"}
	if !reflect.DeepEqual(ledger.events, want) {
		t.Fatalf("admission order = %v, want %v", ledger.events, want)
	}
}

func TestDispatcherRejectsChangedCanonicalPermissionRequest(t *testing.T) {
	root := t.TempDir()
	ledger := &testLedger{root: root}
	dispatcher := NewDispatcher(ledger, NewWorkspaces(), nil)
	if err := dispatcher.Register(Registration{
		Operation: "write", Mutation: MutationPath, Permission: true,
		Path: func(arguments json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(arguments, &args); err != nil {
				return "", err
			}
			return args.Path, nil
		},
		Handler: func(context.Context, Call) (string, error) { return "unexpected", nil },
	}); err != nil {
		t.Fatal(err)
	}
	request := Request{RootID: "root", AgentID: "agent", CapabilityID: "writer", OperationID: "operation", TraceID: "trace", Operation: "write", Arguments: json.RawMessage(`{"path":"file"}`)}
	if _, err := dispatcher.Dispatch(context.Background(), request); err == nil {
		t.Fatal("permission should remain pending")
	}
	ledger.pending.CanonicalPath = filepath.Join(root, "other")
	if _, err := dispatcher.Decide(context.Background(), "permission", Decision{Allow: true, PrincipalID: "human"}); !errors.Is(err, ErrStaleAdmission) {
		t.Fatalf("changed admission error = %v", err)
	}
}

func TestDispatcherRejectsCanonicalPathChangeAfterApproval(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	redirected := filepath.Join(root, "redirected")
	for _, dir := range []string{allowed, redirected} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ledger := &testLedger{root: root, onDecide: func() error {
		return os.Symlink(redirected, filepath.Join(allowed, "target"))
	}}
	dispatcher := NewDispatcher(ledger, NewWorkspaces(), nil)
	runs := 0
	if err := dispatcher.Register(Registration{
		Operation: "write", Mutation: MutationPath, Permission: true,
		Path: func(json.RawMessage) (string, error) { return filepath.Join("allowed", "target", "file"), nil },
		Handler: func(_ context.Context, call Call) (string, error) {
			runs++
			return "unexpected", os.WriteFile(call.CanonicalPath, []byte("changed"), 0o600)
		},
	}); err != nil {
		t.Fatal(err)
	}
	request := Request{RootID: "root", AgentID: "agent", CapabilityID: "writer", OperationID: "operation", TraceID: "trace", Operation: "write"}
	if _, err := dispatcher.Dispatch(context.Background(), request); err == nil {
		t.Fatal("permission should remain pending")
	}
	if _, err := dispatcher.Decide(context.Background(), "permission", Decision{Allow: true, PrincipalID: "human"}); !errors.Is(err, ErrStaleAdmission) {
		t.Fatalf("changed path error = %v, want stale admission", err)
	}
	if runs != 0 {
		t.Fatalf("handler ran %d times after the canonical path changed", runs)
	}
	if _, err := os.Stat(filepath.Join(redirected, "file")); !os.IsNotExist(err) {
		t.Fatalf("changed path wrote outside its admitted target: %v", err)
	}
	if ledger.completed.Status != StatusFailed || ledger.completed.Error != ErrStaleAdmission.Error() {
		t.Fatalf("completion = %+v", ledger.completed)
	}
}

func TestDispatcherCancellationRejectsPendingPermission(t *testing.T) {
	root := t.TempDir()
	ledger := &testLedger{root: root}
	dispatcher := NewDispatcher(ledger, NewWorkspaces(), permissionApprover(func(context.Context, PermissionPrompt) (Decision, error) {
		return Decision{}, context.Canceled
	}))
	if err := dispatcher.Register(Registration{Operation: "bash", Mutation: MutationWorkspace, Permission: true, Handler: func(context.Context, Call) (string, error) {
		return "unexpected", nil
	}}); err != nil {
		t.Fatal(err)
	}
	request := Request{RootID: "root", AgentID: "agent", CapabilityID: "shell", WriterCapabilityID: "writer", OperationID: "operation", TraceID: "trace", Operation: "bash", Arguments: json.RawMessage(`{"command":"true"}`)}
	if _, err := dispatcher.Dispatch(context.Background(), request); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch error = %v, want canceled", err)
	}
	if want := []string{"begin", "decide"}; !reflect.DeepEqual(ledger.events, want) {
		t.Fatalf("events = %v, want %v", ledger.events, want)
	}
}

// Shell commands take no workspace lock: they overlap each other and any
// in-flight path mutation. Only same-path edits serialize.
func TestDispatcherWorkspaceMutationRunsAlongsidePathMutations(t *testing.T) {
	root := t.TempDir()
	dispatcher := NewDispatcher(immediateLedger{root: root}, NewWorkspaces(), nil)
	pathEntered := make(chan struct{})
	releasePath := make(chan struct{})
	shellEntered := make(chan struct{}, 2)
	releaseShell := make(chan struct{})
	if err := dispatcher.Register(Registration{
		Operation: "write", Mutation: MutationPath,
		Path: func(json.RawMessage) (string, error) { return "file", nil },
		Handler: func(context.Context, Call) (string, error) {
			close(pathEntered)
			<-releasePath
			return "path", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Register(Registration{
		Operation: "bash", Mutation: MutationWorkspace,
		Handler: func(context.Context, Call) (string, error) {
			shellEntered <- struct{}{}
			<-releaseShell
			return "workspace", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 3)
	go func() {
		_, err := dispatcher.Dispatch(context.Background(), Request{
			RootID: "root", AgentID: "agent", CapabilityID: "writer", OperationID: "path", TraceID: "trace", Operation: "write", Arguments: json.RawMessage(`{"path":"file"}`),
		})
		errCh <- err
	}()
	<-pathEntered
	for _, id := range []string{"shell-1", "shell-2"} {
		go func() {
			_, err := dispatcher.Dispatch(context.Background(), Request{
				RootID: "root", AgentID: "agent", CapabilityID: "shell", WriterCapabilityID: "writer", OperationID: id, TraceID: "trace", Operation: "bash", Arguments: json.RawMessage(`{"command":"true"}`),
			})
			errCh <- err
		}()
	}
	for range 2 {
		select {
		case <-shellEntered:
		case <-time.After(time.Second):
			t.Fatal("a shell command waited behind a path mutation or another shell command")
		}
	}
	close(releaseShell)
	close(releasePath)
	for range 3 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestDispatcherRegistrationValidation(t *testing.T) {
	handler := func(context.Context, Call) (string, error) { return "", nil }
	for _, tc := range []struct {
		name string
		reg  Registration
		want string
	}{
		{"missing operation", Registration{Handler: handler}, "requires an operation"},
		{"missing handler", Registration{Operation: "read"}, "requires an operation and handler"},
		{"invalid mutation", Registration{Operation: "read", Mutation: "invalid", Handler: handler}, "invalid mutation mode"},
		{"missing path extractor", Registration{Operation: "write", Mutation: MutationPath, Handler: handler}, "path extractor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := NewDispatcher(nil, nil, nil).Register(tc.reg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Register error = %v, want %q", err, tc.want)
			}
		})
	}

	d := NewDispatcher(nil, nil, nil)
	reg := Registration{Operation: "read", Handler: handler}
	if err := d.Register(reg); err != nil {
		t.Fatal(err)
	}
	if err := d.Register(reg); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate Register error = %v", err)
	}
}

func TestDispatcherRequestValidation(t *testing.T) {
	root := t.TempDir()
	d := NewDispatcher(immediateLedger{root: root}, NewWorkspaces(), nil)
	if err := d.Register(Registration{Operation: "read", Handler: func(context.Context, Call) (string, error) { return "", nil }}); err != nil {
		t.Fatal(err)
	}
	valid := Request{RootID: "root", AgentID: "agent", CapabilityID: "files", OperationID: "operation", TraceID: "trace", Operation: "read", Arguments: json.RawMessage(`{}`)}
	for _, tc := range []struct {
		name    string
		request Request
		want    string
	}{
		{"unknown operation", Request{Operation: "missing"}, "unknown capability operation"},
		{"incomplete identity", Request{Operation: "read"}, "identity is incomplete"},
		{"incomplete command identity", func() Request { r := valid; r.CommandID = "command"; return r }(), "command identity is incomplete"},
		{"malformed arguments", func() Request { r := valid; r.Arguments = json.RawMessage(`{`); return r }(), "invalid capability arguments"},
		{"multiple argument values", func() Request { r := valid; r.Arguments = json.RawMessage(`{} {}`); return r }(), "multiple JSON values"},
		{"invalid reservation", func() Request { r := valid; r.Reservations = []Reservation{{Kind: "", Amount: 1}}; return r }(), "positive amount"},
		{"duplicate reservation", func() Request {
			r := valid
			r.Reservations = []Reservation{{Kind: "tokens", Amount: 1}, {Kind: "tokens", Amount: 2}}
			return r
		}(), "duplicate capability reservation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := d.Dispatch(context.Background(), tc.request); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Dispatch error = %v, want %q", err, tc.want)
			}
		})
	}

	withoutAuthority := NewDispatcher(nil, NewWorkspaces(), nil)
	if err := withoutAuthority.Register(Registration{Operation: "read", Handler: func(context.Context, Call) (string, error) { return "", nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err := withoutAuthority.Dispatch(context.Background(), valid); err == nil || !strings.Contains(err.Error(), "requires a ledger") {
		t.Fatalf("missing authority error = %v", err)
	}
}

func TestDispatcherRecordsHandlerFailure(t *testing.T) {
	ledger := &testLedger{root: t.TempDir()}
	d := NewDispatcher(ledger, NewWorkspaces(), nil)
	wantErr := errors.New("handler failed")
	if err := d.Register(Registration{Operation: "fail", Handler: func(context.Context, Call) (string, error) {
		return "partial", wantErr
	}}); err != nil {
		t.Fatal(err)
	}
	response, err := d.Dispatch(context.Background(), Request{
		RootID: "root", AgentID: "agent", CapabilityID: "files", OperationID: "operation", TraceID: "trace", Operation: "fail",
	})
	if !errors.Is(err, wantErr) || response.Output != "partial" {
		t.Fatalf("response = %+v, error = %v", response, err)
	}
	if ledger.completed.Status != StatusFailed || ledger.completed.Output != "partial" || ledger.completed.Error != wantErr.Error() {
		t.Fatalf("completion = %+v", ledger.completed)
	}
}

func TestPermissionErrorMessages(t *testing.T) {
	if got := (&PermissionPendingError{PermissionID: "permission", OperationID: "operation"}).Error(); !strings.Contains(got, "permission") || !strings.Contains(got, "operation") {
		t.Fatalf("pending error = %q", got)
	}
	denied := &PermissionDeniedError{}
	if denied.Error() != "Permission denied: the user rejected this action" || !errors.Is(denied, ErrDenied) {
		t.Fatalf("denied error = %q", denied)
	}
}

func TestDispatcherRemainingValidationPaths(t *testing.T) {
	if _, err := NewDispatcher(&testLedger{}, NewWorkspaces(), nil).Decide(t.Context(), "", Decision{}); err == nil {
		t.Fatal("Decide accepted missing identities")
	}
	ledger := &testLedger{root: t.TempDir(), pending: Admission{Request: Request{Operation: "missing"}}}
	if _, err := NewDispatcher(ledger, NewWorkspaces(), nil).Decide(t.Context(), "permission", Decision{PrincipalID: "human"}); err == nil || !strings.Contains(err.Error(), "unknown capability operation") {
		t.Fatalf("unknown pending operation error = %v", err)
	}

	register := func(t *testing.T, d *Dispatcher, reg Registration) {
		t.Helper()
		if err := d.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	request := Request{RootID: "root", AgentID: "agent", CapabilityID: "files", OperationID: "operation", TraceID: "trace", Operation: "read"}
	d := NewDispatcher(immediateLedger{root: filepath.Join(t.TempDir(), "missing")}, NewWorkspaces(), nil)
	register(t, d, Registration{Operation: "read", Handler: func(context.Context, Call) (string, error) { return "", nil }})
	if _, err := d.Dispatch(t.Context(), request); err == nil {
		t.Fatal("Dispatch accepted a missing workspace")
	}

	root := t.TempDir()
	d = NewDispatcher(immediateLedger{root: root}, NewWorkspaces(), nil)
	register(t, d, Registration{Operation: "read", Handler: func(context.Context, Call) (string, error) { return "", nil }})
	request.WorkingDirectory = filepath.Dir(root)
	if _, err := d.Dispatch(t.Context(), request); err == nil {
		t.Fatal("Dispatch accepted a working directory outside the workspace")
	}

	d = NewDispatcher(immediateLedger{root: root}, NewWorkspaces(), nil)
	register(t, d, Registration{Operation: "read", Mutation: MutationPath, Path: func(json.RawMessage) (string, error) {
		return "", errors.New("path failed")
	}, Handler: func(context.Context, Call) (string, error) { return "", nil }})
	request.WorkingDirectory = ""
	if _, err := d.Dispatch(t.Context(), request); err == nil || !strings.Contains(err.Error(), "path failed") {
		t.Fatalf("path extraction error = %v", err)
	}
}

func TestDispatcherDeniedDecision(t *testing.T) {
	ledger := &testLedger{root: t.TempDir()}
	d := NewDispatcher(ledger, NewWorkspaces(), nil)
	if err := d.Register(Registration{Operation: "write", Permission: true, Handler: func(context.Context, Call) (string, error) {
		return "", nil
	}}); err != nil {
		t.Fatal(err)
	}
	request := Request{RootID: "root", AgentID: "agent", CapabilityID: "files", OperationID: "operation", TraceID: "trace", Operation: "write"}
	if _, err := d.Dispatch(t.Context(), request); err == nil {
		t.Fatal("permission was not left pending")
	}
	_, err := d.Decide(t.Context(), "permission", Decision{PrincipalID: "human", Reason: "no"})
	var denied *PermissionDeniedError
	if !errors.As(err, &denied) || denied.Reason != "no" {
		t.Fatalf("denied decision error = %v", err)
	}
}
