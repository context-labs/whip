import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { Dialog, ThemeProvider, UIProvider } from '@whip/ui';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { modelOptions, readModelCatalog, priceLabel, modelSettings, type ModelCatalog } from '../src/model-options';
import { CatalogModelPicker } from '../src/model-selection';
import { providerFixture, model, route, revision } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const catalog: ModelCatalog = {
  inventory: { revision, routes: [route(), route('openai-codex')], defaults: null, compaction_model: null }, truncated: false,
  providers: ['openrouter', 'openai-codex'].map(id => ({ id, catalog: { provider: id, state: 'missing', scope_state: 'unverified', discovery: 'not_checked', fetched_at: null, stale: false, failure: null, models: [] }, models: [model('gpt-5.5')] })),
};
it('keeps exact provider routes and unknown versus free versus large prices', () => {
  expect(modelOptions(catalog).map(option => [option.name, option.provider])).toEqual([['gpt-5.5', 'openai-codex'], ['gpt-5.5', 'openrouter']]);
  expect(priceLabel(null)).toBe('Unknown'); expect(priceLabel('0')).toBe('Free'); expect(priceLabel('9007199254740993')).toBe('$9007199.254740993 / million tokens');
  expect(modelSettings(catalog, 'openrouter', 'gpt-5.5')?.prices.input).toBeNull();
});
it('a successful empty scoped catalog stays empty instead of reviving bundled membership', async () => {
  const f = await providerFixture(); f.data.handlers['providers.catalog'] = () => ({ provider: 'openrouter', state: 'cached', scope_state: 'current', discovery: 'account_catalog', fetched_at: '2026-09-28T12:00:00Z', stale: false, failure: null, models: [] });
  const value = await readModelCatalog(f.client, new AbortController().signal);
  expect(modelOptions(value)).toEqual([]); expect(f.count('providers.refresh')).toBe(0);
});
it('configured models retain exact settings even without catalog membership', async () => {
  const f = await providerFixture(); const settings = { prices: { ...model().prices, input: '9007199254740993' }, context_window_tokens: null, max_output_tokens: '4096', timeout_millis: '5000', max_attempts: 2 };
  f.data.inventory.routes[0]!.models = { exact: settings };
  const value = await readModelCatalog(f.client, new AbortController().signal);
  expect(modelOptions(value).find(item => item.name === 'exact')?.model.metadata_source).toBe('configured');
  expect(modelSettings(value, 'openrouter', 'exact')).toEqual(settings);
});
it('bounds a large host catalog and reports truncation explicitly', async () => {
  const f = await providerFixture(); f.data.inventory.routes = Array.from({ length: 8 }, (_, i) => route('provider-' + i));
  f.data.handlers['providers.bundled'] = () => ({ items: Array.from({ length: 1024 }, (_, i) => model('m' + i)) });
  const value = await readModelCatalog(f.client, new AbortController().signal);
  expect(modelOptions(value)).toHaveLength(4096); expect(value.truncated).toBe(true); expect(f.count('providers.bundled')).toBe(4);
});
it('shows the routing provider logo and updates it when choosing another provider for the same model', async () => {
  function Picker() {
    const [route, setRoute] = useState({ model: 'gpt-5.5', provider: 'openrouter' });
    return <CatalogModelPicker {...route} settings catalog={catalog} onChange={(model, provider) => setRoute({ model, provider })} />;
  }
  render(<ThemeProvider initialTheme="light"><UIProvider><Picker /></UIProvider></ThemeProvider>);
  const trigger = screen.getByRole('button', { name: 'Model', exact: true });
  expect(trigger.querySelector('use')?.getAttribute('href')).toMatch(/#openrouter$/);
  expect(trigger.textContent).toBe('gpt-5.5');
  fireEvent.click(trigger);
  const option = await screen.findByRole('option', { name: 'gpt-5.5 · openai-codex' });
  expect(option.querySelector('[data-model-name]')?.textContent).toBe('gpt-5.5');
  expect(option.querySelector('[data-model-provider]')?.textContent).toBe('openai-codex');
  fireEvent.click(option);
  expect(trigger.querySelector('use')?.getAttribute('href')).toMatch(/#openai$/);
  expect(trigger.textContent).toBe('gpt-5.5');
  expect(trigger.title).toBe('gpt-5.5 · openai-codex');
});

it.each(['pointer', 'keyboard'])('closes the model picker when opening session options with %s', async interaction => {
  const user = userEvent.setup();
  const change = vi.fn();
  function Picker() {
    const [showOptions, setShowOptions] = useState(false);
    return <>
      <CatalogModelPicker model="gpt-5.5" provider="openrouter" catalog={catalog} onChange={change}
        onSessionOptions={() => setShowOptions(true)} />
      <Dialog open={showOptions} onOpenChange={setShowOptions} title="Session options">
        <p>Session configuration</p>
      </Dialog>
    </>;
  }
  const route = createRootRoute({ component: () => <ThemeProvider initialTheme="light"><UIProvider><Picker /></UIProvider></ThemeProvider> });
  const router = createRouter({ routeTree: route, history: createMemoryHistory() });
  render(<RouterProvider router={router} />);
  await user.click(await screen.findByRole('button', { name: 'Model', exact: true }));
  const options = await screen.findByRole('button', { name: 'Session options' });
  if (interaction === 'keyboard') {
    options.focus();
    await user.keyboard('{Enter}');
  } else {
    await user.click(options);
  }
  const dialog = await screen.findByRole('dialog', { name: 'Session options' });
  await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Search models', hidden: true })).toBeNull());
  expect(screen.queryByRole('option', { hidden: true })).toBeNull();
  expect(change).not.toHaveBeenCalled();
  await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
  await user.keyboard('{Escape}');
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Session options' })).toBeNull());
  await user.click(screen.getByRole('button', { name: 'Model', exact: true }));
  expect(await screen.findByRole('textbox', { name: 'Search models' })).toBeTruthy();
});

it('preserves the model selection popup for an action failure and closes on successful retry', async () => {
  const change = vi.fn().mockRejectedValueOnce(new Error('Model change rejected')).mockResolvedValue(undefined);
  render(<ThemeProvider initialTheme="light"><UIProvider><CatalogModelPicker model="gpt-5.5" provider="openrouter" settings catalog={catalog} onChange={change} /></UIProvider></ThemeProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Model', exact: true }));
  fireEvent.click(await screen.findByRole('option', { name: 'gpt-5.5 · openai-codex' }));
  const alert = await screen.findByRole('alert');
  expect(alert.closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('action');
  expect(alert.textContent).toContain('Model change rejected');
  fireEvent.click(screen.getByRole('option', { name: 'gpt-5.5 · openai-codex' }));
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  expect(change).toHaveBeenCalledTimes(2);
});

it('configures the selected child with its exact revision and resets effort on a model route change', async () => {
  const { createSessionView } = await import('@whip/sdk/state');
  const { sessionRecord } = await import('./provider-fixture');
  const { ModelPicker } = await import('../src/model-selection');
  const f = await providerFixture(); f.data.inventory.routes.push(route('openai-codex'));
  const selected = sessionRecord(); const session = f.client.session(selected.id); const view = createSessionView(session);
  const current = view.getSnapshot(); vi.spyOn(view, 'getSnapshot').mockReturnValue({ ...current, activity: { session_id: selected.id, lifecycle: 'active', active_turn: null, active_input_id: null, queued_input_count: '0', pending_permission_count: '0', pending_question_count: '0', execution_permit: false, active_workspace_action_id: null } });
  const refresh = vi.spyOn(view, 'refresh').mockResolvedValue();
  f.data.handlers['sessions.configure'] = () => ({ ...selected, config_revision: '9007199254740994' });
  const root = createRootRoute({ component: () => f.wrap(<ModelPicker client={f.client} session={session} selected={selected} view={view} connected />) });
  render(<RouterProvider router={createRouter({ routeTree: root, history: createMemoryHistory() })} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Model', exact: true })); fireEvent.click(await screen.findByRole('option', { name: 'fixture · openai-codex' }));
  await waitFor(() => expect(f.count('sessions.configure')).toBe(1));
  expect(f.calls.find(call => call.method === 'sessions.configure')?.params).toEqual({ session_id: 'child', expected_revision: '9007199254740993', patch: { model: { name: 'fixture', provider: 'openai-codex', effort: '' } } });
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce()); expect(f.count('providers.defaults')).toBe(0);
});
it('unknown activity disables model effects without treating a missing observation as idle', async () => {
  const { createSessionView } = await import('@whip/sdk/state'); const { sessionRecord } = await import('./provider-fixture'); const { ModelPicker } = await import('../src/model-selection');
  const f = await providerFixture(); const selected = sessionRecord(); const session = f.client.session(selected.id); const view = createSessionView(session);
  f.mount(<ModelPicker client={f.client} session={session} selected={selected} view={view} connected />);
  expect(screen.getByRole('button', { name: 'Model', exact: true }).hasAttribute('disabled')).toBe(true); expect(f.count('sessions.configure')).toBe(0);
});
