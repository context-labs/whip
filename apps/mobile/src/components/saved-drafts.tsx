import { Alert } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Label, Stack } from './primitives';
import { savedDraftPresentation } from '../features/saved-drafts';

export function SavedDrafts() {
  const runtime = useRuntime(); const state = useRuntimeState();
  const entries = runtime.draftEntries();
  return <Stack><Label style={{ fontSize: 22, fontWeight: '600' }}>Drafts on this phone</Label>
    <Label muted>{entries.length} of 16 draft slots used. Includes disconnected and removed servers.</Label>
    {entries.map(({ key, value }) => {
      const draft = savedDraftPresentation(key, value.text, state.hosts);
      return <Stack key={key}>
        <Label style={{ fontWeight: '600' }}>{draft.title}</Label>
        <Label muted selectable style={{ fontSize: 12 }}>{draft.detail}</Label>
        <Label numberOfLines={3}>{draft.preview.slice(0, 240)}</Label>
        {draft.note && <Label muted style={{ fontSize: 12 }}>{draft.note}</Label>}
        <Label muted>{runtime.draftStatus(key) === 'saving' ? 'Saving…' : runtime.draftStatus(key) === 'failed' ? 'Not saved. Copy your text or retry discarding it.' : 'Saved on this phone'}</Label>
        <Actions items={[
          { label: 'Copy draft', secondary: true, onPress: () => { void Clipboard.setStringAsync(draft.copyText).catch(runtime.report); } },
          { label: 'Discard draft', secondary: true, onPress: () => Alert.alert('Discard this local draft?', 'This only removes the draft from this phone. It does not cancel any submitted work.', [{ text: 'Keep', style: 'cancel' }, { text: 'Discard', style: 'destructive', onPress: () => { void runtime.discardDraft(key, value.revision).catch(runtime.report); } }]) },
        ]} />
      </Stack>;
    })}
  </Stack>;
}
