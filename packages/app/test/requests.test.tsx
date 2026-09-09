import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {expect, it, vi} from 'vitest';
import {UIProvider} from '@whip/ui';
import type {Session} from '@whip/sdk';
import type {RootSnapshot} from '@whip/protocol';
import {PendingRequests} from '../src/requests';
import {RuntimeContext} from '../src/context';
import type {AppRuntime} from '../src/runtime';

function permissionQueue() {
  const root = {
    agents: [{id: 'root:explorer', name: 'Explore repository'}, {id: 'root:reviewer', name: 'Review changes'}],
    permissions: [
      {id: 'first', agent_id: 'root:explorer', status: 'pending', operation: 'bash', command: 'git status --short', rule: 'bash:git *'},
      {id: 'second', agent_id: 'root:reviewer', status: 'pending', operation: 'read', canonical_path: '/project/README.md', rule: 'read:/project/*'},
    ],
  } as RootSnapshot;
  const decide = vi.fn(async (_decision: unknown) => {});
  const refresh = vi.fn(async () => {});
  const session = {rootId: 'root', client: {permissions: {decide}}} as unknown as Session;
  const runtime = {report: vi.fn()} as unknown as AppRuntime;
  const ui = (snapshot = root, disabled = false) => <RuntimeContext.Provider value={runtime}><UIProvider>
    <PendingRequests root={snapshot} session={session} disabled={disabled} refresh={refresh}/>
  </UIProvider></RuntimeContext.Provider>;
  return {root, decide, refresh, ui};
}

it('shows one approval with the agent name and advances only when the snapshot resolves it', async () => {
  const f = permissionQueue();
  const {rerender} = render(f.ui());
  expect(screen.getAllByRole('region', {name: 'Your approval is needed'})).toHaveLength(1);
  expect(screen.getByText('Explore repository')).toBeTruthy();
  expect(screen.queryByText('root:explorer')).toBeNull();
  expect(screen.queryByText('/project/README.md')).toBeNull();
  expect(screen.getByRole('status').textContent).toBe('1 more waiting');
  fireEvent.click(screen.getByRole('button', {name: 'Allow once'}));
  await waitFor(() => expect(f.refresh).toHaveBeenCalledOnce());
  expect(f.decide).toHaveBeenCalledExactlyOnceWith({root_id: 'root', permission_id: 'first', allow: true});
  expect(screen.getByText('git status --short')).toBeTruthy();
  rerender(f.ui({...f.root, permissions: [{...f.root.permissions![0], status: 'allowed'}, f.root.permissions![1]]}));
  expect(screen.queryByText('git status --short')).toBeNull();
  expect(screen.getByText('Review changes')).toBeTruthy();
  expect(screen.getByText('/project/README.md')).toBeTruthy();
  expect(screen.queryByRole('status')).toBeNull();
  fireEvent.click(screen.getByRole('button', {name: 'Deny'}));
  await waitFor(() => expect(f.decide).toHaveBeenLastCalledWith({root_id: 'root', permission_id: 'second', allow: false}));
});

it('resets the permission scope when another client resolves the displayed request', async () => {
  const f = permissionQueue();
  const user = userEvent.setup();
  const {rerender} = render(f.ui());
  await user.click(screen.getByRole('combobox', {name: 'Permission scope'}));
  await user.click(await screen.findByRole('option', {name: 'Remember on this host'}));
  expect(screen.getByRole('button', {name: 'Allow and remember'})).toBeTruthy();
  rerender(f.ui({...f.root, permissions: f.root.permissions!.slice(1)}));
  expect(screen.getByRole('combobox', {name: 'Permission scope'}).textContent).toContain('This request only');
  fireEvent.click(screen.getByRole('button', {name: 'Allow once'}));
  await waitFor(() => expect(f.decide).toHaveBeenCalledExactlyOnceWith({root_id: 'root', permission_id: 'second', allow: true}));
});

it('holds an uncertain decision on its original request until an explicit state refresh', async () => {
  const f = permissionQueue();
  let reject!: (error: Error) => void;
  f.decide.mockImplementationOnce(() => new Promise((_, fail) => {reject = fail;}));
  render(f.ui());
  fireEvent.click(screen.getByRole('button', {name: 'Allow once'}));
  expect(screen.getByRole('combobox', {name: 'Permission scope'})).toHaveProperty('disabled', true);
  expect(screen.getByRole('button', {name: 'Deny'})).toHaveProperty('disabled', true);
  await act(async () => reject(new Error('Connection lost')));
  expect(screen.getByText('Explore repository')).toBeTruthy();
  expect(screen.queryByRole('button', {name: 'Allow once'})).toBeNull();
  fireEvent.click(screen.getByRole('button', {name: 'Refresh permission state before retrying'}));
  await screen.findByRole('button', {name: 'Allow once'});
  expect(f.refresh).toHaveBeenCalledOnce();
  expect(f.decide).toHaveBeenCalledOnce();
});

it('uses readable missing-name fallbacks and qualifies an incomplete permission queue', () => {
  const f = permissionQueue();
  const root = {...f.root, agents: [], permissions: [f.root.permissions![0]], omitted: {permissions: true}};
  const {rerender} = render(f.ui(root, true));
  expect(screen.getByText('Unnamed agent')).toBeTruthy();
  expect(screen.queryByText('root:explorer')).toBeNull();
  expect(screen.getByRole('status').textContent).toBe('More approvals pending');
  expect(screen.getByRole('button', {name: 'Allow once'})).toHaveProperty('disabled', true);
  rerender(f.ui({...root, permissions: [{...root.permissions[0], agent_id: 'root'}]}));
  expect(screen.getByText('Root agent')).toBeTruthy();
});

function fixture(multiple: boolean) {
  const question = {question_id: 'question', question: 'Choose an approach', multiple, options: [{label: 'Inspect', description: 'Read the current state.'}, {label: 'Implement', description: 'Apply the agreed changes.'}]};
  const answerQuestion = vi.fn(() => ({}));
  const session = {rootId: 'root', answerQuestion} as unknown as Session;
  const runtime = {run: vi.fn(async () => {}), report: vi.fn()} as unknown as AppRuntime;
  render(<RuntimeContext.Provider value={runtime}><UIProvider><PendingRequests root={{questions: [question]} as RootSnapshot} session={session} disabled={false} refresh={async () => {}}/></UIProvider></RuntimeContext.Provider>);
  return {answerQuestion};
}
it('single-choice questions use named radios, retain descriptions and submit only one choice', async () => {
  const {answerQuestion} = fixture(false);
  expect(screen.queryByRole('checkbox')).toBeNull();
  expect(screen.getByRole('radiogroup', {name: 'Choose an approach'})).toBeTruthy();
  const inspect = screen.getByRole('radio', {name: /Inspect/});
  const implement = screen.getByRole('radio', {name: /Implement/});
  expect(document.getElementById(inspect.getAttribute('aria-describedby')!)?.textContent).toBe('Read the current state.');
  fireEvent.click(inspect);
  fireEvent.click(implement);
  expect(inspect.getAttribute('aria-checked')).toBe('false');
  expect(implement.getAttribute('aria-checked')).toBe('true');
  fireEvent.click(screen.getByRole('button', {name: 'Send'}));
  await waitFor(() => expect(answerQuestion).toHaveBeenCalledWith('question', ['Implement'], false));
});
it('multiple-choice questions retain independently selectable checkboxes', async () => {
  const {answerQuestion} = fixture(true);
  expect(screen.queryByRole('radio')).toBeNull();
  fireEvent.click(screen.getByRole('checkbox', {name: /Inspect/}));
  fireEvent.click(screen.getByRole('checkbox', {name: /Implement/}));
  fireEvent.click(screen.getByRole('button', {name: 'Send'}));
  await waitFor(() => expect(answerQuestion).toHaveBeenCalledWith('question', ['Inspect', 'Implement'], false));
});

for (const kind of ['permission', 'question'] as const) {
  for (const next of ['composer', 'resolved card', 'another control', 'another recipient', 'failed answer'] as const) {
    it(`${kind} resolution respects focus when the next destination is ${next}`, async () => {
      let resolve!: () => void;
      let reject!: (error: Error) => void;
      const completion = new Promise<void>((done, fail) => {resolve = done; reject = fail;});
      const decide = vi.fn(() => completion);
      const session = {rootId: 'root', client: {permissions: {decide}}, answerQuestion: vi.fn(() => ({}))} as unknown as Session;
      const runtime = {run: vi.fn(() => completion), report: vi.fn()} as unknown as AppRuntime;
      const root = (kind === 'permission'
        ? {permissions: [{id: 'permission', status: 'pending', operation: 'write', rule: 'write:*', canonical_path: '/tmp/example.txt'}]}
        : {questions: [{question_id: 'question', question: 'Continue?'}]}) as RootSnapshot;
      const ui = (recipient: number, pending = true) => <RuntimeContext.Provider value={runtime}><UIProvider>
        <PendingRequests root={pending ? root : {} as RootSnapshot} session={session} disabled={false} refresh={async () => {}}/>
        <textarea key={recipient} aria-label="Composer" data-whip-composer/>
        <input aria-label="Another control"/>
      </UIProvider></RuntimeContext.Provider>;
      const {rerender} = render(ui(0));
      if (kind === 'permission') expect(screen.getByRole('combobox', {name: 'Permission scope'}).textContent).toContain('This request only');
      const button = screen.getByRole('button', {name: kind === 'permission' ? 'Deny' : 'Dismiss question', exact: true});
      button.focus();
      fireEvent.click(button);
      if (next === 'resolved card') {
        rerender(ui(0, false));
        expect(document.activeElement).toBe(document.body);
      }
      if (next === 'another control') screen.getByLabelText('Another control').focus();
      if (next === 'another recipient') rerender(ui(1));
      await act(async () => {
        if (next === 'failed answer') reject(new Error('Request was not accepted'));
        else resolve();
      });
      const composer = screen.getByLabelText('Composer');
      if (next === 'composer' || next === 'resolved card') expect(document.activeElement).toBe(composer);
      else expect(document.activeElement).not.toBe(composer);
      if (next === 'another control') expect(document.activeElement).toBe(screen.getByLabelText('Another control'));
    });
  }
}

it('single-choice text and option answers replace each other', async () => {
  const {answerQuestion} = fixture(false);
  fireEvent.click(screen.getByRole('radio', {name: /Inspect/}));
  fireEvent.change(screen.getByLabelText('Write your own response'), {target: {value: 'My approach'}});
  expect(screen.getByRole('radio', {name: /Inspect/}).getAttribute('aria-checked')).toBe('false');
  fireEvent.click(screen.getByRole('radio', {name: /Implement/}));
  expect((screen.getByLabelText('Write your own response') as HTMLInputElement).value).toBe('');
  fireEvent.click(screen.getByRole('button', {name: 'Send', exact: true}));
  await waitFor(() => expect(answerQuestion).toHaveBeenCalledWith('question', ['Implement'], false));
});

it('batched questions preserve earlier answers and submit a skipped last page exactly once', async () => {
  const answerQuestions = vi.fn(() => ({}));
  const session = {rootId: 'root', answerQuestions} as unknown as Session;
  const runtime = {run: vi.fn(async () => {}), report: vi.fn()} as unknown as AppRuntime;
  const root = {questions: [{question_id: 'batch', questions: [
    {question: 'Approach?', options: [{label: 'Inspect', recommended: true}, {label: 'Implement'}]},
    {question: 'Storage?', options: [{label: 'SQLite'}, {label: 'Postgres'}]},
  ]}]} as RootSnapshot;
  render(<RuntimeContext.Provider value={runtime}><UIProvider><PendingRequests root={root} session={session} disabled={false} refresh={async () => {}}/></UIProvider></RuntimeContext.Provider>);
  expect(screen.getByRole('radio', {name: /Inspect/}).getAttribute('aria-checked')).toBe('false');
  fireEvent.click(screen.getByRole('radio', {name: /Inspect/}));
  fireEvent.click(screen.getByRole('button', {name: 'Next', exact: true}));
  fireEvent.click(screen.getByRole('radio', {name: /SQLite/}));
  fireEvent.click(screen.getByRole('button', {name: 'Back', exact: true}));
  expect(screen.getByRole('radio', {name: /Inspect/}).getAttribute('aria-checked')).toBe('true');
  fireEvent.click(screen.getByRole('button', {name: 'Next', exact: true}));
  fireEvent.click(screen.getByRole('button', {name: 'Skip', exact: true}));
  await waitFor(() => expect(answerQuestions).toHaveBeenCalledExactlyOnceWith('batch', [{answer: ['Inspect']}, null]));
});

it('a one-item batch retains the batched answer contract', async () => {
  const answerQuestions = vi.fn(() => ({}));
  const session = { rootId: 'root', answerQuestions } as unknown as Session;
  const runtime = { run: vi.fn(async () => {}), report: vi.fn() } as unknown as AppRuntime;
  render(<RuntimeContext.Provider value={runtime}><UIProvider><PendingRequests root={{ questions: [{ question_id: 'batch', questions: [{ question: 'Continue?', options: [{ label: 'Yes' }] }] }] } as RootSnapshot} session={session} disabled={false} refresh={async () => {}} /></UIProvider></RuntimeContext.Provider>);
  fireEvent.click(screen.getByRole('radio', { name: /Yes/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(answerQuestions).toHaveBeenCalledWith('batch', [{ answer: ['Yes'] }]));
});
