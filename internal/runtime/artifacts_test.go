package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func TestBothEnginesArtifactPublicationNeedsConsentAndRetainsOwnedBytes(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `notice=permissions.request()
item=artifacts.put(text="hello🙂", source="fixture")
info=artifacts.inspect(id=item["id"])
page=artifacts.read(id=item["id"])
print(notice["status"],info["bytes"],page["data"])`
			if engine == session.QuickJS {
				code = `const notice=await permissions.request(); const item=await artifacts.put({text:"hello🙂",source:"fixture"}); const info=await artifacts.inspect({id:item.id}); const page=await artifacts.read({id:item.id}); console.log(notice.status,info.bytes,page.data)`
			}
			directory := t.TempDir()
			r := openEngineTest(t, directory, cellProvider(map[string]string{"artifact": code}))
			owner := createEngineSession(t, r, engine)
			for _, capability := range []string{"artifacts.inspect", "artifacts.read"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(capability), SessionID: owner.ID, Capability: capability, Resource: string(owner.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			submitTest(t, r, owner.ID, "artifact")
			operation := awaitRuntimeFilePermission(t, r, owner.ID, "artifact", "artifacts.put")
			id := "artifact_" + string(operation.ID)
			if _, err := r.store.ContentReference(t.Context(), owner.ID, id); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("artifact published before consent", err)
			}
			if _, err := r.ResolvePermission(t.Context(), operation.ID, true); err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "artifact", terminal, 20*time.Second)
			if finished.Turn.State != session.Succeeded {
				t.Fatal("artifact cell failed", finished.Turn)
			}
			operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 100)
			if err != nil || len(operations) != 4 {
				t.Fatal(operations, err)
			}
			for _, op := range operations {
				if op.State != session.OperationSucceeded || op.Result == nil {
					t.Fatal("missing operation evidence", op)
				}
				if op.Capability == "permissions.inspect" && (op.GrantID != nil || op.PermissionRevision != nil || !strings.Contains(string(op.Result.Value), "invoke_operation")) {
					t.Fatal("informational request created authority", op)
				}
			}
			stranger := createEngineSession(t, r, engine)
			prepared, err := r.prepareArtifact(stranger, tool.Invocation{Name: "inspect", Arguments: map[string]any{"id": id}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepared.Run(t.Context(), "foreign-inspection"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("foreign metadata escaped", err)
			}
			permission, err := r.preparePermissionInspection(owner, tool.Invocation{Name: "status", Arguments: map[string]any{"id": string(operation.ID)}})
			if err != nil {
				t.Fatal(err)
			}
			value, err := permission.Run(t.Context(), "permission-read")
			raw, marshalErr := json.Marshal(value)
			if err != nil || marshalErr != nil || !strings.Contains(string(raw), `"state":"approved"`) {
				t.Fatal("permission decision unavailable", string(raw), err, marshalErr)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = openTest(t, directory, model.Scripted{})
			reference, data, err := r.ReadContent(t.Context(), owner.ID, id, session.MaxContentBytes)
			if err != nil || string(data) != "hello🙂" || reference.Size != 9 {
				t.Fatal("artifact bytes did not survive restart", reference, string(data), err)
			}
		})
	}
}

func TestArtifactAndPermissionInspectionRejectInvalidArguments(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	for _, args := range []map[string]any{{}, {"text": strings.Repeat("x", (128<<10)+1)}, {"text": "ok", "source": strings.Repeat("s", 257)}, {"text": "ok", "owner": "foreign"}} {
		if _, err := r.prepareArtifact(owner, tool.Invocation{Name: "put", Arguments: args}); err == nil {
			t.Fatal("invalid artifact accepted")
		}
	}
	for _, call := range []tool.Invocation{{Name: "request", Arguments: map[string]any{"id": "foreign"}}, {Name: "status"}, {Name: "approve"}, {Name: "request", Arguments: map[string]any{"approved": true}}} {
		if _, err := r.preparePermissionInspection(owner, call); err == nil {
			t.Fatal("invalid permission inspection accepted", call)
		}
	}
}
