package protocol

import (
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type Grant struct {
	ID          ID      `json:"id"`
	SessionID   ID      `json:"session_id"`
	Capability  string  `json:"capability"`
	Resource    string  `json:"resource"`
	OperationID *ID     `json:"operation_id"`
	IssuerID    *ID     `json:"issuer_id"`
	CreatedAt   string  `json:"created_at"`
	RevokedAt   *string `json:"revoked_at"`
}
type CreateGrantParams struct {
	ID         ID     `json:"id"`
	SessionID  ID     `json:"session_id"`
	Capability string `json:"capability"`
	Resource   string `json:"resource"`
	IssuerID   *ID    `json:"issuer_id,omitempty"`
}
type GrantParams struct {
	GrantID ID `json:"grant_id"`
}
type GrantsParams struct {
	SessionID ID  `json:"session_id"`
	After     *ID `json:"after,omitempty"`
	Limit     int `json:"limit" min:"1" max:"100"`
}
type GrantsResult struct {
	Items []Grant `json:"items"`
}
type HostOperationResult struct {
	State   string          `json:"state" enum:"succeeded,failed,denied,cancelled,uncertain"`
	Value   json.RawMessage `json:"value,omitempty"`
	Failure *string         `json:"failure,omitempty"`
}
type HostOperation struct {
	PermissionRevision *Counter             `json:"permission_revision" pattern:"^[1-9][0-9]{0,18}$"`
	ID                 ID                   `json:"id"`
	SessionID          ID                   `json:"session_id"`
	TurnID             ID                   `json:"turn_id"`
	CellID             ID                   `json:"cell_id"`
	RequestID          ID                   `json:"request_id"`
	Capability         string               `json:"capability"`
	Resource           string               `json:"resource"`
	Arguments          json.RawMessage      `json:"arguments"`
	State              string               `json:"state" enum:"waiting,ready,dispatched,succeeded,failed,denied,cancelled,uncertain"`
	GrantID            *ID                  `json:"grant_id"`
	Result             *HostOperationResult `json:"result"`
	CreatedAt          string               `json:"created_at"`
	DispatchedAt       *string              `json:"dispatched_at"`
	FinishedAt         *string              `json:"finished_at"`
}
type HostOperationParams struct {
	OperationID ID `json:"operation_id"`
}
type HostOperationsParams struct {
	TurnID ID  `json:"turn_id"`
	After  *ID `json:"after,omitempty"`
	Limit  int `json:"limit" min:"1" max:"100"`
}
type HostOperationsResult struct {
	Items []HostOperation `json:"items"`
}
type Permission struct {
	OperationID ID      `json:"operation_id"`
	State       string  `json:"state" enum:"pending,approved,denied,cancelled"`
	CreatedAt   string  `json:"created_at"`
	ResolvedAt  *string `json:"resolved_at"`
}
type PermissionsParams struct {
	SessionID ID  `json:"session_id"`
	After     *ID `json:"after,omitempty"`
	Limit     int `json:"limit" min:"1" max:"100"`
}
type PermissionsResult struct {
	Items []Permission `json:"items"`
}
type ResolvePermissionParams struct {
	OperationID ID   `json:"operation_id"`
	Approved    bool `json:"approved"`
}

func GrantFromDomain(value session.Grant) Grant {
	result := Grant{ID: ID(value.ID), SessionID: ID(value.SessionID), Capability: value.Capability, Resource: value.Resource, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), RevokedAt: timeString(value.RevokedAt)}
	if value.OperationID != nil {
		result.OperationID = new(ID(*value.OperationID))
	}
	if value.IssuerID != nil {
		result.IssuerID = new(ID(*value.IssuerID))
	}
	return result
}

func OperationFromDomain(value session.Operation) HostOperation {
	result := HostOperation{ID: ID(value.ID), SessionID: ID(value.SessionID), TurnID: ID(value.TurnID), CellID: ID(value.CellID), RequestID: ID(value.RequestID), Capability: value.Capability, Resource: value.Resource, Arguments: append(json.RawMessage(nil), value.Arguments...), State: string(value.State), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), DispatchedAt: timeString(value.DispatchedAt), FinishedAt: timeString(value.FinishedAt)}
	if value.GrantID != nil {
		result.GrantID = new(ID(*value.GrantID))
	}
	if value.PermissionRevision != nil {
		result.PermissionRevision = new(Counter(*value.PermissionRevision))
	}
	if value.Result != nil {
		result.Result = &HostOperationResult{State: string(value.Result.State), Value: append(json.RawMessage(nil), value.Result.Value...), Failure: value.Result.Failure}
	}
	return result
}

func PermissionFromDomain(value session.Permission) Permission {
	return Permission{OperationID: ID(value.OperationID), State: string(value.State), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), ResolvedAt: timeString(value.ResolvedAt)}
}
