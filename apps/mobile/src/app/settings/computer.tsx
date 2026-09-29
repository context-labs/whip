import { Stack as RouterStack, router, useLocalSearchParams } from 'expo-router';
import { ComputerSettings } from '../../components/computer-settings';
import { RuntimeScope } from '../../runtime/context';
import { useWorkspace, useWorkspaceState } from '../../runtime/workspace-context';
import { Button, Notice, Screen } from '../../ui';
export default function ComputerRoute() {
  const { hostId } = useLocalSearchParams<{ hostId?: string }>(), workspace = useWorkspace(), state = useWorkspaceState();
  const host = typeof hostId === 'string' ? state.hosts.find(item => item.id === hostId) : undefined;
  const runtime = host && workspace.runtime(host.id);
  return <><RouterStack.Screen options={{ title: 'Computer setup' }} />{runtime ? <RuntimeScope key={host.id} runtime={runtime}><ComputerSettings /></RuntimeScope> : <Screen><Notice>Choose a saved host to inspect its computer setup.</Notice><Button label="Choose host" onPress={() => router.push('/settings/hosts')} /></Screen>}</>;
}
