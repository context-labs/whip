package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

func browserOwner(ctx context.Context, q querier, spec session.OperationSpec, owner session.SessionID, tree session.TreeID, revision session.Revision) error {
	actual, turn, err := operationOwnerLive(ctx, q, spec)
	if err != nil {
		return err
	}
	var actualTree session.TreeID
	var actualRevision session.Revision
	if err := q.QueryRowContext(ctx, `SELECT s.tree_id,t.config_revision FROM turns t JOIN sessions s ON s.id=t.session_id WHERE t.id=?`, turn).Scan(&actualTree, &actualRevision); err != nil {
		return found(err)
	}
	if actual != owner || actualTree != tree || actualRevision != revision {
		return ErrConflict
	}
	configuration, err := readConfiguration(ctx, q, owner, revision)
	if err != nil {
		return err
	}
	if !slices.Contains(configuration.Modules, "browser") {
		return ErrConflict
	}
	return nil
}

func validateBrowserCatalog(ctx context.Context, q querier, spec session.OperationSpec) error {
	var request session.BrowserCatalogRequest
	decoder := json.NewDecoder(bytes.NewReader(spec.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	if request.Action != "list_tabs" || spec.Resource != string(request.TreeID) {
		return session.ErrInvalid
	}
	return browserOwner(ctx, q, spec, request.SessionID, request.TreeID, request.ConfigRevision)
}

func browserIntent(ctx context.Context, q querier, spec session.OperationSpec) (session.BrowserIntent, error) {
	var request session.BrowserIntent
	decoder := json.NewDecoder(bytes.NewReader(spec.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	if spec.Capability != "browser.control" || spec.Resource != request.Scope.Resource() {
		return request, ErrConflict
	}
	if spec.DirectTurnID != "" {
		_, accepted, err := directOperationLive(ctx, q, spec.DirectTurnID)
		if err != nil {
			return request, err
		}
		if accepted.Module != "browser" || accepted.Name != request.Kind {
			return request, ErrConflict
		}
	}
	return request, browserOwner(ctx, q, spec, request.SessionID, request.TreeID, request.ConfigRevision)
}

// PublishBrowserControl publishes standing authority only after a native
// lifecycle acknowledgement. It does not settle the operation: the caller must
// still activate its connection-bound handle. Any uncertain publication retires
// that handle; neither SQL facts nor a checkpoint may restore control.
func (s *Store) PublishBrowserControl(ctx context.Context, id session.OperationID) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		op, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if op.State != session.OperationDispatched {
			return ErrConflict
		}
		intent, err := browserIntent(ctx, tx, op.OperationSpec)
		if err != nil {
			return err
		}
		if err := authorizeOperation(ctx, tx, op); err != nil {
			return err
		}
		if intent.Kind == "detach" {
			return retireBrowserGrants(ctx, tx, intent.SessionID, op.Resource)
		}
		if intent.Kind != "open" && intent.Kind != "attach" && intent.Kind != "allow_preview_port" {
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, intent.SessionID)
		if err != nil {
			return err
		}
		// Children receive existing exact standing authority by explicit transfer;
		// creating or widening native scope never manufactures an issuer chain.
		if owner.ParentID != nil {
			return ErrConflict
		}
		if intent.PreviousResource != "" && intent.PreviousResource != op.Resource {
			if err := retireBrowserGrants(ctx, tx, owner.ID, intent.PreviousResource); err != nil {
				return err
			}
		}
		if _, err := matchingGrant(ctx, tx, owner.ID, "browser.control", op.Resource); err == nil {
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		sum := sha256.Sum256([]byte(string(id) + "\x00" + op.Resource))
		grant := session.Grant{ID: session.GrantID("browser_" + hex.EncodeToString(sum[:])), SessionID: owner.ID, Capability: "browser.control", Resource: op.Resource}
		if _, err := readGrant(ctx, tx, grant.ID); err == nil {
			return ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		return insertGrant(ctx, tx, grant)
	})
}

func retireBrowserGrants(ctx context.Context, tx *sql.Tx, owner session.SessionID, resource string) error {
	for {
		grant, err := scanGrant(tx.QueryRowContext(ctx, grantSelect+` WHERE session_id=? AND capability='browser.control' AND resource=? AND operation_id IS NULL AND revoked_at IS NULL ORDER BY id LIMIT 1`, owner, resource))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := revokeGrant(ctx, tx, grant.ID); err != nil {
			return err
		}
	}
}
