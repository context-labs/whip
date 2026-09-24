import { act, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionContent } from '../src/conversation';

// A session that is still opening must hold its footprint with neutral copy;
// a failed open keeps every "unavailable" message. Only the pieces that read
// host data through queries are stubbed.
vi.mock('@tanstack/react-router', async importOriginal => ({
  ...await importOriginal<typeof import('@tanstack/react-router')>(),
  useNavigate: () => vi.fn(async () => {}),
}));
vi.mock('@whip/ui', async importOriginal => ({
  ...await importOriginal<typeof import('@whip/ui')>(),
  Sheet: ({ children }: { children: ReactNode }) => <section>{children}</section>,
}));
vi.mock('../src/timeline', async importOriginal => ({
  ...await importOriginal<typeof import('../src/timeline')>(),
  Timeline: () => <div data-testid="timeline" />,
}));
vi.mock('../src/requests', () => ({ PendingRequests: () => null }));
vi.mock('../src/inspector', () => ({ SessionInspector: () => null }));
vi.mock('../src/agent-turn-notice', () => ({ AgentTurnNotice: () => null, useSelectedAgent: () => undefined }));
vi.mock('../src/session-actions', () => ({ useSessionActions: () => ({ items: () => [], prepare: () => {} }) }));
vi.mock('../src/model-selection', async importOriginal => ({
  ...await importOriginal<typeof import('../src/model-selection')>(),
  SessionModelPicker: () => <button type="button">gpt-6-astra</button>,
}));
vi.mock('../src/permission-mode', () => ({ PermissionModePicker: () => <button type="button">Ask for approval</button> }));

const runtimes: AppRuntime[] = [];
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
});
afterEach(() => { vi.unstubAllGlobals(); for (const runtime of runtimes.splice(0)) runtime.dispose(); });

const liveRoot = { root_id: 'root', meta: { cwd: '/work/whip' }, history_revision: '1', active_turns: {}, presentation: [], inbox: [], agents: [] };
const liveHistory = { root: { revision: '1', throughSeq: 0, nextSeq: 0, hasMore: false, loading: false, truncated: false, messages: [] } };

function fixture(initial: Partial<SessionViewSnapshot>, summaryCwd?: string) {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  runtimes.push(runtime);
  vi.spyOn(runtime.connections, 'isAttached').mockReturnValue(true);
  const appState = { ...runtime.getSnapshot(), hosts: [{ id: 'local', runtimeId: 'host', name: 'Local', state: 'connected' }] as never };
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue(appState);
  const connection = { state: 'connected', info: { runtime_id: 'host' } };
  let current = { status: 'loading', history: {}, collections: {}, retainedBytes: 0, truncated: false, unavailable: false, ...initial } as unknown as SessionViewSnapshot;
  const listeners = new Set<() => void>();
  const view = {
    subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    getSnapshot: () => current,
    refresh: vi.fn(async () => {}), loadOlder: vi.fn(async () => {}),
    session: { rootId: 'root', client: { subscribe: () => () => {}, getSnapshot: () => connection, supports: () => false }, history: {}, fork: vi.fn() },
  } as unknown as SessionView;
  render(<RuntimeContext.Provider value={runtime}><QueryClientProvider client={runtime.queries}><ThemeProvider><UIProvider>
    <SessionContent kind="chat" view={view} expectedRuntimeId="host" agentId="root" summaryCwd={summaryCwd} />
  </UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider>);
  return { update: (next: Partial<SessionViewSnapshot>) => act(() => { current = { ...current, ...next } as SessionViewSnapshot; listeners.forEach(listener => listener()); }) };
}

it('holds a neutral footprint while a session opens, then fills it in', () => {
  const { update } = fixture({ status: 'loading' }, '/work/whip');
  expect(screen.getByLabelText('Local / /work/whip')).toBeTruthy();
  expect(screen.queryByText(/unavailable/i)).toBeNull();
  expect(screen.getByRole('status', { name: 'Opening session' })).toBeTruthy();
  expect(screen.getByText('Loading session…')).toBeTruthy();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(3);
  expect(screen.queryByText('Ask for approval')).toBeNull();

  update({ status: 'live', root: liveRoot as never, history: liveHistory as never });
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
  expect(screen.getByText('Ask for approval')).toBeTruthy();
  expect(screen.getByText('gpt-6-astra')).toBeTruthy();
  expect(screen.getByText('Idle')).toBeTruthy();
  expect(screen.queryByText(/unavailable/i)).toBeNull();
});

it('keeps the unavailable copy when the open fails', () => {
  fixture({ status: 'error', error: new Error('history read failed') });
  expect(screen.getByText('Session content is unavailable. Your draft stays here; it will not be sent automatically.')).toBeTruthy();
  expect(screen.getByLabelText('Local / Directory unavailable')).toBeTruthy();
  expect(screen.getByText('Session unavailable')).toBeTruthy();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
});

it('shows the host alone while opening without a summary directory', () => {
  fixture({ status: 'loading' });
  expect(screen.getByLabelText('Local')).toBeTruthy();
  expect(screen.queryByText(/Directory unavailable/)).toBeNull();
});
