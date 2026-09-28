package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func lastEventOfKind(t *testing.T, st *Store, rootID, kind string) LifecycleEvent {
	t.Helper()
	envelopes, _, err := st.ReplayEvents(context.Background(), rootID, 0, MaxEventReplay)
	if err != nil {
		t.Fatal(err)
	}
	for _, envelope := range slices.Backward(envelopes) {
		if envelope.Kind != kind {
			continue
		}
		var event LifecycleEvent
		if err := json.Unmarshal(envelope.Payload.Inline, &event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	t.Fatalf("no %s event", kind)
	return LifecycleEvent{}
}

func TestPermissionRulesAutoApproveMatchingPrompts(t *testing.T) {
	st, rootID, agentID := actorFailureFixture(t)
	ctx := context.Background()
	shell := func(operationID string) capability.Admission {
		return capability.Admission{Request: capability.Request{
			RootID: rootID, AgentID: agentID, CapabilityID: "shell:" + rootID, CapabilityGeneration: 1,
			OperationID: operationID, Operation: "bash", Arguments: json.RawMessage(`{"command":"sleep 5"}`),
		}, RequirePermission: true, RequestDigest: "digest-" + operationID}
	}

	// no rule: prompt, with the command and rule in the event and the pending list
	ticket, err := st.Begin(ctx, shell("prompted"))
	if err != nil || ticket.PermissionID == "" || ticket.LeaseID != "" {
		t.Fatalf("ticket=%+v err=%v", ticket, err)
	}
	if event := lastEventOfKind(t, st, rootID, "permission.pending"); event.Command != "sleep 5" || event.Rule != "sleep" || event.PermissionID != ticket.PermissionID {
		t.Fatalf("pending event=%+v", event)
	}
	pending, err := st.ListPendingPermissions(ctx, rootID)
	if err != nil || len(pending) != 1 || pending[0].ID != ticket.PermissionID || pending[0].Command != "sleep 5" || pending[0].Rule != "sleep" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	// tree rule: idempotent add, lease instead of prompt, auto_approved event
	rule, err := st.AddPermissionRule(ctx, rootID, "bash", "sleep", "human")
	if err != nil || rule.ID == "" || rule.PrincipalID != "human" {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}
	if again, err := st.AddPermissionRule(ctx, rootID, "bash", "sleep", "other"); err != nil || again.ID != rule.ID || again.PrincipalID != "human" {
		t.Fatalf("re-add=%+v err=%v", again, err)
	}
	if rules, err := st.ListPermissionRules(ctx, rootID); err != nil || len(rules) != 1 || rules[0] != rule {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	admission := shell("tree")
	ticket, err = st.Begin(ctx, admission)
	if err != nil || ticket.LeaseID == "" || ticket.PermissionID != "" {
		t.Fatalf("tree ticket=%+v err=%v", ticket, err)
	}
	event := lastEventOfKind(t, st, rootID, "permission.auto_approved")
	if event.OperationID != "tree" || event.Operation != "bash" || event.Command != "sleep 5" || event.Rule != "sleep" || event.RuleSource != "tree" || event.Status != "approved" {
		t.Fatalf("auto_approved event=%+v", event)
	}
	var payload []byte
	if err := st.db.QueryRowContext(ctx, `SELECT payload_inline FROM operations WHERE id='tree'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var stored capability.Admission
	if err := json.Unmarshal(payload, &stored); err != nil || stored.RequirePermission {
		t.Fatalf("stored admission=%+v err=%v", stored, err)
	}
	// the dispatcher finishes with the admission it proposed (RequirePermission still true)
	if err := st.Finish(ctx, capability.Completion{Admission: admission, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}); err != nil {
		t.Fatalf("finish auto-approved: %v", err)
	}
	if pending, err := st.ListPendingPermissions(ctx, rootID); err != nil || len(pending) != 1 {
		t.Fatalf("auto-approved operation left a prompt: %+v err=%v", pending, err)
	}
	// a rule covers only its own operation
	if source, err := st.PermissionRuleSource(ctx, rootID, "shell_start", []string{"sleep"}); err != nil || source != "" {
		t.Fatalf("cross-operation source=%q err=%v", source, err)
	}

	// a chain is covered only when every command in it is; substitution never is
	chain := shell("chain")
	chain.Request.Arguments = json.RawMessage(`{"command":"sleep 5 && rm -rf /"}`)
	if ticket, err := st.Begin(ctx, chain); err != nil || ticket.PermissionID == "" {
		t.Fatalf("chain ticket=%+v err=%v", ticket, err)
	}
	if event := lastEventOfKind(t, st, rootID, "permission.pending"); event.Rule != "sleep, rm" {
		t.Fatalf("chain event=%+v", event)
	}
	if _, err := st.AddPermissionRule(ctx, rootID, "bash", "rm", "human"); err != nil {
		t.Fatal(err)
	}
	chain.Request.OperationID = "chain-covered"
	if ticket, err := st.Begin(ctx, chain); err != nil || ticket.LeaseID == "" {
		t.Fatalf("covered chain ticket=%+v err=%v", ticket, err)
	}
	substitution := shell("substitution")
	substitution.Request.Arguments = json.RawMessage(`{"command":"sleep $(cat delay)"}`)
	if ticket, err := st.Begin(ctx, substitution); err != nil || ticket.PermissionID == "" {
		t.Fatalf("substitution ticket=%+v err=%v", ticket, err)
	}
	if err := st.DeletePermissionRule(ctx, rootID, mustRule(t, st, rootID, "rm").ID); err != nil {
		t.Fatal(err)
	}

	// forget: second delete errors, the next request prompts again
	if err := st.DeletePermissionRule(ctx, rootID, rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePermissionRule(ctx, rootID, rule.ID); err == nil {
		t.Fatal("deleting a missing rule should fail")
	}
	if ticket, err := st.Begin(ctx, shell("forgotten")); err != nil || ticket.PermissionID == "" {
		t.Fatalf("forgotten ticket=%+v err=%v", ticket, err)
	}

	// global allowlist
	st.SetGlobalPermissionRules([]string{"bash:sleep", "go:go test"})
	if got := st.GlobalPermissionRules(); len(got) != 2 || got[0] != "bash:sleep" {
		t.Fatalf("global rules=%v", got)
	}
	if ticket, err := st.Begin(ctx, shell("global")); err != nil || ticket.LeaseID == "" {
		t.Fatalf("global ticket=%+v err=%v", ticket, err)
	}
	if event := lastEventOfKind(t, st, rootID, "permission.auto_approved"); event.OperationID != "global" || event.RuleSource != "global" {
		t.Fatalf("global event=%+v", event)
	}
	st.SetGlobalPermissionRules(nil)
	if ticket, err := st.Begin(ctx, shell("cleared")); err != nil || ticket.PermissionID == "" {
		t.Fatalf("cleared ticket=%+v err=%v", ticket, err)
	}
}

func TestPermissionPathRuleAndSessionDelete(t *testing.T) {
	st, rootID, agentID := actorFailureFixture(t)
	ctx := context.Background()
	root, err := st.WorkspaceRoot(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.workspaces.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace.Root(), "notes.txt")
	write := func(operationID string) capability.Admission {
		return capability.Admission{Request: capability.Request{
			RootID: rootID, AgentID: agentID, CapabilityID: "files:" + rootID, CapabilityGeneration: 1,
			OperationID: operationID, Operation: "write", Arguments: json.RawMessage(`{"path":"notes.txt"}`),
		}, CanonicalPath: path, RequirePermission: true, RequestDigest: "digest-" + operationID}
	}
	if ticket, err := st.Begin(ctx, write("prompted")); err != nil || ticket.PermissionID == "" {
		t.Fatalf("ticket=%+v err=%v", ticket, err)
	}
	if event := lastEventOfKind(t, st, rootID, "permission.pending"); event.Command != path || event.Rule != path {
		t.Fatalf("pending event=%+v", event)
	}
	if _, err := st.AddPermissionRule(ctx, rootID, "write", path, "human"); err != nil {
		t.Fatal(err)
	}
	if ticket, err := st.Begin(ctx, write("allowed")); err != nil || ticket.LeaseID == "" {
		t.Fatalf("allowed ticket=%+v err=%v", ticket, err)
	}
	if event := lastEventOfKind(t, st, rootID, "permission.auto_approved"); event.CanonicalPath != path || event.RuleSource != "tree" {
		t.Fatalf("auto_approved event=%+v", event)
	}
	// a rule on a different path does not cover this one
	if _, err := st.AddPermissionRule(ctx, rootID, "write", filepath.Join(workspace.Root(), "other.txt"), "human"); err != nil {
		t.Fatal(err)
	}
	other := write("other")
	other.CanonicalPath = filepath.Join(workspace.Root(), "third.txt")
	if ticket, err := st.Begin(ctx, other); err != nil || ticket.PermissionID == "" {
		t.Fatalf("other ticket=%+v err=%v", ticket, err)
	}

	if err := st.DeleteSession(ctx, rootID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM permission_rules WHERE root_id=?`, rootID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rules after delete=%d err=%v", n, err)
	}
}

func mustRule(t *testing.T, st *Store, rootID, rule string) PermissionRule {
	t.Helper()
	rules, err := st.ListPermissionRules(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rules {
		if row.Rule == rule {
			return row
		}
	}
	t.Fatalf("no rule %q in %+v", rule, rules)
	return PermissionRule{}
}
