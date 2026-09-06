import {
  assertValid, type CommandParams, type ConfigurationUpdate, type InitializeResult,
  type PermissionDecision, type ProviderKeySetup,
} from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import { RpcError, WhipError } from './errors.js';
import { decodeBase64, encodeBase64, sha256, subtle, utf8, uuid, withSignal } from './util.js';

/** Signs the SHA-256 protocol digest, using a key owned by the application. */
export interface ApprovalSigner { sign(digest: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> }
export function createWebCryptoSigner(privateKey: CryptoKey): ApprovalSigner {
  return { async sign(digest) { return new Uint8Array(await subtle().sign('Ed25519', privateKey, digest)); } };
}
function join(...parts: Uint8Array[]): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(parts.reduce((size, part) => size + part.byteLength, 0));
  let offset = 0; for (const part of parts) { bytes.set(part, offset); offset += part.byteLength; } return bytes;
}
export function approvalDigest(method: 'permission.decide' | 'permission.mode', generation: string, nonce: Uint8Array, payload: string): Promise<Uint8Array<ArrayBuffer>> {
  return sha256(join(utf8.encode(`whip privileged request v2\0${method}${generation}\0`), nonce, utf8.encode(payload)));
}

export class Permissions {
  private nonce = new Uint8Array(0);
  private info?: Readonly<InitializeResult>;
  private serial: Promise<unknown> = Promise.resolve();
  constructor(private readonly client: WhipClient, private readonly signer?: ApprovalSigner) {}
  connected(info: Readonly<InitializeResult>): void { this.info = info; this.nonce = decodeBase64(info.nonce ?? ''); }
  status(options: CallOptions = {}) { return this.client.call('identity.status', {}, options); }
  private serialize<T>(work: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    const result = this.serial.then(() => { signal?.throwIfAborted(); return work(); });
    this.serial = result.catch(() => {});
    return withSignal(result, signal);
  }
  private identity(): Readonly<InitializeResult> {
    const info = this.client.requireConnected();
    if (this.client.clientKind !== 'human') throw new WhipError('permission_denied', 'Automation clients cannot make human approval decisions');
    if (!this.info || info.connection_id !== this.info.connection_id) throw new WhipError('recovery_required', 'Human approval requires a fresh connection nonce');
    return info;
  }
  private async signed(method: 'permission.decide' | 'permission.mode', payload: string, options: CallOptions): Promise<string> {
    const info = this.identity();
    if (!this.signer) throw new WhipError('unavailable_capability', 'Supply an ApprovalSigner for an existing paired human identity');
    const signature = await this.signer.sign(await approvalDigest(method, info.generation, this.nonce, payload));
    options.signal?.throwIfAborted();
    if (this.client.requireConnected().connection_id !== info.connection_id) throw new WhipError('recovery_required', 'Connection changed while signing; review the current request before retrying');
    if (signature.length !== 64) throw new WhipError('invalid_arguments', 'Expected a 64-byte Ed25519 signature');
    return encodeBase64(signature);
  }
  private uncertain(error: unknown): never {
    const rejectedBeforeSend = error instanceof TypeError
      || error instanceof WhipError && ['resource_limit', 'unsupported_operation', 'invalid_arguments'].includes(error.kind);
    if (!(error instanceof RpcError) && !rejectedBeforeSend && this.client.getSnapshot().state === 'connected') this.client.reconnect();
    throw error;
  }
  decide(decision: Omit<PermissionDecision, 'command_id'> & { command_id?: string }, options: CallOptions = {}) {
    const payload = JSON.stringify({ ...decision, command_id: decision.command_id ?? uuid() });
    assertValid('PermissionDecision', JSON.parse(payload));
    return this.serialize(async () => {
      const signature = await this.signed('permission.decide', payload, options);
      try {
        const result = await this.client.callEncoded('permission.decide', `{"decision":${payload},"signature":${JSON.stringify(signature)}}`, options);
        this.nonce = decodeBase64(result.nonce ?? ''); return result;
      } catch (error) { return this.uncertain(error); }
    }, options.signal);
  }
  setMode(rootId: string, externalPermissions: boolean, options: CallOptions & { commandId?: string } = {}) {
    const command: CommandParams = { command_id: options.commandId ?? uuid(), scope: 'root', root_id: rootId, operation: 'permission.mode', payload: { external_permissions: externalPermissions } };
    const payload = JSON.stringify(command);
    return this.serialize(async () => {
      const signature = await this.signed('permission.mode', payload, options);
      try {
        const result = await this.client.callEncoded('permission.mode', `{"command":${payload},"signature":${JSON.stringify(signature)}}`, options);
        this.nonce = decodeBase64(result.nonce ?? ''); return result.command;
      } catch (error) { return this.uncertain(error); }
    }, options.signal);
  }
  /** Browser enrollment requires authorization by an already paired human. */
  enroll(publicKey: Uint8Array, authorizedBy: string, authorizer: ApprovalSigner, options: CallOptions = {}) {
    return this.serialize(async () => {
      const info = this.identity();
      const status = await this.status(options);
      if (status.enrollment_open) throw new WhipError('permission_denied', 'Pair the first human identity using WHIP in a terminal, then authorize this client');
      if (publicKey.length !== 32 || !authorizedBy) throw new TypeError('Enrollment requires a public key and paired authorizer');
      const digest = await sha256(join(utf8.encode(`whip identity enrollment v2\0${info.generation}\0`), this.nonce, utf8.encode(this.client.clientId + '\0' + this.client.clientKind), publicKey));
      const signature = await authorizer.sign(digest);
      if (this.client.requireConnected().connection_id !== info.connection_id) throw new WhipError('recovery_required', 'Connection changed during enrollment');
      try {
        const result = await this.client.call('identity.enroll', { public_key: encodeBase64(publicKey), authorized_by: authorizedBy, signature: encodeBase64(signature) }, options);
        this.nonce = decodeBase64(result.nonce ?? ''); return result;
      } catch (error) { return this.uncertain(error); }
    }, options.signal);
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
