import { Stack as RouterStack } from 'expo-router';
import { useWorkspace, useWorkspaceState } from '../../runtime/workspace-context';
import { RuntimeScope } from '../../runtime/context';
import RecoverySettings from '../../components/recovery-settings';
import { ListRow, Screen, Text } from '../../ui';
import { useState } from 'react';
import { View } from 'react-native';
export default function ActivityScreen() {
  const workspace = useWorkspace(); const state = useWorkspaceState(); const [hostId, setHostId] = useState(state.selectedHostId);
  const runtime = workspace.runtime(hostId) ?? workspace.settings;
  return <><RouterStack.Screen options={{ title: 'Drafts & recovery' }} />{state.connections.length > 1 && <View style={{ paddingHorizontal: 20 }}><Text muted>Activity host</Text>{state.connections.map(r => <ListRow key={r.getSnapshot().host?.id} title={r.getSnapshot().host?.name ?? 'Connecting'} selected={r === runtime} onPress={() => setHostId(r.getSnapshot().host?.id)} />)}</View>}<RuntimeScope runtime={runtime}><RecoverySettings /></RuntimeScope></>;
}
