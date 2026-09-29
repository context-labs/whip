import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { DeliveryError, type ProviderDefaultsParams, type SetDefaultPermissionModeParams } from '@whip/sdk';
import { ProviderDefaultsSettings } from '../src/settings/provider-defaults';
import { providerFixture, revision, nextRevision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
it('saves only the selected model defaults with the captured host revision', async () => {
  const f = await providerFixture(); f.data.handlers['providers.defaults'] = request => { const p = request.params as ProviderDefaultsParams; f.data.inventory = { ...f.data.inventory, revision: nextRevision, defaults: p.defaults.selection }; return f.data.inventory; };
  f.mount(<ProviderDefaultsSettings client={f.client} enabled />);
  fireEvent.change(await screen.findByLabelText('Exact model ID'), { target: { value: 'explicit-unknown-model' } });
  expect(f.count('providers.defaults')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Save model default' })); await screen.findByText('Host defaults saved. Existing sessions are unchanged.');
  expect(f.calls.find(call => call.method === 'providers.defaults')?.params).toEqual({ revision, defaults: { selection: { name: 'explicit-unknown-model', provider: 'openrouter', effort: '' }, settings: null } });
  expect(f.count('sessions.configure')).toBe(0); expect(f.count('host.set_permission_default')).toBe(0);
});
it('keeps the failed draft and refreshes after a lost CAS acknowledgement without replay', async () => {
  const f = await providerFixture(); f.data.handlers['providers.defaults'] = () => { f.data.inventory = { ...f.data.inventory, revision: nextRevision, defaults: { provider: 'openrouter', name: 'other-client', effort: '' } }; throw new DeliveryError('Acknowledgement lost'); };
  f.mount(<ProviderDefaultsSettings client={f.client} enabled />);
  fireEvent.change(await screen.findByLabelText('Exact model ID'), { target: { value: 'my-draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save model default' })); await screen.findByText(/Acknowledgement lost/);
  await screen.findByRole('button', { name: 'Discard edits and load current defaults' });
  expect((screen.getByLabelText('Exact model ID') as HTMLInputElement).value).toBe('my-draft'); expect(f.count('providers.defaults')).toBe(1);
  expect(screen.getByRole('button', { name: 'Save model default' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Discard edits and load current defaults' })); expect((screen.getByLabelText('Exact model ID') as HTMLInputElement).value).toBe('other-client');
});
it('saves compaction defaults separately without replacing the normal model', async () => {
  const f = await providerFixture(); f.data.handlers['providers.compaction'] = request => { const p = request.params as ProviderDefaultsParams; return { ...f.data.inventory, revision: nextRevision, compaction_model: p.defaults.selection }; };
  f.mount(<ProviderDefaultsSettings client={f.client} enabled compaction />);
  fireEvent.change(await screen.findByLabelText('Exact model ID'), { target: { value: 'summary' } }); fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'openrouter' } }); fireEvent.click(screen.getByRole('button', { name: 'Save model default' }));
  await waitFor(() => expect(f.count('providers.compaction')).toBe(1)); expect(f.count('providers.defaults')).toBe(0); expect(f.data.inventory.defaults?.name).toBe('fixture');
});
it('saves Ask and Full Access using the dedicated revision CAS instead of a root mutation', async () => {
  const f = await providerFixture(); f.data.handlers['host.set_permission_default'] = request => ({ revision: nextRevision, mode: (request.params as SetDefaultPermissionModeParams).mode });
  f.data.handlers['host.permission_default'] = () => ({ revision: f.count('host.set_permission_default') ? nextRevision : revision, mode: f.count('host.set_permission_default') ? 'automatic' : 'prompt' });
  f.mount(<ProviderDefaultsSettings client={f.client} enabled />);
  const user = userEvent.setup(); await user.click(await screen.findByRole('combobox', { name: 'Default permission level' })); await user.click(await screen.findByRole('option', { name: 'Full Access' })); await user.click(screen.getByRole('button', { name: 'Save permission default' }));
  await waitFor(() => expect(f.count('host.set_permission_default')).toBe(1)); expect(f.calls.find(call => call.method === 'host.set_permission_default')?.params).toEqual({ expected_revision: revision, mode: 'automatic' }); expect(f.count('permissions.set_mode')).toBe(0);
});
