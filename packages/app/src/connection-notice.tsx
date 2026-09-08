import { useEffect, useState } from 'react';
import { WhipError, type WhipClient } from '@whip/sdk';
import { useWhipConnection } from '@whip/sdk/react';
import { Button, Dialog, IconButton } from '@whip/ui';
import { X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';
import { useAppState, useRuntime } from './context';
import type { ConnectionProfile } from './connections';

export function ConnectionNotice({ client }: { client: WhipClient }) {
  const connection = useWhipConnection(client);
  const [dismissed, setDismissed] = useState<Error>();
  const error = connection.error;
  if (connection.state === 'connected' || !error || error === dismissed) return null;
  return (
    <div role="alert" {...stylex.props(layout.row, layout.notice)}>
      <span {...stylex.props(layout.grow)}>{error.message}</span>
      {error instanceof WhipError && error.kind === 'runtime_changed' && <ReplacementRuntime client={client} error={error} />}
      <IconButton label="Dismiss connection error" onClick={() => setDismissed(error)}>
        <X size={14} />
      </IconButton>
    </div>
  );
}

type Replacement = { client: WhipClient; profile: ConnectionProfile; error: WhipError; runtimeId?: string; observedRuntimeId?: string };

function ReplacementRuntime({ client, error }: { client: WhipClient; error: WhipError }) {
  const runtime = useRuntime();
  const state = useAppState();
  const [confirmation, setConfirmation] = useState<Replacement>();
  const current = (value: Replacement) => {
    const latest = runtime.getSnapshot();
    const attached = value.client.getSnapshot();
    return latest.client === value.client && latest.connection === value.profile
      && latest.connection.runtimeId === value.runtimeId && attached.info?.runtime_id === value.observedRuntimeId
      && attached.state !== 'connected' && attached.error === value.error;
  };
  const valid = !!confirmation && current(confirmation);
  useEffect(() => { if (confirmation && !valid) setConfirmation(undefined); }, [confirmation, valid]);
  return <>
    <Button variant="secondary" onClick={() => {
      const candidate: Replacement = { client, profile: state.connection, error,
        runtimeId: state.connection.runtimeId, observedRuntimeId: client.getSnapshot().info?.runtime_id };
      if (current(candidate)) setConfirmation(candidate);
    }}>Connect to replacement runtime</Button>
    <Dialog open={valid} onOpenChange={open => { if (!open) setConfirmation(undefined); }}
      title="Connect to replacement runtime?"
      description="This host now serves a different runtime. Your old tabs, drafts and command recovery records remain tied to the old runtime and will not be moved to the replacement.">
      <p>You will connect to the replacement at {confirmation?.profile.label}. Existing work on the old runtime is not cancelled.</p>
      <div {...stylex.props(layout.row)}>
        <Button variant="secondary" onClick={() => setConfirmation(undefined)}>Cancel</Button>
        <Button variant="primary" onClick={() => {
          if (!confirmation || !current(confirmation)) { setConfirmation(undefined); return; }
          const replacement = { ...confirmation.profile };
          delete replacement.runtimeId;
          setConfirmation(undefined);
          // connect owns errors and host-change cancellation. This retired notice
          // must not update another host when the asynchronous attempt settles.
          void runtime.connect(replacement).catch(() => {});
        }}>Connect to replacement runtime</Button>
      </div>
    </Dialog>
  </>;
}
