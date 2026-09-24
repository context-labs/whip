import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { HistoryView } from '@whip/sdk/state';
import {
  SubmittedInputs,
  queuedInputRows,
  admittedText,
  isAcceptedInputNotice,
  type InboxInput,
} from '../src/input-presentation';
import { conversationRows, MessageRow } from '../src/timeline';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

it('excludes schedules in every admission state without hiding other fallback inputs', () => {
  for (const status of ['queued', 'running', 'consumed']) expect(isAcceptedInputNotice(inbox('1', status, 'schedule'))).toBe(false);
  for (const kind of ['submit', 'steer', 'submit.parts', 'steer.parts']) expect(isAcceptedInputNotice(inbox('1', 'queued', kind))).toBe(false);
  for (const kind of ['mailbox', 'goal', 'unknown']) expect(isAcceptedInputNotice(inbox('1', 'queued', kind))).toBe(true);
});

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


it('partitions queued inputs from chat and keeps identical messages and files distinct', () => {
  const store = new SubmittedInputs();
  const a = store.add(scope, 'Again', true);
  const b = store.add(scope, 'Again', true);
  store.accept(a, '10'); store.accept(b, '11');
  const inputs = ['10', '11'].map(seq => ({ ...inbox(seq, 'queued'), origin: 'client', preview: { text: 'Again', attachment_count: 2, attachments: [] } }));
  const queue = queuedInputRows(inputs.map(item => ({ item, stale: false })), store.getSnapshot());
  expect(queue.map(row => row.id)).toEqual([`input:${a}`, `input:${b}`]);
  expect(queue[0]?.preview?.attachment_count).toBe(2);
  expect(conversationRows(undefined, presentation, inputs, store.getSnapshot(), new Map(), true, true)).toHaveLength(1);
  expect(conversationRows(undefined, presentation, inputs, store.getSnapshot())).toHaveLength(3);
});

it('places a boundary-consumed queued message between the surrounding response fragments', () => {
  const input = { ...inbox('7'), origin: 'client', delivery_seq: '25' };
  const rows = conversationRows(undefined, [...presentation, { seq: '30', kind: 'stream.text', payload: { text: 'Changed direction' } }], [input], [], new Map(), true, true);
  expect(rows.map(row => row.text)).toEqual(['Working', 'Again', 'Changed direction']);
  expect(queuedInputRows([{ item: input, stale: false }], [])).toEqual([]);
});

it('retains unverified queue entries with a truthful state and excludes internal inputs', () => {
  const human = { ...inbox('7', 'queued'), origin: 'client' };
  const internal = inbox('8', 'queued');
  const rows = queuedInputRows([{ item: human, stale: true }, { item: internal, stale: false }], []);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({ stale: true, status: 'Checking queue…' });
});

it('correlates an inbox event before its receipt and keeps its key after local preview eviction or reload', () => {
  const store = new SubmittedInputs();
  const command = store.add({ ...scope, clientId: 'window' }, 'Same text', true);
  const sending = queuedInputRows([], store.getSnapshot());
  const accepted = { ...inbox('9', 'queued'), origin: 'client', command_client_id: 'window', command_id: command };
  const received = queuedInputRows([{ item: accepted, stale: false }], store.getSnapshot());
  expect(received).toHaveLength(1);
  expect(received[0]?.id).toBe(sending[0]?.id);
  expect(queuedInputRows([{ item: accepted, stale: false }], [])[0]?.id).toBe(sending[0]?.id);
  const foreign = { ...accepted, seq: '10', command_client_id: 'other-window' };
  expect(queuedInputRows([{ item: foreign, stale: false }], store.getSnapshot())).toHaveLength(2);
});
