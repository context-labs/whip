import { validate, type CommandResult, type ConfigurationUpdate, type HostAttentionParams, type HostDirectoryParams, type HostDirectoryPickParams, type PermissionDecision, type PermissionDecisionResult, type ProviderCreateParams, type ProviderDisconnectParams, type ProviderKeySetup, type ProviderLoginBeginParams, type ProviderRemoveParams, type ProviderUpdateParams, type ProviderValidateParams } from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import type { CommandOptions } from './command.js';
import { WhipError } from './errors.js';

/** Host reads do not construct session actors or change client preferences. */
export class Host {
  constructor(private readonly client: WhipClient) {}
  directories(params: Partial<HostDirectoryParams> = {}, options: CallOptions = {}) {
    return this.client.call('host.directories.list', { limit: 64, ...params }, options);
  }
  /** Opens the OS folder chooser on the execution host; rejects where the host has no desktop picker. */
  pickDirectory(params: HostDirectoryPickParams = {}, options: CallOptions = {}) {
    return this.client.call('host.directory.pick', params, options);
  }
  /** Advisory paged attention index. Refresh its first page to see newly active roots. */
  attention(params: Partial<HostAttentionParams> = {}, options: CallOptions = {}) {
    return this.client.call('host.attention', { limit: 64, max_bytes: 256 << 10, ...params }, options);
  }
  readonly themes = {
    list: (options: CallOptions = {}) => this.client.call('host.themes.list', {}, options),
    resolve: (name: string, options: CallOptions = {}) => this.client.call('host.themes.resolve', { name }, options),
    resolveJSON: (json: string, options: CallOptions = {}) => this.client.call('host.themes.resolve', { json }, options),
  };
}

export type PermissionDecisionStatus = Pick<CommandResult, 'command_id' | 'ingress_seq'> & { operation: 'permission.decide' } & (
  | { status: 'queued' | 'running' | 'waiting'; result?: never; failure?: never }
  | { status: 'succeeded'; result: PermissionDecisionResult; failure?: never }
  | { status: 'failed' | 'cancelled' | 'interrupted'; result?: never; failure: NonNullable<CommandResult['failure']> }
);

export class Permissions {
  constructor(private readonly client: WhipClient) {}
  /** A decision is sent once. After an uncertain reply, inspect pending state before retrying. */
  decide(decision: Omit<PermissionDecision, 'command_id'> & { command_id?: string }, options: CallOptions = {}) {
    return this.client.call('permission.decide', { decision: { ...decision, command_id: decision.command_id ?? this.client.createId() } }, options);
  }
  /** Inspect the original client-scoped decision identity without resending it. */
  async status(commandId: string, options: CallOptions = {}): Promise<PermissionDecisionStatus> {
    if (!commandId.trim()) throw new TypeError('commandId must not be empty');
    const outcome = await this.client.call('command.status', { command_id: commandId }, options);
    if (outcome.command_id !== commandId || outcome.operation !== 'permission.decide' || outcome.content != null) {
      throw new WhipError('invalid_response', 'Permission decision status does not match its identity or operation');
    }
    switch (outcome.status) {
      case 'succeeded':
        if (outcome.failure != null) throw new WhipError('invalid_response', 'Successful permission decision contains a failure');
        if (!validate('PermissionDecisionResult', outcome.result, 'response')) throw new WhipError('invalid_response', 'Invalid successful permission decision result');
        break;
      case 'failed': case 'cancelled': case 'interrupted':
        if (outcome.result !== undefined || outcome.failure == null) throw new WhipError('invalid_response', 'Failed permission decision is missing its failure outcome');
        break;
      case 'queued': case 'running': case 'waiting':
        if (outcome.result !== undefined || outcome.failure != null) throw new WhipError('invalid_response', 'Pending permission decision contains a terminal outcome');
        break;
      default:
        throw new WhipError('invalid_response', 'Unknown permission decision status');
    }
    return outcome as PermissionDecisionStatus;
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
  /** Persist missing routes using named keys on the execution host; never sends credential values. */
  discover({ model, provider, ...options }: CallOptions & { model?: string; provider?: string } = {}) {
    return this.client.call('provider.discover', { ...(model ? { model } : {}), ...(provider ? { provider } : {}) }, options);
  }
  list({ model, provider, ...options }: CallOptions & { model?: string; provider?: string } = {}) {
    return this.client.call('provider.list', { ...(model ? { model } : {}), ...(provider ? { provider } : {}) }, options);
  }
  /** Read an execution host's editable definition without retrieving its secret. */
  get(provider: string, options: CallOptions = {}) { return this.client.call('provider.get', { provider }, options); }
  /** Sent once without recovery storage. After an uncertain result, reread the provider before retrying. */
  create(params: ProviderCreateParams, options: CallOptions = {}) { return this.client.call('provider.create', params, options); }
  /** Omitted fields are retained; credentials remain host-owned and are never journaled. */
  update(params: ProviderUpdateParams, options: CallOptions = {}) { return this.client.call('provider.update', params, options); }
  /** Remove an unreferenced custom definition using its current configuration revision. */
  remove(params: ProviderRemoveParams, options: CallOptions = {}) { return this.client.call('provider.remove', params, options); }
  disconnect(params: ProviderDisconnectParams, options: CallOptions = {}) { return this.client.call('provider.disconnect', params, options); }
  catalogs({ refresh, provider, ...options }: CallOptions & { refresh?: boolean; provider?: string } = {}) {
    return this.client.query('provider.catalogs', { ...(refresh ? { refresh } : {}), ...(provider ? { provider } : {}) }, options);
  }
  status(name: string, options: CallOptions = {}) { return this.client.call('provider.status', { provider: name }, options); }
  setKey(params: ProviderKeySetup, options: CallOptions = {}) { return this.client.call('provider.key.set', params, options); }
  validate(params: ProviderValidateParams, options: CallOptions = {}) { return this.client.call('provider.validate', params, options); }
  rotateKey(name: string, options: CallOptions = {}) { return this.client.call('provider.key.rotate', { provider: name }, options); }
  logout(name: string, options: CallOptions = {}) { return this.client.call('provider.logout', { provider: name }, options); }
  readonly login = {
    begin: (options: CallOptions & ProviderLoginBeginParams = {}) => {
      const { provider, ...callOptions } = options;
      return this.client.call('provider.login.begin', provider ? { provider } : {}, callOptions);
    },
    list: (options: CallOptions = {}) => this.client.call('provider.login.list', {}, options),
    status: (flowId: string, options: CallOptions = {}) => this.client.call('provider.login.status', { flow_id: flowId }, options),
    cancel: (flowId: string, options: CallOptions = {}) => this.client.call('provider.login.cancel', { flow_id: flowId }, options),
    selectTeam: (flowId: string, teamId: string, options: CallOptions = {}) => this.client.call('provider.login.team.select', { flow_id: flowId, team_id: teamId }, options),
    selectProject: (flowId: string, projectId: string, options: CallOptions = {}) => this.client.call('provider.login.project.select', { flow_id: flowId, project_id: projectId }, options),
    createProject: (flowId: string, name: string, options: CallOptions = {}) => this.client.call('provider.login.project.create', { flow_id: flowId, name }, options),
  };
}
