import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { ExternalBrowserSession } from '@whip/sdk';
import { Button } from '@whip/ui';
import { ErrorNotice } from '../error-feedback';
import { ExternalBrowserSettings } from '../settings/external-browser';
import { Empty, QueryFeedback, Section, type InspectorProps } from './shared';

export function ExternalBrowser(props: InspectorProps) {
  return <><ExternalBrowserSettings client={props.client} enabled={props.connected}/><ExternalBrowserConnections key={`${props.client.runtimeID}:${props.client.processEpoch}:${props.session.id}`} {...props}/></>;
}
export function ExternalBrowserConnections(props: Pick<InspectorProps, 'client' | 'session' | 'rootId' | 'selected' | 'connected'>) {
  const query = useQuery({ queryKey: ['external-browser-sessions', props.client.runtimeID, props.client.processEpoch, props.rootId, props.session.id], enabled: props.connected, gcTime: 0, retry: false,
    queryFn: async ({ signal }) => { const result = await props.client.hosts.externalBrowserSessions(props.session.id, { signal }); if (result.items.some(item => item.root_id !== props.rootId)) throw new Error('External browser metadata belongs to another root.'); return result; } });
  const [busy, setBusy] = useState(false), [review, setReview] = useState(false), [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null), available = useRef(props.connected);
  available.current = props.connected;
  useEffect(() => { available.current = props.connected; return () => { available.current = false; request.current?.abort(); }; }, [props.client]);
  useEffect(() => { if (!props.connected) request.current?.abort(); }, [props.connected]);
  const root = props.session.id === props.rootId && props.selected.parent_id === null;
  async function change(item: ExternalBrowserSession, reconnect: boolean) {
    if (!available.current || request.current || review || !root || props.selected.lifecycle !== 'active') return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setReview(true); setError(undefined);
    try {
      if (reconnect) await props.client.hosts.reconnectExternalBrowser(props.rootId, item.name, item.generation, { signal: controller.signal });
      else await props.client.hosts.disconnectExternalBrowser(props.rootId, item.name, item.generation, { signal: controller.signal });
      controller.signal.throwIfAborted(); await query.refetch({ throwOnError: true }); controller.signal.throwIfAborted(); setReview(false);
    } catch (error) { if (available.current) { setError(error); setReview(true); } }
    finally { if (request.current === controller) request.current = null; setBusy(false); }
  }
  return <Section title="Named external connections" description="Status does not contact Chrome. Reconnect prepares a fresh generation without opening Chrome or replaying work; its next operation needs permission.">
    <QueryFeedback query={query} connected={props.connected}/>
    {!root && <p>Open the root agent to reconnect or disconnect these resources. Children still need exact delegated browser grants.</p>}
    {query.data?.items.length === 0 && <Empty>No external browser resource has been prepared for this root.</Empty>}
    {query.data?.items.map(item => <article key={item.generation}><strong>{item.name}</strong><p>{item.mode} · {item.driver} · {item.state}</p><details><summary>Exact browser authority</summary><p>Generation: <code>{item.generation}</code></p><p>Resource: <code>{item.resource}</code></p></details>
      {root && <><Button disabled={!props.connected || busy || review || query.isFetching || props.selected.lifecycle !== 'active'} onClick={() => void change(item, true)}>Reconnect {item.name}</Button><Button variant="ghost" disabled={!props.connected || busy || review || query.isFetching || item.state === 'ended' || props.selected.lifecycle !== 'active'} onClick={() => void change(item, false)}>Disconnect {item.name}</Button></>}
    </article>)}
    {review && <p role="status">The connection change was not confirmed. Read current generations before another action; no request will be replayed.</p>}
    <ErrorNotice type="action" owner={`${props.client.runtimeID}:${props.rootId}:external-browser`} title="Could not change external browser connection" error={error}/>
    <Button variant="ghost" disabled={!props.connected || busy || query.isFetching} onClick={() => { void query.refetch({ throwOnError: true }).then(() => { if (available.current) { setReview(false); setError(undefined); } }, error => { if (available.current) setError(error); }); }}>Read current external connections</Button>
  </Section>;
}
