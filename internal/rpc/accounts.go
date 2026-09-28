package rpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/protocol"
)

func dispatchAccount(ctx context.Context, accounts *account.Service, method string, raw json.RawMessage) (any, error) {
	if accounts == nil {
		return nil, ErrMethod
	}
	switch method {
	case "accounts.openai.begin":
		value, err := accounts.Begin(ctx)
		return accountFlow(value), err
	case "accounts.openai.get", "accounts.openai.cancel":
		return decode(raw, func(params protocol.OpenAIFlowParams) (any, error) {
			var value account.Flow
			var err error
			if method == "accounts.openai.get" {
				value, err = accounts.Get(ctx, params.FlowID)
			} else {
				value, err = accounts.Cancel(ctx, params.FlowID)
			}
			return accountFlow(value), err
		})
	case "accounts.openai.list":
		values, err := accounts.List(ctx)
		result := protocol.OpenAIFlowsResult{Items: []protocol.OpenAILoginFlow{}}
		for _, value := range values {
			result.Items = append(result.Items, accountFlow(value))
		}
		return result, err
	case "accounts.openai.status":
		value, err := accounts.Status(ctx)
		return accountStatus(value), err
	case "accounts.openai.setup":
		value, err := accounts.Setup(ctx)
		return accountStatus(value), err
	case "accounts.openai.logout":
		value, err := accounts.Logout(ctx)
		return accountStatus(value), err
	default:
		return nil, ErrMethod
	}
}

func accountFlow(value account.Flow) protocol.OpenAILoginFlow {
	return protocol.OpenAILoginFlow{
		ID: value.ID, State: string(value.State), VerificationURL: optionalText(value.VerificationURL),
		UserCode: optionalText(value.UserCode), ExpiresAt: accountTime(value.ExpiresAt), Failure: optionalText(value.Failure),
	}
}

func accountStatus(value account.Status) protocol.OpenAIAccountStatus {
	result := protocol.OpenAIAccountStatus{
		AuthState: value.AuthState, RouteState: value.RouteState, AccountID: optionalText(value.AccountID),
		Email: optionalText(value.Email), Plan: optionalText(value.Plan), Failure: optionalText(value.Failure),
	}
	if value.ExpiresAt != nil {
		result.ExpiresAt = accountTime(*value.ExpiresAt)
	}
	return result
}

func accountTime(value time.Time) *protocol.AccountTimestamp {
	if value.IsZero() {
		return nil
	}
	return new(protocol.AccountTimestamp(value.UTC().Format(time.RFC3339Nano)))
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
