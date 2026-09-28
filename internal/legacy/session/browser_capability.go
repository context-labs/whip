package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/context-labs/whip/internal/capability"
)

func browserOperation(operation string) bool {
	switch operation {
	case "browser.open", "browser.attach", "browser.run", "browser.detach", "browser.allow_preview_port":
		return true
	default:
		return false
	}
}

func validBrowserScope(scope capability.BrowserScope, attachment bool) bool {
	if scope.ProviderID == "" || scope.ProviderEpoch == "" || scope.TabID == "" ||
		scope.TabGeneration == "" || scope.ProfileID == "" || len(scope.Rights) == 0 {
		return false
	}
	if (scope.AttachmentID == "") != (scope.AttachmentGeneration == "") ||
		(attachment && scope.AttachmentID == "") {
		return false
	}
	seen := make(map[string]bool)
	for _, right := range scope.Rights {
		if (right != "create" && right != "control" && right != "route") || seen[right] {
			return false
		}
		seen[right] = true
	}
	if preview := scope.Preview; preview != nil {
		if preview.HostID == "" || preview.HostIdentity == "" || preview.ConnectionGeneration == "" ||
			preview.EnvironmentID == "" || (preview.Loopback != "127.0.0.1" && preview.Loopback != "::1") {
			return false
		}
		previous := 0
		for _, port := range preview.Ports {
			if port <= previous || port > 65535 {
				return false
			}
			previous = port
		}
	}
	return true
}

func browserScopeSubset(child, parent capability.BrowserScope) bool {
	if child.ProviderID != parent.ProviderID || child.ProviderEpoch != parent.ProviderEpoch ||
		child.TabID != parent.TabID || child.TabGeneration != parent.TabGeneration || child.ProfileID != parent.ProfileID {
		return false
	}
	for _, right := range child.Rights {
		if !slices.Contains(parent.Rights, right) {
			return false
		}
	}
	if child.Preview == nil || parent.Preview == nil {
		return child.Preview == nil && parent.Preview == nil
	}
	c, p := child.Preview, parent.Preview
	if c.HostID != p.HostID || c.HostIdentity != p.HostIdentity || c.ConnectionGeneration != p.ConnectionGeneration ||
		c.EnvironmentID != p.EnvironmentID || c.Loopback != p.Loopback {
		return false
	}
	for _, port := range c.Ports {
		if !slices.Contains(p.Ports, port) {
			return false
		}
	}
	return true
}

func sameBrowserScope(a, b capability.BrowserScope) bool {
	return a.AttachmentID == b.AttachmentID && a.AttachmentGeneration == b.AttachmentGeneration &&
		browserScopeSubset(a, b) && browserScopeSubset(b, a)
}

func browserRight(operation string) string {
	switch operation {
	case "browser.open":
		return "create"
	case "browser.attach", "browser.run":
		return "control"
	case "browser.allow_preview_port":
		return "route"
	default:
		return "" // Detach may always give up an otherwise valid grant.
	}
}

// AuthorizeBrowser checks an exact attachment and its live delegation ancestry.
func (s *Store) AuthorizeBrowser(
	ctx context.Context, rootID, agentID string, ref capability.Reference,
	operation string, scope capability.BrowserScope,
) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := authorizeBrowserTx(ctx, tx, rootID, agentID, ref, operation, scope); err != nil {
		return err
	}
	return tx.Commit()
}

func authorizeBrowserTx(
	ctx context.Context, tx *sql.Tx, rootID, agentID string, ref capability.Reference,
	operation string, scope capability.BrowserScope,
) error {
	if !browserOperation(operation) || !validBrowserScope(scope, true) {
		return capability.ErrDenied
	}
	grant, err := loadBrowserAuthorityTx(ctx, tx, rootID, agentID, ref)
	if err != nil {
		return err
	}
	if !slices.Contains(grant.operations, operation) || !sameBrowserScope(*grant.scopes.Browser, scope) ||
		(grant.scopes.BrowserDelegationOnly && operation != "browser.detach") {
		return capability.ErrDenied
	}
	if right := browserRight(operation); right != "" && !slices.Contains(scope.Rights, right) {
		return capability.ErrDenied
	}
	return nil
}

func loadBrowserAuthorityTx(
	ctx context.Context, tx *sql.Tx, rootID, agentID string, ref capability.Reference,
) (loadedCapability, error) {
	ancestors := make(map[string]int)
	for id := agentID; ; {
		if id == "" || ancestors[id] != 0 {
			return loadedCapability{}, capability.ErrDenied
		}
		ancestors[id] = len(ancestors) + 1
		agent, err := loadAgentTx(ctx, tx, rootID, id)
		if err != nil {
			if errors.Is(err, ErrAgentAccess) {
				return loadedCapability{}, capability.ErrDenied
			}
			return loadedCapability{}, err
		}
		if isTerminalAgentStatus(agent.Status) {
			return loadedCapability{}, capability.ErrDenied
		}
		if id == rootID {
			break
		}
		id = agent.ParentID
	}
	var result loadedCapability
	var child *capability.BrowserScope
	var childOperations []string
	seen := make(map[string]bool)
	for {
		if ref.ID == "" || ref.Generation <= 0 || seen[ref.ID] {
			return loadedCapability{}, capability.ErrDenied
		}
		seen[ref.ID] = true
		grant, err := loadCapabilityGrant(ctx, tx, rootID, agentID, ref.ID, ref.Generation)
		if err != nil {
			return loadedCapability{}, err
		}
		scopes := grant.scopes
		if scopes.Browser == nil || !validBrowserScope(*scopes.Browser, true) || len(scopes.Paths) != 0 ||
			scopes.MCPAll || len(scopes.MCP) != 0 || len(grant.operations) == 0 {
			return loadedCapability{}, capability.ErrDenied
		}
		for _, operation := range grant.operations {
			if operation != "browser.run" && operation != "browser.detach" && operation != "browser.allow_preview_port" {
				return loadedCapability{}, capability.ErrDenied
			}
		}
		if child == nil {
			result = grant
		} else if !scopes.BrowserDelegationOnly || !browserScopeSubset(*child, *scopes.Browser) {
			return loadedCapability{}, capability.ErrDenied
		}
		for _, operation := range childOperations {
			if !slices.Contains(grant.operations, operation) {
				return loadedCapability{}, capability.ErrDenied
			}
		}
		if scopes.BrowserIssuerID == "" {
			// A fresh human-approved attach can be issued directly to a child.
			if scopes.BrowserIssuerGeneration != 0 || grant.issuerAgentID != "" {
				return loadedCapability{}, capability.ErrDenied
			}
			return result, nil
		}
		if ancestors[grant.issuerAgentID] <= ancestors[agentID] {
			return loadedCapability{}, capability.ErrDenied
		}
		child = scopes.Browser
		childOperations = grant.operations
		agentID = grant.issuerAgentID
		ref = capability.Reference{ID: scopes.BrowserIssuerID, Generation: scopes.BrowserIssuerGeneration}
	}
}

// IssueBrowserCapability records a trusted, approved attachment. A nonzero parent
// delegates a narrower resource to a descendant; it remains unusable until the
// broker acknowledges native handoff and marks the parent delegation-only.
func (s *Store) IssueBrowserCapability(
	ctx context.Context, rootID, agentID, issuerAgentID string,
	scope capability.BrowserScope, parent capability.Reference,
) (capability.Reference, error) {
	if !validBrowserScope(scope, true) {
		return capability.Reference{}, capability.ErrDenied
	}
	id, err := runtimeID()
	if err != nil {
		return capability.Reference{}, err
	}
	ref := capability.Reference{ID: "browser:" + id, Generation: 1}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capability.Reference{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateBrowserAgentTx(ctx, tx, rootID, agentID); err != nil {
		return capability.Reference{}, err
	}
	scopes := storedCapabilityScopes{Browser: &scope}
	ops := []string{"browser.run", "browser.detach", "browser.allow_preview_port"}
	if parent.ID != "" {
		issuer, err := loadBrowserAuthorityTx(ctx, tx, rootID, issuerAgentID, parent)
		if err != nil {
			return capability.Reference{}, err
		}
		allowed, err := agentInSubtreeTx(ctx, tx, rootID, issuerAgentID, agentID)
		if err != nil {
			return capability.Reference{}, err
		}
		if !allowed || issuerAgentID == agentID || issuer.scopes.BrowserDelegationOnly ||
			!browserScopeSubset(scope, *issuer.scopes.Browser) ||
			scope.AttachmentID == issuer.scopes.Browser.AttachmentID {
			return capability.Reference{}, capability.ErrDenied
		}
		var children int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM capabilities
			WHERE root_id=? AND status='active' AND json_extract(scopes,'$.browser_issuer_id')=?`,
			rootID, parent.ID).Scan(&children); err != nil {
			return capability.Reference{}, err
		}
		if children != 0 {
			return capability.Reference{}, capability.ErrDenied
		}
		ops = issuer.operations
		scopes.BrowserIssuerID = parent.ID
		scopes.BrowserIssuerGeneration = parent.Generation
		scopes.ExpiresAt = issuer.scopes.ExpiresAt
	} else if parent.Generation != 0 || issuerAgentID != "" {
		return capability.Reference{}, capability.ErrDenied
	}
	operations, err := json.Marshal(ops)
	if err != nil {
		return capability.Reference{}, err
	}
	raw, err := json.Marshal(scopes)
	if err != nil {
		return capability.Reference{}, err
	}
	stamp := now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO capabilities
		(id,root_id,agent_id,issuer_agent_id,operations,scopes,generation,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,1,'active',?,?)`,
		ref.ID, rootID, agentID, issuerAgentID, operations, raw, stamp, stamp); err != nil {
		return capability.Reference{}, err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "capability.delegated", actorEvent{
		AgentID: agentID, SenderAgentID: issuerAgentID, CapabilityID: ref.ID, Generation: 1, Status: "active",
	}, stamp); err != nil {
		return capability.Reference{}, err
	}
	return ref, tx.Commit()
}

func validateBrowserAgentTx(ctx context.Context, tx *sql.Tx, rootID, agentID string) error {
	seen := make(map[string]bool)
	for {
		if agentID == "" || seen[agentID] {
			return capability.ErrDenied
		}
		seen[agentID] = true
		agent, err := loadAgentTx(ctx, tx, rootID, agentID)
		if err != nil || isTerminalAgentStatus(agent.Status) {
			return capability.ErrDenied
		}
		if agentID == rootID {
			return nil
		}
		agentID = agent.ParentID
	}
}

// SetBrowserDelegationOnly retains revocation authority while disabling control.
// The broker must finish native handoff first. Restoration requires revoking all
// tentative descendants so it cannot create two executable controllers.
func (s *Store) SetBrowserDelegationOnly(
	ctx context.Context, rootID, agentID string, ref capability.Reference, delegationOnly bool,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	grant, err := loadBrowserAuthorityTx(ctx, tx, rootID, agentID, ref)
	if err != nil {
		return err
	}
	if !delegationOnly {
		var descendants int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM capabilities
			WHERE root_id=? AND status='active' AND json_extract(scopes,'$.browser_issuer_id')=?`,
			rootID, ref.ID).Scan(&descendants); err != nil {
			return err
		}
		if descendants != 0 {
			return capability.ErrDenied
		}
	}
	grant.scopes.BrowserDelegationOnly = delegationOnly
	raw, err := json.Marshal(grant.scopes)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE capabilities SET scopes=?,updated_at=?
		WHERE id=? AND root_id=? AND agent_id=? AND generation=? AND status='active'`,
		raw, now(), ref.ID, rootID, agentID, ref.Generation); err != nil {
		return err
	}
	if err := s.cancelPendingPermissionsTx(ctx, tx, rootID, "", ref.ID, "denied", agentID, "browser control delegated"); err != nil {
		return err
	}
	return tx.Commit()
}

func validateBrowserAdmissionTx(ctx context.Context, tx *sql.Tx, admission capability.Admission) error {
	request := admission.Request
	var call capability.BrowserCall
	decoder := json.NewDecoder(bytes.NewReader(request.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&call); err != nil || !validBrowserScope(call.Scope, false) {
		return capability.ErrDenied
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return capability.ErrDenied
	}
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments == nil {
		return capability.ErrDenied
	}
	if admission.CanonicalPath != "" || admission.Mutation != capability.MutationNone || request.WriterCapabilityID != "" {
		return capability.ErrDenied
	}
	ref := capability.Reference{ID: request.CapabilityID, Generation: request.CapabilityGeneration}
	if request.Operation == "browser.open" || request.Operation == "browser.attach" {
		grant, err := loadCapabilityGrant(ctx, tx, request.RootID, request.AgentID, ref.ID, ref.Generation)
		if err != nil {
			return err
		}
		if grant.scopes.Browser != nil || !slices.Contains(grant.operations, request.Operation) ||
			call.Grant != (capability.Reference{}) || !slices.Contains(call.Scope.Rights, browserRight(request.Operation)) {
			return capability.ErrDenied
		}
		return validateBrowserAgentTx(ctx, tx, request.RootID, request.AgentID)
	}
	if call.Grant != ref {
		return capability.ErrDenied
	}
	return authorizeBrowserTx(ctx, tx, request.RootID, request.AgentID, ref, request.Operation, call.Scope)
}
