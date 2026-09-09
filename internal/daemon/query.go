package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func (s *Session) QueryClient(ctx context.Context, operation string, payload json.RawMessage) (string, error) {
	if operation == "history.user.list" {
		history, err := s.store.UserHistoryContext(ctx, 500)
		return marshalClientOutput(history, err)
	}
	if operation == "agents.list" {
		agents, err := s.store.RootAgentViews(ctx, s.authority.RootID)
		return marshalClientOutput(agents, err)
	}
	if operation == "browser.status" {
		manager, err := routeControlValue(s, ctx, func(context.Context) (*browser.Manager, error) {
			runner, ok := s.runner.(*AgentSession)
			if !ok {
				return nil, nil //nolint:nilnil // non-agent sessions have no optional browser manager
			}
			return runner.browserManager(), nil
		})
		if err != nil {
			return "", err
		}
		if manager == nil {
			return marshalClientOutput(protocol.BrowserStatusResult{}, nil)
		}
		// Query integration state outside the actor after capturing its owner.
		return marshalClientOutput(protocol.BrowserStatusResult{Enabled: true, Driver: manager.Driver()}, nil)
	}
	if operation == "provider.catalogs" {
		return queryProviderCatalogs(ctx, s.providers, payload)
	}
	return routeControlValue(s, ctx, func(actorCtx context.Context) (string, error) {
		// Query cancellation belongs to the connection/request, not to the root.
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return s.applyClientCommand(ctx, operation, payload)
	})
}

func (s *Server) query(ctx context.Context, params protocol.QueryParams) (protocol.QueryResult, error) {
	operation, ok := protocol.LookupRuntime(params.Operation)
	if !ok || operation.Execution != protocol.Query {
		return protocol.QueryResult{}, rpcFailure(-32601, "operation is not a query")
	}
	if err := protocol.ValidateRuntime(params.Operation, params.Payload); err != nil {
		return protocol.QueryResult{}, rpcFailure(-32602, err.Error())
	}
	var output string
	var err error
	switch params.Operation {
	case "provider.catalogs":
		output, err = queryProviderCatalogs(ctx, s.providers, params.Payload)
	case "session.list":
		var list protocol.ListParams
		if err := json.Unmarshal(params.Payload, &list); err != nil && len(params.Payload) > 0 {
			return protocol.QueryResult{}, err
		}
		if list.Limit == 0 {
			list.Limit = 50
		}
		if list.Limit < 1 || list.Limit > 128 {
			return protocol.QueryResult{}, errors.New("session list limit must be 1..128")
		}
		metas, readErr := s.daemon.store.RecentContext(ctx, list.Limit)
		output, err = marshalClientOutput(metas, readErr)
	default:
		root, openErr := s.daemon.Open(params.RootID)
		if openErr != nil {
			return protocol.QueryResult{}, openErr
		}
		output, err = root.QueryClient(ctx, params.Operation, params.Payload)
	}
	if err != nil {
		return protocol.QueryResult{}, err
	}
	encoded := encodeCommandOutcome(params.Operation, output, nil)
	if len(encoded) <= 512<<10 {
		return protocol.QueryResult{Result: encoded}, nil
	}
	if params.RootID == "" {
		return protocol.QueryResult{}, errors.New("query exceeds inline budget; use a paginated query")
	}
	value, err := s.daemon.store.StoreContent(ctx, session.ContentGrant{RootID: params.RootID, Scope: session.ContentGrantRoot}, session.RuntimePayload{Data: encoded, MediaType: "application/json", Source: "query." + params.Operation})
	if err != nil {
		return protocol.QueryResult{}, err
	}
	return protocol.QueryResult{RootID: params.RootID, Content: &protocol.ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size, MediaType: value.MediaType, Source: value.Source}}, nil
}

func (c *Client) Query(ctx context.Context, params protocol.QueryParams) (protocol.QueryResult, error) {
	var result protocol.QueryResult
	err := c.Call(ctx, "query", params, &result)
	if err == nil && result.Content != nil {
		if result.RootID != params.RootID {
			return protocol.QueryResult{}, errors.New("query content root does not match request")
		}
		command := CommandResult{Content: result.Content, Operation: params.Operation}
		if err := c.commandContent(ctx, result.RootID, &command); err != nil {
			return protocol.QueryResult{}, err
		}
		result.Result = command.Result
	}
	return result, err
}

func queryProviderCatalogs(ctx context.Context, providers *ProviderService, payload json.RawMessage) (string, error) {
	var params protocol.ProviderCatalogParams
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &params); err != nil {
			return "", err
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return clientProviderCatalogs(bounded, providers, params.Refresh)
}
