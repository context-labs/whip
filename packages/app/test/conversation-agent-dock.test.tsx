import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { useImperativeHandle, type ComponentProps } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { conversationFixture } from './native-conversation-fixture';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionContent } from '../src/conversation';
import * as agentDock from '../src/agent-dock';
import {
  sessionPanes,
  sessionViewPane,
  type SessionViewKind,
} from '../src/session-tabs';

beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
afterEach(() => vi.unstubAllGlobals());
const navigate = vi.hoisted(() => vi.fn(async () => {}));
const jumpToLatest = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}));
vi.mock('../src/timeline', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../src/timeline')>()),
  Timeline: ({
    onAgent,
    readingActionsRef,
    bookmarkKey,
  }: ComponentProps<typeof import('../src/timeline').Timeline>) => {
    useImperativeHandle(readingActionsRef, () => ({
      jumpToLatest: () => jumpToLatest(bookmarkKey),
    }));
    return <button onClick={() => onAgent?.('a')}>Open inline child</button>;
  },
}));
// Test composition/recipient ownership with a stateful uncontrolled draft; real
// composer/draft/upload preservation is additionally exercised in the browser.
vi.mock('../src/composer', () => ({
  Composer: ({
    session,
    onAccepted,
    viewId,
    notice,
    agents,
    queue,
  }: ComponentProps<typeof import('../src/composer').Composer>) => (
    <>
      {notice}
      {agents}
      {queue}
      <textarea aria-label={`Draft for ${session.id}`} defaultValue="" />
      <button onClick={onAccepted}>Accept send in {viewId}</button>
    </>
  ),
}));
vi.mock('../src/repl-view', () => ({ ReplView: () => null }));
vi.mock('../src/trace-view', () => ({ TraceView: () => null }));
vi.mock('../src/requests', () => ({ PendingRequests: () => null }));
vi.mock('../src/inspector', () => ({ SessionInspector: () => null }));
vi.mock('../src/agent-turn-notice', async (original) => ({
  ...(await original<typeof import('../src/agent-turn-notice')>()),
  AgentTurnNotice: () => null,
}));
vi.mock('../src/session-actions', () => ({
  useSessionActions: () => ({ items: () => [], prepare: () => {} }),
}));

afterEach(() => {
  vi.restoreAllMocks();
  navigate.mockClear();
  jumpToLatest.mockClear();
});

async function fixture(width = 1000) {
  const f = await conversationFixture(),
    { runtime, client, session, view, execution } = f;
  runtime.tabs.visit('host', 'root', {});
  const source = runtime.tabs.workspace().tabs[0]!;
  const pane = sessionViewPane(runtime.tabs.workspace(), source.id)!;
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(width);
  const app = (kind: SessionViewKind = 'chat', id = source.id) =>
    f.wrap(
      <div data-workspace-frame={pane.id}>
        <div data-workspace-view={id} tabIndex={-1}>
          <SessionContent
            client={client}
            session={session}
            rootId="root"
            kind={kind}
            view={view}
            execution={execution}
            expectedRuntimeId="host"
            agentId="root"
            viewId={id}
          />
        </div>
      </div>,
    );
  const rendered = render(app());
  await waitFor(() =>
    expect(rendered.container.querySelector('[data-agent-dock]')).toBeTruthy(),
  );
  return {
    ...f,
    source: runtime.tabs.workspace().tabs.find((tab) => tab.id === source.id)!,
    app,
    ...rendered,
  };
}

it('an accepted send scrolls only its own chat view, not another view of the same session', async () => {
  const first = await fixture();
  const second = render(first.app('chat', 'other-view'));
  fireEvent.click(
    [...first.container.querySelectorAll('button')].find(
      (button) => button.textContent === `Accept send in ${first.source.id}`,
    )!,
  );
  expect(jumpToLatest.mock.calls).toEqual([[`host:${first.source.id}:root`]]);
  jumpToLatest.mockClear();
  fireEvent.click(
    [...second.container.querySelectorAll('button')].find(
      (button) => button.textContent === 'Accept send in other-view',
    )!,
  );
  expect(jumpToLatest.mock.calls).toEqual([['host:other-view:root']]);
});

it('dock and inline links share a reusable child split without retargeting or remounting the source composer', async () => {
  const { runtime, source, container } = await fixture();
  const draft = screen.getByRole('textbox', { name: 'Draft for root' });
  fireEvent.change(draft, { target: { value: 'Keep the main draft' } });
  fireEvent.click(
    container.querySelector('[data-agent-dock] button[aria-expanded]')!,
  );
  fireEvent.click(container.querySelector('[data-agent-dock-row="a"]')!);
  const child = runtime.tabs
    .workspace()
    .tabs.find((tab) => tab.id !== source.id)!;
  expect(child).toMatchObject({ kind: 'chat', location: { agent: 'a' } });
  expect(sessionPanes(runtime.tabs.workspace().layout)).toHaveLength(2);
  expect(
    runtime.tabs.workspace().tabs.find((tab) => tab.id === source.id),
  ).toEqual(source);
  expect(screen.getByRole('textbox', { name: 'Draft for root' })).toBe(draft);
  expect((draft as HTMLTextAreaElement).value).toBe('Keep the main draft');
  fireEvent.click(container.querySelector('[data-agent-dock-row="b"]')!);
  expect(runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(
    runtime.tabs.workspace().tabs.find((tab) => tab.id === child.id),
  ).toMatchObject({ location: { agent: 'b' } });
  fireEvent.click(screen.getByRole('button', { name: 'Open inline child' }));
  expect(runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(
    runtime.tabs.workspace().tabs.find((tab) => tab.id === child.id),
  ).toMatchObject({ location: { agent: 'a' } });
  expect(
    container
      .querySelector('[data-agent-dock-row="a"]')
      ?.getAttribute('aria-current'),
  ).toBe('true');
  await waitFor(() =>
    expect(navigate).toHaveBeenLastCalledWith(
      expect.objectContaining({
        search: expect.objectContaining({ agent: 'a' }),
        state: { whipViewId: child.id },
      }),
    ),
  );
  expect((draft as HTMLTextAreaElement).value).toBe('Keep the main draft');
});

it.each(['repl', 'trace'] as const)(
  'retains the companion and open-child highlight across an in-place %s round trip',
  async (kind) => {
    const { runtime, source, container, app, rerender } = await fixture();
    fireEvent.click(
      container.querySelector('[data-agent-dock] button[aria-expanded]')!,
    );
    fireEvent.click(container.querySelector('[data-agent-dock-row="a"]')!);
    const child = runtime.tabs
      .workspace()
      .tabs.find((tab) => tab.id !== source.id)!;
    act(() => runtime.tabs.visit('host', 'root', { view: kind }, source.id));
    rerender(app(kind));
    act(() => runtime.tabs.visit('host', 'root', {}, source.id));
    rerender(app());
    await waitFor(() =>
      expect(container.querySelector('[data-agent-dock]')).toBeTruthy(),
    );
    fireEvent.click(
      container.querySelector('[data-agent-dock] button[aria-expanded]')!,
    );
    expect(
      container
        .querySelector('[data-agent-dock-row="a"]')
        ?.getAttribute('aria-current'),
    ).toBe('true');
    fireEvent.click(container.querySelector('[data-agent-dock-row="b"]')!);
    expect(runtime.tabs.workspace().tabs).toHaveLength(2);
    expect(sessionPanes(runtime.tabs.workspace().layout)).toHaveLength(2);
    expect(
      runtime.tabs.workspace().tabs.find((tab) => tab.id === child.id),
    ).toMatchObject({ location: { agent: 'b' } });
  },
);

it('places upcoming wakes before agents in the composer and never in REPL or trace', async () => {
  const f = await fixture();
  f.data.handlers['schedules.list'] = () => ({
    items: [
      {
        id: 'schedule',
        session_id: 'root',
        expression: '2026-09-21T01:30:00Z',
        first_due: '2026-09-21T01:30:00Z',
        next_due: '2026-09-21T01:30:00Z',
        cancelled_at: null,
        failure: null,
        created_at: '2026-09-20T00:00:00Z',
        parts_bytes: '10',
        preview: 'Wake prompt',
        preview_truncated: false,
        latest: null,
      },
    ],
    next_after: null,
    next_cursor: null,
  });
  await act(async () => {
    await f.runtime.queries.invalidateQueries({
      queryKey: ['upcoming-schedules'],
    });
  });
  await waitFor(() =>
    expect(f.container.querySelector('[data-scheduled-wake]')).toBeTruthy(),
  );
  const notice = f.container.querySelector('[data-scheduled-wake]')!;
  const dock = f.container.querySelector('[data-agent-dock]')!;
  expect(
    notice.compareDocumentPosition(dock) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  for (const kind of ['repl', 'trace'] as const) {
    f.rerender(f.app(kind));
    expect(f.container.querySelector('[data-scheduled-wake]')).toBeNull();
  }
});

it('insufficient split space leaves the main view intact until Open in tab is explicitly chosen', async () => {
  const { runtime, source, container } = await fixture(600);
  const before = runtime.tabs.workspace();
  fireEvent.click(
    container.querySelector('[data-agent-dock] button[aria-expanded]')!,
  );
  fireEvent.click(container.querySelector('[data-agent-dock-row="a"]')!);
  expect(runtime.tabs.workspace()).toBe(before);
  expect(navigate).not.toHaveBeenCalled();
  expect(screen.getByText(/Not enough room/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Open in tab' }));
  expect(sessionPanes(runtime.tabs.workspace().layout)).toHaveLength(1);
  expect(runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(
    runtime.tabs.workspace().tabs.find((tab) => tab.id === source.id),
  ).toEqual(source);
  expect(navigate).toHaveBeenCalledWith(
    expect.objectContaining({
      search: expect.objectContaining({ agent: 'a' }),
    }),
  );
});
