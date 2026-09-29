import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError, type ExternalBrowserStatus, type ExternalBrowserSession, type ConfigureExternalBrowserParams } from '@whip/sdk';
import { ExternalBrowserSettings } from '../src/settings/external-browser';
import { ExternalBrowserConnections } from '../src/details/external-browser';
import { providerFixture, revision, nextRevision, sessionRecord } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const status = (): ExternalBrowserStatus => ({ revision, configuration: { mode: 'disabled', executable: '', live_endpoint: '', live_profile: '', allow_private_urls: false }, driver: 'rod', driver_pinned: false });
const connection = (): ExternalBrowserSession => ({ root_id: 'root', name: 'default', mode: 'headless', driver: 'rod', generation: 'old-generation', resource: 'browser-external:' + 'a'.repeat(64), state: 'connected' });
async function fixture() {
  const f = await providerFixture(); let current = status();
  f.data.handlers['host.external_browser'] = () => current;
  f.data.handlers['host.set_external_browser'] = request => { const p = request.params as ConfigureExternalBrowserParams; current = { ...current, revision: nextRevision, configuration: p.configuration }; return current; };
  return { ...f, current: () => current, change: (value: ExternalBrowserStatus) => { current = value; } };
}
it('saves one complete reviewed host declaration without preparing or opening a browser', async () => {
  const f = await fixture(); f.mount(<ExternalBrowserSettings client={f.client}/>); const user = userEvent.setup();
  await user.click(await screen.findByRole('combobox', { name: 'External browser mode' })); await user.click(await screen.findByRole('option', { name: 'Headless Chrome' }));
  fireEvent.change(screen.getByLabelText('Chrome executable'), { target: { value: '/host/Chrome' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save external Chrome settings' }));
  await screen.findByText(/External Chrome settings saved/);
  expect(f.calls.find(call => call.method === 'host.set_external_browser')?.params).toEqual({ expected_revision: revision, configuration: { mode: 'headless', executable: '/host/Chrome', live_endpoint: '', live_profile: '', allow_private_urls: false } });
  expect(f.calls.every(call => ['initialize', 'host.external_browser', 'host.set_external_browser'].includes(call.method))).toBe(true);
});
it('lost settings acknowledgement keeps its draft and blocks resend until explicit read', async () => {
  const f = await fixture(); f.data.handlers['host.set_external_browser'] = () => { f.change({ ...status(), revision: nextRevision, configuration: { ...status().configuration, mode: 'extension' } }); throw new DeliveryError('Lost acknowledgement'); };
  f.mount(<ExternalBrowserSettings client={f.client}/>); const user = userEvent.setup();
  await user.click(await screen.findByRole('combobox', { name: 'External browser mode' })); await user.click(await screen.findByRole('option', { name: 'Chrome extension' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save external Chrome settings' })); await screen.findByText(/Lost acknowledgement/);
  const save = screen.getByRole('button', { name: 'Save external Chrome settings' }); expect(save).toHaveProperty('disabled', true); fireEvent.click(save); expect(f.count('host.set_external_browser')).toBe(1);
  f.data.handlers['host.external_browser'] = () => { throw new Error('Read unavailable'); };
  fireEvent.click(screen.getByRole('button', { name: 'Discard edits and read external Chrome settings' })); await screen.findAllByText(/Read unavailable/); expect(save).toHaveProperty('disabled', true);
  f.data.handlers['host.external_browser'] = () => f.current();
  fireEvent.click(screen.getByRole('button', { name: 'Discard edits and read external Chrome settings' })); await screen.findByText(/Current host settings loaded/);
  expect(f.count('host.set_external_browser')).toBe(1); expect(save).toHaveProperty('disabled', true);
});
it('offline transition aborts a settings wait and a late acknowledgement cannot clear required review', async () => {
  const f = await fixture(); let signal: AbortSignal | undefined, complete!: (value: ExternalBrowserStatus) => void;
  f.data.handlers['host.set_external_browser'] = (_request, observed) => { signal = observed; return new Promise(resolve => { complete = resolve; }); };
  const mounted = f.mount(<ExternalBrowserSettings client={f.client}/>), user = userEvent.setup();
  await user.click(await screen.findByRole('combobox', { name: 'External browser mode' })); await user.click(await screen.findByRole('option', { name: 'Chrome extension' })); fireEvent.click(screen.getByRole('button', { name: 'Save external Chrome settings' }));
  await waitFor(() => expect(signal).toBeDefined()); mounted.rerender(f.wrap(<ExternalBrowserSettings client={f.client} enabled={false}/>)); expect(signal?.aborted).toBe(true);
  await act(async () => { complete({ ...status(), revision: nextRevision }); }); mounted.rerender(f.wrap(<ExternalBrowserSettings client={f.client}/>));
  expect(screen.getByRole('button', { name: 'Save external Chrome settings' })).toHaveProperty('disabled', true); expect(f.count('host.set_external_browser')).toBe(1);
});
it('connection controls display exact authority, capture generation and never replay a lost reply', async () => {
  const f = await fixture(); let entry = connection(); f.data.handlers['browser.external_sessions'] = () => ({ items: [entry] });
  f.data.handlers['browser.reconnect_external'] = () => { entry = { ...entry, generation: 'fresh-generation', state: 'prepared' }; throw new DeliveryError('Connection reply lost'); };
  f.mount(<ExternalBrowserConnections client={f.client} connected session={f.client.session('root')} rootId="root" selected={sessionRecord('root')}/>);
  fireEvent.click(await screen.findByRole('button', { name: 'Reconnect default' })); await screen.findByText(/Connection reply lost/);
  expect(f.calls.find(call => call.method === 'browser.reconnect_external')?.params).toEqual({ root_id: 'root', name: 'default', generation: 'old-generation' });
  expect(screen.getByRole('button', { name: 'Reconnect default' })).toHaveProperty('disabled', true); expect(f.count('browser.reconnect_external')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Read current external connections' })); await screen.findByText('fresh-generation');
  expect(screen.getByRole('button', { name: 'Reconnect default' })).toHaveProperty('disabled', false); expect(f.count('browser.reconnect_external')).toBe(1);
});
it('children inspect exact root resources without controls and foreign metadata is rejected', async () => {
  const f = await fixture(); f.data.handlers['browser.external_sessions'] = () => ({ items: [connection()] });
  const mounted = f.mount(<ExternalBrowserConnections client={f.client} connected session={f.client.session('child')} rootId="root" selected={sessionRecord('child')}/>);
  await screen.findByText('old-generation'); expect(screen.queryByRole('button', { name: 'Reconnect default' })).toBeNull(); expect(f.calls.find(call => call.method === 'browser.external_sessions')?.params).toEqual({ session_id: 'child' });
  mounted.unmount(); f.queries.clear(); f.data.handlers['browser.external_sessions'] = () => ({ items: [{ ...connection(), root_id: 'foreign' }] });
  f.mount(<ExternalBrowserConnections client={f.client} connected session={f.client.session('child')} rootId="root" selected={sessionRecord('child')}/>); await screen.findByText(/metadata belongs to another root/); expect(screen.queryByText('old-generation')).toBeNull();
});
