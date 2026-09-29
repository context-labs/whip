package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// ChildPreview is an observation, never admission or a reusable grant. The
// committed spawn resolves and narrows the same request again in its transaction.
type ChildPreview struct {
	Name               string                `json:"name,omitempty"`
	Template           string                `json:"template,omitempty"`
	Definition         session.DefinitionRef `json:"definition"`
	Configuration      session.Configuration `json:"configuration"`
	WorkingDirectory   string                `json:"working_directory"`
	GrantIDs           []session.GrantID     `json:"grant_ids"`
	PermissionRevision *session.Revision     `json:"permission_revision,string"`
}

func resolveChild(ctx context.Context, q querier, parent session.Session, request SpawnSession) (ChildPreview, error) {
	ref := parent.Definition
	var document session.DefinitionDocument
	if request.Template != "" {
		if request.Definition != nil {
			return ChildPreview{}, fmt.Errorf("%w: choose a child template or definition, not both", session.ErrInvalid)
		}
		var ok bool
		ref, ok = parent.Config.Children[request.Template]
		if !ok {
			return ChildPreview{}, fmt.Errorf("%w: unknown child template %q", session.ErrInvalid, request.Template)
		}
	} else if request.Definition != nil {
		ref = *request.Definition
	}
	if request.Definition != nil || request.Template != "" {
		declared, err := definition(ctx, q, ref)
		if err != nil {
			return ChildPreview{}, err
		}
		document = declared.Document
	}
	configuration, err := session.Resolve(parent.Config, document, request.Overrides)
	if err != nil {
		return ChildPreview{}, err
	}
	configuration.Run = nil
	if err := session.NarrowBindings(parent.Config, configuration); err != nil {
		return ChildPreview{}, err
	}
	cwd := request.WorkingDirectory
	if cwd == "" {
		cwd = parent.WorkingDirectory
	}
	return ChildPreview{Name: request.Name, Template: request.Template, Definition: ref, Configuration: configuration, WorkingDirectory: cwd}, nil
}

func (s *Store) PreviewChild(ctx context.Context, cellID session.CellID, request ChildRequest) (result ChildPreview, err error) {
	return s.PreviewChildOperation(ctx, session.OperationSpec{CellID: cellID, Capability: "agents.spawn"}, request)
}

func (s *Store) PreviewChildOperation(ctx context.Context, spec session.OperationSpec, request ChildRequest) (result ChildPreview, err error) {
	if err := validateChildRequest(session.RequestIdentity{ClientID: "operation", RequestID: "preview"}, request); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		owner, turnID, err := operationOwnerLive(ctx, tx, spec)
		if err != nil {
			return err
		}
		var revision session.Revision
		if err := tx.QueryRowContext(ctx, "SELECT config_revision FROM turns WHERE id=?", turnID).Scan(&revision); err != nil {
			return found(err)
		}
		if owner != request.ParentID {
			return ErrConflict
		}
		parent, err := readSession(ctx, tx, owner)
		if err != nil {
			return err
		}
		if parent.Lifecycle != session.Active {
			return ErrStopped
		}
		parent, err = capturedSession(ctx, tx, parent, revision)
		if err != nil {
			return err
		}
		result, err = resolveChild(ctx, tx, parent, request.SpawnSession)
		if err != nil {
			return err
		}
		grants, err := delegatedGrants(ctx, tx, owner, request.GrantIDs)
		if err != nil {
			return err
		}
		result.GrantIDs = []session.GrantID{}
		for _, grant := range grants {
			result.GrantIDs = append(result.GrantIDs, grant.ID)
		}
		result.PermissionRevision, err = childPermissionRevision(ctx, tx, parent, result.WorkingDirectory, request.GrantIDs)
		return err
	})
	return
}
