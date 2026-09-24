import { Alert } from 'react-native';
import { Stack as RouterStack, router } from 'expo-router';
import { Monitor, Plus } from 'lucide-react-native';
import { useWorkspace, useWorkspaceState } from '../../runtime/workspace-context';
import { useRuntime } from '../../runtime/context';
import { useTheme } from '../../theme/theme';
import { Button, ListRow, Screen, Section, Stack, StatusBadge, Text } from '../../ui';
export default function HostsScreen() {
  const workspace = useWorkspace(); const state = useWorkspaceState(); const runtime = useRuntime(); const { colors } = useTheme();
  return <><RouterStack.Screen options={{ title: 'Hosts' }} /><Screen><Text variant="title">Your computers.</Text><Text muted>Connect once. Pick up your work from anywhere on your private network.</Text>
    {state.hosts.map(host => { const connection = workspace.runtime(host.id)?.getSnapshot(); return <Section key={host.id}><ListRow title={host.name} detail={host.url} leading={<Monitor size={22} color={colors.muted} />} onPress={() => router.push({ pathname: '/server', params: { hostId: host.id } })} /><StatusBadge label={connection?.connecting ? 'Connecting…' : connection?.ready ? 'Connected' : connection?.client ? 'Reconnecting' : 'Disconnected'} tone={connection?.ready ? 'success' : 'muted'} /><Stack style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 8 }}><Button label={connection?.client ? 'Disconnect' : 'Connect'} variant="secondary" disabled={connection?.connecting} onPress={() => { void (connection?.client ? workspace.disconnect(host.id) : workspace.connect(host)).catch(runtime.report); }} /><Button label="Remove" variant="quiet" onPress={() => Alert.alert(`Remove ${host.name}?`, 'This removes the saved connection. Drafts and recovery records stay on this phone, and work on the host continues.', [{ text: 'Keep', style: 'cancel' }, { text: 'Remove', style: 'destructive', onPress: () => { void workspace.removeHost(host.id).catch(runtime.report); } }])} /></Stack></Section>; })}
    <Button label="Add host" icon={<Plus size={20} color={colors.onPrimary} />} disabled={state.hosts.length >= 4} onPress={() => router.push('/server')} />{state.hosts.length >= 4 && <Text muted>Four hosts are saved. Remove one to add another.</Text>}
  </Screen></>;
}
