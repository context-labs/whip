package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestExternalBrowserPassiveCASCapturedDispatchAndNoReopen(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	before, err := r.ExternalBrowserStatus(t.Context())
	if err != nil || before.Configuration.Mode != "disabled" {
		t.Fatal(before, err)
	}
	settings := browserconfig.Config{Mode: "live", LiveEndpoint: server.URL}
	after, err := r.ConfigureExternalBrowser(t.Context(), before.Revision, settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ConfigureExternalBrowser(t.Context(), before.Revision, settings); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal(err)
	}
	call := browserCall(owner, cell, "external", "run", map[string]any{"session": "default", "code": "info()"})
	prepared, err := r.prepareBrowser(t.Context(), owner, call)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Capability != "browser.external" {
		t.Fatal(prepared)
	}
	list, err := r.ExternalBrowserSessions(t.Context(), owner.ID)
	if err != nil || len(list) != 1 || list[0].State != "prepared" {
		t.Fatal(list, err)
	}
	if requests.Load() != 0 {
		t.Fatal("reads/preparation started CDP")
	}
	op, err := r.store.AdmitOperation(t.Context(), session.OperationSpec{ID: call.OperationID(), CellID: cell.ID, RequestID: call.RequestID, Capability: prepared.Capability, Resource: prepared.Resource, Arguments: prepared.Arguments})
	if err != nil {
		t.Fatal(err)
	}
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Run(t.Context(), op.ID); err == nil {
		t.Fatal("ran before dispatch")
	}
	release()
	if requests.Load() != 0 {
		t.Fatal("uncommitted operation touched Chrome")
	}
	if _, err := r.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	// The rejected attempt used its one-use lease, but did not retire the prepared resource.
	prepared, err = r.prepareBrowser(t.Context(), owner, call)
	if err != nil {
		t.Fatal(err)
	}
	release, err = prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := r.store.DispatchOperation(t.Context(), op.ID)
	if err != nil || !dispatched {
		t.Fatal(dispatched, err)
	}
	_, err = prepared.Run(t.Context(), op.ID)
	release()
	if err == nil || requests.Load() != 1 {
		t.Fatal(err, requests.Load())
	}
	if _, err := r.prepareBrowser(t.Context(), owner, call); !errors.Is(err, browser.ErrNativeStale) {
		t.Fatal("auto-reopen", err)
	}
	// Explicit reset changes generation without HTTP or retrying the failed effect.
	fresh, err := r.ChangeExternalBrowserConnection(t.Context(), owner.ID, "default", list[0].Generation, true)
	if err != nil || fresh.Generation == list[0].Generation || requests.Load() != 1 {
		t.Fatal(fresh, err, requests.Load())
	}
	next, err := r.prepareBrowser(t.Context(), owner, call)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ConfigureExternalBrowser(t.Context(), after.Revision, browserconfig.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := next.Acquire(t.Context()); !errors.Is(err, browser.ErrNativeStale) {
		t.Fatal("obsolete config acquired", err)
	}
}

func TestExternalUploadDoesNotBorrowGenericGrant(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	if err := os.WriteFile(filepath.Join(owner.WorkingDirectory, "upload.txt"), []byte("private bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := r.ExternalBrowserStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ConfigureExternalBrowser(t.Context(), status.Revision, browserconfig.Config{Mode: "headless", Executable: "/never/launched"}); err != nil {
		t.Fatal(err)
	}
	normal, err := r.prepareBrowser(t.Context(), owner, browserCall(owner, cell, "control", "run", map[string]any{"session": "default", "code": "info()"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "external-control", SessionID: owner.ID, Capability: normal.Capability, Resource: normal.Resource}); err != nil {
		t.Fatal(err)
	}
	call := browserCall(owner, cell, "upload", "run", map[string]any{"session": "default", "code": `upload("input", "upload.txt")`})
	upload, err := r.prepareBrowser(t.Context(), owner, call)
	if err != nil {
		t.Fatal(err)
	}
	if upload.Capability != "browser.external.upload" || upload.Resource == normal.Resource {
		t.Fatal(upload.Capability, upload.Resource)
	}
	op, err := r.store.AdmitOperation(t.Context(), session.OperationSpec{ID: call.OperationID(), CellID: cell.ID, RequestID: call.RequestID, Capability: upload.Capability, Resource: upload.Resource, Arguments: upload.Arguments})
	if err != nil {
		t.Fatal(err)
	}
	if op.State != session.OperationWaiting {
		raw, _ := json.Marshal(op)
		t.Fatalf("generic control granted file bytes: %s", raw)
	}
}

func TestExternalBrowserOwnedHeadlessUploadAndScreenshot(t *testing.T) {
	executable := os.Getenv("WHIP_BROWSER_NATIVE_TEST_BINARY")
	if executable == "" {
		t.Skip("owned Chromium fixture binary unavailable")
	}
	for _, driver := range []string{"rod", "chromedp"} {
		t.Run(driver, func(t *testing.T) {
			t.Setenv("WHIP_BROWSER_DRIVER", driver)
			r, owner, cell := modelHelperFixture(t, model.Scripted{})
			file := filepath.Join(owner.WorkingDirectory, "upload.txt")
			if err := os.WriteFile(file, []byte("exact private snapshot"), 0o600); err != nil {
				t.Fatal(err)
			}
			status, err := r.ExternalBrowserStatus(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ConfigureExternalBrowser(t.Context(), status.Revision, browserconfig.Config{Mode: "headless", Executable: executable}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "external-full", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			_, uploadID, err := r.tools.Call(t.Context(), browserCall(owner, cell, "native_upload", "run", map[string]any{"session": "default", "code": `js("document.body.innerHTML = '<input type=file id=file>'"); upload("#file", "upload.txt")`}))
			if err != nil {
				t.Fatal(err)
			}
			upload, err := r.Operation(t.Context(), uploadID)
			if err != nil || upload.State != session.OperationSucceeded || upload.Capability != "browser.external.upload" {
				t.Fatal(upload, err)
			}
			if err := os.WriteFile(file, []byte("changed workspace bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			value, id, err := r.tools.Call(t.Context(), browserCall(owner, cell, "native_later", "run", map[string]any{"session": "default", "code": `js("document.querySelector('#file').files[0].text()"); screenshot()`}))
			if err != nil {
				t.Fatal(err)
			}
			result, ok := value.(externalBrowserResult)
			if !ok || !strings.Contains(result.Output, "exact private snapshot") || len(result.Screenshots) != 1 {
				t.Fatalf("%T %+v", value, value)
			}
			op, err := r.Operation(t.Context(), id)
			if err != nil || op.Result == nil || len(op.Result.ContentReferences) != 1 {
				t.Fatal(op, err)
			}
			refs, err := filepath.Glob(filepath.Join(r.directory, "browser-upload-*"))
			if err != nil || len(refs) != 1 {
				t.Fatal(refs, err)
			}
			if _, _, err := r.tools.Call(t.Context(), browserCall(owner, cell, "native_detach", "detach", map[string]any{"session": "default"})); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(refs[0]); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("detached private files retained", err)
			}
		})
	}
}

func TestExternalBrowserChildDelegationDoesNotExpandUploadWorkspace(t *testing.T) {
	r, parent, cell := modelHelperFixture(t, model.Scripted{})
	childDirectory := filepath.Join(parent.WorkingDirectory, "child")
	if err := os.Mkdir(childDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(parent.WorkingDirectory, "file.txt"), filepath.Join(childDirectory, "file.txt")} {
		if err := os.WriteFile(file, []byte("owned"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	status, err := r.ExternalBrowserStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ConfigureExternalBrowser(t.Context(), status.Revision, browserconfig.Config{Mode: "headless", Executable: "/not/launched"}); err != nil {
		t.Fatal(err)
	}
	parentControl, err := r.prepareBrowser(t.Context(), parent, browserCall(parent, cell, "parent_control", "run", map[string]any{"session": "default", "code": "info()"}))
	if err != nil {
		t.Fatal(err)
	}
	parentUpload, err := r.prepareBrowser(t.Context(), parent, browserCall(parent, cell, "parent_upload", "run", map[string]any{"session": "default", "code": `upload("input", "file.txt")`}))
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []session.Grant{{ID: "parent-control", SessionID: parent.ID, Capability: parentControl.Capability, Resource: parentControl.Resource}, {ID: "parent-upload", SessionID: parent.ID, Capability: parentUpload.Capability, Resource: parentUpload.Resource}} {
		if _, err := r.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	spawned, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "native-browser", RequestID: "child"}, store.ChildRequest{ParentID: parent.ID, WorkingDirectory: childDirectory, Parts: []session.Part{{Type: "text", Text: "child"}}, GrantIDs: []session.GrantID{"parent-control", "parent-upload"}})
	if err != nil {
		t.Fatal(err)
	}
	child := *spawned.Session
	claimed, err := r.store.Claim(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := r.store.AppendMessage(t.Context(), claimed.Turn.ID, session.MessageDraft{ID: "child-browser-call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	childCell, _, err := r.store.BeginCell(t.Context(), session.CellSpec{ID: "child-browser-cell", TurnID: claimed.Turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil {
		t.Fatal(err)
	}
	normalCall := browserCall(child, childCell, "child-control", "run", map[string]any{"session": "default", "code": "info()"})
	normal, err := r.prepareBrowser(t.Context(), child, normalCall)
	if err != nil {
		t.Fatal(err)
	}
	if normal.Resource != parentControl.Resource {
		t.Fatal("exact parent resource not shared for explicit delegation")
	}
	op, err := r.store.AdmitOperation(t.Context(), session.OperationSpec{ID: normalCall.OperationID(), CellID: childCell.ID, RequestID: normalCall.RequestID, Capability: normal.Capability, Resource: normal.Resource, Arguments: normal.Arguments})
	if err != nil || op.State != session.OperationReady {
		t.Fatal(op, err)
	}
	if _, err := r.prepareBrowser(t.Context(), child, browserCall(child, childCell, "escape", "run", map[string]any{"session": "default", "code": `upload("input", "../file.txt")`})); err == nil {
		t.Fatal("parent grant expanded child workspace")
	}
	uploadCall := browserCall(child, childCell, "child-upload", "run", map[string]any{"session": "default", "code": `upload("input", "file.txt")`})
	upload, err := r.prepareBrowser(t.Context(), child, uploadCall)
	if err != nil {
		t.Fatal(err)
	}
	if upload.Resource == parentUpload.Resource {
		t.Fatal("different child cwd reused parent path authority")
	}
	op, err = r.store.AdmitOperation(t.Context(), session.OperationSpec{ID: uploadCall.OperationID(), CellID: childCell.ID, RequestID: uploadCall.RequestID, Capability: upload.Capability, Resource: upload.Resource, Arguments: upload.Arguments})
	if err != nil || op.State != session.OperationDenied {
		t.Fatal(op, err)
	}
	if _, err := r.ChangeExternalBrowserConnection(t.Context(), child.ID, "default", "any", true); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("child reset host resource", err)
	}
}

func TestExternalBrowserBothEnginesRestartKeepsValuesWithoutControl(t *testing.T) {
	executable := os.Getenv("WHIP_BROWSER_NATIVE_TEST_BINARY")
	if executable == "" {
		t.Skip("owned Chromium fixture binary unavailable")
	}
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"run": `v=browser.run(session="default",code='print("native")'); print(v["generation"])`, "read": `print(v["generation"])`}
			if engine == session.QuickJS {
				codes = map[string]string{"run": `var v=await browser.run({session:"default",code:'print("native")'}); console.log(v.generation)`, "read": `console.log(v.generation)`}
			}
			directory := t.TempDir()
			provider := cellProvider(codes)
			r := openEngineTest(t, directory, provider)
			root := createEngineSession(t, r, engine)
			status, err := r.ExternalBrowserStatus(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ConfigureExternalBrowser(t.Context(), status.Revision, browserconfig.Config{Mode: "headless", Executable: executable}); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"session": "default", "code": `print("native")`}
			prepared, err := r.prepareBrowser(t.Context(), root, browserCall(root, session.Cell{}, "capture", "run", args))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "external-grant", SessionID: root.ID, Capability: prepared.Capability, Resource: prepared.Resource}); err != nil {
				t.Fatal(err)
			}
			entries, err := r.ExternalBrowserSessions(t.Context(), root.ID)
			if err != nil || len(entries) != 1 {
				t.Fatal(entries, err)
			}
			generation := entries[0].Generation
			runCellTurn(t, r, root.ID, "run", generation+"\n")
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			restarted := openEngineTest(t, directory, provider)
			entries, err = restarted.ExternalBrowserSessions(t.Context(), root.ID)
			if err != nil || len(entries) != 0 {
				t.Fatal("restart restored external browser", entries, err)
			}
			runCellTurn(t, restarted, root.ID, "read", generation+"\n")
			fresh, err := restarted.prepareBrowser(t.Context(), root, browserCall(root, session.Cell{}, "fresh", "run", args))
			if err != nil || fresh.Resource == prepared.Resource {
				t.Fatal("old grant became fresh browser authority", fresh.Resource, err)
			}
			if _, err := restarted.ChangeExternalBrowserConnection(t.Context(), root.ID, "default", generation, true); !errors.Is(err, store.ErrConflict) {
				t.Fatal("checkpoint generation reset a new resource", err)
			}
		})
	}
}
