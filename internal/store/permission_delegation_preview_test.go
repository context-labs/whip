package store

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestChildPermissionPreviewSerializesExactRevision(t *testing.T) {
	revision := session.Revision(9007199254740993)
	for _, test := range []struct {
		name     string
		revision *session.Revision
		want     string
	}{
		{name: "above JavaScript integer precision", revision: &revision, want: `"9007199254740993"`},
		{name: "no inherited policy", want: "null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			preview := ChildPreview{PermissionRevision: test.revision}
			raw, err := json.Marshal(preview)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if got := string(fields["permission_revision"]); got != test.want {
				t.Fatalf("before_spawn preview revision = %s, want %s", got, test.want)
			}
			var decoded ChildPreview
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if test.revision == nil {
				if decoded.PermissionRevision != nil {
					t.Fatal("absent policy acquired a revision during serialization")
				}
			} else if decoded.PermissionRevision == nil || *decoded.PermissionRevision != *test.revision {
				t.Fatalf("policy revision lost precision: %+v", decoded.PermissionRevision)
			}
		})
	}
}

func TestChildPermissionPreviewObservesPolicyWithoutDelegating(t *testing.T) {
	for _, test := range []struct {
		name    string
		grants  []session.GrantID
		inherit bool
	}{
		{name: "default inheritance", inherit: true},
		{name: "explicit empty selection", grants: []session.GrantID{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			root, cell := operationCell(t, s)
			policy := setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic).Policy
			request := childRequest(root.ID)
			request.GrantIDs = test.grants
			preview, err := s.PreviewChild(t.Context(), cell.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			if test.inherit {
				if preview.PermissionRevision == nil || *preview.PermissionRevision != policy.Revision {
					t.Fatalf("preview did not observe the automatic policy: %+v", preview)
				}
			} else if preview.PermissionRevision != nil {
				t.Fatalf("explicit empty grant selection previewed policy authority: %+v", preview)
			}
			if preview.WorkingDirectory != root.WorkingDirectory || len(preview.GrantIDs) != 0 {
				t.Fatalf("preview changed workspace or invented grants: %+v", preview)
			}
			if count(t, s, "sessions") != 1 || count(t, s, "receipts") != 1 ||
				count(t, s, "grants") != 0 || count(t, s, "operations") != 0 ||
				count(t, s, "child_permission_policies") != 0 {
				t.Fatal("permission preview admitted work or delegated authority")
			}
		})
	}
}

func TestChildPermissionPreviewIsReevaluatedAtCommittedSpawn(t *testing.T) {
	for _, test := range []struct {
		name     string
		reenable bool
	}{
		{name: "automatic disabled before commit"},
		{name: "new automatic revision before commit", reenable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			root, cell := operationCell(t, s)
			observed := setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic).Policy
			// Keep spawn itself authorized independently of the changing policy.
			// The only standing grant cannot authorize the child's later file read.
			grant, err := s.CreateGrant(t.Context(), session.Grant{
				ID: "spawn", SessionID: root.ID, Capability: "agents.spawn", Resource: string(root.TreeID),
			})
			if err != nil {
				t.Fatal(err)
			}
			request := childRequest(root.ID)
			preview, err := s.PreviewChild(t.Context(), cell.ID, request)
			if err != nil || preview.PermissionRevision == nil || *preview.PermissionRevision != observed.Revision {
				t.Fatalf("initial preview: %+v %v", preview, err)
			}
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			spawn := admitOperation(t, s, session.OperationSpec{
				ID: "spawn-op", CellID: cell.ID, RequestID: "spawn-op",
				Capability: grant.Capability, Resource: grant.Resource, Arguments: raw,
			})
			if spawn.State != session.OperationReady || spawn.GrantID == nil || *spawn.GrantID != grant.ID || spawn.PermissionRevision != nil {
				t.Fatalf("spawn lacks independent standing authority: %+v", spawn)
			}
			current := setModeTest(t, s, root.ID, "disable", observed.Revision, session.PermissionPrompt).Policy
			if test.reenable {
				current = setModeTest(t, s, root.ID, "reenable", current.Revision, session.PermissionAutomatic).Policy
			}
			child, err := s.SpawnChildOperation(t.Context(), spawn.ID)
			if err != nil || child.Session == nil {
				t.Fatalf("committed spawn: %+v %v", child, err)
			}
			childCell := childOperationCell(t, s, child.Session.ID)
			spec := operationSpec(childCell, "child-read")
			spec.Capability, spec.Resource = "files.read", child.Session.WorkingDirectory
			read := admitOperation(t, s, spec)
			if test.reenable {
				if read.State != session.OperationReady || read.PermissionRevision == nil ||
					*read.PermissionRevision != current.Revision || read.GrantID != nil {
					t.Fatalf("spawn reused stale preview instead of current policy: %+v", read)
				}
				if allowed, err := s.DispatchOperation(t.Context(), read.ID); err != nil || !allowed {
					t.Fatalf("current delegated policy cannot dispatch: %v %v", allowed, err)
				}
			} else if read.State != session.OperationDenied || read.PermissionRevision != nil || read.GrantID != nil {
				t.Fatalf("preview retained automatic authority after policy was disabled: %+v", read)
			}
			if *preview.PermissionRevision != observed.Revision {
				t.Fatal("later policy changes rewrote the earlier preview observation")
			}
		})
	}
}
