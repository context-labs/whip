/** @jest-environment node */
import type { SessionViewSnapshot } from '@whip/sdk/state';
import type { CommandState } from '../runtime/runtime';
import { idleRootSettings, pendingRootSetting, sessionEfforts } from './session-settings';

const snapshot = (active_turns: Record<string, string> = {}): SessionViewSnapshot => ({ status: 'live', unavailable: false,
  root: { root_id: 'root', active_turns } } as SessionViewSnapshot);

test('root settings refuse child recipients, active descendants and stale or replaced roots', () => {
  expect(idleRootSettings(snapshot(), 'root', 'root')).toBe(true);
  expect(idleRootSettings(snapshot(), 'root', 'child')).toBe(false);
  expect(idleRootSettings(snapshot({ child: 'turn' }), 'root', 'root')).toBe(false);
  expect(idleRootSettings(snapshot(), 'another-root', 'another-root')).toBe(false);
  expect(idleRootSettings({ ...snapshot(), status: 'stale' }, 'root', 'root')).toBe(false);
  const omitted = snapshot(); omitted.root!.omitted = { active_turns: true };
  expect(idleRootSettings(omitted, 'root', 'root')).toBe(false);
});

test('unresolved or admitted settings block both setting actions across remounts', () => {
  const command: CommandState = { record: { version: 1, runtimeId: 'runtime', clientId: 'client', commandId: 'id', operation: 'session.model', rootId: 'root' }, status: 'queued', accepted: true };
  for (const status of ['sending', 'checking', 'queued', 'running', 'waiting']) expect(pendingRootSetting([{ ...command, status }], 'runtime', 'root')).toBe(true);
  for (const status of ['succeeded', 'failed', 'cancelled', 'interrupted', 'not_found']) expect(pendingRootSetting([{ ...command, status }], 'runtime', 'root')).toBe(false);
  expect(pendingRootSetting([command], 'other-runtime', 'root')).toBe(false);
  expect(pendingRootSetting([command], 'runtime', 'other-root')).toBe(false);
});

test('reasoning choices follow the applied model and provider instead of another provider catalog', () => {
  const models = [{ model: 'model', provider: 'a', efforts: ['none', 'low', 'high', 'high'] }, { model: 'model', provider: 'b', efforts: ['off', 'max'] }];
  expect(sessionEfforts(models, 'model', 'a')).toEqual(['off', 'low', 'high']);
  expect(sessionEfforts(models, 'model', 'b')).toEqual(['off', 'max']);
  expect(sessionEfforts([], 'unknown', 'a')).toEqual(['off', 'low', 'medium', 'high', 'xhigh', 'max']);
});
