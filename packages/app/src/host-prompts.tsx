import { useContext, useEffect, useState, useSyncExternalStore } from 'react';
import { Dialog } from '@whip/ui';
import { RuntimeContext } from './context';
import { HostConnectionDialog, type SSHConnectionRequest } from './host-connection-dialog';

import type { HostPromptsController, PendingPrompt } from './host-prompt-controller';
import { HostPromptForm } from './host-prompt-form';
export { createHostPrompts } from './host-prompt-controller';

export function HostPrompts({ prompts }: { prompts: HostPromptsController }) {
  const active = useSyncExternalStore(prompts.subscribe, prompts.getSnapshot, prompts.getSnapshot);
  const runtime = useContext(RuntimeContext);
  const [request, setRequest] = useState<SSHConnectionRequest>();
  useEffect(() => {
    // A foreground dialog claims its host in a layout effect before this runs.
    const current = prompts.getSnapshot();
    if (request || !runtime || !current?.hostId) return;
    const host = runtime.connections.host(current.hostId);
    if (!host) return;
    setRequest({ profile: host.profile, onCancel: () => setRequest(undefined), onConnected: () => setRequest(undefined) });
  }, [active, prompts, request, runtime]);
  if (request) return <HostConnectionDialog open title="Connect to server" request={request} onOpenChange={() => setRequest(undefined)} />;
  return active && (!runtime || !active.hostId) ? <PromptDialog key={active.serial} active={active} prompts={prompts} /> : null;
}

function PromptDialog({ active, prompts }: { active: PendingPrompt; prompts: HostPromptsController }) {
  return <Dialog open onOpenChange={open => { if (!open) prompts.answer(active.serial, null); }} title={active.prompt.title}
    closeLabel="Cancel SSH request">
    <HostPromptForm active={active} prompts={prompts} />
  </Dialog>;
}
