import { useEffect, useRef, useState } from 'react';
import { Alert, ScrollView, View } from 'react-native';
import { Stack as RouterStack } from 'expo-router';
import * as DocumentPicker from 'expo-document-picker';
import { File } from 'expo-file-system';
import { themeFromHost } from '@whip/app/presentation';
import { type ThemeDefinition } from '@whip/ui/theme-data';
import { useRuntime, useRuntimeState } from '../../runtime/context';
import { defaultAppearance, nativeTheme, themeCatalog, useTheme, useThemePreview, type Appearance } from '../../theme/theme';
import { Button, ChoiceGroup, ListRow, Notice, PickerList, Screen, Section, Sheet, Stack, Surface, SwitchRow, Text } from '../../ui';
import { Markdown } from '../../components/markdown';

function Swatch({ theme }: { theme: ThemeDefinition }) {
  const { colors } = nativeTheme(theme);
  return <View accessible={false} style={{ width: 60, height: 38, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.background, padding: 5, borderRadius: 6, gap: 4 }}><View style={{ height: 5, width: 30, backgroundColor: colors.foreground, borderRadius: 2 }} /><View style={{ height: 6, width: 22, alignSelf: 'flex-end', backgroundColor: colors.element, borderRadius: 2 }} /><View style={{ height: 5, width: 42, backgroundColor: colors.primary, borderRadius: 2 }} /></View>;
}
export default function AppearanceScreen() {
  const runtime = useRuntime(); const state = useRuntimeState(); const theme = useTheme(); const preview = useThemePreview();
  const [choosing, setChoosing] = useState<'light' | 'dark'>(); const [candidate, setCandidate] = useState<string>();
  const [importing, setImporting] = useState(false); const [hostThemes, setHostThemes] = useState<{ id: string; title: string }[]>(); const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null); const saving = useRef(false);
  const all = [...themeCatalog, ...(state.customThemes ?? [])]; const appearance = { ...defaultAppearance, ...state.appearance };
  useEffect(() => () => { preview(); request.current?.abort(); }, [preview]);
  useEffect(() => { if (!state.active) { request.current?.abort(); request.current = null; setImporting(false); } }, [state.active]);
  const update = async (next: Appearance) => { if (saving.current) return; saving.current = true; setError(''); try { await runtime.setAppearance(next); return true; } catch (e) { setError(String(e instanceof Error ? e.message : e)); return false; } finally { saving.current = false; } };
  const close = () => { preview(); setChoosing(undefined); setCandidate(undefined); };
  async function loadHostThemes() {
    request.current?.abort(); const controller = new AbortController(); request.current = controller; setError('');
    try { const result = await runtime.requireReady().host.themes.list({ signal: controller.signal }); if (!controller.signal.aborted) { setHostThemes((result.themes ?? []).filter(t => t.source !== 'builtin').map(t => ({ id: t.id, title: t.name }))); if (result.errors?.length || result.truncated) setError('Some host themes could not be listed. You can still import a JSON file.'); } }
    catch (e) { if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e)); }
  }
  async function importTheme(id?: string) {
    if (importing) return; request.current?.abort(); const controller = new AbortController(); request.current = controller; setImporting(true); setError('');
    try {
      const client = runtime.requireReady(); const runtimeId = client.requireConnected().runtime_id;
      let resolved;
      if (id) resolved = await client.host.themes.resolve(id, { signal: controller.signal });
      else {
        const result = await DocumentPicker.getDocumentAsync({ type: ['application/json', 'text/plain'], copyToCacheDirectory: true, multiple: false });
        controller.signal.throwIfAborted(); if (result.canceled) return;
        const picked = result.assets[0]; const file = new File(picked.uri);
        try { if (file.size > 64 * 1024) throw new Error('Choose a theme JSON file smaller than 64 KiB.'); const json = await file.text(); if (new TextEncoder().encode(json).length > 64 * 1024) throw new Error('This theme file is too large.'); controller.signal.throwIfAborted(); resolved = await client.host.themes.resolveJSON(json, { signal: controller.signal }); }
        finally { try { file.delete(); } catch { /* OS cache cleanup can retry later. */ } }
      }
      controller.signal.throwIfAborted(); if (runtime.requireReady() !== client) throw new Error('Host changed before theme import completed.');
      const value = themeFromHost(resolved, id ? `host:${runtimeId}` : `import:${client.createId()}`);
      await runtime.addTheme(value); setHostThemes(undefined);
    } catch (e) { if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e)); }
    finally { if (request.current === controller) { setImporting(false); request.current = null; } }
  }
  return <><RouterStack.Screen options={{ title: 'Appearance' }} /><Screen>
    <Text variant="title">Make Whip yours.</Text><Text muted>Choose the same palettes you use on desktop. Appearance stays on this phone.</Text>
    {error && <Notice danger>{error}</Notice>}
    <Section title="APPEARANCE"><ChoiceGroup label="Appearance mode" value={appearance.mode} options={[{ value: 'system', label: 'System' }, { value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }]} onChange={mode => { void update({ ...appearance, mode }); }} />
      {(['light', 'dark'] as const).map(kind => <ListRow key={kind} title={`${kind === 'light' ? 'Light' : 'Dark'} theme`} detail={all.find(t => t.id === appearance[kind])?.name} onPress={() => { setCandidate(appearance[kind]); setChoosing(kind); }} trailing={<Swatch theme={all.find(t => t.id === appearance[kind]) ?? theme} />} />)}
    </Section>
    <Surface><Stack><Text variant="caption" muted>LIVE PREVIEW · {theme.name}</Text><Surface tone="element" style={{ alignSelf: 'flex-end', maxWidth: '90%', borderBottomRightRadius: 6 }}><Text>Make the session list easier to scan.</Text></Surface><Markdown text={'### A quieter workspace\nYour hosts, sessions, and ideas.\n\n```typescript\nconst session = await host.create();\n```'} /><ListRow title="Explored 6 files" detail="Ready for your next idea" /><Button label="Continue" variant="secondary" disabled onPress={() => {}} /></Stack></Surface>
    <Section title="CUSTOM THEMES"><Text muted>Import a Whip JSON theme or choose one from the connected host. Saved imports work offline.</Text><ListRow title="Theme source" detail={state.host?.name ?? 'Connect a host to import'} />
      <Button label="Import theme JSON" variant="secondary" disabled={!state.ready || importing} loading={importing} onPress={() => { void importTheme(); }} /><Button label="Browse host themes" variant="quiet" disabled={!state.ready || importing} onPress={() => { void loadHostThemes(); }} />
      {(state.customThemes ?? []).map(t => <ListRow key={t.id} title={t.name} detail={`${t.dark ? 'Dark' : 'Light'} · Saved on this phone`} trailing={<Swatch theme={t} />} onPress={() => Alert.alert(`Remove ${t.name}?`, 'A selected theme will return to its default. Your other themes stay saved.', [{ text: 'Keep', style: 'cancel' }, { text: 'Remove', style: 'destructive', onPress: () => { void runtime.removeTheme(t.id).catch(e => setError(String(e.message))); } }])} />)}
    </Section>
    <Section title="READING"><ChoiceGroup label="Text size" value={String(appearance.textScale)} options={[{ value: '0.9', label: 'Small' }, { value: '1', label: 'Default' }, { value: '1.15', label: 'Large' }, { value: '1.3', label: 'Larger' }]} onChange={size => { void update({ ...appearance, textScale: Number(size) }); }} /><ChoiceGroup label="Interface font" value={appearance.font!} options={[{ value: 'inter', label: 'Inter' }, { value: 'system', label: 'System' }]} onChange={font => { void update({ ...appearance, font }); }} /><ChoiceGroup label="Code font" value={appearance.codeFont!} options={[{ value: 'jetbrains-mono', label: 'JetBrains Mono' }, { value: 'system', label: 'System mono' }]} onChange={codeFont => { void update({ ...appearance, codeFont }); }} /><ChoiceGroup label="Code size" value={String(appearance.codeSize)} options={[{ value: '12', label: '12' }, { value: '13', label: '13' }, { value: '16', label: '16' }, { value: '20', label: '20' }, { value: '24', label: '24' }]} onChange={size => { void update({ ...appearance, codeSize: Number(size) }); }} /><SwitchRow label="Wrap code" value={appearance.wrapCode!} onChange={wrapCode => { void update({ ...appearance, wrapCode }); }} /><ChoiceGroup label="Tool detail" value={appearance.toolDensity!} options={[{ value: 'compact', label: 'Compact' }, { value: 'comfortable', label: 'Comfortable' }, { value: 'detailed', label: 'Detailed' }]} onChange={toolDensity => { void update({ ...appearance, toolDensity }); }} /></Section>
    <Section title="ACCESSIBILITY"><ChoiceGroup label="Contrast" value={appearance.contrast!} options={[{ value: 'system', label: 'System' }, { value: 'standard', label: 'Standard' }, { value: 'increased', label: 'Increased' }]} onChange={contrast => { void update({ ...appearance, contrast }); }} /><SwitchRow label="Reduce motion" value={appearance.motion === 'reduce'} onChange={reduce => { void update({ ...appearance, motion: reduce ? 'reduce' : 'system' }); }} /><Text variant="caption" muted>Device text scaling and reduced-motion preferences are always respected.</Text></Section>
    <Button label="Reset appearance" variant="quiet" onPress={() => { void update(defaultAppearance); }} />
  </Screen>
  <Sheet title={`${choosing === 'light' ? 'Light' : 'Dark'} theme`} visible={!!choosing} onClose={close} full><PickerList options={all.filter(t => t.dark === (choosing === 'dark')).map(t => ({ id: t.id, title: t.name, detail: t.dark ? 'Dark' : 'Light', trailing: <Swatch theme={t} /> }))} selected={candidate} onSelect={id => { setCandidate(id); preview(id); }} placeholder="Search themes…" /><View style={{ padding: 20 }}><Button label="Apply theme" onPress={() => { if (choosing && candidate) { void update({ ...appearance, [choosing]: candidate }).then(saved => { if (saved) close(); }); } }} /><Button label="Cancel" variant="quiet" onPress={close} /></View></Sheet>
  <Sheet title="Host themes" visible={!!hostThemes} onClose={() => { request.current?.abort(); setHostThemes(undefined); }}><PickerList options={hostThemes ?? []} onSelect={id => { void importTheme(id); }} />{importing && <Text style={{ padding: 20 }}>Importing…</Text>}{error && <Notice danger>{error}</Notice>}</Sheet>
  </>;
}
