import path from 'node:path';
import { unixSocket } from '@whip/sdk/node';
import type { ConnectionProfile } from '@whip/app/platform';
import type { BrowserAgentSelection, BrowserAgentScope, BrowserAgentPreview } from '@whip/app/desktop-bridge';
import { PreviewEnvironments, type PreviewIdentity, type PreviewScope } from './preview-environments';
import type { SSHConnection } from './ssh';
import { verifyProjectRuntime } from './project-open';
import { browserID, object } from './browser-policy';

type Connection = { profile: ConnectionProfile; ssh?: SSHConnection; socket?: string; controller: AbortController };
type Binding = { connectionId: string; connection: Connection; identity: PreviewIdentity; generation: string; preview: BrowserAgentPreview };
const key = (identity: PreviewIdentity) => JSON.stringify([identity.savedHostId, identity.runtimeId, identity.projectId]);
function bounded(value: unknown): string {
  if (typeof value !== 'string' || !value || value.length > 4096 || /[\u0000-\u001f\u007f]/.test(value)) throw new Error('Invalid preview identity'); return value;
}
/** Binds an inert offered identity to the exact live native connection, never a hostname lookup. */
export class BrowserPreviewAuthority {
  readonly environments: PreviewEnvironments;
  private bindings = new Map<string, Binding>();
  constructor(directory: string, private connection: (id: string) => Connection | undefined,
    invalidate: (id: string, reason: string) => void, disconnected: (id: string) => void) {
    this.environments = new PreviewEnvironments({ directory: path.join(directory, 'browser-preview'),
      connection: identity => {
        const binding = this.bindings.get(key(identity)); if (!binding || !this.current(binding)) return;
        const ssh = binding.connection.ssh!, preview = ssh.previewConnection; if (!preview) return;
        return { generation: preview.generation, signal: preview.signal,
          acquirePreviewRoute: (request, signal) => ssh.acquirePreviewRoute(request, signal) };
      }, invalidateAttachments: invalidate, onDisconnected: disconnected });
  }
  private current(binding: Binding): boolean {
    return this.connection(binding.connectionId) === binding.connection && !binding.connection.controller.signal.aborted &&
      !!binding.connection.ssh?.previewConnection && !binding.connection.ssh.previewConnection.signal.aborted && binding.connection.ssh.previewConnection.generation === binding.generation;
  }
  async describe(value: unknown): Promise<BrowserAgentPreview> {
    const input = object(value, ['connectionId', 'runtimeId', 'projectId', 'loopback']);
    const connectionId = browserID(input.connectionId), runtimeId = bounded(input.runtimeId), projectId = bounded(input.projectId);
    if (input.loopback !== '127.0.0.1' && input.loopback !== '::1') throw new Error('Only literal remote loopback preview is supported');
    const connection = this.connection(connectionId);
    if (!connection?.ssh?.previewConnection || !connection.socket || connection.profile.target.kind !== 'ssh') throw new Error('The selected native SSH connection is unavailable');
    const generation = connection.ssh.previewConnection.generation;
    await verifyProjectRuntime(unixSocket(connection.socket), runtimeId, connection.controller.signal);
    if (this.connection(connectionId) !== connection || connection.controller.signal.aborted || connection.ssh.previewConnection?.generation !== generation) throw new Error('SSH connection changed while verifying preview identity');
    const identity = { savedHostId: connection.profile.id, runtimeId, projectId };
    const environmentId = await this.environments.describe(identity);
    if (this.connection(connectionId) !== connection || connection.controller.signal.aborted || connection.ssh.previewConnection?.generation !== generation) throw new Error('SSH connection changed while preparing preview metadata');
    const existing = this.bindings.get(key(identity));
    if (existing && !this.current(existing)) this.bindings.delete(key(identity));
    if (existing && this.current(existing) && (existing.connection !== connection || existing.generation !== generation)) throw new Error('The preview identity is already bound to another live connection');
    if (existing && this.current(existing)) {
      if (existing.preview.loopback !== input.loopback) throw new Error('The preview environment has another literal loopback scope');
      return structuredClone(existing.preview);
    }
    const preview: BrowserAgentPreview = { host_id: identity.savedHostId, host_identity: runtimeId, connection_generation: generation,
      environment_id: environmentId, loopback: input.loopback, ports: [] };
    this.bindings.set(key(identity), { connectionId, connection, identity, generation, preview });
    return structuredClone(preview);
  }
  offered(environmentId: string): BrowserAgentPreview | undefined {
    const binding = [...this.bindings.values()].find(binding => binding.preview.environment_id === environmentId && this.current(binding));
    return binding ? structuredClone(binding.preview) : undefined;
  }
  private binding(selection: BrowserAgentSelection, preview: BrowserAgentPreview): Binding {
    if (!selection.connectionId || !selection.projectId) throw new Error('Preview requires its exact live connection and project context');
    const binding = this.bindings.get(key({ savedHostId: preview.host_id, runtimeId: preview.host_identity, projectId: selection.projectId }));
    if (!binding || binding.connectionId !== selection.connectionId || !this.current(binding) || binding.generation !== preview.connection_generation || binding.preview.environment_id !== preview.environment_id || binding.preview.loopback !== preview.loopback) throw new Error('The offered preview identity changed');
    return binding;
  }
  async prepare(selection: BrowserAgentSelection): Promise<void> {
    for (const preview of [...selection.offer.offered_preview_hosts ?? [], ...selection.offer.offered_tabs?.flatMap(tab => tab.preview ? [tab.preview] : []) ?? []]) {
      const binding = this.binding(selection, preview);
      if (JSON.stringify(preview.ports ?? []) !== JSON.stringify(binding.preview.ports ?? [])) throw new Error('Preview ports are not the current offered scope');
    }
  }
  private scope(binding: Binding, preview: BrowserAgentPreview): PreviewScope {
    if (!preview.ports || preview.ports.length > 64 || new Set(preview.ports).size !== preview.ports.length || preview.ports.some(port => !Number.isInteger(port) || port < 1 || port > 65535)) throw new Error('Invalid preview port scope');
    return { ...binding.identity, connectionGeneration: binding.generation, destinations: preview.ports.map(port => ({ remoteHost: preview.loopback as '127.0.0.1' | '::1', port })) };
  }
  async ensure(selection: BrowserAgentSelection, scope: BrowserAgentScope): Promise<string> {
    if (!scope.preview) throw new Error('No approved preview scope');
    const binding = this.binding(selection, scope.preview);
    const id = await this.environments.ensure(this.scope(binding, scope.preview));
    if (id !== scope.preview.environment_id) throw new Error('Preview environment identity changed');
    binding.preview = structuredClone(scope.preview); return id;
  }
  async expand(selection: BrowserAgentSelection, scope: BrowserAgentScope, port: number): Promise<void> {
    if (!scope.preview) throw new Error('No approved preview scope');
    const binding = this.binding(selection, scope.preview);
    this.environments.assertScope(scope.preview.environment_id, this.scope(binding, scope.preview));
    const preview = { ...scope.preview, ports: [...new Set([...(scope.preview.ports ?? []), port])].sort((a, b) => a - b) };
    await this.environments.expand(preview.environment_id, this.scope(binding, preview)); binding.preview = structuredClone(preview);
  }
  /** Main-only human consent plan. Preparing this object has no network authority. */
  async prepareHuman(input: { connectionId: string; runtimeId: string; projectId: string; loopback: '127.0.0.1' | '::1'; port: number }) {
    const { port, ...request } = input;
    const original = await this.describe(request);
    const binding = this.bindings.get(key({ savedHostId: original.host_id, runtimeId: request.runtimeId, projectId: request.projectId }))!;
    const preview = { ...original, ports: [...new Set([...(original.ports ?? []), port])].sort((a, b) => a - b) };
    this.scope(binding, preview);
    const assertCurrent = () => {
      if (this.bindings.get(key(binding.identity)) !== binding || !this.current(binding) || JSON.stringify(binding.preview) !== JSON.stringify(original))
        throw new Error('The preview identity or destination scope changed; confirm it again');
    };
    const ssh = binding.connection.profile.target;
    if (ssh.kind !== 'ssh') throw new Error('Only selected SSH connections support previews');
    return {
      preview, signal: binding.connection.controller.signal, assertCurrent,
      verifyCurrent: async () => {
        assertCurrent(); await verifyProjectRuntime(unixSocket(binding.connection.socket!), binding.identity.runtimeId, binding.connection.controller.signal); assertCurrent();
      },
      host: `${binding.connection.profile.label} (${ssh.user ? ssh.user + '@' : ''}${ssh.host}:${ssh.port ?? 22})`,
      // Called only after explicit confirmation and workspace admission, under the control barrier.
      admit: async (tabId: string, guard: () => void, realize: () => Promise<void>, rollback: () => Promise<void>) => {
        assertCurrent(); guard();
        const added = preview.ports.filter(value => !original.ports?.includes(value));
        const base = original.ports?.length ? original : preview;
        const lease = await this.environments.acquire(this.scope(binding, base), `human-admission:${tabId}`);
        let expanded = false;
        try {
          assertCurrent(); guard();
          if (base === original && added.length) { await this.environments.expand(preview.environment_id, this.scope(binding, preview)); expanded = true; }
          assertCurrent(); guard(); await realize(); assertCurrent(); guard();
          binding.preview = structuredClone(preview);
        } catch (error) {
          await rollback();
          if (expanded) for (const value of added) await this.environments.revoke(preview.environment_id, value);
          throw error;
        } finally { await lease.release(); }
      },
    };
  }
  async dispose(): Promise<void> { this.bindings.clear(); await this.environments.close(); }
}
