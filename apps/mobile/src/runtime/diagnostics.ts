import type { MobileRuntime } from './runtime';
import { version } from '../../package.json';

/** Deliberate allowlist: never spread snapshots or include raw errors/content. */
export function diagnostics(state: ReturnType<MobileRuntime['getSnapshot']>, platform: string, osVersion: string | number) {
  const connection = state.client?.getSnapshot();
  return {
    app: `@whip/mobile:${version}`, sdk: '@whip/sdk:0.1.0', platform, osVersion,
    protocol: connection?.info ? `${connection.info.protocol_major}.${connection.info.protocol_minor}` : null,
    connection: connection?.state ?? (state.connecting ? 'connecting' : 'disconnected'),
    runtime: state.host?.runtimeId ?? null, active: state.active, ready: state.ready,
    lastReconciledAt: state.lastSync ?? null, lastErrorCode: state.lastErrorCode ?? null,
    storage: 'encrypted-open', savedHosts: state.hosts.length, commandRecords: state.commands.length,
  };
}
