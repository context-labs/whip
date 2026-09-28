package runtime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func TestCreateTreeSnapshotsHostAndRequestResourceLimits(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	r.host.Resources = []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(3))}}
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	request := store.CreateTree{Engine: session.Starlark, Definition: refs[0], WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"}}, Resources: []session.ResourceLimit{{Kind: session.ResourceQueuedInputs, Limit: new(int64(2))}}}
	_, root, err := r.CreateTree(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	*r.host.Resources[0].Limit = 4
	*request.Resources[0].Limit = 5
	usage, err := r.Resources(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != len(session.ResourceKinds()) {
		t.Fatalf("missing default scopes: %+v", usage)
	}
	for _, resource := range usage {
		if resource.Limit == nil || resource.SessionID != root.ID {
			t.Fatalf("root resource is not finite and owned: %+v", resource)
		}
		if (resource.Kind == session.ResourceDescendants && *resource.Limit != 3) || (resource.Kind == session.ResourceQueuedInputs && *resource.Limit != 2) {
			t.Fatalf("resource snapshot changed: %+v", resource)
		}
	}
	request.Resources = append(request.Resources, request.Resources[0])
	if _, _, err := r.CreateTree(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("duplicate override was silently merged: %v", err)
	}
}

func TestGuestSpawnRetainsResourceIntentForAdmissionValidation(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	root := createTest(t, r)
	prepared, err := r.PrepareCoordination(t.Context(), root, tool.Invocation{Module: "agents", Name: "spawn", Arguments: map[string]any{
		"prompt": "child", "resources": []any{
			map[string]any{"kind": "descendants", "limit": "9007199254740993"},
			map[string]any{"kind": "queued_inputs", "limit": nil},
			map[string]any{"kind": "descendants", "limit": "2"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var request store.ChildRequest
	if err := json.Unmarshal(prepared.Arguments, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Resources) != 3 || *request.Resources[0].Limit != 9007199254740993 || request.Resources[1].Limit != nil || *request.Resources[2].Limit != 2 {
		t.Fatalf("spawn changed resource intent: %+v", request.Resources)
	}
}
