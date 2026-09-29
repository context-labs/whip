package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/computerconfig"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func hostTestPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func computerHelperFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "helper")
	record := binary + ".calls"

	path := filepath.Join("..", "computer", "testdata", "controlledhelper", "main.go")
	if raw, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, path).CombinedOutput(); err != nil {
		t.Fatalf("fake helper %v: %s", err, raw)
	}
	return binary, record
}

func configureComputerTest(t *testing.T, r *Runtime, binary string, allow bool) ComputerStatus {
	t.Helper()
	status, err := r.ComputerStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	settings := computerconfig.Config{Enabled: true, HelperExecutable: binary, DefaultDeny: true}
	if allow {
		settings.Allow = []string{"test app"}
	}
	status, err = r.ConfigureComputer(t.Context(), status.Revision, settings)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func computerCall(owner session.Session, cell session.Cell, id, code string) tool.Invocation {
	return tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: id, Module: "computer", Name: "run", Arguments: map[string]any{"code": code}}
}

func awaitComputerLog(t *testing.T, path, fragment string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), fragment) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fake helper did not reach", fragment)
}

func TestComputerUnlistedConsentPrecedesHelperAndCommitsImages(t *testing.T) {
	binary, record := computerHelperFixture(t)
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	configureComputerTest(t, r, binary, false)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.ComputerStatus(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status launched helper", err)
	}
	call := computerCall(owner, cell, "batch", `state("Test App"); click("Test App",0)`)
	type result struct {
		id  session.OperationID
		err error
	}
	done := make(chan result, 1)
	go func() { _, id, err := r.tools.Call(t.Context(), call); done <- result{id, err} }()
	var operation session.Operation
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := r.Operations(t.Context(), cell.TurnID, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) > 0 {
			operation = rows[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if operation.State != session.OperationWaiting || operation.Capability != "computer.run" || operation.PermissionRevision != nil {
		t.Fatal(operation)
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("helper started before consent", err)
	}
	if !bytes.Contains(operation.Arguments, []byte(`"applications":["test app"]`)) {
		t.Fatal("intent hidden", string(operation.Arguments))
	}
	if _, err := r.ResolvePermission(t.Context(), operation.ID, true); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	op, err := r.Operation(t.Context(), got.id)
	if err != nil || len(op.Result.ContentReferences) != 2 {
		t.Fatal(op, err)
	}
	if _, err := r.store.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: cell.CallID, Output: "exact text"}, nil); err != nil {
		t.Fatal(err)
	}
	parts, err := r.store.CellResultParts(t.Context(), owner.ID, cell.ID)
	if err != nil || len(parts) != 3 || parts[0].Result.Output != "exact text" {
		t.Fatal(parts, err)
	}
	before, _ := os.ReadFile(record)
	if _, _, err := r.tools.Call(t.Context(), call); err == nil {
		t.Fatal("accepted effect replayed")
	}
	after, _ := os.ReadFile(record)
	if !bytes.Equal(before, after) {
		t.Fatal("replayed helper effect")
	}
}

func TestComputerObservationSurvivesModelChangeButNotKernelOrConnection(t *testing.T) {
	binary, _ := computerHelperFixture(t)
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	status := configureComputerTest(t, r, binary, true)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	run := func(id, code string) error {
		_, _, err := r.tools.Call(t.Context(), computerCall(owner, cell, id, code))
		return err
	}
	if err := run("state", `ax("Test App")`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "changed"}}); err != nil {
		t.Fatal(err)
	}
	if err := run("click", `click("Test App",0)`); err != nil {
		t.Fatal("model edit lost observed indices", err)
	}
	r.mu.Lock()
	entry := r.kernels[owner.ID]
	r.mu.Unlock()
	r.discardKernel(owner.ID, entry)
	if err := run("lost", `click("Test App",0)`); err == nil {
		t.Fatal("kernel recreation restored observed authority")
	}
	if err := run("state-again", `ax("Test App")`); err != nil {
		t.Fatal(err)
	}
	next, err := r.ChangeComputerConnection(t.Context(), status.Control.Generation, true)
	if err != nil || next.Control.Generation == status.Control.Generation {
		t.Fatal(next, err)
	}
	if err := run("reconnected", `click("Test App",0)`); err == nil {
		t.Fatal("connection recreation restored observed authority")
	}
	if _, err := r.ChangeComputerConnection(t.Context(), status.Control.Generation, true); !errors.Is(err, store.ErrConflict) {
		t.Fatal("reconnect replay accepted", err)
	}
}

func TestComputerGrantRevocationCancelsAndJoinsBlockedBatch(t *testing.T) {
	binary, record := computerHelperFixture(t)
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	configureComputerTest(t, r, binary, true)
	call := computerCall(owner, cell, "blocked", `state("Test App"); type("Test App","block"); state("Test App")`)
	prepared, err := r.prepareComputer(t.Context(), owner, call)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "control", SessionID: owner.ID, Capability: prepared.Capability, Resource: prepared.Resource})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := r.tools.Call(t.Context(), call); done <- err }()
	awaitComputerLog(t, record, "type\n")
	if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked effect succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revocation did not join batch")
	}
	raw, _ := os.ReadFile(record)
	if strings.Count(string(raw), "state\n") != 1 {
		t.Fatal("batch suffix ran", string(raw))
	}
	rows, err := r.Operations(t.Context(), cell.TurnID, "", 100)
	if err != nil || len(rows) != 1 || rows[0].State != session.OperationUncertain || len(rows[0].Result.ContentReferences) != 1 {
		t.Fatal(rows, err)
	}
	status, err := r.ComputerStatus(t.Context())
	if err != nil || status.Control.State != "retired" {
		t.Fatal(status, err)
	}
}

func TestBothEnginesComputerImagesReachSameTurnProvider(t *testing.T) {
	binary, _ := computerHelperFixture(t)
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `computer.run(code='state("Test App")')`
			if engine == session.QuickJS {
				code = `await computer.run({code:'state("Test App")'})`
			}
			calls := 0
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				calls++
				if calls == 1 {
					raw, _ := json.Marshal(map[string]string{"code": code})
					return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "computer", Name: "execute", Arguments: raw}}}}, nil
				}
				if len(request.Contents) != 1 || len(request.Messages) == 0 || request.Messages[len(request.Messages)-1].Role != session.Tool || len(request.Messages[len(request.Messages)-1].Parts) != 2 {
					return model.Response{}, errors.New("same-turn screenshot missing")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "saw screenshot"}}}, nil
			})
			r := openEngineTest(t, t.TempDir(), provider)
			owner := createEngineSession(t, r, engine)
			configureComputerTest(t, r, binary, true)
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "image")
			finished := waitTestWithin(t, r, "image", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded || calls != 2 {
				t.Fatal(finished.Turn, calls, r.Err())
			}
		})
	}
}

func TestBothEnginesWorkerEvictionCannotRestoreComputerIndices(t *testing.T) {
	binary, record := computerHelperFixture(t)
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"observe": `computer.run(code='ax("Test App")')`, "click": `computer.run(code='click("Test App",0)')`}
			if engine == session.QuickJS {
				codes = map[string]string{"observe": `await computer.run({code:'ax("Test App")'})`, "click": `await computer.run({code:'click("Test App",0)'})`}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			owner := createEngineSession(t, r, engine)
			configureComputerTest(t, r, binary, true)
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "observe")
			waitTestWithin(t, r, "observe", terminal, 30*time.Second)
			r.mu.Lock()
			entry := r.kernels[owner.ID]
			r.mu.Unlock()
			if err := entry.kernel.Suspend(); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(record)
			submitTest(t, r, owner.ID, "click")
			finished := waitTestWithin(t, r, "click", terminal, 30*time.Second)
			rows, err := r.Operations(t.Context(), finished.Turn.ID, "", 100)
			if err != nil || len(rows) != 1 || rows[0].State != session.OperationUncertain || rows[0].Result == nil || !strings.Contains(*rows[0].Result.Failure, "requires current state") {
				t.Fatal(rows, err)
			}
			after, _ := os.ReadFile(record)
			if strings.Count(string(after), "click\n") != strings.Count(string(before), "click\n") {
				t.Fatal("evicted indices reached native helper")
			}
		})
	}
}

func TestComputerImageAllowanceStopsBeforeNinthAction(t *testing.T) {
	binary, record := computerHelperFixture(t)
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	configureComputerTest(t, r, binary, true)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	_, id, err := r.tools.Call(t.Context(), computerCall(owner, cell, "many", strings.Repeat(`state("Test App");`, 9)))
	if err == nil {
		t.Fatal("image bound silently passed")
	}
	operation, err := r.Operation(t.Context(), id)
	if err != nil || len(operation.Result.ContentReferences) != 8 || operation.State != session.OperationUncertain {
		t.Fatal(operation, err)
	}
	before, _ := os.ReadFile(record)
	if strings.Count(string(before), "state\n") != 8 {
		t.Fatal("ninth native action ran", string(before))
	}
	if _, _, err := r.tools.Call(t.Context(), computerCall(owner, cell, "full", `state("Test App")`)); err == nil {
		t.Fatal("full cell allowed effect")
	}
	after, _ := os.ReadFile(record)
	if !bytes.Equal(before, after) {
		t.Fatal("full cell contacted helper")
	}
}

func TestDirectComputerImagesDoNotCreateKernelOrConversation(t *testing.T) {
	binary, _ := computerHelperFixture(t)
	r := openTest(t, t.TempDir(), providerFunc(func(context.Context, model.Request) (model.Response, error) {
		t.Error("direct computer action called model")
		return model.Response{}, errors.New("unexpected model")
	}))
	owner := createTest(t, r)
	configureComputerTest(t, r, binary, true)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err := r.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "direct"}, owner.ID, session.HostOperation{Module: "computer", Name: "run", Arguments: json.RawMessage(`{"code":"state(\"Test App\");click(\"Test App\",0)"}`)})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitTestWithin(t, r, "direct", terminal, 30*time.Second)
	if finished.Turn.State != session.Succeeded {
		t.Fatal(finished.Turn)
	}
	operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || len(operations) != 1 || operations[0].CellID != "" || len(operations[0].Result.ContentReferences) != 2 {
		t.Fatal(operations, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 0 {
		t.Fatal("direct action made messages", history, err)
	}
	r.mu.Lock()
	kernels := len(r.kernels)
	r.mu.Unlock()
	if kernels != 0 {
		t.Fatal("direct action started kernel")
	}
}
