import { useRef, useState, useSyncExternalStore } from 'react';
import { ScrollView } from 'react-native';
import type { DeepReadonly, SessionView } from '@whip/sdk/state';
import type { LifecycleEvent, QuestionAnswerParams, RootSnapshot } from '@whip/protocol';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Field, Label, Notice, RowButton, Stack } from './primitives';

export type AnswerPage = { selected: string[]; text: string; skipped: boolean };
type QuestionEntry = { question: string; multiple?: boolean; options?: readonly { label: string; description?: string; recommended?: boolean }[] | null };
type QuestionForm = { version: 1; definition: string; answers: AnswerPage[] };
const emptyAnswer = (): AnswerPage => ({ selected: [], text: '', skipped: false });
const byteLength = (value: string) => new TextEncoder().encode(value).byteLength;
export const questionDraftKey = (runtimeId: string, rootId: string, requestId: string) => JSON.stringify([runtimeId, rootId, 'question', requestId]);
export function questionEntries(question: DeepReadonly<LifecycleEvent>): readonly QuestionEntry[] {
  const entries = question.questions?.length ? question.questions : [{ question: question.question ?? '', options: question.options, multiple: question.multiple }];
  if (entries.length > 16 || entries.some(entry => (entry.options?.length ?? 0) > 64) || byteLength(JSON.stringify(entries)) > 32 << 10) throw new Error('This request exceeds the mobile form limit. Answer it from Whip on the host.');
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
export function questionPayload(id: string, answers: AnswerPage[], batched: boolean): QuestionAnswerParams {
  const entries = answers.map(answer => ({ answer: answer.skipped ? [] : answerValues(answer), dismissed: answer.skipped || !answerValues(answer).length }));
  return batched ? { id, answer: [], dismissed: false, answers: entries } : { id, ...entries[0] };
}
function requireCurrentRequest(view: SessionView, runtime: ReturnType<typeof useRuntime>, runtimeId: string, rootId: string) {
  const state = runtime.getSnapshot();
  const snapshot = view.getSnapshot();
  if (!state.ready || !state.active || state.host?.runtimeId !== runtimeId || state.client !== view.session.client || view.session.rootId !== rootId || snapshot.status !== 'live' || snapshot.root?.root_id !== rootId) throw new Error('Refresh this session before answering; its request state is unavailable.');
  return snapshot.root;
}
export function Requests({ root, view, disabled }: { root: DeepReadonly<RootSnapshot>; view: SessionView; disabled: boolean }) {
  const runtime = useRuntime(); const state = useRuntimeState();
  useSyncExternalStore(runtime.decisions.subscribe, runtime.decisions.getSnapshot);
  const runtimeId = state.host?.runtimeId;
  const permissions = root.permissions?.filter(p => p.status === 'pending') ?? [];
  const questions = root.questions?.filter(q => q.question_id) ?? [];
  const unavailable = disabled || !runtimeId || state.client !== view.session.client;
  return <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ padding: 20, gap: 24 }}>
    {!permissions.length && !questions.length && <Label muted>No pending requests in this session.</Label>}
    {permissions.map(permission => {
      const prior = runtime.decisions.forRequest(permission.id, root.root_id);
      const blocked = unavailable || runtime.decisions.isBlocked(permission.id, root.root_id);
      const validate = () => {
        const live = requireCurrentRequest(view, runtime, runtimeId!, root.root_id).permissions?.find(item => item.id === permission.id);
        if (!live || live.status !== 'pending' || live.agent_id !== permission.agent_id || live.operation !== permission.operation || live.command !== permission.command || live.canonical_path !== permission.canonical_path) throw new Error('This permission request changed or was answered elsewhere. Refresh the session.');
      };
      const decide = async (allow: boolean) => {
        validate();
        await runtime.decisions.decide(root.root_id, permission.id, permission.agent_id, allow, validate);
        if (runtime.getSnapshot().client === view.session.client) await view.refresh();
      };
      return <Stack key={permission.id}><Label style={{ fontWeight: '600' }}>Permission requested · {permission.operation}</Label>
        <Label muted>{permission.agent_id === root.root_id ? 'Root agent' : `Agent ${permission.agent_id}`}</Label>
        <Label selectable>{permission.command || permission.canonical_path}</Label>
        {permission.rule && <Label muted>Rule: {permission.rule}</Label>}
        {prior && <Notice>{prior.status}{prior.message ? ` · ${prior.message}` : ''}</Notice>}
        <Actions items={prior ? [
          { label: 'Refresh decision status', secondary: true, disabled: unavailable, onPress: () => { void runtime.decisions.check(prior.record.commandId).then(() => { if (runtime.getSnapshot().client === view.session.client) return view.refresh(); }).catch(runtime.report); } },
          ...(['failed', 'cancelled', 'interrupted', 'not_found'].includes(prior.status) ? [{ label: 'Review a new decision', secondary: true, disabled: unavailable, onPress: () => { try { validate(); void runtime.decisions.clear(prior.record.commandId).catch(runtime.report); } catch (error) { runtime.report(error); } } }] : []),
        ] : [
          { label: 'Allow once', disabled: blocked, onPress: () => { void decide(true).catch(runtime.report); } },
          { label: 'Deny', disabled: blocked, secondary: true, onPress: () => { void decide(false).catch(runtime.report); } },
        ]} />
      </Stack>;
    })}
    {runtimeId && questions.map(question => <Question key={questionDraftKey(runtimeId, root.root_id, question.question_id!)} question={question} rootId={root.root_id} runtimeId={runtimeId} disabled={unavailable} view={view} />)}
  </ScrollView>;
}
function Question(props: { question: DeepReadonly<LifecycleEvent>; rootId: string; runtimeId: string; disabled: boolean; view: SessionView }) {
  try {
    const entries = questionEntries(props.question);
    return <QuestionForm key={questionDefinition(entries)} {...props} entries={entries} />;
  } catch (error) { return <Notice danger>{error instanceof Error ? error.message : String(error)}</Notice>; }
}
function QuestionForm({ question, entries, rootId, runtimeId, disabled, view }: { question: DeepReadonly<LifecycleEvent>; entries: readonly QuestionEntry[]; rootId: string; runtimeId: string; disabled: boolean; view: SessionView }) {
  const runtime = useRuntime(); const state = useRuntimeState();
  const key = questionDraftKey(runtimeId, rootId, question.question_id!);
  const saved = runtime.draft(key);
  const { answers, error: restoreError } = restoreAnswers(saved.text, entries);
  const [index, setIndex] = useState(0); const [reviewedRevision, setReviewedRevision] = useState<string>(); const [pending, setPending] = useState(false);
  const pendingRef = useRef(false);
  const answer = answers[index]; const entry = entries[index];
  const prior = state.commands.find(c => c.record.runtimeId === runtimeId && c.record.clientId === view.session.client.clientId && c.record.rootId === rootId && c.intent?.requestId === question.question_id && !['failed', 'cancelled', 'interrupted', 'not_found'].includes(c.status));
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
      const live = requireCurrentRequest(view, runtime, runtimeId, rootId).questions?.find(item => item.question_id === question.question_id);
      if (!live || questionDefinition(questionEntries(live)) !== questionDefinition(entries)) throw new Error('This question changed or was answered elsewhere. Refresh the session.');
      const draft = runtime.draft(key);
      if (draft.revision !== reviewedRevision) throw new Error('The answers changed after review. Review them again before sending.');
      const restored = restoreAnswers(draft.text, entries);
      if (restored.error) throw new Error(restored.error);
      const payload = questionPayload(question.question_id!, restored.answers, !!question.questions?.length);
      const result = await runtime.run('question.answer', payload, { rootId, intent: { requestId: question.question_id, agentId: question.agent_id ?? rootId, draftKey: key, draftRevision: draft.revision } });
      if (result.status !== 'succeeded') throw new Error(result.failure?.message ?? `Answer ${result.status}`);
      if (runtime.getSnapshot().client === view.session.client) await view.refresh();
    } catch (error) { runtime.report(error); } finally { pendingRef.current = false; setPending(false); }
  }
  return <Stack><Label muted>{(question.agent_id ?? rootId) === rootId ? 'Root agent' : `Agent ${question.agent_id ?? rootId}`} · Question {index + 1} of {entries.length}</Label>
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
