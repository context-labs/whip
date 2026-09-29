import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { history as nativeHistory, inbox, message, preview } from './native-presentation-fixtures';
import {
  SubmittedInputs,
  queuedInputRows,
  admittedText,
  isAcceptedInputNotice,
  submittedInputId,
} from '../src/input-presentation';
import { conversationRows, MessageRow } from '../src/timeline';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

it('excludes schedules in every admission state without hiding maintenance notices', () => {
  for (const state of ['queued', 'claimed', 'cancelled'] as const) expect(isAcceptedInputNotice(inbox('1', state, { source: 'schedule' }))).toBe(false);
  for (const source of ['user', 'agent'] as const) expect(isAcceptedInputNotice(inbox('1', 'queued', { source }))).toBe(false);
  for (const kind of ['compact', 'goal_formulation', 'automatic_title', 'host_operation'] as const) expect(isAcceptedInputNotice(inbox('1', 'queued', { kind }))).toBe(true);
});

const scope = { runtimeId: 'host', rootId: 'root', agentId: 'root', clientId: 'window' };
const history = nativeHistory(message({ id: 'authored', role: 'user', input_id: '9007199254740993', opening_input: true, parts: [{ type: 'text', text: 'Again' }] }), message({ id: 'answer', sequence: '2' }));
const presentation = preview();
const key = (id: string) => submittedInputId({ ...scope, id } as Parameters<typeof submittedInputId>[0]);

it('renders immediately, reconciles by inbox identity, and hands over to committed history', () => {
  const store = new SubmittedInputs();
  const id = store.add(scope, 'Again');
  expect(
    conversationRows(undefined, undefined, [], store.getSnapshot()),
  ).toMatchObject([
    { id: key(id), role: 'user', text: 'Again', delivery: 'Sending…' },
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
    id: key(id),
    text: 'Again',
    delivery: 'Accepted',
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
  const second = store.add(scope, 'Again', true);
  store.accept(first, '1');
  store.accept(second, '2');
  const rows = conversationRows(
    undefined,
    presentation,
    [inbox('1'), inbox('2', 'queued')],
    store.getSnapshot(),
  );
  expect(rows.map((row) => row.role)).toEqual(['user', 'assistant']);
  const queued = queuedInputRows([{ item: inbox('2', 'queued'), stale: false }], store.getSnapshot());
  expect(rows[0]?.id).not.toBe(queued[0]?.id);
  expect(queued[0]?.status).toBe('Queued');
  const steering = conversationRows(
    undefined,
    presentation,
    [inbox('2', 'claimed', { steering: { id: 'steer', turn_id: 'turn', consumed: true } })],
    [],
  );
  expect(steering.map((row) => row.role)).toEqual(['user', 'assistant']);
});

it('reconstructs accepted messages on reload without a local preview and excludes runtime wakeups', () => {
  const rows = conversationRows(
    undefined,
    presentation,
    [inbox('1'), inbox('2', 'queued', { source: 'goal', kind: 'goal_formulation' })],
    [],
  );
  expect(rows).toHaveLength(2);
  expect(rows[0]).toMatchObject({
    role: 'user',
    text: 'Again',
    sentAt: undefined,
  });
  expect(admittedText({ id: 'full', session_id: 'root', source: 'user', kind: 'prompt', state: 'claimed', turn_id: 'turn', goal: null, schedule: null, host_operation: null, created_at: '2026-09-28T00:00:00Z', parts: [{ type: 'text', text: 'Inspect this' }, { type: 'content', reference_id: 'file' }] })).toBe('Inspect this\n1 attached files');
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
  const a = store.add(scope, 'Again', true, undefined, { text: 'Again', attachment_count: 2 });
  const b = store.add(scope, 'Again', true);
  store.accept(a, '10'); store.accept(b, '11');
  const inputs = ['10', '11'].map(seq => inbox(seq, 'queued', { attachment_count: '2' }));
  const queue = queuedInputRows(inputs.map(item => ({ item, stale: false })), store.getSnapshot());
  expect(queue.map(row => row.id)).toEqual([key(a), key(b)]);
  expect(queue[0]?.preview?.attachment_count).toBe(2);
  expect(conversationRows(undefined, presentation, inputs, store.getSnapshot())).toHaveLength(1);
});

it('places a boundary-consumed queued message between the surrounding response fragments', () => {
  const input = inbox('7', 'claimed', { steering: { id: 'steer', turn_id: 'turn', consumed: true } });
  // Native steering is committed canonical history, not an event sequence hint.
  const retained = nativeHistory(message({ id: 'before', parts: [{ type: 'text', text: 'Working' }] }),
    message({ id: 'steered', role: 'user', input_id: '7', sequence: '2', parts: [{ type: 'text', text: 'Again' }] }));
  const rows = conversationRows(retained, preview({ text: 'Changed direction' }), [input], []);
  expect(rows.map(row => row.text)).toEqual(['Working', 'Again', 'Changed direction']);
  expect(queuedInputRows([{ item: input, stale: false }], [])).toEqual([]);
});

it('retains unverified queue entries with a truthful state and excludes internal inputs', () => {
  const human = inbox('7', 'queued');
  const internal = inbox('8', 'queued', { source: 'agent' });
  const rows = queuedInputRows([{ item: human, stale: true }, { item: internal, stale: false }], []);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({ stale: true, status: 'Checking queue…' });
});

it('correlates an inbox event before its receipt and keeps its key after local preview eviction or reload', () => {
  const store = new SubmittedInputs();
  const command = store.add({ ...scope, clientId: 'window' }, 'Same text', true);
  const sending = queuedInputRows([], store.getSnapshot());
  const accepted = inbox('9', 'queued', { identity: { client_id: 'window', request_id: command } });
  const received = queuedInputRows([{ item: accepted, stale: false }], store.getSnapshot());
  expect(received).toHaveLength(1);
  expect(received[0]?.id).toBe(sending[0]?.id);
  expect(queuedInputRows([{ item: accepted, stale: false }], [])[0]?.id).toBe(sending[0]?.id);
  const foreign = inbox('10', 'queued', { identity: { client_id: 'other-window', request_id: command } });
  expect(queuedInputRows([{ item: foreign, stale: false }], store.getSnapshot())).toHaveLength(2);
  const otherOwner = { ...accepted, session_id: 'child' };
  expect(queuedInputRows([{ item: otherOwner, stale: false }], store.getSnapshot())).toHaveLength(2);
  const committed = nativeHistory(message({ id: 'accepted-message', role: 'user', input_id: '9', input_identity: accepted.identity, parts: [{ type: 'text', text: 'Same text' }] }));
  expect(conversationRows(committed, undefined, [], store.getSnapshot())).toHaveLength(1);
  expect(conversationRows(committed, undefined, [], [])[0]?.id).toBe(sending[0]?.id);
  expect(conversationRows(committed, undefined, [], [])[0]?.memberIds).toContain('message:accepted-message');
});
