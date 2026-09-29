import { assertValid, type ContractTypes } from '@whip/protocol';
import type { ReactNode } from 'react';
import { render } from '@testing-library/react';
import { afterEach, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import {
  type Cell,
  type HostOperation,
  type Message,
  type SessionActivity,
  type Turn,
} from '@whip/sdk';
import {
  createExecutionView,
  createSessionView,
  type CellExecutionRow,
  type HistoryView,
  type SessionViewSnapshot,
} from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { providerFixture, sessionRecord } from './provider-fixture';
import type { HostConnection } from '../src/hosts';

export function parameters<T extends keyof ContractTypes>(
  type: T,
  value: unknown,
): ContractTypes[T] {
  assertValid(type, value);
  return value;
}

export const at = '2026-09-28T12:00:00Z';
export const messageBase = {
  session_id: 'root',
  group_id: 'turn',
  opening_input: false,
  turn_id: 'turn',
  input_id: null,
  input_identity: null,
  mail: null,
  source: null,
  retired_by: null,
  retired_revision: null,
  created_at: at,
};
export const turn = (
  id = 'turn',
  state: Turn['state'] = 'running',
  session_id = 'root',
): Turn => ({
  id,
  session_id,
  history_revision: '1',
  config_revision: '1',
  goal: null,
  kind: 'prompt',
  state,
  failure: null,
  started_at: at,
  finished_at: state === 'running' ? null : '2026-09-28T12:00:02Z',
});
export const operation = (
  id = 'operation',
  state: HostOperation['state'] = 'dispatched',
  capability = 'files.read',
): HostOperation => ({
  id,
  session_id: 'root',
  turn_id: 'turn',
  cell_id: 'cell',
  origin: 'cell',
  permission_revision: null,
  request_id: id,
  capability,
  resource: '/project/README.md',
  arguments: { path: 'README.md' },
  state,
  grant_id: 'grant',
  result: null,
  created_at: at,
  dispatched_at: at,
  finished_at: state === 'succeeded' ? '2026-09-28T12:00:02Z' : null,
});
export const activity = (
  id = 'root',
  active: Turn | null = null,
): SessionActivity => ({
  session_id: id,
  lifecycle: 'active',
  active_turn: active,
  active_input_id: null,
  active_workspace_action_id: null,
  queued_input_count: '0',
  pending_permission_count: '0',
  pending_question_count: '0',
  execution_permit: !!active,
});
export function cellRow(
  id = 'cell',
  sequence?: string,
  state: Cell['state'] = 'succeeded',
  turnID = 'turn',
): CellExecutionRow {
  if (arguments.length < 2) sequence = '9007199254740993';
  const call = {
    id: 'call-' + id,
    name: 'execute',
    arguments: { code: `print('${id}')` },
  };
  const message: Message = {
    ...messageBase,
    id: 'message-' + id,
    turn_id: turnID,
    sequence: sequence ?? '1',
    role: 'assistant',
    parts: [{ type: 'tool_call', call }],
  };
  return {
    displayID: JSON.stringify(['root', message.id, call.id]),
    output: null,
    cell: {
      id,
      session_id: 'root',
      turn_id: turnID,
      call_message_id: message.id,
      call_id: call.id,
      state,
      result_message_id: null,
      checkpoint: null,
      created_at: at,
      finished_at: state === 'running' ? null : '2026-09-28T12:00:02Z',
    },
    turn: turn(
      turnID,
      state === 'running'
        ? 'running'
        : state === 'uncertain'
          ? 'interrupted'
          : state,
    ),
    call: sequence === undefined ? null : { message, value: call },
    result: null,
    operations: [],
  };
}
export const history = (messages: Message[] = []): HistoryView => ({
  snapshot: {
    session_id: messages[0]?.session_id ?? 'root',
    revision: '9007199254740993',
    through_sequence: messages.at(-1)?.sequence ?? '0',
    message_count: String(messages.length),
  },
  messages,
  gaps: [],
  olderCursor: null,
  latestMissing: false,
});
export const observation = (
  fields: Partial<SessionActivity> = {},
): SessionViewSnapshot => ({
  status: 'live',
  runtimeID: 'host',
  sessionID: 'root',
  epoch: 'boot',
  activity: { ...activity('root', turn()), ...fields },
  history: history(),
  preview: null,
  previewUnavailable: false,
  retainedBytes: 0,
  truncated: false,
  unavailable: false,
});

export async function conversationFixture(owner = 'root') {
  const f = await providerFixture();
  const values = new Map<string, string>();
  const runtime = new AppRuntime({
    defaultEndpoint: 'http://127.0.0.1:8080',
    storage: {
      persistent: false,
      keys: () => [...values.keys()],
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        values.set(key, value);
      },
      removeItem: (key) => {
        values.delete(key);
      },
    },
    copy: vi.fn(async () => {}),
    openExternal: vi.fn(async () => {}),
    download: vi.fn(async () => {}),
  });
  const host: HostConnection = {
    id: 'host',
    name: 'Test host',
    endpoint: 'http://127.0.0.1:8080',
    local: false,
    runtimeId: f.client.runtimeID,
    connectOnLaunch: false,
    state: 'connected',
    client: f.client,
    profile: {
      id: 'host',
      label: 'Test host',
      target: { kind: 'url', endpoint: 'http://127.0.0.1:8080' },
    },
    device: true,
  };
  const state = { ...runtime.getSnapshot(), hosts: [host] };
  vi.spyOn(runtime, 'getSnapshot').mockImplementation(() => state);
  vi.spyOn(runtime.connections, 'isAttached').mockImplementation(
    (client) => host.client === client && host.state === 'connected',
  );
  vi.spyOn(runtime.connections, 'host').mockReturnValue(host);
  vi.spyOn(runtime.connections, 'signal').mockReturnValue(
    new AbortController().signal,
  );
  const data = {
    messages: [
      {
        ...messageBase,
        session_id: owner,
        id: 'input-message',
        sequence: '9007199254740993',
        role: 'user',
        opening_input: true,
        input_id: 'input',
        parts: [{ type: 'text', text: 'Hello' }],
      } as Message,
    ],
    revision: '9007199254740993',
    active: null as Turn | null,
    children: [sessionRecord('a'), sessionRecord('b')],
  };
  for (const child of data.children) child.definition.id = child.id;
  f.data.handlers['sessions.get'] = (request) =>
    sessionRecord(parameters('SessionParams', request.params).session_id);
  f.data.handlers['trees.get'] = () => ({
    id: 'tree',
    revision: '1',
    metadata: { title: 'Native session', archived: false, pinned: false },
    engine: 'starlark',
    created_at: at,
  });
  f.data.handlers['sessions.list'] = () => ({
    items: [sessionRecord('root'), ...data.children],
  });
  f.data.handlers['sessions.activity'] = (request) =>
    activity(
      parameters('SessionParams', request.params).session_id,
      parameters('SessionParams', request.params).session_id === 'root'
        ? data.active
        : turn(
            'child-turn',
            'running',
            parameters('SessionParams', request.params).session_id,
          ),
    );
  f.data.handlers['sessions.turns'] = (request) => ({
    items:
      parameters('TurnPageParams', request.params).session_id === 'root'
        ? data.active
          ? [data.active]
          : []
        : [
            turn(
              'child-turn',
              'running',
              parameters('TurnPageParams', request.params).session_id,
            ),
          ],
    next_cursor: null,
  });
  f.data.handlers['turns.cells'] = () => ({ items: [] });
  f.data.handlers['turns.cells_page'] = () => ({ items: [], next_cursor: null });
  f.data.handlers['turns.operations'] = () => ({ items: [] });
  const snapshot = () => ({
    session_id: owner,
    revision: data.revision,
    through_sequence: data.messages.at(-1)?.sequence ?? '0',
    message_count: String(data.messages.length),
  });
  f.data.handlers['sessions.history_page'] = () => ({
    snapshot: snapshot(),
    messages: data.messages,
    next_cursor: null,
  });
  f.data.handlers['sessions.observe'] = () => ({
    snapshot: snapshot(),
    messages: [],
    epoch: 'boot',
    preview: null,
  });
  f.data.handlers['inputs.page'] = () => ({ items: [], next_cursor: null });
  f.data.handlers['permissions.list'] = () => ({ items: [] });
  f.data.handlers['questions.list'] = () => ({ items: [] });
  f.data.handlers['permissions.policy'] = () => ({
    tree_id: 'tree',
    mode: 'prompt',
    deny_interactive: false,
    revision: '1',
    updated_at: at,
  });
  f.data.handlers['schedules.list'] = () => ({
    items: [],
    next_after: null,
    next_cursor: null,
  });
  const session = f.client.session(owner),
    view = createSessionView(session, { pollIntervalMs: 60_000 });
  await view.start();
  const execution = createExecutionView(session, view, {
    pollIntervalMs: 60_000,
  });
  await execution.start();
  const wrap = (children: ReactNode) => (
    <RuntimeContext.Provider value={runtime}>
      <QueryClientProvider client={runtime.queries}>
        <ThemeProvider>
          <UIProvider>{children}</UIProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </RuntimeContext.Provider>
  );
  afterEach(async () => {
    await execution.dispose();
    await view.dispose();
    runtime.dispose();
    f.queries.clear();
  });
  return {
    ...f,
    data: { ...f.data, ...data },
    observed: data,
    runtime,
    state,
    host,
    session,
    view,
    execution,
    wrap,
    mount: (children: ReactNode) => render(wrap(children)),
  };
}
