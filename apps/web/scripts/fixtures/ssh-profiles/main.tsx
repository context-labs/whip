import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Button, initializeTheme, ThemeProvider, UIProvider } from '@whip/ui';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
import { RuntimeContext } from '../../../../../packages/app/src/context';
import { HostDialog, ServerManager } from '../../../../../packages/app/src/host-dialog';
import { createHostPrompts, HostPrompts } from '../../../../../packages/app/src/host-prompts';
import type { DesktopBridge, DesktopEvent } from '../../../../../packages/app/src/desktop-bridge';
import type { AppRuntime } from '../../../../../packages/app/src/runtime';
import type { ConnectionProfile } from '../../../../../packages/app/src/connections';

// Real shared UI; synthetic profiles and connection results. No host access.
const params = new URLSearchParams(location.search);
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: params.get('theme') ?? 'dark' }));
initializeTheme({ storage: localStorage });
const query = new QueryClient();
const saved = { id: 'saved', name: 'Staging', device: true, state: 'closed', profile: { id: 'saved', label: 'Staging', runtimeId: 'known', target: { kind: 'ssh' as const, host: 'staging' } } };
const state = { hosts: [saved], home: { state: 'connected' }, profilesReady: true, legacyHosts: [] };
let reads = 0;
let connectionCount = 0;
const events = new Set<(event: DesktopEvent) => void>();
let answer: ((values: string[] | null) => void) | undefined;
let cancel: (() => void) | undefined;
const prompts = createHostPrompts({
  onEvent(listener) { events.add(listener); return () => { events.delete(listener); }; },
  async answerPrompt(_id, values) { answer?.(values); },
} as DesktopBridge);
async function connect(profile: ConnectionProfile, signal?: AbortSignal) {
  const number = ++connectionCount;
  const attemptId = `fixture-${number}`;
  const unregister = prompts.registerAttempt(attemptId, profile.id);
  try {
    await new Promise<void>((resolve, reject) => {
      cancel = () => { clearTimeout(timer); unregister(); reject(new Error('cancelled')); };
      signal?.addEventListener('abort', cancel, { once: true });
      const timer = setTimeout(() => {
        if (!params.has('auth')) { resolve(); return; }
        answer = values => { if (values === null) cancel?.(); else {
          for (const listener of events) listener({ kind: 'prompt-dismissed', id: attemptId });
          setTimeout(resolve, 500);
        } };
        for (const listener of events) listener({ kind: 'prompt', prompt: { id: attemptId, attemptId,
          title: 'Authentication required', message: `Enter the SSH password for ${profile.target.kind === 'ssh' ? profile.target.host : profile.label}.`,
          fields: [{ label: 'Password', secret: true }], confirmLabel: 'Continue' } });
      }, params.has('connecting') ? 60_000 : 600);
    });
    if (params.has('failure') && number === 1) throw new Error('The host did not respond. Check your SSH connection and try again.');
    return profile.id;
  } finally { unregister(); if (cancel) signal?.removeEventListener('abort', cancel); cancel = undefined; answer = undefined; }
}
const tabs = { previous: [] };
const runtime = {
  queries: query, getSnapshot: () => state, subscribe: () => () => {},
  tabs: { getSnapshot: () => tabs, subscribe: () => () => {} },
  platform: { hostPrompts: prompts, connectionKinds: ['url', 'ssh'], listSSHProfiles: async () => {
    reads++;
    if (params.has('slow')) await new Promise(resolve => setTimeout(resolve, 1000));
    if (params.has('error') && reads === 1) throw new Error('The SSH configuration could not be read.');
    return { truncated: false, profiles: params.has('empty') ? [] : [
      { alias: 'kuzco-4090', hostname: 'kuzco-4090', user: 'sam', port: 22 },
      { alias: 'mac-mini', hostname: 'mac-mini.local', user: 'sam', port: 22 },
      { alias: 'build-box', hostname: 'build-box.internal', user: 'deploy', port: 2222 },
      { alias: 'staging', hostname: 'staging.internal', user: 'deploy', port: 22 },
    ] };
  } },
  connections: { refreshProfiles: async () => {}, getSnapshot: () => ({ legacyProfiles: [] }), host: () => saved,
    select() {}, connect: async () => { await connect(saved.profile); }, disconnect: () => cancel?.(),
    saveNative: (profile: ConnectionProfile, _accept: boolean, signal: AbortSignal) => connect(profile, signal),
  },
} as unknown as AppRuntime;
function Fixture() {
  const [open, setOpen] = useState(true);
  return <RuntimeContext.Provider value={runtime}><ThemeProvider storage={localStorage}><UIProvider><QueryClientProvider client={query}>
    {params.has('reconnect') ? <ServerManager /> : <><Button onClick={() => setOpen(true)}>Add server</Button><HostDialog open={open} onOpenChange={setOpen} /></>}
    <HostPrompts prompts={prompts} />
  </QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
}
createRoot(document.getElementById('root')!).render(<Fixture />);
