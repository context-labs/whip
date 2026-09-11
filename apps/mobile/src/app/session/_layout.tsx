import { Stack, router, useLocalSearchParams } from 'expo-router';
import { useWorkspace, useWorkspaceState } from '../../runtime/workspace-context';
import { RuntimeScope } from '../../runtime/context';
import { EmptyState, Screen } from '../../ui';
export default function SessionLayout() {
  const { runtimeId = '', hostId } = useLocalSearchParams<{ runtimeId: string; hostId?: string }>();
  const workspace = useWorkspace(); useWorkspaceState(); const runtime = workspace.sessionRuntime(runtimeId, hostId);
  if (!runtime) return <Screen scroll={false}><EmptyState title="Reconnect this host" description="This conversation belongs to a host that is not connected. Your drafts and delivery records are still on this phone." action={{ label: 'Manage hosts', onPress: () => router.push('/settings/hosts') }} /></Screen>;
  return <RuntimeScope runtime={runtime}><Stack screenOptions={{ headerShown: false }} /></RuntimeScope>;
}
