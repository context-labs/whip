package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) permissionsCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "list" && len(fields) <= 2 {
		owner := m.owner
		params := protocol.GrantsParams{SessionID: owner.ID, Limit: 100}
		if len(fields) == 2 {
			params.After = new(protocol.ID(fields[1]))
		}
		return m.control("Permission authority", false, func(ctx context.Context) nativeControlResult {
			var policy protocol.PermissionPolicy
			if err := m.connection.Call(ctx, "permissions.policy", protocol.SessionParams{SessionID: owner.ID}, &policy); err != nil {
				return nativeControlResult{err: err}
			}
			if policy.TreeID != owner.TreeID {
				return nativeControlResult{err: errors.New("permission policy ownership mismatch")}
			}
			var grants protocol.GrantsResult
			if err := m.connection.Call(ctx, "grants.list", params, &grants); err != nil {
				return nativeControlResult{err: err}
			}
			if len(grants.Items) > params.Limit {
				return nativeControlResult{err: errors.New("grant page exceeds its limit")}
			}
			previous := protocol.ID("")
			if params.After != nil {
				previous = *params.After
			}
			for _, grant := range grants.Items {
				if grant.SessionID != owner.ID || grant.ID <= previous {
					return nativeControlResult{err: errors.New("grant page ownership or ordering mismatch")}
				}
				previous = grant.ID
			}
			return nativeControlResult{policy: &policy, notice: nativeGrantText(owner, policy, grants.Items)}
		})
	}
	if len(fields) == 2 && fields[0] == "forget" {
		if !m.navigationAllowed() {
			return nil
		}
		params := protocol.GrantParams{GrantID: protocol.ID(fields[1]), SessionID: new(m.owner.ID)}
		return m.control("Revoke grant "+fields[1], true, func(ctx context.Context) nativeControlResult {
			var grant protocol.Grant
			err := m.connection.Call(ctx, "grants.revoke", params, &grant)
			if err == nil && (grant.ID != params.GrantID || grant.SessionID != *params.SessionID || grant.RevokedAt == nil) {
				err = errors.New("grant revocation ownership mismatch")
			}
			return nativeControlResult{err: err}
		})
	}
	if len(fields) == 2 && fields[0] == "mode" && (fields[1] == "prompt" || fields[1] == "automatic") {
		if !m.navigationAllowed() {
			return nil
		}
		if m.owner.ParentID != nil {
			m.status = "Permission mode is a root control. Children require explicit delegated grants."
			return nil
		}
		if m.permissionPolicy == nil || m.permissionPolicy.TreeID != m.owner.TreeID {
			m.status = "Read /permissions first to capture the current policy revision."
			return nil
		}
		params := protocol.SetPermissionModeParams{EditID: protocol.ID(uuid.NewString()), SessionID: m.owner.ID, ExpectedRevision: m.permissionPolicy.Revision, Mode: fields[1]}
		owner := m.owner
		tree := owner.TreeID
		return m.control("Set permission mode "+fields[1], true, func(ctx context.Context) nativeControlResult {
			var edit protocol.PermissionModeEdit
			err := m.connection.Call(ctx, "permissions.set_mode", params, &edit)
			if err == nil && (edit.ID != params.EditID || edit.SessionID != params.SessionID || edit.ExpectedRevision != params.ExpectedRevision || edit.Mode != params.Mode || edit.Policy.TreeID != tree) {
				err = errors.New("permission edit ownership mismatch")
			}
			if err != nil {
				return nativeControlResult{err: err}
			}
			return nativeControlResult{policy: &edit.Policy, notice: nativeGrantText(owner, edit.Policy, nil)}
		})
	}
	m.status = "usage: /permissions [list [after-grant-id]|forget <grant-id>|mode prompt|automatic]"
	return nil
}

func nativeGrantText(owner protocol.Session, policy protocol.PermissionPolicy, grants []protocol.Grant) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Owner %s\nTree mode: %s · revision %d · interactive denial: %t\n", owner.ID, policy.Mode, policy.Revision, policy.DenyInteractive)
	text.WriteString("Automatic mode applies only to eligible root operations. Children need delegated grants; protected capabilities still need explicit consent.\n")
	if grants == nil {
		return text.String()
	}
	text.WriteString("\nThis owner's grants (current observation, at most 100):\n")
	for _, grant := range grants {
		state, kind := "active", "standing"
		if grant.RevokedAt != nil {
			state = "revoked"
		}
		if grant.OperationID != nil {
			kind = "one-use"
		} else if grant.IssuerID != nil {
			kind = "delegated"
		}
		resource, cut := nativeTextPrefix(nativeDisplayText(grant.Resource), 256)
		if cut {
			resource += "…"
		}
		fmt.Fprintf(&text, "%s · %s · %s · %s · %s\n", grant.ID, state, kind, grant.Capability, resource)
	}
	if len(grants) == 0 {
		text.WriteString("No grants on this page.\n")
	} else {
		fmt.Fprintf(&text, "Continue: /permissions list %s\n", grants[len(grants)-1].ID)
	}
	text.WriteString("Forget revokes that exact owner's grant and derived authority; it does not undo dispatched effects. /permissions rereads the first page.")
	return text.String()
}
