import { useEffect, useRef, useState } from 'react';
import { Alert } from 'react-native';
import { router } from 'expo-router';
import * as Crypto from 'expo-crypto';
import { useQuery } from '@tanstack/react-query';
import type { Session as SessionRecord } from '@whip/protocol';
import type { Client as NativeClient } from '@whip/sdk';
import { createTraceView, type SessionView, type TraceView } from '@whip/sdk/state';
import { useSessionView, useTraceView } from '@whip/sdk/react';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { historyBoundaries, historyCut } from '../features/history-actions';
import { Actions, Field, Label, Loading, Notice, Stack } from './primitives';

/** Controls borrow the selected transcript; they never own a second history or work queue. */
export function SessionControls({ rootId, session, view }: { rootId: string; session: SessionRecord; view: SessionView }) {
  const runtime = useRuntime(), state = useRuntimeState(), observed = useSessionView(view);
  const [keep, setKeep] = useState(''), [busy, setBusy] = useState(false), [trace, setTrace] = useState(false), [uncertain, setUncertain] = useState(false); const lock = useRef(false), lifecycleUnknown = useRef(false);
  const online = state.ready && state.active && observed.status === 'live';
  const policy = useQuery({ queryKey: [state.host?.runtimeId, rootId, 'permission-mode'], enabled: online,
    queryFn: ({ signal }) => runtime.requireReady().session(rootId).permissions.policy({ signal }) });
  const disabled = !online || busy || uncertain || runtime.isBlocked(rootId, session.id);
  const lifecycle = observed.activity?.lifecycle ?? session.lifecycle;
  const historyDisabled = disabled || !!observed.activity?.active_turn;
  const boundaries = historyBoundaries(observed);
  const boundary = keep || observed.history.snapshot?.through_sequence || '0';
  async function setLifecycle(target?: 'active' | 'stopped') {
    if (lock.current || target && lifecycleUnknown.current) return; lock.current = true; setBusy(true); let sent = false;
    try {
      const client = runtime.requireReady(); if (client !== state.client) throw new Error('Host changed. Reopen these controls.');
      if (target) { sent = true; await client.session(session.id).lifecycle(target); }
      await runtime.query.refetchQueries({ queryKey: [client.runtimeID, 'session-metadata', rootId, session.id], type: 'active' }, { throwOnError: true });
      await view.refresh(); if (runtime.requireReady() !== client) throw new Error('Host changed while reading session status.');
      lifecycleUnknown.current = false; setUncertain(false);
    } catch (error) { if (sent) { lifecycleUnknown.current = true; setUncertain(true); } runtime.report(error); }
    finally { lock.current = false; setBusy(false); }
  }
  async function change(kind: 'compact' | 'fork' | 'rewind' | 'prompt' | 'automatic') {
    if (lock.current || lifecycleUnknown.current || !runtime.getSnapshot().active) return;
    lock.current = true; setBusy(true);
    try {
      const client = runtime.requireReady(); if (client !== state.client) throw new Error('Host changed. Reopen these controls.');
      if (kind === 'prompt' || kind === 'automatic') {
        if (!policy.data || policy.data.tree_id !== session.tree_id) throw new Error('Read the current permission mode before changing it.');
        await runtime.run('permissions.set_mode', { session_id: rootId, edit_id: Crypto.randomUUID(), expected_revision: policy.data.revision, mode: kind }, { rootId });
        await policy.refetch();
      } else if (kind === 'compact') {
        await runtime.run('sessions.compact', { session_id: session.id, identity: { client_id: client.clientID, request_id: Crypto.randomUUID() } }, { rootId });
      } else {
        if (kind === 'rewind' && lifecycle !== 'stopped') throw new Error('Stop this recipient before rewinding its history.');
        const cut = historyCut(session, view.getSnapshot(), boundary);
        if (kind === 'fork') {
          const result = await runtime.run('sessions.fork', { ...cut, fork_id: Crypto.randomUUID(), title: null }, { rootId });
          if (result.deleted || !result.root) throw new Error('The fork was deleted. Its original request will not be replayed.');
          if (runtime.getSnapshot().client === client) router.push({ pathname: '/session/[rootId]', params: { rootId: result.root.id, runtimeId: client.runtimeID, hostId: state.host?.id } });
        } else await runtime.run('sessions.rewind', { session_id: cut.session_id, edit_id: Crypto.randomUUID(), expected_revision: cut.expected_history_revision, observed_through: cut.observed_through, keep_through: cut.keep_through }, { rootId });
      }
    } catch (error) { runtime.report(error); }
    finally { lock.current = false; setBusy(false); }
  }
  return <Stack><Label muted>Recipient status</Label><Label>{lifecycle}</Label>
    {uncertain && <Notice>The status response was not confirmed. Read current status before another change; no request will be replayed.</Notice>}
    <Actions items={[
      { label: lifecycle === 'stopped' ? 'Resume this recipient' : 'Stop this recipient…', disabled, secondary: true, onPress: () => {
        if (lifecycle === 'stopped') void setLifecycle('active');
        else Alert.alert('Stop this recipient?', 'Active work will be cancelled. Queued inputs remain saved for an explicit resume. Stop and history rewind are separate actions.', [{ text: 'Keep active', style: 'cancel' }, { text: 'Stop recipient', style: 'destructive', onPress: () => { void setLifecycle('stopped'); } }]);
      } },
      { label: 'Read current recipient status', disabled: !online || busy, secondary: true, onPress: () => { void setLifecycle(); } },
    ]} />
    <Label muted>Permission mode for this session tree</Label><Label>{policy.data?.mode ?? 'Unavailable'}</Label>{policy.error && <Notice>{policy.error.message}</Notice>}
    <Actions items={(['prompt', 'automatic'] as const).map(mode => ({ label: mode === 'prompt' ? 'Ask before effects' : 'Use automatic mode…', secondary: true, disabled: disabled || !policy.data || policy.data.mode === mode || runtime.isBlocked(rootId), onPress: () => {
      if (mode === 'automatic') Alert.alert('Use automatic permission mode?', 'Eligible operations in this session tree may run without asking. Host restrictions and explicit-consent capabilities still apply.', [{ text: 'Keep current mode', style: 'cancel' }, { text: 'Use automatic mode', onPress: () => { void change(mode); } }]);
      else void change(mode);
    } }))} />
    <Label muted>History actions for {session.id === rootId ? 'the root agent' : 'this child agent'}</Label><Notice>These actions use the exact observed history. A lost response stays in Drafts &amp; recovery; reconnecting does not retry it.</Notice>
    <Notice>Stop this recipient before rewinding. Choose the end of a whole completed message group. Use 0 to keep no messages. Boundaries on this page: {boundaries.slice(-8).join(', ')}{boundaries.length > 8 ? ' (latest 8 shown)' : ''}.</Notice>
    <Field label="Keep through message sequence" value={keep} placeholder={observed.history.snapshot?.through_sequence ?? '0'} onChangeText={setKeep} maxLength={19} editable={!historyDisabled} keyboardType="number-pad" />
    <Actions items={[
      { label: 'Fork through this message', disabled: historyDisabled, onPress: () => { void change('fork'); } },
      { label: 'Rewind through this message…', secondary: true, disabled: historyDisabled || lifecycle !== 'stopped', onPress: () => Alert.alert('Rewind this recipient’s history?', `Messages after sequence ${boundary} will be retired from this conversation. Existing operation evidence remains.`, [{ text: 'Keep history', style: 'cancel' }, { text: 'Rewind', style: 'destructive', onPress: () => { void change('rewind'); } }]) },
      { label: 'Request compaction', secondary: true, disabled, onPress: () => { void change('compact'); } },
      { label: trace ? 'Close trace inspection' : 'Inspect execution traces', secondary: true, onPress: () => setTrace(!trace) },
    ]} />
    {trace && state.client && <TraceInspection client={state.client} rootId={rootId} active={online} />}
  </Stack>;
}
function TraceInspection({ client, rootId, active }: { client: NativeClient; rootId: string; active: boolean }) {
  const runtime = useRuntime(); const [view, setView] = useState<TraceView>();
  useEffect(() => {
    const held = createTraceView(client, rootId, { maxRows: 64, maxRoots: 16, maxBytes: 256 << 10, pollIntervalMs: 2000 }); setView(held);
    return () => { setView(undefined); void held.dispose().catch(runtime.report); };
  }, [client, rootId, runtime]);
  useEffect(() => { if (view) void (active ? view.start() : view.suspend()).catch(runtime.report); }, [view, active, runtime]);
  return view ? <TraceRows view={view} active={active} /> : <Loading />;
}
function TraceRows({ view, active }: { view: TraceView; active: boolean }) {
  const runtime = useRuntime(), snapshot = useTraceView(view);
  return <Stack><Label muted>Root-scoped execution evidence · {snapshot.status}</Label>{snapshot.error && <Notice>{snapshot.error.message}</Notice>}{snapshot.truncated && <Notice>This trace page is bounded. Open another page to inspect more evidence.</Notice>}
    {snapshot.roots.map(row => row.span && <Actions key={row.span_id} items={[{ label: `${row.span.name} · ${row.span.state}`, disabled: !active, secondary: true, onPress: () => { void view.selectTrace(row.span!.trace_id).catch(runtime.report); } }]} />)}
    {snapshot.rows.map(row => <Stack key={row.span_id}><Label>{row.span ? `${row.span.name} · ${row.span.state}` : 'Retired trace entry'}</Label><Label muted selectable>{row.session_id} / {row.source_kind} / {row.source_id}</Label>{row.span?.end_ns && <Label muted>{(BigInt(row.span.end_ns) - BigInt(row.span.start_ns)).toString()} ns</Label>}</Stack>)}
    <Actions items={[
      ...(snapshot.olderCursor ? [{ label: 'Older trace entries', secondary: true, disabled: !active, onPress: () => { void view.loadOlder().catch(runtime.report); } }] : []),
      ...(snapshot.olderRootsCursor ? [{ label: 'Older traces', secondary: true, disabled: !active, onPress: () => { void view.loadOlderRoots().catch(runtime.report); } }] : []),
      { label: 'Latest traces', secondary: true, disabled: !active, onPress: () => { void view.latest().then(() => view.latestRoots()).catch(runtime.report); } },
    ]} />
  </Stack>;
}
