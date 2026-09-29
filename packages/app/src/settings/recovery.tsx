import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { DurableCommand, type Client, type RecoveryRecord, type RecoveryCheck, type RecoveryEvidence } from '@whip/sdk';
import { Button, Dialog } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { SettingsGroup } from './section-layout';

function outcome(evidence: RecoveryEvidence): string {
  if ('receipt' in evidence) {
    if (evidence.receipt.deleted_at) return 'Accepted request; its session was deleted.';
    if (evidence.turn) return `Accepted · turn ${evidence.turn.state}${evidence.turn.failure ? `: ${evidence.turn.failure}` : ''}.`;
    return `Accepted · input ${evidence.input?.state ?? 'unavailable'}.`;
  }
  if ('state' in evidence) return `Accepted workspace action · ${evidence.state}${evidence.failure ? `: ${evidence.failure}` : ''}.`;
  if ('steering' in evidence) return `Input steering accepted${evidence.deleted ? ' · session deleted' : evidence.input?.steering?.consumed ? ' · consumed by its target turn' : ' · waiting for consumption'}.`;
  if ('creation' in evidence) return `Accepted root creation${evidence.root ? '' : ' · root unavailable or deleted'}.`;
  return 'Permission policy edit accepted.';
}
export function recoveryStatus(check: RecoveryCheck | undefined, accepted: boolean): string {
  if (!check) return accepted ? 'Host acknowledgement saved. Execution outcome has not been checked.' : 'Delivery outcome unknown. Nothing is replayed automatically.';
  switch (check.state) {
    case 'found': return outcome(check.evidence);
    case 'identity_only': return 'An identity receipt exists, but it does not verify this exact payload. Acceptance remains unconfirmed.';
    case 'missing': return 'No matching receipt was found. Delivery is not confirmed.';
    case 'unavailable': return 'This command has no read-only receipt lookup. Its outcome remains unknown.';
  }
}
export function RecoverySettings() {
  const runtime = useRuntime();
  const { hosts } = useAppState();
  const query = useQuery({ queryKey: ['command-recovery'], queryFn: () => runtime.recovery.list(), retry: false, gcTime: 0 });
  return <div id="commandRecovery" tabIndex={-1}>
    <SettingsGroup title="Saved commands" action={<Button disabled={query.isFetching} onClick={() => void query.refetch()}>Refresh saved commands</Button>}>
      <p>These exact requests are saved on this device for delivery recovery. Opening this page only reads local storage. Checking a receipt does not run work.</p>
      <ErrorNotice type="resource" owner="command-recovery" title="Could not read saved commands" error={query.error} />
      {query.isPending && <p role="status">Reading saved commands…</p>}
      {query.data?.length === 0 && <p>No saved commands.</p>}
      {query.data?.map(record => {
        const host = hosts.find(host => host.runtimeId === record.runtimeID);
        const client = host?.state === 'connected' && host.client?.clientID === record.clientID ? host.client : undefined;
        return <RecoveryRow key={`${record.runtimeID}:${record.clientID}:${record.request}`} record={record} client={client} hostName={host?.name ?? record.runtimeID} refresh={async () => { await query.refetch(); }} />;
      })}
    </SettingsGroup>
  </div>;
}
function RecoveryRow({ record, client, hostName, refresh }: { record: RecoveryRecord; client?: Client; hostName: string; refresh(): Promise<void> }) {
  const runtime = useRuntime();
  // The journal validated the whitelisted request. This is display metadata only;
  // the SDK recovers the original serialized request for every check and retry.
  const { method } = JSON.parse(record.request) as { method: string };
  const [check, setCheck] = useState<RecoveryCheck>();
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState<'retry' | 'forget'>();
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => { request.current = null; setBusy(false); setCheck(undefined); setError(undefined); return () => request.current?.abort(); }, [client]);
  async function action(kind: 'check' | 'retry' | 'forget') {
    if (request.current || kind !== 'forget' && !client) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined); setConfirm(undefined);
    try {
      if (kind === 'forget') await runtime.recovery.forget(record);
      else if (client) {
        const command = DurableCommand.recover(client, record, { journal: runtime.recovery });
        if (kind === 'retry') await command.retry({ signal: controller.signal });
        const found = await command.check({ signal: controller.signal });
        if (!controller.signal.aborted) setCheck(found);
      }
      if (!controller.signal.aborted) await refresh();
    } catch (error) { if (!controller.signal.aborted) { setError(error); await refresh(); } }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  return <section aria-label={`Saved ${method} on ${hostName}`} {...stylex.props(layout.column)}>
    <strong>{method} · {hostName}</strong>
    <p role="status">{recoveryStatus(check, record.accepted)}</p>
    {!client && <p>Connect the saved runtime with this device’s original client identity to check or retry. A replacement runtime cannot receive this request.</p>}
    <details><summary>Inspect saved request</summary><pre {...stylex.props(layout.muted)} style={{ maxHeight: 240, overflow: 'auto', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{record.request.slice(0, 4096)}</pre>{record.request.length > 4096 && <p>Preview limited to 4,096 characters. Retry retains the complete original payload.</p>}</details>
    <div {...stylex.props(layout.row)}><Button disabled={!client || busy} onClick={() => void action('check')}>Check delivery</Button><Button disabled={!client || busy} onClick={() => setConfirm('retry')}>Retry exact request…</Button><Button disabled={busy} onClick={() => setConfirm('forget')}>Forget tracking…</Button></div>
    <ErrorNotice type="action" owner={`recovery:${record.runtimeID}:${method}`} title="Saved command needs attention" error={error} />
    {confirm && <Dialog open onOpenChange={open => { if (!open) setConfirm(undefined); }} title={confirm === 'retry' ? 'Retry this exact request?' : 'Forget command tracking?'} description={confirm === 'retry'
      ? 'Whip will resend the original identity and complete payload to the saved runtime. The host checks its durable receipt first. This can admit work if the request never arrived. Closing Settings only stops waiting; it does not cancel accepted work.'
      : 'This removes the local recovery record. It does not cancel or undo host work, and an unresolved delivery may become impossible to track from this device.'} footer={<><Button onClick={() => setConfirm(undefined)}>Cancel</Button><Button disabled={busy} onClick={() => void action(confirm)}>{confirm === 'retry' ? 'Retry exact request' : 'Forget tracking'}</Button></>} />}
  </section>;
}
