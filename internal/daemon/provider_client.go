package daemon

import (
	"context"
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
