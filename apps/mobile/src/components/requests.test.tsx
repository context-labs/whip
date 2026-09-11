import { act, fireEvent, render } from '@testing-library/react-native';
import type { LifecycleEvent, RootSnapshot } from '@whip/protocol';
import type { SessionView } from '@whip/sdk/state';
import { Requests, answerValues, encodeAnswers, questionDefinition, questionDraftKey, questionEntries, questionPayload, restoreAnswers, toggleAnswer } from './requests';
import type { MobileRuntime } from '../runtime/runtime';
import type { Draft } from '../runtime/storage';

let mockRuntime: MobileRuntime;
const mockNativeActions = new Map<string, () => void>();
jest.mock('../runtime/context', () => ({
  useRuntime: () => mockRuntime,
  useRuntimeState: () => require('react').useSyncExternalStore(mockRuntime.subscribe, mockRuntime.getSnapshot),
}));
jest.mock('../theme/theme', () => jest.requireActual('../theme/theme'));
jest.mock('@expo/ui', () => {
  const React = require('react'); const { View, Text, Pressable } = require('react-native');
  return { Host: ({ children }: { children: React.ReactNode }) => React.createElement(View, {}, children), Column: ({ children }: { children: React.ReactNode }) => React.createElement(View, {}, children),
    Button: ({ label, onPress, disabled }: { label: string; onPress(): void; disabled?: boolean }) => { mockNativeActions.set(label, onPress); return React.createElement(Pressable, { accessibilityRole: 'button', accessibilityState: { disabled }, disabled, onPress }, React.createElement(Text, {}, label)); } };
});
const single: LifecycleEvent = { question_id: 'question', agent_id: 'child', question: 'Pick one', options: [{ label: 'A', recommended: true }, { label: 'B' }], multiple: false };
function fixture(question: LifecycleEvent = single) {
  const drafts = new Map<string, Draft>(); const listeners = new Set<() => void>(); let revision = 0;
  const client = { clientId: 'client' };
  let state = { client, host: { runtimeId: 'runtime' }, ready: true, active: true, commands: [], revision };
  let root = { root_id: 'root', permissions: [], questions: [question] } as unknown as RootSnapshot;
  let status = 'live';
  const decisionState = Object.freeze({ items: [], ready: true });
  const runtime = {
    subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); }, getSnapshot: () => state,
    decisions: { getSnapshot: () => decisionState, subscribe: () => () => {}, forRequest: () => undefined, isBlocked: () => false },
    draft: (key: string) => drafts.get(key) ?? { text: '', revision: '' },
    setDraft: (key: string, text: string) => { const draft = { text, revision: `revision-${++revision}` }; drafts.set(key, draft); state = { ...state, revision }; listeners.forEach(listener => listener()); return draft; },
    run: jest.fn(async () => ({ status: 'succeeded', result: {} })), report: jest.fn(),
  };
  mockRuntime = runtime as unknown as MobileRuntime;
  const view = { session: { rootId: 'root', client }, getSnapshot: () => ({ root, status }), refresh: jest.fn(async () => {}) } as unknown as SessionView;
  return { runtime, drafts, view, root: () => root, replaceQuestion: (next: LifecycleEvent) => { root = { ...root, questions: [next] }; }, answeredElsewhere: () => { root = { ...root, questions: [] }; }, stale: () => { status = 'stale'; } };
}
const key = questionDraftKey('runtime', 'root', 'question');

test('recommended is not selected; single choice replaces custom text and final review is required', async () => {
  const f = fixture(); const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
  expect(screen.queryByRole('button', { name: 'Send answers' })).toBeNull();
  expect(screen.getByRole('button', { name: 'A (Recommended)' })).not.toBeSelected();
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'custom');
  await fireEvent.press(screen.getByText('A (Recommended)'));
  expect(screen.getByLabelText('Your answer')).toHaveDisplayValue('');
  expect(f.runtime.run).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Review answers'));
  expect(f.runtime.run).not.toHaveBeenCalled();
  const revision = f.drafts.get(key)!.revision;
  await fireEvent.press(screen.getByText('Send answers'));
  expect(f.runtime.run).toHaveBeenCalledWith('question.answer', { id: 'question', answer: ['A'], dismissed: false }, { rootId: 'root', intent: { requestId: 'question', agentId: 'child', draftKey: key, draftRevision: revision } });
});

test('batched single, multiple plus custom, and skipped pages preserve question and choice order', async () => {
  const question: LifecycleEvent = { question_id: 'question', agent_id: 'child', questions: [
    { question: 'Single', options: [{ label: 'A' }, { label: 'B' }] },
    { question: 'Multiple', multiple: true, options: [{ label: 'C' }, { label: 'D' }] },
    { question: 'Skipped', options: [{ label: 'E', recommended: true }] },
  ] };
  const f = fixture(question); const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
  await fireEvent.press(screen.getByText('B')); await fireEvent.press(screen.getByText('Next question'));
  await fireEvent.press(screen.getByText('D')); await fireEvent.press(screen.getByText('C'));
  await fireEvent.changeText(screen.getByLabelText('Your answer'), '  own answer  ');
  await fireEvent.press(screen.getByText('Next question')); await fireEvent.press(screen.getByText('Skip this question'));
  expect(f.runtime.run).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Send answers'));
  expect(f.runtime.run).toHaveBeenCalledWith('question.answer', { id: 'question', answer: [], dismissed: false, answers: [
    { answer: ['B'], dismissed: false }, { answer: ['D', 'C', 'own answer'], dismissed: false }, { answer: [], dismissed: true },
  ] }, expect.anything());
});

test('blank all-skipped review has a durable revision and changing it invalidates the reviewed send', async () => {
  const f = fixture(); const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
  await fireEvent.press(screen.getByText('Review answers'));
  const before = f.drafts.get(key)!;
  expect(before.revision).not.toBe('');
  await act(() => { f.runtime.setDraft(key, encodeAnswers([{ selected: ['B'], text: '', skipped: false }], questionEntries(single))); });
  expect(screen.getByRole('button', { name: 'Send answers' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Send answers')); expect(f.runtime.run).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Edit answers')); await fireEvent.press(screen.getByText('Skip this question'));
  await fireEvent.press(screen.getByText('Send answers'));
  expect(f.runtime.run).toHaveBeenCalledWith('question.answer', { id: 'question', answer: [], dismissed: true }, expect.anything());
});

test('answered-elsewhere and stale snapshots reject a send even when rendered controls were enabled', async () => {
  for (const changed of ['answered', 'stale']) {
    const f = fixture(); const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
    await fireEvent.press(screen.getByText('A (Recommended)')); await fireEvent.press(screen.getByText('Review answers'));
    if (changed === 'answered') f.answeredElsewhere(); else f.stale();
    await fireEvent.press(screen.getByText('Send answers'));
    expect(f.runtime.run).not.toHaveBeenCalled(); expect(f.runtime.report).toHaveBeenCalled();
    expect(f.drafts.get(key)?.text).toContain('A');
    await screen.unmount();
  }
});

test('a different request resets page/review while preserving the earlier request draft', async () => {
  const f = fixture(); const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'keep this'); await fireEvent.press(screen.getByText('Review answers'));
  const before = f.drafts.get(key);
  f.replaceQuestion({ ...single, question_id: 'next-question' });
  await screen.rerender(<Requests root={f.root()} view={f.view} disabled={false} />);
  expect(screen.queryByText('Send answers')).toBeNull();
  expect(screen.getByLabelText('Your answer')).toHaveDisplayValue('');
  expect(f.drafts.get(key)).toEqual(before);
  expect(questionDraftKey('runtime', 'other-root', 'question')).not.toBe(key);
  expect(questionDraftKey('other-runtime', 'root', 'question')).not.toBe(key);
});

test('form restoration validates definition, values, cardinality and skip flags instead of guessing', () => {
  const entries = questionEntries(single);
  const good = [{ selected: ['A'], text: '', skipped: false }];
  expect(restoreAnswers(encodeAnswers(good, entries), entries)).toEqual({ answers: good });
  for (const invalid of [
    '{invalid', JSON.stringify(good),
    encodeAnswers([{ selected: ['injected'], text: '', skipped: false }], entries),
    encodeAnswers([{ selected: ['A', 'B'], text: '', skipped: false }], entries),
    encodeAnswers([{ selected: ['A'], text: 'other', skipped: false }], entries),
    encodeAnswers([{ selected: ['A'], text: '', skipped: true }], entries),
    JSON.stringify({ version: 1, definition: questionDefinition(entries), answers: [{ selected: [], text: '', skipped: 'yes' }] }),
  ]) expect(restoreAnswers(invalid, entries).error).toBeDefined();
  expect(restoreAnswers(encodeAnswers(good, entries), [{ ...entries[0], question: 'A different question' }]).error).toBeDefined();
  expect(() => questionEntries({ questions: Array.from({ length: 17 }, () => ({ question: 'Too many' })) })).toThrow('mobile form limit');
  const multi = { ...entries[0], multiple: true };
  expect(answerValues(toggleAnswer({ selected: ['B'], text: 'custom', skipped: false }, multi, 'A'))).toEqual(['B', 'A', 'custom']);
  expect(questionPayload('id', [{ selected: [], text: '', skipped: true }, { selected: [], text: '', skipped: true }], true)).toMatchObject({ answers: [{ dismissed: true }, { dismissed: true }] });
});


test('repeated send presses admit one command and failed outcomes retain the reviewed form', async () => {
  const f = fixture();
  let finish!: (value: { status: string; result: object }) => void;
  f.runtime.run.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'authored answer');
  await fireEvent.press(screen.getByText('Review answers'));
  const before = f.drafts.get(key);
  const send = screen.getByRole('button', { name: 'Send answers' }).props.onPress;
  await act(() => { send(); send(); });
  expect(f.runtime.run).toHaveBeenCalledTimes(1);
  await act(() => { finish({ status: 'failed', result: {} }); });
  expect(f.drafts.get(key)).toEqual(before);
  expect(f.runtime.report).toHaveBeenCalled();
});

test('changed definitions preserve their old draft and require an explicit form reset', async () => {
  const f = fixture(); const screen = await render(<Requests root={f.root()} view={f.view} disabled={false} />);
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'preserve me');
  const before = f.drafts.get(key);
  f.replaceQuestion({ ...single, question: 'Changed request text' });
  await screen.rerender(<Requests root={f.root()} view={f.view} disabled={false} />);
  expect(f.drafts.get(key)).toEqual(before);
  expect(screen.getByText(/saved form no longer matches/)).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Review answers' })).toBeDisabled();
  expect(f.runtime.run).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Start this form again'));
  expect(screen.getByRole('button', { name: 'Review answers' })).not.toBeDisabled();
  expect(screen.getByLabelText('Your answer')).toHaveDisplayValue('');
});
