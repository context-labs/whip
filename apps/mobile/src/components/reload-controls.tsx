import { useEffect, useRef, useState } from 'react';
import { Alert } from 'react-native';
import * as Crypto from 'expo-crypto';
import { useQuery } from '@tanstack/react-query';
import type { ReloadEdit, Session } from '@whip/protocol';
import type { SessionView } from '@whip/sdk/state';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Label, Notice, Stack } from './primitives';

function bounded(value: ReloadEdit, rootId: string, treeId: string) {
  if (value.session_id !== rootId || value.tree_id !== treeId) throw new Error('Reload receipt belongs to another tree.');
  if (new TextEncoder().encode(JSON.stringify(value)).byteLength > 1 << 20) throw new Error('Reload receipt exceeds the mobile inspection limit.');
  return value;
}
const descriptions: Record<ReloadEdit['state'], string> = {
  pending: 'Accepted, waiting for an idle tree boundary. These settings have not been applied.',
  applied: 'The captured settings were applied at the recorded configuration revision.',
  conflicted: 'The session configuration changed first. This captured reload was not applied.',
  interrupted: 'This reload was cancelled or interrupted. It was not applied.',
  unavailable: 'The captured settings could not be applied. Inspect the host before requesting another reload.',
};

/** One exact receipt read at a time. Local command records retain identity only;
 * acceptance never replaces the live session configuration or transcript. */
export function ReloadControls({ rootId, session, view, online }: { rootId: string; session: Session; view: Pick<SessionView, 'refresh'>; online: boolean }) {
  const runtime = useRuntime(), state = useRuntimeState(), client = state.client;
  const records = state.commands.filter(item => item.record.operation === 'sessions.reload' && item.record.runtimeId === client?.runtimeID && item.record.clientId === client?.clientID && item.record.sessionId === rootId);
  const [chosen, choose] = useState<string>(), [busy, setBusy] = useState(false), [cancelUnknown, setCancelUnknown] = useState(false), [error, setError] = useState('');
  const selected = records.find(item => item.record.commandId === chosen) ?? records.at(-1);
  const editId = selected?.record.commandId;
  const rootSelected = session.id === rootId && session.parent_id === null;
  const lock = useRef(false), cancellation = useRef(false), lifetime = useRef<AbortController | undefined>(undefined);
  const scope = JSON.stringify([client?.runtimeID, client?.processEpoch, rootId, session.id]);
  const currentScope = useRef(scope); currentScope.current = scope;
  useEffect(() => { const controller = new AbortController(); lifetime.current = controller; return () => { controller.abort(); if (lifetime.current === controller) lifetime.current = undefined; }; }, [client, scope, online]);
  useEffect(() => { cancellation.current = false; setCancelUnknown(false); setError(''); }, [editId, client]);
  const queryKey = [client?.runtimeID, client?.processEpoch, rootId, 'captured-reload', editId];
  const current = () => {
    if (!online || !lifetime.current || lifetime.current.signal.aborted || currentScope.current !== scope || !client || runtime.requireReady() !== client) throw new Error('Host or recipient changed. Reopen these reload controls.');
    return client;
  };
  const receipt = useQuery({ queryKey, enabled: online && !!client && !!editId,
    queryFn: async ({ signal }) => {
      const owner = current(); if (!editId) throw new Error('Select a reload request first.');
      const result = bounded(await owner.session(rootId).reloads.get(editId, { signal }), rootId, session.tree_id);
      current(); return result;
    } });
  async function refreshSession() {
    await runtime.query.refetchQueries({ queryKey: [client?.runtimeID, 'session-metadata', rootId, rootId], type: 'active' }, { throwOnError: true });
    await view.refresh();
  }
  async function request(expectedRevision: string) {
    if (lock.current || !rootSelected) return; lock.current = true; setBusy(true); setError('');
    try {
      current();
      const id = Crypto.randomUUID(); choose(id);
      const result = bounded(await runtime.run('sessions.reload', { session_id: rootId, expected_revision: expectedRevision, edit_id: id }, { rootId }), rootId, session.tree_id);
      current(); runtime.query.setQueryData([client?.runtimeID, client?.processEpoch, rootId, 'captured-reload', id], result);
      if (result.state === 'applied') await refreshSession();
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Reload acceptance could not be confirmed. Check the saved request.'); }
    finally { lock.current = false; setBusy(false); }
  }
  async function inspect(cancel = false, forget = false) {
    if (lock.current || !selected || cancel && (!rootSelected || cancellation.current)) return;
    lock.current = true; setBusy(true); setError(''); let sent = false;
    try {
      const owner = current(), signal = lifetime.current!.signal;
      // This performs exact SDK matching when originals remain, identity-only
      // inspection after restart. Reading never sends a replacement reload.
      const checked = await runtime.checkCommand(selected); current();
      if (checked.status !== 'accepted' && checked.status !== 'identity_only') throw new Error(checked.message || 'The captured request could not be verified. Keep its recovery record.');
      let result = bounded(await owner.session(rootId).reloads.get(selected.record.commandId, { signal }), rootId, session.tree_id); current();
      if (cancel && result.state === 'pending') {
        sent = true; result = bounded(await owner.session(rootId).reloads.cancel(selected.record.commandId, { signal }), rootId, session.tree_id); current();
      }
      runtime.query.setQueryData(queryKey, result);
      cancellation.current = false; setCancelUnknown(false);
      if (result.state === 'applied') await refreshSession();
      if (forget) {
        if (result.state === 'pending') throw new Error('This reload is still pending. Keep its local recovery record.');
        await runtime.forgetCommand(selected);
      }
    } catch (cause) {
      if (sent) { cancellation.current = true; setCancelUnknown(true); }
      setError(cause instanceof Error ? cause.message : 'Reload receipt could not be read.');
    } finally { lock.current = false; setBusy(false); }
  }
  const value = receipt.data, disabled = !online || busy || !client;
  return <Stack><Label muted>Captured host settings reload</Label>
    <Notice>A reload captures current inherited host settings once and waits for an idle tree boundary. Existing model choices, explicit overrides, history, children and REPL state remain intact. Acceptance is not application.</Notice>
    {records.length > 1 && <Actions items={records.map(item => ({ label: `${item.record.commandId}${item.record.commandId === editId ? ' · selected' : ''}`, secondary: true, disabled: busy, onPress: () => choose(item.record.commandId) }))} />}
    {selected && <><Label selectable>Request {selected.record.commandId} · delivery {selected.status}</Label>
      {!value && <Notice>{receipt.isFetching ? 'Reading the captured reload receipt…' : 'The host outcome has not been read. No applied configuration is inferred from delivery status.'}</Notice>}
      {selected.message && <Notice>{selected.message}</Notice>}
    </>}
    {value && <><Label>Reload: {value.state}</Label><Notice>{descriptions[value.state]}</Notice><Label selectable muted>Host revision {value.host_revision}</Label><Label muted>Expected configuration {value.expected_revision}{value.revision !== null ? ` · applied configuration ${value.revision}` : ''}</Label>
      <Label muted>Captured model: {value.configuration.model.provider} / {value.configuration.model.name}</Label>
    </>}
    {(error || receipt.error) && <Notice>{error || receipt.error!.message}</Notice>}
    {cancelUnknown && <Notice>Cancellation was not confirmed. Read this exact receipt before another cancellation; no request is replayed automatically.</Notice>}
    <Actions items={[
      { label: 'Capture current host settings…', secondary: true, disabled: disabled || !rootSelected || runtime.isBlocked(rootId) || !!selected && (!value || !!receipt.error || receipt.isFetching || value.state === 'pending'), onPress: () => {
        const expected = session.config_revision;
        Alert.alert('Capture host settings for this root?', `This saves one reload request against configuration ${expected}. It may remain pending while the tree is busy. Later host edits will not change the captured request.`, [{ text: 'Keep current settings', style: 'cancel' }, { text: 'Capture settings', onPress: () => { void request(expected); } }]);
      } },
      ...(selected ? [{ label: 'Check captured reload', secondary: true, disabled, onPress: () => { void inspect(); } }] : []),
      ...(selected && value && value.state !== 'pending' ? [{ label: 'Clear inspected reload record', secondary: true, disabled, onPress: () => { void inspect(false, true); } }] : []),
      ...(selected && value?.state === 'pending' ? [{ label: 'Cancel this captured reload…', secondary: true, disabled: disabled || !rootSelected || cancelUnknown, onPress: () => Alert.alert('Cancel this captured reload?', 'Only this pending settings reload is cancelled. Running work and history remain unchanged. A reload already applied cannot be undone here.', [{ text: 'Keep reload', style: 'cancel' }, { text: 'Cancel reload', onPress: () => { void inspect(true); } }]) }] : []),
    ]} />
  </Stack>;
}
