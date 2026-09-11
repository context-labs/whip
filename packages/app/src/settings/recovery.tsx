import { ErrorNotice } from '../error-feedback';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import { Button, Dialog } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from '../context';
import { layout } from '../styles';
import { SettingsGroup } from './section-layout';

export function RecoverySettings({ client, enabled = client?.getSnapshot().state === 'connected' }: { client?: WhipClient; enabled?: boolean }) {
  const runtime = useRuntime();
  const [discard, setDiscard] = useState(false);
  const [error, setError] = useState('');
  return <>
    <div id="drafts" tabIndex={-1}><SettingsGroup title="Saved drafts">
      <p {...stylex.props(layout.muted)}>Up to 32 unsent drafts are stored on this device. Discard drafts to reclaim space, including drafts from deleted or inaccessible sessions.</p>
      <div><Button variant="secondary" onClick={() => { setError(''); setDiscard(true); }}>Discard saved drafts…</Button></div>
      <Dialog open={discard} onOpenChange={setDiscard} title="Discard saved drafts?" description="This removes unsent draft text stored on this device. Accepted commands keep running. Other open windows may still have unsaved edits."
        footer={<Button variant="danger" onClick={() => { try { runtime.discardDrafts(); setError(''); setDiscard(false); } catch (error) { setError(error instanceof Error ? error.message : String(error)); } }}>Discard drafts</Button>}>
        {error && <ErrorNotice type="action" owner="discard-drafts" title="Could not discard drafts" error={error} />}
      </Dialog>
    </SettingsGroup></div>
    <div id="commands" tabIndex={-1}><SettingsGroup title="Command recovery">
      <p {...stylex.props(layout.muted)}>A lost connection does not cancel accepted work. Check an uncertain command before submitting it again. Stored records contain identities, never prompt bodies.</p>
      {client ? <CommandRecovery key={client.getSnapshot().info?.runtime_id} client={client} enabled={enabled} /> : <p role="status">Connect to an execution host to inspect its command recovery records. Local drafts remain available above.</p>}
    </SettingsGroup></div>
  </>;
}

function CommandRecovery({ client, enabled }: { client: WhipClient; enabled: boolean }) {
  const { commands } = useAppState();
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const records = useQuery({
    queryKey: ['command-recovery', runtimeId],
    queryFn: () => client.recoveryRecords(),
  });
  const hostRecords = records.data?.filter(record => record.runtimeId === runtimeId);
  const [outcomes, setOutcomes] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const clearError = (id: string) => setErrors(previous => { const next = { ...previous }; delete next[id]; return next; });
  return <>
    {commands.filter(command => command.runtimeId === runtimeId).map(command => <div key={command.id} {...stylex.props(layout.notice)}>
      {command.label} · {command.status}
    </div>)}
    {hostRecords?.map(record => <div key={`${record.runtimeId}:${record.commandId}`} {...stylex.props(layout.notice, layout.column)}>
      <strong>{record.operation}</strong>
      <span {...stylex.props(layout.muted)}>{record.commandId}</span>
      {outcomes[record.commandId] && <span role="status">{outcomes[record.commandId]}</span>}
      {errors[record.commandId] && <ErrorNotice type="action" owner={`recovery:${record.commandId}`} title="Could not update this recovery record" error={errors[record.commandId]} />}
      <div {...stylex.props(layout.row, layout.wrap)}>
        <Button variant="secondary" disabled={!enabled || record.runtimeId !== runtimeId} onClick={async () => {
          clearError(record.commandId);
          try { const outcome = await client.recover(record).status(); setOutcomes(previous => ({ ...previous, [record.commandId]: outcome.status })); }
          catch (error) { setErrors(previous => ({ ...previous, [record.commandId]: error instanceof Error ? error.message : String(error) })); }
        }}>Check status</Button>
        <Button variant="ghost" onClick={() => { clearError(record.commandId); void client.forget(record).then(() => { setOutcomes(previous => { const next = { ...previous }; delete next[record.commandId]; return next; }); return records.refetch(); }).catch(error => setErrors(previous => ({ ...previous, [record.commandId]: error instanceof Error ? error.message : String(error) }))); }}>Forget record</Button>
      </div>
    </div>)}
    {hostRecords?.length === 0 && !commands.some(command => command.runtimeId === runtimeId) && <p>No recovery records for this host.</p>}
    {records.error && <ErrorNotice type="resource" owner={`${runtimeId}:recovery-records`} title="Could not load recovery records" error={records.error} />}
    {!enabled && <p role="status">Reconnect this host to check command status. Forgetting a local record does not cancel host work.</p>}
  </>;
}
