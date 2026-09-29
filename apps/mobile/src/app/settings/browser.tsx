import { Stack as RouterStack, router, useLocalSearchParams } from 'expo-router';
import { BrowserSettings } from '../../components/browser-settings';
import { RuntimeScope } from '../../runtime/context';
import { useWorkspace, useWorkspaceState } from '../../runtime/workspace-context';
import { Button, Notice, Screen } from '../../ui';
export default function BrowserRoute() {
  const { hostId } = useLocalSearchParams<{ hostId?: string }>(), workspace = useWorkspace(), state = useWorkspaceState();
  const host = typeof hostId === 'string' ? state.hosts.find(item => item.id === hostId) : undefined;
  const runtime = host && workspace.runtime(host.id);
  return <><RouterStack.Screen options={{ title: 'Browser driver' }} />{runtime ? <RuntimeScope key={host.id} runtime={runtime}><BrowserSettings /></RuntimeScope> : <Screen><Notice>Choose a saved host to inspect its browser settings.</Notice><Button label="Choose host" onPress={() => router.push('/settings/hosts')} /></Screen>}</>;
}
