import type { ReactNode } from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { TreeSummariesResult } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionTabStrip } from '../src/session-tab-strip';
import { SessionActionsProvider, useSessionActions } from '../src/session-actions';
import { providerFixture, sessionRecord } from './provider-fixture';

const roots = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useLocation: () => ({ pathname: '/h/mac/s/active', state: {} }), useNavigate: () => vi.fn() }));
vi.mock('../src/conversation', () => ({ SessionContent: () => null, SessionLoading: () => null }));
vi.mock('../src/workspace-views', () => ({
  useWorkspaceViews: (_runtime: unknown, requested: unknown) => { roots(requested); return { views: new Map(), executions: new Map(), errors: new Map() }; },
  workspaceRootKey: ({ runtimeId, rootId }: { runtimeId: string; rootId: string }) => JSON.stringify([runtimeId, rootId]),
}));
vi.mock('@whip/ui/workspace-tabs', () => ({
  WorkspaceTabs: ({ items, value }: { items: { value: string; label: ReactNode; accessibleLabel: string }[]; value: string }) => <div role="tablist">{items.map(item => <button key={item.value} role="tab" aria-selected={item.value === value} title={item.accessibleLabel}>{item.label}</button>)}</div>,
  workspaceTabId: (id: string) => id,
}));
vi.mock('@whip/ui/workspace-layout', () => ({
  WorkspaceLayout: ({ renderHeader, layout }: { renderHeader(id: string): ReactNode; layout: { id: string } }) => <>{renderHeader(layout.id)}</>, workspacePanelId: (id: string) => id,
}));
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });
function RenameInactive() {
  const actions = useSessionActions();
  return <button onClick={() => actions.items({ runtimeId: 'mac', rootId: 'inactive', title: 'Help me repair the build' }).find(item => item.id === 'rename')?.onSelect?.()}>Rename inactive</button>;
}
const summary = (root_id: string, title: string): TreeSummariesResult['items'][number] => ({
  root_id, tree: { id: `tree-${root_id}`, metadata: { title, archived: false, pinned: false }, engine: 'quickjs', revision: '1', created_at: '2026-09-28T00:00:00Z' },
  working_directory: '/work', activity: { active_turn_count: '0', queued_input_count: '0', pending_permission_count: '0', pending_question_count: '0', active_workspace_action_count: '0' },
});

it.each([false, true])('updates inactive native tab titles and preserves authored rename drafts (catalog invalidation: %s)', async invalidation => {
  vi.useFakeTimers();
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  const f = await providerFixture({ runtimeID: 'mac' });
  let items = [summary('active', 'Active conversation'), summary('inactive', 'Help me repair the build')];
  f.data.handlers['trees.summaries'] = () => ({ items, missing_root_ids: [] });
  f.data.handlers['sessions.get'] = request => ({ ...sessionRecord(request.params.session_id as string), parent_id: null, tree_id: `tree-${request.params.session_id}` });
  f.data.handlers['trees.get'] = request => items.find(item => item.tree.id === request.params.tree_id)!.tree;
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultEndpoint: 'http://localhost:8080', storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } }, copy: async () => {}, openExternal: async () => {}, download: async () => 'saved', sessionLink: value => value });
  const host = { id: 'mac-profile', name: 'Mac', runtimeId: 'mac', state: 'connected', client: f.client, profile: { target: { kind: 'url' } }, list: { getSnapshot: () => ({ items: [] }), refresh: async () => {} } };
  const snapshot = { ...runtime.getSnapshot(), hosts: [host] };
  Object.assign(runtime, { getSnapshot: () => snapshot });
  vi.spyOn(runtime.connections, 'host').mockReturnValue(host as never);
  runtime.tabs.open('mac', 'inactive'); runtime.tabs.open('mac', 'active'); runtime.tabs.visit('mac', 'active', {});
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={runtime.queries}>
    <SessionActionsProvider><RenameInactive /><SessionTabStrip compact={false} onManageHosts={() => {}} utilities={null} /></SessionActionsProvider>
  </QueryClientProvider></UIProvider></RuntimeContext.Provider>);
  try {
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    expect(screen.getByRole('tab', { name: 'Help me repair the build' }).getAttribute('aria-selected')).toBe('false');
    expect(f.calls.find(call => call.method === 'trees.summaries')?.params).toEqual({ root_ids: ['active', 'inactive'] });
    expect(f.count('sessions.observe')).toBe(0); expect(f.count('sessions.get')).toBe(0);
    if (invalidation) {
      await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Rename inactive' })); await vi.advanceTimersByTimeAsync(1); });
      fireEvent.change(screen.getByRole('textbox'), { target: { value: 'My unsent rename' } });
    }
    const before = f.count('trees.summaries');
    items = [items[0]!, summary('inactive', 'Repair build')];
    if (invalidation) await act(async () => { await runtime.queries.invalidateQueries({ queryKey: ['session-tab-summaries', 'mac'] }); await vi.advanceTimersByTimeAsync(1); });
    else await act(async () => { await vi.advanceTimersByTimeAsync(2001); });
    expect(f.count('trees.summaries')).toBeGreaterThan(before);
    if (invalidation) {
      expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('My unsent rename');
      await act(async () => { fireEvent.click(screen.getByLabelText('Close', { exact: true })); });
    }
    expect(screen.getByRole('tab', { name: 'Repair build' }).getAttribute('aria-selected')).toBe('false');
    expect(screen.getByRole('tab', { name: 'Active conversation' }).getAttribute('aria-selected')).toBe('true');
    expect(runtime.tabs.workspace().tabs.find(tab => tab.kind === 'chat' && tab.rootId === 'inactive')?.titleHint).toBe('Repair build');
    items = [items[0]!, summary('inactive', 'Recovered by polling')];
    await act(async () => { await vi.advanceTimersByTimeAsync(2001); });
    expect(screen.getByRole('tab', { name: 'Recovered by polling' })).toBeTruthy();
    expect(roots.mock.calls.every(([requested]) => requested.length === 1 && requested[0].rootId === 'active')).toBe(true);
    expect(f.count('sessions.observe')).toBe(0); expect(f.count('sessions.history_page')).toBe(0); expect(f.count('sessions.submit')).toBe(0);
  } finally { view.unmount(); runtime.dispose(); f.queries.clear(); }
});
