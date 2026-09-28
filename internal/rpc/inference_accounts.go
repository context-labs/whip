package rpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/protocol"
)

func dispatchInferenceAccount(ctx context.Context, host HostServices, method string, raw json.RawMessage) (any, error) {
	s := host.Inference
	if s == nil {
		return nil, ErrMethod
	}
	switch method {
	case "accounts.inference.begin", "accounts.inference.rotate":
		var value inferenceaccount.Flow
		var err error
		if method == "accounts.inference.begin" {
			value, err = s.Begin(ctx)
		} else {
			value, err = s.Rotate(ctx)
		}
		return inferenceFlow(value), err
	case "accounts.inference.get", "accounts.inference.cancel", "accounts.inference.retry":
		return decode(raw, func(p protocol.InferenceFlowParams) (any, error) {
			var value inferenceaccount.Flow
			var err error
			switch method {
			case "accounts.inference.get":
				value, err = s.Get(ctx, p.FlowID)
			case "accounts.inference.cancel":
				value, err = s.Cancel(ctx, p.FlowID)
			default:
				value, err = s.Retry(ctx, p.FlowID)
			}
			return inferenceFlow(value), err
		})
	case "accounts.inference.list":
		values, err := s.List(ctx)
		result := protocol.InferenceFlowsResult{Items: []protocol.InferenceFlow{}}
		for _, value := range values {
			result.Items = append(result.Items, inferenceFlow(value))
		}
		return result, err
	case "accounts.inference.team":
		return decode(raw, func(p protocol.InferenceTeamParams) (any, error) {
			value, err := s.SelectTeam(ctx, p.FlowID, p.TeamID)
			return inferenceFlow(value), err
		})
	case "accounts.inference.project":
		return decode(raw, func(p protocol.InferenceProjectParams) (any, error) {
			value, err := s.SelectProject(ctx, p.FlowID, p.ProjectID)
			return inferenceFlow(value), err
		})
	case "accounts.inference.create_project":
		return decode(raw, func(p protocol.InferenceCreateProjectParams) (any, error) {
			value, err := s.CreateProject(ctx, p.FlowID, p.Name)
			return inferenceFlow(value), err
		})
	case "accounts.inference.status", "accounts.inference.setup":
		var value inferenceaccount.Status
		var err error
		if method == "accounts.inference.status" {
			value, err = s.Status(ctx)
		} else {
			value, err = s.Setup(ctx)
		}
		return inferenceStatus(ctx, host, value), err
	case "accounts.inference.logout":
		value, err := s.Logout(ctx)
		return protocol.InferenceLogoutResult{
			Status: inferenceStatus(ctx, host, value.Status), LocalFailure: optionalText(value.LocalFailure),
			CleanupFailure: optionalText(value.CleanupFailure), Cleanup: inferenceCleanup(value.Cleanup),
		}, err
	case "accounts.inference.cleanup", "accounts.inference.retry_cleanup":
		var values []inferenceaccount.Cleanup
		var err error
		if method == "accounts.inference.cleanup" {
			values, err = s.ListCleanup(ctx)
		} else {
			values, err = s.RetryCleanup(ctx)
		}
		result := protocol.InferenceCleanupResult{Items: inferenceCleanup(values)}
		// An accepted cleanup returns its retained outcomes even when remote
		// cleanup failed. Pre-admission errors have no projection and stay errors.
		if err != nil && values != nil {
			result.Failure = new("Remote cleanup remains unconfirmed; inspect retained outcomes")
			return result, nil
		}
		return result, err
	default:
		return nil, ErrMethod
	}
}

func inferenceFlow(value inferenceaccount.Flow) protocol.InferenceFlow {
	result := protocol.InferenceFlow{
		ID: value.ID, Kind: optionalText(value.Kind), State: string(value.State),
		VerificationURL: optionalText(value.VerificationURL), UserCode: optionalText(value.UserCode),
		ExpiresAt: accountTime(value.ExpiresAt), Teams: []protocol.InferenceTeam{}, Projects: []protocol.InferenceProject{},
		TeamID: optionalText(value.TeamID), ProjectID: optionalText(value.ProjectID), Failure: optionalText(value.Failure),
	}
	for _, team := range value.Teams {
		result.Teams = append(result.Teams, protocol.InferenceTeam{ID: team.ID, Name: team.Name, Slug: team.Slug})
	}
	for _, project := range value.Projects {
		result.Projects = append(result.Projects, protocol.InferenceProject{ID: project.ID, Name: project.Name})
	}
	return result
}

func inferenceStatus(ctx context.Context, host HostServices, value inferenceaccount.Status) protocol.InferenceAccountStatus {
	result := protocol.InferenceAccountStatus{
		ManagementState: value.ManagementState, InferenceState: value.InferenceState, RouteState: "unavailable",
		UserID: optionalText(value.UserID), Email: optionalText(value.Email), TeamID: optionalText(value.TeamID),
		TeamName: optionalText(value.TeamName), ProjectID: optionalText(value.ProjectID), ProjectName: optionalText(value.ProjectName),
		Failure: optionalText(value.Failure), CleanupPending: value.CleanupPending,
	}
	if value.ExpiresAt != nil {
		result.ExpiresAt = accountTime(*value.ExpiresAt)
	}
	if host.Config != nil {
		current, err := host.Config.Snapshot(ctx)
		if err == nil {
			if _, exists := current.Host.Providers["inference-net"]; !exists {
				result.RouteState = "missing"
			} else if current.Host.EnsureInference() != nil {
				result.RouteState = "conflict"
			} else {
				result.RouteState = "configured"
			}
		}
	}
	return result
}

func inferenceCleanup(values []inferenceaccount.Cleanup) []protocol.InferenceCleanup {
	result := make([]protocol.InferenceCleanup, 0, len(values))
	for _, value := range values {
		result = append(result, protocol.InferenceCleanup{
			ID: value.ID, ExpiresAt: protocol.AccountTimestamp(value.ExpiresAt.UTC().Format(time.RFC3339Nano)),
			TeamID: optionalText(value.TeamID), KeyID: optionalText(value.KeyID), KeyState: value.KeyState,
			SessionState: value.SessionState, Failure: optionalText(value.Failure),
		})
	}
	return result
}
