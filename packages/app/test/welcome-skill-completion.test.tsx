import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { fixture } from './welcome-fixture';
import { welcomeDraftKey } from '../src/session-tabs';

function skillFixture(ready = true, supported = true, catalog = false, globalSupported = true) {
  const f = fixture(false, ready);
  const snapshot = f.raw.getSnapshot();
  const call = vi.fn(async (_method: string, _params: unknown, _options: unknown) => ({ candidates: [{ text: '$ponytail', description: 'Least code that works.' }], truncated: false }));
  const connected = { ...snapshot, info: { ...snapshot.info, negotiated_capabilities: supported ? ['host_skill_completion', ...(globalSupported ? ['host_global_skill_completion'] : []), ...(catalog ? ['skill_catalog_completion'] : [])] : [] } };
  Object.assign(f.raw, { getSnapshot: () => connected, call });
  return { ...f, call };
}
async function typeSlash(text = '/') {
  const input = await screen.findByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement;
  act(() => input.focus());
  fireEvent.change(input, { target: { value: text } });
  return input;
}
it('keeps skills behind provider setup, then inserts without creating a session; repeat Enter does not send', async () => {
  const f = skillFixture(false); f.render();
  await screen.findByRole('region', { name: 'Provider setup' });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(f.call).not.toHaveBeenCalled();
  act(() => f.runtime.queries.setQueryData(['provider-list', 'host'], {
    ...f.inventory, selection: { ...f.inventory.selection, ready: true },
    providers: [{ ...f.inventory.providers[0], status: { available: true, key_source: 'literal' } }],
  }));
  const input = await typeSlash('/po');
  await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith('host.skills.complete', { cwd: '/project/whip', definition: 'coding', permission_mode: 'prompt', prefix: 'po', limit: 32 }, { signal: expect.any(AbortSignal) });
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(input.value).toBe('$ponytail ');
  fireEvent.keyDown(input, { key: 'Enter', repeat: true });
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('$ponytail ');
});
it('filters a warned Unicode host name and inserts its canonical reference', async () => {
  const f = skillFixture();
  const response = { candidates: [{ text: '$café', description: 'Unicode host skill' }], truncated: false, warnings: ['Skill name café is outside the specification.'] };
  f.call.mockResolvedValue(response);
  f.render(); const input = await typeSlash('/café');
  await screen.findByRole('option', { name: /café/ });
  expect(screen.getByText('Skill name café is outside the specification.')).toBeTruthy();
  expect(f.call).toHaveBeenCalledWith('host.skills.complete', expect.objectContaining({ prefix: 'café' }), { signal: expect.any(AbortSignal) });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(input.value).toBe('$café ');
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('$café ');
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});
it.each([['unsupported', 'Skill suggestions require a newer host.'], ['no-folder', 'Update this host or choose a project folder to browse skills.']])('shows %s without querying or sending on Enter', async (mode, message) => {
  const f = skillFixture(true, mode !== 'unsupported', false, false);
  if (mode === 'no-folder') f.runtime.tabs.updateNew(f.tab.id, { cwd: '' });
  f.render(); const input = await typeSlash();
  await screen.findByText(message);
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(f.raw.sessions.create).not.toHaveBeenCalled(); expect(f.call).not.toHaveBeenCalled();
  fireEvent.keyDown(input, { key: 'Escape' });
  expect(input.value).toBe('/');
  expect(screen.queryByRole('listbox')).toBeNull();
});
it('dismissal cancels the request and late results cannot reopen it; editing deliberately reopens', async () => {
  const f = skillFixture();
  let resolve!: (value: { candidates: { text: string; description: string }[]; truncated: boolean }) => void;
  f.call.mockImplementationOnce(() => new Promise(done => { resolve = done; }));
  f.render(); const input = await typeSlash('/p');
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(1));
  const signal = (f.call.mock.calls[0]![2] as { signal: AbortSignal }).signal;
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  fireEvent.keyDown(input, { key: 'Escape' });
  await waitFor(() => expect(signal.aborted).toBe(true));
  await act(async () => resolve({ candidates: [{ text: '$previous', description: '' }], truncated: false }));
  expect(screen.queryByRole('listbox')).toBeNull();
  fireEvent.select(input); expect(screen.queryByRole('listbox')).toBeNull();
  fireEvent.change(input, { target: { value: '/po' } });
  await screen.findByRole('option', { name: /ponytail/ });
});
it('scope changes close suggestions and cancel the old authority without changing the draft', async () => {
  const f = skillFixture();
  f.call.mockImplementationOnce(() => new Promise(() => {}));
  f.render(); const input = await typeSlash('/po');
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(1));
  const signal = (f.call.mock.calls[0]![2] as { signal: AbortSignal }).signal;
  act(() => f.runtime.tabs.updateNew(f.tab.id, { cwd: '/other' }));
  await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
  expect(signal.aborted).toBe(true); expect(input.value).toBe('/po');
  act(() => f.runtime.tabs.updateNew(f.tab.id, { cwd: '/project/whip' }));
  expect(screen.queryByRole('listbox')).toBeNull();
  expect(f.call).toHaveBeenCalledTimes(1);
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});
it('IME Enter and compatibility key 229 neither accept nor send', async () => {
  const f = skillFixture(); f.render(); const input = await typeSlash();
  await screen.findByRole('option', { name: /ponytail/ });
  fireEvent.compositionStart(input);
  fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
  fireEvent.compositionEnd(input);
  fireEvent.keyDown(input, { key: 'Enter', keyCode: 229 });
  expect(input.value).toBe('/'); expect(f.raw.sessions.create).not.toHaveBeenCalled();
});

it.each(['/project/whip', ''])('uses the effective host permission default for skill completions (cwd=%s)', async cwd => {
  const f = skillFixture();
  f.runtime.tabs.updateNew(f.tab.id, { cwd });
  f.runtime.queries.setQueryData(['runtime-configuration', 'host'], { default_permission_mode: 'automatic' });
  f.render(); await typeSlash('/po');
  await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith('host.skills.complete', expect.objectContaining({ permission_mode: 'automatic' }), { signal: expect.any(AbortSignal) });
});

it.each(['/project/whip', ''])('does not complete skills with an unknown permission default (cwd=%s)', async cwd => {
  const f = skillFixture();
  f.runtime.tabs.updateNew(f.tab.id, { cwd });
  f.runtime.queries.removeQueries({ queryKey: ['runtime-configuration', 'host'] });
  f.raw.configuration.get.mockImplementation(() => new Promise(() => {}));
  f.render(); await typeSlash('/po');
  expect(screen.queryByRole('listbox', { name: 'Skills' })).toBeNull();
  expect(f.call).not.toHaveBeenCalled();
  act(() => { f.runtime.queries.setQueryData(['runtime-configuration', 'host'], { default_permission_mode: 'automatic' }); });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Full Access'));
  await typeSlash('/pon');
  await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith('host.skills.complete', expect.objectContaining({ permission_mode: 'automatic' }), { signal: expect.any(AbortSignal) });
});

it('preloads the ready welcome catalog and filters warm typing without extra calls', async () => {
  const f = skillFixture(true, true, true); f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement;
  act(() => input.focus());
  await waitFor(() => expect(f.call).toHaveBeenCalledWith('host.skills.complete', expect.objectContaining({ prefix: '', limit: 1024 }), { signal: expect.any(AbortSignal) }));
  fireEvent.change(input, { target: { value: '/' } }); await screen.findByRole('option', { name: /ponytail/ });
  for (const value of ['/p', '/po', '/p']) {
    fireEvent.change(input, { target: { value } });
    expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy();
  }
  fireEvent.change(input, { target: { value: '/none' } }); expect(screen.getByText('No matching skills.')).toBeTruthy();
  fireEvent.keyDown(input, { key: 'Enter' }); expect(f.raw.sessions.create).not.toHaveBeenCalled();
  expect(f.call).toHaveBeenCalledTimes(1);
});

it.each(['/project/whip', ''])('preloads when permission readiness resolves after focus without a slash edit (cwd=%s)', async cwd => {
  const f = skillFixture(true, true, true);
  f.runtime.tabs.updateNew(f.tab.id, { cwd });
  f.runtime.queries.removeQueries({ queryKey: ['runtime-configuration', 'host'] });
  f.raw.configuration.get.mockImplementation(() => new Promise(() => {}));
  f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  act(() => input.focus());
  expect(document.activeElement).toBe(input); expect(f.call).not.toHaveBeenCalled();
  act(() => f.runtime.queries.setQueryData(['runtime-configuration', 'host'], { default_permission_mode: 'prompt' }));
  await waitFor(() => expect(f.call).toHaveBeenCalledWith('host.skills.complete', expect.objectContaining({ permission_mode: 'prompt', prefix: '', limit: 1024 }), { signal: expect.any(AbortSignal) }));
  expect(document.activeElement).toBe(input);
  expect(screen.getByRole('textbox', { name: 'Your first message' })).toBe(input);
  expect(f.call).toHaveBeenCalledTimes(1); expect(f.raw.sessions.create).not.toHaveBeenCalled();
});
it('preloads globals before choosing a project and inserts without creating a session', async () => {
  const f = skillFixture(true, true, true);
  f.runtime.tabs.updateNew(f.tab.id, { cwd: '' });
  f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement;
  act(() => input.focus());
  expect(document.activeElement).toBe(input);
  await waitFor(() => expect(f.call).toHaveBeenCalledWith('host.skills.complete', {
    scope: 'global', definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024,
  }, { signal: expect.any(AbortSignal) }));
  fireEvent.change(input, { target: { value: 'Use /po' } });
  await screen.findByRole('option', { name: /ponytail/ });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(input.value).toBe('Use $ponytail ');
  expect(input.selectionStart).toBe(input.value.length);
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe(input.value);
  expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ kind: 'new', cwd: '' });
  expect(f.call).toHaveBeenCalledTimes(1);
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  expect((screen.getByRole('button', { name: 'Send first message' }) as HTMLButtonElement).disabled).toBe(true);
});

it('still requires a project to send an inserted global reference with a ready provider', async () => {
  const f = skillFixture(true, true, true);
  f.runtime.tabs.updateNew(f.tab.id, { cwd: '' });
  f.render(); const input = await typeSlash('/po');
  await screen.findByRole('option', { name: /ponytail/ });
  fireEvent.keyDown(input, { key: 'Enter' });
  fireEvent.keyDown(input, { key: 'Enter' });
  await screen.findByText('Choose a project folder on this host before sending.');
  expect(input.value).toBe('$ponytail ');
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});

it('an old host makes no global call, then uses the unchanged project request after picking a folder', async () => {
  const f = skillFixture(true, true, true, false);
  f.runtime.tabs.updateNew(f.tab.id, { cwd: '' });
  f.render(); const input = await typeSlash('/po');
  await screen.findByText('Update this host or choose a project folder to browse skills.');
  expect(f.call).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Project folder', exact: true }));
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ cwd: '/selected/project' }));
  act(() => input.focus());
  await waitFor(() => expect(f.call).toHaveBeenCalledWith('host.skills.complete', {
    cwd: '/selected/project', definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024,
  }, { signal: expect.any(AbortSignal) }));
  expect(input.value).toBe('/po');
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});

it('an explicit permission mode permits global discovery while host defaults are unresolved', async () => {
  const f = skillFixture(true, true, true);
  f.runtime.tabs.updateNew(f.tab.id, { cwd: '', definition: 'research', permissionMode: 'automatic' });
  f.runtime.queries.removeQueries({ queryKey: ['runtime-configuration', 'host'] });
  f.raw.configuration.get.mockImplementation(() => new Promise(() => {}));
  f.render(); await typeSlash();
  await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith('host.skills.complete', {
    scope: 'global', definition: 'research', permission_mode: 'automatic', prefix: '', limit: 1024,
  }, { signal: expect.any(AbortSignal) });
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});
