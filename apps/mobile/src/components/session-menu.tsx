import { useRef, useState } from 'react';
import * as Clipboard from 'expo-clipboard';
import type { Tree } from '@whip/sdk';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { useWorkspace, useWorkspaceState } from '../runtime/workspace-context';
import { Button, ListRow, Notice, Stack, TextField } from '../ui';
export function SessionMenu({ rootId, tree, onDetails, onClose }: { rootId: string; tree: Tree; onDetails(): void; onClose(): void }) {
  const runtime = useRuntime(); const state = useRuntimeState(); const workspace = useWorkspace(); const device = useWorkspaceState();
  const [renaming, setRenaming] = useState(false); const [title, setTitle] = useState(tree.metadata.title ?? ''); const [error, setError] = useState<string>(); const [busy, setBusy] = useState(false); const lock = useRef(false);
  const runtimeId = state.host?.runtimeId!; const pinned = device.pins.includes(JSON.stringify([runtimeId, rootId]));
  async function perform(action: 'rename' | 'archive' | 'pin' | 'copy') {
    if (lock.current) return; lock.current = true; setBusy(true); setError(undefined);
    try {
      if (action === 'pin') await workspace.pin(runtimeId, rootId, !pinned);
      else if (action === 'copy') await Clipboard.setStringAsync(rootId);
      else {
        const client = runtime.requireReady(); if (client !== state.client || client.runtimeID !== runtimeId) throw new Error('Reconnect this host before changing the session.');
        try { await client.trees.update(tree.id, tree.revision, { ...tree.metadata, ...(action === 'rename' ? { title: title.trim() } : { archived: !tree.metadata.archived }) }); }
        finally { await runtime.query.invalidateQueries({ queryKey: [runtimeId] }); }
      }
      onClose();
    } catch (e) { setError((e instanceof Error ? e.message : String(e)) + ' Reopen the menu to inspect current host metadata before trying again.'); }
    finally { lock.current = false; setBusy(false); }
  }
  return <Stack style={{ paddingHorizontal: 20, paddingBottom: 24 }}>{error && <Notice danger>{error}</Notice>}{renaming ? <><TextField label="Session name" value={title} onChangeText={setTitle} maxLength={256} editable={!busy} autoFocus /><Button label="Save name" disabled={busy || !title.trim() || !state.ready || !!error} loading={busy} onPress={() => { void perform('rename'); }} /></> : <>
    <ListRow title={pinned ? 'Unpin on this phone' : 'Pin on this phone'} disabled={busy} onPress={() => { void perform('pin'); }} />
    <ListRow title="Rename" disabled={busy || !state.ready || !!error} onPress={() => setRenaming(true)} />
    <ListRow title={tree.metadata.archived ? 'Restore session' : 'Archive session'} disabled={busy || !state.ready || !!error} onPress={() => { void perform('archive'); }} />
    <ListRow title="Session details" onPress={onDetails} /><ListRow title="Copy session ID" disabled={busy} onPress={() => { void perform('copy'); }} />
  </>}</Stack>;
}
