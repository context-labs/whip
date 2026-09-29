package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestBothEnginesExistingChildrenFollowPermissionModeChanges(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `print(files.read(path="proof.txt")["output"])`
			if engine == session.QuickJS {
				code = `console.log((await files.read({path:"proof.txt"})).output);`
			}
			codes := map[string]string{}
			for step := range 4 {
				for _, name := range []string{"inheriting", "restricted"} {
					codes[fmt.Sprintf("%s-%d", name, step)] = code
				}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			root := createEngineSession(t, r, engine)
			if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "proof.txt"), []byte("mode propagation proof"), 0o600); err != nil {
				t.Fatal(err)
			}
			children := map[string]session.Session{}
			for _, name := range []string{"inheriting", "restricted"} {
				key := name + "-0"
				request := store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: key}}}
				if name == "restricted" {
					request.GrantIDs = []session.GrantID{}
				}
				admission, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, request)
				if err != nil || admission.Session == nil {
					t.Fatal(admission, err)
				}
				children[name] = *admission.Session
			}
			for step, mode := range []session.PermissionMode{session.PermissionPrompt, session.PermissionAutomatic, session.PermissionPrompt, session.PermissionAutomatic} {
				if step != 0 {
					setRuntimeMode(t, r, root.ID, fmt.Sprintf("mode-%d", step), session.Revision(step), mode)
				}
				for _, name := range []string{"inheriting", "restricted"} {
					child, key := children[name], fmt.Sprintf("%s-%d", name, step)
					if step != 0 {
						submitTest(t, r, child.ID, key)
					}
					finished := waitTestWithin(t, r, key, terminal, 30*time.Second)
					operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 100)
					if err != nil || len(operations) != 1 {
						t.Fatal(key, operations, err)
					}
					op := operations[0]
					if name == "inheriting" && mode == session.PermissionAutomatic {
						if op.State != session.OperationSucceeded || op.PermissionRevision == nil || *op.PermissionRevision != session.Revision(step+1) || op.GrantID != nil {
							t.Fatal("existing child missed current policy", key, op)
						}
					} else if op.State != session.OperationDenied || op.DispatchedAt != nil || op.PermissionRevision != nil || op.GrantID != nil {
						t.Fatal("disabled or restricted child executed", key, op)
					}
				}
			}
			for _, child := range children {
				grants, err := r.Grants(t.Context(), child.ID, "", 100)
				if err != nil || len(grants) != 0 {
					t.Fatal("policy propagation fabricated grants", grants, err)
				}
			}
		})
	}
}
