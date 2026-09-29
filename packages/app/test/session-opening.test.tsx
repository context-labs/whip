import { act, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { createSessionView, createExecutionView } from '@whip/sdk/state';
import { SessionContent } from '../src/conversation';
import { conversationFixture } from './native-conversation-fixture';
import { sessionRecord } from './provider-fixture';

vi.mock('@tanstack/react-router', async importOriginal => ({
  ...await importOriginal<typeof import('@tanstack/react-router')>(), useNavigate: () => vi.fn(async () => {}),
}));
vi.mock('@whip/ui', async importOriginal => ({
  ...await importOriginal<typeof import('@whip/ui')>(), Sheet: ({ children }: { children: ReactNode }) => <section>{children}</section>,
}));
vi.mock('../src/timeline', async importOriginal => ({ ...await importOriginal<typeof import('../src/timeline')>(), Timeline: () => <div data-testid="timeline" /> }));
vi.mock('../src/requests', () => ({ PendingRequests: () => null }));
vi.mock('../src/inspector', () => ({ SessionInspector: () => null }));
vi.mock('../src/session-actions', () => ({ useSessionActions: () => ({ items: () => [], prepare: () => {} }) }));
vi.mock('../src/model-selection', async importOriginal => ({ ...await importOriginal<typeof import('../src/model-selection')>(), SessionModelPicker: () => <button type="button">gpt-6-astra</button> }));
vi.mock('../src/permission-mode', () => ({ PermissionModePicker: () => <button type="button">Ask for approval</button> }));
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
});
afterEach(() => vi.unstubAllGlobals());

async function fixture(summaryCwd?: string) {
  const f = await conversationFixture();
  f.host.name = 'Local';
  f.observed.messages = [];
  let resolve!: () => void, reject!: (error: Error) => void;
  const metadata = new Promise<ReturnType<typeof sessionRecord>>((success, failure) => { resolve = () => success({ ...sessionRecord('root'), working_directory: '/work/whip' }); reject = failure; });
  f.data.handlers['sessions.get'] = () => metadata;
  const view = createSessionView(f.session, { pollIntervalMs: 60000 });
  const execution = createExecutionView(f.session, view, { pollIntervalMs: 60000 });
  afterEach(async () => { await execution.dispose(); await view.dispose(); });
  f.mount(<SessionContent kind="chat" client={f.client} session={f.session} rootId="root" view={view} execution={execution} expectedRuntimeId="host" agentId="root" summaryCwd={summaryCwd} />);
  return {
    async open() { await act(async () => { resolve(); await view.start(); await execution.start(); }); },
    async fail() { await act(async () => { reject(new Error('history read failed')); }); },
  };
}

it('holds a neutral footprint while a session opens, then fills it in', async () => {
  const f = await fixture('/work/whip');
  expect(screen.getByLabelText('Local / /work/whip')).toBeTruthy();
  expect(screen.queryByText(/unavailable/i)).toBeNull();
  expect(screen.getByRole('status', { name: 'Opening session' })).toBeTruthy();
  expect(screen.getByText('Loading session…')).toBeTruthy();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(3);
  expect(screen.queryByText('Ask for approval')).toBeNull();
  await f.open();
  await screen.findByText('Ask for approval');
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
  expect(screen.getByText('gpt-6-astra')).toBeTruthy();
  expect(screen.getByText('Idle')).toBeTruthy();
  expect(screen.queryByText(/unavailable/i)).toBeNull();
});

it('keeps unavailable copy when the native metadata read fails', async () => {
  const f = await fixture(); await f.fail();
  await screen.findByText('Session content is unavailable. Use Refresh above.');
  expect(screen.getByLabelText('Local / Directory unavailable')).toBeTruthy();
  expect(screen.getByText('Session unavailable')).toBeTruthy();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
});

it('shows the host alone while opening without a summary directory', async () => {
  await fixture();
  expect(screen.getByLabelText('Local')).toBeTruthy();
  expect(screen.queryByText(/Directory unavailable/)).toBeNull();
});
