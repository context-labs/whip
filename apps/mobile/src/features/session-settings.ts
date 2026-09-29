import type { DeepReadonly, SessionViewSnapshot } from '@whip/sdk/state';
import type { CommandState } from '../runtime/runtime';
import type { CreationModel } from './creation';
export function idleRootSettings(snapshot: DeepReadonly<SessionViewSnapshot>, rootId: string, agentId: string): boolean {
  return snapshot.status === 'live' && !snapshot.unavailable && snapshot.sessionID === rootId && agentId === rootId
    && snapshot.activity !== null && snapshot.activity.active_turn === null && snapshot.activity.queued_input_count === '0';
}
export function pendingRootSetting(commands: readonly CommandState[], runtimeId: string, rootId: string): boolean {
  return commands.some(command => command.record.runtimeId === runtimeId && command.record.sessionId === rootId && !command.knownAccepted && !['missing', 'failed'].includes(command.status));
}
export function sessionEfforts(models: readonly CreationModel[], model: string, provider: string): string[] {
  const supported = models.find(item => item.model === model && item.provider === provider)?.efforts ?? [];
  return ['', ...new Set(supported.filter(Boolean))];
}
