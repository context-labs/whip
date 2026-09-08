import { memo, useMemo, useRef } from 'react';
import { Pressable, ScrollView, View } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import { useQuery } from '@tanstack/react-query';
import { useRecyclingState } from '@shopify/flash-list';
import { messagePresentation, type TimelineRow } from '@whip/app/presentation';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Label, Loading, Notice, Stack } from './primitives';
import { PagedText, textPreview } from './paged-text';
import { useTheme } from '../theme/theme';

/** Recycling must reset disclosure state; scrolling can never trigger a body read. */
export const ConversationRow = memo(function ConversationRow({ row, onInspect, onPageChange }: { row: TimelineRow; onInspect(row: TimelineRow): void; onPageChange?(): void }) {
  const runtime = useRuntime(); const theme = useTheme();
  const [expanded, setExpanded] = useRecyclingState(false, [row.id]);
  const [copied, setCopied] = useRecyclingState(false, [row.id]);
  const currentRow = useRef(row.id); currentRow.current = row.id;
  const detail = row.role === 'tool' || row.role === 'reasoning' || row.role === 'mailbox';
  const text = useMemo(() => row.role === 'tool' && row.args ? `${row.args}${row.text ? `\n\n${row.text}` : ''}` : row.text, [row.role, row.args, row.text]);
  const retained = row.body && !row.text ? 'Full message retained on host. Open full message to inspect.' : undefined;
  const empty = retained ?? (row.live ? 'Waiting for output…' : 'No text output was retained.');
  const user = row.role === 'user';
  const heading = `${user ? 'You' : row.label ?? (row.role === 'assistant' ? 'Whip' : row.role)}${row.live ? ' · Live' : ''}${row.delivery ? ` · ${row.delivery}` : ''}`;
  return <Stack style={{ paddingHorizontal: 16, paddingVertical: user ? 12 : 16, gap: 6, marginVertical: 8,
    ...(user ? { backgroundColor: theme.colors.panel, borderRadius: 16, alignSelf: 'flex-end', maxWidth: '92%', marginRight: 16, marginLeft: 24 } : {}) }}>
    <Label muted style={{ fontSize: 12, fontWeight: '600' }}>{textPreview(heading)}</Label>
    {detail && !expanded ? <Label muted numberOfLines={2} testID="collapsed-message-preview">{textPreview(row.text || row.args || empty)}</Label>
      : text ? <><PagedText text={text} identity={row.id} source={row.role === 'tool'} onPageChange={onPageChange} />{retained && <Notice>{retained}</Notice>}</>
        : <Notice>{empty}</Notice>}
    {!!row.images?.length && <Notice>{row.images.length} image attachment{row.images.length === 1 ? '' : 's'} available in the web app.</Notice>}
    <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 16 }}>
      {detail && <Pressable accessibilityRole="button" onPress={() => setExpanded(!expanded)} style={{ minHeight: 44, justifyContent: 'center' }}><Label muted style={{ fontSize: 12 }}>{expanded ? 'Collapse details' : 'Show details'}</Label></Pressable>}
      {row.body && <Pressable accessibilityRole="button" onPress={() => onInspect(row)} style={{ minHeight: 44, justifyContent: 'center' }}><Label muted style={{ fontSize: 12 }}>Open full message</Label></Pressable>}
      <Pressable accessibilityRole="button" accessibilityLabel="Copy message" accessibilityState={{ disabled: !text }} disabled={!text}
        onPress={() => { void Clipboard.setStringAsync(text).then(() => { if (currentRow.current === row.id) setCopied(true); }).catch(runtime.report); }} style={{ minHeight: 44, justifyContent: 'center' }}><Label muted style={{ fontSize: 12 }}>{copied ? 'Copied' : 'Copy'}</Label></Pressable>
    </View>
  </Stack>;
});

/** A single sheet owns the only explicit content body query (256 KiB maximum). */
export function BodyInspector({ row, rootId, agentId }: { row: TimelineRow; rootId: string; agentId: string }) {
  const runtime = useRuntime(); const { client, host, ready, active } = useRuntimeState();
  const scroll = useRef<ScrollView>(null);
  const result = useQuery({ queryKey: [host?.runtimeId, rootId, agentId, 'body', row.body?.reference_id], enabled: !!row.body && ready && active && !!client,
    queryFn: ({ signal }) => client!.content(row.body!, { rootId, agentId }).readJSON({ maxBytes: 256 << 10, signal }),
  });
  const text = result.data && typeof result.data === 'object' && 'content' in result.data ? messagePresentation(result.data.content).text : undefined;
  return <ScrollView ref={scroll} contentContainerStyle={{ padding: 20, gap: 16 }}><Label style={{ fontSize: 24, fontWeight: '600' }}>Full message</Label>
    {result.isFetching ? <Loading /> : result.error ? <Notice danger>{textPreview(result.error.message)}</Notice> : text !== undefined ? <><PagedText text={text} identity={JSON.stringify([host?.runtimeId, rootId, agentId, row.id, row.body?.reference_id])} onPageChange={() => scroll.current?.scrollTo({ y: 0, animated: false })} /><Actions items={[{ label: 'Copy message', secondary: true, onPress: () => { void Clipboard.setStringAsync(text).catch(runtime.report); } }]} /></> : <Notice>{!ready ? 'Reconnect to read this message.' : 'The retained message body is unavailable.'}</Notice>}
    <Label muted>Content reads are limited to 256 KiB and verified against the host’s content hash.</Label>
  </ScrollView>;
}
