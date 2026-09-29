import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError, type HostExecutionDefaults, type SetExecutionPreferencesParams } from '@whip/sdk';
import { ExecutionSettings } from '../src/settings/configuration';
import { providerFixture, revision, nextRevision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const f = await providerFixture();
  let defaults: HostExecutionDefaults = { revision, engine: 'starlark', effort: 'high', compaction_percent: 0, goal_max_continuations: '100', max_attempts: 3, preferences: { engine: 'starlark', compaction_percent: 0, compaction_model: { selection: null, settings: null }, goal_max_continuations: null, max_attempts: 0, import_claude: true, import_codex: true } };
  f.data.handlers['host.execution_defaults'] = () => defaults;
  f.data.handlers['host.set_execution_preferences'] = request => { const p = request.params as SetExecutionPreferencesParams; defaults = { ...defaults, preferences: p.preferences, revision: nextRevision }; return defaults; };
  return { ...f, render: () => f.mount(<ExecutionSettings client={f.client} enabled />), change: (value: Partial<HostExecutionDefaults>) => { defaults = { ...defaults, ...value }; }, get: () => defaults };
}
it('saves the reference Execution category together, including summary model and source preferences', async () => {
  const f = await fixture(); f.render(); const user = userEvent.setup();
  await user.click(await screen.findByRole('combobox', { name: 'Execution language' })); await user.click(await screen.findByRole('option', { name: 'JavaScript' }));
  await user.click(screen.getByRole('button', { name: 'Summary model' })); await user.click(await screen.findByRole('option', { name: 'fixture · openrouter' }));
  await user.click(screen.getByRole('combobox', { name: 'Compact at' })); await user.click(await screen.findByRole('option', { name: '70%' }));
  fireEvent.change(screen.getByLabelText('Goal round limit'), { target: { value: '9007199254740993' } });
  fireEvent.change(screen.getByLabelText('Retry limit'), { target: { value: '8' } });
  await user.click(screen.getByRole('switch', { name: 'Import Claude configuration' }));
  expect(f.count('host.set_execution_preferences')).toBe(0);
  await user.click(screen.getByRole('button', { name: 'Save host defaults' })); await screen.findByText('Host defaults saved.');
  expect(f.calls.find(call => call.method === 'host.set_execution_preferences')?.params).toMatchObject({ expected_revision: revision, preferences: { engine: 'quickjs', compaction_percent: 70, compaction_model: { selection: { provider: 'openrouter', name: 'fixture' } }, goal_max_continuations: '9007199254740993', max_attempts: 8, import_claude: false, import_codex: true } });
  expect(f.count('providers.compaction')).toBe(0); expect(f.count('mcp.configure')).toBe(0); expect(f.count('sessions.configure')).toBe(0);
});
it.each([['Retry limit', '-1'], ['Retry limit', '9007199254740992'], ['Goal round limit', '-1'], ['Goal round limit', '1e4']])('rejects invalid %s without a host write', async (label, value) => {
  const f = await fixture(); f.render(); fireEvent.change(await screen.findByLabelText(label), { target: { value } }); fireEvent.submit(screen.getByRole('button', { name: 'Save host defaults' }).closest('form')!);
  await screen.findByRole('alert'); expect(f.count('host.set_execution_preferences')).toBe(0);
});
it('zero in the restored fields selects default intent, preserving explicit native no-continuation settings', async () => {
  const f = await fixture(); f.change({ preferences: { ...f.get().preferences, goal_max_continuations: '0' } }); f.render();
  expect(await screen.findByText('Additional goal continuations are disabled on this host.')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Retry limit'), { target: { value: '8' } }); fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await waitFor(() => expect(f.count('host.set_execution_preferences')).toBe(1)); expect(f.calls.find(call => call.method === 'host.set_execution_preferences')?.params).toMatchObject({ preferences: { goal_max_continuations: '0' } });
  fireEvent.click(screen.getByRole('button', { name: 'Use default goal limit' }));
  expect((await screen.findByLabelText('Goal round limit') as HTMLInputElement).value).toBe('0');
  fireEvent.change(screen.getByLabelText('Retry limit'), { target: { value: '0' } }); fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await waitFor(() => expect(f.count('host.set_execution_preferences')).toBe(2)); expect(f.calls.filter(call => call.method === 'host.set_execution_preferences')[1]?.params).toMatchObject({ preferences: { goal_max_continuations: null, max_attempts: 0 } });
});
it('refreshes a failed publication, retains draft, and never retries with a newer revision', async () => {
  const f = await fixture(); f.data.handlers['host.set_execution_preferences'] = () => { f.change({ revision: nextRevision, preferences: { ...f.get().preferences, max_attempts: 5 } }); throw new DeliveryError('Lost acknowledgement'); }; f.render();
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '4' } }); fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' })); await screen.findByText(/Lost acknowledgement/);
  await screen.findByRole('button', { name: 'Discard edits and load current defaults' }); expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('4'); expect(f.count('host.set_execution_preferences')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Discard edits and load current defaults' })); expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('5');
});
