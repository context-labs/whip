import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import {
  DeliveryError,
  type Operations,
  type Permission,
  type Question,
} from '@whip/sdk';
import {
  PendingRequests,
  PermissionRequest,
  QuestionRequest,
} from '../src/requests';
import { providerFixture } from './provider-fixture';
import { at, operation, parameters } from './native-conversation-fixture';

beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
afterEach(() => vi.unstubAllGlobals());
const question = (
  batch = false,
  multiple = false,
): Extract<Question, { state: 'pending' }> => ({
  operation_id: 'question-op',
  session_id: 'root',
  turn_id: 'turn',
  cell_id: 'cell',
  request: {
    batch,
    questions: [
      {
        question: 'Choose a direction',
        multiple,
        options: [
          {
            label: 'North',
            description: 'Follow the river',
            recommended: true,
          },
          { label: 'South', description: 'Take the road', recommended: false },
        ],
      },
      ...(batch
        ? [
            {
              question: 'Choose a color',
              multiple: false,
              options: [
                { label: 'Blue', description: 'Sea', recommended: false },
                { label: 'Green', description: 'Forest', recommended: true },
              ] satisfies Question['request']['questions'][number]['options'],
            },
          ]
        : []),
    ],
  },
  state: 'pending',
  answers: [],
  close_reason: null,
  created_at: at,
  deadline: '2026-09-28T12:05:00Z',
  closed_at: null,
});
const permission: Permission = {
  operation_id: 'operation',
  state: 'pending',
  created_at: at,
  resolved_at: null,
};
async function questionFixture(batch = false, multiple = false) {
  const f = await providerFixture(),
    session = f.client.session('root'),
    refresh = vi.fn(async () => {}),
    q = question(batch, multiple);
  f.data.handlers['questions.answer'] = (request) => ({
    ...q,
    state: 'answered',
    answers: parameters('AnswerQuestionParams', request.params).answers,
    closed_at: at,
  });
  f.data.handlers['questions.get'] = () => q;
  return {
    ...f,
    q,
    session,
    refresh,
    app: (disabled = false) => (
      <>
        <QuestionRequest
          question={q}
          session={session}
          disabled={disabled}
          refresh={refresh}
        />
        <textarea
          data-whip-composer
          aria-label="Draft"
          defaultValue="Keep draft"
        />
      </>
    ),
  };
}
it('keeps ordinary approval pending in place and shows the requested command and friendly requester', async () => {
  const f = await providerFixture();
  const op = { ...operation('operation', 'waiting', 'shell.run'), arguments: { command: 'git status --short' } };
  f.data.handlers['operations.get'] = () => op;
  let resolve!: () => void;
  f.data.handlers['permissions.resolve'] = () => new Promise(done => { resolve = () => done({ ...permission, state: 'approved', resolved_at: at }); });
  f.mount(<PermissionRequest permission={permission} operation={op} waiting="0" session={f.client.session('root')} disabled={false} refresh={async () => {}} />);
  expect(screen.getByText('Root agent')).toBeTruthy();
  expect(screen.getByLabelText('Requested operation').textContent).toBe('git status --short');
  const button = screen.getByRole('button', { name: 'Allow once' });
  fireEvent.click(button);
  await waitFor(() => expect(f.count('permissions.resolve')).toBe(1));
  expect(screen.getByRole('button', { name: 'Allow once' })).toBe(button);
  expect(button).toHaveProperty('disabled', true);
  expect(screen.queryByRole('button', { name: 'Retry same approval' })).toBeNull();
  await act(async () => resolve());
  expect(screen.queryByRole('button', { name: 'Retry same approval' })).toBeNull();
});

it.each(['browser.attach', 'shell.run', 'files.read'])(
  'shows exact %s resource and arguments with operation-only approval',
  async (capability) => {
    const f = await providerFixture(),
      session = f.client.session('root'),
      op = {
        ...operation('operation', 'waiting', capability),
        session_id: 'root',
        arguments: {
          command: 'literal request',
          profile: 'recorded profile',
          ports: [3000],
        },
      };
    f.data.handlers['operations.get'] = () => op;
    f.data.handlers['permissions.resolve'] = () => ({
      ...permission,
      state: 'approved',
      resolved_at: at,
    });
    const refresh = vi.fn(async () => {});
    f.mount(
      <PermissionRequest
        permission={permission}
        operation={op}
        waiting="9007199254740993"
        session={session}
        disabled={false}
        refresh={refresh}
      />,
    );
    expect(screen.getByLabelText('Requested resource').textContent).toBe(
      op.resource,
    );
    expect(screen.getByLabelText('Exact operation arguments').textContent).toContain(
      'literal request',
    );
    expect(screen.getByRole('status').textContent).toContain(
      '9007199254740993',
    );
    expect(screen.queryByRole('combobox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Allow once' }));
    await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
    expect(
      f.calls.find((call) => call.method === 'permissions.resolve')?.params,
    ).toEqual({ operation_id: 'operation', approved: true });
    expect(f.count('grants.create')).toBe(0);
  },
);
it('keeps an uncertain approval pinned and checks without switching or replaying the decision', async () => {
  const f = await providerFixture(),
    session = f.client.session('root'),
    op = operation('operation', 'waiting');
  f.data.handlers['operations.get'] = () => op;
  f.data.handlers['permissions.resolve'] = () => {
    throw new DeliveryError('Lost acknowledgement');
  };
  const refresh = vi.fn(async () => {});
  f.mount(
    <PermissionRequest
      permission={permission}
      operation={op}
      waiting="0"
      session={session}
      disabled={false}
      refresh={refresh}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Deny' }));
  await screen.findByRole('button', { name: 'Retry same denial' });
  expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Check approval state' }));
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
  expect(f.count('permissions.resolve')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Retry same denial' }));
  await waitFor(() => expect(f.count('permissions.resolve')).toBe(2));
  expect(
    f.calls
      .filter((call) => call.method === 'permissions.resolve')
      .map((call) => call.params),
  ).toEqual([
    { operation_id: 'operation', approved: false },
    { operation_id: 'operation', approved: false },
  ]);
});
it('uses native pending-only metadata and fetches only the selected operation, with root questions and approvals', async () => {
  const f = await providerFixture(),
    session = f.client.session('root');
  f.data.handlers['permissions.list'] = () => ({ items: [permission] });
  f.data.handlers['operations.get'] = () => ({
    ...operation('operation', 'waiting'),
    session_id: 'root',
  });
  f.data.handlers['questions.list'] = () => ({ items: [question()] });
  f.mount(
    <PendingRequests
      session={session}
      rootId="root"
      disabled={false}
      pendingCount="2"
      refresh={async () => {}}
    />,
  );
  await screen.findByRole('button', { name: 'Allow once' });
  await screen.findByText('Choose a direction');
  expect(
    f.calls.find((call) => call.method === 'permissions.list')?.params,
  ).toEqual({ session_id: 'root', limit: 1, pending_only: true });
  expect(
    f.calls.find((call) => call.method === 'questions.list')?.params,
  ).toEqual({ session_id: 'root', limit: 4, pending_only: true });
  expect(f.count('operations.get')).toBe(1);
  expect(f.count('sessions.history_page')).toBe(0);
});
it('keeps child delegation separate from root-only human approval', async () => {
  const f = await providerFixture();
  f.data.handlers['permissions.list'] = () => ({ items: [permission] });
  f.data.handlers['questions.list'] = () => ({ items: [question()] });
  f.mount(<PendingRequests session={f.client.session('child')} rootId="root" disabled={false} refresh={async () => {}} />);
  await screen.findByText('Choose a direction');
  expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull();
  expect(f.count('permissions.list')).toBe(0);
  expect(f.count('operations.get')).toBe(0);
  expect(f.count('permissions.resolve')).toBe(0);
});
it.each([{ session_id: 'foreign' }, { id: 'different-operation' }])('rejects mismatched approval evidence %j', async patch => {
  const f = await providerFixture();
  f.data.handlers['permissions.list'] = () => ({ items: [permission] });
  f.data.handlers['questions.list'] = () => ({ items: [] });
  f.data.handlers['operations.get'] = () => ({ ...operation('operation', 'waiting'), ...patch });
  f.mount(<PendingRequests session={f.client.session('root')} rootId="root" disabled={false} refresh={async () => {}} />);
  await screen.findByText('Operation belongs to another session or identity');
  expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull();
  expect(f.count('permissions.resolve')).toBe(0);
});

it('rejects foreign pending question evidence before exposing an answer control', async () => {
  const f = await providerFixture();
  f.data.handlers['permissions.list'] = () => ({ items: [] });
  f.data.handlers['questions.list'] = () => ({
    items: [{ ...question(), session_id: 'foreign' }],
  });
  f.mount(
    <PendingRequests
      session={f.client.session('root')}
      rootId="root"
      disabled={false}
      refresh={async () => {}}
    />,
  );
  await screen.findByText(
    'Question list belongs to another session or is no longer pending',
  );
  expect(screen.queryByRole('button', { name: 'Send' })).toBeNull();
});
it('preserves recommended options, descriptions and custom free text without modifying the composer', async () => {
  const f = await questionFixture();
  f.mount(f.app());
  expect(screen.getByText('Recommended')).toBeTruthy();
  expect(screen.getByText('Follow the river')).toBeTruthy();
  fireEvent.click(screen.getByRole('radio', { name: /North/ }));
  fireEvent.change(screen.getByLabelText('Write your own response'), {
    target: { value: 'Take a detour' },
  });
  expect(
    screen.getByRole('radio', { name: /North/ }).getAttribute('aria-checked'),
  ).toBe('false');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(f.refresh).toHaveBeenCalledOnce());
  expect(
    f.calls.find((call) => call.method === 'questions.answer')?.params,
  ).toEqual({
    session_id: 'root',
    operation_id: 'question-op',
    answers: [{ answer: ['Take a detour'], dismissed: false }],
  });
  expect(screen.getByLabelText('Draft')).toHaveProperty('value', 'Keep draft');
});
it('retains multi-selection and batch drafts across Back, with explicit per-question dismissal', async () => {
  const f = await questionFixture(true, true);
  f.mount(f.app());
  fireEvent.click(screen.getByRole('checkbox', { name: /North/ }));
  fireEvent.click(screen.getByRole('checkbox', { name: /South/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Next' }));
  fireEvent.click(screen.getByRole('button', { name: 'Back' }));
  expect(
    screen
      .getByRole('checkbox', { name: /North/ })
      .getAttribute('aria-checked'),
  ).toBe('true');
  fireEvent.click(screen.getByRole('button', { name: 'Next' }));
  fireEvent.click(screen.getByRole('button', { name: 'Skip' }));
  await waitFor(() => expect(f.count('questions.answer')).toBe(1));
  expect(
    parameters(
      'AnswerQuestionParams',
      f.calls.find((call) => call.method === 'questions.answer')?.params,
    ).answers,
  ).toEqual([
    { answer: ['North', 'South'], dismissed: false },
    { answer: [], dismissed: true },
  ]);
});
it('dismisses the entire batch using one exact ordinary operation result', async () => {
  const f = await questionFixture(true);
  f.mount(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss question' }));
  await waitFor(() => expect(f.count('questions.answer')).toBe(1));
  expect(
    parameters(
      'AnswerQuestionParams',
      f.calls.find((call) => call.method === 'questions.answer')?.params,
    ).answers,
  ).toEqual([
    { answer: [], dismissed: true },
    { answer: [], dismissed: true },
  ]);
});
it('freezes an uncertain response, performs read-only checking, then retries only the exact answers', async () => {
  const f = await questionFixture();
  f.data.handlers['questions.answer'] = () => {
    throw new DeliveryError('Lost acknowledgement');
  };
  f.mount(f.app());
  fireEvent.change(screen.getByLabelText('Write your own response'), {
    target: { value: 'Original response' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  await screen.findByRole('button', { name: 'Retry same response' });
  expect(screen.getByLabelText('Write your own response')).toHaveProperty(
    'disabled',
    true,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Check answer state' }));
  await waitFor(() => expect(f.refresh).toHaveBeenCalledOnce());
  expect(f.count('questions.answer')).toBe(1);
  expect(f.count('questions.get')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Retry same response' }));
  await waitFor(() => expect(f.count('questions.answer')).toBe(2));
  const requests = f.calls.filter((call) => call.method === 'questions.answer');
  expect(requests[1]?.params).toEqual(requests[0]?.params);
});
it('supports keyboard free-text submission and disables all effects while disconnected', async () => {
  const f = await questionFixture();
  const view = f.mount(f.app(true));
  for (const button of screen.getAllByRole('button'))
    expect(button).toHaveProperty('disabled', true);
  expect(f.count('questions.answer')).toBe(0);
  view.rerender(f.wrap(f.app()));
  const user = userEvent.setup();
  await user.type(
    screen.getByLabelText('Write your own response'),
    'Keyboard response{Enter}',
  );
  await waitFor(() => expect(f.count('questions.answer')).toBe(1));
});

it('keeps an uncertain decision through query eviction and same-host client replacement', async () => {
  const f = await providerFixture();
  const op = { ...operation('operation', 'waiting'), session_id: 'root', arguments: { path: 'retained.txt' } };
  f.data.handlers['permissions.list'] = () => ({ items: [permission] });
  f.data.handlers['questions.list'] = () => ({ items: [] });
  f.data.handlers['operations.get'] = () => op;
  f.data.handlers['permissions.resolve'] = () => { throw new DeliveryError('Decision acknowledgement lost'); };
  const renderDock = (session: ReturnType<typeof f.client.session>, disabled = false) => <PendingRequests session={session} rootId="root" disabled={disabled} refresh={async () => {}} />;
  const first = f.client.session('root'), view = f.mount(renderDock(first));
  fireEvent.click(await screen.findByRole('button', { name: 'Deny', exact: true }));
  await screen.findByRole('button', { name: 'Retry same denial' });
  view.rerender(f.wrap(renderDock(first, true)));
  act(() => f.queries.removeQueries({ queryKey: ['pending-requests'] }));
  expect(screen.getByRole('button', { name: 'Retry same denial' })).toHaveProperty('disabled', true);
  const next = await providerFixture();
  next.data.handlers['permissions.list'] = () => ({ items: [permission] });
  next.data.handlers['questions.list'] = () => ({ items: [] });
  next.data.handlers['operations.get'] = () => op;
  view.rerender(f.wrap(renderDock(next.client.session('root'))));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Retry same denial' })).toHaveProperty('disabled', false));
  expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull();
  expect(screen.getByLabelText('Requested operation').textContent).toBe('retained.txt');
  expect(f.count('permissions.resolve')).toBe(1); expect(next.count('permissions.resolve')).toBe(0);
});

it('does not replace a lost approval with the next queue entry until an exact read check', async () => {
  const f = await providerFixture(); let next = false;
  const first = { ...operation('operation', 'waiting'), session_id: 'root', arguments: { path: 'first.txt' } };
  const second = { ...first, id: 'second-operation', arguments: { path: 'second.txt' } };
  f.data.handlers['permissions.list'] = () => ({ items: [{ ...permission, operation_id: next ? second.id : first.id }] });
  f.data.handlers['questions.list'] = () => ({ items: [] });
  f.data.handlers['operations.get'] = request => (request.params as { operation_id: string }).operation_id === second.id ? second : { ...first, state: next ? 'succeeded' : 'waiting' };
  f.data.handlers['permissions.resolve'] = () => { next = true; throw new DeliveryError('Decision acknowledgement lost'); };
  f.mount(<PendingRequests session={f.client.session('root')} rootId="root" disabled={false} refresh={async () => {}} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Allow once' })); await screen.findByRole('button', { name: 'Retry same approval' });
  await act(async () => { await f.queries.invalidateQueries({ queryKey: ['pending-requests'] }); });
  expect(screen.getByLabelText('Requested operation').textContent).toBe('first.txt');
  expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull();
  const beforeCheck = f.count('operations.get');
  fireEvent.click(screen.getByRole('button', { name: 'Check approval state' }));
  await waitFor(() => expect(screen.getByLabelText('Requested operation').textContent).toBe('second.txt'));
  expect(screen.getByRole('button', { name: 'Allow once' })).toBeTruthy();
  expect(f.count('permissions.resolve')).toBe(1);
  expect(f.calls.filter(call => call.method === 'operations.get')[beforeCheck]?.params).toEqual({ operation_id: first.id });
});
