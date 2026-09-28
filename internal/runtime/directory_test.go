package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestBothEnginesDirectoryOperationsUseRootAndChildAuthority(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := "print(files.list(path=\".\")[\"output\"])\nprint(files.search(path=\".\",query=\"a.*b\")[\"output\"])"
			revokedCode := "files.search(path=\".\",query=\"a.*b\")"
			if engine == session.QuickJS {
				code = "print((await files.list({path:'.'})).output); print((await files.search({path:'.',query:'a.*b'})).output)"
				revokedCode = "await files.search({path:'.',query:'a.*b'})"
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"root-scan": code, "child-scan": code, "revoked": revokedCode}))
			root := createEngineSession(t, r, engine)
			if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "note.txt"), []byte("a.*b literal\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "root-scan")
			for _, capability := range []string{"files.list", "files.search"} {
				pending := awaitRuntimeFilePermission(t, r, root.ID, "root-scan", capability)
				if pending.Resource != root.WorkingDirectory || pending.GrantID != nil {
					t.Fatal("inspection ran without scoped approval", pending)
				}
				if _, err := r.ResolvePermission(t.Context(), pending.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			verify := func(key string, owner session.SessionID) {
				t.Helper()
				admitted := waitTestWithin(t, r, key, terminal, 30*time.Second)
				if admitted.Turn.State != session.Succeeded {
					t.Fatal(admitted.Turn)
				}
				operations, err := r.Operations(t.Context(), admitted.Turn.ID, "", 10)
				if err != nil || len(operations) != 2 {
					t.Fatal(operations, err)
				}
				for _, operation := range operations {
					if operation.SessionID != owner || operation.Resource != root.WorkingDirectory || operation.GrantID == nil || operation.State != session.OperationSucceeded || operation.Result == nil {
						t.Fatal(operation)
					}
					var result struct {
						Output    string `json:"output"`
						Truncated bool   `json:"truncated"`
					}
					if err := json.Unmarshal(operation.Result.Value, &result); err != nil || result.Truncated || !strings.Contains(result.Output, "note.txt") {
						t.Fatal(result, err)
					}
					if operation.Capability == "files.search" && result.Output != "note.txt:1:a.*b literal\n" {
						t.Fatal("search changed literal semantics", result.Output)
					}
				}
			}
			verify("root-scan", root.ID)
			var searchGrant session.Grant
			for _, capability := range []string{"files.list", "files.search"} {
				grant, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(capability), SessionID: root.ID, Capability: capability, Resource: root.WorkingDirectory})
				if err != nil {
					t.Fatal(err)
				}
				if capability == "files.search" {
					searchGrant = grant
				}
			}
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child-scan"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "child-scan"}}})
			if err != nil {
				t.Fatal(err)
			}
			verify("child-scan", child.Session.ID)
			if _, err := r.RevokeGrant(t.Context(), searchGrant.ID); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, child.Session.ID, "revoked")
			finished := waitTestWithin(t, r, "revoked", terminal, 30*time.Second)
			operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 10)
			if err != nil || len(operations) != 1 || operations[0].State != session.OperationDenied || operations[0].DispatchedAt != nil {
				t.Fatal("descendant reused revoked authority", operations, err)
			}
		})
	}
}
