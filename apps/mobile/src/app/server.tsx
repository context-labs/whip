import { useEffect, useRef, useState } from 'react';
import { Keyboard } from 'react-native';
import { router, useLocalSearchParams } from 'expo-router';
import { useRuntime } from '../runtime/context';
import { useWorkspace, useWorkspaceState } from '../runtime/workspace-context';
import { Button, ListRow, Text } from '../ui';
import { serverOrigin } from '../runtime/address';
import { connectionIssue, connectionStages, testConnection, type ConnectionIssue, type ConnectionProgress, type ConnectionTestResult } from '../runtime/connection-test';
import { Actions, Field, Label, Notice, Screen, Stack } from '../components/primitives';

type Attempt = { kind: 'test' | 'connect'; controller: AbortController; hostId?: string };
export default function ServerScreen() {
  const runtime = useRuntime(); const workspace = useWorkspace(); const state = useWorkspaceState();
  const { hostId } = useLocalSearchParams<{ hostId?: string }>();
  const existing = state.hosts.find(host => host.id === hostId);
  const [url, setURL] = useState(existing?.url ?? ''); const [name, setName] = useState(existing?.name ?? '');
  const [pending, setPending] = useState<Attempt['kind']>();
  const [issue, setIssue] = useState<ConnectionIssue>();
  const [progress, setProgress] = useState<ConnectionProgress>();
  const [result, setResult] = useState<ConnectionTestResult>();
  const attempt = useRef<Attempt | undefined>(undefined);
  function cancelAttempt() {
    const current = attempt.current;
    attempt.current = undefined;
    current?.controller.abort();
    if (current?.hostId) void workspace.disconnect(current.hostId).catch(runtime.report);
  }
  useEffect(() => () => cancelAttempt(), [runtime]);
  useEffect(() => {
    if (!state.active && attempt.current?.kind === 'test') {
      cancelAttempt(); setPending(undefined); setProgress(undefined);
      setIssue({ title: 'Connection test paused', message: 'Keep Whip open while testing, then try again.' });
    }
  }, [state.active]);
  let canonical: string | undefined; let invalid: string | undefined;
  try { if (url.trim()) canonical = serverOrigin(url, __DEV__); } catch (error) { invalid = error instanceof Error ? error.message : String(error); }
  function changeURL(value: string) { setURL(value); setIssue(undefined); setResult(undefined); setProgress(undefined); }
  async function run(kind: Attempt['kind']) {
    if (attempt.current || !canonical || !state.active) return;
    Keyboard.dismiss();
    const current: Attempt = { kind, controller: new AbortController() };
    attempt.current = current;
    setPending(kind); setIssue(undefined); setResult(undefined); setProgress(undefined);
    try {
      if (kind === 'test') {
        const saved = existing ?? state.hosts.find(host => host.url === canonical);
        const checked = await testConnection(canonical, { signal: current.controller.signal, expectedRuntimeId: saved?.runtimeId,
          onProgress: update => { if (attempt.current === current) setProgress(update); } });
        if (attempt.current === current) { setResult(checked); setProgress(undefined); }
      } else {
        runtime.clearError();
        const host = existing ? { ...existing, url: canonical, name: name.trim() || existing.name } : workspace.newHost(canonical, name);
        current.hostId = host.id;
        await workspace.connect(host);
        if (attempt.current !== current) return;
        attempt.current = undefined;
        router.replace('/');
      }
    } catch (error) {
      if (attempt.current === current) { setIssue(connectionIssue(error)); setProgress(undefined); }
    } finally {
      if (attempt.current === current) { attempt.current = undefined; setPending(undefined); }
    }
  }
  return <Screen>
    <Stack><Label style={{ fontSize: 24, lineHeight: 30, fontWeight: '600' }}>{existing ? 'Edit host' : 'Connect your host'}</Label>
      <Label muted>Connect this phone and your computer to Tailscale, then enter Whip’s HTTPS address.</Label></Stack>
    <Field label="Server URL" value={url} onChangeText={changeURL} editable={!pending} maxLength={2048} placeholder="https://whip.example.ts.net" autoCapitalize="none" autoCorrect={false} keyboardType="url" textContentType="URL" testID="server-url" />
    <Field label="Name (optional)" value={name} onChangeText={setName} editable={!pending} placeholder="My computer" maxLength={80} />
    {invalid && <Notice danger>{invalid}</Notice>}
    {progress && <Stack accessibilityLiveRegion="polite" style={{ gap: 4 }}>
      {progress.completed.map(stage => <Label key={stage} muted>{connectionStages[stage]} — passed</Label>)}
      <Label>{connectionStages[progress.stage]}…</Label>
    </Stack>}
    {pending === 'connect' && <Notice>Connecting securely and restoring this phone’s saved state…</Notice>}
    {issue && <Stack accessibilityLiveRegion="polite" style={{ gap: 8 }} testID="connection-error">
      <Notice danger>{issue.title}</Notice><Label>{issue.message}</Label>
      {issue.detail && <Label muted selectable style={{ fontSize: 13 }}>Details: {issue.detail}</Label>}
    </Stack>}
    {result && <Notice>Connection test passed. HTTPS, the live connection and session access work. {result.empty ? 'This server has no sessions yet. ' : ''}Tap Connect to use this server. Testing does not save it or run any work.</Notice>}
    <Actions items={[
      { label: pending === 'test' ? 'Testing connection…' : 'Test Connection', secondary: true, disabled: !!pending || !canonical || !state.active, onPress: () => { void run('test'); }, testID: 'test-connection' },
      { label: pending === 'connect' ? 'Connecting…' : existing ? 'Save and connect' : 'Connect', disabled: !!pending || !canonical || !state.active, onPress: () => { void run('connect'); }, testID: 'connect-server' },
      { label: pending ? 'Cancel attempt' : 'Cancel', secondary: true, onPress: () => {
        if (pending) { cancelAttempt(); setPending(undefined); setProgress(undefined); setIssue({ title: 'Connection attempt cancelled', message: 'You can retry when you are ready. Work on the server continues.' }); }
        else router.back();
      } },
    ]} />
    <ListRow title="Need help connecting?" detail="Set up Whip and Tailscale on your computer" onPress={() => router.push('/setup-help')} />
    <Label muted>The base address can also open the web app in Safari. That is expected: mobile uses the API and WebSocket at the same address. Do not add /api/v3/ws.</Label>
    <Notice>Access is controlled by your Tailscale network. Devices allowed to reach this server can direct Whip work. Keep the server private to your tailnet.</Notice>
  </Screen>;
}
