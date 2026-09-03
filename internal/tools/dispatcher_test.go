package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/session"
)

type countingLedger struct {
	*session.Store
	begins     atomic.Int64
	mu         sync.Mutex
	admissions []capability.Admission
}

func (l *countingLedger) Begin(ctx context.Context, admission capability.Admission) (capability.Ticket, error) {
	l.begins.Add(1)
	l.mu.Lock()
	l.admissions = append(l.admissions, admission)
	l.mu.Unlock()
	return l.Store.Begin(ctx, admission)
}

func (l *countingLedger) lastAdmission() capability.Admission {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.admissions[len(l.admissions)-1]
}

func TestBoundToolsUseDispatcherWithoutChangingOutput(t *testing.T) {
	root := t.TempDir()
	st, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(session.SessionKindAgent, root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}

	ledger := &countingLedger{Store: st}
	services := NewServices()
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateAllowOnce, "" })
	if err := services.BindDispatcher(ledger, st.Workspaces(), st.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	dispatcher := services.dispatcher
	if err := services.BindDispatcher(ledger, st.Workspaces(), st.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	if services.dispatcher != dispatcher {
		t.Fatal("rebinding identical authority replaced the dispatcher")
	}
	dispatched := AllWithServices(services)
	direct := directTools()
	path := filepath.Join(root, "file.txt")

	cases := []struct {
		name    string
		args    json.RawMessage
		prepare func()
	}{
		{name: "write", args: json.RawMessage(`{"path":` + quoteJSON(path) + `,"content":"one\n"}`)},
		{name: "read", args: json.RawMessage(`{"path":` + quoteJSON(path) + `}`), prepare: func() { _ = os.WriteFile(path, []byte("one\n"), 0o600) }},
		{name: "edit", args: json.RawMessage(`{"path":` + quoteJSON(path) + `,"old_string":"one","new_string":"two"}`), prepare: func() { _ = os.WriteFile(path, []byte("one\n"), 0o600) }},
		{name: "bash", args: json.RawMessage(`{"command":"printf bound"}`)},
	}
	ctx, err := WithTurnIdentity(context.Background(), "bound-test")
	if err != nil {
		t.Fatal(err)
	}
	var commandID, traceID string
	for _, tc := range cases {
		if tc.prepare != nil {
			tc.prepare()
		}
		want := Execute(context.Background(), direct, tc.name, tc.args)
		if tc.prepare != nil {
			tc.prepare()
		}
		got := Execute(WithOperationIdentity(ctx, tc.name+"-bound"), dispatched, tc.name, tc.args)
		if got != want {
			t.Errorf("%s output = %q, want %q", tc.name, got, want)
		}
		bound := ledger.lastAdmission()
		if commandID == "" {
			commandID, traceID = bound.Request.CommandID, bound.Request.TraceID
		} else if bound.Request.CommandID != commandID || bound.Request.TraceID != traceID {
			t.Errorf("%s lost turn command/trace identity: %+v", tc.name, bound.Request)
		}
		directRequest := bound.Request
		directRequest.OperationID = directRequest.CommandID + ":" + tc.name + "-direct"
		if tc.prepare != nil {
			tc.prepare()
		}
		directResponse, err := dispatcher.Dispatch(ctx, directRequest)
		if err != nil {
			t.Fatalf("direct %s dispatch: %v", tc.name, err)
		}
		if directResponse.Output != want {
			t.Errorf("direct %s output = %q, want %q", tc.name, directResponse.Output, want)
		}
		directAdmission := ledger.lastAdmission()
		if bound.Mutation != directAdmission.Mutation || bound.RequirePermission != directAdmission.RequirePermission ||
			bound.CanonicalRoot != directAdmission.CanonicalRoot || bound.CanonicalPath != directAdmission.CanonicalPath ||
			bound.Request.TraceID != directAdmission.Request.TraceID || bound.Request.CommandClientID != directAdmission.Request.CommandClientID ||
			bound.Request.CommandID != directAdmission.Request.CommandID || !reflect.DeepEqual(bound.Request.Reservations, directAdmission.Request.Reservations) {
			t.Errorf("%s bound/direct admission mismatch:\nbound:  %+v\ndirect: %+v", tc.name, bound, directAdmission)
		}
	}
	res, err := services.RunBash(context.Background(), "printf wait", time.Second)
	if err != nil || res.Output != "wait" {
		t.Fatalf("managed bash result = %+v, error = %v", res, err)
	}
	if _, err := BrowserExec(services).Run(context.Background(), json.RawMessage(`{"code":"info()"}`)); err == nil {
		t.Fatal("uninitialized browser should fail after dispatcher admission")
	}
	if got, want := ledger.begins.Load(), int64(2*len(cases)+2); got != want {
		t.Fatalf("dispatcher admissions = %d, want %d", got, want)
	}
}

func TestAuthorityCloneKeepsHostIntegrationsAndPermissionMode(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(session.SessionKindAgent, root, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}

	manager := browser.NewManager(browser.ModeHeadless)
	policy := computer.NewPolicy([]string{"Finder"}, nil, true)
	parent := NewServices()
	parent.SetBrowser(manager, true)
	parent.SetComputerPolicy(policy)
	parent.SetComputerApprover(func(string) bool { return true })
	parent.SetScreenshotSink(func([][]byte) {})
	parent.SetExternalPermissions(true)
	parent.noteGeneration("Finder", 7)

	clone, err := parent.CloneForAuthority(store, store.Workspaces(), store.Processes(), authority)
	if err != nil {
		t.Fatal(err)
	}
	cloneBrowser, allowPrivate, screenshot := clone.browserConfig()
	clonePolicy, approver := clone.computerApproval()
	if cloneBrowser != manager || !allowPrivate || clonePolicy != policy || approver == nil {
		t.Fatal("authority clone dropped host integrations")
	}
	if screenshot != nil || !clone.ExternalPermissionsEnabled() {
		t.Fatal("authority clone copied an agent callback or lost external permission mode")
	}
	if clone.permissionWaiters == nil || clone.permissionEarly == nil || clone.generationFor("Finder") != 7 {
		t.Fatal("authority clone did not initialize independent runtime state")
	}
}

func TestUnboundToolsFailClosed(t *testing.T) {
	for _, tool := range All() {
		if tool.Def.Function.Name == "read" {
			if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"go.mod"}`)); err == nil {
				t.Fatal("unbound tool execution should fail")
			}
			return
		}
	}
	t.Fatal("read tool missing")
}

func quoteJSON(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}
