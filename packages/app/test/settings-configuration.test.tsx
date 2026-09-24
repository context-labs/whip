import userEvent from '@testing-library/user-event';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { ConfigurationUpdate, RuntimeConfiguration } from '@whip/protocol';
import type { ReactNode } from 'react';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ConfigurationSettings } from '../src/settings/configuration';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const initial: RuntimeConfiguration = {
  revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high', default_execution_engine: 'starlark',
  compact_model: 'small-a', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8,
  max_retries: 2, import_claude: true, import_codex: false,
};
function fixture() {
  let server = { ...initial };
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const get = vi.fn(async () => server);
  const update = vi.fn(async (patch: ConfigurationUpdate, _options: { signal: AbortSignal }) => {
    if (patch.revision !== server.revision) throw new Error('Configuration revision conflict');
    server = { ...server, ...patch, revision: 'v2' };
    return server;
  });
  const client = { getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }), configuration: { get, update }, providers: { catalogs: vi.fn(async () => ({ result: { models: {}, providers: {}, catalogs: { openrouter: { models: ['model-a', 'model-b', 'host-a-draft', 'new-host-b-model', 'temporary-edit'].map(id => ({ id, reasoning_efforts: ['low', 'high'] })) }, subscription: { models: [{ id: 'model-a', reasoning_efforts: ['low', 'ultra'] }, { id: 'no-reasoning', reasoning_efforts: [] }] } } } })) } } as unknown as WhipClient;
  const state = { commands: [] };
  const runtime = { queries, report: vi.fn(), getSnapshot: () => state, subscribe: () => () => {} } as unknown as AppRuntime;
  function wrapper(children: ReactNode) { return <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>{children}</QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>; }
  return { get, update, runtime, queries, client, wrapper, changeServer(next: RuntimeConfiguration) { server = next; },
    render(category: 'providers' | 'execution') { return render(wrapper(<ConfigurationSettings client={client} enabled category={category} />)); },
  };
}

async function chooseModel(model: string, provider = 'openrouter') {
  fireEvent.click(await screen.findByRole('button', { name: 'Default model', exact: true }));
  fireEvent.click(await screen.findByRole('option', { name: `${model} · ${provider}`, exact: true }));
}

it('saves only the model defaults owned by Providers and models', async () => {
  const f = fixture(); f.render('providers');
  await chooseModel('model-b');
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update).toHaveBeenCalledExactlyOnceWith({ revision: 'v1', default_model: 'model-b', default_provider: 'openrouter', default_effort: 'high' }, { signal: expect.any(AbortSignal) });
  expect((screen.getByRole('button', { name: 'Save host defaults' }) as HTMLButtonElement).disabled).toBe(true);
});

it('saves only execution-owned fields and retains model defaults from the host', async () => {
  const f = fixture(); f.render('execution');
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '4' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update).toHaveBeenCalledExactlyOnceWith({ revision: 'v1', default_execution_engine: 'starlark', compact_model: 'small-a', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8, max_retries: 4, import_claude: true, import_codex: false }, { signal: expect.any(AbortSignal) });
  expect((f.queries.getQueryData(['runtime-configuration', 'host-a']) as RuntimeConfiguration).default_model).toBe('model-a');
});

it('saves the execution language as a future-session default through the existing revisioned form', async () => {
  const f = fixture();
  vi.spyOn(f.client, 'getSnapshot').mockReturnValue({ state: 'connected', info: { runtime_id: 'host-a', execution_engines: [
    { id: 'starlark', language: 'starlark', label: 'Starlark' }, { id: 'quickjs', language: 'javascript', label: 'JavaScript (QuickJS)' },
  ] } } as ReturnType<WhipClient['getSnapshot']>);
  f.render('execution');
  fireEvent.click(await screen.findByRole('combobox', { name: 'Execution language' }));
  const option = await screen.findByRole('option', { name: 'JavaScript (QuickJS)' });
  fireEvent.pointerDown(option); fireEvent.click(option);
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update).toHaveBeenCalledWith(expect.objectContaining({ revision: 'v1', default_execution_engine: 'quickjs' }), expect.anything());
  expect((f.queries.getQueryData(['runtime-configuration', 'host-a']) as RuntimeConfiguration).default_execution_engine).toBe('quickjs');
});

it('preserves a stale draft after conflict and requires an explicit reload before a new revision is used', async () => {
  const f = fixture(); f.render('execution');
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '4' } });
  f.changeServer({ ...initial, revision: 'other-client', max_retries: 6 });
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Configuration revision conflict');
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('4');
  fireEvent.click(await screen.findByRole('button', { name: 'Discard edits and load current defaults' }));
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('6');
  expect(f.update).toHaveBeenCalledTimes(1);
  fireEvent.change(screen.getByLabelText('Retry limit'), { target: { value: '7' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update.mock.calls[1]![0].revision).toBe('other-client');
});

it('does not save disconnected host drafts and cancels a local wait when the form unmounts', async () => {
  const f = fixture(); const view = f.render('providers');
  await chooseModel('model-b');
  view.rerender(f.wrapper(<ConfigurationSettings client={f.client} enabled={false} category="providers" />));
  expect((screen.getByRole('button', { name: 'Save host defaults' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByLabelText('Default model').textContent).toContain('model-b');
  view.rerender(f.wrapper(<ConfigurationSettings client={f.client} enabled category="providers" />));
  f.update.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await waitFor(() => expect(f.update).toHaveBeenCalledTimes(1));
  const signal = f.update.mock.calls[0]![1].signal;
  expect(signal.aborted).toBe(false);
  view.unmount(); expect(signal.aborted).toBe(true);
});

it('retains the one mounted draft when disconnect cleanup removes host query data', async () => {
  const f = fixture(); const view = f.render('execution');
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '5' } });
  view.rerender(f.wrapper(<ConfigurationSettings client={f.client} enabled={false} category="execution" />));
  act(() => f.queries.removeQueries({ queryKey: ['runtime-configuration', 'host-a'] }));
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('5');
  expect((screen.getByRole('button', { name: 'Save host defaults' }) as HTMLButtonElement).disabled).toBe(true);
  expect(f.update).not.toHaveBeenCalled();
});

it('never carries another host draft into a replacement host form', async () => {
  const f = fixture(); const other = fixture();
  other.client.getSnapshot = () => ({ state: 'connected', info: { runtime_id: 'host-b' } } as ReturnType<WhipClient['getSnapshot']>);
  other.changeServer({ ...initial, default_model: 'host-b-model' });
  const view = f.render('providers');
  await chooseModel('host-a-draft');
  view.rerender(f.wrapper(<ConfigurationSettings client={other.client} enabled category="providers" />));
  await screen.findByText('host-b-model');
  expect(screen.queryByText('host-a-draft')).toBeNull();
  await chooseModel('new-host-b-model');
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update).not.toHaveBeenCalled();
  expect(other.update.mock.calls[0]![0].default_model).toBe('new-host-b-model');
});

it('treats edited then restored values as unchanged rather than requiring a redundant save', async () => {
  const f = fixture(); f.render('providers');
  await chooseModel('temporary-edit');
  expect((screen.getByRole('button', { name: 'Save host defaults' }) as HTMLButtonElement).disabled).toBe(false);
  await chooseModel('model-a');
  expect((screen.getByRole('button', { name: 'Save host defaults' }) as HTMLButtonElement).disabled).toBe(true);
  expect(f.update).not.toHaveBeenCalled();
});

it('selects the provider with its model and resets unsupported reasoning only after a user choice', async () => {
  const f = fixture(); f.render('providers');
  await chooseModel('model-a', 'subscription');
  expect(screen.queryByLabelText('Default provider')).toBeNull();
  fireEvent.click(screen.getByRole('combobox', { name: 'Reasoning effort' }));
  expect(screen.getByRole('option', { name: 'Model default' }).getAttribute('aria-selected')).toBe('true');
  expect(screen.queryByRole('option', { name: 'High', exact: true })).toBeNull();
  await userEvent.click(screen.getByRole('option', { name: 'ultra', exact: true }));
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'Reasoning effort' }).textContent).toContain('ultra'));
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update.mock.calls[0]![0]).toEqual({ revision: 'v1', default_model: 'model-a', default_provider: 'subscription', default_effort: 'ultra' });
  await chooseModel('no-reasoning', 'subscription');
  fireEvent.click(screen.getByRole('combobox', { name: 'Reasoning effort' }));
  expect(screen.getAllByRole('option').map(option => option.textContent)).toEqual(['Model default']);
});

it('preserves an unavailable saved effort during catalog refresh and displays the resolved implicit provider', async () => {
  const f = fixture();
  f.changeServer({ ...initial, default_provider: '', default_effort: 'max' });
  render(f.wrapper(<ConfigurationSettings client={f.client} enabled category="providers" defaultProvider="openrouter" />));
  expect(await screen.findByText('model-a')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Default model' }).title).toBe('model-a · openrouter');
  await screen.findByText('Max (unavailable)');
  expect((screen.getByRole('button', { name: 'Save host defaults' }) as HTMLButtonElement).disabled).toBe(true);
  expect(f.update).not.toHaveBeenCalled();
});

it('classifies invalid defaults as validation and preserves the draft without writing to the host', async () => {
  const f = fixture(); const view = f.render('execution');
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '-1' } });
  fireEvent.submit(view.container.querySelector('form')!);
  await screen.findByText('Use whole numbers: compaction from 0 to 100%, and non-negative goal rounds and retries.');
  expect(view.container.querySelector('[data-error-type="validation"]')).not.toBeNull();
  expect(f.update).not.toHaveBeenCalled();
  expect(f.runtime.report).not.toHaveBeenCalled();
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('-1');
  fireEvent.change(screen.getByLabelText('Retry limit'), { target: { value: '4' } });
  fireEvent.submit(view.container.querySelector('form')!);
  await screen.findByText('Host defaults saved.');
  expect(screen.queryByRole('alert')).toBeNull();
});


async function choosePermission(name: string) {
  fireEvent.click(await screen.findByRole('button', { name: 'Default permission level' }));
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(name) }));
}

it('places the shared permission control below effort and saves it only on Save host defaults', async () => {
  const f = fixture(); f.changeServer({ ...initial, default_permission_mode: 'prompt' });
  const view = f.render('providers');
  const control = await screen.findByRole('button', { name: 'Default permission level' });
  expect(control.textContent).toContain('Ask for approval');
  const rows = [...view.container.querySelectorAll('[role="group"]')].map(row => row.id);
  expect(rows.indexOf('default_permission_mode')).toBe(rows.indexOf('default_effort') + 1);
  await choosePermission('Full Access');
  await waitFor(() => expect(control.textContent).toContain('Full Access'));
  expect(f.update).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update.mock.calls[0]![0]).toEqual({ revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high', default_permission_mode: 'automatic' });
  expect(f.queries.getQueryData(['runtime-configuration', 'host-a'])).toMatchObject({ default_permission_mode: 'automatic' });
  expect(screen.getByRole('button', { name: 'Save host defaults' })).toHaveProperty('disabled', true);
});

it('keeps legacy hosts on Ask without offering an unsupported setting save', async () => {
  const f = fixture(); f.render('providers');
  expect(await screen.findByRole('button', { name: 'Default permission level' })).toHaveProperty('disabled', true);
  expect(screen.getByRole('button', { name: 'Default permission level' }).textContent).toContain('Ask for approval');
  await chooseModel('model-b');
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update.mock.calls[0]![0]).not.toHaveProperty('default_permission_mode');
});

it('retains a permission draft through revision conflicts and resets only when requested', async () => {
  const f = fixture(); f.changeServer({ ...initial, default_permission_mode: 'prompt' }); f.render('providers');
  await choosePermission('Full Access');
  f.changeServer({ ...initial, revision: 'v2', default_permission_mode: 'prompt' });
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Configuration revision conflict');
  expect(screen.getByRole('button', { name: 'Default permission level' }).textContent).toContain('Full Access');
  fireEvent.click(await screen.findByRole('button', { name: 'Discard edits and load current defaults' }));
  expect(screen.getByRole('button', { name: 'Default permission level' }).textContent).toContain('Ask for approval');
  expect(screen.getByRole('button', { name: 'Save host defaults' })).toHaveProperty('disabled', true);
});

it('does not include permissions in execution patches', async () => {
  const f = fixture(); f.changeServer({ ...initial, default_permission_mode: 'automatic' }); f.render('execution');
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '4' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await screen.findByText('Host defaults saved.');
  expect(f.update.mock.calls[0]![0]).not.toHaveProperty('default_permission_mode');
  expect(f.queries.getQueryData(['runtime-configuration', 'host-a'])).toMatchObject({ default_permission_mode: 'automatic' });
});

it('isolates permission drafts and saved defaults when switching execution hosts', async () => {
  const f = fixture(); f.changeServer({ ...initial, default_permission_mode: 'prompt' });
  const view = f.render('providers');
  await choosePermission('Full Access');
  const hostB = { ...f.client, getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-b' } }),
    configuration: { get: vi.fn(async () => ({ ...initial, revision: 'b1', default_permission_mode: 'prompt' })), update: vi.fn() } } as unknown as WhipClient;
  view.rerender(f.wrapper(<ConfigurationSettings client={hostB} enabled category="providers" />));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Default permission level' }).textContent).toContain('Ask for approval'));
  expect(screen.getByRole('button', { name: 'Save host defaults' })).toHaveProperty('disabled', true);
  expect(hostB.configuration.update).not.toHaveBeenCalled();
  expect(f.update).not.toHaveBeenCalled();
  view.rerender(f.wrapper(<ConfigurationSettings client={f.client} enabled category="providers" />));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Default permission level' }).textContent).toContain('Ask for approval'));
});

it('disables permission changes while disconnected or saving', async () => {
  const f = fixture(); f.changeServer({ ...initial, default_permission_mode: 'prompt' });
  const view = f.render('providers'); await choosePermission('Full Access');
  view.rerender(f.wrapper(<ConfigurationSettings client={f.client} enabled={false} category="providers" />));
  expect(screen.getByRole('button', { name: 'Default permission level' })).toHaveProperty('disabled', true);
  expect(screen.getByRole('button', { name: 'Save host defaults' })).toHaveProperty('disabled', true);
  view.rerender(f.wrapper(<ConfigurationSettings client={f.client} enabled category="providers" />));
  f.update.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.click(screen.getByRole('button', { name: 'Save host defaults' }));
  await waitFor(() => expect(f.update).toHaveBeenCalledTimes(1));
  expect(screen.getByRole('button', { name: 'Default permission level' })).toHaveProperty('disabled', true);
});
