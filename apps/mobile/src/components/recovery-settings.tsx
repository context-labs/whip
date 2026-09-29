import { useQuery } from '@tanstack/react-query';
import { useState, useSyncExternalStore } from 'react';
import { Alert, Platform } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { themeCatalog, useTheme, type Appearance } from '../theme/theme';
import { Actions, Label, Notice, RowButton, Screen, Stack } from './primitives';
import { Connection } from './connection';
import { SavedDrafts } from './saved-drafts';
import { diagnostics } from '../runtime/diagnostics';
import { useResetStorage } from '../runtime/reset-context';
export default function SettingsScreen() {
  const runtime = useRuntime(); const state = useRuntimeState(); const theme = useTheme();
  const resetStorage = useResetStorage();
  const retired = useQuery({ queryKey: ['retired-local-recovery'], queryFn: () => runtime.storage.listRecovery() });
  const decisions = useSyncExternalStore(runtime.decisions.subscribe, runtime.decisions.getSnapshot, runtime.decisions.getSnapshot);
  const [choosing, setChoosing] = useState<'light' | 'dark'>();
  const changeAppearance = (appearance: Appearance) => { void runtime.setAppearance(appearance).catch(runtime.report); };
  return <Screen><Connection /><Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Command activity</Label>
    {!state.commands.length && <Label muted>No retained command records for this host.</Label>}
    {state.commands.map(command => <Stack key={command.record.commandId}><Label>{command.record.operation} · {command.status}</Label>
      <Label muted style={{ fontSize: 12 }}>{command.record.rootId ?? 'New session'}{command.record.sessionId ? ` / ${command.record.sessionId}` : ''}</Label>
      {command.message && <Notice>{command.message}</Notice>}
      <Actions items={[
        ...(!['accepted', 'failed'].includes(command.status) ? [{ label: 'Check delivery', secondary: true, disabled: !state.ready, onPress: () => { void runtime.checkCommand(command).catch(runtime.report); } }] : []),
        ...(command.destination && !command.destination.deleted ? [{ label: 'Open located session', secondary: true, onPress: () => router.push({ pathname: '/session/[rootId]', params: { rootId: command.destination!.rootId, runtimeId: command.record.runtimeId, hostId: state.host?.id } }) }] : []),
        ...(command.retryable ? [{ label: 'Retry original request', disabled: !state.ready, secondary: true, onPress: () => { void runtime.retryCommand(command).catch(runtime.report); } }] : []),
        ...(['accepted', 'failed', 'missing'].includes(command.status) ? [{ label: 'Clear resolved record', secondary: true, onPress: () => { void runtime.forgetCommand(command).catch(runtime.report); } }] : []),
      ]} />
    </Stack>)}
  </Stack><Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Permission decisions</Label>
    {!decisions.items.length && <Label muted>No retained permission decisions for this host.</Label>}
    {decisions.items.map(decision => <Stack key={decision.record.commandId}>
      <Label>{decision.status === 'delivered' ? 'Decision delivered' : decision.status}</Label>
      <Label muted>{decision.record.rootId} / {decision.record.sessionId ?? 'Unknown agent'}</Label>
      {decision.message && <Notice>{decision.message}</Notice>}
      {decision.status === 'delivered' && <Label muted>The tool has its decision. Its execution result appears in the conversation.</Label>}
      <Actions items={['delivered', 'resolved', 'unavailable'].includes(decision.status)
        ? [{ label: 'Clear resolved decision', secondary: true, onPress: () => { void runtime.decisions.clear(decision.record.commandId).catch(runtime.report); } }]
        : [{ label: 'Check decision delivery', secondary: true, disabled: !state.ready, onPress: () => { void runtime.decisions.check(decision.record.commandId).catch(runtime.report); } }]} />
    </Stack>)}
  </Stack><Notice>Whip mobile connects directly to your private host. Drafts and recovery metadata are stored in an encrypted database on this device. Sessions refresh when the app is active; background notifications are not enabled.</Notice>
    <Notice>After an app restart, only receipt identities remain. Inspect them before sending anything new; retry is available only while this app still holds the original request. Records from older protocol versions are preserved and are never replayed.</Notice><SavedDrafts />
    {!!retired.data?.length && <Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Older local records</Label><Notice>These records use a retired protocol. They remain on this phone and cannot be checked or replayed against this host.</Notice>{retired.data.map(value => <Stack key={JSON.stringify([value.record.runtimeId, value.record.clientId, value.record.commandId])}><Label>{value.record.operation}</Label><Label muted selectable>{value.record.runtimeId} / {value.record.rootId ?? value.record.commandId}</Label><Actions items={[{ label: 'Remove old local record…', secondary: true, onPress: () => Alert.alert('Remove this old local record?', 'This removes only its local metadata. It does not cancel or resend any host work.', [{ text: 'Keep', style: 'cancel' }, { text: 'Remove', style: 'destructive', onPress: () => { void runtime.storage.recoveryStorage.delete(value.record).then(() => retired.refetch()).catch(runtime.report); } }]) }]} /></Stack>)}</Stack>}
    {retired.error && <Notice>{retired.error.message} Older records have been preserved.</Notice>}
    <Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Diagnostics</Label>
      <Label muted>Copy app, protocol, connection and storage status. Includes runtime ID; excludes server addresses, prompts, transcripts, drafts and keys.</Label>
      <Actions items={[{ label: 'Copy diagnostics', secondary: true, onPress: () => { void Clipboard.setStringAsync(JSON.stringify(diagnostics(state, Platform.OS, Platform.Version), null, 2)).catch(runtime.report); } }]} />
    </Stack><Actions items={[{ label: 'Reset local data…', secondary: true, onPress: () => Alert.alert('Erase Whip data on this phone?', 'This permanently deletes saved servers, unsent drafts and delivery recovery records on this phone. Work and history on your hosts continue. Copy any drafts you want to keep first.', [{ text: 'Keep data', style: 'cancel' }, { text: 'Erase local data', style: 'destructive', onPress: () => { void resetStorage(); } }]) }]} />
    <RowButton title="Font licenses" onPress={() => router.push('/licenses')} />
    <Label muted style={{ fontSize: 12 }}>Whip mobile 0.1.0</Label>
  </Screen>;
}
