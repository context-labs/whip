import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ActivityIndicator, ThemeProvider, UIProvider } from '@whip/ui';
import type { CellExecutionRow } from '@whip/sdk/state';
import {
  conversationActivityRows,
  activitySummary,
  fileOperationPath,
  operationSubject,
  isActivityGroup,
  responseCopies,
  type ActivityGroup,
} from '../src/chat-activity-rows';
import { activityStatus, TranscriptWorking } from '../src/chat-activity';
import { timelineRows, type TimelineRow } from '../src/conversation-rows';
import { readingTarget } from '../src/reading-positions';
import {
  ActivityHeader,
  ActivityDetail,
  ActivityStep,
  InlineAgent,
} from '../src/transcript-activity';
import { ExecutionTime, formatHostDuration } from '../src/execution-time';
import { MotionContext } from '../src/transcript-motion';
import {
  cellRow,
  history,
  observation,
  operation,
  turn,
} from './native-conversation-fixture';
import { sessionRecord } from './provider-fixture';

beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
const tool = (cell: CellExecutionRow): TimelineRow => ({
  id: `tool:${cell.cell.id}`,
  seq: cell.call?.message.sequence,
  turnId: cell.cell.turn_id,
  role: 'tool',
  callId: cell.cell.call_id,
  toolName: 'execute',
  text: '',
  label: 'Execution',
});
const group = (cells: CellExecutionRow[]): ActivityGroup => ({
  id: 'group',
  role: 'activity',
  text: '',
  cells,
  memberIds: cells.map((item) => item.cell.id),
  memberSeqs: [],
});
const wrap = (content: React.ReactNode) => (
  <ThemeProvider>
    <UIProvider>{content}</UIProvider>
  </ThemeProvider>
);

it('shows the canonical requested file separately from its permission scope', () => {
  const host = { ...operation(), resource: '/workspace', arguments: { path: '<img src=x>.txt' } };
  const item = { id: host.id, kind: 'operation' as const, host };
  const value = { ...group([]), items: [item] };
  const { container } = render(wrap(<>
    <ActivityHeader group={value} open={false} toggle={vi.fn()} connected density="comfortable" />
    <ActivityStep item={item} groupId={value.id} open={false} toggle={vi.fn()} connected last />
    <ActivityDetail item={item} groupId={value.id} readBody={vi.fn()} connected />
  </>));
  expect(screen.getByTitle('<img src=x>.txt')).toBeTruthy();
  expect(container.querySelector('[data-tool-preview]')?.textContent).toBe('<img src=x>.txt');
  expect(screen.getByText('Permission scope: /workspace')).toBeTruthy();
  expect(container.querySelector('img')).toBeNull();
});
it('does not turn missing or malformed file paths or unrelated tool arguments into file subjects', () => {
  const base = { ...operation(), resource: '/workspace' };
  for (const path of [null, 42, {}, [], '', '\0', 'x'.repeat(4097), 'é'.repeat(2049)])
    expect(fileOperationPath({ ...base, arguments: { path } })).toBeUndefined();
  for (const args of [null, [], {}, 'README.md'])
    expect(fileOperationPath({ ...base, arguments: args })).toBeUndefined();
  expect(fileOperationPath({ ...base, capability: 'tools.read', arguments: { path: 'README.md' } })).toBeUndefined();
  render(wrap(<ActivityStep item={{ id: base.id, kind: 'operation', host: { ...base, arguments: {} } }}
    groupId="group" open={false} toggle={vi.fn()} connected last />));
  expect(screen.getByTitle('/workspace')).toBeTruthy();
});
it.each([
  ['shell.run', { command: 'git status --short', secret: 'never show this' }, 'git status --short'],
  ['files.search', { path: 'src', pattern: 'createRoot' }, 'src · createRoot'],
  ['browser.navigate', { url: 'https://example.com/docs' }, 'https://example.com/docs'],
  ['browser.search', { query: 'Whip documentation' }, 'Whip documentation'],
] as const)('uses the captured %s subject instead of a generic permission scope', (capability, arguments_, subject) => {
  const host = { ...operation('op', 'succeeded', capability), resource: '/workspace', arguments: arguments_ };
  const item = { id: host.id, kind: 'operation' as const, host };
  render(wrap(<ActivityStep item={item} groupId="group" open={false} toggle={vi.fn()} connected last />));
  expect(screen.getByTitle(subject)).toBeTruthy();
  expect(screen.queryByText('never show this')).toBeNull();
  expect(screen.getByText('Done')).toBeTruthy();
});
it.each([
  ['denied', 'Denied'], ['uncertain', 'Outcome uncertain'], ['waiting', 'Needs approval'],
] as const)('keeps a %s operation outcome distinct from successful parent work', (state, label) => {
  const host = operation('op', state);
  render(wrap(<ActivityStep item={{ id: host.id, kind: 'operation', host }} groupId="group" open={false} toggle={vi.fn()} connected last />));
  expect(screen.getByText(label)).toBeTruthy();
  expect(screen.queryByText('Done')).toBeNull();
});

it('bounds identifying excerpts by UTF-8 bytes and excludes unrelated argument fields', () => {
  const host = { ...operation('op', 'succeeded', 'shell.run'), arguments: { command: '🙂'.repeat(300), prompt: 'private prompt' } };
  const subject = operationSubject(host);
  expect(subject).toBe('🙂'.repeat(128) + '…');
  expect(subject).not.toContain('�');
  expect(operationSubject({ ...host, capability: 'models.call' })).toBe(host.resource);
});

it('counts distinct edited paths within the same workspace and keeps unknown edits honest', () => {
  const hosts = [
    { ...operation('a', 'succeeded', 'files.write'), resource: '/workspace', arguments: { path: 'a.txt' } },
    { ...operation('a-again', 'succeeded', 'files.patch'), resource: '/workspace', arguments: { path: 'a.txt' } },
    { ...operation('b', 'succeeded', 'files.write'), resource: '/workspace', arguments: { path: 'b.txt' } },
    { ...operation('foreign-a', 'succeeded', 'files.write'), resource: '/other', arguments: { path: 'a.txt' } },
    { ...operation('unknown', 'succeeded', 'files.patch'), resource: '/workspace', arguments: {} },
  ];
  expect(activitySummary({ ...group([]), items: hosts.map(host => ({ id: host.id, kind: 'operation', host })) }))
    .toBe('Edited 3 files · made 1 edit');
});

it('formats recorded Go durations without changing the measurement', () => {
  for (const [raw, display] of [
    ['2.93134ms', '3ms'],
    ['125µs', '0.13ms'],
    ['400ns', '0.00ms'],
    ['999.9ms', '1s'],
    ['1m59.9s', '2m0s'],
    ['unavailable', 'unavailable'],
  ])
    expect(formatHostDuration(raw!)).toBe(display);
});
it('distinguishes an unavailable session from a disconnected host', () => {
  expect(
    activityStatus(
      { ...observation(), activity: null, error: { message: 'Read failed' } },
      [],
      true,
    ).text,
  ).toBe('Activity unavailable');
  expect(
    activityStatus({ ...observation(), activity: null }, [], true).text,
  ).toBe('Loading session…');
  expect(activityStatus(observation(), [], false).text).toBe('Updates paused');
});
it('joins only exact canonical call sequence, turn and call identity above 2^53', () => {
  const old = cellRow('old', '9007199254740992', 'succeeded', 'old-turn'),
    current = cellRow('current', '9007199254740993');
  const rows = conversationActivityRows(
    [tool(old), tool(current)],
    [old, current],
  ).filter(isActivityGroup);
  expect(rows.map((row) => row.cells.map((item) => item.cell.id))).toEqual([
    ['old'],
    ['current'],
  ]);
  expect(rows[1]?.memberSeqs).toEqual(['9007199254740993']);
  expect(
    conversationActivityRows(
      [{ ...tool(current), turnId: 'foreign' }],
      [current],
    )[0]?.role,
  ).toBe('tool');
  expect(
    conversationActivityRows(
      [{ ...tool(current), callId: 'foreign' }],
      [current],
    )[0]?.role,
  ).toBe('tool');
  expect(
    conversationActivityRows(
      [{ ...tool(current), toolName: 'custom' }],
      [current],
    )[0]?.role,
  ).toBe('tool');
});
it('keeps provisional execute arguments as a preview rather than a fabricated committed cell', () => {
  const preview = {
    attempt_id: 'attempt',
    turn_id: 'turn',
    message_id: 'preview',
    revision: '1',
    text: '',
    reasoning: 'Thinking',
    calls: [
      { index: 0, id: 'call-cell', name: 'execute', arguments: '{"code":' },
    ],
    truncated: false,
  };
  const rows = timelineRows(undefined, preview),
    cells = [cellRow()];
  const projected = conversationActivityRows(rows, cells);
  expect(projected.filter(isActivityGroup).flatMap((row) => row.cells)).toEqual(
    [],
  );
  expect(projected.at(-1)).toMatchObject({ live: true, args: '{"code":' });
  expect(projected.at(-1)?.seq).toBeUndefined();
});
it('preserves prose and notices while grouping contiguous canonical work', () => {
  const a = cellRow('a', '1'),
    b = cellRow('b', '2'),
    c = cellRow('c', '4');
  const prose = {
    id: 'prose',
    role: 'assistant',
    text: 'Keep **every** byte.\n',
  };
  const output = conversationActivityRows(
    [tool(a), tool(b), prose, tool(c)],
    [a, b, c],
  );
  expect(output.filter(isActivityGroup).map((row) => row.cells.length)).toEqual(
    [2, 1],
  );
  expect(output[1]).toBe(prose);
});
it('splits at gaps and never attaches an old unplaced cell below a later response', () => {
  const a = cellRow('a', '1'),
    b = cellRow('b', '3'),
    old = cellRow('missing', undefined, 'running', 'old');
  const gap: TimelineRow = {
    id: 'gap',
    role: 'history-gap',
    text: '',
    seq: '2',
    historyGap: {
      messageID: 'missing',
      sequence: '2',
      reason: 'message_too_large',
      bytes: 99999,
    },
  };
  const output = conversationActivityRows(
    [tool(a), gap, tool(b)],
    [a, b, old],
    [],
    'turn',
  );
  expect(output.filter(isActivityGroup).map((row) => row.cells.length)).toEqual(
    [1, 1],
  );
  expect(output[1]).toBe(gap);
  expect(
    output
      .filter(isActivityGroup)
      .flatMap((row) => row.cells)
      .map((row) => row.cell.id),
  ).not.toContain('missing');
});
it('retains group and bookmark identity as active missing-prefix work becomes recorded', () => {
  const pending = cellRow('current', undefined, 'running');
  const before = conversationActivityRows([], [pending], [], 'turn').filter(
    isActivityGroup,
  );
  const recorded = cellRow('current', '9007199254740993');
  const after = conversationActivityRows(
    [tool(recorded)],
    [recorded],
    before,
  ).filter(isActivityGroup);
  expect(after[0]?.id).toBe(before[0]?.id);
  expect(
    readingTarget(after, '1', {
      messageId: before[0]!.id,
      revision: '1',
      offset: 7,
      follow: false,
    }),
  ).toEqual({ index: 0, offset: 7, fallback: false });
  expect(conversationActivityRows([], [pending], before, 'other-turn')).toEqual(
    [],
  );
});
it('merged groups retain aliases for both previous groups', () => {
  const a = cellRow('a', '1'),
    b = cellRow('b', '2');
  const before = conversationActivityRows(
    [tool(a), { id: 'boundary', role: 'notice', text: '' }, tool(b)],
    [a, b],
  ).filter(isActivityGroup);
  const after = conversationActivityRows([tool(a), tool(b)], [a, b], before);
  expect(
    readingTarget(after, '1', {
      messageId: before[1]!.id,
      revision: '1',
      offset: 12,
      follow: false,
    }),
  ).toEqual({ index: 0, offset: 12, fallback: false });
});
it('bounds mailbox folding and leaves exact message text available', () => {
  const updates = Array.from({ length: 14 }, (_, index) => ({
    id: `mail-${index}`,
    role: 'mailbox',
    text: `Exact mail ${index}`,
  }));
  const groups = conversationActivityRows(updates, []).filter(isActivityGroup);
  expect(groups.map((row) => row.updates?.length)).toEqual([6, 6, 2]);
  render(
    wrap(
      <ActivityHeader
        group={groups[0]!}
        open
        connected
        density="detailed"
        toggle={vi.fn()}
      />,
    ),
  );
  expect(screen.getByText('Agent updates')).toBeTruthy();
});
it('copies each completed visible response once without tool, mail, queued or provisional prose', () => {
  const rows: TimelineRow[] = [
    { id: 'u', role: 'user', text: 'Request' },
    { id: 'a', role: 'assistant', text: 'Start' },
    { id: 'tool', role: 'tool', text: 'secret tool output' },
    { id: 'mail', role: 'mailbox', text: 'internal' },
    { id: 'b', role: 'assistant', text: 'End' },
    { id: 'next', role: 'user', text: 'Next' },
    { id: 'live', role: 'assistant', text: 'Working', live: true },
  ];
  expect([...responseCopies(rows, true)]).toEqual([
    ['b', { text: 'Start\n\nEnd', label: 'Copy response' }],
  ]);
  expect(responseCopies(rows.slice(1, 5), false, true).get('b')?.label).toBe(
    'Copy visible response',
  );
});
it('human requests take priority over operation and provider status, with honest waiting and queue states', () => {
  const current = {
    ...cellRow('cell', undefined, 'running'),
    operations: [operation()],
  };
  expect(activityStatus(observation(), [current], true).text).toBe(
    'Reading files',
  );
  expect(
    activityStatus(
      observation({ pending_permission_count: '9007199254740993' }),
      [current],
      true,
    ),
  ).toMatchObject({ text: 'Waiting for your approval', active: false });
  expect(
    activityStatus(observation({ pending_question_count: '1' }), [], true).text,
  ).toBe('Waiting for your answer');
  expect(
    activityStatus(observation({ execution_permit: false }), [], true),
  ).toMatchObject({ text: 'Waiting for work to continue', active: false });
  expect(
    activityStatus(
      observation({ active_turn: null, queued_input_count: '1' }),
      [],
      true,
      turn('last', 'failed'),
    ).text,
  ).toBe('Queued · waiting to start');
  expect(
    activityStatus(
      observation({ active_turn: null }),
      [],
      true,
      undefined,
      'Checking delivery…',
    ),
  ).toMatchObject({ active: false, pending: true });
});
it('shows a direct human operation without manufacturing an execution cell or prior transcript', () => {
  const direct = {
    ...operation(),
    origin: 'host_operation' as const,
    cell_id: null,
  };
  const output = conversationActivityRows([], [], [], 'turn', [direct]).filter(
    isActivityGroup,
  );
  expect(output).toHaveLength(1);
  expect(output[0]?.cells).toEqual([]);
  expect(output[0]?.items?.[0]?.host).toBe(direct);
  expect(output[0]?.live).toBe(true);
  expect(conversationActivityRows([], [], [], undefined, [direct])).toEqual([]);
});
it('launch records use canonical result identity and do not reinterpret later child lifecycle', () => {
  const child = sessionRecord('child');
  child.definition.id = 'Reviewer';
  const host = {
    ...operation('spawn', 'succeeded', 'agents.spawn'),
    result: {
      state: 'succeeded' as const,
      content_references: [],
      value: { session_id: 'child', input_id: 'input' },
      failure: null,
    },
  };
  const row = {
      ...tool(cellRow()),
      role: 'agent-activity' as const,
      agentHost: host,
      cell: cellRow(),
    },
    open = vi.fn();
  render(
    wrap(
      <InlineAgent
        row={row}
        agent={child}
        connected={false}
        onAgent={open}
        readBody={vi.fn()}
      />,
    ),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Launched Reviewer' }));
  expect(open).toHaveBeenCalledWith('child');
});
it.each([
  ['denied', 'Launch denied'],
  ['failed', 'Failed to launch'],
  ['cancelled', 'Launch cancelled'],
  ['uncertain', 'Launch outcome uncertain'],
] as const)(
  'keeps %s launch evidence without inventing a child',
  (state, label) => {
    const host = {
      ...operation('spawn', state, 'agents.spawn'),
      result: {
        state,
        content_references: [],
        failure: 'Launch did not complete',
      },
    };
    render(
      wrap(
        <InlineAgent
          row={{
            ...tool(cellRow()),
            role: 'agent-activity',
            agentHost: host,
            cell: cellRow(),
          }}
          connected
          onAgent={vi.fn()}
          readBody={vi.fn()}
        />,
      ),
    );
    expect(
      screen.getByRole('button', { name: `${label} Agent` }),
    ).toHaveProperty('disabled', true);
    expect(screen.getByText('Launch did not complete')).toBeTruthy();
  },
);
it('execution detail exposes exact retained code and operation evidence without eager content reads', () => {
  const cell = cellRow(),
    readBody = vi.fn(),
    open = vi.fn();
  render(
    wrap(
      <ActivityDetail
        item={{ id: 'cell', kind: 'execution', cell }}
        groupId="group"
        connected
        readBody={readBody}
        onOpenRepl={open}
      />,
    ),
  );
  expect(screen.getByText("print('cell')")).toBeTruthy();
  expect(readBody).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Open in REPL' }));
  expect(open).toHaveBeenCalledOnce();
});
it('completed time stays fixed; live host-clock estimates pause while hidden and reject future starts', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-28T12:00:10Z'));
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const view = render(
    <ExecutionTime
      cell={cellRow('live', undefined, 'running').cell}
      connected
    />,
  );
  expect(screen.getByText('10s elapsed')).toBeTruthy();
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(5000));
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(screen.getByText('15s elapsed')).toBeTruthy();
  view.rerender(<ExecutionTime cell={cellRow().cell} connected />);
  expect(screen.getByText('2.0s')).toBeTruthy();
  expect(vi.getTimerCount()).toBe(0);
  view.rerender(
    <ExecutionTime
      cell={{
        ...cellRow('future', undefined, 'running').cell,
        created_at: '2026-09-29T00:00:00Z',
      }}
      connected
    />,
  );
  expect(view.container.textContent).toBe('');
});
it('motion stops when hidden and reduced, and turn elapsed labels distinguish observed starts', () => {
  vi.useFakeTimers();
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const view = render(<ActivityIndicator active />);
  act(() => vi.advanceTimersByTime(1100));
  expect(
    view.container
      .querySelector('[data-activity-animation]')
      ?.getAttribute('data-activity-animation'),
  ).toBe('running');
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(0);
  view.unmount();
  hidden.mockReturnValue(false);
  const working = render(
    <MotionContext.Provider value={false}>
      <TranscriptWorking
        status={{ text: 'Working', active: true }}
        turnId="turn"
      />
    </MotionContext.Provider>,
  );
  expect(screen.getByText('Thinking…')).toBeTruthy();
  expect(
    screen.getByTitle('Time since this client observed the turn'),
  ).toBeTruthy();
  working.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it('names retained agent messages in a mixed execution summary', () => {
  const cell = cellRow();
  const rows: TimelineRow[] = [
    { id: 'mail', role: 'mailbox', text: 'Exact child findings', turnId: cell.cell.turn_id },
    tool(cell),
  ];
  const groups = conversationActivityRows(rows, [cell]).filter(isActivityGroup);
  expect(groups).toHaveLength(1);
  expect(groups[0]!.updates?.map(row => row.text)).toEqual(['Exact child findings']);
  expect(activitySummary(groups[0]!)).toBe('1 execution · agent updates');
});
