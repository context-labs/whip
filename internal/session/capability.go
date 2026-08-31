package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

type storedCapabilityScopes struct {
	Paths     []string `json:"paths,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

func (s *Store) Workspaces() *capability.Workspaces { return s.workspaces }

func (s *Store) Processes() *capability.ProcessManager { return s.processes }

// EnsureClassicAuthority installs the root Classic agent's initial grants once.
func (s *Store) EnsureClassicAuthority(ctx context.Context, rootID string) (capability.ClassicAuthority, error) {
	authority := capability.ClassicAuthority{
		RootID: rootID, AgentID: "classic:" + rootID,
		Files: capability.Reference{ID: "classic-files:" + rootID},
		Shell: capability.Reference{ID: "classic-shell:" + rootID},
	}
	root, err := s.WorkspaceRoot(ctx, rootID)
	if err != nil {
		return capability.ClassicAuthority{}, err
	}
	workspace, err := s.workspaces.Open(root)
	if err != nil {
		return capability.ClassicAuthority{}, err
	}
	fileOperations, _ := json.Marshal([]string{"read", "write", "edit", "workspace.write"})
	fileScopes, _ := json.Marshal(storedCapabilityScopes{Paths: []string{workspace.Root()}})
	shellOperations, _ := json.Marshal([]string{"bash", "browser_exec", "computer_exec", "workspace_process"})
	shellScopes, _ := json.Marshal(storedCapabilityScopes{})
	stamp := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capability.ClassicAuthority{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,root_id,parent_id,status,created_at,updated_at)
		VALUES(?,?,NULL,'idle',?,?) ON CONFLICT(id) DO NOTHING`, authority.AgentID, rootID, stamp, stamp); err != nil {
		return capability.ClassicAuthority{}, err
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM agents WHERE id=? AND root_id=?`, authority.AgentID, rootID).Scan(&status); err != nil {
		return capability.ClassicAuthority{}, err
	}
	switch status {
	case "failed", "stopped", "cancelled", "interrupted", "deleted", "succeeded":
		return capability.ClassicAuthority{}, ErrRootTerminal
	}
	for _, grant := range []struct {
		id         string
		operations []byte
		scopes     []byte
	}{
		{authority.Files.ID, fileOperations, fileScopes},
		{authority.Shell.ID, shellOperations, shellScopes},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO capabilities(id,root_id,agent_id,operations,scopes,generation,status,created_at,updated_at)
			VALUES(?,?,?,?,?,1,'active',?,?) ON CONFLICT(id) DO NOTHING`, grant.id, rootID, authority.AgentID, grant.operations, grant.scopes, stamp, stamp); err != nil {
			return capability.ClassicAuthority{}, err
		}
	}
	if err := insertDefaultRootBudgets(ctx, tx, rootID, stamp); err != nil {
		return capability.ClassicAuthority{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM capabilities WHERE id=? AND root_id=? AND agent_id=?`,
		authority.Files.ID, rootID, authority.AgentID).Scan(&authority.Files.Generation); err != nil {
		return capability.ClassicAuthority{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM capabilities WHERE id=? AND root_id=? AND agent_id=?`,
		authority.Shell.ID, rootID, authority.AgentID).Scan(&authority.Shell.Generation); err != nil {
		return capability.ClassicAuthority{}, err
	}
	if err := tx.Commit(); err != nil {
		return capability.ClassicAuthority{}, err
	}
	return authority, nil
}

func (s *Store) WorkspaceRoot(ctx context.Context, rootID string) (string, error) {
	var root string
	if err := s.db.QueryRowContext(ctx, `SELECT cwd FROM sessions WHERE id=?`, rootID).Scan(&root); err != nil {
		return "", err
	}
	return root, nil
}

func (s *Store) IssueCapability(ctx context.Context, grant capability.Grant) error {
	if grant.ID == "" || grant.RootID == "" || grant.AgentID == "" || len(grant.Operations) == 0 {
		return errors.New("capability grant identity and operations are required")
	}
	root, err := s.WorkspaceRoot(ctx, grant.RootID)
	if err != nil {
		return err
	}
	workspace, err := s.workspaces.Open(root)
	if err != nil {
		return err
	}
	scopes := storedCapabilityScopes{}
	for _, scope := range grant.Scopes {
		canonical, err := workspace.Resolve(scope)
		if err != nil {
			return err
		}
		if !slices.Contains(scopes.Paths, canonical) {
			scopes.Paths = append(scopes.Paths, canonical)
		}
	}
	if slices.Contains(grant.Operations, "bash") && len(scopes.Paths) != 0 {
		return errors.New("shell capability cannot carry path scopes")
	}
	if !grant.ExpiresAt.IsZero() {
		scopes.ExpiresAt = grant.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	operationsJSON, err := json.Marshal(grant.Operations)
	if err != nil {
		return err
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	stamp := now()
	result, err := s.db.ExecContext(ctx, `INSERT INTO capabilities(id,root_id,agent_id,issuer_agent_id,operations,scopes,generation,status,created_at,updated_at)
		SELECT ?,?,?,?,?,?,?,'active',?,? WHERE EXISTS(SELECT 1 FROM agents WHERE root_id=? AND id=?)
		AND (?='' OR EXISTS(SELECT 1 FROM agents WHERE root_id=? AND id=?))`,
		grant.ID, grant.RootID, grant.AgentID, grant.IssuerAgentID, operationsJSON, scopesJSON, grant.Generation, stamp, stamp,
		grant.RootID, grant.AgentID, grant.IssuerAgentID, grant.RootID, grant.IssuerAgentID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return err
		}
		return capability.ErrDenied
	}
	return nil
}

func (s *Store) RevokeCapability(ctx context.Context, capabilityID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE capabilities SET status='revoked',generation=generation+1,updated_at=? WHERE id=? AND status='active'`, now(), capabilityID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return err
		}
		return capability.ErrDenied
	}
	return nil
}

func (s *Store) Begin(ctx context.Context, admission capability.Admission) (capability.Ticket, error) {
	payload, err := json.Marshal(admission)
	if err != nil {
		return capability.Ticket{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capability.Ticket{}, err
	}
	defer tx.Rollback()
	if err := validateCapabilityAgent(ctx, tx, admission.Request.RootID, admission.Request.AgentID); err != nil {
		return capability.Ticket{}, err
	}
	prepared, err := s.prepareRuntimeValue(RuntimePayload{Data: payload, MediaType: "application/json", Source: "capability operation"}, ContentGrant{
		RootID: admission.Request.RootID, AgentID: admission.Request.AgentID, Scope: ContentGrantAgent,
	})
	if err != nil {
		return capability.Ticket{}, err
	}
	if err := validateCapabilityAdmission(ctx, tx, admission); err != nil {
		return capability.Ticket{}, s.commitDeniedAdmission(ctx, tx, prepared, admission, err)
	}
	if err := validateCapabilityBudgets(ctx, tx, admission.Request.RootID, admission.Request.AgentID, admission.Request.Reservations, false); err != nil {
		return capability.Ticket{}, s.commitDeniedAdmission(ctx, tx, prepared, admission, err)
	}
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return capability.Ticket{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	status := "running"
	ticket := capability.Ticket{OperationID: admission.Request.OperationID}
	if admission.RequirePermission {
		status = "waiting"
		ticket.PermissionID, err = runtimeID()
	} else {
		ticket.LeaseID, err = runtimeID()
	}
	if err != nil {
		return capability.Ticket{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id,root_id,agent_id,command_client_id,command_id,status,payload_inline,payload_ref,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, admission.Request.OperationID, admission.Request.RootID, admission.Request.AgentID,
		admission.Request.CommandClientID, admission.Request.CommandID, status, inline, reference, stamp, stamp); err != nil {
		return capability.Ticket{}, err
	}
	if err := reserveCapabilityBudgets(ctx, tx, admission.Request.RootID, admission.Request.AgentID, admission.Request.Reservations); err != nil {
		return capability.Ticket{}, err
	}
	if admission.RequirePermission {
		request, _ := json.Marshal(map[string]any{
			"operation_id": admission.Request.OperationID, "request_digest": admission.RequestDigest,
			"capability_generation": admission.Request.CapabilityGeneration,
		})
		if _, err := tx.ExecContext(ctx, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,request_inline,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, ticket.PermissionID, admission.Request.RootID, admission.Request.AgentID,
			admission.Request.OperationID, "pending", request, stamp, stamp); err != nil {
			return capability.Ticket{}, err
		}
	} else if err := insertCapabilityLease(ctx, tx, ticket.LeaseID, admission, stamp); err != nil {
		return capability.Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return capability.Ticket{}, err
	}
	return ticket, nil
}

func (s *Store) Pending(ctx context.Context, permissionID string) (capability.Admission, error) {
	var inline []byte
	var reference sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT o.payload_inline,o.payload_ref FROM permission_requests p
		JOIN operations o ON o.root_id=p.root_id AND o.id=p.operation_id
		WHERE p.id=? AND p.status='pending' AND o.status='waiting'`, permissionID).Scan(&inline, &reference)
	if err != nil {
		return capability.Admission{}, err
	}
	payload, err := s.readRuntimeValue(ctx, inline, reference)
	if err != nil {
		return capability.Admission{}, err
	}
	var admission capability.Admission
	if err := json.Unmarshal(payload, &admission); err != nil {
		return capability.Admission{}, err
	}
	return admission, nil
}

func (s *Store) Decide(ctx context.Context, admission capability.Admission, permissionID string, decision capability.Decision) (capability.Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capability.Ticket{}, err
	}
	defer tx.Rollback()
	var rootID, agentID, operationID, permissionStatus, operationStatus string
	var inline []byte
	var reference sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT p.root_id,p.agent_id,p.operation_id,p.status,o.status,o.payload_inline,o.payload_ref FROM permission_requests p
		JOIN operations o ON o.root_id=p.root_id AND o.id=p.operation_id WHERE p.id=?`, permissionID).
		Scan(&rootID, &agentID, &operationID, &permissionStatus, &operationStatus, &inline, &reference); err != nil {
		return capability.Ticket{}, err
	}
	if permissionStatus != "pending" || operationStatus != "waiting" {
		return capability.Ticket{}, capability.ErrDenied
	}
	payload, err := s.readRuntimeValueTx(ctx, tx, inline, reference)
	if err != nil {
		return capability.Ticket{}, err
	}
	var stored capability.Admission
	if err := json.Unmarshal(payload, &stored); err != nil {
		return capability.Ticket{}, err
	}
	if !sameCapabilityAdmission(stored, admission) || rootID != stored.Request.RootID || agentID != stored.Request.AgentID || operationID != stored.Request.OperationID {
		if err := terminalizePermission(ctx, tx, stored, permissionID, "denied", decision.PrincipalID, capability.ErrStaleAdmission.Error()); err != nil {
			return capability.Ticket{}, err
		}
		if err := tx.Commit(); err != nil {
			return capability.Ticket{}, err
		}
		return capability.Ticket{}, capability.ErrStaleAdmission
	}
	admission = stored
	if !decision.Allow {
		if err := terminalizePermission(ctx, tx, admission, permissionID, "denied", decision.PrincipalID, decision.Reason); err != nil {
			return capability.Ticket{}, err
		}
		if err := tx.Commit(); err != nil {
			return capability.Ticket{}, err
		}
		return capability.Ticket{}, capability.ErrDenied
	}
	if err := validateCapabilityAgent(ctx, tx, rootID, agentID); err != nil {
		return capability.Ticket{}, s.denyStalePermission(ctx, tx, admission, permissionID, decision, err)
	}
	if err := validateCapabilityAdmission(ctx, tx, admission); err != nil {
		return capability.Ticket{}, s.denyStalePermission(ctx, tx, admission, permissionID, decision, err)
	}
	if err := validateCapabilityBudgets(ctx, tx, rootID, agentID, admission.Request.Reservations, true); err != nil {
		return capability.Ticket{}, s.denyStalePermission(ctx, tx, admission, permissionID, decision, err)
	}
	leaseID, err := runtimeID()
	if err != nil {
		return capability.Ticket{}, err
	}
	stamp := now()
	if err := insertCapabilityLease(ctx, tx, leaseID, admission, stamp); err != nil {
		return capability.Ticket{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status='running',updated_at=? WHERE id=? AND status='waiting'`, stamp, operationID); err != nil {
		return capability.Ticket{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE permission_requests SET status=?,updated_at=? WHERE id=? AND status='pending'`, permissionStatusValue("approved", decision.PrincipalID), stamp, permissionID); err != nil {
		return capability.Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return capability.Ticket{}, err
	}
	return capability.Ticket{OperationID: operationID, LeaseID: leaseID}, nil
}

func (s *Store) Finish(ctx context.Context, completion capability.Completion) error {
	if completion.Status != capability.StatusSucceeded && completion.Status != capability.StatusFailed {
		return fmt.Errorf("invalid capability completion status %q", completion.Status)
	}
	result, err := json.Marshal(map[string]string{"output": completion.Output, "error": completion.Error})
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var operationStatus, leaseStatus string
	var inline []byte
	var reference sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT o.status,l.status,o.payload_inline,o.payload_ref FROM operations o JOIN leases l ON l.root_id=o.root_id AND l.operation_id=o.id
		WHERE o.id=? AND o.root_id=? AND o.agent_id=? AND l.id=?`, completion.Admission.Request.OperationID,
		completion.Admission.Request.RootID, completion.Admission.Request.AgentID, completion.LeaseID).
		Scan(&operationStatus, &leaseStatus, &inline, &reference); err != nil {
		return err
	}
	if operationStatus != "running" || leaseStatus != "running" {
		return capability.ErrDenied
	}
	payload, err := s.readRuntimeValueTx(ctx, tx, inline, reference)
	if err != nil {
		return err
	}
	var stored capability.Admission
	if err := json.Unmarshal(payload, &stored); err != nil {
		return err
	}
	if !sameCapabilityAdmission(stored, completion.Admission) {
		return capability.ErrStaleAdmission
	}
	completion.Admission = stored
	prepared, err := s.prepareRuntimeValue(RuntimePayload{Data: result, MediaType: "application/json", Source: "capability result"}, ContentGrant{
		RootID: stored.Request.RootID, AgentID: stored.Request.AgentID, Scope: ContentGrantAgent,
	})
	if err != nil {
		return err
	}
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return err
	}
	resultInline, resultReference := runtimeValueColumns(prepared.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status=?,result_inline=?,result_ref=?,updated_at=? WHERE id=? AND status='running'`,
		completion.Status, resultInline, resultReference, stamp, completion.Admission.Request.OperationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE leases SET status=?,updated_at=? WHERE id=? AND status='running'`, completion.Status, stamp, completion.LeaseID); err != nil {
		return err
	}
	if err := settleCapabilityBudgets(ctx, tx, stored.Request.RootID, stored.Request.AgentID, stored.Request.Reservations, completion.Usage); err != nil {
		return err
	}
	return tx.Commit()
}

func validateCapabilityAgent(ctx context.Context, tx *sql.Tx, rootID, agentID string) error {
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM agents WHERE root_id=? AND id=?`, rootID, agentID).Scan(&status); err != nil {
		return capability.ErrDenied
	}
	if slices.Contains([]string{"stopped", "failed", "cancelled", "interrupted", "deleted"}, status) {
		return capability.ErrDenied
	}
	return nil
}

func validateCapabilityAdmission(ctx context.Context, tx *sql.Tx, admission capability.Admission) error {
	grant, err := loadCapabilityGrant(ctx, tx, admission.Request.RootID, admission.Request.AgentID, admission.Request.CapabilityID, admission.Request.CapabilityGeneration)
	if err != nil {
		return err
	}
	if !slices.Contains(grant.operations, admission.Request.Operation) {
		return capability.ErrDenied
	}
	if admission.CanonicalPath != "" && !scopeContains(grant.scopes.Paths, admission.CanonicalPath) {
		return capability.ErrDenied
	}
	if admission.Mutation != capability.MutationWorkspace {
		return nil
	}
	if admission.Request.WriterCapabilityID == "" || admission.Request.WriterCapabilityID == admission.Request.CapabilityID {
		return capability.ErrDenied
	}
	writer, err := loadCapabilityGrant(ctx, tx, admission.Request.RootID, admission.Request.AgentID,
		admission.Request.WriterCapabilityID, admission.Request.WriterCapabilityGeneration)
	if err != nil {
		return err
	}
	if !slices.Contains(writer.operations, "workspace.write") || !slices.Contains(writer.scopes.Paths, admission.CanonicalRoot) {
		return capability.ErrDenied
	}
	return nil
}

type loadedCapability struct {
	operations []string
	scopes     storedCapabilityScopes
}

func loadCapabilityGrant(ctx context.Context, tx *sql.Tx, rootID, agentID, capabilityID string, generation int64) (loadedCapability, error) {
	var operationsJSON, scopesJSON []byte
	var currentGeneration int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT operations,scopes,generation,status FROM capabilities WHERE id=? AND root_id=? AND agent_id=?`,
		capabilityID, rootID, agentID).Scan(&operationsJSON, &scopesJSON, &currentGeneration, &status); err != nil {
		return loadedCapability{}, capability.ErrDenied
	}
	if status != "active" || currentGeneration != generation {
		return loadedCapability{}, capability.ErrDenied
	}
	var grant loadedCapability
	if err := json.Unmarshal(operationsJSON, &grant.operations); err != nil {
		return loadedCapability{}, err
	}
	if err := json.Unmarshal(scopesJSON, &grant.scopes); err != nil {
		return loadedCapability{}, err
	}
	if grant.scopes.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, grant.scopes.ExpiresAt)
		if err != nil || !expiresAt.After(time.Now()) {
			return loadedCapability{}, capability.ErrDenied
		}
	}
	return grant, nil
}

func insertCapabilityLease(ctx context.Context, tx *sql.Tx, leaseID string, admission capability.Admission, stamp string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO leases(id,root_id,agent_id,operation_id,capability_id,trace_id,command_client_id,command_id,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, leaseID, admission.Request.RootID, admission.Request.AgentID, admission.Request.OperationID,
		admission.Request.CapabilityID, admission.Request.TraceID, admission.Request.CommandClientID, admission.Request.CommandID, "running", stamp, stamp)
	return err
}

func (s *Store) commitDeniedAdmission(ctx context.Context, tx *sql.Tx, prepared preparedRuntimeValue, admission capability.Admission, denial error) error {
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id,root_id,agent_id,command_client_id,command_id,status,payload_inline,payload_ref,result_inline,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, admission.Request.OperationID, admission.Request.RootID, admission.Request.AgentID,
		admission.Request.CommandClientID, admission.Request.CommandID, capability.StatusDenied, inline, reference, []byte(denial.Error()), stamp, stamp); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %v", capability.ErrDenied, denial)
}

func terminalizePermission(ctx context.Context, tx *sql.Tx, admission capability.Admission, permissionID, status, principal, reason string) error {
	stamp := now()
	if _, err := tx.ExecContext(ctx, `UPDATE permission_requests SET status=?,updated_at=? WHERE id=? AND status='pending'`,
		permissionStatusValue(status, principal), stamp, permissionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status=?,result_inline=?,updated_at=? WHERE id=? AND status='waiting'`,
		capability.StatusDenied, []byte(reason), stamp, admission.Request.OperationID); err != nil {
		return err
	}
	return releaseCapabilityBudgets(ctx, tx, admission.Request.RootID, admission.Request.AgentID, admission.Request.Reservations)
}

func (s *Store) denyStalePermission(ctx context.Context, tx *sql.Tx, admission capability.Admission, permissionID string, decision capability.Decision, denial error) error {
	if err := terminalizePermission(ctx, tx, admission, permissionID, "denied", decision.PrincipalID, denial.Error()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %v", capability.ErrDenied, denial)
}

func permissionStatusValue(status, principal string) string {
	if principal == "" {
		return status
	}
	return status + "/" + principal
}

func scopeContains(scopes []string, path string) bool {
	for _, scope := range scopes {
		rel, err := filepath.Rel(scope, path)
		if err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Store) readRuntimeValue(ctx context.Context, inline []byte, reference sql.NullString) ([]byte, error) {
	if !reference.Valid {
		return inline, nil
	}
	var digest string
	var size int64
	if err := s.db.QueryRowContext(ctx, `SELECT digest,size FROM content_references WHERE id=?`, reference.String).Scan(&digest, &size); err != nil {
		return nil, err
	}
	return s.readContentBody(digest, size)
}

func (s *Store) readRuntimeValueTx(ctx context.Context, tx *sql.Tx, inline []byte, reference sql.NullString) ([]byte, error) {
	if !reference.Valid {
		return inline, nil
	}
	var digest string
	var size int64
	if err := tx.QueryRowContext(ctx, `SELECT digest,size FROM content_references WHERE id=?`, reference.String).Scan(&digest, &size); err != nil {
		return nil, err
	}
	return s.readContentBody(digest, size)
}

func (s *Store) readContentBody(digest string, size int64) ([]byte, error) {
	data := make([]byte, 0, size)
	for offset := int64(0); offset < size; {
		chunk, err := s.content.Read(digest, offset, MaxContentRead)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			return nil, errors.New("content body ended early")
		}
		data = append(data, chunk...)
		offset += int64(len(chunk))
	}
	return data, nil
}

func sameCapabilityAdmission(a, b capability.Admission) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && bytes.Equal(aJSON, bJSON)
}
