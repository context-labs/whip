import { useEffect, useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import { useWhipConnection } from '@whip/sdk/react';
import { Button } from '@whip/ui';
import { ErrorNotice } from './error-feedback';
import type { HostConnection } from './hosts';

export function HostNotice({ host, onManage }: { host: HostConnection; onManage(): void }) {
  return <ConnectionError state={host.state} error={host.error} name={host.name} owner={host.id} onManage={onManage} />;
}

// Kept as a standalone subscriber for embedders; the shell uses host records so
// failed setup without an admitted SDK client has the same canonical location.
export function ConnectionNotice({ client }: { client: WhipClient }) {
  const connection = useWhipConnection(client);
  return <ConnectionError state={connection.state} error={connection.error?.message} name="Server" owner="connection" />;
}

function ConnectionError({ state, error, name, owner, onManage }: {
  state: string; error?: string; name: string; owner: string; onManage?(): void;
}) {
  const [dismissed, setDismissed] = useState('');
  useEffect(() => { if (state === 'connected') setDismissed(''); }, [state]);
  const identity = `${state}:${error}`;
  if (state === 'connected' || !error) return null;
  const action = onManage && <Button variant="ghost" onClick={onManage}>Manage servers</Button>;
  if (dismissed === identity) return <div role="status">{name} · connection unavailable {action}
    <Button variant="ghost" onClick={() => setDismissed('')}>Show details</Button></div>;
  return <ErrorNotice type="host" owner={owner} error={error}
    title={`${name} · ${state === 'incompatible' ? 'Connection needs attention' : state === 'reconnecting' ? 'Reconnecting' : 'Connection unavailable'}`}
    action={action} onDismiss={() => setDismissed(identity)} />;
}
