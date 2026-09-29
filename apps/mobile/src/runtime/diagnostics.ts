import type { MobileRuntime } from './runtime';
import { version } from '../../package.json';

/** Deliberate allowlist: never spread snapshots or include raw errors/content. */
export function diagnostics(state: ReturnType<MobileRuntime['getSnapshot']>, platform: string, osVersion: string | number) {
  return {
    app: `@whip/mobile:${version}`, sdk: '@whip/sdk:4.0.0', platform, osVersion,
    protocol: state.client ? '4' : null,
    connection: !state.active ? 'suspended' : state.connecting ? 'connecting' : state.ready ? 'ready' : 'unavailable',
    runtime: state.host?.runtimeId ?? null, active: state.active, ready: state.ready,
    lastReconciledAt: state.lastSync ?? null, lastErrorCode: state.lastErrorCode ?? null,
    storage: 'encrypted-open', savedHosts: state.hosts.length, commandRecords: state.commands.length,
  };
}
