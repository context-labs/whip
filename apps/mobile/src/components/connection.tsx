import { router } from 'expo-router';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Label, Notice, Stack } from './primitives';
export function Connection() {
  const runtime = useRuntime(); const { host, ready, connecting, active } = useRuntimeState();
  return <Stack style={{ padding: 16, gap: 8 }}>
    {host ? <><Label muted style={{ fontSize: 13 }}>{host.name} · {connecting ? 'Connecting' : ready ? 'Connected' : active ? 'Unavailable' : 'Paused'}</Label>{!ready && <Notice>Your host keeps working. Reconnect to refresh and send.</Notice>}</> : <><Label muted>Direct your Whip sessions from your phone.</Label><Actions items={[{ label: 'Connect a server', onPress: () => router.push('/server') }]} /></>}
    {host && !ready && !connecting && <Actions items={[{ label: 'Reconnect', secondary: true, onPress: () => { void runtime.reconnect().catch(runtime.report); } }, { label: 'Test Connection…', secondary: true, onPress: () => router.push({ pathname: '/server', params: { hostId: host.id } }) }]} />}
  </Stack>;
}
