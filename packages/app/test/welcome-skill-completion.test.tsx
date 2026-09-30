import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it } from 'vitest';
import { fixture } from './welcome-fixture';
import { revision } from './provider-fixture';
import { welcomeDraftKey } from '../src/session-tabs';

async function skillFixture() {
  const f = await fixture();
  const call = f.on('host.skills.complete', () => ({ candidates: [{ text: '$ponytail', description: 'Least code that works.' }], truncated: false }));
  return { ...f, call };
}
async function typeSlash(text = '/') {
  const input = await screen.findByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement;
  act(() => input.focus()); fireEvent.change(input, { target: { value: text } }); return input;
}
it('allows metadata-only skill completion in the composer; repeat Enter does not send', async () => {
  const f = await skillFixture(); f.render();
  const input = await typeSlash('/po');
  await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith({ scope: 'project', cwd: '/project/whip', definition: { id: 'coding', revision }, prefix: '', limit: 1024 }, expect.any(AbortSignal));
  fireEvent.keyDown(input, { key: 'Enter' }); expect(input.value).toBe('$ponytail ');
  fireEvent.keyDown(input, { key: 'Enter', repeat: true });
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('$ponytail ');
  expect(screen.getByRole('button', { name: 'Send first message' })).toHaveProperty('disabled', false);
  expect(f.rpc['sessions.submit']).not.toHaveBeenCalled();
});
it('filters a Unicode host name and inserts its canonical reference', async () => {
  const f = await skillFixture();
  f.call.mockResolvedValue({ candidates: [{ text: '$café', description: 'Unicode host skill' }], truncated: false });
  f.render(); const input = await typeSlash('/café');
  await screen.findByRole('option', { name: /café/ });
  fireEvent.keyDown(input, { key: 'Enter' }); expect(input.value).toBe('$café ');
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('dismisses suggestions without allowing late catalog results to reopen them', async () => {
  const f = await skillFixture(); let resolve!: (value: unknown) => void;
  f.call.mockImplementationOnce(() => new Promise(done => { resolve = done; }));
  f.render(); const input = await typeSlash('/p');
  await waitFor(() => expect(f.call).toHaveBeenCalledOnce());
  fireEvent.keyDown(input, { key: 'Enter' }); fireEvent.keyDown(input, { key: 'Escape' });
  await act(async () => resolve({ candidates: [{ text: '$ponytail', description: '' }], truncated: false }));
  expect(screen.queryByRole('listbox')).toBeNull();
  fireEvent.select(input); expect(screen.queryByRole('listbox')).toBeNull();
  fireEvent.change(input, { target: { value: '/po' } }); await screen.findByRole('option', { name: /ponytail/ });
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('scope changes cancel the old authority and preserve the authored draft', async () => {
  const f = await skillFixture(); f.call.mockImplementationOnce(() => new Promise(() => {}));
  f.render(); const input = await typeSlash('/po');
  await waitFor(() => expect(f.call).toHaveBeenCalledOnce());
  const signal = f.call.mock.calls[0]![1] as AbortSignal;
  act(() => f.runtime.tabs.updateNew(f.tab.id, { cwd: '/other' }));
  await waitFor(() => expect(signal.aborted).toBe(true));
  expect(screen.queryByRole('listbox')).toBeNull(); expect(input.value).toBe('/po');
  await waitFor(() => expect(f.call).toHaveBeenLastCalledWith(expect.objectContaining({ cwd: '/other' }), expect.any(AbortSignal)));
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('IME Enter and compatibility key229 neither accept nor send', async () => {
  const f = await skillFixture(); f.render(); const input = await typeSlash();
  await screen.findByRole('option', { name: /ponytail/ });
  fireEvent.compositionStart(input); fireEvent.keyDown(input, { key: 'Enter', isComposing: true }); fireEvent.compositionEnd(input);
  fireEvent.keyDown(input, { key: 'Enter', keyCode: 229 });
  expect(input.value).toBe('/'); expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it.each(['/project/whip', ''])('reads exact immutable definition metadata with the appropriate native scope (cwd=%s)', async cwd => {
  const f = await skillFixture();
  f.runtime.tabs.updateNew(f.tab.id, { cwd, definition: { id: 'research', revision: 'b'.repeat(64) }, permissionMode: 'automatic' });
  f.render(); await typeSlash('/po'); await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith({ scope: cwd ? 'project' : 'global', cwd, definition: { id: 'research', revision: 'b'.repeat(64) }, prefix: '', limit: 1024 }, expect.any(AbortSignal));
});
it.each(['/project/whip', ''])('warms only after permission readiness resolves, without requiring a slash edit (cwd=%s)', async cwd => {
  const f = await skillFixture(); f.runtime.tabs.updateNew(f.tab.id, { cwd });
  f.runtime.queries.removeQueries({ queryKey: ['host-permission-default', 'host', 'boot'] });
  f.rpc['host.permission_default'].mockImplementation(() => new Promise(() => {}));
  f.render(); const input = await screen.findByRole('textbox', { name: 'Your first message' }); act(() => input.focus());
  expect(f.call).not.toHaveBeenCalled();
  act(() => f.runtime.queries.setQueryData(['host-permission-default', 'host', 'boot'], { revision, mode: 'prompt' }));
  await waitFor(() => expect(f.call).toHaveBeenCalledWith(expect.objectContaining({ cwd, prefix: '', limit: 1024 }), expect.any(AbortSignal)));
  expect(document.activeElement).toBe(input); expect(f.call).toHaveBeenCalledOnce();
});
it('preloads the ready catalog and filters warm typing without extra calls', async () => {
  const f = await skillFixture(); f.render(); const input = await typeSlash();
  await screen.findByRole('option', { name: /ponytail/ });
  for (const value of ['/p', '/po', '/p']) { fireEvent.change(input, { target: { value } }); expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy(); }
  fireEvent.change(input, { target: { value: '/none' } }); expect(screen.getByText('No matching skills.')).toBeTruthy();
  fireEvent.keyDown(input, { key: 'Enter' }); expect(f.rpc['trees.create']).not.toHaveBeenCalled(); expect(f.call).toHaveBeenCalledOnce();
});
it('falls back to a bounded prefix query when the warmed catalog is incomplete', async () => {
  const f = await skillFixture(); f.call.mockResolvedValueOnce({ candidates: [], truncated: true });
  f.render(); const input = await typeSlash('/po'); await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenLastCalledWith(expect.objectContaining({ prefix: 'po', limit: 32 }), expect.any(AbortSignal));
  fireEvent.keyDown(input, { key: 'Enter' }); expect(input.value).toBe('$ponytail '); expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('inserts a global reference but still requires a project folder before sending', async () => {
  const f = await skillFixture(); f.runtime.tabs.updateNew(f.tab.id, { cwd: '' });
  f.render(); const input = await typeSlash('Use /po'); await screen.findByRole('option', { name: /ponytail/ });
  fireEvent.keyDown(input, { key: 'Enter' }); expect(input.value).toBe('Use $ponytail ');
  fireEvent.keyDown(input, { key: 'Enter' }); await screen.findByText('Choose a project folder on this host before sending.');
  expect(f.runtime.tabs.workspace().tabs[0]).toMatchObject({ kind: 'new', cwd: '' }); expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
it('an explicit permission mode permits global discovery while host defaults are unresolved', async () => {
  const f = await skillFixture(); f.runtime.tabs.updateNew(f.tab.id, { cwd: '', permissionMode: 'automatic' });
  f.runtime.queries.removeQueries({ queryKey: ['host-permission-default', 'host', 'boot'] });
  f.rpc['host.permission_default'].mockImplementation(() => new Promise(() => {}));
  f.render(); await typeSlash(); await screen.findByRole('option', { name: /ponytail/ });
  expect(f.call).toHaveBeenCalledWith({ scope: 'global', cwd: '', definition: { id: 'coding', revision }, prefix: '', limit: 1024 }, expect.any(AbortSignal));
  expect(f.rpc['trees.create']).not.toHaveBeenCalled();
});
