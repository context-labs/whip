package session

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func testBrowserScope(attachment string) capability.BrowserScope {
	return capability.BrowserScope{
		ProviderID: "desktop", ProviderEpoch: "epoch-1", TabID: "tab-1", TabGeneration: "tab-generation-1",
		ProfileID: "profile-1", AttachmentID: attachment, AttachmentGeneration: "attachment-generation-1",
		Rights: []string{"create", "control", "route"},
		Preview: &capability.BrowserPreviewScope{
			HostID: "saved-host", HostIdentity: "verified-runtime", ConnectionGeneration: "connection-1",
			EnvironmentID: "project-environment", Loopback: "127.0.0.1", Ports: []int{3000, 8080},
		},
	}
}

func issueTestBrowser(t *testing.T, store *Store, root, agent, issuer string, scope capability.BrowserScope, parent capability.Reference) capability.Reference {
	t.Helper()
	ref, err := store.IssueBrowserCapability(t.Context(), root, agent, issuer, scope, parent)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func browserTestAdmission(t *testing.T, root, agent, operation string, ref capability.Reference, scope capability.BrowserScope) capability.Admission {
	t.Helper()
	grant := ref
	if operation == "browser.open" || operation == "browser.attach" {
		grant = capability.Reference{}
	}
	raw, err := json.Marshal(capability.BrowserCall{Grant: grant, Scope: scope, Arguments: json.RawMessage(`{"port":9000}`)})
	if err != nil {
		t.Fatal(err)
	}
	return capability.Admission{
		Request: capability.Request{
			RootID: root, AgentID: agent, CapabilityID: ref.ID, CapabilityGeneration: ref.Generation,
			OperationID: operation + ":" + agent, Operation: operation, Arguments: raw,
			Reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}},
		},
		Mutation: capability.MutationNone, RequestDigest: operation + ":" + agent,
	}
}

func TestBrowserExactScopeAndOwnership(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	admitTestChild(t, store, root, root, "child")
	scope := testBrowserScope("root-attachment")
	ref := issueTestBrowser(t, store, root, root, "", scope, capability.Reference{})
	if err := store.AuthorizeBrowser(t.Context(), root, root, ref, "browser.run", scope); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeBrowser(t.Context(), root, "child", ref, "browser.run", scope); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("copied attachment authorized: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*capability.BrowserScope)
	}{
		{"provider", func(s *capability.BrowserScope) { s.ProviderID = "other" }},
		{"epoch", func(s *capability.BrowserScope) { s.ProviderEpoch = "reconnected" }},
		{"tab", func(s *capability.BrowserScope) { s.TabID = "other" }},
		{"tab generation", func(s *capability.BrowserScope) { s.TabGeneration = "new" }},
		{"profile", func(s *capability.BrowserScope) { s.ProfileID = "other" }},
		{"attachment", func(s *capability.BrowserScope) { s.AttachmentID = "other" }},
		{"attachment generation", func(s *capability.BrowserScope) { s.AttachmentGeneration = "new" }},
		{"host", func(s *capability.BrowserScope) { s.Preview.HostID = "other" }},
		{"host identity", func(s *capability.BrowserScope) { s.Preview.HostIdentity = "other-runtime" }},
		{"connection", func(s *capability.BrowserScope) { s.Preview.ConnectionGeneration = "new" }},
		{"environment", func(s *capability.BrowserScope) { s.Preview.EnvironmentID = "other" }},
		{"loopback", func(s *capability.BrowserScope) { s.Preview.Loopback = "::1" }},
		{"ports widened", func(s *capability.BrowserScope) { s.Preview.Ports = []int{3000, 8080, 9000} }},
		{"ports narrowed", func(s *capability.BrowserScope) { s.Preview.Ports = []int{3000} }},
		{"rights changed", func(s *capability.BrowserScope) { s.Rights = []string{"control"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := testBrowserScope(scope.AttachmentID)
			tc.mutate(&changed)
			if err := store.AuthorizeBrowser(t.Context(), root, root, ref, "browser.run", changed); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("mismatched scope authorized: %v", err)
			}
		})
	}
	ref.Generation++
	if err := store.AuthorizeBrowser(t.Context(), root, root, ref, "browser.run", scope); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("stale reference authorized: %v", err)
	}
}

func TestBrowserDelegationTransferAndCascadingRevoke(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	admitTestChild(t, store, root, root, "child")
	admitTestChild(t, store, root, "child", "grandchild")
	parentScope := testBrowserScope("parent")
	parent := issueTestBrowser(t, store, root, root, "", parentScope, capability.Reference{})
	childScope := testBrowserScope("child")
	childScope.Rights = []string{"control", "route"}
	childScope.Preview.Ports = []int{3000}
	child := issueTestBrowser(t, store, root, "child", root, childScope, parent)
	if err := store.AuthorizeBrowser(t.Context(), root, "child", child, "browser.run", childScope); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("tentative child active before handoff: %v", err)
	}
	if err := store.SetBrowserDelegationOnly(t.Context(), root, root, parent, true); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"browser.run", "browser.allow_preview_port"} {
		if err := store.AuthorizeBrowser(t.Context(), root, root, parent, operation, parentScope); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("delegation-only parent %s authorized: %v", operation, err)
		}
	}
	if err := store.AuthorizeBrowser(t.Context(), root, root, parent, "browser.detach", parentScope); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeBrowser(t.Context(), root, "child", child, "browser.run", childScope); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBrowserDelegationOnly(t.Context(), root, root, parent, false); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("two controllers restored: %v", err)
	}
	grandScope := childScope
	grandScope.AttachmentID = "grandchild"
	grandchild := issueTestBrowser(t, store, root, "grandchild", "child", grandScope, child)
	if err := store.SetBrowserDelegationOnly(t.Context(), root, "child", child, true); err != nil {
		t.Fatal(err)
	}
	admission := browserTestAdmission(t, root, "grandchild", "browser.allow_preview_port", grandchild, grandScope)
	admission.RequirePermission = true
	ticket, err := store.Begin(t.Context(), admission)
	if err != nil || ticket.PermissionID == "" {
		t.Fatalf("permission ticket=%+v err=%v", ticket, err)
	}
	if _, err := store.RevokeCapabilityFor(t.Context(), root, root, parent.ID); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		agent string
		ref   capability.Reference
		scope capability.BrowserScope
	}{
		{"child", child, childScope}, {"grandchild", grandchild, grandScope},
	} {
		if err := store.AuthorizeBrowser(t.Context(), root, pair.agent, pair.ref, "browser.run", pair.scope); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("descendant survived revocation: %v", err)
		}
		record, err := store.InspectCapability(t.Context(), root, root, pair.ref.ID)
		if err != nil || record.Status != "revoked" {
			t.Fatalf("record=%+v err=%v", record, err)
		}
	}
	if pending, err := store.ListPendingPermissions(t.Context(), root); err != nil || len(pending) != 0 {
		t.Fatalf("descendant prompt survived revoke: %+v %v", pending, err)
	}
}

func TestBrowserDelegationRejectsWideningAndGenericBypass(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	admitTestChild(t, store, root, root, "child")
	parentScope := testBrowserScope("parent")
	parentScope.Rights = []string{"control"}
	parent := issueTestBrowser(t, store, root, root, "", parentScope, capability.Reference{})
	for _, tc := range []struct {
		name   string
		mutate func(*capability.BrowserScope)
	}{
		{"right", func(s *capability.BrowserScope) { s.Rights = []string{"control", "route"} }},
		{"port", func(s *capability.BrowserScope) { s.Preview.Ports = []int{3000, 8080, 9000} }},
		{"epoch", func(s *capability.BrowserScope) { s.ProviderEpoch = "other" }},
		{"same attachment", func(s *capability.BrowserScope) { s.AttachmentID = "parent" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := testBrowserScope("child")
			scope.Rights = []string{"control"}
			tc.mutate(&scope)
			if _, err := store.IssueBrowserCapability(t.Context(), root, "child", root, scope, parent); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("widening permitted: %v", err)
			}
		})
	}
	if _, err := store.DelegateCapability(t.Context(), root, root, CapabilityDelegation{
		ID: "generic-bypass", Issuer: parent, AgentID: "child", Operations: []string{"browser.run"},
	}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("generic delegation dropped browser scope: %v", err)
	}
	if err := store.AuthorizeBrowser(t.Context(), root, root, parent, "browser.allow_preview_port", parentScope); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("missing route right accepted: %v", err)
	}
}

func TestBrowserAdmissionAndOnceOnlyConsent(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	scope := testBrowserScope("attachment")
	ref := issueTestBrowser(t, store, root, root, "", scope, capability.Reference{})
	for _, operation := range []string{"browser.open", "browser.attach", "browser.allow_preview_port"} {
		t.Run(operation, func(t *testing.T) {
			admissionRef := ref
			if operation != "browser.allow_preview_port" {
				admissionRef = capability.Reference{ID: "shell:" + root, Generation: 1}
			}
			admission := browserTestAdmission(t, root, root, operation, admissionRef, scope)
			if operation == "browser.open" {
				// The broker resolves the initial URL port into the proposed
				// scope, even though the prior offer contains only 3000/8080.
				proposed := scope
				preview := *scope.Preview
				preview.Ports = []int{3000, 5173, 8080}
				proposed.Preview = &preview
				raw, err := json.Marshal(capability.BrowserCall{Scope: proposed, Arguments: json.RawMessage(`{"url":"http://127.0.0.1:5173/"}`)})
				if err != nil {
					t.Fatal(err)
				}
				admission.Request.Arguments = raw
			}
			admission.RequirePermission = true
			ticket, err := store.Begin(t.Context(), admission)
			if err != nil || ticket.PermissionID == "" {
				t.Fatalf("begin=%+v %v", ticket, err)
			}
			command, rules, hasRule := capability.PermissionRule(operation, admission.Request.Arguments, "")
			if hasRule || rules != nil {
				t.Fatalf("browser summary installed rules: %v", rules)
			}
			for _, detail := range []string{"Agent control", "tab-1", "profile-1", "SSH preview network", "saved-host", "verified-runtime", "127.0.0.1", "connection-1", "project-environment"} {
				if !strings.Contains(command, detail) {
					t.Errorf("missing %q: %s", detail, command)
				}
			}
			switch operation {
			case "browser.open":
				if !strings.Contains(command, "Proposed preview ports (not yet approved for this new tab): [3000 5173 8080]") || strings.Contains(command, "approved ports:") {
					t.Fatalf("pending open scope mislabeled approved: %s", command)
				}
			case "browser.attach":
				if !strings.Contains(command, "Offered existing-tab preview ports: [3000 8080]") {
					t.Fatalf("pending attach omitted existing offered scope: %s", command)
				}
			case "browser.allow_preview_port":
				if !strings.Contains(command, "Currently approved preview ports: [3000 8080]") || !strings.Contains(command, "Requested additional preview port: 9000 (proposed; not yet approved)") {
					t.Fatalf("pending expansion mixed approved and proposed ports: %s", command)
				}
			}
			if !strings.Contains(command, "This approval also authorizes") || strings.Contains(command, "from this Once decision") {
				t.Fatalf("pending consent omitted its network authorization: %s", command)
			}
			if event := lastEventOfKind(t, store, root, "permission.pending"); event.Command != command || event.Rule != "" || event.PermissionID != ticket.PermissionID {
				t.Fatalf("durable pending event lost Once-only resource summary: %+v", event)
			}
			pending, err := store.ListPendingPermissions(t.Context(), root)
			if err != nil || len(pending) != 1 || pending[0].Command != command || pending[0].Rule != "" {
				t.Fatalf("pending summary lost: %+v %v", pending, err)
			}
			snapshot, err := store.SnapshotRoot(t.Context(), root)
			if err != nil || len(snapshot.Permissions) != 1 || snapshot.Permissions[0].Command != command || snapshot.Permissions[0].Rule != "" {
				t.Fatalf("snapshot summary lost: %+v %v", snapshot.Permissions, err)
			}
			page, err := store.RootCollectionPage(t.Context(), root, "permissions", CollectionPageOptions{Limit: 16, MaxBytes: 64 << 10})
			if err != nil || len(page.Items) != 1 || page.Items[0].Permission == nil || page.Items[0].Permission.Command != command || page.Items[0].Permission.Rule != "" {
				t.Fatalf("collection summary lost: %+v %v", page.Items, err)
			}
			for _, remember := range []string{"tree", "global", "unexpected"} {
				if _, err := store.Decide(t.Context(), admission, ticket.PermissionID, capability.Decision{
					Allow: true, PrincipalID: "human", Remember: remember,
				}); !errors.Is(err, capability.ErrDenied) {
					t.Fatalf("remember %q accepted: %v", remember, err)
				}
			}
			if _, err := store.Decide(t.Context(), admission, ticket.PermissionID, capability.Decision{Allow: true, PrincipalID: "human"}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := store.AddPermissionRule(t.Context(), root, "browser.open", "*", "human"); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("browser rule saved: %v", err)
	}
	store.SetGlobalPermissionRules([]string{"browser.open:*"})
	if source, err := store.PermissionRuleSource(t.Context(), root, "browser.open", []string{"*"}); err != nil || source != "" {
		t.Fatalf("global browser rule honored: %q %v", source, err)
	}
	// Even a preexisting/injected database rule cannot authorize a browser prompt.
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO permission_rules(id,root_id,operation,rule,principal_id,created_at)
		VALUES('browser-rule',?,'browser.open','*','human',?)`, root, now()); err != nil {
		t.Fatal(err)
	}
	store.SetGlobalPermissionRules(nil)
	if source, err := store.PermissionRuleSource(t.Context(), root, "browser.open", []string{"*"}); err != nil || source != "" {
		t.Fatalf("stored browser rule honored: %q %v", source, err)
	}
	if _, err := store.AddPermissionRule(t.Context(), root, "browser_exec", "legacy", "human"); err != nil {
		t.Fatalf("legacy behavior changed: %v", err)
	}
}

func TestBrowserAdmissionRejectsUnboundAndStaleGrant(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	scope := testBrowserScope("attachment")
	ref := issueTestBrowser(t, store, root, root, "", scope, capability.Reference{})
	for _, tc := range []struct {
		name   string
		mutate func(*capability.Admission)
	}{
		{"module only", func(a *capability.Admission) { a.Request.CapabilityID = "shell:" + root }},
		{"stale scope", func(a *capability.Admission) {
			changed := scope
			changed.ProviderEpoch = "stale"
			a.Request.Arguments, _ = json.Marshal(capability.BrowserCall{Grant: ref, Scope: changed, Arguments: json.RawMessage(`{}`)})
		}},
		{"missing envelope grant", func(a *capability.Admission) {
			a.Request.Arguments, _ = json.Marshal(capability.BrowserCall{Scope: scope, Arguments: json.RawMessage(`{}`)})
		}},
		{"unknown operation", func(a *capability.Admission) { a.Request.Operation = "browser.escape" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := browserTestAdmission(t, root, root, "browser.run", ref, scope)
			a.Request.OperationID += tc.name
			tc.mutate(&a)
			if _, err := store.Begin(t.Context(), a); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("invalid admission accepted: %v", err)
			}
		})
	}
}

func TestBrowserFreshChildAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	admitTestChild(t, store, root, root, "child")
	scope := testBrowserScope("child")
	ref := issueTestBrowser(t, store, root, "child", "", scope, capability.Reference{})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.AuthorizeBrowser(t.Context(), root, "child", ref, "browser.run", scope); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, root, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeBrowser(t.Context(), root, "child", ref, "browser.run", scope); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("stopped agent retained browser authority: %v", err)
	}
}

func TestBrowserAncestorGenerationAndTerminalState(t *testing.T) {
	for _, mutation := range []string{"generation", "terminal", "missing reference", "cycle", "widened persisted scope"} {
		t.Run(mutation, func(t *testing.T) {
			store, root, _ := newSwarmFixture(t)
			admitTestChild(t, store, root, root, "parent")
			admitTestChild(t, store, root, "parent", "child")
			parentScope := testBrowserScope("parent")
			parent := issueTestBrowser(t, store, root, "parent", "", parentScope, capability.Reference{})
			childScope := testBrowserScope("child")
			child := issueTestBrowser(t, store, root, "child", "parent", childScope, parent)
			if err := store.SetBrowserDelegationOnly(t.Context(), root, "parent", parent, true); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "generation":
				mcpTestSQL(t, store, `UPDATE capabilities SET generation=generation+1 WHERE id=?`, parent.ID)
			case "terminal":
				mcpTestSQL(t, store, `UPDATE agents SET status='stopped' WHERE id='parent'`)
			case "missing reference":
				mcpTestSQL(t, store, `UPDATE capabilities SET scopes=json_remove(scopes,'$.browser_issuer_id') WHERE id=?`, child.ID)
			case "cycle":
				mcpTestSQL(t, store, `UPDATE capabilities SET issuer_agent_id='child',scopes=json_set(scopes,'$.browser_issuer_id',?,'$.browser_issuer_generation',1) WHERE id=?`, child.ID, parent.ID)
			case "widened persisted scope":
				mcpTestSQL(t, store, `UPDATE capabilities SET scopes=json_set(scopes,'$.browser.preview.ports',json('[3000,8080,9000]')) WHERE id=?`, child.ID)
				childScope.Preview.Ports = []int{3000, 8080, 9000}
			}
			if err := store.AuthorizeBrowser(t.Context(), root, "child", child, "browser.run", childScope); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("invalid ancestry accepted: %v", err)
			}
		})
	}
}

func TestBrowserFailedHandoffCanRestoreParent(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	admitTestChild(t, store, root, root, "child")
	parentScope := testBrowserScope("parent")
	parent := issueTestBrowser(t, store, root, root, "", parentScope, capability.Reference{})
	child := issueTestBrowser(t, store, root, "child", root, testBrowserScope("child"), parent)
	// Before native acknowledgment, the tentative grant has not disturbed parent control.
	if err := store.AuthorizeBrowser(t.Context(), root, root, parent, "browser.run", parentScope); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueBrowserCapability(t.Context(), root, "child", root, testBrowserScope("second"), parent); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("second handoff admitted concurrently: %v", err)
	}
	if _, err := store.RevokeCapabilityFor(t.Context(), root, root, child.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBrowserDelegationOnly(t.Context(), root, root, parent, false); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeBrowser(t.Context(), root, root, parent, "browser.run", parentScope); err != nil {
		t.Fatal(err)
	}
	// Resource-specific capabilities must never be mistaken for module authority.
	authority, _, err := store.LoadAgentAuthority(t.Context(), root, root)
	if err != nil || authority.Shell.ID != "shell:"+root {
		t.Fatalf("authority=%+v err=%v", authority, err)
	}
}
