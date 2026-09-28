package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchOperation(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "grants.create":
		return decode(raw, func(p protocol.CreateGrantParams) (any, error) {
			value, err := r.CreateGrant(ctx, session.Grant{ID: session.GrantID(p.ID), SessionID: session.SessionID(p.SessionID), Capability: p.Capability, Resource: p.Resource})
			return protocol.GrantFromDomain(value), err
		})
	case "grants.revoke":
		return decode(raw, func(p protocol.GrantParams) (any, error) {
			value, err := r.RevokeGrant(ctx, session.GrantID(p.GrantID))
			return protocol.GrantFromDomain(value), err
		})
	case "grants.list":
		return decode(raw, func(p protocol.GrantsParams) (any, error) {
			var after session.GrantID
			if p.After != nil {
				after = session.GrantID(*p.After)
			}
			values, err := r.Grants(ctx, session.SessionID(p.SessionID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.GrantsResult{Items: []protocol.Grant{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.GrantFromDomain(value))
			}
			return result, nil
		})
	case "operations.get":
		return decode(raw, func(p protocol.HostOperationParams) (any, error) {
			value, err := r.Operation(ctx, session.OperationID(p.OperationID))
			return protocol.OperationFromDomain(value), err
		})
	case "turns.operations":
		return decode(raw, func(p protocol.HostOperationsParams) (any, error) {
			var after session.OperationID
			if p.After != nil {
				after = session.OperationID(*p.After)
			}
			values, err := r.Operations(ctx, session.TurnID(p.TurnID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.HostOperationsResult{Items: []protocol.HostOperation{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.OperationFromDomain(value))
			}
			return result, nil
		})
	case "permissions.list":
		return decode(raw, func(p protocol.PermissionsParams) (any, error) {
			var after session.OperationID
			if p.After != nil {
				after = session.OperationID(*p.After)
			}
			values, err := r.Permissions(ctx, session.SessionID(p.SessionID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.PermissionsResult{Items: []protocol.Permission{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.PermissionFromDomain(value))
			}
			return result, nil
		})
	case "permissions.resolve":
		return decode(raw, func(p protocol.ResolvePermissionParams) (any, error) {
			value, err := r.ResolvePermission(ctx, session.OperationID(p.OperationID), p.Approved)
			return protocol.PermissionFromDomain(value), err
		})
	case "cells.get":
		return decode(raw, func(p protocol.CellParams) (any, error) {
			value, err := r.Cell(ctx, session.CellID(p.CellID))
			return protocol.CellFromDomain(value), err
		})
	case "turns.cells":
		return decode(raw, func(p protocol.CellsParams) (any, error) {
			var after session.CellID
			if p.After != nil {
				after = session.CellID(*p.After)
			}
			values, err := r.Cells(ctx, session.TurnID(p.TurnID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.CellsResult{Items: []protocol.Cell{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.CellFromDomain(value))
			}
			return result, nil
		})
	default:
		return nil, ErrMethod
	}
}
