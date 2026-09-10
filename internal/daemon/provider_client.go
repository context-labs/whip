package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
)

// providerCall waits for a connection but never retries an issued mutation.
// Callers can query login status after reconnect; credentials are never replayed.
func (c *RootClient) providerCall(ctx context.Context, method string, params, result any) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := c.WaitLive(ctx); err != nil {
		return err
	}
	c.mu.RLock()
	connection := c.conn
	c.mu.RUnlock()
	caller, ok := connection.(interface {
		Call(context.Context, string, any, any) error
	})
	if !ok {
		return errors.New("daemon connection does not support provider operations")
	}
	return caller.Call(ctx, method, params, result)
}

func (c *Client) ReadConfiguration(ctx context.Context) (RuntimeConfiguration, error) {
	var result RuntimeConfiguration
	err := c.Call(ctx, "config.get", struct{}{}, &result)
	return result, err
}

func (c *Client) ListProviders(ctx context.Context) (protocol.ProviderList, error) {
	return c.ListProvidersFor(ctx, "", "")
}

func (c *Client) ListProvidersFor(ctx context.Context, model, provider string) (protocol.ProviderList, error) {
	var result protocol.ProviderList
	err := c.Call(ctx, "provider.list", protocol.ProviderListParams{Model: model, Provider: provider}, &result)
	return result, err
}

func (c *Client) DiscoverProviders(ctx context.Context, model, provider string) (protocol.ProviderList, error) {
	var result protocol.ProviderList
	err := c.Call(ctx, "provider.discover", protocol.ProviderListParams{Model: model, Provider: provider}, &result)
	if failure, ok := errors.AsType[*RPCError](err); ok && failure.Code == -32601 {
		return c.ListProvidersFor(ctx, model, provider)
	}
	return result, err
}

func (c *Client) ProviderCatalogs(ctx context.Context, refresh bool) (protocol.ProviderCatalogsResult, error) {
	return c.ProviderCatalogsFor(ctx, "", refresh)
}

func (c *Client) ProviderCatalogsFor(ctx context.Context, provider string, refresh bool) (protocol.ProviderCatalogsResult, error) {
	payload, err := json.Marshal(protocol.ProviderCatalogParams{Refresh: refresh, Provider: provider})
	if err != nil {
		return protocol.ProviderCatalogsResult{}, err
	}
	response, err := c.Query(ctx, protocol.QueryParams{Operation: "provider.catalogs", Payload: payload})
	if err != nil {
		return protocol.ProviderCatalogsResult{}, err
	}
	var result protocol.ProviderCatalogsResult
	err = json.Unmarshal(response.Result, &result)
	return result, err
}

func (c *Client) UpdateConfiguration(ctx context.Context, p ConfigurationUpdate) (RuntimeConfiguration, error) {
	var result RuntimeConfiguration
	err := c.Call(ctx, "config.update", p, &result)
	return result, err
}

func (c *Client) SetProviderKey(ctx context.Context, p ProviderKeySetup) (RuntimeConfiguration, error) {
	var result RuntimeConfiguration
	err := c.Call(ctx, "provider.key.set", p, &result)
	return result, err
}

func (c *Client) BeginLogin(ctx context.Context) (ProviderLoginStatus, error) {
	return c.BeginProviderLogin(ctx, "")
}

func (c *Client) BeginProviderLogin(ctx context.Context, provider string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.Call(ctx, "provider.login.begin", protocol.ProviderLoginBeginParams{Provider: provider}, &result)
	return result, err
}

func (c *Client) LoginStatus(ctx context.Context, id string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.Call(ctx, "provider.login.status", ProviderLoginParams{FlowID: id}, &result)
	return result, err
}

func (c *Client) CancelLogin(ctx context.Context, id string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.Call(ctx, "provider.login.cancel", ProviderLoginParams{FlowID: id}, &result)
	return result, err
}

func (c *Client) SelectLoginTeam(ctx context.Context, id, teamID string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.Call(ctx, "provider.login.team.select", ProviderLoginTeamParams{FlowID: id, TeamID: teamID}, &result)
	return result, err
}

func (c *Client) SelectLoginProject(ctx context.Context, id, projectID string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.Call(ctx, "provider.login.project.select", ProviderLoginProjectParams{FlowID: id, ProjectID: projectID}, &result)
	return result, err
}

func (c *Client) CreateLoginProject(ctx context.Context, id, name string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.Call(ctx, "provider.login.project.create", ProviderLoginCreateParams{FlowID: id, Name: name}, &result)
	return result, err
}

func (c *RootClient) ReadConfiguration(ctx context.Context) (RuntimeConfiguration, error) {
	var result RuntimeConfiguration
	err := c.providerCall(ctx, "config.get", struct{}{}, &result)
	return result, err
}

func (c *RootClient) ListProviders(ctx context.Context) (protocol.ProviderList, error) {
	return c.ListProvidersFor(ctx, "", "")
}

func (c *RootClient) ListProvidersFor(ctx context.Context, model, provider string) (protocol.ProviderList, error) {
	var result protocol.ProviderList
	err := c.providerCall(ctx, "provider.list", protocol.ProviderListParams{Model: model, Provider: provider}, &result)
	return result, err
}

func (c *RootClient) DiscoverProviders(ctx context.Context, model, provider string) (protocol.ProviderList, error) {
	var result protocol.ProviderList
	err := c.providerCall(ctx, "provider.discover", protocol.ProviderListParams{Model: model, Provider: provider}, &result)
	if failure, ok := errors.AsType[*RPCError](err); ok && failure.Code == -32601 {
		return c.ListProvidersFor(ctx, model, provider)
	}
	return result, err
}

func (c *RootClient) ProviderCatalogs(ctx context.Context, refresh bool) (protocol.ProviderCatalogsResult, error) {
	return c.ProviderCatalogsFor(ctx, "", refresh)
}

func (c *RootClient) ProviderCatalogsFor(ctx context.Context, provider string, refresh bool) (protocol.ProviderCatalogsResult, error) {
	payload, err := json.Marshal(protocol.ProviderCatalogParams{Refresh: refresh, Provider: provider})
	if err != nil {
		return protocol.ProviderCatalogsResult{}, err
	}
	var response protocol.QueryResult
	err = c.providerCall(ctx, "query", protocol.QueryParams{Operation: "provider.catalogs", Payload: payload}, &response)
	if err != nil {
		return protocol.ProviderCatalogsResult{}, err
	}
	if response.Content != nil {
		return protocol.ProviderCatalogsResult{}, errors.New("provider catalog exceeds inline budget")
	}
	var result protocol.ProviderCatalogsResult
	err = json.Unmarshal(response.Result, &result)
	return result, err
}

func (c *RootClient) UpdateConfiguration(ctx context.Context, p ConfigurationUpdate) (RuntimeConfiguration, error) {
	var result RuntimeConfiguration
	err := c.providerCall(ctx, "config.update", p, &result)
	return result, err
}

func (c *RootClient) SetProviderKey(ctx context.Context, p ProviderKeySetup) (RuntimeConfiguration, error) {
	var result RuntimeConfiguration
	err := c.providerCall(ctx, "provider.key.set", p, &result)
	return result, err
}

func (c *RootClient) BeginLogin(ctx context.Context) (ProviderLoginStatus, error) {
	return c.BeginProviderLogin(ctx, "")
}

func (c *RootClient) BeginProviderLogin(ctx context.Context, provider string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.providerCall(ctx, "provider.login.begin", protocol.ProviderLoginBeginParams{Provider: provider}, &result)
	return result, err
}

func (c *RootClient) LoginStatus(ctx context.Context, id string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.providerCall(ctx, "provider.login.status", ProviderLoginParams{FlowID: id}, &result)
	return result, err
}

func (c *RootClient) CancelLogin(ctx context.Context, id string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.providerCall(ctx, "provider.login.cancel", ProviderLoginParams{FlowID: id}, &result)
	return result, err
}

func (c *RootClient) SelectLoginTeam(ctx context.Context, id, teamID string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.providerCall(ctx, "provider.login.team.select", ProviderLoginTeamParams{FlowID: id, TeamID: teamID}, &result)
	return result, err
}

func (c *RootClient) SelectLoginProject(ctx context.Context, id, projectID string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.providerCall(ctx, "provider.login.project.select", ProviderLoginProjectParams{FlowID: id, ProjectID: projectID}, &result)
	return result, err
}

func (c *RootClient) CreateLoginProject(ctx context.Context, id, name string) (ProviderLoginStatus, error) {
	var result ProviderLoginStatus
	err := c.providerCall(ctx, "provider.login.project.create", ProviderLoginCreateParams{FlowID: id, Name: name}, &result)
	return result, err
}

func (c *Client) ListLogins(ctx context.Context) (ProviderLoginList, error) {
	var result ProviderLoginList
	err := c.Call(ctx, "provider.login.list", struct{}{}, &result)
	return result, err
}

func (c *Client) ProviderStatus(ctx context.Context, provider string) (ProviderStatus, error) {
	var result ProviderStatus
	err := c.Call(ctx, "provider.status", ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *Client) LogoutProvider(ctx context.Context, provider string) (ProviderStatus, error) {
	var result ProviderStatus
	err := c.Call(ctx, "provider.logout", ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *Client) RotateProviderKey(ctx context.Context, provider string) (ProviderStatus, error) {
	var result ProviderStatus
	err := c.Call(ctx, "provider.key.rotate", ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *RootClient) ListLogins(ctx context.Context) (ProviderLoginList, error) {
	var result ProviderLoginList
	err := c.providerCall(ctx, "provider.login.list", struct{}{}, &result)
	return result, err
}

func (c *RootClient) ProviderStatus(ctx context.Context, provider string) (ProviderStatus, error) {
	var result ProviderStatus
	err := c.providerCall(ctx, "provider.status", ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *RootClient) LogoutProvider(ctx context.Context, provider string) (ProviderStatus, error) {
	var result ProviderStatus
	err := c.providerCall(ctx, "provider.logout", ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *RootClient) RotateProviderKey(ctx context.Context, provider string) (ProviderStatus, error) {
	var result ProviderStatus
	err := c.providerCall(ctx, "provider.key.rotate", ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *Client) ReadProvider(ctx context.Context, provider string) (protocol.ProviderConfiguration, error) {
	var result protocol.ProviderConfiguration
	err := c.Call(ctx, "provider.get", protocol.ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *Client) CreateProvider(ctx context.Context, p protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error) {
	var result protocol.ProviderConfiguration
	err := c.Call(ctx, "provider.create", p, &result)
	return result, err
}

func (c *Client) UpdateProvider(ctx context.Context, p protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error) {
	var result protocol.ProviderConfiguration
	err := c.Call(ctx, "provider.update", p, &result)
	return result, err
}

func (c *Client) RemoveProvider(ctx context.Context, p protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error) {
	var result protocol.ProviderRemoveResult
	err := c.Call(ctx, "provider.remove", p, &result)
	return result, err
}

func (c *Client) DisconnectProvider(ctx context.Context, p protocol.ProviderDisconnectParams) (protocol.ProviderStatus, error) {
	var result protocol.ProviderStatus
	err := c.Call(ctx, "provider.disconnect", p, &result)
	return result, err
}

func (c *RootClient) ReadProvider(ctx context.Context, provider string) (protocol.ProviderConfiguration, error) {
	var result protocol.ProviderConfiguration
	err := c.providerCall(ctx, "provider.get", protocol.ProviderNameParams{Provider: provider}, &result)
	return result, err
}

func (c *RootClient) CreateProvider(ctx context.Context, p protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error) {
	var result protocol.ProviderConfiguration
	err := c.providerCall(ctx, "provider.create", p, &result)
	return result, err
}

func (c *RootClient) UpdateProvider(ctx context.Context, p protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error) {
	var result protocol.ProviderConfiguration
	err := c.providerCall(ctx, "provider.update", p, &result)
	return result, err
}

func (c *RootClient) RemoveProvider(ctx context.Context, p protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error) {
	var result protocol.ProviderRemoveResult
	err := c.providerCall(ctx, "provider.remove", p, &result)
	return result, err
}

func (c *RootClient) DisconnectProvider(ctx context.Context, p protocol.ProviderDisconnectParams) (protocol.ProviderStatus, error) {
	var result protocol.ProviderStatus
	err := c.providerCall(ctx, "provider.disconnect", p, &result)
	return result, err
}
