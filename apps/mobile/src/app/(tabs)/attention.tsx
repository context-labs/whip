import { router } from 'expo-router';
import { FlashList } from '@shopify/flash-list';
import { useWorkspaceAttention } from '../../features/workspace-index';
import { Button, EmptyState, ListRow, Loading, Notice, Screen, Stack, Text } from '../../ui';
export default function AttentionScreen() {
  const attention = useWorkspaceAttention();
  return <Screen scroll={false}><FlashList data={attention.items} keyExtractor={item => JSON.stringify([item.host.id, item.root_id])} contentContainerStyle={{ padding: 20 }}
    ListHeaderComponent={<Stack style={{ marginBottom: 16 }}><Text variant="title">Needs you</Text><Text muted>Questions and permissions from your connected hosts.</Text>{attention.partial && <Notice>This is a partial view. Some hosts or earlier pages may have more requests.</Notice>}{attention.pages.filter(p => p.error).map(p => <Notice key={p.host.id}>{p.host.name}: {p.error}</Notice>)}</Stack>}
    renderItem={({ item }) => <ListRow title={item.title || 'Untitled session'} detail={`${item.host.name} · ${item.questions?.length ?? 0} questions · ${item.pending_permissions} permissions`} onPress={() => router.push({ pathname: '/session/[rootId]', params: { rootId: item.root_id, runtimeId: item.host.runtimeId!, hostId: item.host.id, requests: 'true' } })} />}
    onRefresh={() => { void attention.refetch(); }} refreshing={attention.isRefetching}
    ListEmptyComponent={attention.isFetching ? <Loading /> : <EmptyState title={attention.enabled ? 'All caught up here' : 'Connect a host'} description={attention.partial ? 'No requests on the loaded pages. Other hosts or pages may still need you.' : attention.enabled ? 'Questions and permission requests will appear here.' : 'Reconnect a host to check its requests.'} />}
    ListFooterComponent={<Stack style={{ paddingTop: 20 }}>{attention.pages.filter(p => p.attention?.has_more && p.attention.next_after_id).map(p => <Button key={p.host.id} label={`Next requests · ${p.host.name}`} variant="quiet" onPress={() => attention.next(p.host.id, p.attention!.next_after_id!)} />)}{attention.hasPrevious && <Button label="First requests" variant="quiet" onPress={attention.first} />}</Stack>} /></Screen>;
}
