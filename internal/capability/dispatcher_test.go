package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type testLedger struct {
	root      string
	pending   Admission
	events    []string
	completed Completion
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

func TestDispatcherWorkspaceMutationBlocksPathMutations(t *testing.T) {
	root := t.TempDir()
	dispatcher := NewDispatcher(immediateLedger{root: root}, NewWorkspaces(), nil)
	pathEntered := make(chan struct{})
	releasePath := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releasePath)
		}
	}()
	workspaceEntered := make(chan struct{})
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
			close(workspaceEntered)
			return "workspace", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 2)
	go func() {
		_, err := dispatcher.Dispatch(context.Background(), Request{
			RootID: "root", AgentID: "agent", CapabilityID: "writer", OperationID: "path", TraceID: "trace", Operation: "write", Arguments: json.RawMessage(`{"path":"file"}`),
		})
		errCh <- err
	}()
	<-pathEntered
	go func() {
		_, err := dispatcher.Dispatch(context.Background(), Request{
			RootID: "root", AgentID: "agent", CapabilityID: "shell", WriterCapabilityID: "writer", OperationID: "workspace", TraceID: "trace", Operation: "bash", Arguments: json.RawMessage(`{"command":"true"}`),
		})
		errCh <- err
	}()
	select {
	case <-workspaceEntered:
		t.Fatal("workspace mutation overlapped a path mutation")
	case <-time.After(50 * time.Millisecond):
	}
	close(releasePath)
	released = true
	select {
	case <-workspaceEntered:
	case <-time.After(time.Second):
		t.Fatal("workspace mutation did not run after the path mutation released")
	}
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}
