import { useEffect, useState } from 'react';
import { router } from 'expo-router';
import { FlashList } from '@shopify/flash-list';
import { useQuery } from '@tanstack/react-query';
import { useIsFocused } from 'expo-router';
import { useRuntime, useRuntimeState, useSessionList } from '../../runtime/context';
import { Actions, Field, Label, Loading, Notice, RowButton, Screen, Stack } from '../../components/primitives';
import { Connection } from '../../components/connection';

export default function SessionsScreen() {
  const runtime = useRuntime(); const { client, host, list, ready, active } = useRuntimeState();
  const catalog = useSessionList(); const focused = useIsFocused();
  const [text, setText] = useState(''); const [search, setSearch] = useState('');
  useEffect(() => { const timer = setTimeout(() => setSearch(text.trim()), 300); return () => clearTimeout(timer); }, [text]);
  const result = useQuery({ queryKey: [host?.runtimeId, 'sessions.search', search], enabled: !!search && ready && active && focused && !!client,
    queryFn: ({ signal }) => client!.sessions.list({ search, limit: 128, max_bytes: 256 << 10 }, { signal }) });
  const page = search ? result.data : catalog.page;
  const error = search ? result.error : catalog.error;
  return <Screen scroll={false}><Connection />
    {host && <Stack style={{ paddingHorizontal: 16, paddingBottom: 12 }}>
      <Actions items={[{ label: 'New session', disabled: !ready, onPress: () => router.push('/new-session') }]} />
      <Field label="Search sessions" value={text} onChangeText={value => { setText(value); if (!value) setSearch(''); }} maxLength={256} placeholder="Title or working directory" returnKeyType="search" onSubmitEditing={() => setSearch(text.trim())} />
    </Stack>}
    {error && <Notice danger>{error.message}</Notice>}
    <FlashList data={page?.items ?? []} keyExtractor={item => item.id} onRefresh={() => { if (ready) void (search ? result.refetch() : list?.refresh().catch(runtime.report)); }} refreshing={search ? result.isRefetching : catalog.status === 'loading'}
      renderItem={({ item }) => <RowButton title={item.title || 'Untitled session'} detail={`${item.cwd}\n${item.model || 'Host default'} · ${new Date(item.updated_at).toLocaleDateString()}`}
        onPress={() => router.push({ pathname: '/session/[rootId]', params: { rootId: item.id, runtimeId: host!.runtimeId! } })} />}
      ListEmptyComponent={host ? catalog.status === 'loading' || result.isFetching ? <Loading /> : <Stack style={{ padding: 24 }}><Label>{!ready || error ? 'Sessions are unavailable' : search ? 'No matching sessions' : 'No sessions yet'}</Label><Label muted>{!ready ? 'Connect to this host to refresh its sessions.' : error ? 'Refresh after the connection recovers.' : search ? 'Try a different title or directory.' : 'Start a session on this host to begin.'}</Label></Stack> : null}
      ListFooterComponent={<Stack style={{ padding: 16 }}>{(page?.has_more || catalog.truncated) && <Notice>{search ? 'Showing the first 128 matches. Narrow the search to see more.' : catalog.truncated ? 'The list has reached its memory limit. Refresh to start a new window.' : 'More sessions are available.'}</Notice>}{!search && page?.has_more && <Actions items={[{ label: 'Load more sessions', disabled: !ready || catalog.truncated, secondary: true, onPress: () => { void list?.loadMore().catch(runtime.report); } }]} />}</Stack>} />
  </Screen>;
}
