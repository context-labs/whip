import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { AppRuntime } from '../src/runtime';
import { welcomeDraftKey } from '../src/session-tabs';
import { revision } from './provider-fixture';
import { wire } from './welcome-fixture';
import { fixture } from './welcome-fixture';

it('keeps provider controls neutral while configured readiness is pending', async () => {
  const f = await fixture();
  f.runtime.queries.removeQueries({ queryKey: ['provider-readiness'] });
  let resolve!: (value: unknown) => void;
  f.on(
    'providers.readiness',
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  f.render();
  const input = await screen.findByRole('textbox', {
    name: 'Your first message',
  });
  fireEvent.change(input, {
    target: { value: 'Keep my draft while the host answers' },
  });
  await waitFor(() =>
    expect(f.rpc['providers.readiness']).toHaveBeenCalled(),
  );
  expect(
    screen.queryByRole('button', { name: 'Connect a provider', exact: true }),
  ).toBeNull();
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  expect(
    screen.getByRole('button', { name: 'Send first message' }),
  ).toHaveProperty('disabled', true);
  await act(async () =>
    resolve({
      configured: true,
      disabled: false,
      credential_state: 'available',
      catalog_state: 'missing',
      model_state: 'configured',
      inference_state: 'not_tested',
    }),
  );
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Send first message' }),
    ).toHaveProperty('disabled', false),
  );
  expect(screen.getByRole('textbox', { name: 'Your first message' })).toBe(
    input,
  );
  expect(input).toHaveProperty(
    'value',
    'Keep my draft while the host answers',
  );
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});

it.each(['unchecked', 'refresh_required'] as const)(
  'allows first use of a configured %s credential without a setup detour',
  async (credential_state) => {
    const f = await fixture();
    f.runtime.queries.removeQueries({ queryKey: ['provider-readiness'] });
    f.on('providers.readiness', () => ({
      configured: true,
      disabled: false,
      credential_state,
      catalog_state: 'missing',
      model_state: 'configured',
      inference_state: 'not_tested',
    }));
    f.render();
    const input = await screen.findByRole('textbox', {
      name: 'Your first message',
    });
    fireEvent.change(input, {
      target: { value: 'Use my configured provider' },
    });
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Send first message' }),
      ).toHaveProperty('disabled', false),
    );
    expect(
      screen.queryByRole('region', { name: 'Provider setup' }),
    ).toBeNull();
    expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  },
);

it.each([
  { desktop: true, focused: true, overlay: false, shouldFocus: true },
  { desktop: false, focused: true, overlay: false, shouldFocus: false },
  { desktop: true, focused: false, overlay: false, shouldFocus: false },
  { desktop: true, focused: true, overlay: true, shouldFocus: false },
])('focuses a new draft only in an active desktop pane without an overlay: %j', async ({ desktop, focused, overlay, shouldFocus }) => {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: desktop && query === '(min-width: 768px)', addEventListener() {}, removeEventListener() {} }));
  const dialog = document.createElement('div');
  dialog.setAttribute('role', 'dialog');
  const button = document.createElement('button');
  dialog.append(button);
  if (overlay) { document.body.append(dialog); button.focus(); }
  const f = await fixture(false, true, focused); f.render();
  try {
    const input = await screen.findByRole('textbox', { name: 'Your first message' });
    await act(async () => { await new Promise(resolve => requestAnimationFrame(resolve)); });
    expect(document.activeElement === input).toBe(shouldFocus);
    if (overlay) expect(document.activeElement).toBe(button);
    if (shouldFocus) {
      const project = screen.getByRole('button', { name: 'Project folder' });
      project.focus();
      await act(async () => { await f.runtime.queries.invalidateQueries({ queryKey: ['provider-list'] }); });
      expect(document.activeElement).toBe(project);
    }
  } finally { dialog.remove(); }
});

it('shows local setup without the execution host picker in a new draft', async () => {
  const f = await fixture();
  const status = { state: 'missing' as const, home: '/tmp/whip-test', message: 'No installation.', canInstall: true };
  f.runtime.platform.localRuntime = {
    test: vi.fn(async () => status), choose: vi.fn(async () => status),
    install: vi.fn(async () => status), restart: vi.fn(async () => status),
  };
  const snapshot = f.runtime.getSnapshot();
  vi.mocked(f.runtime.getSnapshot).mockReturnValue({ ...snapshot, hosts: snapshot.hosts.map(host =>
    host.id === 'local' ? { ...host, state: 'closed', client: undefined, localRuntime: status } : host) });
  f.render();
  await screen.findByRole('heading', { name: 'Welcome to WhipCode' });
  expect(screen.getByRole('button', { name: 'Get Started' })).toBeTruthy();
  const openExternal = vi.spyOn(f.runtime.platform, 'openExternal');
  fireEvent.click(screen.getByRole('button', { name: 'View GitHub' }));
  expect(openExternal).toHaveBeenCalledWith('https://github.com/context-labs/whip');
  expect(screen.getByRole('button', { name: 'Advanced' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Execution host' })).toBeNull();
});

it('replaces the send icon with one spinner while creating a session', async () => {
  const f = await fixture();
  let reject!: (error: Error) => void;
  f.run.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail; }));
  f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Hello' } });
  const send = screen.getByRole('button', { name: 'Send first message' }) as HTMLButtonElement;
  expect(send.querySelector('.lucide-arrow-up')).not.toBeNull();
  fireEvent.click(send);
  await waitFor(() => expect(send.getAttribute('aria-busy')).toBe('true'));
  expect(send.disabled).toBe(true);
  expect(send.querySelectorAll('svg')).toHaveLength(1);
  expect(send.querySelector('[aria-label="Loading"]')).not.toBeNull();
  expect(send.querySelector('.lucide-arrow-up')).toBeNull();
  await act(async () => { reject(new Error('Creation failed')); });
  await waitFor(() => expect(send.disabled).toBe(false));
  expect(send.querySelectorAll('svg')).toHaveLength(1);
  expect(send.querySelector('.lucide-arrow-up')).not.toBeNull();
  expect(send.querySelector('[aria-label="Loading"]')).toBeNull();
});

it('keeps the ready composer focused and saves model/effort choices to this draft only', async () => {
  const f = await fixture(); f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.queryByText('Change host')).toBeNull();
  expect(screen.queryByLabelText('Execution language')).toBeNull();
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  fireEvent.change(input, { target: { value: 'Explain the code' } });
  fireEvent.click(screen.getByRole('button', { name: 'Model', exact: true }));
  fireEvent.click(await screen.findByRole('option', { name: /gpt-5.5/ }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Reasoning effort' })).toHaveProperty('disabled', false));
  fireEvent.click(screen.getByRole('button', { name: 'Reasoning effort' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'High' }));
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ model: 'gpt-5.5', provider: 'openai', effort: 'high' });
  expect(f.rpc['providers.defaults']).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Explain the code');
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledWith(expect.objectContaining({ working_directory: '/project/whip', overrides: { model: { name: 'gpt-5.5', provider: 'openai', effort: 'high' } }, permission_mode: 'prompt', engine: 'starlark' }), undefined));
  await screen.findByText('Promoted to created');
  expect(f.rpc['sessions.submit']).toHaveBeenCalledWith(expect.objectContaining({ session_id: 'created', parts: [{ type: 'text', text: 'Explain the code' }] }), undefined);
  expect(f.run.mock.calls.map(call => call[1])).toEqual(['Create session', 'Send message']);
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('');
});

it('switches execution host without sending or losing the prompt and clears host-specific choices', async () => {
  const f = await fixture(); f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Keep this task' } });
  fireEvent.click(screen.getByRole('button', { name: 'Execution host' }));
  const item = await screen.findByRole('menuitem', { name: /Mac mini/ });
  expect(item.textContent).toContain('Disconnected'); expect(item.textContent).toContain('mm.local:8080');
  fireEvent.click(item);
  await waitFor(() => expect(f.connect).toHaveBeenCalledWith('remote'));
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ hostProfileId: 'remote', cwd: '' });
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep this task');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});

it('opens the native system picker directly from the local folder control', async () => {
  const f = await fixture(); f.render();
  const folder = await screen.findByRole('button', { name: 'Project folder' });
  expect(screen.queryByRole('button', { name: 'Browse host' })).toBeNull();
  fireEvent.click(folder);
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ cwd: '/selected/project' }));
  expect(f.pickDirectory).toHaveBeenCalledOnce();
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(f.rpc['host.directory.pick']).not.toHaveBeenCalled();
  expect(f.rpc['host.directories.list']).not.toHaveBeenCalled();
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});

it('uses remote recents and sends the confirmed remote directory with the retained draft', async () => {
  const f = await fixture(true); f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Explain the remote project' } });
  fireEvent.click(screen.getByRole('button', { name: 'Project folder' }));
  await screen.findByRole('dialog', { name: 'Choose a folder' });
  expect(screen.getByText('sam@kuzco')).toBeTruthy();
  fireEvent.click(await screen.findByRole('button', { name: 'recent', exact: true }));
  await waitFor(() => expect((screen.getByRole('button', { name: 'Choose folder', exact: true }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ cwd: '/remote/recent' });
  expect(f.pickDirectory).not.toHaveBeenCalled(); expect(f.rpc['host.directory.pick']).not.toHaveBeenCalled();
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Explain the remote project');
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledWith(expect.objectContaining({ working_directory: '/remote/recent' }), undefined));
});

it('keeps an unsupported saved effort visible and requires an explicit replacement before sending', async () => {
  const f = await fixture();
  f.runtime.tabs.updateNew(f.tab.id, { effort: 'high' });
  f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Use my saved choice' } });
  act(() => { f.runtime.queries.setQueryData(['provider-catalogs', 'host', ''], { ...f.catalog, providers: [] }); });
  await screen.findByText('Choose an available reasoning effort for this model before sending.');
  expect(screen.getByRole('button', { name: 'Reasoning effort' }).textContent).toContain('High');
  fireEvent.keyDown(screen.getByRole('textbox', { name: 'Your first message' }), { key: 'Enter' });
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ effort: 'high' });
  fireEvent.click(screen.getByRole('button', { name: 'Reasoning effort' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Default' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', false));
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledOnce());
  await waitFor(() => expect(f.rpc['trees.create']).toHaveBeenCalledWith(expect.objectContaining({ overrides: { model: { provider: 'openai', name: 'gpt-6-astra', effort: '' } } }), undefined));
});

it('preserves the collapsed provider top spacing while expanded and restores centering on collapse', async () => {
  const f = await fixture(false, false); f.render();
  const heading = await screen.findByRole('heading', { name: 'Connect a provider to get started' });
  const column = heading.parentElement!;
  const computedStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, 'getComputedStyle').mockImplementation(element => {
    const style = computedStyle(element);
    if (element === column) Object.defineProperty(style, 'marginTop', { value: '180px' });
    return style;
  });
  fireEvent.click(screen.getByRole('button', { name: 'Show all providers' }));
  expect(column.style.marginTop).toBe('180px');
  expect(column.style.marginBottom).toBe('0px');
  fireEvent.click(screen.getByRole('button', { name: 'Show fewer providers' }));
  expect(column.style.marginTop).toBe('');
  expect(column.style.marginBottom).toBe('');
});

it('shows standalone provider setup, reuses the connection dialog and restores the draft with the preset after connecting', async () => {
  const f = await fixture(false, false);
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Keep my task'); f.render();
  await screen.findByRole('heading', { name: 'Connect a provider to get started', level: 1 });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Project folder' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Remote' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Show all providers' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'fixture-api-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.getByRole('heading', { name: 'What do you want to work on?' }).parentElement!.style.marginTop).toBe('');
  expect((input as HTMLTextAreaElement).value).toBe('Keep my task');
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  expect(f.rpc['providers.create']).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ provider: 'openai', key: { id: expect.any(String), key: 'fixture-api-key' } }), expect.anything());
  expect(f.rpc['providers.defaults']).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ defaults: expect.objectContaining({ selection: { name: 'gpt-6-astra', provider: 'openai', effort: 'medium' } }) }), expect.anything());
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});

it('keeps the composer footprint while the provider inventory is pending, then defers to setup', async () => {
  const f = await fixture(false, false);
  f.runtime.queries.removeQueries({ queryKey: ['provider-list', 'host'] });
  let resolveList!: (value: unknown) => void;
  f.rpc['providers.list'].mockImplementation(() => new Promise(resolve => { resolveList = resolve; }));
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.getByRole('heading', { name: 'What do you want to work on?', level: 1 })).toBeTruthy();
  expect(screen.queryByText('Connect a provider')).toBeNull();
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Reasoning effort' })).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(2);
  await act(async () => { resolveList(f.inventory); });
  await screen.findByRole('heading', { name: 'Connect a provider to get started', level: 1 });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
});

it('renders the composer disabled while the host is still connecting', async () => {
  const f = await fixture();
  vi.mocked(f.runtime.getSnapshot).mockReturnValue({ ...f.runtime.getSnapshot(), hosts: f.runtime.getSnapshot().hosts.map(host => host.id === 'local' ? { ...host, state: 'connecting' } : host) });
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.getByRole('heading', { name: 'What do you want to work on?', level: 1 })).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Send first message' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText(/^Reconnecting to Local\./)).toBeTruthy();
  expect(screen.queryByText('Review unavailable session options')).toBeNull();
  expect(screen.getByRole('button', { name: 'Model', exact: true })).toHaveProperty('disabled', true);
});

it('paints provider setup first when the device remembers this host has no ready provider', async () => {
  const f = await fixture(false, false);
  f.runtime.queries.removeQueries({ queryKey: ['provider-list', 'host'] });
  f.runtime.platform.storage.setItem('whip.web.provider-ready.v1:host', 'false');
  f.rpc['providers.list'].mockImplementation(() => new Promise(() => {}));
  f.render();
  await screen.findByRole('heading', { name: 'Connect a provider to get started', level: 1 });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
});

it('remembers the inventory answer on the device', async () => {
  const f = await fixture();
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  await waitFor(() => expect(f.runtime.platform.storage.getItem('whip.web.provider-ready.v1:host')).toBe('true'));
});

it('keeps the draft and shows the delivery notice while the first send is unresolved', async () => {
  const f = await fixture();
  const checkCommand = vi.spyOn(f.runtime, 'checkCommand').mockResolvedValue(undefined);
  vi.mocked(f.runtime.getSnapshot).mockReturnValue({ ...f.runtime.getSnapshot(), commands: [{ id: 'pending', commandId: 'c1', runtimeId: 'host', label: 'Create session', status: 'Acceptance unresolved', draftKey: welcomeDraftKey(f.tab.id), delivery: 'uncertain' }] });
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Still here');
  f.render();
  expect((await screen.findByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement).value).toBe('Still here');
  expect(screen.getByText('Checking whether your first message was received')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Check status' }));
  expect(checkCommand).toHaveBeenCalledWith('pending');
});

it('keeps the draft and reports the error when the first send fails before acceptance', async () => {
  const f = await fixture();
  f.run.mockImplementationOnce((async () => { throw new Error('Host refused the session'); }) as never);
  f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Try this' } });
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [imageFile()] } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await screen.findByText('Host refused the session');
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Try this');
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ kind: 'new' });
  expect(f.runtime.compositions.get(welcomeDraftKey(f.tab.id)).attachments[0]).toMatchObject({ staged: true, previewUrl: 'blob:first.png' });
  expect(f.rpc['content.put']).not.toHaveBeenCalled();
  expect(URL.revokeObjectURL).not.toHaveBeenCalled();
});

function imageFile(name = 'first.png') {
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = vi.fn(() => `blob:${name}`);
    static revokeObjectURL = vi.fn();
  });
  return Object.assign(new File(['image'], name, { type: 'image/png' }), {
    arrayBuffer: vi.fn(async () => new TextEncoder().encode('image').buffer),
  });
}

it.each(['picker', 'paste', 'drop'])('stages a first image through %s without creating a session or uploading', async method => {
  const f = await fixture(); f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  const image = imageFile();
  expect((screen.getByRole('button', { name: 'Attach text or images' }) as HTMLButtonElement).disabled).toBe(false);
  if (method === 'picker') fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [image] } });
  else if (method === 'paste') fireEvent.paste(input, { clipboardData: { files: [image] } });
  else fireEvent.drop(input, { dataTransfer: { files: [image] } });
  fireEvent.load(screen.getByRole('img', { name: 'first.png' }));
  expect(screen.queryByRole('img', { name: 'Uploading first.png' })).toBeNull();
  expect(image.arrayBuffer).not.toHaveBeenCalled();
  expect(f.rpc['content.put']).not.toHaveBeenCalled();
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  expect((screen.getByRole('button', { name: 'Send first message' }) as HTMLButtonElement).disabled).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Remove first.png' }));
  expect(f.runtime.compositions.getSnapshot().attachmentCount).toBe(0);
  expect(URL.revokeObjectURL).toHaveBeenCalledExactlyOnceWith('blob:first.png');
});

it('sends multiple images without text only after scoped uploads finish and clears previews on acceptance', async () => {
  const f = await fixture(); f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  const first = imageFile(); const second = imageFile('second.png');
  let release!: () => void;
  const uploaded = f.rpc['content.put'].getMockImplementation()!;
  f.rpc['content.put'].mockImplementationOnce((...args) => new Promise(resolve => { release = () => resolve(uploaded(...args)); }));
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [first, second] } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  // Repeated Enter/click cannot create a second session while awaiting upload.
  await screen.findByText('Promoted to created');
  expect(f.rpc['trees.create']).toHaveBeenCalledOnce();
  expect(f.rpc['sessions.submit']).not.toHaveBeenCalled();
  expect(f.runtime.compositions.get('host:created:created').sending).toBe(true);
  await waitFor(() => expect(f.rpc['content.put']).toHaveBeenCalled());
  await act(async () => release());
  await waitFor(() => expect(f.rpc['sessions.submit']).toHaveBeenCalledWith(expect.objectContaining({ session_id: 'created', parts: [
    { type: 'content', reference_id: expect.any(String) }, { type: 'content', reference_id: expect.any(String) },
  ] }), undefined));
  expect(f.rpc['content.put']).toHaveBeenCalledTimes(2);
  expect(f.rpc['content.put']).toHaveBeenCalledWith(expect.objectContaining({ session_id: 'created', media_type: 'image/png' }), expect.any(AbortSignal));
  await waitFor(() => expect(f.runtime.compositions.getSnapshot().attachmentCount).toBe(0));
  expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2);
});

it('keeps local attachments with the new draft across closing/reopening and changing hosts', async () => {
  const f = await fixture(); f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [imageFile()] } });
  const attachments = f.runtime.compositions.get(welcomeDraftKey(f.tab.id)).attachments;
  act(() => { f.runtime.tabs.closeViews([f.tab.id]); });
  act(() => { f.runtime.tabs.reopenView(f.tab.id); });
  await screen.findByRole('button', { name: 'Preview first.png' });
  fireEvent.click(screen.getByRole('button', { name: 'Execution host' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: /Mac mini/ }));
  await waitFor(() => expect(f.connect).toHaveBeenCalledWith('remote'));
  expect(f.runtime.compositions.get(welcomeDraftKey(f.tab.id)).attachments).toBe(attachments);
  expect(f.rpc['content.put']).not.toHaveBeenCalled();
  expect(URL.revokeObjectURL).not.toHaveBeenCalled();
});

it.each(['upload', 'send'])('preserves the draft in its created session after %s failure', async phase => {
  const f = await fixture(); f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Keep my image' } });
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [imageFile()] } });
  if (phase === 'upload') f.rpc['content.put'].mockRejectedValueOnce(new Error('Upload unavailable'));
  else {
    const run = f.run.getMockImplementation()!;
    f.run.mockImplementation(((...args: Parameters<AppRuntime['run']>) => {
      if (args[1] === 'Send message') throw new Error('Submission refused');
      return run(...args);
    }) as never);
  }
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await screen.findByText('Promoted to created');
  await waitFor(() => expect(f.runtime.compositions.get('host:created:created').sending).toBe(false));
  expect(f.rpc['trees.create']).toHaveBeenCalledOnce();
  expect(f.runtime.draft('host:created:created')).toBe('Keep my image');
  expect(f.runtime.compositions.get('host:created:created').attachments[0]?.previewUrl).toBe('blob:first.png');
  expect(URL.revokeObjectURL).not.toHaveBeenCalled();
  if (phase === 'upload') expect(f.rpc['sessions.submit']).not.toHaveBeenCalled();
});

// Import discovery is scoped to global declarations before a session exists.
function withMCPImport(f: Awaited<ReturnType<typeof fixture>>, offered = false) {
  const configuration = { ...wire('MCPConfiguration'), revision, imports: { claude: null, codex: null, project: null, opencode: null, offered } };
  f.on('mcp.configuration', () => configuration);
  const candidates = f.on('mcp.import.candidates', () => ({ ...wire('MCPImportCandidatesResult'), revision, candidates: [{ ...wire('MCPImportCandidatesResult').candidates[0], name: 'paper' }] }));
  const apply = f.on('mcp.import.apply', () => ({ configuration: { ...configuration, imports: { ...configuration.imports, offered: true } }, added: [], skipped: {} }));
  return { candidates, apply };
}
it('offers global MCP import once a provider is ready and returns the composer after Skip', async () => {
  const f = await fixture(); const mcp = withMCPImport(f); f.render();
  await screen.findByRole('heading', { name: 'Bring your MCP servers into Whip' });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(screen.getByRole('checkbox', { name: 'Import paper' })).toBeTruthy();
  expect(mcp.candidates).toHaveBeenCalledWith({ session_id: null }, expect.any(AbortSignal));
  fireEvent.click(screen.getByRole('button', { name: 'Skip for now' }));
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(mcp.apply).toHaveBeenCalledExactlyOnceWith({ session_id: null, revision, fingerprints: {} }, expect.any(AbortSignal));
});
it.each([false, true])('does not offer import before provider readiness or after an answer (ready=%s)', async ready => {
  const f = await fixture(false, ready); const mcp = withMCPImport(f, ready); f.render();
  if (ready) await screen.findByRole('textbox', { name: 'Your first message' });
  else await screen.findByRole('region', { name: 'Provider setup' });
  expect(screen.queryByRole('heading', { name: 'Bring your MCP servers into Whip' })).toBeNull();
  expect(mcp.candidates).not.toHaveBeenCalled();
});
it('requires an exact revision for a saved mutable agent name', async () => {
  const f = await fixture(); f.runtime.tabs.updateNew(f.tab.id, { unresolvedDefinition: 'coding' }); f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Keep my draft' } });
  expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('button', { name: 'Review unavailable session options' }));
  fireEvent.click(await screen.findByRole('combobox', { name: 'Agent' }));
  const revisionChoice = await screen.findByRole('option', { name: /Coding · coding @ aaaaaaaaaaaa/ });
  fireEvent.pointerDown(revisionChoice); fireEvent.click(revisionChoice);
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ definition: { id: 'coding', revision } }));
  expect(f.runtime.tabs.workspace().tabs[0]).not.toHaveProperty('unresolvedDefinition');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
