package runtime

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/session"
)

func TestBothEnginesPermissionModeLanguageServerLifetimeAndRetry(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `print(files.write(path="main.go",content="package main"))`
			if engine == session.QuickJS {
				code = `print(await files.write({path:'main.go',content:'package main'}))`
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"first": code, "recreated": code}))
			setHostForTest(t, r, func(host *config.Host) {
				host.LSP = map[string]lsp.Config{
					"fixture": {Command: []string{os.Args[0], "-test.run=^TestLanguageServerFixture$"}, Extensions: []string{".go"}, RootMarkers: []string{"go.mod"}, Env: map[string]string{"WHIP_LSP_FIXTURE": "1"}},
					"gopls":   {Enabled: new(false)},
				}
			})
			root := createEngineSession(t, r, engine)
			initialGeneration := r.languageServers.Generation()
			enable := setRuntimeMode(t, r, root.ID, "enable", 1, session.PermissionAutomatic)
			if r.languageServers.Generation() != initialGeneration+1 {
				t.Fatal("new policy did not retire prior lifetime")
			}
			run := func(key string, revision session.Revision) {
				t.Helper()
				submitTest(t, r, root.ID, key)
				finished := waitTestWithin(t, r, key, terminal, 30*time.Second)
				if finished.Turn.State != session.Succeeded {
					t.Fatal("automatic file turn failed", finished.Turn)
				}
				operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 10)
				if err != nil || len(operations) != 2 {
					t.Fatal("optional diagnostics not admitted", operations, err)
				}
				diagnostics := false
				for _, operation := range operations {
					if operation.PermissionRevision == nil || *operation.PermissionRevision != revision || operation.GrantID != nil || operation.State != session.OperationSucceeded {
						t.Fatal("automatic operation lost exact authority", operation)
					}
					if operation.Capability == "lsp.diagnostics" {
						diagnostics = strings.Contains(string(operation.Result.Value), "fixture diagnostic")
					}
				}
				if !diagnostics {
					t.Fatal("server did not return real fixture diagnostics")
				}
			}
			status := func(want string) {
				t.Helper()
				values, err := r.LSPStatus(t.Context(), root.ID)
				if err != nil || len(values) != 1 || values[0].State != want {
					t.Fatal("unexpected retained server lifetime", values, err)
				}
			}
			run("first", 2)
			status("connected")
			generation := r.languageServers.Generation()
			same := setRuntimeMode(t, r, root.ID, "same", 2, session.PermissionAutomatic)
			if same.Policy != enable.Policy || r.languageServers.Generation() != generation {
				t.Fatal("same-value edit retired retained server")
			}
			status("connected")
			if retry, err := r.SetPermissionMode(t.Context(), enable.PermissionModeRequest); err != nil || retry != enable || r.languageServers.Generation() != generation {
				t.Fatal("exact retry retired retained server", retry, err)
			}
			status("connected")
			disable := setRuntimeMode(t, r, root.ID, "disable", 2, session.PermissionPrompt)
			if r.languageServers.Generation() != generation+1 {
				t.Fatal("actual downgrade did not invalidate pending lifetime")
			}
			status("not started")
			setRuntimeMode(t, r, root.ID, "reenable", 3, session.PermissionAutomatic)
			run("recreated", 4)
			status("connected")
			generation = r.languageServers.Generation()
			for _, old := range []session.PermissionModeEdit{enable, same, disable} {
				if retry, err := r.SetPermissionMode(t.Context(), old.PermissionModeRequest); err != nil || retry != old || r.languageServers.Generation() != generation {
					t.Fatal("historical receipt retired a new server", retry, err)
				}
				status("connected")
			}
		})
	}
}
