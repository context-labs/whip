import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import * as stylex from '@stylexjs/stylex';
import { Button, IconButton, Tooltip, useTheme } from '@whip/ui';
import { validateTheme, validateDisplayPreferences } from '@whip/ui/theme-data';
import { colors, scale } from '@whip/ui/tokens.stylex';
import { Scan, X } from 'lucide-react';
import { useRuntime } from './context';
import type { AppRuntime } from './runtime';
import { isSessionTab } from './session-tabs';
import type { BrowserDesignRecipient } from './browser-design-types';
import { BrowserDesignController, type DesignSubmission } from './browser-design-controller';
import { compositionKey } from './compositions';
import { boundedDesignText } from './browser-design-geometry';
import { submitChatInput } from './chat-submission';
import { designContextSummary } from './browser-design-presentation';

type Recipient = BrowserDesignRecipient & { runtimeId: string; rootId: string; agentId: string; hostId: string };
/** Exact host/root/agent identity. A focused chat is never an implicit destination. */
export function designRecipients(runtime: AppRuntime): Recipient[] {
  const recipients = new Map<string, Recipient>();
  const hosts = runtime.connections.getSnapshot().hosts;
  for (const tab of runtime.tabs.workspace().tabs) {
    if (!isSessionTab(tab)) continue;
    const host = hosts.find(item => item.runtimeId === tab.runtimeId);
    if (!host) continue;
    const agentId = tab.location.agent || tab.rootId;
    const id = JSON.stringify([host.id, tab.runtimeId, tab.rootId, agentId]);
    recipients.set(id, { id, runtimeId: tab.runtimeId, rootId: tab.rootId, agentId, hostId: host.id,
      label: boundedDesignText(`${tab.titleHint || tab.rootId}${agentId !== tab.rootId ? ` / ${agentId}` : ''} · ${host.name}`, 512), available: host.state === 'connected' && !!host.client });
  }
  return [...recipients.values()].slice(0, 100);
}

async function submitDesign(runtime: AppRuntime, tabId: string, input: DesignSubmission) {
  const recipient = designRecipients(runtime).find(item => item.id === input.recipientId && item.available);
  const host = runtime.connections.getSnapshot().hosts.find(item => item.id === recipient?.hostId && item.runtimeId === recipient?.runtimeId && item.state === 'connected');
  if (!recipient || !host?.client || !input.current()) throw new Error('The page or destination changed. Select the elements and destination again.');
  const session = host.client.session(recipient.rootId), surfaceId = `design:${tabId}`;
  const key = compositionKey(recipient.runtimeId, recipient.rootId, recipient.agentId, surfaceId);
  if (runtime.getSnapshot().commands.some(item => item.draftKey === key && item.delivery)) return { accepted: false, uncertain: true, error: 'Check this conversation’s pending delivery before sending again.' };
  // Only this design surface's previously rejected uploads are replaced. Normal drafts stay untouched.
  runtime.compositions.clear(key);
  const files = [new File([input.capture.text], 'browser-design-context.txt', { type: 'text/plain' })];
  if (input.capture.image) {
    const encoded = input.capture.image.slice('data:image/png;base64,'.length);
    const bytes = Uint8Array.from(atob(encoded), value => value.charCodeAt(0));
    files.push(new File([bytes], 'browser-design-viewport.png', { type: 'image/png' }));
  }
  const uploading = runtime.compositions.add(key, session, recipient.runtimeId, recipient.agentId, files, surfaceId);
  const attachmentIds = runtime.compositions.get(key).attachments.map(item => item.id);
  try {
  await uploading;
  if (!input.current()) { runtime.compositions.clear(key, attachmentIds); throw new Error('The page or destination changed during upload. Your prompt was preserved; select the elements again.'); }
  const attachments = runtime.compositions.get(key).attachments.filter(item => attachmentIds.includes(item.id));
  if (attachments.length !== files.length) throw new Error('The evidence upload was cancelled. Your prompt is preserved.');
  const failed = attachments.find(item => item.error || !item.value);
  if (failed) throw new Error(failed.error || 'Evidence upload did not finish. Try again when connected.');
  // The live snapshot also validates that the destination agent still exists before admission.
  const snapshot = await session.snapshot();
  if (!input.current()) { runtime.compositions.clear(key, attachmentIds); throw new Error('The page changed before sending. Select the elements again.'); }
  if (snapshot.root_id !== recipient.rootId || (recipient.agentId !== recipient.rootId && !snapshot.agents?.some(agent => agent.id === recipient.agentId && !agent.terminal_cause))) throw new Error('This conversation recipient is no longer available. Choose another conversation.');
  const result = await submitChatInput({ runtime, session, runtimeId: recipient.runtimeId, agentId: recipient.agentId,
    compositionKey: key, connected: host.client.getSnapshot().state === 'connected', text: input.prompt, attachments,
    designContext: designContextSummary(input.capture.text, attachments[0]!.value!.content.reference_id, attachments[1]?.value?.content.reference_id),
    delivery: input.delivery === 'queue' ? 'queued' : 'steer', activeTurn: snapshot.active_turns[recipient.agentId], onAccepted: input.accepted });
  if (result.status === 'completed') return { accepted: result.accepted };
  if (result.status === 'skipped') return { accepted: false, uncertain: !!result.delivery, error: 'The message was not sent. Check the destination connection and pending delivery.' };
  return { accepted: result.accepted, uncertain: !!result.delivery, error: result.error instanceof Error ? result.error.message : String(result.error) };
  } finally {
    // Rejected uploads are reproducible from the preserved design draft. Retain only
    // unresolved command evidence, which recovery may still admit with its original ID.
    if (!runtime.getSnapshot().commands.some(item => item.draftKey === key && item.delivery)) runtime.compositions.clear(key, attachmentIds);
  }
}

const controllers = new WeakMap<AppRuntime, Map<string, BrowserDesignController>>();
function controllerFor(runtime: AppRuntime, tabId: string) {
  const platform = runtime.platform.browser?.design;
  if (!platform) return undefined;
  let tabs = controllers.get(runtime);
  if (!tabs) { tabs = new Map(); controllers.set(runtime, tabs); }
  // Prune closed tabs without changing another tab's live design draft.
  const open = new Set(runtime.tabs.workspace().tabs.map(tab => tab.id));
  for (const [id, controller] of tabs) if (!open.has(id)) { void controller.stop().catch(runtime.report); tabs.delete(id); }
  let controller = tabs.get(tabId);
  if (!controller) { controller = new BrowserDesignController(platform, input => submitDesign(runtime, tabId, input)); tabs.set(tabId, controller); }
  return controller;
}

export function BrowserDesignControl({ tabId, available }: { tabId: string; available: boolean }) {
  const runtime = useRuntime();
  const controller = useMemo(() => controllerFor(runtime, tabId), [runtime, tabId]);
  if (!controller) return null;
  return <DesignControl controller={controller} tabId={tabId} available={available}/>;
}
function DesignControl({ controller, tabId, available }: { controller: BrowserDesignController; tabId: string; available: boolean }) {
  const runtime = useRuntime(), { resolvedTheme, display, increasedContrast } = useTheme();
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const tabs = useSyncExternalStore(runtime.tabs.subscribe, runtime.tabs.getSnapshot, runtime.tabs.getSnapshot);
  const hosts = useSyncExternalStore(runtime.connections.subscribe, runtime.connections.getSnapshot, runtime.connections.getSnapshot);
  const associations = useSyncExternalStore(runtime.browserAssociations.subscribe, runtime.browserAssociations.getSnapshot, runtime.browserAssociations.getSnapshot);
  const [starting, setStarting] = useState(false), [error, setError] = useState('');
  const active = !!snapshot.state && snapshot.state.status !== 'stopped' && snapshot.state.status !== 'unavailable';
  useEffect(() => {
    const recipients = designRecipients(runtime);
    const associated = associations.filter(item => item.tabId === tabId && item.status === 'selected').map(item => JSON.stringify([item.hostId, item.runtimeId, item.rootId, item.rootId]));
    controller.configure(recipients, associated, { name: resolvedTheme.id, mode: resolvedTheme.dark ? 'dark' : 'light', palette: validateTheme(resolvedTheme), display: validateDisplayPreferences({ ...display, contrast: increasedContrast ? 'more' : 'standard' }) });
  }, [runtime, controller, tabId, tabs, hosts, associations, resolvedTheme, display, increasedContrast]);
  useEffect(() => () => { void controller.stop().catch(error => runtime.report(error)); }, [controller, runtime]);
  const toggling = useRef(false);
  const toggle = useCallback(async (restoreGuestFocus = false) => {
    if (toggling.current) return;
    toggling.current = true;
    setError('');
    try {
      const state = controller.getSnapshot().state;
      if (state && state.status !== 'stopped' && state.status !== 'unavailable') {
        const target = runtime.browser.target(tabId);
        await controller.stop();
        if (restoreGuestFocus && target) await runtime.browser.restoreDesignFocus(target);
      }
      else { const target = runtime.browser.target(tabId); if (!target) throw new Error('This Browser tab is unavailable.'); setStarting(true); await controller.start(target); }
    } catch (error) { setError(error instanceof Error ? error.message : String(error)); }
    finally { toggling.current = false; setStarting(false); }
  }, [controller, runtime, tabId]);
  useEffect(() => runtime.browser.onEvent(event => {
    const target = runtime.browser.target(tabId);
    if (event.kind === 'shortcut' && event.shortcut === 'design-toggle' && event.tabId === tabId &&
      event.epoch === target?.epoch && event.generation === target.generation && available && runtime.browser.canToggleDesign(tabId)) void toggle(true);
  }), [runtime, tabId, available, toggle]);
  return <Tooltip label={error || snapshot.draft.error || (active ? 'Exit Design Mode (⌘⇧D)' : 'Select page elements and describe a change (⌘⇧D)')}>
    {active ? <Button type="button" size="sm" variant="ghost" aria-label="Exit Design Mode" aria-pressed={true} xstyle={styles.active} onClick={() => { void toggle(); }}><Scan size={14}/>Design<X size={12}/></Button>
      : <IconButton type="button" size="sm" variant="ghost" label={starting ? 'Starting Design Mode' : 'Enter Design Mode'} disabled={!available || starting} onClick={() => { void toggle(); }}><Scan size={16}/></IconButton>}
  </Tooltip>;
}
const styles = stylex.create({ active: { color: colors.primary, backgroundColor: 'color-mix(in srgb, currentColor 10%, transparent)', borderRadius: scale.radiusControl, gap: 5 } });
