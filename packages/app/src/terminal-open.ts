import type { Client, TerminalInfo } from '@whip/sdk';
import type { AppRuntime } from './runtime';
import type { TerminalTab } from './session-tabs';

/** A route may address a pending local view; that locator is never sent as a
 * native shell handle. Native methods require a non-null terminalId + epoch. */
export const terminalLocator = (tab: TerminalTab) => tab.terminalId ?? tab.id;

// This observes outstanding local sends only. The saved tab remains the recovery
// authority after reload; no promise or remote outcome is restored from here.
const sends = new WeakMap<AppRuntime, { active: Set<string>; listeners: Set<() => void> }>();
function sendState(runtime: AppRuntime) {
  let state = sends.get(runtime);
  if (!state) { state = { active: new Set(), listeners: new Set() }; sends.set(runtime, state); }
  return state;
}
export const terminalOpenInFlight = (runtime: AppRuntime, id: string) => sendState(runtime).active.has(id);
export function subscribeTerminalOpens(runtime: AppRuntime, listener: () => void) {
  const state = sendState(runtime); state.listeners.add(listener);
  return () => { state.listeners.delete(listener); };
}

/** One explicit send after its marker was saved. No catch path repeats open. */
export async function sendTerminalOpen(runtime: AppRuntime, client: Client, tab: TerminalTab, size = { cols: 80, rows: 24 }) {
  const opening = tab.opening;
  const current = runtime.tabs.pendingTerminal(tab.runtimeId);
  if (!opening || current?.id !== tab.id || current.opening?.id !== opening.id || client.runtimeID !== tab.runtimeId ||
      client.processEpoch !== opening.processEpoch || !runtime.connections.isAttached(client)) throw new Error('The terminal open no longer belongs to this host connection.');
  const state = sendState(runtime);
  if (state.active.has(tab.id)) throw new Error('Wait for the outstanding terminal open before inspecting its outcome.');
  state.active.add(tab.id); for (const listener of state.listeners) listener();
  try {
    const result = await client.openTerminal({ cwd: tab.cwd, ...size });
    if (result.process_epoch !== opening.processEpoch || !runtime.connections.isAttached(client)) throw new Error('Reconnect and inspect existing shells before opening another.');
    if (!runtime.tabs.updateTerminal(tab.id, { terminalId: result.id, processEpoch: result.process_epoch, cwd: result.cwd, opening: undefined }, opening.id)) {
      throw new Error('A shell opened after this view changed. Review the host’s existing shells before opening another.');
    }
    return result;
  } finally {
    state.active.delete(tab.id); for (const listener of state.listeners) listener();
  }
}

export async function inspectTerminals(client: Client, signal: AbortSignal): Promise<TerminalInfo[]> {
  const result = await client.listTerminals({ signal });
  signal.throwIfAborted();
  if (result.process_epoch !== client.processEpoch || result.items.some(item => item.process_epoch !== client.processEpoch) ||
      new Set(result.items.map(item => item.id)).size !== result.items.length) throw new Error('The shell list belongs to another host process or repeats an identity.');
  if (result.items.length > 16 || new TextEncoder().encode(JSON.stringify(result)).length > 128 * 1024) throw new Error('The shell list exceeds the inspection limit.');
  return result.items;
}
