import type { DeepReadonly, SessionViewSnapshot } from '@whip/sdk/state';
import type { CommandState } from '../runtime/runtime';
import type { CreationModel } from './creation';

export function idleRootSettings(snapshot: DeepReadonly<SessionViewSnapshot>, rootId: string, agentId: string): boolean {
  return snapshot.status === 'live' && !snapshot.unavailable && snapshot.root?.root_id === rootId
    && agentId === rootId && !snapshot.root.omitted?.active_turns && !Object.keys(snapshot.root.active_turns ?? {}).length;
}

export function pendingRootSetting(commands: readonly CommandState[], runtimeId: string, rootId: string): boolean {
  return commands.some(command => command.record.runtimeId === runtimeId && command.record.rootId === rootId
    && ['session.model', 'session.effort'].includes(command.record.operation)
    && !['succeeded', 'failed', 'cancelled', 'interrupted', 'not_found'].includes(command.status));
}

export function sessionEfforts(models: readonly CreationModel[], model: string, provider: string): string[] {
  const selected = models.find(item => item.model === model && item.provider === provider);
  const supported = selected?.efforts.filter(effort => effort !== 'off' && effort !== 'none') ?? [];
  return ['off', ...new Set(supported.length ? supported : ['low', 'medium', 'high', 'xhigh', 'max'])];
}
