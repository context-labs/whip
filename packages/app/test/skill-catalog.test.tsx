import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useRef, useState } from 'react';
import { UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import type { CompletionResult } from '@whip/protocol';
import { useSkillCompletion } from '../src/use-skill-completion';

type Scope = Parameters<typeof useSkillCompletion>[0]['scope'];
const workspace: Scope = { rootId: 'root', agentId: 'child' };
const welcome: Scope = { cwd: '/project', definition: 'coding', permissionMode: 'prompt' };
const global: Scope = { cwd: '', definition: 'coding', permissionMode: 'prompt' };
const catalog: CompletionResult = { candidates: [...Array.from({ length: 70 }, (_, i) => ({ text: `$alpha${i}`, description: '' })),
  { text: '$ponytail', description: 'Least code' }, { text: '$café', description: 'Unicode' }], truncated: false };

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })));
});
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });
function fixture(scope: Scope = workspace, catalogSupported = true) {
  const call = vi.fn((_method: string, _params: unknown, _options: unknown): Promise<CompletionResult> => Promise.resolve(catalog));
  const client = { getSnapshot: () => ({ info: { runtime_id: 'host', negotiated_capabilities:
    ['workspace_completion', 'host_skill_completion', 'host_global_skill_completion', ...(catalogSupported ? ['skill_catalog_completion'] : [])] } }), call } as unknown as WhipClient;
  const queries = new QueryClient({ defaultOptions: { queries: { staleTime: 10_000, gcTime: 0, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false } } });
  const send = vi.fn();
  function Composer({ current = scope, connected = true, blocked = false, host = client }: { current?: Scope; connected?: boolean; blocked?: boolean; host?: WhipClient }) {
    const input = useRef<HTMLTextAreaElement>(null);
    const [draft, setDraft] = useState('');
    const skills = useSkillCompletion({ client: host, owner: 'composer', scope: current, connected, blocked, draft, input, change: setDraft });
    return <><textarea ref={input} aria-label="Draft" {...skills.inputProps} value={draft}
      onChange={e => { setDraft(e.target.value); skills.onChange(); }} onSelect={skills.onSelect}
      onKeyDown={e => { if (!skills.onKeyDown(e) && e.key === 'Enter') send(); }} />{skills.popup}</>;
  }
  const app = (props = {}) => <QueryClientProvider client={queries}><UIProvider><Composer {...props} /></UIProvider></QueryClientProvider>;
  const mounted = render(app());
  const input = screen.getByRole('textbox', { name: 'Draft' }) as HTMLTextAreaElement;
  const focus = () => act(() => input.focus());
  const type = (value: string) => fireEvent.change(input, { target: { value } });
  return { call, client, queries, input, focus, type, send, mounted, app };
}
async function loaded(f: ReturnType<typeof fixture>) {
  await waitFor(() => expect(f.queries.getQueryCache().findAll({ queryKey: ['slash-skills'] }).some(q => q.state.data === catalog)).toBe(true));
  // Query notifications have reached the observer before testing synchronous edits.
  f.type('/'); await screen.findByRole('option', { name: '/alpha0' });
}

it.each([['workspace', workspace, 'workspace.complete'], ['welcome', welcome, 'host.skills.complete'], ['global', global, 'host.skills.complete']] as const)('%s preloads once on focus, filters immediately before the row cap, and never fetches on warm typing', async (_name, scope, method) => {
  const f = fixture(scope);
  expect(f.call).not.toHaveBeenCalled(); f.focus();
  await loaded(f);
  expect(f.call).toHaveBeenCalledTimes(1);
  expect(f.call).toHaveBeenCalledWith(method, expect.objectContaining({ prefix: '', limit: 1024 }), { signal: expect.any(AbortSignal) });
  expect(screen.getAllByRole('option')).toHaveLength(32);
  expect(screen.queryByRole('status')).toBeNull();
  expect(screen.queryByText(/Selection inserts a skill reference|Keep typing to narrow the list/)).toBeNull();
  for (const prefix of ['/p', '/po', '/pon', '/p']) {
    f.type(prefix); expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy();
    expect(screen.queryByText('Loading skills…')).toBeNull();
  }
  f.type('/P'); expect(screen.getByText('No matching skills.')).toBeTruthy();
  f.type('/café'); expect(screen.getByRole('option', { name: /café/ })).toBeTruthy();
  vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 11_000);
  f.type('/po'); expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy();
  expect(f.call).toHaveBeenCalledTimes(1);
  vi.restoreAllMocks();
  fireEvent.keyDown(f.input, { key: 'Enter' }); expect(f.input.value).toBe('$ponytail '); expect(f.send).not.toHaveBeenCalled();
});

it.each([workspace, global])('keeps cold preload through Escape and cancels only on disposal (%j)', async scope => {
  const f = fixture(scope); let finish!: (value: CompletionResult) => void;
  f.call.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  f.focus(); f.type('/po'); await waitFor(() => expect(f.call).toHaveBeenCalledTimes(1));
  const signal = (f.call.mock.calls[0]![2] as { signal: AbortSignal }).signal;
  fireEvent.keyDown(f.input, { key: 'Enter' }); expect(f.send).not.toHaveBeenCalled();
  expect(f.input.value).toBe('/po');
  fireEvent.keyDown(f.input, { key: 'Escape' }); expect(signal.aborted).toBe(false);
  await act(async () => finish(catalog)); expect(screen.queryByRole('listbox')).toBeNull();
  f.type('/pon'); await screen.findByRole('option', { name: /ponytail/ }); expect(f.call).toHaveBeenCalledTimes(1);
  f.mounted.unmount(); await waitFor(() => expect(f.queries.getQueryCache().getAll()).toHaveLength(0));
});

it.each([workspace, global])('refreshes stale reopen with retryable same-scope matches (%j)', async scope => {
  const f = fixture(scope); f.focus(); await loaded(f);
  fireEvent.keyDown(f.input, { key: 'Escape' });
  f.type('/p'); expect(f.call).toHaveBeenCalledTimes(1);
  fireEvent.keyDown(f.input, { key: 'Escape' });
  vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 11_000);
  let fail!: (error: Error) => void;
  f.call.mockImplementationOnce(() => new Promise((_resolve, reject) => { fail = reject; }));
  f.type('/po'); expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy();
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(2));
  expect(screen.queryByText('Loading skills…')).toBeNull();
  await act(async () => fail(new Error('Offline metadata')));
  await screen.findByText('Showing saved skills. Refresh failed.');
  expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(3));
  vi.restoreAllMocks();
});

it('delays only the cold indicator by 150ms and cancels it on dismissal', async () => {
  vi.useFakeTimers(); const f = fixture();
  f.call.mockImplementation(() => new Promise(() => {}));
  f.focus(); f.type('/');
  expect(f.call).toHaveBeenCalledTimes(1); expect(screen.queryByText('Loading skills…')).toBeNull();
  await act(async () => { await vi.advanceTimersByTimeAsync(149); });
  expect(screen.queryByText('Loading skills…')).toBeNull();
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(screen.getByText('Loading skills…')).toBeTruthy();
  fireEvent.keyDown(f.input, { key: 'Escape' });
  await act(async () => { await vi.advanceTimersByTimeAsync(200); });
  expect(screen.queryByRole('listbox')).toBeNull(); f.mounted.unmount();
});

it('starts when readiness arrives while focused and refetches through runtime reconnect invalidation', async () => {
  const f = fixture(welcome); f.mounted.rerender(f.app({ blocked: true })); f.focus();
  expect(f.call).not.toHaveBeenCalled();
  f.mounted.rerender(f.app()); await loaded(f);
  f.mounted.rerender(f.app({ connected: false })); expect(screen.queryByRole('option')).toBeNull();
  await act(async () => { await f.queries.invalidateQueries({ predicate: q => q.queryKey[1] === 'host' }); });
  f.mounted.rerender(f.app()); await waitFor(() => expect(f.call).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole('listbox')).toBeNull();
});

it('cancels and releases old scopes on A→B→A and client replacement without resurrecting menus', async () => {
  const f = fixture(); f.call.mockImplementation(() => new Promise(() => {})); f.focus(); f.type('/po');
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(1));
  const first = (f.call.mock.calls[0]![2] as { signal: AbortSignal }).signal;
  f.mounted.rerender(f.app({ current: { rootId: 'root', agentId: 'other' } }));
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(2)); expect(first.aborted).toBe(true);
  expect(screen.queryByRole('listbox')).toBeNull();
  f.mounted.rerender(f.app()); await waitFor(() => expect(f.call).toHaveBeenCalledTimes(3));
  expect(screen.queryByRole('listbox')).toBeNull(); expect(f.input.value).toBe('/po');
  const third = (f.call.mock.calls[2]![2] as { signal: AbortSignal }).signal;
  f.mounted.rerender(f.app({ host: { ...f.client } }));
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(4)); expect(third.aborted).toBe(true);
  f.mounted.unmount(); expect((f.call.mock.calls[3]![2] as { signal: AbortSignal }).signal.aborted).toBe(true);
});

it.each([[workspace, false], [workspace, true], [global, false], [global, true]] as const)('uses same-scope debounced prefix fallback (%j, incomplete=%s)', async (scope, incomplete) => {
  const f = fixture(scope, incomplete);
  if (incomplete) f.call.mockResolvedValueOnce({ candidates: [{ text: '$alpha', description: '' }], truncated: true, warnings: ['Metadata was bounded.'] });
  f.call.mockResolvedValue({ candidates: [{ text: '$outside', description: '' }], truncated: true });
  f.focus(); f.type('/outside');
  await screen.findByRole('option', { name: /outside/ });
  expect(f.call).toHaveBeenLastCalledWith(scope === global ? 'host.skills.complete' : 'workspace.complete',
    expect.objectContaining({ prefix: 'outside', limit: 32, ...(scope === global ? { scope: 'global' } : {}) }), { signal: expect.any(AbortSignal) });
  if (scope === global) expect(f.call.mock.calls.every(([, params]) => !('cwd' in (params as object)))).toBe(true);
  expect(f.call).toHaveBeenCalledTimes(incomplete ? 2 : 1);
  expect(screen.queryByRole('status')).toBeNull();
  expect(screen.queryByText(/Selection inserts a skill reference|Keep typing to narrow the list/)).toBeNull();
  if (incomplete) expect(screen.getByText('Metadata was bounded.')).toBeTruthy();
  f.type('/other'); await waitFor(() => expect(f.call).toHaveBeenCalledTimes(incomplete ? 3 : 2));
});

it('refreshes on a stale focus boundary but not stale Escape, and does not reopen on refresh completion', async () => {
  const f = fixture(); f.focus(); await loaded(f);
  vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 11_000);
  fireEvent.keyDown(f.input, { key: 'Escape' }); expect(f.call).toHaveBeenCalledTimes(1);
  act(() => f.input.blur()); expect(f.call).toHaveBeenCalledTimes(1);
  let finish!: (value: CompletionResult) => void;
  f.call.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  f.focus(); await waitFor(() => expect(f.call).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole('listbox')).toBeNull();
  await act(async () => finish({ candidates: [{ text: '$new', description: '' }], truncated: false }));
  expect(screen.queryByRole('listbox')).toBeNull();
  f.type('/new'); await screen.findByRole('option', { name: '/new' });
  expect(f.call).toHaveBeenCalledTimes(2); vi.restoreAllMocks();
});

it.each([workspace, global])('shows a cold catalog failure without prefix compatibility retry (%j)', async scope => {
  const f = fixture(scope); f.call.mockRejectedValueOnce(new Error('Catalog read denied'));
  f.focus(); f.type('/po'); await screen.findByText('Could not load skills.');
  expect(f.call).toHaveBeenCalledTimes(1);
  expect(f.call).toHaveBeenLastCalledWith(scope === global ? 'host.skills.complete' : 'workspace.complete', expect.objectContaining({ prefix: '', limit: 1024 }), { signal: expect.any(AbortSignal) });
  expect(screen.queryByText('No matching skills.')).toBeNull();
  fireEvent.keyDown(f.input, { key: 'Enter' }); expect(f.send).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' })); await screen.findByRole('option', { name: /ponytail/ });
});
it('isolates global, project, definition, permission and remote-client catalogs without reopening or changing the draft', async () => {
  const f = fixture(global);
  const pending: ((value: CompletionResult) => void)[] = [];
  f.call.mockImplementation(() => new Promise(resolve => pending.push(resolve)));
  f.focus(); f.type('/po');
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(1));
  const project = { cwd: '/remote/project', definition: 'coding', permissionMode: 'prompt' } as const;
  const contexts = [project, { ...project, cwd: '/remote/other' }, global,
    { ...global, definition: 'research' }, { ...global, permissionMode: 'automatic' }] as const;
  for (const [index, current] of contexts.entries()) {
    const oldSignal = (f.call.mock.calls[index]![2] as { signal: AbortSignal }).signal;
    f.mounted.rerender(f.app({ current }));
    await waitFor(() => expect(f.call).toHaveBeenCalledTimes(index + 2));
    expect(oldSignal.aborted).toBe(true);
    await act(async () => pending[index]!({ candidates: [{ text: '$obsolete', description: '' }], truncated: false }));
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(f.input.value).toBe('/po');
    expect(f.input.selectionStart).toBe(3);
  }
  const remoteCall = vi.fn(async () => ({ candidates: [{ text: '$remote', description: '' }], truncated: false }));
  const remote = { ...f.client, call: remoteCall, getSnapshot: () => ({ info: {
    ...f.client.getSnapshot().info, runtime_id: 'other-host',
  } }) } as unknown as WhipClient;
  const oldSignal = (f.call.mock.calls.at(-1)![2] as { signal: AbortSignal }).signal;
  f.mounted.rerender(f.app({ current: global, host: remote }));
  await waitFor(() => expect(remoteCall).toHaveBeenCalledWith('host.skills.complete', {
    scope: 'global', definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024,
  }, { signal: expect.any(AbortSignal) }));
  expect(oldSignal.aborted).toBe(true);
  await act(async () => pending.at(-1)!({ candidates: [{ text: '$obsolete', description: '' }], truncated: false }));
  expect(screen.queryByRole('listbox')).toBeNull();
  f.type('/remote'); await screen.findByRole('option', { name: '/remote' });
  expect(screen.queryByRole('option', { name: '/obsolete' })).toBeNull();
  expect(f.send).not.toHaveBeenCalled();
});

it('only physical focus preloads globals; hiding and disconnect cancel reads and never restore old menus', async () => {
  const f = fixture(global);
  f.call.mockImplementation(() => new Promise(() => {}));
  f.mounted.rerender(f.app({ blocked: true }));
  f.focus(); f.type('/po');
  expect(f.call).not.toHaveBeenCalled(); expect(screen.queryByRole('listbox')).toBeNull();
  act(() => f.input.blur());
  f.mounted.rerender(f.app());
  expect(f.call).not.toHaveBeenCalled();
  f.focus(); await waitFor(() => expect(f.call).toHaveBeenCalledTimes(1));
  f.type('/pon');
  const first = (f.call.mock.calls[0]![2] as { signal: AbortSignal }).signal;
  f.mounted.rerender(f.app({ blocked: true }));
  expect(first.aborted).toBe(true); expect(screen.queryByRole('listbox')).toBeNull();
  f.mounted.rerender(f.app());
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(2));
  const second = (f.call.mock.calls[1]![2] as { signal: AbortSignal }).signal;
  f.mounted.rerender(f.app({ connected: false }));
  expect(second.aborted).toBe(true); expect(screen.queryByRole('listbox')).toBeNull();
  f.type('/pony'); await screen.findByText('Reconnect to search skills.');
  f.mounted.rerender(f.app());
  await waitFor(() => expect(f.call).toHaveBeenCalledTimes(3));
  expect(screen.queryByRole('listbox')).toBeNull(); expect(f.input.value).toBe('/pony');
});

it('shows a truthful empty global catalog without a folder prerequisite', async () => {
  const f = fixture(global); f.call.mockResolvedValue({ candidates: [], truncated: false });
  f.focus(); f.type('/'); await screen.findByText('No global skills available.');
  f.type('/missing'); expect(screen.getByText('No matching skills.')).toBeTruthy();
  fireEvent.keyDown(f.input, { key: 'Enter' }); expect(f.send).not.toHaveBeenCalled();
});
