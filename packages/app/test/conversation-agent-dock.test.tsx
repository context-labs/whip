import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { SessionView } from '@whip/sdk/state';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionContent } from '../src/conversation';
import { sessionPanes, sessionViewPane } from '../src/session-tabs';

const navigate = vi.hoisted(() => vi.fn(async () => {}));
vi.mock('@tanstack/react-router', async importOriginal => ({
  ...await importOriginal<typeof import('@tanstack/react-router')>(), useNavigate: () => navigate,
}));
vi.mock('../src/timeline', async importOriginal => ({
  ...await importOriginal<typeof import('../src/timeline')>(),
  Timeline: ({ onAgent }: ComponentProps<typeof import('../src/timeline').Timeline>) =>
    <button onClick={() => onAgent?.('a')}>Open inline child</button>,
}));
// Test composition/recipient ownership with a stateful uncontrolled draft; real
// composer/draft/upload preservation is additionally exercised in the browser.
vi.mock('../src/composer', () => ({ Composer: ({ agentId }: { agentId: string }) => <textarea aria-label={`Draft for ${agentId}`} defaultValue="" /> }));
vi.mock('../src/requests', () => ({ PendingRequests: () => null }));
vi.mock('../src/inspector', () => ({ SessionInspector: () => null }));
vi.mock('../src/agent-turn-notice', () => ({ AgentTurnNotice: () => null, useSelectedAgent: () => undefined }));
vi.mock('../src/session-actions', () => ({ useSessionActions: () => ({ items: () => [], prepare: () => {} }) }));

const runtimes: AppRuntime[] = [];
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.restoreAllMocks(); navigate.mockClear(); });

function fixture(width = 1000) {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  runtimes.push(runtime);
  runtime.tabs.visit('host', 'root', {});
  const source = runtime.tabs.workspace().tabs[0]!;
  const pane = sessionViewPane(runtime.tabs.workspace(), source.id)!;
  vi.spyOn(runtime.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(runtime.connections, 'host').mockReturnValue({} as NonNullable<ReturnType<typeof runtime.connections.host>>);
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(width);
  const connection = { state: 'connected', info: { runtime_id: 'host' } };
  const snapshot = {
    status: 'live', collections: {},
    root: { root_id: 'root', meta: {}, history_revision: '1', active_turns: {}, presentation: [], inbox: [],
      agents: ['a', 'b'].map(id => ({ id, name: id, parent_id: 'root', status: 'running' })) },
    history: { root: { revision: '1', throughSeq: 1, nextSeq: 1, hasMore: false, loading: false, truncated: false,
      messages: [{ seq: 1, message: { role: 'user', content: 'Hello' } }] } },
  };
  const view = {
    subscribe: () => () => {}, getSnapshot: () => snapshot,
    session: { rootId: 'root', client: { subscribe: () => () => {}, getSnapshot: () => connection, supports: () => false } },
  } as unknown as SessionView;
  const rendered = render(<RuntimeContext.Provider value={runtime}><ThemeProvider><UIProvider>
    <div data-workspace-frame={pane.id}><div data-workspace-view={source.id} tabIndex={-1}>
      <SessionContent kind="chat" view={view} expectedRuntimeId="host" agentId="root" viewId={source.id} />
    </div></div>
  </UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { runtime, source, ...rendered };
}

it('dock and inline links share a reusable child split without retargeting or remounting the source composer', async () => {
  const { runtime, source, container } = fixture();
  const draft = screen.getByRole('textbox', { name: 'Draft for root' });
  fireEvent.change(draft, { target: { value: 'Keep the main draft' } });
  fireEvent.click(container.querySelector('[data-agent-dock-row="a"]')!);
  const child = runtime.tabs.workspace().tabs.find(tab => tab.id !== source.id)!;
  expect(child).toMatchObject({ kind: 'chat', location: { agent: 'a' } });
  expect(sessionPanes(runtime.tabs.workspace().layout)).toHaveLength(2);
  expect(runtime.tabs.workspace().tabs.find(tab => tab.id === source.id)).toEqual(source);
  expect(screen.getByRole('textbox', { name: 'Draft for root' })).toBe(draft);
  expect((draft as HTMLTextAreaElement).value).toBe('Keep the main draft');
  fireEvent.click(container.querySelector('[data-agent-dock-row="b"]')!);
  expect(runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(runtime.tabs.workspace().tabs.find(tab => tab.id === child.id)).toMatchObject({ location: { agent: 'b' } });
  fireEvent.click(screen.getByRole('button', { name: 'Open inline child' }));
  expect(runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(runtime.tabs.workspace().tabs.find(tab => tab.id === child.id)).toMatchObject({ location: { agent: 'a' } });
  expect(container.querySelector('[data-agent-dock-row="a"]')?.getAttribute('aria-current')).toBe('true');
  await waitFor(() => expect(navigate).toHaveBeenLastCalledWith(expect.objectContaining({
    search: expect.objectContaining({ agent: 'a' }), state: { whipViewId: child.id },
  })));
  expect((draft as HTMLTextAreaElement).value).toBe('Keep the main draft');
});

it('insufficient split space leaves the main view intact until Open in tab is explicitly chosen', () => {
  const { runtime, source, container } = fixture(600);
  const before = runtime.tabs.workspace();
  fireEvent.click(container.querySelector('[data-agent-dock-row="a"]')!);
  expect(runtime.tabs.workspace()).toBe(before);
  expect(navigate).not.toHaveBeenCalled();
  expect(screen.getByText(/Not enough room/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Open in tab' }));
  expect(sessionPanes(runtime.tabs.workspace().layout)).toHaveLength(1);
  expect(runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(runtime.tabs.workspace().tabs.find(tab => tab.id === source.id)).toEqual(source);
  expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ search: expect.objectContaining({ agent: 'a' }) }));
});
