import '../runtime/polyfills';
import { useEffect, useRef, useState } from 'react';
import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { Alert, AppState, Pressable, Text, View } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { KeyboardProvider } from 'react-native-keyboard-controller';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { openMobileStorage, resetMobileStorage } from '../runtime/storage';
import { ResetContext } from '../runtime/reset-context';
import { MobileWorkspace } from '../runtime/workspace';
import { WorkspaceProvider } from '../runtime/workspace-context';
import { RuntimeProvider, useRuntime, useRuntimeState } from '../runtime/context';
import { useTheme } from '../theme/theme';
import { Actions, Notice } from '../components/primitives';
import { loadFonts } from '../theme/fonts';

// One bootstrap per JS process, including React StrictMode effect probes.
let bootRuntime: MobileWorkspace | undefined;
let bootDisposed = false;
const appState = AppState.addEventListener('change', state => { if (state !== 'inactive') bootRuntime?.setActive(state === 'active'); });
const createRuntime = () => Promise.all([openMobileStorage(), loadFonts()]).then(async ([storage]) => {
  if (bootDisposed) { await storage.close(); throw new Error('Bootstrap replaced'); }
  const runtime = new MobileWorkspace(storage); bootRuntime = runtime;
  runtime.setActive(AppState.currentState !== 'background');
  await runtime.start();
  return runtime;
});
const hot = (module as typeof module & { hot?: { dispose(callback: () => void): void } }).hot;
const development = globalThis as typeof globalThis & { __whipMobileCleanup?: Promise<unknown> };
// Fast Refresh must finish closing the old SQLCipher connection before opening
// another runtime. React's effect cleanup cannot serialize module replacement.
let bootstrap = Promise.resolve(development.__whipMobileCleanup).then(createRuntime);
hot?.dispose(() => {
  bootDisposed = true; appState.remove();
  development.__whipMobileCleanup = Promise.all([bootstrap.catch(() => {}), bootRuntime?.dispose()]);
});
void bootstrap.catch(() => {});
export default function RootLayout() {
  const [runtime, setRuntime] = useState<MobileWorkspace>();
  const [error, setError] = useState<string>();
  const resetting = useRef(false);
  async function reset() {
    if (resetting.current) return;
    resetting.current = true; setError(undefined); setRuntime(undefined);
    try {
      await bootRuntime?.dispose(); bootRuntime = undefined;
      await resetMobileStorage('erase-local-whip-data');
      bootstrap = createRuntime();
      setRuntime(await bootstrap);
    } catch (failure) { setError(failure instanceof Error ? failure.message : String(failure)); }
    finally { resetting.current = false; }
  }
  useEffect(() => { let live = true; void bootstrap.then(r => { if (live) { setError(undefined); setRuntime(r); } }, e => { if (live) { setRuntime(undefined); setError(e instanceof Error ? e.message : String(e)); } }); return () => { live = false; }; }, [bootstrap]);
  if (!runtime) return <View style={{ flex: 1, justifyContent: 'center', padding: 32, backgroundColor: '#141414' }}><Text style={{ color: '#eeeeee', fontSize: 18 }}>{error ? `Whip could not open secure storage. ${error}` : resetting.current ? 'Resetting local Whip data…' : 'Opening Whip…'}</Text>{error && <>
    <Text style={{ color: '#bbbbbb', marginTop: 16 }}>Whip has kept the remaining local files. Reopen after resolving storage access, or explicitly erase local data to start over.</Text>
    <Pressable accessibilityRole="button" style={{ minHeight: 48, paddingVertical: 16 }} onPress={() => Alert.alert('Erase Whip data on this phone?', 'This permanently deletes saved servers, unsent drafts and recovery records on this phone. Work and history on your Whip hosts continue. Interrupted resets must be finished before Whip can reopen.', [{ text: 'Keep data', style: 'cancel' }, { text: 'Erase local data', style: 'destructive', onPress: () => { void reset(); } }])}><Text style={{ color: '#eeeeee', fontSize: 16 }}>Reset local data…</Text></Pressable>
  </>}</View>;
  return <GestureHandlerRootView style={{ flex: 1 }}><SafeAreaProvider><KeyboardProvider><ResetContext.Provider value={reset}><WorkspaceProvider workspace={runtime}><RuntimeProvider runtime={runtime.settings}><Navigation /></RuntimeProvider></WorkspaceProvider></ResetContext.Provider></KeyboardProvider></SafeAreaProvider></GestureHandlerRootView>;
}
function Navigation() {
  const theme = useTheme(); const runtime = useRuntime(); const state = useRuntimeState();
  return <View style={{ flex: 1, backgroundColor: theme.colors.background }}>
    <StatusBar style={theme.dark ? 'light' : 'dark'} />
    <Stack screenOptions={{ headerStyle: { backgroundColor: theme.colors.background }, headerTintColor: theme.colors.foreground, contentStyle: { backgroundColor: theme.colors.background }, headerShadowVisible: false, headerBackTitle: 'Back' }}>
      <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
      <Stack.Screen name="server" options={{ title: 'Connect to Whip', presentation: 'modal' }} />
      <Stack.Screen name="new-session" options={{ title: 'New session', presentation: 'modal' }} />
      <Stack.Screen name="session" options={{ headerShown: false }} />
    </Stack>
    {state.error && <View style={{ padding: 12 }}><Notice danger>{state.error}</Notice><Actions items={[{ label: 'Dismiss message', secondary: true, onPress: runtime.clearError }]} /></View>}
  </View>;
}
