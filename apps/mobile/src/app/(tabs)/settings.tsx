import { useState, useSyncExternalStore } from 'react';
import { Alert, Platform } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { useRuntime, useRuntimeState } from '../../runtime/context';
import { themeCatalog, useTheme, type Appearance } from '../../theme/theme';
import { Actions, Label, Notice, RowButton, Screen, Stack } from '../../components/primitives';
import { Connection } from '../../components/connection';
import { SavedDrafts } from '../../components/saved-drafts';
import { diagnostics } from '../../runtime/diagnostics';
import { useResetStorage } from '../../runtime/reset-context';
export default function SettingsScreen() {
  const runtime = useRuntime(); const state = useRuntimeState(); const theme = useTheme();
  const resetStorage = useResetStorage();
  const decisions = useSyncExternalStore(runtime.decisions.subscribe, runtime.decisions.getSnapshot, runtime.decisions.getSnapshot);
  const [choosing, setChoosing] = useState<'light' | 'dark'>();
  const changeAppearance = (appearance: Appearance) => { void runtime.setAppearance(appearance).catch(runtime.report); };
  return <Screen><Connection /><Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Servers</Label>
    {state.hosts.map(host => <Stack key={host.id} style={{ gap: 4 }}><RowButton title={host.name} detail={host.url} selected={host.id === state.host?.id} disabled={state.connecting}
      onPress={() => { void runtime.connect(host).catch(runtime.report); }} /><Actions items={[{ label: `Edit ${host.name}`, secondary: true, disabled: state.connecting, onPress: () => router.push({ pathname: '/server', params: { hostId: host.id } }) }, { label: `Remove ${host.name}`, secondary: true, onPress: () => Alert.alert('Remove saved server?', 'Drafts and unresolved command records remain on this device. Work on the host continues.', [{ text: 'Keep', style: 'cancel' }, { text: 'Remove', style: 'destructive', onPress: () => { void runtime.removeHost(host.id).catch(runtime.report); } }]) }]} /></Stack>)}
    <Actions items={[{ label: 'Add server', secondary: true, disabled: state.hosts.length >= 4, onPress: () => router.push('/server') }, ...(state.host ? [{ label: 'Disconnect', secondary: true, onPress: () => { void runtime.detach().catch(runtime.report); } }] : [])]} />
  </Stack><Stack><RowButton title="Appearance" detail={`${theme.name} · Themes, text and motion`} onPress={() => router.push('/settings/appearance')} />  </Stack><Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Command activity</Label>
    {!state.commands.length && <Label muted>No retained command records for this host.</Label>}
    {state.commands.map(command => <Stack key={command.record.commandId}><Label>{command.record.operation} · {command.status}</Label>
      <Label muted style={{ fontSize: 12 }}>{command.record.rootId ?? 'New session'}{command.intent?.agentId ? ` / ${command.intent.agentId}` : ''}</Label>
      {command.message && <Notice>{command.message}</Notice>}
      <Actions items={[
        ...(!['succeeded', 'failed', 'cancelled', 'interrupted'].includes(command.status) ? [{ label: 'Check delivery', secondary: true, disabled: !state.ready, onPress: () => { void runtime.checkCommand(command).catch(runtime.report); } }] : []),
        ...(command.retryable ? [{ label: 'Retry original request', disabled: !state.ready, secondary: true, onPress: () => { void runtime.retryCommand(command).catch(runtime.report); } }] : []),
        ...(['succeeded', 'failed', 'cancelled', 'interrupted', 'not_found'].includes(command.status) ? [{ label: 'Clear resolved record', secondary: true, onPress: () => { void runtime.forgetCommand(command).catch(runtime.report); } }] : []),
      ]} />
    </Stack>)}
  </Stack><Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Permission decisions</Label>
    {!decisions.items.length && <Label muted>No retained permission decisions for this host.</Label>}
    {decisions.items.map(decision => <Stack key={decision.record.commandId}>
      <Label>{decision.status === 'succeeded' ? 'Decision delivered' : decision.status}</Label>
      <Label muted>{decision.record.rootId} / {decision.intent?.agentId ?? 'Unknown agent'}</Label>
      {decision.message && <Notice>{decision.message}</Notice>}
      {decision.status === 'succeeded' && <Label muted>The tool has its decision. Its execution result appears in the conversation.</Label>}
      <Actions items={['succeeded', 'failed', 'cancelled', 'interrupted', 'not_found'].includes(decision.status)
        ? [{ label: 'Clear resolved decision', secondary: true, onPress: () => { void runtime.decisions.clear(decision.record.commandId).catch(runtime.report); } }]
        : [{ label: 'Check decision delivery', secondary: true, disabled: !state.ready, onPress: () => { void runtime.decisions.check(decision.record.commandId).catch(runtime.report); } }]} />
    </Stack>)}
  </Stack><Notice>Whip mobile connects directly to your private host. Drafts and recovery metadata are stored in an encrypted database on this device. Sessions refresh when the app is active; background notifications are not enabled.</Notice>
    <SavedDrafts />
    <Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Diagnostics</Label>
      <Label muted>Copy app, protocol, connection and storage status. Includes runtime ID; excludes server addresses, prompts, transcripts, drafts and keys.</Label>
      <Actions items={[{ label: 'Copy diagnostics', secondary: true, onPress: () => { void Clipboard.setStringAsync(JSON.stringify(diagnostics(state, Platform.OS, Platform.Version), null, 2)).catch(runtime.report); } }]} />
    </Stack><Actions items={[{ label: 'Reset local data…', secondary: true, onPress: () => Alert.alert('Erase Whip data on this phone?', 'This permanently deletes saved servers, unsent drafts and delivery recovery records on this phone. Work and history on your hosts continue. Copy any drafts you want to keep first.', [{ text: 'Keep data', style: 'cancel' }, { text: 'Erase local data', style: 'destructive', onPress: () => { void resetStorage(); } }]) }]} />
    <RowButton title="Font licenses" onPress={() => router.push('/licenses')} />
    <Label muted style={{ fontSize: 12 }}>Whip mobile 0.1.0</Label>
  </Screen>;
}
