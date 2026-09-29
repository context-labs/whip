package rpc_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestGrantRevocationRPCUsesAtomicOptionalOwnerScope(t *testing.T) {
	_, c := fixture(t)
	root := create(t, c).Root
	grant := call[protocol.Grant](t, c, "grants.create", protocol.CreateGrantParams{ID: "reused-rpc-grant", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory})
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: root.ID})
	replacement := create(t, c).Root
	grant = call[protocol.Grant](t, c, "grants.create", protocol.CreateGrantParams{ID: grant.ID, SessionID: replacement.ID, Capability: "files.read", Resource: replacement.WorkingDirectory})
	var rejected *client.Error
	var raw json.RawMessage
	if err := c.Call(t.Context(), "grants.revoke", protocol.GrantParams{GrantID: grant.ID, SessionID: &root.ID}, &raw); !errors.As(err, &rejected) || rejected.Kind != "CONFLICT" {
		t.Fatal(err)
	}
	page := call[protocol.GrantsResult](t, c, "grants.list", protocol.GrantsParams{SessionID: replacement.ID, Limit: 1})
	if len(page.Items) != 1 || page.Items[0].RevokedAt != nil {
		t.Fatal(page)
	}
	value := call[protocol.Grant](t, c, "grants.revoke", protocol.GrantParams{GrantID: grant.ID, SessionID: &replacement.ID})
	if value.SessionID != replacement.ID || value.RevokedAt == nil {
		t.Fatal(value)
	}
	value = call[protocol.Grant](t, c, "grants.revoke", protocol.GrantParams{GrantID: grant.ID})
	if value.RevokedAt == nil {
		t.Fatal(value)
	}
}
