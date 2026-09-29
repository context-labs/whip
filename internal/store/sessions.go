package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/context-labs/whip/internal/session"
)

type CreateTree struct {
	PermissionMode   *session.PermissionMode
	Metadata         session.TreeMetadata
	Engine           session.Engine
	Resources        []session.ResourceLimit
	Definition       session.DefinitionRef
	Defaults         session.Configuration
	Overrides        session.ConfigPatch
	WorkingDirectory string
}

func validMetadata(metadata session.TreeMetadata) error {
	if metadata.Title != nil {
		return session.ValidateText(*metadata.Title, 1024)
	}
	return nil
}

// CreateTree is the fresh-identity convenience for trusted in-process callers.
// Public delivery uses CreateRoot with an identity persisted by the caller.
func (s *Store) CreateTree(ctx context.Context, request CreateTree) (session.Tree, session.Session, error) {
	result, err := s.CreateRoot(ctx, session.TreeCreationRequest{
		ID: session.CreationID(newID("creation")), Metadata: request.Metadata, Engine: request.Engine,
		Resources: request.Resources, Definition: request.Definition, Overrides: request.Overrides,
		WorkingDirectory: request.WorkingDirectory, PermissionMode: request.PermissionMode,
	}, session.TreeCreationDefaults{Configuration: request.Defaults})
	if err != nil {
		return session.Tree{}, session.Session{}, err
	}
	return *result.Tree, *result.Root, nil
}

func insertSession(ctx context.Context, tx *sql.Tx, tree session.TreeID, parent *session.SessionID, ref session.DefinitionRef, config session.Configuration, cwd string) (session.Session, error) {
	if err := validateBindingSources(ctx, tx, config); err != nil {
		return session.Session{}, err
	}
	if !filepath.IsAbs(cwd) || session.ValidateText(cwd, 4096) != nil {
		return session.Session{}, fmt.Errorf("%w: working directory must be an absolute path", session.ErrInvalid)
	}
	for _, child := range config.Children {
		if _, err := definition(ctx, tx, child); err != nil {
			return session.Session{}, fmt.Errorf("child definition: %w", err)
		}
	}
	raw, err := encode(config)
	if err != nil {
		return session.Session{}, err
	}
	id := session.SessionID(newID("session"))
	created := now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO sessions (id,tree_id,parent_id,definition_id,definition_revision,config_revision,working_directory,lifecycle,created_at) VALUES (?,?,?,?,?,1,?,'active',?)", id, tree, parent, ref.ID, ref.Revision, filepath.Clean(cwd), created); err != nil {
		return session.Session{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO session_configurations VALUES (?,1,?,?)", id, raw, created); err != nil {
		return session.Session{}, err
	}
	return readSession(ctx, tx, id)
}

type SpawnSession struct {
	ParentID session.SessionID `json:"parent_id"`
	// Nil inherits the parent's effective configuration and definition origin.
	// A specified revision applies its defaults before explicit overrides.
	Definition       *session.DefinitionRef `json:"definition"`
	Overrides        session.ConfigPatch    `json:"overrides"`
	WorkingDirectory string                 `json:"working_directory"`
}

// ChildRequest preserves the original request for idempotency. Nil GrantIDs
// inherits live standing grants; an explicit empty slice delegates none.
type ChildRequest struct {
	SpawnSession
	Parts     []session.Part          `json:"parts"`
	GrantIDs  []session.GrantID       `json:"grant_ids"`
	Budgets   []session.BudgetLimit   `json:"budgets"`
	Resources []session.ResourceLimit `json:"resources"`
}

// ChildAdmission projects the child from its input. A deleted receipt has no child.
type ChildAdmission struct {
	Session   *session.Session
	Admission Admission
}

func spawnSession(ctx context.Context, tx *sql.Tx, request SpawnSession, captured *session.Configuration) (session.Session, error) {
	parent, err := readSession(ctx, tx, request.ParentID)
	if err != nil {
		return session.Session{}, err
	}
	if parent.Lifecycle != session.Active {
		return session.Session{}, ErrStopped
	}
	if captured != nil {
		parent.Config = captured.Clone()
	}
	resolved, err := resolveChild(ctx, tx, parent, request)
	if err != nil {
		return session.Session{}, err
	}
	child, err := insertSession(ctx, tx, parent.TreeID, &parent.ID, resolved.Definition, resolved.Configuration, resolved.WorkingDirectory)
	if err != nil {
		return child, err
	}
	if err := reserveCompletion(ctx, tx, parent.ID, child.ID); err != nil {
		return child, err
	}
	return child, checkResources(ctx, tx, child.ID, session.ResourceDepth, session.ResourceDescendants)
}

func (s *Store) SpawnChild(ctx context.Context, identity session.RequestIdentity, request ChildRequest) (result ChildAdmission, err error) {
	if err := validatePublicIdentity(identity); err != nil {
		return result, err
	}
	if err := validateChildRequest(identity, request); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = spawnChild(ctx, tx, identity, request, nil)
		return err
	})
	return
}

func validateChildRequest(identity session.RequestIdentity, request ChildRequest) error {
	for _, id := range []string{identity.ClientID, identity.RequestID, string(request.ParentID)} {
		if err := session.ValidateID(id); err != nil {
			return err
		}
	}
	if err := session.ValidateInputParts(request.Parts); err != nil {
		return err
	}
	if len(request.GrantIDs) > session.MaxGrantsPerSession {
		return ErrLimit
	}
	seen := make(map[session.GrantID]bool, len(request.GrantIDs))
	for _, id := range request.GrantIDs {
		if err := session.ValidateID(string(id)); err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("%w: duplicate delegated grant", session.ErrInvalid)
		}
		seen[id] = true
	}
	if err := session.ValidateResourceLimits(request.Resources); err != nil {
		return err
	}
	seenBudgets := make(map[session.BudgetKind]bool, len(request.Budgets))
	for _, limit := range request.Budgets {
		if err := limit.Validate(); err != nil {
			return err
		}
		if seenBudgets[limit.Kind] {
			return fmt.Errorf("%w: duplicate child budget", session.ErrInvalid)
		}
		seenBudgets[limit.Kind] = true
	}
	return nil
}

func readChildAdmission(ctx context.Context, tx *sql.Tx, identity session.RequestIdentity) (result ChildAdmission, err error) {
	result.Admission, err = readAdmission(ctx, tx, identity)
	if err != nil || result.Admission.Input == nil {
		return result, err
	}
	child, err := readSession(ctx, tx, result.Admission.Input.SessionID)
	if err != nil {
		return result, err
	}
	result.Session = &child
	return result, nil
}

func spawnChild(ctx context.Context, tx *sql.Tx, identity session.RequestIdentity, request ChildRequest, captured *session.Configuration) (ChildAdmission, error) {
	digest, err := requestDigest("spawn_child", request)
	if err != nil {
		return ChildAdmission{}, err
	}
	receipt, err := readReceipt(ctx, tx, identity)
	if err == nil {
		if receipt.Digest != digest {
			return ChildAdmission{}, ErrConflict
		}
		return readChildAdmission(ctx, tx, identity)
	}
	if !errors.Is(err, ErrNotFound) {
		return ChildAdmission{}, err
	}
	issuers, err := delegatedGrants(ctx, tx, request.ParentID, request.GrantIDs)
	if err != nil {
		return ChildAdmission{}, err
	}
	child, err := spawnSession(ctx, tx, request.SpawnSession, captured)
	if err != nil {
		return ChildAdmission{}, err
	}
	for _, limit := range request.Resources {
		if _, err := setResource(ctx, tx, child.ID, 0, limit); err != nil {
			return ChildAdmission{}, err
		}
	}
	for _, limit := range request.Budgets {
		if _, err := setBudget(ctx, tx, child.ID, 0, limit); err != nil {
			return ChildAdmission{}, err
		}
	}
	for _, issuer := range issuers {
		grant := session.Grant{
			ID: session.GrantID(newID("grant")), SessionID: child.ID,
			Capability: issuer.Capability, Resource: issuer.Resource, IssuerID: &issuer.ID,
		}
		if err := insertGrant(ctx, tx, grant); err != nil {
			return ChildAdmission{}, err
		}
	}
	parts, err := shareChildContent(ctx, tx, request.ParentID, child.ID, request.Parts)
	if err != nil {
		return ChildAdmission{}, err
	}
	if err := session.ValidateInputParts(parts); err != nil {
		return ChildAdmission{}, err
	}
	admission, err := admitInput(ctx, tx, identity, digest, Submission{SessionID: child.ID, Source: session.AgentInput, Parts: parts})
	if err != nil {
		return ChildAdmission{}, err
	}
	if err := chargeWrite(ctx, tx, request.ParentID, "input", string(admission.Input.ID), 0, inputWriteBytes(request.Parts)); err != nil {
		return ChildAdmission{}, err
	}
	return ChildAdmission{Session: &child, Admission: admission}, nil
}

// SpawnChildOperation applies only persisted, authorized agents.spawn intent.
// Dispatch, all child rows, the receipt and success are committed together.
func (s *Store) SpawnChildOperation(ctx context.Context, id session.OperationID) (result ChildAdmission, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.Capability != "agents.spawn" {
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, operation.SessionID)
		if err != nil {
			return err
		}
		if operation.Resource != string(owner.TreeID) {
			return ErrConflict
		}
		identity := session.RequestIdentity{ClientID: "operation", RequestID: string(id)}
		if operation.State == session.OperationSucceeded {
			result, err = readChildAdmission(ctx, tx, identity)
			return err
		}
		var request ChildRequest
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return err
		}
		if request.ParentID != owner.ID {
			return ErrConflict
		}
		if err := validateChildRequest(identity, request); err != nil {
			return err
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		var revision session.Revision
		if err := tx.QueryRowContext(ctx, "SELECT t.config_revision FROM cells c JOIN turns t ON t.id=c.turn_id WHERE c.id=?", operation.CellID).Scan(&revision); err != nil {
			return found(err)
		}
		captured, err := readConfiguration(ctx, tx, owner.ID, revision)
		if err != nil {
			return err
		}
		result, err = spawnChild(ctx, tx, identity, request, &captured)
		if err != nil {
			return err
		}
		if result.Session == nil || result.Admission.Input == nil {
			return ErrConflict
		}
		value, err := json.Marshal(struct {
			SessionID session.SessionID `json:"session_id"`
			InputID   session.InputID   `json:"input_id"`
		}{result.Session.ID, result.Admission.Input.ID})
		if err != nil {
			return err
		}
		operation.State = session.OperationDispatched
		_, err = settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationSucceeded, Value: value})
		return err
	})
	return
}

const sessionSelect = `SELECT s.id,s.tree_id,s.parent_id,s.definition_id,s.definition_revision,
 s.config_revision,s.history_revision,s.working_directory,s.lifecycle,s.created_at,c.configuration
 FROM sessions s JOIN session_configurations c ON c.session_id=s.id AND c.revision=s.config_revision`

type scanner interface{ Scan(...any) error }

func scanSession(row scanner) (result session.Session, err error) {
	var raw string
	var created int64
	err = row.Scan(&result.ID, &result.TreeID, &result.ParentID, &result.Definition.ID, &result.Definition.Revision,
		&result.ConfigRevision, &result.HistoryRevision, &result.WorkingDirectory, &result.Lifecycle, &created, &raw)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	err = json.Unmarshal([]byte(raw), &result.Config)
	return
}

func readSession(ctx context.Context, q querier, id session.SessionID) (session.Session, error) {
	return scanSession(q.QueryRowContext(ctx, sessionSelect+" WHERE s.id=?", id))
}

func (s *Store) Session(ctx context.Context, id session.SessionID) (session.Session, error) {
	return readSession(ctx, s.db, id)
}

func (s *Store) Root(ctx context.Context, id session.TreeID) (session.Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, sessionSelect+" WHERE s.tree_id=? AND s.parent_id IS NULL", id))
}

func (s *Store) Sessions(ctx context.Context, tree session.TreeID, after session.SessionID, limit int) ([]session.Session, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, sessionSelect+" WHERE s.tree_id=? AND s.id>? ORDER BY s.id LIMIT ?", tree, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Session{}
	size := 0
	for rows.Next() {
		value, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(value)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func readTree(ctx context.Context, q querier, id session.TreeID) (result session.Tree, err error) {
	var metadata string
	var created int64
	err = q.QueryRowContext(ctx, "SELECT id,metadata,engine,revision,created_at FROM session_trees WHERE id=?", id).
		Scan(&result.ID, &metadata, &result.Engine, &result.Revision, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	if err = json.Unmarshal([]byte(metadata), &result.Metadata); err != nil {
		return
	}
	return
}

func (s *Store) Tree(ctx context.Context, id session.TreeID) (session.Tree, error) {
	return readTree(ctx, s.db, id)
}

func (s *Store) UpdateTree(ctx context.Context, id session.TreeID, expected session.Revision, metadata session.TreeMetadata) (result session.Tree, err error) {
	if err := validMetadata(metadata); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readTree(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Revision != expected || expected == math.MaxInt64 {
			return ErrConflict
		}
		raw, err := encode(metadata)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE session_trees SET metadata=?,revision=revision+1 WHERE id=?", raw, id); err != nil {
			return err
		}
		var rootID session.SessionID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM sessions WHERE tree_id=? AND parent_id IS NULL", id).Scan(&rootID); err != nil {
			return err
		}
		root, err := readSession(ctx, tx, rootID)
		if err != nil {
			return err
		}
		if err := initializeTitle(ctx, tx, root, nil, "manual"); err != nil {
			return err
		}
		if err := bumpTreeCatalog(ctx, tx); err != nil {
			return err
		}
		result, err = readTree(ctx, tx, id)
		return err
	})
	return
}

func (s *Store) Configuration(ctx context.Context, id session.SessionID, revision session.Revision) (session.Configuration, error) {
	return readConfiguration(ctx, s.db, id, revision)
}

func (s *Store) UpdateConfiguration(ctx context.Context, id session.SessionID, expected session.Revision, patch session.ConfigPatch) (result session.Session, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readSession(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.ConfigRevision != expected || expected == math.MaxInt64 {
			return ErrConflict
		}
		config, err := session.Resolve(current.Config, session.DefinitionDocument{}, patch)
		if err != nil {
			return err
		}
		initial, err := readConfiguration(ctx, tx, id, 1)
		if err != nil {
			return err
		}
		if err := session.UpdateBindings(initial, config); err != nil {
			return err
		}
		if err := validateBindingSources(ctx, tx, config); err != nil {
			return err
		}
		for _, child := range config.Children {
			if _, err := definition(ctx, tx, child); err != nil {
				return err
			}
		}
		raw, err := encode(config)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_configurations VALUES (?,?,?,?)", id, expected+1, raw, now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET config_revision=? WHERE id=?", expected+1, id); err != nil {
			return err
		}
		result, err = readSession(ctx, tx, id)
		return err
	})
	return
}

const sessionAncestry = `WITH RECURSIVE ancestors(id,parent_id,depth) AS (
 SELECT id,parent_id,0 FROM sessions WHERE id=? UNION ALL
 SELECT s.id,s.parent_id,a.depth+1 FROM sessions s JOIN ancestors a ON a.parent_id=s.id
) `

func sessionAncestors(ctx context.Context, q querier, owner session.SessionID) ([]session.SessionID, error) {
	rows, err := q.QueryContext(ctx, sessionAncestry+"SELECT id FROM ancestors ORDER BY depth", owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []session.SessionID
	for rows.Next() {
		var id session.SessionID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}
