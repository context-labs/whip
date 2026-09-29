import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { expect, it } from 'vitest';
import { welcomeDraftKey, type NewChatTab } from '../src/session-tabs';
import { fixture } from './welcome-fixture';
import { preset, revision, route, sessionRecord } from './provider-fixture';

async function openSessionOptions() {
  fireEvent.click(await screen.findByRole('button', { name: 'Model', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Session options', exact: true }));
}
async function chooseFolder(f: Awaited<ReturnType<typeof fixture>>) {
  f.pickDirectory.mockResolvedValue('/edited');
  fireEvent.click(screen.getByRole('button', { name: 'Project folder' }));
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id)).toMatchObject({ cwd: '/edited' }));
}
function detectedOpenRouter(f: Awaited<ReturnType<typeof fixture>>) {
  f.data.inventory = { ...f.data.inventory, routes: [route('openrouter')], defaults: null };
  f.data.presets.items.push({ ...preset('openrouter'), suggested_models: ['gpt-6-astra'] });
  f.runtime.queries.setQueryData(['provider-list', 'host'], f.data.inventory);
  f.runtime.queries.setQueryData(['provider-presets', 'host'], f.data.presets);
}
it('keeps independent drafts and setup through tab and host switches without sending', async () => {
  const f = await fixture(); f.render();
  fireEvent.change(await screen.findByLabelText('Your first message'), { target: { value: 'Local draft' } });
  await chooseFolder(f);
  let second!: NewChatTab;
  act(() => { second = f.runtime.tabs.openNew({ runtimeId: 'host', hostProfileId: 'local', cwd: '/second' }); f.select(second.id); });
  expect(await screen.findByLabelText('Your first message')).toHaveProperty('value', '');
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Second draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Execution host' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: /Mac mini/ }));
  await waitFor(() => expect(f.connect).toHaveBeenCalledWith('remote'));
  expect(f.runtime.draft(welcomeDraftKey(second.id))).toBe('Second draft');
  expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === second.id)).toMatchObject({ hostProfileId: 'remote', cwd: '' });
  act(() => f.select(f.tab.id));
  expect(await screen.findByLabelText('Your first message')).toHaveProperty('value', 'Local draft');
  expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id)).toMatchObject({ cwd: '/edited' });
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('sends the edited folder, explicit ready model, prompt and default Ask only after Send', async () => {
  const f = await fixture(); f.render();
  fireEvent.change(await screen.findByLabelText('Your first message'), { target: { value: 'Explain auth' } });
  await chooseFolder(f);
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ working_directory: '/edited', overrides: { model: { provider: 'openai', name: 'gpt-6-astra', effort: 'high' } }, permission_mode: 'prompt', engine: 'starlark' }), undefined));
  await screen.findByText('Promoted to created');
  expect(f.rpc['sessions.submit']).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ session_id: 'created', parts: [{ type: 'text', text: 'Explain auth' }] }), undefined);
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('');
  expect(f.router.state.location.pathname).toBe('/');
});
it('retains the selected execution language in its draft and sends it explicitly', async () => {
  const f = await fixture(); f.render();
  await openSessionOptions();
  fireEvent.click(screen.getByRole('combobox', { name: 'Execution language' }));
  const option = await screen.findByRole('option', { name: 'JavaScript' });
  fireEvent.pointerDown(option); fireEvent.click(option);
  expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ executionEngine: 'quickjs' });
  fireEvent.click(screen.getByRole('button', { name: 'Close', exact: true }));
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Use this language' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledWith(expect.objectContaining({ engine: 'quickjs' }), undefined));
});
it('does not send while execution defaults are unavailable and preserves the draft', async () => {
  const f = await fixture();
  f.runtime.queries.removeQueries({ queryKey: ['host-execution-defaults'] });
  f.on('host.execution_defaults', () => { throw new Error('Execution defaults unavailable'); });
  f.render();
  fireEvent.change(await screen.findByLabelText('Your first message'), { target: { value: 'Keep this task' } });
  await screen.findByText('Could not load execution defaults');
  expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', true);
  fireEvent.keyDown(screen.getByLabelText('Your first message'), { key: 'Enter' });
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep this task');
});
it('offers one explicit default confirmation for a detected OpenRouter route', async () => {
  const f = await fixture(false, false); detectedOpenRouter(f);
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Retain this task'); f.render();
  const panel = await screen.findByRole('region', { name: 'Provider setup' });
  expect(await within(panel).findByRole('button', { name: 'Use OpenRouter' })).toBeTruthy();
  expect(screen.getAllByRole('button', { name: 'Use gpt-6-astra' })).toHaveLength(1);
  expect(f.rpc['providers.defaults']).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Use gpt-6-astra' }));
  await waitFor(() => expect(f.rpc['providers.defaults']).toHaveBeenCalledExactlyOnceWith({ revision, defaults: expect.objectContaining({ selection: { name: 'gpt-6-astra', provider: 'openrouter', effort: '' } }) }, expect.any(AbortSignal)));
  expect(await screen.findByLabelText('Your first message')).toHaveProperty('value', 'Retain this task');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('masks a setup key and preserves the draft until explicit model confirmation', async () => {
  const f = await fixture(false, false);
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Keep me while signing in'); f.render();
  fireEvent.click(await screen.findByRole('button', { name: 'Show all providers' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI' }));
  const key = await screen.findByLabelText('API key');
  expect(key).toHaveProperty('type', 'password');
  fireEvent.change(key, { target: { value: 'secret-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  const confirm = await screen.findByRole('button', { name: 'Use gpt-6-astra' });
  expect(f.rpc['providers.defaults']).not.toHaveBeenCalled();
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep me while signing in');
  expect(JSON.stringify(f.runtime.platform.storage.keys().map(key => f.runtime.platform.storage.getItem(key)))).not.toContain('secret-key');
  expect(JSON.stringify(f.runtime.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('secret-key');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  fireEvent.click(confirm);
  expect(await screen.findByLabelText('Your first message')).toHaveProperty('value', 'Keep me while signing in');
  expect(screen.getByLabelText('Your first message')).toHaveProperty('disabled', false);
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('replaces an unavailable draft route only after explicit provider setup confirmation', async () => {
  const f = await fixture();
  f.on('providers.readiness', ({ selection: { provider } }) => ({ configured: provider === 'openai', disabled: false, credential_state: provider === 'openai' ? 'available' : 'missing', catalog_state: 'missing', model_state: 'configured', inference_state: 'not_tested' }));
  f.render();
  fireEvent.change(await screen.findByLabelText('Your first message'), { target: { value: 'Keep this draft' } });
  act(() => f.runtime.tabs.updateNew(f.tab.id, { model: 'unavailable-model', provider: 'openrouter', effort: 'high' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use OpenAI' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use gpt-6-astra' }));
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull());
  const tab = f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id) as NewChatTab;
  expect(tab.model).toBeUndefined(); expect(tab.provider).toBeUndefined(); expect(tab.effort).toBeUndefined();
  expect(await screen.findByLabelText('Your first message')).toHaveProperty('value', 'Keep this draft');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('does not navigate away from a later route when session creation resolves', async () => {
  const f = await fixture(); let finish!: () => void;
  f.on('trees.create', params => new Promise(resolve => { finish = () => resolve({ creation: { id: params.creation_id, root_id: 'created', tree_id: 'tree', created_at: '2026-09-28T00:00:00Z' }, root: { ...sessionRecord('created'), parent_id: null, definition: params.definition }, tree: { id: 'tree', engine: params.engine, revision: '1', metadata: params.metadata, created_at: '2026-09-28T00:00:00Z' }, deleted: false }); }));
  f.render();
  fireEvent.change(await screen.findByLabelText('Your first message'), { target: { value: 'Task' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledOnce());
  await act(async () => { await f.router.navigate({ to: '/', hash: 'another-visit' }); });
  await act(async () => { finish(); });
  await screen.findByText('Promoted to created');
  expect(f.router.state.location.hash).toBe('another-visit');
});
it('retains the draft when native provider metadata fails without looping through setup', async () => {
  const f = await fixture(false, false);
  f.runtime.queries.removeQueries({ queryKey: ['provider-list'] });
  f.on('providers.list', () => { throw new Error('Provider metadata unavailable'); });
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Keep this task'); f.render();
  await screen.findByText('Could not load provider status');
  expect(screen.queryByRole('button', { name: 'Use OpenAI' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', true);
  expect(f.rpc['trees.create']).not.toHaveBeenCalled(); expect(f.rpc['providers.defaults']).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep this task');
  f.on('providers.list', () => f.data.inventory);
  fireEvent.click(screen.getByRole('button', { name: 'Retry provider status' }));
  await screen.findByRole('region', { name: 'Provider setup' });
  expect(screen.queryByText('Could not load provider status')).toBeNull();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep this task');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('does not focus a background pane composer when provider setup completes', async () => {
  const f = await fixture(false, false, false); detectedOpenRouter(f); f.render();
  const confirm = await screen.findByRole('button', { name: 'Use gpt-6-astra' });
  confirm.focus(); fireEvent.click(confirm);
  const input = await screen.findByLabelText('Your first message');
  await act(async () => { await new Promise(resolve => requestAnimationFrame(resolve)); });
  expect(document.activeElement).not.toBe(input);
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('sends the exact immutable agent revision chosen for the first message', async () => {
  const f = await fixture(), selected = { id: 'support-triage', revision: 'b'.repeat(64) };
  f.on('definitions.list', () => ({ items: [{ ref: { id: 'coding', revision }, name: 'Coding', created_at: '2026-09-28T00:00:00Z' }, { ref: selected, name: 'Support triage', created_at: '2026-09-28T00:00:00Z' }], next_cursor: null }));
  f.render();
  fireEvent.change(await screen.findByLabelText('Your first message'), { target: { value: 'Triage the queue' } });
  await openSessionOptions();
  fireEvent.click(await screen.findByRole('combobox', { name: 'Agent' }));
  const option = await screen.findByRole('option', { name: /Support triage · support-triage @ bbbbbbbbbbbb/ });
  fireEvent.pointerDown(option); fireEvent.click(option);
  expect(f.runtime.tabs.workspace().tabs.find(tab => tab.id === f.tab.id)).toMatchObject({ definition: selected });
  fireEvent.click(screen.getByRole('button', { name: 'Close', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ working_directory: '/project/whip', definition: selected, engine: 'starlark' }), undefined));
});
