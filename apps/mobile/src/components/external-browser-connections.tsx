import { useEffect, useRef, useState } from 'react';
import { Alert } from 'react-native';
import { useQuery } from '@tanstack/react-query';
import type { Client, ExternalBrowserSession } from '@whip/sdk';
import type { Session } from '@whip/protocol';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Button, Notice, Section, Text } from '../ui';

export function ExternalBrowserConnections({ rootId, session, online }: { rootId: string; session: Session; online: boolean }) {
  const state = useRuntimeState();
  return state.client ? <Connections key={`${state.client.runtimeID}:${state.client.processEpoch}:${rootId}:${session.id}`} client={state.client} rootId={rootId} session={session} online={online && state.ready && state.active}/> : null;
}
function Connections({ client, rootId, session, online }: { client: Client; rootId: string; session: Session; online: boolean }) {
  const runtime = useRuntime();
  const query = useQuery({ queryKey: [client.runtimeID, client.processEpoch, rootId, session.id, 'external-browser-connections'], enabled: online, retry: false, gcTime: 0, queryFn: async ({ signal }) => {
    if (runtime.requireReady() !== client) throw new Error('Host changed. Reopen connection controls.');
    const value = await client.hosts.externalBrowserSessions(session.id, { signal });
    if (runtime.requireReady() !== client) throw new Error('Host changed while reading connections.');
    if (value.items.some(item => item.root_id !== rootId)) throw new Error('Browser metadata belongs to another root.');
    if (value.items.length > 4 || new TextEncoder().encode(JSON.stringify(value)).byteLength > 32 << 10) throw new Error('Browser metadata exceeds the mobile inspection limit.');
    return value;
  } });
  const [busy, setBusy] = useState(false), [review, setReview] = useState(false), [error, setError] = useState('');
  const guard = useRef({ busy: false, review: false }), request = useRef<AbortController | undefined>(undefined), visible = useRef(online);
  visible.current = online;
  useEffect(() => { visible.current = online; return () => { visible.current = false; request.current?.abort(); }; }, [client]);
  useEffect(() => { if (!online) request.current?.abort(); }, [online]);
  const root = rootId === session.id && session.parent_id === null, active = session.lifecycle === 'active';
  async function change(item: ExternalBrowserSession, reconnect: boolean) {
    if (!visible.current || guard.current.busy || guard.current.review || !root || !active) return;
    guard.current.busy = true; setBusy(true); setError(''); const controller = new AbortController(); request.current = controller;
    try {
      if (runtime.requireReady() !== client) throw new Error('Host changed. Reopen connection controls.');
      guard.current.review = true; setReview(true);
      if (reconnect) await client.hosts.reconnectExternalBrowser(rootId, item.name, item.generation, { signal: controller.signal });
      else await client.hosts.disconnectExternalBrowser(rootId, item.name, item.generation, { signal: controller.signal });
      controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while changing the connection.');
      await query.refetch({ throwOnError: true }); controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while reading connections.');
      guard.current.review = false; setReview(false);
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Connection change could not be confirmed.'); }
    finally { guard.current.busy = false; setBusy(false); if (request.current === controller) request.current = undefined; }
  }
  async function refresh() {
    if (!visible.current || guard.current.busy) return;
    guard.current.busy = true; setBusy(true); setError(''); const controller = new AbortController(); request.current = controller;
    try {
      if (runtime.requireReady() !== client) throw new Error('Host changed. Reopen connection controls.');
      await query.refetch({ throwOnError: true }); controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed while reading connections.');
      guard.current.review = false; setReview(false);
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Connections could not be read.'); }
    finally { guard.current.busy = false; setBusy(false); if (request.current === controller) request.current = undefined; }
  }
  function confirm(item: ExternalBrowserSession, reconnect: boolean) {
    Alert.alert(`${reconnect ? 'Reconnect' : 'Disconnect'} ${item.name}?`, `This targets generation ${item.generation}. ${reconnect ? 'A new generation still needs permission; Chrome is not opened and no work is replayed.' : 'Active work on this generation is cancelled; human Chrome remains open.'}`, [{ text: 'Keep connection', style: 'cancel' }, { text: reconnect ? 'Reconnect' : 'Disconnect', onPress: () => { void change(item, reconnect); } }]);
  }
  return <Section title="NAMED EXTERNAL BROWSER CONNECTIONS"><Text muted>Status is passive. Reconnect prepares a new generation without opening Chrome or granting control.</Text>
    {!root && <Notice>Open the root agent to reconnect or disconnect. Child access still requires an exact delegated grant.</Notice>}
    {(error || query.error) && <Notice danger>{error || query.error!.message}</Notice>}
    {review && <Notice>The connection change was not confirmed. Read current generations before another action; no request will be replayed.</Notice>}
    {query.data?.items.length === 0 && <Text>No external connection has been prepared for this root.</Text>}
    {query.data?.items.map(item => <Section key={item.generation} title={item.name}><Text>{item.mode} · {item.driver} · {item.state}</Text><Text selectable>Generation: {item.generation}</Text><Text selectable>Resource: {item.resource}</Text>
      {root && <><Button label={`Reconnect ${item.name}…`} disabled={!online || busy || review || query.isFetching || !active} onPress={() => confirm(item, true)}/><Button label={`Disconnect ${item.name}…`} variant="secondary" disabled={!online || busy || review || query.isFetching || !active || item.state === 'ended'} onPress={() => confirm(item, false)}/></>}
    </Section>)}
    <Button label="Read current external connections" variant="secondary" disabled={!online || busy || query.isFetching} onPress={() => { void refresh(); }}/>
  </Section>;
}
