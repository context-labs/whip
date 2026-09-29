import { useEffect, useRef, useState } from 'react';
import type { Client, Grant, Operations, Session, SessionRecord } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import { useQuery } from '@tanstack/react-query';
import { Button, Field, Input, Select } from '@whip/ui';
import { ErrorNotice } from '../error-feedback';
import { PageControls, QueryFeedback, Section } from './shared';

type Request = Operations['grants.create']['params'];
type Props = {
  client: Client;
  session: Session;
  selected: DeepReadonly<SessionRecord>;
  connected: boolean;
  onCreated(): Promise<unknown>;
};

export function StandingGrant(props: Props) {
  return <GrantForm key={`${props.client.runtimeID}:${props.client.processEpoch}:${props.session.id}`} {...props} />;
}
function GrantForm({ client, session, selected, connected, onCreated }: Props) {
  const parentID = selected.parent_id;
  const [capability, setCapability] = useState('');
  const [resource, setResource] = useState('');
  const [issuerID, setIssuerID] = useState('');
  const [after, setAfter] = useState<string>();
  const [attempt, setAttempt] = useState<Request>();
  const [result, setResult] = useState<Grant>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const active = useRef({ client, owner: session.id });
  active.current = { client, owner: session.id };
  const lifetime = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    return () => { controller.abort(); if (lifetime.current === controller) lifetime.current = undefined; };
  }, [client, session.id]);
  const parents = useQuery({
    queryKey: ['standing-grant-parent', client.runtimeID, client.processEpoch, session.id, parentID, after],
    queryFn: async ({ signal }) => {
      const page = await client.call('grants.list', { session_id: parentID!, limit: 32, ...(after ? { after } : {}) }, { signal });
      const items = page.items ?? [];
      if (items.some(grant => grant.session_id !== parentID)) throw new Error('Parent grants belong to another session');
      return { ...page, items };
    },
    enabled: connected && !!parentID && selected.id === session.id,
    gcTime: 0, retry: false,
  });
  const issuers = parents.data?.items.filter(grant => grant.revoked_at === null && grant.operation_id === null) ?? [];
  const issuer = issuers.find(grant => grant.id === issuerID);
  const locked = busy || !!attempt;
  const valid = selected.id === session.id && session.client === client && (parentID ? !!issuer :
    capability.length <= 128 && /^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z][A-Za-z0-9_-]*)*$/.test(capability) && !!resource && new TextEncoder().encode(resource).length <= 4096 && !/[\u0000-\u001f\u007f]/.test(resource));
  async function send() {
    if (!connected || busy || result || (!attempt && !valid) || !lifetime.current) return;
    const controller = lifetime.current, owner = session.id;
    const request = attempt ?? { id: crypto.randomUUID(), session_id: owner,
      capability: issuer?.capability ?? capability, resource: issuer?.resource ?? resource,
      ...(issuer ? { issuer_id: issuer.id } : {}) };
    setAttempt(request); setBusy(true); setError(undefined);
    const current = () => !controller.signal.aborted && active.current.client === client && active.current.owner === owner;
    try {
      const grant = await client.call('grants.create', request, { signal: controller.signal });
      if (grant.id !== request.id || grant.session_id !== owner || grant.capability !== request.capability ||
        grant.resource !== request.resource || grant.issuer_id !== (request.issuer_id ?? null) || grant.operation_id !== null)
        throw new Error('Grant acknowledgement does not match the exact request');
      if (!current()) return;
      setResult(grant);
      await onCreated();
    } catch (error) { if (current()) setError(error); }
    finally { if (current()) setBusy(false); }
  }
  return <Section title="Create standing grant" description="Authorize future operations with this exact capability and resource. A resource may name an entire workspace. No wildcard or host-wide approval is created; existing pending requests still require their own decision.">
    <p>Selected owner: <code>{session.id}</code></p>
    {parentID ? <>
      <p>This child can receive only an active standing grant from its direct parent. The capability and resource cannot be widened.</p>
      <QueryFeedback query={parents} connected={connected} />
      <Select label="Parent standing grant" value={issuerID} disabled={locked || !connected}
        options={[{ value: '', label: 'Choose an exact parent grant' }, ...issuers.map(grant => ({ value: grant.id, label: `${grant.capability} · ${grant.resource} · ${grant.id}` }))]}
        onValueChange={setIssuerID} />
      {issuer && <p><code>{issuer.capability}</code> · <code>{issuer.resource}</code></p>}
      <PageControls after={after} next={parents.data?.items.length === 32 ? parents.data.items.at(-1)?.id : undefined}
        busy={parents.isFetching || locked} connected={connected} onChange={value => { setIssuerID(''); setAfter(value); }} />
    </> : <>
      <Field label="Exact capability"><Input value={capability} maxLength={128} disabled={locked || !connected} onChange={event => setCapability(event.target.value)} placeholder="files.read" /></Field>
      <Field label="Exact resource"><Input value={resource} maxLength={4096} disabled={locked || !connected} onChange={event => setResource(event.target.value)} placeholder="Copy the exact resource from a requested operation" /></Field>
    </>}
    {error !== undefined && <ErrorNotice type="action" owner={`${client.runtimeID}:${session.id}:standing-grant`} title="Standing grant needs checking" error={error} />}
    {attempt && <p>Grant identity: <code>{attempt.id}</code>. The exact request stays in this form for explicit retry. Keep this inspector open until its outcome is known.</p>}
    {result ? <>
      <p role="status">{result.revoked_at ? 'This exact grant was created and has since been revoked.' : 'Standing grant created.'}</p>
      <Button onClick={() => { setAttempt(undefined); setResult(undefined); setError(undefined); setCapability(''); setResource(''); setIssuerID(''); }}>Create another grant</Button>
    </> : <Button disabled={!connected || busy || (!attempt && !valid)} loading={busy} onClick={() => void send()}>{attempt ? 'Retry same grant' : 'Create standing grant'}</Button>}
  </Section>;
}
