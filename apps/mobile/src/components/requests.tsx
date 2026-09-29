import { useRef, useState, useSyncExternalStore } from 'react';
import { ScrollView } from 'react-native';
import type { DeepReadonly, SessionView } from '@whip/sdk/state';
import { useQuery } from '@tanstack/react-query';
import { PagedText } from './paged-text';
import type { Question as NativeQuestion, AnswerQuestionParams, HostOperation } from '@whip/protocol';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Field, Label, Notice, RowButton, Stack } from './primitives';

type QuestionRequest = NativeQuestion['request'];
export type AnswerPage = { selected: string[]; text: string; skipped: boolean };
type QuestionEntry = { question: string; multiple?: boolean; options?: readonly { label: string; description?: string; recommended?: boolean }[] | null };
type QuestionForm = { version: 1; definition: string; answers: AnswerPage[] };
const emptyAnswer = (): AnswerPage => ({ selected: [], text: '', skipped: false });
const byteLength = (value: string) => new TextEncoder().encode(value).byteLength;
export const questionDraftKey = (runtimeId: string, rootId: string, requestId: string) => JSON.stringify([runtimeId, rootId, 'question', requestId]);
export function questionEntries(question: DeepReadonly<NativeQuestion>): readonly QuestionEntry[] {
  const entries = question.request.questions;
  if (entries.length > 8 || entries.some(entry => (entry.options?.length ?? 0) > 6) || byteLength(JSON.stringify(entries)) > 32 << 10) throw new Error('This request exceeds the mobile form limit. Answer it from Whip on the host.');
  return entries;
}
export const questionDefinition = (entries: readonly QuestionEntry[]) => JSON.stringify(entries.map(entry => [entry.question, !!entry.multiple, (entry.options ?? []).map(option => option.label)]));
export function restoreAnswers(text: string, entries: readonly QuestionEntry[]): { answers: AnswerPage[]; error?: string } {
  const empty = () => entries.map(emptyAnswer);
  if (!text) return { answers: empty() };
  try {
    if (byteLength(text) > 64 << 10) throw new Error('oversize');
    const saved: QuestionForm = JSON.parse(text);
    if (!saved || saved.version !== 1 || saved.definition !== questionDefinition(entries) || !Array.isArray(saved.answers) || saved.answers.length !== entries.length) throw new Error('changed');
    const answers = saved.answers.map((answer, index) => {
      const entry = entries[index];
      if (!answer || !Array.isArray(answer.selected) || typeof answer.text !== 'string' || typeof answer.skipped !== 'boolean') throw new Error('shape');
      const allowed = new Set((entry.options ?? []).map(option => option.label));
      if (answer.selected.some(label => typeof label !== 'string' || !allowed.has(label)) || new Set(answer.selected).size !== answer.selected.length) throw new Error('option');
      if (!entry.multiple && answer.selected.length + (answer.text.trim() ? 1 : 0) > 1) throw new Error('single');
      if (answer.skipped && (answer.selected.length || answer.text)) throw new Error('skip');
      return { selected: [...answer.selected], text: answer.text, skipped: answer.skipped };
    });
    return { answers };
  } catch { return { answers: empty(), error: 'The saved form no longer matches this request or could not be read. Its text has been preserved. Start this form again only after reviewing or copying your saved text.' }; }
}
export function encodeAnswers(answers: AnswerPage[], entries: readonly QuestionEntry[]) {
  return JSON.stringify({ version: 1, definition: questionDefinition(entries), answers } satisfies QuestionForm);
}
export function toggleAnswer(answer: AnswerPage, entry: QuestionEntry, label: string): AnswerPage {
  if (!entry.options?.some(option => option.label === label)) throw new Error('This answer option is no longer available.');
  const selected = answer.selected.includes(label) ? answer.selected.filter(value => value !== label) : entry.multiple ? [...answer.selected, label] : [label];
  return { selected, text: entry.multiple ? answer.text : '', skipped: false };
}
export function answerValues(answer: AnswerPage) { return [...answer.selected, ...(answer.text.trim() ? [answer.text.trim()] : [])]; }
export function questionPayload(sessionId: string, id: string, answers: AnswerPage[]): AnswerQuestionParams {
  if (!answers.length || answers.length > 8) throw new Error('A question requires one to eight answers.');
  return { session_id: sessionId, operation_id: id, answers: answers.map(answer => ({ answer: answer.skipped ? [] : answerValues(answer), dismissed: answer.skipped || !answerValues(answer).length })) as AnswerQuestionParams['answers'] };
}
function requireCurrentRequest(view: SessionView, runtime: ReturnType<typeof useRuntime>, runtimeId: string, sessionId: string) {
  const state = runtime.getSnapshot(); const snapshot = view.getSnapshot();
  if (!state.ready || !state.active || state.client?.runtimeID !== runtimeId || snapshot.runtimeID !== runtimeId || snapshot.sessionID !== sessionId || snapshot.status !== 'live') throw new Error('Refresh this session before answering; its request state is unavailable');
  return state.client.session(sessionId);
}
export function Requests({ rootId, sessionId, view, disabled }: { rootId: string; sessionId: string; view: SessionView; disabled: boolean }) {
  const runtime = useRuntime(); const state = useRuntimeState();
  useSyncExternalStore(runtime.decisions.subscribe, runtime.decisions.getSnapshot);
  const runtimeId = state.host?.runtimeId;
  const [permissionAfter, setPermissionAfter] = useState<string>(); const [questionAfter, setQuestionAfter] = useState<string>();
  const unavailable = disabled || !state.active || !runtimeId || !state.client || view.getSnapshot().sessionID !== sessionId;
  const requests = useQuery({ queryKey: [runtimeId, sessionId, 'requests', permissionAfter, questionAfter], enabled: !unavailable, refetchInterval: unavailable ? false : 3000,
    queryFn: async ({ signal }) => {
      const session = requireCurrentRequest(view, runtime, runtimeId!, sessionId);
      const [permissions, questions] = await Promise.all([session.permissions.list({ pending_only: true, limit: 16, after: permissionAfter }, { signal }), session.questions.list({ limit: 16, after: questionAfter, pending_only: true }, { signal })]);
      const operations: HostOperation[] = []; let bytes = new TextEncoder().encode(JSON.stringify(questions)).byteLength;
      const pending = (permissions.items ?? []).filter(item => item.state === 'pending');
      for (let offset = 0; offset < pending.length; offset += 4) {
        const batch = await Promise.all(pending.slice(offset, offset + 4).map(item => session.operations.get(item.operation_id, { signal })));
        bytes += new TextEncoder().encode(JSON.stringify(batch)).byteLength;
        if (bytes > 1 << 20) throw new Error('These requests exceed the 1 MiB mobile inspection limit. Review them on the host.');
        operations.push(...batch.filter(item => item.state === 'waiting'));
      }
      return { permissions, questions, operations };
    },
  });
  const operations = requests.data?.operations ?? []; const questions = requests.data?.questions.items ?? [];
  return <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ padding: 20, gap: 24 }}>
    {requests.error && <Notice danger>{requests.error.message}</Notice>}
    {requests.isFetching && <Label muted>Refreshing requests…</Label>}
    {!operations.length && !questions.length && <Label muted>No pending requests on this page.</Label>}
    {operations.map(operation => {
      const prior = runtime.decisions.forRequest(operation.id, rootId); const blocked = unavailable || requests.isFetching || runtime.decisions.isBlocked(operation.id, rootId);
      const validate = () => { requireCurrentRequest(view, runtime, runtimeId!, sessionId); if (!requests.data?.operations.some(item => item.id === operation.id && item.session_id === sessionId)) throw new Error('Refresh this permission request before answering'); };
      const decide = async (allow: boolean) => { validate(); await runtime.decisions.decide(rootId, operation.id, sessionId, allow, validate); await requests.refetch(); await view.refresh(); };
      return <Stack key={operation.id}><Label style={{ fontWeight: '600' }}>Permission requested · {operation.capability}</Label>
        <Label muted>{sessionId === rootId ? 'Root agent' : `Agent ${sessionId}`}</Label><Label selectable>{operation.resource}</Label>
        <PagedText text={JSON.stringify(operation.arguments, null, 2)} identity={operation.id} source />
        {prior && <Notice>{prior.status}{prior.message ? ` · ${prior.message}` : ''}</Notice>}
        <Actions items={prior ? [
          { label: 'Refresh decision status', secondary: true, disabled: unavailable, onPress: () => { void runtime.decisions.check(operation.id).then(() => requests.refetch()).catch(runtime.report); } },
          ...(prior.status === 'pending' ? [{ label: 'Review a new decision', secondary: true, disabled: unavailable, onPress: () => { void runtime.decisions.clear(operation.id).catch(runtime.report); } }] : []),
        ] : [
          { label: 'Allow once', disabled: blocked, onPress: () => { void decide(true).catch(runtime.report); } },
          { label: 'Deny', disabled: blocked, secondary: true, onPress: () => { void decide(false).catch(runtime.report); } },
        ]} />
      </Stack>;
    })}
    {runtimeId && questions.map(question => <Question key={questionDraftKey(runtimeId, rootId, question.operation_id)} question={question} rootId={rootId} runtimeId={runtimeId} disabled={unavailable || requests.isFetching} view={view} />)}
    <Actions items={[
      ...(requests.data?.permissions.items?.length === 16 ? [{ label: 'Next permissions', secondary: true, onPress: () => setPermissionAfter(requests.data!.permissions.items!.at(-1)!.operation_id) }] : []),
      ...(questions.length === 16 ? [{ label: 'Next questions', secondary: true, onPress: () => setQuestionAfter(questions.at(-1)!.operation_id) }] : []),
      ...(permissionAfter || questionAfter ? [{ label: 'First requests', secondary: true, onPress: () => { setPermissionAfter(undefined); setQuestionAfter(undefined); } }] : []),
    ]} />
  </ScrollView>;
}
function Question(props: { question: DeepReadonly<NativeQuestion>; rootId: string; runtimeId: string; disabled: boolean; view: SessionView }) {
  try {
    const entries = questionEntries(props.question);
    return <QuestionForm key={questionDefinition(entries)} {...props} entries={entries} />;
  } catch (error) { return <Notice danger>{error instanceof Error ? error.message : String(error)}</Notice>; }
}
function QuestionForm({ question, entries, rootId, runtimeId, disabled, view }: { question: DeepReadonly<NativeQuestion>; entries: readonly QuestionEntry[]; rootId: string; runtimeId: string; disabled: boolean; view: SessionView }) {
  const runtime = useRuntime(); const state = useRuntimeState();
  const key = questionDraftKey(runtimeId, rootId, question.operation_id);
  const saved = runtime.draft(key);
  const { answers, error: restoreError } = restoreAnswers(saved.text, entries);
  const [index, setIndex] = useState(0); const [reviewedRevision, setReviewedRevision] = useState<string>(); const [pending, setPending] = useState(false);
  const pendingRef = useRef(false);
  const answer = answers[index]; const entry = entries[index];
  useSyncExternalStore(runtime.decisions.subscribe, runtime.decisions.getSnapshot);
  const prior = runtime.decisions.forRequest(question.operation_id, rootId);
  const blocked = disabled || pending || !!prior || !!restoreError;
  function persist(next: AnswerPage[]) { return runtime.setDraft(key, encodeAnswers(next, entries)); }
  function latestAnswers() {
    const latest = restoreAnswers(runtime.draft(key).text, entries);
    if (latest.error) throw new Error(latest.error);
    return latest.answers;
  }
  function update(transform: (answer: AnswerPage) => AnswerPage) {
    if (pendingRef.current || blocked) return;
    try { persist(latestAnswers().map((value, page) => page === index ? transform(value) : value)); setReviewedRevision(undefined); } catch (error) { runtime.report(error); }
  }
  function review() { if (blocked) return; try { const draft = persist(latestAnswers()); setReviewedRevision(draft.revision); } catch (error) { runtime.report(error); } }
  async function send() {
    if (pendingRef.current || blocked || !reviewedRevision) return;
    pendingRef.current = true; setPending(true);
    try {
      const session = requireCurrentRequest(view, runtime, runtimeId, question.session_id);
      const live = await session.questions.get(question.operation_id);
      if (live.state !== 'pending' || questionDefinition(questionEntries(live)) !== questionDefinition(entries)) throw new Error('This question changed or was answered elsewhere. Refresh the session.');
      const draft = runtime.draft(key);
      if (draft.revision !== reviewedRevision) throw new Error('The answers changed after review. Review them again before sending.');
      const restored = restoreAnswers(draft.text, entries);
      if (restored.error) throw new Error(restored.error);
      const payload = questionPayload(question.session_id, question.operation_id, restored.answers);
      await runtime.answerQuestion(rootId, payload, { draftKey: key, draftRevision: draft.revision }, structuredClone(question.request) as QuestionRequest);
      await view.refresh();
    } catch (error) { runtime.report(error); } finally { pendingRef.current = false; setPending(false); }
  }
  return <Stack><Label muted>{question.session_id === rootId ? 'Root agent' : `Agent ${question.session_id}`} · Question {index + 1} of {entries.length}</Label>
    {prior && <Notice>Answer {prior.status}. Refreshing this request does not send it again.</Notice>}
    {restoreError && <Stack><Notice danger>{restoreError}</Notice><Label selectable>{saved.text}</Label><Actions items={[{ label: 'Start this form again', secondary: true, disabled: disabled || pending || !!prior, onPress: () => { try { persist(entries.map(emptyAnswer)); setIndex(0); setReviewedRevision(undefined); } catch (error) { runtime.report(error); } } }]} /></Stack>}
    {reviewedRevision ? <Stack>{entries.map((item, page) => <Stack key={page}><Label style={{ fontWeight: '600' }}>{item.question}</Label><Label>{answers[page].skipped || !answerValues(answers[page]).length ? 'Skipped' : answerValues(answers[page]).join(', ')}</Label></Stack>)}</Stack> : <>
      <Label style={{ fontWeight: '600', fontSize: 18 }}>{entry.question}</Label>
      {(entry.options ?? []).map(option => <RowButton key={option.label} title={`${option.label}${option.recommended ? ' (Recommended)' : ''}`} detail={option.description} selected={answer.selected.includes(option.label)} disabled={blocked}
        onPress={() => update(current => toggleAnswer(current, entry, option.label))} />)}
      <Field label="Your answer" value={answer.text} onChangeText={text => update(current => ({ ...current, text, skipped: false, ...(!entry.multiple ? { selected: [] } : {}) }))} multiline editable={!blocked} placeholder="Write an answer…" />
    </>}
    <Actions items={reviewedRevision ? [
      { label: pending ? 'Sending answers…' : 'Send answers', disabled: blocked || saved.revision !== reviewedRevision, onPress: () => { void send(); } },
      { label: 'Edit answers', secondary: true, disabled: pending, onPress: () => setReviewedRevision(undefined) },
    ] : [
      { label: index < entries.length - 1 ? 'Next question' : 'Review answers', disabled: blocked, onPress: () => index < entries.length - 1 ? setIndex(index + 1) : review() },
      { label: 'Skip this question', secondary: true, disabled: blocked, onPress: () => { if (pendingRef.current || blocked) return; try { const next = latestAnswers().map((value, page) => page === index ? { selected: [], text: '', skipped: true } : value); const draft = persist(next); if (index < entries.length - 1) setIndex(index + 1); else setReviewedRevision(draft.revision); } catch (error) { runtime.report(error); } } },
      ...(index > 0 ? [{ label: 'Previous question', secondary: true, disabled: blocked, onPress: () => setIndex(index - 1) }] : []),
    ]} />
  </Stack>;
}
