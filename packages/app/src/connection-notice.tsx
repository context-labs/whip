import { useContext, useEffect, useState, useSyncExternalStore } from 'react';
import { Button } from '@whip/ui';
import { ErrorNotice } from './error-feedback';
import type { HostConnection } from './hosts';
import { RuntimeContext } from './context';

const noSubscribe = () => () => {};

export function HostNotice({ host, onManage }: { host: HostConnection; onManage(): void }) {
  const prompts = useContext(RuntimeContext)?.platform.hostPrompts;
  const inDialog = useSyncExternalStore(prompts?.subscribe ?? noSubscribe, () => prompts?.isClaimed(host.id) ?? false);
  return inDialog ? null : <ConnectionError state={host.state} error={host.error} name={host.name} owner={host.id} onManage={onManage} />;
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
    title={`${name} · ${state === 'incompatible' ? 'Connection needs attention' : state === 'connecting' ? 'Reconnecting' : 'Connection unavailable'}`}
    action={action} onDismiss={() => setDismissed(identity)} />;
}
