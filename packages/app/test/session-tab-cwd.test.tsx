import type { ReactNode } from 'react';
import { act, render, screen } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import type { SessionSummariesResult } from '@whip/protocol';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionTabStrip } from '../src/session-tab-strip';

const connection = vi.hoisted(() => ({ client: undefined as WhipClient | undefined }));
vi.mock('@whip/sdk', async importOriginal => ({ ...await importOriginal<typeof import('@whip/sdk')>(), createWhipClient: () => connection.client }));
vi.mock('@whip/sdk/state', async importOriginal => ({ ...await importOriginal<typeof import('@whip/sdk/state')>(), createSessionListView: () => ({ start: async () => {}, dispose: async () => {}, getSnapshot: () => ({}) }) }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useLocation: () => ({ pathname: '/h/mac/s/active' }), useNavigate: () => vi.fn(),
}));
vi.mock('../src/conversation', () => ({ SessionContent: () => null, SessionLoading: () => null }));
vi.mock('../src/workspace-views', () => ({
  useWorkspaceViews: () => ({ views: new Map(), errors: new Map() }),
  workspaceRootKey: ({ runtimeId, rootId }: { runtimeId: string; rootId: string }) => JSON.stringify([runtimeId, rootId]),
}));
vi.mock('@whip/ui/workspace-tabs', () => ({
  WorkspaceTabs: ({ items, value }: { items: { value: string; label: ReactNode; metadata?: ReactNode }[]; value: string }) => <div role="tablist">{items.map(item => <button key={item.value} role="tab" aria-selected={item.value === value}>{item.label} {item.metadata}</button>)}</div>,
  workspaceTabId: (id: string) => id,
}));
vi.mock('@whip/ui/workspace-layout', () => ({
  WorkspaceLayout: ({ renderHeader, layout }: { renderHeader(id: string): ReactNode; layout: { id: string } }) => <>{renderHeader(layout.id)}</>,
  workspacePanelId: (id: string) => id,
}));
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

it('shows each tab project directory beside its title', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultEndpoint: 'http://localhost:8080',
    storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } },
    copy: async () => {}, openExternal: async () => {}, download: async () => 'saved', sessionLink: value => value,
  });
  const summary = (root_id: string, title: string, cwd: string): SessionSummariesResult['items'][number] => ({
    root_id, title, cwd, missing: false, archived: false, running_agents: '0', queued_agents: '0',
    pending_permissions: '0', pending_questions: '0', truncated: false,
  });
  const items = [summary('active', 'Active conversation', '/work/whip'), summary('inactive', 'Fix the parser', '/work/renderer')];
  const connected = { state: 'connected', info: { runtime_id: 'mac', connection_id: 'connection', negotiated_capabilities: ['session_summaries'] } };
  connection.client = { getSnapshot: () => connected, sessions: { summaries: async () => ({ items }), list: async () => ({ items: [] }), get: async () => ({ root_id: 'inactive', title: items[1]!.title, cwd: items[1]!.cwd, archived: false, history_revision: '1' }) }, events: { subscribe: vi.fn() }, session: vi.fn(),
    configuration: { get: async () => ({ revision: '1', remote_hosts: [] }) },
    providers: { list: async () => ({}), catalogs: async () => ({}) },
    onNotification: () => () => {},
    subscribe: () => () => {}, connect: async () => {}, onEvent: () => () => {}, close: vi.fn(),
  } as unknown as WhipClient;
  await runtime.connect();
  runtime.tabs.open('mac', 'active');
  runtime.tabs.open('mac', 'inactive');
  runtime.tabs.visit('mac', 'active', {});
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={runtime.queries}>
    <SessionTabStrip compact={false} onManageHosts={() => {}} utilities={null} />
  </QueryClientProvider></UIProvider></RuntimeContext.Provider>);
  try {
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    const active = screen.getByRole('tab', { name: /Active conversation/ });
    const inactive = screen.getByRole('tab', { name: /Fix the parser/ });
    expect(active.textContent ?? '').toContain('whip');
    expect(inactive.textContent ?? '').toContain('renderer');
    expect(active.textContent ?? '').not.toContain('/work');
  } finally {
    view.unmount();
    runtime.dispose();
  }
});
