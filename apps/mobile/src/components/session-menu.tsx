import { useRef, useState } from 'react';
import * as Clipboard from 'expo-clipboard';
import { useSessionView } from '@whip/sdk/react';
import type { SessionView } from '@whip/sdk/state';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { useWorkspace, useWorkspaceState } from '../runtime/workspace-context';
import { Button, ListRow, Notice, Stack, TextField } from '../ui';
export function SessionMenu({ view, onDetails, onClose }: { view: SessionView; onDetails(): void; onClose(): void }) {
  const runtime = useRuntime(); const state = useRuntimeState(); const workspace = useWorkspace(); const device = useWorkspaceState();
  const snapshot = useSessionView(view); const rootId = view.session.rootId; const meta = snapshot.root?.meta;
  const [renaming, setRenaming] = useState(false); const [title, setTitle] = useState(meta?.title ?? ''); const [error, setError] = useState<string>(); const [busy, setBusy] = useState(false); const lock = useRef(false);
  const runtimeId = state.host?.runtimeId!; const pinned = device.pins.includes(JSON.stringify([runtimeId, rootId]));
  async function perform(action: 'rename' | 'archive' | 'pin' | 'copy') {
    if (lock.current) return; lock.current = true; setBusy(true); setError(undefined);
    try {
      if (action === 'pin') await workspace.pin(runtimeId, rootId, !pinned);
      else if (action === 'copy') await Clipboard.setStringAsync(rootId);
      else {
        if (runtime.requireReady() !== view.session.client || view.getSnapshot().status !== 'live') throw new Error('Reconnect this host before changing the session.');
        const outcome = action === 'rename' ? await runtime.run('session.rename', { title: title.trim() }, { rootId }) : await runtime.run('session.archive', { archived: !view.getSnapshot().root?.meta.archived }, { rootId });
        if (outcome.status !== 'succeeded') throw new Error(outcome.failure?.message ?? `Change ${outcome.status}. Check delivery in Settings.`);
      }
      onClose();
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { lock.current = false; setBusy(false); }
  }
  return <Stack style={{ paddingHorizontal: 20, paddingBottom: 24 }}>{error && <Notice danger>{error}</Notice>}{renaming ? <><TextField label="Session name" value={title} onChangeText={setTitle} maxLength={256} editable={!busy} autoFocus /><Button label="Save name" disabled={busy || !title.trim() || !state.ready} loading={busy} onPress={() => { void perform('rename'); }} /></> : <>
    <ListRow title={pinned ? 'Unpin on this phone' : 'Pin on this phone'} disabled={busy} onPress={() => { void perform('pin'); }} />
    <ListRow title="Rename" disabled={busy || !state.ready || !state.client?.supports('runtime', 'session.rename')} onPress={() => setRenaming(true)} />
    <ListRow title={meta?.archived ? 'Restore session' : 'Archive session'} disabled={busy || !state.ready || !state.client?.supports('runtime', 'session.archive')} onPress={() => { void perform('archive'); }} />
    <ListRow title="Session details" onPress={onDetails} />
    <ListRow title="Copy session ID" disabled={busy} onPress={() => { void perform('copy'); }} />
  </>}</Stack>;
}
