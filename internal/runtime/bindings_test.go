package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func bindingCellFailure(t *testing.T, r *Runtime, id session.SessionID, key, want string) {
	t.Helper()
	submitTest(t, r, id, key)
	admission := waitTestWithin(t, r, key, terminal, 30*time.Second)
	if admission.Turn.State != session.Succeeded {
		t.Fatal("model could not observe host rejection", admission.Turn)
	}
	cell, err := r.store.LatestCell(t.Context(), id)
	if err != nil || cell.State != session.CellFailed {
		t.Fatal("host rejection missing", cell, err)
	}
	history, err := r.History(t.Context(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(history[len(history)-1].Parts[0].Text, want) {
		t.Fatal("wrong host rejection", history[len(history)-1].Parts[0].Text)
	}
	operations, err := r.store.Operations(t.Context(), admission.Turn.ID, "", 100)
	if err != nil || len(operations) != 0 {
		t.Fatal("rejected binding admitted effect", operations, err)
	}
}

func TestBothEnginesBindingCeilingCapturedTurnAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{
				"setup":            "x=41\nreader=files.read\nlookup=tools.lookup\nprint(x)",
				"model":            "x+=1\nprint(x)",
				"captured":         "reader(path=\"note.txt\")\nprint(x)",
				"disabled":         "reader(path=\"note.txt\")",
				"disabled-tool":    "lookup(id=\"item\")",
				"restart":          "print(x)",
				"restart-disabled": "files.read(path=\"note.txt\")",
				"unavailable":      "tools.lookup(id=\"item\")",
			}
			if engine == session.QuickJS {
				codes = map[string]string{"setup": "var x=41; var reader=files.read; var lookup=tools.lookup; console.log(x)", "model": "x+=1; console.log(x)", "captured": "await reader({path:'note.txt'}); console.log(x)", "disabled": "await reader({path:'note.txt'})", "disabled-tool": "await lookup({id:'item'})", "restart": "console.log(x)", "restart-disabled": "await files.read({path:'note.txt'})", "unavailable": "await tools.lookup({id:'item'})"}
			}
			entered, release := make(chan model.Request, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			base := cellProvider(codes)
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.User && last.Parts[0].Text == "captured" {
					entered <- request
					select {
					case <-release:
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					}
				}
				return base(ctx, request)
			})
			directory := t.TempDir()
			r := openEngineTest(t, directory, provider)
			t.Cleanup(unblock)
			definition, err := r.RegisterDefinition(t.Context(), session.DefinitionDocument{ID: "bound", Name: "Bound", Defaults: session.ConfigPatch{Modules: []string{"files"}, Tools: map[string]session.ToolDeclaration{"lookup": {Description: "Look up one item", InputSchema: json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}},"additionalProperties":false}`)}}}})
			if err != nil {
				t.Fatal(err)
			}
			_, root, err := r.CreateTree(t.Context(), store.CreateTree{Engine: engine, Definition: definition.Ref, WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "first"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "note.txt"), []byte("readable"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory}); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "setup", "41\n")
			root, err = r.UpdateConfiguration(t.Context(), root.ID, 1, session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "changed"}})
			if err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "model", "42\n")

			submitTest(t, r, root.ID, "captured")
			var request model.Request
			select {
			case request = <-entered:
			case <-time.After(30 * time.Second):
				t.Fatal("captured turn did not reach provider")
			}
			if !strings.Contains(request.Instructions, "Available workspace operations") || !strings.Contains(request.Instructions, "Custom tool tools.lookup") || strings.Contains(request.Instructions, "Stateless model helpers") || strings.Contains(request.Instructions, "Spawn children") {
				t.Fatal("instructions do not reflect captured bindings", request.Instructions)
			}
			root, err = r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Modules: []string{}, Tools: map[string]session.ToolDeclaration{}})
			if err != nil {
				t.Fatal(err)
			}
			unblock()
			admission := waitTestWithin(t, r, "captured", terminal, 30*time.Second)
			if admission.Turn.State != session.Succeeded {
				t.Fatal("capture failed", admission.Turn)
			}
			operations, err := r.store.Operations(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].State != session.OperationSucceeded {
				t.Fatal("active captured binding was removed", operations, err)
			}
			bindingCellFailure(t, r, root.ID, "disabled", "host module is not enabled for this turn")
			bindingCellFailure(t, r, root.ID, "disabled-tool", "custom tool is not enabled for this turn")
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openEngineTest(t, directory, provider)
			runCellTurn(t, reopened, root.ID, "restart", "42\n")
			bindingCellFailure(t, reopened, root.ID, "restart-disabled", "host module is not enabled for this turn")
			if _, err := reopened.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Modules: []string{"models"}}); err == nil {
				t.Fatal("restart expanded immutable ceiling")
			}
		})
	}
}

func TestBindingInstructionsReadCapturedConfigurationAfterEdit(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	root := createTest(t, r)
	submitTest(t, r, root.ID, "captured-guide")
	claim, err := r.store.Claim(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Modules: []string{}}); err != nil {
		t.Fatal(err)
	}
	text, err := r.Instructions(t.Context(), claim.Turn, root.Config.Instructions)
	if err != nil || !strings.Contains(text, "Available workspace operations") {
		t.Fatal("guide used mutable configuration", text, err)
	}
}
