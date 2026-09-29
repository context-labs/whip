import { useEffect, useRef, useState } from 'react';
import { Alert } from 'react-native';
import { useIsFocused } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import type { HostBrowserDriver } from '@whip/protocol';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Button, Notice, Screen, Section, Text } from '../ui';
import { ExternalBrowserSettings } from './external-browser-settings';

function bounded(value: HostBrowserDriver) {
  if (new TextEncoder().encode(JSON.stringify(value)).byteLength > 4096) throw new Error('Browser settings exceed the mobile inspection limit.');
  return value;
}
const label = (driver: HostBrowserDriver['driver']) => driver === 'rod' ? 'Rod' : 'ChromeDP';

/** Reads and explicit host configuration CAS only. Neither action opens a browser
 * or creates a session grant. A lost CAS reply requires a fresh read. */
export function BrowserSettings() {
  const runtime = useRuntime(), state = useRuntimeState(), focused = useIsFocused();
  const online = state.ready && state.active && focused;
  const queryKey = [state.client?.runtimeID, state.client?.processEpoch, 'browser-driver'];
  const status = useQuery({ queryKey, enabled: online, queryFn: async ({ signal }) => {
    const client = state.client; if (!client || runtime.requireReady() !== client) throw new Error('Host changed. Read browser settings again.');
    const value = await client.hosts.browserDriver({ signal });
    if (runtime.requireReady() !== client) throw new Error('Host changed while reading browser settings.');
    return bounded(value);
  } });
  const [busy, setBusy] = useState(false), [uncertain, setUncertain] = useState(false), [error, setError] = useState('');
  const guard = useRef({ busy: false, uncertain: false }), request = useRef<AbortController | undefined>(undefined), visible = useRef(online);
  visible.current = online;
  useEffect(() => () => { visible.current = false; request.current?.abort(); }, []);
  useEffect(() => { if (!online) request.current?.abort(); }, [online]);
  async function save(reviewed: HostBrowserDriver, driver: HostBrowserDriver['driver']) {
    if (guard.current.busy || guard.current.uncertain || !visible.current || (reviewed.pinned && driver !== reviewed.driver)) return;
    guard.current.busy = true; setBusy(true); setError(''); const controller = new AbortController(); request.current = controller; let sent = false;
    try {
      const client = runtime.requireReady(); if (client !== state.client) throw new Error('Host changed. Read browser settings again.');
      sent = true; const result = bounded(await client.hosts.setBrowserDriver(reviewed.revision, driver, { signal: controller.signal }));
      controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while saving browser settings.');
      runtime.query.setQueryData(queryKey, result);
    } catch (cause) {
      if (sent) { guard.current.uncertain = true; setUncertain(true); }
      setError(cause instanceof Error ? cause.message : 'Browser settings could not be confirmed.');
    } finally { guard.current.busy = false; setBusy(false); if (request.current === controller) request.current = undefined; }
  }
  async function refresh() {
    if (guard.current.busy || !visible.current) return; guard.current.busy = true; setBusy(true); setError('');
    try {
      const client = runtime.requireReady(); if (client !== state.client) throw new Error('Host changed. Reopen these settings.');
      const result = await status.refetch({ throwOnError: true }); if (!result.data) throw new Error('Browser settings are unavailable.');
      if (runtime.requireReady() !== client) throw new Error('Host changed while reading browser settings.');
      guard.current.uncertain = false; setUncertain(false);
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Browser settings could not be read.'); }
    finally { guard.current.busy = false; setBusy(false); }
  }
  const value = status.data;
  return <Screen><Text variant="title">Browser automation</Text><Text muted>{state.host?.name ?? 'Selected host'}</Text>
    {!online && <Notice>Connect this host and keep this screen open to inspect its browser settings.</Notice>}
    {(error || status.error) && <Notice danger>{error || status.error!.message}</Notice>}
    {uncertain && <Notice>The change was not confirmed. Read current settings before another change. The request will not be replayed.</Notice>}
    {value && <Section title="HOST BROWSER DRIVER"><Text>Saved driver: {label(value.configured_driver)}</Text><Text>Effective driver: {label(value.driver)}</Text>
      {value.pinned && <Notice>This host process pins {label(value.driver)}. Changing that override requires restarting the host with different process settings. You can save the pinned driver for future starts.</Notice>}
      <Text muted>Desktop batches keep their captured driver. Changing the effective driver retires external Chrome connections. These settings do not open a browser, attach a tab, or grant control.</Text>
      {(['rod', 'chromedp'] as const).map(driver => <Button key={driver} label={`Use ${label(driver)}…`} variant="secondary" disabled={!online || busy || uncertain || status.isFetching || !!status.error || value.configured_driver === driver || (value.pinned && value.driver !== driver)} onPress={() => {
        Alert.alert(`Use ${label(driver)} on ${state.host?.name ?? 'this host'}?`, 'This saves the host driver. A changed effective driver retires external Chrome connections. Desktop batches keep their captured driver; no browser is opened.', [{ text: 'Keep current driver', style: 'cancel' }, { text: `Use ${label(driver)}`, onPress: () => { void save(value, driver); } }]);
      }} />)}
    </Section>}
    <Button label="Read current browser settings" variant="secondary" disabled={!online || busy || status.isFetching} onPress={() => { void refresh(); }} />
    <ExternalBrowserSettings/>
  </Screen>;
}
