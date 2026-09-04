package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestCapabilityAuthorityRoutesThroughRootActor(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := store.WorkspaceRoot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := root.AdmitAgent(ctx, session.AgentAdmission{
		ParentAgentID: root.authority.AgentID, ChildAgentID: "child", Name: "child",
		Capabilities: []session.CapabilityDelegation{{
			ID: "child-read", Issuer: root.authority.Files, AgentID: "child", Operations: []string{"read"}, Scopes: []string{workspace},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	record, err := root.InspectCapability(ctx, root.authority.AgentID, "child-read")
	if err != nil || record.AgentID != "child" || record.Status != "active" {
		t.Fatalf("actor inspection = %+v, %v", record, err)
	}
	record, err = root.RevokeCapability(ctx, root.authority.AgentID, "child-read")
	if err != nil || record.Status != "revoked" || record.Generation != 2 {
		t.Fatalf("actor revocation = %+v, %v", record, err)
	}
	if err := root.AdmitAgent(ctx, session.AgentAdmission{ParentAgentID: "child", ChildAgentID: "grandchild", Name: "grandchild"}); err != nil {
		t.Fatal(err)
	}
	if _, err := root.DelegateCapability(ctx, "child", session.CapabilityDelegation{
		ID: "stale-child", Issuer: capability.Reference{ID: "child-read", Generation: 1}, AgentID: "grandchild", Operations: []string{"read"},
	}); err == nil {
		t.Fatal("actor accepted a stale capability reference")
	}
}

func TestDirectPermissionControlsStayRootScoped(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	otherRootID := createRoot(t, store)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := value.Open(otherRootID)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path:    func(json.RawMessage) (string, error) { return "permission.txt", nil },
		Handler: func(context.Context, capability.Call) (string, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	_, err = dispatcher.Dispatch(t.Context(), capability.Request{
		RootID: rootID, AgentID: root.AgentID(), CapabilityID: root.authority.Files.ID,
		CapabilityGeneration: root.authority.Files.Generation, OperationID: "direct-permission", Operation: "write",
		Arguments: json.RawMessage(`{}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
	})
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("permission admission = %v", err)
	}
	if inspected, err := root.InspectPermission(t.Context(), pending.PermissionID); err != nil || inspected.Request.OperationID != "direct-permission" {
		t.Fatalf("permission inspection = %+v, %v", inspected, err)
	}
	if _, err := other.InspectPermission(t.Context(), pending.PermissionID); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cross-root inspection = %v", err)
	}
	if _, err := other.DecidePermission(t.Context(), pending.PermissionID, capability.Decision{Allow: true}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cross-root decision = %v", err)
	}
	if _, err := root.DecidePermission(t.Context(), "missing", capability.Decision{Allow: true}); err == nil {
		t.Fatal("missing permission was decided")
	}
	if _, err := root.DecidePermission(t.Context(), pending.PermissionID, capability.Decision{PrincipalID: "tester", Reason: "not approved"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("denied permission = %v", err)
	}
}

func TestPermissionRuleResolvesCoveredPromptsAndSkipsFutureOnes(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "bash", Mutation: capability.MutationWorkspace, Permission: true,
		Handler: func(context.Context, capability.Call) (string, error) { return "ran", nil },
	}); err != nil {
		t.Fatal(err)
	}
	dispatch := func(operationID, command string) (string, error) {
		response, err := dispatcher.Dispatch(t.Context(), capability.Request{
			RootID: rootID, AgentID: root.AgentID(), CapabilityID: root.authority.Shell.ID,
			CapabilityGeneration: root.authority.Shell.Generation, WriterCapabilityID: root.authority.Files.ID,
			WriterCapabilityGeneration: root.authority.Files.Generation, OperationID: operationID, Operation: "bash",
			Arguments: json.RawMessage(`{"command":"` + command + `"}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
		})
		return response.Output, err
	}
	prompt := func(operationID, command string) string {
		t.Helper()
		_, err := dispatch(operationID, command)
		var pending *capability.PermissionPendingError
		if !errors.As(err, &pending) {
			t.Fatalf("%s admission = %v", command, err)
		}
		return pending.PermissionID
	}
	decide := func(commandID, permissionID string, decision capability.Decision) (capability.Ticket, error) {
		payload := json.RawMessage(`{"command_id":"` + commandID + `"}`)
		digest, err := requestDigest("root", rootID, "permission.decide", payload)
		if err != nil {
			t.Fatal(err)
		}
		return root.DecidePermissionCommand(t.Context(), session.CommandAdmission{
			ClientID: "human", CommandID: commandID, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "permission decision"},
		}, permissionID, decision)
	}

	first := prompt("sleep-1", "sleep 5")
	prompt("sleep-2", "sleep 10")
	prompt("sleep-3", "sleep 15")
	pending, err := store.ListPendingPermissions(t.Context(), rootID)
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending prompts = %+v, %v", pending, err)
	}
	for _, p := range pending {
		if p.Rule != "sleep" {
			t.Fatalf("prompt rule = %+v", p)
		}
	}
	if _, err := decide("bogus", first, capability.Decision{Allow: true, PrincipalID: "human", Remember: "bogus"}); err == nil {
		t.Fatal("bogus remember scope was accepted")
	}
	if ticket, err := decide("tree", first, capability.Decision{Allow: true, PrincipalID: "human", Remember: "tree"}); err != nil || ticket.LeaseID == "" {
		t.Fatalf("tree decision = %+v, %v", ticket, err)
	}
	if pending, err = store.ListPendingPermissions(t.Context(), rootID); err != nil || len(pending) != 0 {
		t.Fatalf("rule left prompts pending: %+v, %v", pending, err)
	}
	rules, err := store.ListPermissionRules(t.Context(), rootID)
	if err != nil || len(rules) != 1 || rules[0].Operation != "bash" || rules[0].Rule != "sleep" || rules[0].PrincipalID != "human" {
		t.Fatalf("tree rules = %+v, %v", rules, err)
	}
	if out, err := dispatch("sleep-4", "sleep 20"); err != nil || out != "ran" {
		t.Fatalf("covered dispatch = %q, %v", out, err)
	}

	ls := prompt("ls-1", "ls -la")
	if _, err := decide("global", ls, capability.Decision{Allow: true, PrincipalID: "human", Remember: "global"}); err != nil {
		t.Fatalf("global decision = %v", err)
	}
	cfg, err := config.Load()
	if err != nil || !slices.Equal(cfg.Permissions.Allow, []string{"bash:ls"}) || !slices.Equal(store.GlobalPermissionRules(), cfg.Permissions.Allow) {
		t.Fatalf("global allowlist = %+v / %+v, %v", cfg.Permissions.Allow, store.GlobalPermissionRules(), err)
	}
	if out, err := dispatch("ls-2", "ls"); err != nil || out != "ran" {
		t.Fatalf("globally covered dispatch = %q, %v", out, err)
	}

	listed := clientCommand(t, root, "human", "rules-1", "permission.rules", clientActionPayload{})
	if !strings.Contains(listed.Output, rules[0].ID+"  bash  sleep  (human, ") || !strings.Contains(listed.Output, "global  bash:ls") {
		t.Fatalf("permission.rules = %+v", listed)
	}
	if forgot := clientCommand(t, root, "human", "forget-1", "permission.forget", clientActionPayload{Args: rules[0].ID}); forgot.Output != "forgot rule "+rules[0].ID {
		t.Fatalf("permission.forget = %+v", forgot)
	}
	if forgot := clientCommand(t, root, "human", "forget-2", "permission.forget", clientActionPayload{Args: rules[0].ID}); forgot.Status != "failed" {
		t.Fatalf("forgetting twice = %+v", forgot)
	}
	prompt("sleep-5", "sleep 30")
}

func TestPermissionRuleInExternalModeSkipsThePrimaryPrompt(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &permissionModeRunner{fakeRunner: &fakeRunner{}, external: true}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "bash", Mutation: capability.MutationWorkspace, Permission: true,
		Handler: func(context.Context, capability.Call) (string, error) { return "ran", nil },
	}); err != nil {
		t.Fatal(err)
	}
	prompt := func(operationID, command string) string {
		t.Helper()
		_, err := dispatcher.Dispatch(t.Context(), capability.Request{
			RootID: rootID, AgentID: root.AgentID(), CapabilityID: root.authority.Shell.ID,
			CapabilityGeneration: root.authority.Shell.Generation, WriterCapabilityID: root.authority.Files.ID,
			WriterCapabilityGeneration: root.authority.Files.Generation, OperationID: operationID, Operation: "bash",
			Arguments: json.RawMessage(`{"command":"` + command + `"}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
		})
		var pending *capability.PermissionPendingError
		if !errors.As(err, &pending) {
			t.Fatalf("%s admission = %v", command, err)
		}
		return pending.PermissionID
	}
	first := prompt("sleep-1", "sleep 5")
	second := prompt("sleep-2", "sleep 10")
	payload := json.RawMessage(`{"command_id":"tree"}`)
	digest, err := requestDigest("root", rootID, "permission.decide", payload)
	if err != nil {
		t.Fatal(err)
	}
	human := capability.Decision{Allow: true, PrincipalID: "human", Remember: "tree", Reason: "go ahead"}
	ticket, err := root.DecidePermissionCommand(t.Context(), session.CommandAdmission{
		ClientID: "human", CommandID: "tree", RequestDigest: digest,
		Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "permission decision"},
	}, first, human)
	if err != nil || ticket.OperationID != "sleep-1" {
		t.Fatalf("external decision = %+v, %v", ticket, err)
	}
	// The dispatcher has not committed the primary decision yet, so the store
	// still lists it as pending; the rule must not decide it a second time.
	if pending, err := store.ListPendingPermissions(t.Context(), rootID); err != nil || len(pending) != 2 {
		t.Fatalf("pending prompts = %+v, %v", pending, err)
	}
	if len(runner.resolved) != 2 || runner.resolved[0] != (resolvedPermission{first, human}) ||
		runner.resolved[1].id != second || !runner.resolved[1].decision.Allow || runner.resolved[1].decision.Remember != "" ||
		runner.resolved[1].decision.Reason != "covered by rule bash sleep" {
		t.Fatalf("resolved prompts = %+v", runner.resolved)
	}
}
