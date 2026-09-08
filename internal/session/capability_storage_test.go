package session

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func TestAuthorityBootstrapWriteFailureDoesNotLeavePartialAuthority(t *testing.T) {
	for _, table := range []string{"agents", "capabilities", "budgets"} {
		t.Run(table, func(t *testing.T) {
			store, root := collectionStore(t)
			rejectSessionWrite(t, store, "INSERT", table, "")
			_, err := store.EnsureAuthority(t.Context(), root)
			requireSessionWriteFailure(t, err)
			for _, ledger := range []string{"agents", "capabilities", "budgets"} {
				var count int
				if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+ledger+" WHERE root_id=?", root).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed bootstrap left %s=%d %v", ledger, count, err)
				}
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			authority, err := store.EnsureAuthority(t.Context(), root)
			if err != nil || authority.Files.ID == "" || authority.Shell.ID == "" || authority.MCP.ID == "" {
				t.Fatalf("retry incomplete authority %+v %v", authority, err)
			}
		})
	}
}

func TestAuthorityBootstrapRejectsCorruptPersistedGenerations(t *testing.T) {
	for _, kind := range []string{"files", "shell", "mcp"} {
		t.Run(kind, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			exec(t, store, `UPDATE capabilities SET generation='not an integer' WHERE id=?`, kind+":"+root)
			before := inputRootSnapshot(t, store, root)
			if _, err := store.EnsureAuthority(t.Context(), root); err == nil {
				t.Fatal("corrupt authority silently refreshed")
			}
			requireRootUnchanged(t, store, root, before)
			exec(t, store, `UPDATE capabilities SET generation=7 WHERE id=?`, kind+":"+root)
			authority, err := store.EnsureAuthority(t.Context(), root)
			if err != nil || authority.AgentID != agent {
				t.Fatalf("repaired authority %+v %v", authority, err)
			}
			got := map[string]int64{"files": authority.Files.Generation, "shell": authority.Shell.Generation, "mcp": authority.MCP.Generation}
			if got[kind] != 7 {
				t.Fatalf("bootstrap reset generation %v", got)
			}
		})
	}
}

func TestAuthorityDiscoverySelectsAvailableWriteBrowserAndComputerGrants(t *testing.T) {
	for _, test := range []struct {
		name              string
		operations, names []string
	}{
		{"write first", []string{"write", "read"}, []string{"read", "write"}},
		{"browser first", []string{"browser_exec", "bash"}, []string{"browser", "shell"}},
		{"computer first", []string{"computer_exec", "bash"}, []string{"computer", "shell"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root, parent := newSwarmFixture(t)
			admitTestChild(t, store, root, parent, "child")
			grant := capability.Grant{ID: "child-grant", RootID: root, AgentID: "child", Operations: test.operations, Generation: 3}
			if err := store.IssueCapability(t.Context(), grant); err != nil {
				t.Fatal(err)
			}
			got, names, err := store.LoadAgentAuthority(t.Context(), root, "child")
			if err != nil || !slices.Equal(names, test.names) {
				t.Fatalf("names=%v %v", names, err)
			}
			reference := got.Shell
			if test.operations[0] == "write" {
				reference = got.Files
			}
			if reference.ID != grant.ID || reference.Generation != 3 {
				t.Fatalf("wrong selected authority %+v", got)
			}
			if err := store.AuthorizeCapability(t.Context(), root, "child", reference, "ungranted", ""); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("unknown operation authorized: %v", err)
			}
			if err := store.AuthorizeCapability(t.Context(), root, "child", reference, test.operations[0], "/outside"); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("unscoped path authorized: %v", err)
			}
		})
	}
}

func TestAuthorityDiscoveryRejectsPartialCorruptAndClosedLedgers(t *testing.T) {
	for _, mutation := range []string{`UPDATE capabilities SET operations='invalid json' WHERE id LIKE 'shell:%'`, `UPDATE capabilities SET generation='invalid number' WHERE id LIKE 'shell:%'`} {
		t.Run(mutation, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			exec(t, store, mutation)
			authority, names, err := store.LoadAgentAuthority(t.Context(), root, agent)
			if err == nil || !reflect.DeepEqual(authority, capability.Authority{}) || names != nil {
				t.Fatalf("returned partial authority %+v %v %v", authority, names, err)
			}
		})
	}
	store, root, agent := newSwarmFixture(t)
	if _, _, err := store.LoadAgentAuthority(t.Context(), "", agent); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("empty root=%v", err)
	}
	if err := store.AuthorizeMCP(t.Context(), root, agent, capability.Reference{}, capability.MCPSelector{}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("empty selector=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func(context.Context) error{
		"discovery": func(ctx context.Context) error { _, _, err := store.LoadAgentAuthority(ctx, root, agent); return err },
		"paths": func(ctx context.Context) error {
			_, err := store.CapabilityPaths(ctx, root, agent, capability.Reference{ID: "files:" + root, Generation: 1})
			return err
		},
		"selectors": func(ctx context.Context) error {
			_, _, err := store.MCPSelectors(ctx, root, agent, capability.Reference{ID: "mcp:" + root, Generation: 1})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(t.Context()); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed storage result=%v", err)
			}
		})
	}
}
