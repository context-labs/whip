import { memo, useEffect, useMemo } from 'react';
import { Platform, Pressable, View } from 'react-native';
import { useRecyclingState } from '@shopify/flash-list';
import { fonts } from '../theme/fonts';
import { Label, Stack } from './primitives';
import { Markdown } from './markdown';

export const TEXT_PAGE_SIZE = 8192;
export const TEXT_PREVIEW_SIZE = 512;

/** Leave a surrogate pair together without scanning or copying the whole string. */
function boundary(text: string, end: number) {
  const before = text.charCodeAt(end - 1); const after = text.charCodeAt(end);
  return before >= 0xd800 && before <= 0xdbff && after >= 0xdc00 && after <= 0xdfff ? end - 1 : end;
}

export function textPageStarts(text: string): number[] {
  const starts = [0];
  for (let start = 0; start + TEXT_PAGE_SIZE < text.length;) {
    start = boundary(text, start + TEXT_PAGE_SIZE);
    starts.push(start);
  }
  return starts;
}

export function textPreview(text: string): string {
  return text.length <= TEXT_PREVIEW_SIZE ? text : `${text.slice(0, boundary(text, TEXT_PREVIEW_SIZE - 1))}…`;
}

/** Only the selected bounded slice crosses the native text/Markdown boundary. */
export const PagedText = memo(function PagedText({ text, identity, source = false, onPageChange }: {
  text: string; identity: string; source?: boolean; onPageChange?(): void;
}) {
  const starts = useMemo(() => textPageStarts(text), [text]);
  const [selected, setSelected] = useRecyclingState(0, [identity]);
  const page = Math.min(selected, starts.length - 1);
  useEffect(() => { if (selected !== page) setSelected(page); }, [selected, page, setSelected]);
  const paged = starts.length > 1;
  const visible = text.slice(starts[page], starts[page + 1]);
  return <Stack style={{ gap: 8 }}>
    {paged && <Label muted style={{ fontSize: 12 }}>Source text · Page {page + 1} of {starts.length}</Label>}
    {source || paged
      ? <Label selectable testID="source-text-page" style={{ fontFamily: fonts.mono ?? (Platform.OS === 'ios' ? 'Menlo' : 'monospace'), fontSize: 13 }}>{visible}</Label>
      : <Markdown text={visible} />}
    {paged && <View style={{ flexDirection: 'row', gap: 24 }}>
      <Pressable accessibilityRole="button" accessibilityLabel="Previous text page" accessibilityState={{ disabled: page === 0 }} disabled={page === 0}
        onPress={() => { setSelected(page - 1); onPageChange?.(); }} style={{ minHeight: 44, justifyContent: 'center' }}><Label muted>Previous</Label></Pressable>
      <Pressable accessibilityRole="button" accessibilityLabel="Next text page" accessibilityState={{ disabled: page === starts.length - 1 }} disabled={page === starts.length - 1}
        onPress={() => { setSelected(page + 1); onPageChange?.(); }} style={{ minHeight: 44, justifyContent: 'center' }}><Label muted>Next</Label></Pressable>
    </View>}
  </Stack>;
});
