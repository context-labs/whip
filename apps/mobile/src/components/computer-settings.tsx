import { useEffect, useRef, useState } from 'react';
import { Alert } from 'react-native';
import { useIsFocused } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import type { ComputerStatus } from '@whip/protocol';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Button, Notice, Screen, Section, Text } from '../ui';
function bounded(value: ComputerStatus) {
  if (new TextEncoder().encode(JSON.stringify(value)).byteLength > 64 << 10) throw new Error('Computer status exceeds the mobile inspection limit.');
  return value;
}
/** Host status is a read. Publishing a bundled helper is a separate explicit
 * configuration CAS; neither path enables control or starts a helper. */
export function ComputerSettings() {
  const runtime = useRuntime(), state = useRuntimeState(), focused = useIsFocused();
  const online = state.ready && state.active && focused;
  const queryKey = [state.client?.runtimeID, state.client?.processEpoch, 'computer-status'];
  const status = useQuery({ queryKey, enabled: online, queryFn: async ({ signal }) => {
    const client = state.client; if (!client || runtime.requireReady() !== client) throw new Error('Host changed. Read computer status again.');
    const value = await client.computerStatus({ signal }); if (runtime.requireReady() !== client) throw new Error('Host changed while reading computer status.');
    return bounded(value);
  } });
  const [busy, setBusy] = useState(false), [uncertain, setUncertain] = useState(false), [error, setError] = useState('');
  const guard = useRef({ busy: false, uncertain: false }), request = useRef<AbortController | undefined>(undefined), visible = useRef(online);
  visible.current = online;
  useEffect(() => () => { visible.current = false; request.current?.abort(); }, []);
  useEffect(() => { if (!online) request.current?.abort(); }, [online]);
  const value = status.data;
  const available = value?.platform_supported && value.bundled_available && !value.configuration.helper_executable;
  async function publish(reviewed: ComputerStatus) {
    if (guard.current.busy || guard.current.uncertain || !visible.current) return;
    guard.current.busy = true; setBusy(true); setError(''); const controller = new AbortController(); request.current = controller; let sent = false;
    try {
      const client = runtime.requireReady(); if (client !== state.client || !focused) throw new Error('Host changed. Read computer status again.');
      sent = true; const result = bounded(await client.useBundledComputer(reviewed.revision, { signal: controller.signal }));
      controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while publishing the helper.');
      runtime.query.setQueryData(queryKey, result);
    } catch (cause) {
      if (sent) { guard.current.uncertain = true; setUncertain(true); }
      setError(cause instanceof Error ? cause.message : 'Helper publication could not be confirmed.');
    } finally { guard.current.busy = false; setBusy(false); if (request.current === controller) request.current = undefined; }
  }
  async function refresh() {
    if (guard.current.busy || !visible.current) return; guard.current.busy = true; setBusy(true); setError('');
    try {
      const client = runtime.requireReady(); if (client !== state.client) throw new Error('Host changed. Reopen these settings.');
      const result = await status.refetch({ throwOnError: true }); if (!result.data) throw new Error('Computer status is unavailable.');
      if (runtime.requireReady() !== client) throw new Error('Host changed while reading computer status.');
      guard.current.uncertain = false; setUncertain(false);
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Computer status could not be read.'); }
    finally { guard.current.busy = false; setBusy(false); }
  }
  return <Screen><Text variant="title">Computer setup</Text><Text muted>{state.host?.name ?? 'Selected host'}</Text>
    {!online && <Notice>Connect this host and keep this screen open to inspect its computer controls.</Notice>}
    {(error || status.error) && <Notice danger>{error || status.error!.message}</Notice>}
    {uncertain && <Notice>Publication was not confirmed. Read current status before another action. The request will not be replayed.</Notice>}
    {value && <><Section title="HOST STATUS"><Text>{value.configuration.enabled ? 'Computer control enabled' : 'Computer control disabled'}</Text><Text muted>Helper connection: {value.state}</Text>
      {!value.platform_supported && <Notice>Native computer control is not supported on this host’s platform.</Notice>}
      <Text selectable>{value.configuration.helper_executable || 'No helper configured'}</Text><Text muted>{value.bundled_available ? 'This host includes a bundled helper.' : 'This host does not include a bundled helper.'}</Text>
      <Text muted>Checking status does not start or install anything. Host availability and app policy remain separate from a session’s permission grants.</Text>
    </Section><Section title="APP POLICY"><Text muted>Managed on the host. Publishing a helper keeps this policy unchanged.</Text><Text>Unlisted apps: {value.configuration.default_deny ? 'explicit consent required' : 'eligible under host policy'}</Text><Text>Allowed apps: {value.configuration.allow.join(', ') || 'None saved'}</Text><Text>Denied apps: {value.configuration.deny.join(', ') || 'None saved'}</Text></Section></>}
    <Button label="Read current computer status" variant="secondary" disabled={!online || busy || status.isFetching} onPress={() => { void refresh(); }} />
    <Button label="Use bundled helper…" disabled={!online || busy || uncertain || status.isFetching || !!status.error || !available} onPress={() => {
      if (!value) return;
      Alert.alert(`Use the bundled helper on ${state.host?.name ?? 'this host'}?`, `This publishes the bundled helper into this host’s private runtime directory. Computer control stays ${value.configuration.enabled ? 'enabled' : 'disabled'}; app policy stays unchanged. This action does not start the helper.`, [{ text: 'Keep current setup', style: 'cancel' }, { text: 'Use bundled helper', onPress: () => { void publish(value); } }]);
    }} />
  </Screen>;
}
