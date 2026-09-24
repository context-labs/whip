import { useEffect, useState } from 'react';
import { Redirect, Stack as RouterStack, useLocalSearchParams } from 'expo-router';
import { ScrollView, View } from 'react-native';
import { KeyboardStickyView } from 'react-native-keyboard-controller';
import { NativeTheme, defaultAppearance, themeCatalog } from '../theme/theme';
import { Button, ChoiceGroup, EmptyState, ListRow, Notice, PickerList, Screen, Section, Sheet, Stack, StatusBadge, Surface, Text, TextField } from '../ui';
import { StatusBar } from 'expo-status-bar';
import { Composer } from '../components/composer';
import { Markdown } from '../components/markdown';
export default function Gallery() {
  const { theme: selected } = useLocalSearchParams<{ theme?: string }>();
  const [id, setId] = useState(selected ?? 'claude-code'); const [sheet, setSheet] = useState(false); const [draft, setDraft] = useState('');
  useEffect(() => { if (selected && themeCatalog.some(t => t.id === selected)) setId(selected); }, [selected]);
  if (!__DEV__) return <Redirect href="/" />;
  const theme = themeCatalog.find(t => t.id === id) ?? themeCatalog[0];
  return <NativeTheme appearance={{ ...defaultAppearance, mode: theme.dark ? 'dark' : 'light', [theme.dark ? 'dark' : 'light']: theme.id }}><StatusBar style={theme.dark ? 'light' : 'dark'} /><RouterStack.Screen options={{ title: 'Component gallery', headerStyle: { backgroundColor: theme.colors.background }, headerTintColor: theme.colors.foreground }} /><Screen scroll={false}><ScrollView contentContainerStyle={{ padding: 20, gap: 24 }} keyboardShouldPersistTaps="handled"><Text variant="title">Whip, in your colors.</Text><Text muted>{theme.name} · Native component gallery</Text><Button label="Choose theme" onPress={() => setSheet(true)} /><Surface><Stack><StatusBadge label="Connected" tone="success" /><StatusBadge label="Needs your input" tone="warning" /><Text variant="heading">A quieter workspace</Text><Text>Readable conversations, thoughtful details, and your hosts within reach.</Text><ListRow title="Polish mobile navigation" detail="Mac mini · whip · Just now" onPress={() => {}} /><Markdown text={'## Ready when you are\nA **clear conversation**, with [context](https://example.com).\n\n```typescript\nconst message = "Hello, Whip";\n```'} /></Stack></Surface><TextField label="Host address" placeholder="your-host.example.ts.net" /><TextField label="Validation" error="Enter a valid HTTPS address." value="not an address" /><Button label="New session" onPress={() => {}} /><Button label="Secondary action" variant="secondary" onPress={() => {}} /><Button label="Connecting…" loading onPress={() => {}} /><Notice danger>Connection unavailable. Your draft is saved.</Notice></ScrollView><KeyboardStickyView><View style={{ margin: 12 }}><Composer value={draft} onChangeText={setDraft} disabled={!draft.trim()} offline={false} model="Host default" onOptions={() => setSheet(true)} onSend={() => setDraft('')} /></View></KeyboardStickyView></Screen><Sheet title="Theme gallery" visible={sheet} onClose={() => setSheet(false)} full><PickerList options={themeCatalog.map(t => ({ id: t.id, title: t.name, detail: t.dark ? 'Dark' : 'Light' }))} selected={id} onSelect={next => { setId(next); setSheet(false); }} /></Sheet></NativeTheme>;
}
