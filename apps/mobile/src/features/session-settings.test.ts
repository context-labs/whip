/** @jest-environment node */
import type { SessionViewSnapshot } from '@whip/sdk/state';
import { idleRootSettings, pendingRootSetting, sessionEfforts } from './session-settings';
const snapshot = (): SessionViewSnapshot => ({ status: 'live', sessionID: 'root', unavailable: false, activity: { active_turn: null, queued_input_count: '0' } } as SessionViewSnapshot);
test('root edits require current idle root observations; host separately enforces descendant work policy', () => {
  expect(idleRootSettings(snapshot(), 'root', 'root')).toBe(true); expect(idleRootSettings(snapshot(), 'root', 'child')).toBe(false);
  expect(idleRootSettings({ ...snapshot(), status: 'stale' }, 'root', 'root')).toBe(false);
  expect(idleRootSettings({ ...snapshot(), activity: null }, 'root', 'root')).toBe(false);
  expect(idleRootSettings({ ...snapshot(), activity: { ...snapshot().activity!, queued_input_count: '9007199254740993' } }, 'root', 'root')).toBe(false);
});
test('unresolved deliveries block settings for their exact recipient only', () => {
  const command = { record: { runtimeId: 'runtime', sessionId: 'root' }, knownAccepted: false, status: 'unknown' };
  expect(pendingRootSetting([command] as never, 'runtime', 'root')).toBe(true);
  expect(pendingRootSetting([command] as never, 'runtime', 'child')).toBe(false);
});
test('reasoning choices use this configured provider and never invent supported levels', () => {
  const models = [{ model: 'model', provider: 'a', efforts: ['low', 'high', 'high'] }, { model: 'model', provider: 'b', efforts: ['max'] }];
  expect(sessionEfforts(models, 'model', 'a')).toEqual(['', 'low', 'high']); expect(sessionEfforts(models, 'model', 'b')).toEqual(['', 'max']); expect(sessionEfforts([], 'unknown', 'a')).toEqual(['']);
});
