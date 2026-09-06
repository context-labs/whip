import type { ConfigurationUpdate, PermissionDecision, ProviderKeySetup } from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import type { CommandOptions } from './command.js';
import { uuid } from './util.js';

export class Permissions {
  constructor(private readonly client: WhipClient) {}
  /** A decision is sent once. After an uncertain reply, inspect pending state before retrying. */
  decide(decision: Omit<PermissionDecision, 'command_id'> & { command_id?: string }, options: CallOptions = {}) {
    return this.client.call('permission.decide', { decision: { ...decision, command_id: decision.command_id ?? uuid() } }, options);
  }
  setMode(rootId: string, externalPermissions: boolean, options: Pick<CommandOptions, 'commandId'> = {}) {
    return this.client.submit('permission.mode', { external_permissions: externalPermissions }, { ...options, rootId });
  }
}

export class Configuration {
  constructor(private readonly client: WhipClient) {}
  get(options: CallOptions = {}) { return this.client.call('config.get', {}, options); }
  update(patch: ConfigurationUpdate, options: CallOptions = {}) { return this.client.call('config.update', patch, options); }
}
export class Providers {
  constructor(private readonly client: WhipClient) {}
  catalogs(options: CallOptions = {}) { return this.client.query('provider.catalogs', {}, options); }
  status(name: string, options: CallOptions = {}) { return this.client.call('provider.status', { provider: name }, options); }
  setKey(params: ProviderKeySetup, options: CallOptions = {}) { return this.client.call('provider.key.set', params, options); }
  logout(name: string, options: CallOptions = {}) { return this.client.call('provider.logout', { provider: name }, options); }
  readonly login = {
    begin: (options: CallOptions = {}) => this.client.call('provider.login.begin', {}, options),
    list: (options: CallOptions = {}) => this.client.call('provider.login.list', {}, options),
    status: (flowId: string, options: CallOptions = {}) => this.client.call('provider.login.status', { flow_id: flowId }, options),
    cancel: (flowId: string, options: CallOptions = {}) => this.client.call('provider.login.cancel', { flow_id: flowId }, options),
    selectTeam: (flowId: string, teamId: string, options: CallOptions = {}) => this.client.call('provider.login.team.select', { flow_id: flowId, team_id: teamId }, options),
    selectProject: (flowId: string, projectId: string, options: CallOptions = {}) => this.client.call('provider.login.project.select', { flow_id: flowId, project_id: projectId }, options),
    createProject: (flowId: string, name: string, options: CallOptions = {}) => this.client.call('provider.login.project.create', { flow_id: flowId, name }, options),
  };
}
