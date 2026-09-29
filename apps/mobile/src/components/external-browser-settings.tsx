import { useEffect, useRef, useState } from 'react';
import { Alert } from 'react-native';
import { useIsFocused } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import type { Client, ExternalBrowserStatus } from '@whip/sdk';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Button, ChoiceGroup, Notice, Section, SwitchRow, Text, TextField } from '../ui';

type Configuration = ExternalBrowserStatus['configuration'];
const modes: { value: Configuration['mode']; label: string }[] = [
  { value: 'disabled', label: 'Disabled' }, { value: 'live', label: 'Existing Chrome' }, { value: 'dedicated', label: 'Dedicated Chrome' }, { value: 'headless', label: 'Headless Chrome' }, { value: 'extension', label: 'Chrome extension' },
];
function bounded(value: ExternalBrowserStatus) {
  if (new TextEncoder().encode(JSON.stringify(value)).byteLength > 32 << 10) throw new Error('External Chrome settings exceed the mobile inspection limit.');
  return value;
}

export function ExternalBrowserSettings() {
  const state = useRuntimeState(), focused = useIsFocused();
  return state.client ? <Settings key={`${state.client.runtimeID}:${state.client.processEpoch}`} client={state.client} online={state.ready && state.active && focused}/> : <Notice>Connect a host to inspect external Chrome settings.</Notice>;
}
function Settings({ client, online }: { client: Client; online: boolean }) {
  const runtime = useRuntime(), queryKey = [client.runtimeID, client.processEpoch, 'external-browser'];
  const status = useQuery({ queryKey, enabled: online, retry: false, gcTime: 0, queryFn: async ({ signal }) => {
    if (runtime.requireReady() !== client) throw new Error('Host changed. Reopen browser settings.');
    const value = bounded(await client.hosts.externalBrowser({ signal }));
    if (runtime.requireReady() !== client) throw new Error('Host changed while reading browser settings.');
    return value;
  } });
  return <Section title="EXTERNAL CHROME"><Text>Saving selects the execution host's browser mode. It does not launch Chrome, open a relay, or grant control. Changed settings retire external connections; Desktop offered tabs stay separate.</Text>
    {status.error && <Notice danger>{status.error.message}</Notice>}
    {status.data ? <Form client={client} online={online} current={status.data} read={async () => { const result = await status.refetch({ throwOnError: true }); if (!result.data) throw new Error('External Chrome settings are unavailable.'); return result.data; }} publish={value => runtime.query.setQueryData(queryKey, value)}/> : <Button label="Read external Chrome settings" disabled={!online || status.isFetching} variant="secondary" onPress={() => { void status.refetch(); }}/>}
  </Section>;
}
function Form({ client, online, current, read, publish }: { client: Client; online: boolean; current: ExternalBrowserStatus; read(): Promise<ExternalBrowserStatus>; publish(value: ExternalBrowserStatus): void }) {
  const runtime = useRuntime(), [base, setBase] = useState(current), [draft, setDraft] = useState(current.configuration);
  const [busy, setBusy] = useState(false), [review, setReview] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
  const guard = useRef({ busy: false, review: false }), request = useRef<AbortController | undefined>(undefined), visible = useRef(online);
  visible.current = online;
  useEffect(() => { visible.current = online; return () => { visible.current = false; request.current?.abort(); }; }, [client]);
  useEffect(() => { if (!online) request.current?.abort(); }, [online]);
  const dirty = JSON.stringify(base.configuration) !== JSON.stringify(draft), stale = base.revision !== current.revision;
  function edit(patch: Partial<Configuration>) { if (visible.current && !guard.current.busy) { setDraft(value => ({ ...value, ...patch })); setNotice(''); } }
  async function save(reviewed: ExternalBrowserStatus, configuration: Configuration) {
    if (!visible.current || guard.current.busy || guard.current.review || stale) return;
    guard.current.busy = true; setBusy(true); setError(''); setNotice(''); const controller = new AbortController(); request.current = controller;
    try {
      if (runtime.requireReady() !== client) throw new Error('Host changed. Reopen browser settings.');
      guard.current.review = true; setReview(true);
      const value = bounded(await client.hosts.setExternalBrowser(reviewed.revision, configuration, { signal: controller.signal }));
      controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while saving browser settings.');
      setBase(value); setDraft(value.configuration); publish(value); guard.current.review = false; setReview(false); setNotice('External Chrome settings saved. The next operation still needs permission.');
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'External Chrome settings could not be confirmed.'); }
    finally { guard.current.busy = false; setBusy(false); if (request.current === controller) request.current = undefined; }
  }
  async function refresh() {
    if (!visible.current || guard.current.busy) return;
    guard.current.busy = true; setBusy(true); setError(''); const controller = new AbortController(); request.current = controller;
    try {
      if (runtime.requireReady() !== client) throw new Error('Host changed. Reopen browser settings.');
      const value = await read(); controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while reading browser settings.');
      setBase(value); setDraft(value.configuration); guard.current.review = false; setReview(false); setNotice('Current settings loaded. No change was replayed.');
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'External Chrome settings could not be read.'); }
    finally { guard.current.busy = false; setBusy(false); if (request.current === controller) request.current = undefined; }
  }
  return <Section>
    {!online && <Notice>Keep this host connected and this screen open to change browser settings.</Notice>}
    <ChoiceGroup label="External browser mode" value={draft.mode} options={modes} disabled={!online || busy} onChange={mode => edit({ mode })}/>
    {(draft.mode === 'dedicated' || draft.mode === 'headless') && <TextField label="Chrome executable" value={draft.executable} maxLength={4096} autoCapitalize="none" autoCorrect={false} editable={online && !busy} hint="Absolute path on the execution host. No executable is downloaded or discovered." onChangeText={executable => edit({ executable })}/>}
    {draft.mode === 'live' && <><TextField label="Live Chrome endpoint" value={draft.live_endpoint} maxLength={4096} autoCapitalize="none" autoCorrect={false} editable={online && !busy} hint="Literal loopback HTTP or browser WebSocket endpoint with a port; use either endpoint or profile." onChangeText={live_endpoint => edit({ live_endpoint, live_profile: '' })}/><TextField label="Live Chrome profile" value={draft.live_profile} maxLength={4096} autoCapitalize="none" autoCorrect={false} editable={online && !busy} hint="Absolute profile directory on the host. Only this profile is inspected." onChangeText={live_profile => edit({ live_profile, live_endpoint: '' })}/></>}
    {draft.mode === 'extension' && <Text>Run whipcode browser install on the host and load the printed folder in Chrome. After approving a named operation, pin the desired tab. Reconnect requires a new human pin.</Text>}
    {draft.mode !== 'disabled' && draft.mode !== 'live' && <><SwitchRow label="Allow private browser destinations" value={draft.allow_private_urls} disabled={!online || busy} onChange={allow_private_urls => edit({ allow_private_urls })}/><Text muted>Metadata destinations remain blocked. Page JavaScript is not a network sandbox.</Text></>}
    <Text>Effective driver: {current.driver}{current.driver_pinned ? ' (pinned by this host process)' : ''}</Text>
    {(review || stale) && <Notice>Read current settings before another change. A previous request may have arrived; it will not be replayed.</Notice>}
    {error !== '' && <Notice danger>{error}</Notice>}{notice !== '' && <Notice>{notice}</Notice>}
    <Button label="Save external Chrome settings…" disabled={!online || busy || review || stale || !dirty} onPress={() => {
      const reviewed = base, configuration = { ...draft };
      Alert.alert('Save external Chrome settings?', 'This changes the selected host and retires existing external connections. It does not open Chrome or grant control.', [{ text: 'Keep draft', style: 'cancel' }, { text: 'Save settings', onPress: () => { void save(reviewed, configuration); } }]);
    }}/>
    <Button label="Discard edits and read external Chrome settings" variant="secondary" disabled={!online || busy} onPress={() => { void refresh(); }}/>
  </Section>;
}
