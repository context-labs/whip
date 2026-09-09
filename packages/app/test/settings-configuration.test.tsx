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
import { RecoverySettings } from '../src/settings/recovery';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const initial: RuntimeConfiguration = {
  revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high',
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
  const runtime = { queries, report: vi.fn(), getSnapshot: () => state, subscribe: () => () => {}, discardDrafts: vi.fn() } as unknown as AppRuntime;
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
  expect(f.update).toHaveBeenCalledExactlyOnceWith({ revision: 'v1', compact_model: 'small-a', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8, max_retries: 4, import_claude: true, import_codex: false }, { signal: expect.any(AbortSignal) });
  expect((f.queries.getQueryData(['runtime-configuration', 'host-a']) as RuntimeConfiguration).default_model).toBe('model-a');
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

it('keeps draft recovery usable without any host client', async () => {
  const f = fixture(); render(f.wrapper(<RecoverySettings />));
  expect(screen.getByText(/Connect to an execution host to inspect its command recovery/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Discard saved drafts…' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Discard drafts' }));
  expect(f.runtime.discardDrafts).toHaveBeenCalledOnce();
  expect(f.get).not.toHaveBeenCalled();
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
