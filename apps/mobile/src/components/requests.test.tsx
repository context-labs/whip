import { act, fireEvent, render } from '@testing-library/react-native';
import type { Question, AnswerQuestionParams } from '@whip/protocol';
import type { SessionView } from '@whip/sdk/state';
import { Requests, answerValues, encodeAnswers, questionDefinition, questionDraftKey, questionEntries, questionPayload, restoreAnswers, toggleAnswer } from './requests';
import type { MobileRuntime } from '../runtime/runtime';
import type { Draft } from '../runtime/storage';

let mockRuntime: MobileRuntime;
let mockRequests: unknown;
jest.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: mockRequests, refetch: async () => {}, isFetching: false }) }));
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
const entry = (question: string, labels = ['A', 'B'], multiple = false) => ({ question, options: labels.map((label, index) => ({ label, description: '', recommended: index === 0 })), multiple });
const single = { operation_id: 'question', session_id: 'child', turn_id: 'turn', cell_id: 'cell', request: { batch: true, questions: [entry('Pick one')] }, state: 'pending', answers: null, created_at: '', resolved_at: null } as unknown as Question;
function fixture(initial: Question = single) {
  const drafts = new Map<string, Draft>(); const listeners = new Set<() => void>(); let revision = 0, question = initial;
  const client = { clientID: 'client', runtimeID: 'runtime', session: () => ({ questions: { get: async () => question } }) };
  let state = { client, host: { runtimeId: 'runtime' }, ready: true, active: true, commands: [], revision }; let status = 'live';
  const decisionState = Object.freeze({ items: [], ready: true });
  const updateQuery = () => { mockRequests = { permissions: { items: [] }, operations: [], questions: { items: [question] } }; }; updateQuery();
  const runtime = {
    subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); }, getSnapshot: () => state,
    decisions: { getSnapshot: () => decisionState, subscribe: () => () => {}, forRequest: () => undefined, isBlocked: () => false },
    draft: (key: string) => drafts.get(key) ?? { text: '', revision: '' },
    setDraft: (key: string, text: string) => { const draft = { text, revision: `revision-${++revision}` }; drafts.set(key, draft); state = { ...state, revision }; listeners.forEach(listener => listener()); return draft; },
    answerQuestion: jest.fn(async () => {}), report: jest.fn(),
  };
  mockRuntime = runtime as unknown as MobileRuntime;
  const view = { getSnapshot: () => ({ runtimeID: 'runtime', sessionID: 'child', status }), refresh: jest.fn(async () => {}) } as unknown as SessionView;
  return { runtime, drafts, view, replaceQuestion(next: Question) { question = next; updateQuery(); }, answeredElsewhere() { question = { ...question, state: 'answered', answers: [{ answer: ['A'], dismissed: false }] } as unknown as Question; }, stale() { status = 'stale'; } };
}
const key = questionDraftKey('runtime', 'root', 'question');

test('recommended is not selected; single choice replaces custom text and final review is required', async () => {
  const f = fixture(); const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  expect(screen.queryByRole('button', { name: 'Send answers' })).toBeNull();
  expect(screen.getByRole('button', { name: 'A (Recommended)' })).not.toBeSelected();
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'custom');
  await fireEvent.press(screen.getByText('A (Recommended)'));
  expect(screen.getByLabelText('Your answer')).toHaveDisplayValue('');
  expect(f.runtime.answerQuestion).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Review answers'));
  expect(f.runtime.answerQuestion).not.toHaveBeenCalled();
  const revision = f.drafts.get(key)!.revision;
  await fireEvent.press(screen.getByText('Send answers'));
  expect(f.runtime.answerQuestion).toHaveBeenCalledWith('root', { session_id: 'child', operation_id: 'question', answers: [{ answer: ['A'], dismissed: false }] }, { draftKey: key, draftRevision: revision }, single.request);
});

test('batched single, multiple plus custom, and skipped pages preserve question and choice order', async () => {
  const question = { ...single, request: { batch: true, questions: [entry('Single'), entry('Multiple', ['C', 'D'], true), entry('Skipped', ['E', 'F'])] } } as unknown as Question;
  const f = fixture(question); const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  await fireEvent.press(screen.getByText('B')); await fireEvent.press(screen.getByText('Next question'));
  await fireEvent.press(screen.getByText('D')); await fireEvent.press(screen.getByText('C (Recommended)'));
  await fireEvent.changeText(screen.getByLabelText('Your answer'), '  own answer  ');
  await fireEvent.press(screen.getByText('Next question')); await fireEvent.press(screen.getByText('Skip this question'));
  expect(f.runtime.answerQuestion).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Send answers'));
  expect(f.runtime.answerQuestion).toHaveBeenCalledWith('root', { session_id: 'child', operation_id: 'question', answers: [
    { answer: ['B'], dismissed: false }, { answer: ['D', 'C', 'own answer'], dismissed: false }, { answer: [], dismissed: true },
  ] }, expect.anything(), question.request);
});

test('blank all-skipped review has a durable revision and changing it invalidates the reviewed send', async () => {
  const f = fixture(); const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  await fireEvent.press(screen.getByText('Review answers'));
  const before = f.drafts.get(key)!;
  expect(before.revision).not.toBe('');
  await act(() => { f.runtime.setDraft(key, encodeAnswers([{ selected: ['B'], text: '', skipped: false }], questionEntries(single))); });
  expect(screen.getByRole('button', { name: 'Send answers' })).toBeDisabled();
  await fireEvent.press(screen.getByText('Send answers')); expect(f.runtime.answerQuestion).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Edit answers')); await fireEvent.press(screen.getByText('Skip this question'));
  await fireEvent.press(screen.getByText('Send answers'));
  expect(f.runtime.answerQuestion).toHaveBeenCalledWith('root', { session_id: 'child', operation_id: 'question', answers: [{ answer: [], dismissed: true }] }, expect.anything(), single.request);
});

test('answered-elsewhere and stale snapshots reject a send even when rendered controls were enabled', async () => {
  for (const changed of ['answered', 'stale']) {
    const f = fixture(); const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
    await fireEvent.press(screen.getByText('A (Recommended)')); await fireEvent.press(screen.getByText('Review answers'));
    if (changed === 'answered') f.answeredElsewhere(); else f.stale();
    await fireEvent.press(screen.getByText('Send answers'));
    expect(f.runtime.answerQuestion).not.toHaveBeenCalled(); expect(f.runtime.report).toHaveBeenCalled();
    expect(f.drafts.get(key)?.text).toContain('A');
    await screen.unmount();
  }
});

test('a different request resets page/review while preserving the earlier request draft', async () => {
  const f = fixture(); const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'keep this'); await fireEvent.press(screen.getByText('Review answers'));
  const before = f.drafts.get(key);
  f.replaceQuestion({ ...single, operation_id: 'next-question' });
  await screen.rerender(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
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
  expect(() => questionEntries({ ...single, request: { batch: true, questions: Array.from({ length: 17 }, () => entry('Too many')) } } as unknown as Question)).toThrow('mobile form limit');
  const multi = { ...entries[0], multiple: true };
  expect(answerValues(toggleAnswer({ selected: ['B'], text: 'custom', skipped: false }, multi, 'A'))).toEqual(['B', 'A', 'custom']);
  expect(questionPayload('child', 'id', [{ selected: [], text: '', skipped: true }, { selected: [], text: '', skipped: true }])).toMatchObject({ answers: [{ dismissed: true }, { dismissed: true }] });
});


test('repeated send presses admit one command and failed outcomes retain the reviewed form', async () => {
  const f = fixture();
  let fail!: (error: Error) => void;
  f.runtime.answerQuestion.mockImplementation(() => new Promise((_resolve, reject) => { fail = reject; }));
  const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'authored answer');
  await fireEvent.press(screen.getByText('Review answers'));
  const before = f.drafts.get(key);
  await fireEvent.press(screen.getByText('Send answers'));
  await fireEvent.press(screen.getByText('Sending answers…'));
  expect(f.runtime.answerQuestion).toHaveBeenCalledTimes(1);
  await act(() => { fail(new Error('Decision delivery unknown')); });
  expect(f.drafts.get(key)).toEqual(before);
  expect(f.runtime.report).toHaveBeenCalled();
});

test('changed definitions preserve their old draft and require an explicit form reset', async () => {
  const f = fixture(); const screen = await render(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  await fireEvent.changeText(screen.getByLabelText('Your answer'), 'preserve me');
  const before = f.drafts.get(key);
  f.replaceQuestion({ ...single, request: { batch: true, questions: [entry('Changed request text')] } } as unknown as Question);
  await screen.rerender(<Requests rootId="root" sessionId="child" view={f.view} disabled={false} />);
  expect(f.drafts.get(key)).toEqual(before);
  expect(screen.getByText(/saved form no longer matches/)).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Review answers' })).toBeDisabled();
  expect(f.runtime.answerQuestion).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Start this form again'));
  expect(screen.getByRole('button', { name: 'Review answers' })).not.toBeDisabled();
  expect(screen.getByLabelText('Your answer')).toHaveDisplayValue('');
});
