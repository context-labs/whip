import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { DeliveryError, type ProviderPreferencesParams } from '@whip/sdk';
import { ProviderDefaultsSettings } from '../src/settings/provider-defaults';
import { providerFixture, model, revision, nextRevision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
async function chooseModel() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Default model' }));
  await user.click(await screen.findByRole('option', { name: 'other · openrouter' }));
}
it('saves model, explicit reasoning and permission together through one host CAS', async () => {
  const f = await providerFixture();
  f.data.handlers['providers.bundled'] = () => ({ items: [model(), { ...model('other'), reasoning_efforts: ['off', 'high'] }] });
  f.data.handlers['providers.set_preferences'] = request => { const p = request.params as ProviderPreferencesParams; f.data.inventory = { ...f.data.inventory, revision: nextRevision, defaults: p.defaults.selection, permission_mode: p.permission_mode }; return f.data.inventory; };
  f.mount(<ProviderDefaultsSettings client={f.client} enabled />);
  await chooseModel(); const user = userEvent.setup();
  await user.click(screen.getByRole('combobox', { name: 'Reasoning effort' }));
  expect(await screen.findByRole('option', { name: 'Default', exact: true })).toBeTruthy();
  await user.click(screen.getByRole('option', { name: 'Off', exact: true }));
  await user.click(screen.getByRole('button', { name: 'Default permission level' })); await user.click(await screen.findByRole('option', { name: /Full Access/ }));
  expect(f.count('providers.set_preferences')).toBe(0);
  await user.click(screen.getByRole('button', { name: 'Save host defaults' })); await screen.findByText('Host defaults saved.');
  expect(f.calls.find(call => call.method === 'providers.set_preferences')?.params).toMatchObject({ revision, defaults: { selection: { name: 'other', provider: 'openrouter', effort: 'off' } }, permission_mode: 'automatic' });
  expect(f.count('providers.defaults')).toBe(0); expect(f.count('host.set_permission_default')).toBe(0); expect(f.count('sessions.configure')).toBe(0);
});
it('keeps the complete failed form and refreshes after lost CAS acknowledgement without replay', async () => {
  const f = await providerFixture(); f.data.handlers['providers.bundled'] = () => ({ items: [model(), model('other')] });
  f.data.handlers['providers.set_preferences'] = () => { f.data.inventory = { ...f.data.inventory, revision: nextRevision, defaults: { provider: 'openrouter', name: 'other-client', effort: '' } }; throw new DeliveryError('Acknowledgement lost'); };
  f.mount(<ProviderDefaultsSettings client={f.client} enabled />); await chooseModel();
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' })); await screen.findByText(/Acknowledgement lost/);
  await screen.findByRole('button', { name: 'Discard edits and load current defaults' });
  expect(screen.getByRole('button', { name: 'Default model' }).textContent).toBe('other'); expect(f.count('providers.set_preferences')).toBe(1);
  expect(screen.getByRole('button', { name: 'Save host defaults' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Discard edits and load current defaults' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Default model' }).textContent).toBe('other-client'));
});

it('retains unpublished edits and the captured revision through same-host client replacement', async () => {
  const f = await providerFixture(); f.data.handlers['providers.bundled'] = () => ({ items: [model(), model('other')] });
  const view = f.mount(<ProviderDefaultsSettings client={f.client} enabled />); await chooseModel();
  view.rerender(f.wrap(<ProviderDefaultsSettings client={f.client} enabled={false} />));
  act(() => f.queries.removeQueries({ queryKey: ['provider-list', f.client.runtimeID] }));
  expect(screen.getByRole('button', { name: 'Default model' }).textContent).toBe('other');
  expect(screen.getByRole('button', { name: 'Save host defaults' }).hasAttribute('disabled')).toBe(true);
  const replacement = await providerFixture(); replacement.data.inventory = { ...replacement.data.inventory, revision: nextRevision };
  view.rerender(f.wrap(<ProviderDefaultsSettings client={replacement.client} enabled />));
  await screen.findByRole('button', { name: 'Discard edits and load current defaults' });
  expect(screen.getByRole('button', { name: 'Default model' }).textContent).toBe('other');
  expect(screen.getByRole('button', { name: 'Save host defaults' }).hasAttribute('disabled')).toBe(true);
  expect(f.count('providers.set_preferences') + replacement.count('providers.set_preferences')).toBe(0);
});
