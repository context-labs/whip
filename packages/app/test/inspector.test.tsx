import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { webcrypto } from 'node:crypto';
import {
  Client,
  DeliveryError,
  DurableCommand,
  RecoveryJournal,
  type Operations,
  type SessionRecord,
  type Turn,
  type DurableMethod,
  type RecoveryRecord,
} from '@whip/sdk';
import type { Request } from '@whip/protocol';
import { createExecutionView, createSessionView } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import fixtures from '../../protocol/schema/fixtures.json';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { Agents, Mailbox } from '../src/details/observation';
import { MCP } from '../src/details/integrations';
import {
  Compaction,
  Goals,
  Permissions,
  Limits,
  formatBudgetAmount,
} from '../src/details/session-controls';
import { readStateBytes } from '../src/details/state-read';
import type { InspectorProps } from '../src/details/shared';

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#agent">{children}</a>,
}));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.stubGlobal('crypto', webcrypto);
});
afterEach(() => vi.unstubAllGlobals());
const sample = <T,>(type: string) =>
  structuredClone(fixtures.find((value) => value.type === type && value.valid)!.value) as T;
function params<M extends keyof Operations>(request: Request, method: M): Operations[M]['params'] {
  expect(request.method).toBe(method);
  return request.params as Operations[M]['params'];
}
const at = '2026-09-28T12:00:00Z',
  hash = 'a'.repeat(64);
async function fixture() {
  const calls: Request[] = [],
    handlers: Record<string, (request: Request) => unknown | Promise<unknown>> = {};
  const data = {
    session: {
      ...sample<SessionRecord>('Session'),
      id: 'session_child',
      tree_id: 'tree',
      parent_id: 'session_root',
      config_revision: '9007199254740993',
    },
    turn: {
      ...sample<Turn>('Turn'),
      id: 'active_turn',
      session_id: 'session_child',
      state: 'running',
    } as Turn,
    active: false,
    policy: {
      tree_id: 'tree',
      mode: 'prompt',
      revision: '9007199254740993',
      updated_at: at,
    } as Operations['permissions.policy']['result'],
  };
  data.session.configuration.compaction = {
    model: { name: 'original', provider: 'provider', effort: 'high', temperature: 0, top_p: 0 },
    threshold_percent: 50,
  };
  const client = await Client.connect(
    async (request) => {
      calls.push(structuredClone(request));
      if (handlers[request.method])
        return { jsonrpc: '2.0', id: request.id, result: await handlers[request.method]!(request) };
      let result: unknown;
      switch (request.method) {
        case 'initialize':
          result = {
            major: 4,
            minor: 0,
            runtime_id: 'runtime',
            process_epoch: 'boot',
            network_client: false,
            builtins: [],
          };
          break;
        case 'sessions.history_page':
          result = {
            snapshot: {
              session_id: data.session.id,
              revision: '1',
              through_sequence: '0',
              message_count: '0',
            },
            messages: [],
            next_cursor: null,
          };
          break;
        case 'sessions.observe':
          result = {
            snapshot: {
              session_id: data.session.id,
              revision: '1',
              through_sequence: '0',
              message_count: '0',
            },
            epoch: 'boot',
            messages: [],
            preview: null,
          };
          break;
        case 'sessions.activity':
          result = {
            session_id: data.session.id,
            lifecycle: 'active',
            active_turn: data.active ? data.turn : null,
            active_input_id: data.active ? 'input' : null,
            queued_input_count: '0',
            pending_permission_count: '0',
            pending_question_count: '0',
            execution_permit: data.active,
            active_workspace_action_id: null,
          };
          break;
        case 'sessions.turns':
          result = { items: [], next_cursor: null };
          break;
        case 'sessions.list':
          result = { items: [data.session] };
          break;
        case 'goals.current':
          result = { goal: null };
          break;
        case 'schedules.list':
          result = { items: [], next_after: null, next_cursor: null };
          break;
        case 'context.compactions':
        case 'grants.list':
        case 'budgets.list':
        case 'resources.list':
          result = { items: [] };
          break;
        case 'context.head':
          result = { session_id: data.session.id, revision: '1', compaction_id: null };
          break;
        case 'permissions.policy':
          result = data.policy;
          break;
        case 'mcp.configuration':
          result = {
            revision: hash,
            servers: [],
            imports: { claude: null, codex: null, project: null, opencode: null, offered: false },
            brand_icons: false,
          };
          break;
        case 'mcp.status':
          result = { items: [] };
          break;
        case 'mcp.refresh':
        case 'mcp.reload':
          result = {
            added: [],
            existing: [],
            changed: [],
            servers: [],
            blocked: [],
            source_errors: {},
          };
          break;
        default:
          throw new Error(`Unexpected ${request.method}`);
      }
      return { jsonrpc: '2.0', id: request.id, result: structuredClone(result) };
    },
    { clientID: 'test' },
  );
  const session = client.session(data.session.id),
    view = createSessionView(session, { pollIntervalMs: 60_000 });
  await view.start();
  const execution = createExecutionView(session, view, { pollIntervalMs: 60_000 });
  await execution.start();
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const records = new Map<string, RecoveryRecord>(),
    journal = new RecoveryJournal({
      list: async () => [...records.values()],
      put: async (_namespace, key, record) => {
        records.set(key, record);
      },
      delete: async (_namespace, key) => {
        records.delete(key);
      },
    });
  const run = vi.fn(async (command: DurableCommand<DurableMethod>) => command.send());
  const runtime = {
    queries,
    run,
    command: (
      client: Client,
      method: Parameters<Client['command']>[0],
      params: Parameters<Client['command']>[1],
    ) => client.command(method, params, { journal }),
    platform: { copy: vi.fn(), download: vi.fn() },
  } as unknown as AppRuntime;
  const props: InspectorProps = {
    client,
    session,
    rootId: 'session_root',
    tree: {
      id: 'tree',
      metadata: { title: 'Test', pinned: false, archived: false },
      engine: 'starlark',
      revision: '1',
      created_at: at,
    },
    selected: data.session,
    view,
    execution,
    connected: true,
  };
  afterEach(async () => {
    queries.clear();
    await execution.dispose();
    await view.dispose();
  });
  const wrap = (node: ReactNode) => (
    <RuntimeContext.Provider value={runtime}>
      <ThemeProvider initialTheme="light">
        <UIProvider>
          <QueryClientProvider client={queries}>{node}</QueryClientProvider>
        </UIProvider>
      </ThemeProvider>
    </RuntimeContext.Provider>
  );
  return {
    data,
    client,
    props,
    handlers,
    calls,
    queries,
    records,
    run,
    wrap,
    render: (node: ReactNode) => render(wrap(node)),
    count: (method: string) => calls.filter((call) => call.method === method).length,
  };
}
const mail = (
  id = 'mail1',
  revision = '1',
): NonNullable<Operations['mail.list']['result']['items']>[number] => ({
  id,
  revision,
  recipient_id: 'session_child',
  source: { kind: 'session', id: 'session_root' },
  state: 'pending',
  subject: id,
  delivery: 'queued',
  evidence_ref: null,
  body_bytes: '16',
  available_at: at,
  created_at: at,
  revised_at: at,
});
it('reads exact child mail bodies only on selection, refreshes their revision, and replaces bounded pages', async () => {
  const f = await fixture();
  let revision = '1';
  f.handlers['mail.list'] = (request) => ({
    items: [mail(params(request, 'mail.list').after ? 'mail2' : 'mail1', revision)],
  });
  f.handlers['mail.read'] = (request) => ({
    mail: mail(params(request, 'mail.read').mail_id, revision),
    body: `Body revision ${revision}`,
  });
  f.render(<Mailbox {...f.props} />);
  await screen.findByText('mail1');
  expect(f.count('mail.read')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Inspect message' }));
  await screen.findByText('Body revision 1');
  revision = '9007199254740993';
  await act(() =>
    f.queries.invalidateQueries({ predicate: (query) => query.queryKey.includes('mail.list') }),
  );
  await screen.findByText('Body revision 9007199254740993');
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
  await screen.findByText('mail2');
  expect(screen.queryByText('mail1')).toBeNull();
  expect(screen.queryByText('Body revision 9007199254740993')).toBeNull();
  expect(
    f.calls
      .filter((call) => call.method.startsWith('mail.'))
      .every((call) => (call.params as { session_id: string }).session_id === 'session_child'),
  ).toBe(true);
  expect(f.calls.filter((call) => call.method === 'mail.list').at(-1)?.params).toEqual({
    session_id: 'session_child',
    limit: 32,
    after: 'mail1',
  });
  expect(f.run).not.toHaveBeenCalled();
});
it('retains displayed agents while disconnected and never sends disabled controls', async () => {
  const f = await fixture();
  const mounted = f.render(<Agents {...f.props} />);
  await screen.findByText('session_child');
  mounted.rerender(f.wrap(<Agents {...f.props} connected={false} />));
  for (const name of ['Stop subtree', 'Delete agent', 'Resume agent']) {
    const button = screen.getByRole('button', { name });
    expect(button.hasAttribute('disabled')).toBe(true);
    fireEvent.click(button);
  }
  expect(f.count('sessions.lifecycle')).toBe(0);
  expect(f.count('sessions.delete')).toBe(0);
});
it('cancels the captured active child turn after verifying exact session ownership', async () => {
  const f = await fixture();
  f.data.active = true;
  await f.props.view.refresh();
  f.handlers['turns.get'] = () => f.data.turn;
  f.handlers['turns.cancel'] = () => ({ ...f.data.turn, state: 'cancelling' });
  f.render(<Agents {...f.props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Cancel current turn' }));
  await waitFor(() => expect(f.count('turns.cancel')).toBe(1));
  expect(f.calls.find((call) => call.method === 'turns.cancel')?.params).toEqual({
    turn_id: 'active_turn',
  });
  expect(f.calls.filter((call) => call.method === 'turns.get').map((call) => call.params)).toEqual([
    { turn_id: 'active_turn' },
  ]);
});
it('keeps exact captured compaction revision and sampling when another client edits', async () => {
  const f = await fixture();
  const mounted = f.render(<Compaction {...f.props} />);
  fireEvent.change(screen.getByLabelText('Compaction model'), { target: { value: 'my-model' } });
  const selected = structuredClone(f.data.session);
  selected.config_revision = '9007199254740994';
  selected.configuration.compaction.model = { provider: 'other', name: 'their-model', effort: '' };
  mounted.rerender(f.wrap(<Compaction {...f.props} selected={selected} />));
  f.handlers['sessions.configure'] = () => {
    throw new Error('Configuration revision changed');
  };
  fireEvent.click(screen.getByRole('button', { name: 'Apply compaction settings' }));
  await screen.findByText('Configuration revision changed');
  expect(f.calls.find((call) => call.method === 'sessions.configure')?.params).toEqual({
    session_id: 'session_child',
    expected_revision: '9007199254740993',
    patch: {
      compaction: {
        model: { name: 'my-model', provider: 'provider', effort: 'high', temperature: 0, top_p: 0 },
        threshold_percent: 50,
      },
    },
  });
  expect(f.count('sessions.configure')).toBe(1);
  expect(f.run).not.toHaveBeenCalled();
});
it('saves goals and schedules as journaled durable commands with exact IDs', async () => {
  const f = await fixture();
  f.handlers['goals.create'] = (request) => ({
    id: params(request, 'goals.create').goal_id,
    goal: null,
    current: false,
    initial: null,
    deleted_at: at,
  });
  f.handlers['schedules.create'] = (request) => ({
    id: params(request, 'schedules.create').schedule_id,
    schedule: null,
    deleted_at: at,
  });
  f.render(<Goals {...f.props} />);
  await waitFor(() => expect(f.count('goals.current')).toBe(1));
  fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'Finish the audit' } });
  fireEvent.change(screen.getByLabelText('Additional goal continuations'), {
    target: { value: '9007199254740993' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save goal' }));
  await waitFor(() => expect(f.count('goals.create')).toBe(1));
  fireEvent.change(screen.getByLabelText('When'), { target: { value: 'every 30m' } });
  fireEvent.change(screen.getByLabelText('Scheduled prompt'), {
    target: { value: 'Check progress' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Create schedule' }));
  await waitFor(() => expect(f.count('schedules.create')).toBe(1));
  expect(f.run.mock.calls.every(([command]) => command instanceof DurableCommand)).toBe(true);
  expect(f.records.size).toBe(2);
  const sent = f.calls.find((call) => call.method === 'goals.create')!;
  expect(sent.params).toMatchObject({
    session_id: 'session_child',
    expected_current: null,
    start: false,
    spec: { text: 'Finish the audit', max_continuations: '9007199254740993' },
  });
  expect(
    [...f.records.values()].some(
      (record) =>
        JSON.parse(record.request).params.goal_id === params(sent, 'goals.create').goal_id,
    ),
  ).toBe(true);
});
it('full scheduled prompts are explicit reads, including text beyond the metadata preview', async () => {
  const f = await fixture(),
    schedule = {
      id: 'schedule',
      session_id: 'session_child',
      expression: 'every 30m',
      first_due: at,
      next_due: at,
      cancelled_at: null,
      failure: null,
      created_at: at,
      parts_bytes: '6000',
      preview: 'bounded preview',
      preview_truncated: true,
      latest: null,
    };
  f.handlers['schedules.list'] = () => ({ items: [schedule], next_after: null, next_cursor: null });
  f.handlers['schedules.get'] = () => ({
    schedule,
    parts: [{ type: 'text', text: 'x'.repeat(5000) + ' FULL PROMPT' }],
  });
  f.render(<Goals {...f.props} />);
  await screen.findByText('bounded preview…');
  expect(f.count('schedules.get')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Read scheduled prompt' }));
  await waitFor(() =>
    expect(screen.getByRole('region', { name: 'Scheduled prompt' }).textContent).toContain(
      'FULL PROMPT',
    ),
  );
  expect(f.calls.find((call) => call.method === 'schedules.get')?.params).toEqual({
    session_id: 'session_child',
    schedule_id: 'schedule',
  });
});
it('shows a child’s shared permission mode without allowing it to replace root authority', async () => {
  const f = await fixture();
  f.render(<Permissions {...f.props} />);
  await screen.findByText(/Current: Ask for approval/);
  const button = screen.getByRole('button', { name: 'Apply policy' });
  expect(button.hasAttribute('disabled')).toBe(true);
  fireEvent.click(button);
  expect(f.run).not.toHaveBeenCalled();
});
it('revokes only the selected exact grant and keeps resource identities visible', async () => {
  const f = await fixture();
  const grant = {
    id: 'grant1',
    session_id: 'session_child',
    capability: 'mcp.call',
    resource: 'mcp_call_exact_definition',
    operation_id: null,
    issuer_id: 'parent_grant',
    created_at: at,
    revoked_at: null,
  };
  f.handlers['grants.list'] = () => ({ items: [grant] });
  f.handlers['grants.revoke'] = () => ({ ...grant, revoked_at: at });
  f.render(<Permissions {...f.props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Revoke grant' }));
  await waitFor(() => expect(f.count('grants.revoke')).toBe(1));
  expect(f.calls.find((call) => call.method === 'grants.revoke')?.params).toEqual({
    grant_id: 'grant1',
  });
  expect(screen.getByText('mcp_call_exact_definition')).toBeDefined();
});
it('uses exact budget counters without presenting a retained execution window as whole-tree accounting', async () => {
  const f = await fixture();
  f.handlers['budgets.list'] = () => ({
    items: [
      {
        session_id: 'session_child',
        kind: 'model_cost_nano_usd',
        revision: '1',
        limit: null,
        used: '9007199254740993',
        reserved: '0',
        uncertain: '23883863000',
        incomplete: true,
      },
    ],
  });
  f.render(<Limits {...f.props} />);
  await screen.findByText('$9007199.254740993 used · $0.000000000 in flight');
  expect(screen.getByText('No local cap')).toBeDefined();
  expect(screen.getByText('Usage is incomplete · $23.883863000 unconfirmed.')).toBeDefined();
  expect(screen.queryByLabelText('Entire session tree accounting')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Set cap' })).toBeNull();
  expect(formatBudgetAmount('model_elapsed_millis', '12345')).toBe('12.345 s');
});
it('keeps MCP controls state-specific and refreshes only the selected child', async () => {
  const f = await fixture();
  f.handlers['mcp.status'] = () => ({
    items: ['ready', 'blocked', 'unreadable', 'disabled', 'connecting'].map((state) => ({
      name: state,
      state,
      tools: 0,
      source: 'host',
      note: '',
      failure: null,
    })),
  });
  f.render(<MCP {...f.props} />);
  await screen.findByRole('button', { name: 'Reconnect ready' });
  expect(screen.queryByRole('button', { name: 'Reconnect blocked' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Reconnect connecting' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Enable disabled for this session' })).toBeDefined();
  fireEvent.click(
    screen.getByRole('button', { name: 'Refresh MCP configuration for this session' }),
  );
  await waitFor(() => expect(f.count('mcp.refresh')).toBe(1));
  expect(f.calls.find((call) => call.method === 'mcp.refresh')?.params).toEqual({
    session_id: 'session_child',
  });
  expect(f.count('mcp.reload')).toBe(0);
});
it('does not replay a conflicting shared host import edit', async () => {
  const f = await fixture();
  f.handlers['mcp.configure'] = () => {
    throw new Error('Host revision changed');
  };
  f.render(<MCP {...f.props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Enable project imports' }));
  await screen.findByText('Host revision changed');
  expect(f.count('mcp.configure')).toBe(1);
  expect(f.count('mcp.configuration')).toBe(2);
  expect(f.count('mcp.refresh')).toBe(0);
  expect(f.calls.find((call) => call.method === 'mcp.configure')?.params).toMatchObject({
    revision: hash,
    imports: { project: { enabled: true, only: [], exclude: [] } },
  });
});
it('reads immutable shared values across UTF-8 page boundaries and verifies their digest', async () => {
  const f = await fixture(),
    bytes = new TextEncoder().encode('x'.repeat(65535) + '🌍');
  const version = {
    id: 'version',
    tree_id: 'tree',
    session_id: null,
    key: 'state',
    revision: '9007199254740993',
    author_id: 'session_root',
    digest: Buffer.from(await webcrypto.subtle.digest('SHA-256', bytes)).toString('hex'),
    size: String(bytes.length),
    created_at: at,
  };
  f.handlers['state.read'] = (request) => {
    const p = request.params as Operations['state.read']['params'];
    return {
      version,
      offset: p.offset,
      data_base64: Buffer.from(bytes.slice(Number(p.offset), Number(p.offset) + p.length)).toString(
        'base64',
      ),
    };
  };
  const result = await readStateBytes(
    f.props.session,
    version,
    1 << 20,
    new AbortController().signal,
  );
  expect(new TextDecoder().decode(result)).toBe('x'.repeat(65535) + '🌍');
  expect(f.calls.filter((call) => call.method === 'state.read').map((call) => call.params)).toEqual(
    [
      { session_id: 'session_child', version_id: 'version', offset: '0', length: 65536 },
      { session_id: 'session_child', version_id: 'version', offset: '65536', length: 3 },
    ],
  );
  await expect(
    readStateBytes(
      f.props.session,
      { ...version, size: String(2 << 20) },
      1 << 20,
      new AbortController().signal,
    ),
  ).rejects.toThrow('read limit');
  expect(f.count('state.read')).toBe(2);
  f.handlers['state.read'] = (request) => ({
    version: { ...version, tree_id: 'foreign' },
    offset: params(request, 'state.read').offset,
    data_base64: '',
  });
  await expect(
    readStateBytes(f.props.session, version, 1 << 20, new AbortController().signal),
  ).rejects.toThrow('identity changed');
});

it('an uncertain durable action retains its exact request and cannot mint a second identity from the same button', async () => {
  const f = await fixture();
  f.handlers['schedules.create'] = () => {
    throw new DeliveryError('Acknowledgement was lost');
  };
  f.render(<Goals {...f.props} />);
  await waitFor(() => expect(f.count('goals.current')).toBe(1));
  fireEvent.change(screen.getByLabelText('When'), { target: { value: 'every 30m' } });
  fireEvent.change(screen.getByLabelText('Scheduled prompt'), { target: { value: 'Once' } });
  const button = screen.getByRole('button', { name: 'Create schedule' });
  fireEvent.click(button);
  await screen.findByText('Acknowledgement was lost');
  expect(button.hasAttribute('disabled')).toBe(true);
  fireEvent.click(button);
  expect(f.count('schedules.create')).toBe(1);
  expect(f.records.size).toBe(1);
  const request = f.calls.find((call) => call.method === 'schedules.create')!;
  expect(JSON.parse([...f.records.values()][0]!.request)).toEqual({
    method: 'schedules.create',
    params: request.params,
  });
  expect(screen.getByText(/Inspect this command in Settings/)).toBeDefined();
});
it('a stopped root can explicitly save the current policy using an exact receipt and revision', async () => {
  const f = await fixture(),
    selected = {
      ...f.data.session,
      id: 'session_root',
      parent_id: null,
      lifecycle: 'stopped' as const,
    };
  f.handlers['permissions.set_mode'] = (request) => {
    const p = params(request, 'permissions.set_mode');
    return {
      id: p.edit_id,
      session_id: p.session_id,
      expected_revision: p.expected_revision,
      mode: p.mode,
      previous_mode: 'prompt',
      policy: f.data.policy,
      created_at: at,
    };
  };
  f.render(
    <Permissions {...f.props} session={f.client.session('session_root')} selected={selected} />,
  );
  const button = await screen.findByRole('button', { name: 'Apply policy' });
  expect(button.hasAttribute('disabled')).toBe(false);
  fireEvent.click(button);
  await waitFor(() => expect(f.count('permissions.set_mode')).toBe(1));
  expect(f.calls.find((call) => call.method === 'permissions.set_mode')?.params).toMatchObject({
    session_id: 'session_root',
    expected_revision: '9007199254740993',
    mode: 'prompt',
  });
  expect(f.records.size).toBe(1);
});
