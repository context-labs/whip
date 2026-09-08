import { router } from 'expo-router';
import { FlashList } from '@shopify/flash-list';
import { Actions, Label, Loading, Notice, RowButton, Screen, Stack } from '../../components/primitives';
import { Connection } from '../../components/connection';
import { useAttention, useAttentionFocus } from '../../features/attention';
export default function AttentionScreen() {
  const attention = useAttention(); useAttentionFocus();
  return <Screen scroll={false}><Connection /><FlashList data={attention.items} keyExtractor={item => item.root_id}
    ListHeaderComponent={<Stack style={{ padding: 16 }}>
      {attention.status !== 'loading' && attention.status !== 'unavailable' && <Label muted>{attention.countLabel} among {attention.items.length} loaded sessions. Ordered by session ID.</Label>}
      {attention.statusMessage && <Notice>{attention.statusMessage}{attention.status === 'stale' ? ' Showing the last observed index.' : ''}</Notice>}
      <Actions items={[{ label: 'Refresh attention', secondary: true, disabled: !attention.enabled || attention.isFetching, onPress: () => { void attention.refresh(); } }]} />
    </Stack>}
    renderItem={({ item }) => <RowButton title={item.title || 'Untitled session'} detail={`${item.questions?.length ?? 0} questions · ${item.pending_permissions} permissions · ${item.active_agents} active agents`}
      onPress={() => router.push({ pathname: '/session/[rootId]', params: { rootId: item.root_id, runtimeId: attention.runtimeId!, requests: 'true' } })} />}
    onRefresh={() => { void attention.refresh(); }} refreshing={attention.isRefreshing}
    ListEmptyComponent={attention.isFetching ? <Loading /> : <Stack style={{ padding: 24 }}><Label>{attention.status === 'current' ? attention.partial ? 'No human requests in this partial index' : 'Nothing needs your attention' : 'Attention is unavailable'}</Label></Stack>}
    ListFooterComponent={<Stack style={{ padding: 16 }}>{attention.partial && <Notice>This is a partial attention index; the count is a lower bound. Open a session for authoritative requests.</Notice>}{attention.atPageLimit && <Notice>Showing at most 256 sessions. Use Sessions to find another root.</Notice>}{attention.hasNextPage && <Actions items={[{ label: 'Load more', disabled: !attention.enabled || attention.isFetching, secondary: true, onPress: () => { void attention.loadMore(); } }]} />}</Stack>} /></Screen>;
}
