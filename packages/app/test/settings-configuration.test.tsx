import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError, type HostExecutionDefaults, type SetExecutionDefaultsParams } from '@whip/sdk';
import { ExecutionSettings } from '../src/settings/configuration';
import { providerFixture, revision, nextRevision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const f = await providerFixture();
  let defaults: HostExecutionDefaults = { revision, engine: 'starlark', effort: 'high', compaction_percent: 0, goal_max_continuations: '100', max_attempts: 3 };
  f.data.handlers['host.execution_defaults'] = () => defaults;
  f.data.handlers['host.set_execution_defaults'] = request => { const p = request.params as SetExecutionDefaultsParams; defaults = { ...p.defaults, revision: nextRevision }; return defaults; };
  return { ...f, render: () => f.mount(<ExecutionSettings client={f.client} enabled />), change: (value: Partial<HostExecutionDefaults>) => { defaults = { ...defaults, ...value }; } };
}
it('saves explicit future-root engine and exact continuation count including values above 2^53', async () => {
  const f = await fixture(); f.render(); const user = userEvent.setup();
  await user.click(await screen.findByRole('combobox', { name: 'Execution language' })); await user.click(await screen.findByRole('option', { name: 'JavaScript' }));
  fireEvent.change(screen.getByLabelText('Additional goal continuations'), { target: { value: '9007199254740993' } }); fireEvent.click(screen.getByRole('button', { name: 'Save execution defaults' }));
  await screen.findByText('Host defaults saved. Existing sessions keep their captured configuration.');
  expect(f.calls.find(call => call.method === 'host.set_execution_defaults')?.params).toEqual({ expected_revision: revision, defaults: { engine: 'quickjs', effort: 'high', compaction_percent: 0, goal_max_continuations: '9007199254740993', max_attempts: 3 } });
  expect(f.count('sessions.configure')).toBe(0); expect(f.count('providers.defaults')).toBe(0);
});
it.each([['Compaction threshold', '101'], ['Maximum attempts', '0'], ['Maximum attempts', '6'], ['Additional goal continuations', '-1'], ['Additional goal continuations', '1e4']])('rejects invalid %s without a host write', async (label, value) => {
  const f = await fixture(); f.render(); fireEvent.change(await screen.findByLabelText(label), { target: { value } }); fireEvent.submit(screen.getByRole('button', { name: 'Save execution defaults' }).closest('form')!);
  await screen.findByRole('alert'); expect(f.count('host.set_execution_defaults')).toBe(0);
});
it('preserves explicit zero continuations independently of the zero compaction default', async () => {
  const f = await fixture(); f.render(); fireEvent.change(await screen.findByLabelText('Additional goal continuations'), { target: { value: '0' } }); fireEvent.click(screen.getByRole('button', { name: 'Save execution defaults' }));
  await waitFor(() => expect(f.count('host.set_execution_defaults')).toBe(1)); expect(f.calls.find(call => call.method === 'host.set_execution_defaults')?.params).toMatchObject({ defaults: { goal_max_continuations: '0', compaction_percent: 0 } });
  expect(screen.getByText(/Zero uses the host default of 50%/)).toBeTruthy(); expect(screen.getByText(/Zero disables additional continuations/)).toBeTruthy();
});
it('refreshes a failed publication, retains draft, and never retries with a newer revision', async () => {
  const f = await fixture(); f.data.handlers['host.set_execution_defaults'] = () => { f.change({ revision: nextRevision, max_attempts: 5 }); throw new DeliveryError('Lost acknowledgement'); }; f.render();
  fireEvent.change(await screen.findByLabelText('Maximum attempts'), { target: { value: '4' } }); fireEvent.click(screen.getByRole('button', { name: 'Save execution defaults' })); await screen.findByText(/Lost acknowledgement/);
  await screen.findByRole('button', { name: 'Discard edits and load current defaults' }); expect((screen.getByLabelText('Maximum attempts') as HTMLInputElement).value).toBe('4'); expect(f.count('host.set_execution_defaults')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Discard edits and load current defaults' })); expect((screen.getByLabelText('Maximum attempts') as HTMLInputElement).value).toBe('5');
});
