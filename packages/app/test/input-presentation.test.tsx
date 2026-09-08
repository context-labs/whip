import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { HistoryView } from '@whip/sdk/state';
import {
  SubmittedInputs,
  admittedText,
  type InboxInput,
} from '../src/input-presentation';
import { conversationRows, MessageRow } from '../src/timeline';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

const scope = { runtimeId: 'host', rootId: 'root', agentId: 'root' };
const inbox = (
  seq: string,
  status = 'running',
  kind = 'submit',
): InboxInput => ({
  root_id: 'root',
  agent_id: 'root',
  seq,
  kind,
  status,
  payload: {
    text: 'Again',
    reference_id: '',
    digest: '',
    size: '5',
    media_type: 'text/plain',
    source: '',
  },
});
const history: HistoryView = {
  revision: '1',
  throughSeq: 2,
  nextSeq: 1,
  hasMore: false,
  loading: false,
  truncated: false,
  messages: [
    {
      seq: 1,
      message: {
        role: 'user',
        content: 'Again',
        sent_at: '2026-09-07T20:48:00Z',
      },
    },
    { seq: 2, message: { role: 'assistant', content: 'Done' } },
  ],
};
const presentation = [
  { seq: '20', kind: 'stream.text', payload: { text: 'Working' } },
];

it('renders immediately, reconciles by inbox identity, and hands over to committed history', () => {
  const store = new SubmittedInputs();
  const id = store.add(scope, 'Again');
  expect(
    conversationRows(undefined, undefined, [], store.getSnapshot()),
  ).toMatchObject([
    { id: `input:${id}`, role: 'user', text: 'Again', delivery: 'Sending…' },
  ]);
  expect(
    conversationRows(undefined, presentation, [], store.getSnapshot()).map(
      (row) => row.role,
    ),
  ).toEqual(['user', 'assistant']);
  store.accept(id, '9007199254740993');
  const running = conversationRows(
    undefined,
    presentation,
    [inbox('9007199254740993')],
    store.getSnapshot(),
  );
  expect(running).toHaveLength(2);
  expect(running[0]).toMatchObject({
    id: `input:${id}`,
    text: 'Again',
    delivery: undefined,
  });
  expect(running[1]).toMatchObject({ role: 'assistant', text: 'Working' });
  store.confirm([id]);
  expect(
    conversationRows(history, undefined, [], store.getSnapshot()),
  ).toMatchObject([
    { role: 'user', text: 'Again', sentAt: '2026-09-07T20:48:00Z' },
    { role: 'assistant', text: 'Done' },
  ]);
});

it('keeps identical submissions distinct and queued/steering input after the current response', () => {
  const store = new SubmittedInputs();
  const first = store.add(scope, 'Again');
  const second = store.add(scope, 'Again');
  store.accept(first, '1');
  store.accept(second, '2');
  const rows = conversationRows(
    undefined,
    presentation,
    [inbox('1'), inbox('2', 'queued')],
    store.getSnapshot(),
  );
  expect(rows.map((row) => row.role)).toEqual(['user', 'assistant', 'user']);
  expect(rows[0]?.id).not.toBe(rows[2]?.id);
  expect(rows[2]?.delivery).toBe('Queued');
  const steering = conversationRows(
    undefined,
    presentation,
    [inbox('2', 'running', 'steer')],
    [],
  );
  expect(steering.map((row) => row.role)).toEqual(['assistant', 'user']);
});

it('reconstructs accepted messages on reload without a local preview and excludes runtime wakeups', () => {
  const rows = conversationRows(
    undefined,
    presentation,
    [inbox('1'), inbox('2', 'queued', 'mailbox')],
    [],
  );
  expect(rows).toHaveLength(2);
  expect(rows[0]).toMatchObject({
    role: 'user',
    text: 'Again',
    sentAt: undefined,
  });
  expect(
    admittedText('submit.parts', {
      ...inbox('1').payload,
      binary: btoa(
        JSON.stringify({ text: 'Inspect this', attachments: [{ id: 'file' }] }),
      ),
    }),
  ).toBe('Inspect this\n1 attached files');
});

it('keeps uncertain delivery visible, allows rejection cleanup, and clears host-local previews', () => {
  const store = new SubmittedInputs();
  const id = store.add(scope, 'Again');
  expect(
    conversationRows(
      undefined,
      undefined,
      [],
      store.getSnapshot(),
      new Map([[id, 'Checking delivery…']]),
    )[0]?.delivery,
  ).toBe('Checking delivery…');
  store.remove(id);
  expect(store.getSnapshot()).toHaveLength(0);
  store.add(scope, 'Private host message');
  store.clear();
  store.accept(id, '1');
  expect(store.getSnapshot()).toHaveLength(0);
});

it('bounds previews without silently dropping unacknowledged input and avoids no-op notifications', () => {
  const store = new SubmittedInputs();
  const listener = vi.fn();
  store.subscribe(listener);
  for (let index = 0; index < 32; index++) store.add(scope, 'Again');
  expect(() => store.add(scope, 'Overflow')).toThrow(
    'Submission previews are full',
  );
  const oldest = store.getSnapshot()[0]!.id;
  store.confirm([oldest]);
  store.add(scope, 'Replacement');
  expect(store.getSnapshot()).toHaveLength(32);
  expect(store.getSnapshot().some((item) => item.id === oldest)).toBe(false);
  listener.mockClear();
  store.confirm(['missing']);
  expect(listener).not.toHaveBeenCalled();
  const bytes = new SubmittedInputs();
  expect(() => bytes.add(scope, 'x'.repeat(1024 * 1024))).toThrow(
    'Submission previews are full',
  );
  expect(bytes.getSnapshot()).toHaveLength(0);
});

it('renders a user bubble with time and working copy/history controls', async () => {
  const copy = vi.fn(async () => {}),
    historyAction = vi.fn();
  const row = conversationRows(history, undefined, [], [])[0]!;
  const { container, rerender } = render(
    <RuntimeContext.Provider
      value={{ platform: { copy }, report: vi.fn() } as unknown as AppRuntime}
    >
      <UIProvider>
        <MessageRow
          row={row}
          readBody={() => {}}
          historyAction={historyAction}
        />
      </UIProvider>
    </RuntimeContext.Provider>,
  );
  expect(container.querySelector('[data-user-bubble]')?.textContent).toBe(
    'Again',
  );
  expect(container.querySelector('time')?.dateTime).toBe(
    '2026-09-07T20:48:00Z',
  );
  fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
  await screen.findByRole('button', { name: 'Copied' });
  expect(copy).toHaveBeenCalledWith('Again');
  fireEvent.click(
    screen.getByRole('button', { name: 'Message history actions' }),
  );
  fireEvent.click(
    await screen.findByRole('menuitem', { name: 'Fork before this message' }),
  );
  expect(historyAction).toHaveBeenCalledWith(row, 'fork');
  rerender(
    <RuntimeContext.Provider
      value={{ platform: { copy }, report: vi.fn() } as unknown as AppRuntime}
    >
      <UIProvider>
        <MessageRow row={{ ...row, sentAt: 'invalid' }} readBody={() => {}} />
      </UIProvider>
    </RuntimeContext.Provider>,
  );
  expect(container.querySelector('time')).toBeNull();
});
