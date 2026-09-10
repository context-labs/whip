package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func TestSessionFilesystemModeSurvivesRestartAndNavigation(t *testing.T) {
	base := t.TempDir()
	project, sibling := filepath.Join(base, "project"), filepath.Join(base, "sibling")
	for _, path := range []string{project, sibling} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(base, "sessions.db")
	store, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	id, err := store.Create(SessionKindAgent, project, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(sibling)
	if err != nil {
		t.Fatal(err)
	}
	inside, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	check := func(path string, allowed bool) {
		t.Helper()
		err := store.AuthorizeCapability(context.Background(), id, id, authority.Files, "read", path)
		if allowed && err != nil || !allowed && !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("authorize %s allowed=%v: %v", path, allowed, err)
		}
	}
	check(outside, false)
	if err := store.SetPermissionMode(t.Context(), id, PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	check(outside, true)
	if err := store.SetWorkingDirectory(id, sibling); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	authority, err = store.EnsureAuthority(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	check(outside, true)
	if err := store.SetPermissionMode(t.Context(), id, PermissionModePrompt); err != nil {
		t.Fatal(err)
	}
	check(outside, false)
	check(inside, true)
}

func TestSessionFilesystemReopenDoesNotReissueMissingAuthority(t *testing.T) {
	store, rootID, _ := newSwarmFixture(t)
	if err := store.SetPermissionMode(t.Context(), rootID, PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM capabilities WHERE id=?`, "files:"+rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), rootID); err == nil {
		t.Fatal("reissued missing Full Access authority")
	}
	var grants int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM capabilities WHERE id=?`, "files:"+rootID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("grants=%d error=%v", grants, err)
	}
}

func TestSessionFilesystemInheritanceAndExplicitCeilings(t *testing.T) {
	store, rootID, rootAgent := newSwarmFixture(t)
	root, err := store.WorkspaceRoot(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootRef := capability.Reference{ID: "files:" + rootID, Generation: 1}
	inherit := func(parent, child string, issuer capability.Reference, scopes []string) capability.Reference {
		t.Helper()
		ref := capability.Reference{ID: "files:" + child, Generation: 1}
		_, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: rootID, ParentAgentID: parent, ChildAgentID: child, Name: child,
			Capabilities: []CapabilityDelegation{{ID: ref.ID, AgentID: child, Issuer: issuer,
				Operations: []string{"read", "write", "workspace.write"}, InheritScope: scopes == nil, Scopes: scopes}}})
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	child := inherit(rootAgent, "inherited", rootRef, nil)
	grandchild := inherit("inherited", "grandchild", child, nil)
	explicit := inherit(rootAgent, "explicit", rootRef, []string{root})
	narrowGrandchild := inherit("explicit", "narrow-grandchild", explicit, nil)
	check := func(agent string, ref capability.Reference, path string, allowed bool) {
		t.Helper()
		err := store.AuthorizeCapability(t.Context(), rootID, agent, ref, "read", path)
		if allowed && err != nil || !allowed && !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("agent %s allowed=%v: %v", agent, allowed, err)
		}
	}
	check("grandchild", grandchild, outside, false)
	if err := store.SetPermissionMode(t.Context(), rootID, PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	check("inherited", child, outside, true)
	check("grandchild", grandchild, outside, true)
	check("explicit", explicit, root, true)
	check("explicit", explicit, outside, false)
	check("narrow-grandchild", narrowGrandchild, outside, false)
	outsideChild := inherit(rootAgent, "outside", rootRef, []string{outside})
	check("outside", outsideChild, outside, true)
	if err := store.SetPermissionMode(t.Context(), rootID, PermissionModePrompt); err != nil {
		t.Fatal(err)
	}
	check("outside", outsideChild, outside, false)
	check("grandchild", grandchild, root, true)
	if err := store.RevokeCapability(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	check("grandchild", grandchild, root, false)
	check("grandchild", grandchild, "", false)
}

func TestSessionFilesystemModeChangeInvalidatesPendingApproval(t *testing.T) {
	store, rootID, _ := newSwarmFixture(t)
	if err := store.SetPermissionMode(t.Context(), rootID, PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "output")
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	err := dispatcher.Register(capability.Registration{Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path: func(json.RawMessage) (string, error) { return target, nil },
		Handler: func(context.Context, capability.Call) (string, error) {
			t.Fatal("stale approval executed")
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = dispatcher.Dispatch(t.Context(), capability.Request{RootID: rootID, AgentID: rootID, CapabilityID: "files:" + rootID,
		CapabilityGeneration: 1, Operation: "write", OperationID: "pending", TraceID: "trace"})
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("pending: %v", err)
	}
	for _, mode := range []string{PermissionModePrompt, PermissionModeAutomatic} {
		if err := store.SetPermissionMode(t.Context(), rootID, mode); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := dispatcher.Decide(t.Context(), pending.PermissionID, capability.Decision{Allow: true, PrincipalID: "human"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("stale decision: %v", err)
	}
	var status string
	if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM operations WHERE id='pending'`).Scan(&status); err != nil || status != "denied" {
		t.Fatalf("pending operation status=%s error=%v", status, err)
	}
}

func TestSessionFilesystemShellRequiresEffectiveWriter(t *testing.T) {
	for _, test := range []struct {
		name                                    string
		automatic, inherit, outsideCWD, allowed bool
	}{
		{name: "full inherited outside cwd", automatic: true, inherit: true, outsideCWD: true, allowed: true},
		{name: "full explicit project", automatic: true, allowed: false},
		{name: "ask project writer", allowed: true},
		{name: "ask outside cwd", inherit: true, outsideCWD: true, allowed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, rootID, rootAgent := newSwarmFixture(t)
			root, err := store.WorkspaceRoot(t.Context(), rootID)
			if err != nil {
				t.Fatal(err)
			}
			if test.automatic {
				if err := store.SetPermissionMode(t.Context(), rootID, PermissionModeAutomatic); err != nil {
					t.Fatal(err)
				}
			}
			var paths []string
			if !test.inherit {
				paths = []string{root}
			}
			_, err = store.AdmitAgent(t.Context(), AgentAdmission{RootID: rootID, ParentAgentID: rootAgent, ChildAgentID: "child", Name: "child", Capabilities: []CapabilityDelegation{
				{ID: "child-files", AgentID: "child", Issuer: capability.Reference{ID: "files:" + rootID, Generation: 1}, Operations: []string{"read", "workspace.write"}, Scopes: paths, InheritScope: test.inherit},
				{ID: "child-shell", AgentID: "child", Issuer: capability.Reference{ID: "shell:" + rootID, Generation: 1}, Operations: []string{"bash"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
			if err := dispatcher.Register(capability.Registration{Operation: "bash", Mutation: capability.MutationWorkspace,
				Handler: func(context.Context, capability.Call) (string, error) { return "ran", nil }}); err != nil {
				t.Fatal(err)
			}
			cwd := root
			if test.outsideCWD {
				cwd = t.TempDir()
			}
			result, err := dispatcher.Dispatch(t.Context(), capability.Request{RootID: rootID, AgentID: "child", CapabilityID: "child-shell", CapabilityGeneration: 1,
				WriterCapabilityID: "child-files", WriterCapabilityGeneration: 1, Operation: "bash", OperationID: "shell", TraceID: "trace", WorkingDirectory: cwd})
			if test.allowed && (err != nil || result.Output != "ran") || !test.allowed && !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("allowed=%v result=%+v error=%v", test.allowed, result, err)
			}
		})
	}
}
