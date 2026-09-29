import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import test from 'node:test';
import type { BrowserCommand } from '@whip/protocol';
import type { BrowserAgentScope, BrowserAgentSelection } from '@whip/app/desktop-bridge';
import { BrowserControl } from '../src/browser-control';
import type { BrowserManager } from '../src/browser-manager';

function fixture(preview = false) {
  let serial = 0;
  let ports = [3000];
  let sends = 0;
  let screenshot: Buffer = Buffer.from([255, 216, 255, 217]);
  let delayed: (() => void) | undefined;
  const tabs = [{ id: 'human', generation: 'tab-gen', documentGeneration: 1, url: 'http://127.0.0.1:3000/', title: 'Human page', ...(preview ? { environmentId: 'environment' } : {}) }];
  const debuggerAPI = Object.assign(new EventEmitter(), {
    attached: false, isAttached() { return this.attached; }, attach() { this.attached = true; }, detach() { this.attached = false; },
    async sendCommand(method: string) { sends++; if (method === 'Input.insertText') await new Promise<void>(resolve => { delayed = resolve; }); return method === 'Page.captureScreenshot' ? { data: screenshot.toString('base64') } : {}; },
  });
  const contents = { isDestroyed: () => false, isDevToolsOpened: () => false, debugger: debuggerAPI };
  const manager = {
    snapshot: () => ({ epoch: 'native', tabs }),
    controlledState(target: { tabId: string; generation: string }) { const tab = tabs.find(tab => tab.id === target.tabId && tab.generation === target.generation); if (!tab) throw new Error('stale'); return tab; },
    controlledContents: async () => contents,
    createControlled(input: { id: string; generation: string; url: string; environmentId?: string }) { const tab = { ...input, documentGeneration: 1, title: 'Created page' }; tabs.push(tab); return tab; },
    waitForAdmission: async () => {}, discardUnadmitted() { throw new Error('No failed admission expected'); },
  };
  const previewState = () => ({ environment_id: 'environment', connection_generation: 'env-gen', host_identity: 'runtime', host_id: 'ssh', loopback: '127.0.0.1' as const, ports: [...ports] });
  const events: unknown[] = [];
  const control = new BrowserControl(manager as unknown as BrowserManager, event => events.push(event), {
    prepare: async () => {}, previewState, preview: async () => 'environment', expand: async (_selection, _scope, port) => { ports = [...ports, port].sort((a, b) => a - b); },
  });
  const identity = control.identity();
  const input: BrowserAgentSelection = {
    offer: { version: 2, root_id: 'root', offer_revision: 'offer', desktop_id: identity.desktopId, window_id: identity.windowId, create_profile_id: identity.createProfileId, offered_tabs: identity.tabs, offered_preview_hosts: preview ? [previewState()] : [] },
    provider: { version: 2, provider_id: 'provider', provider_epoch: 'provider-gen' },
  };
  const scope: BrowserAgentScope = { provider_id: 'provider', provider_epoch: 'provider-gen', tab_id: 'human', tab_generation: 'tab-gen', profile_id: identity.tabs[0]!.profile_id, attachment_id: 'attachment', attachment_generation: 'attachment-gen', control_lineage: 'lineage', ...(preview ? { preview: previewState() } : {}) };
  const command = (kind: BrowserCommand['kind'], target = scope, args: unknown = {}, agent = 'root'): BrowserCommand => ({ command_id: `command-${++serial}`, root_id: 'root', agent_id: agent, provider_epoch: 'provider-gen', scope: structuredClone(target), operation_id: 'operation', expected_document: '', deadline_millis: String(Date.now() + 10000), kind, arguments: args });
  return { control, input, scope, command, events, tabs, debuggerAPI, get sends() { return sends; }, finish: () => delayed?.(), screenshot: (bytes: Buffer) => { screenshot = bytes; } };
}

test('native transfer preserves lineage, changes exact holder, and retirement never closes the human page', async () => {
  const f = fixture(); await f.control.select(f.input);
  assert.equal((await f.control.dispatch(f.command('attach'))).error, undefined);
  const child = { ...f.scope, attachment_id: 'child-attachment', attachment_generation: 'child-gen' };
  const invalid = await f.control.dispatch(f.command('transfer', f.scope, { child_agent_id: 'child', attachments: [{ parent_scope: f.scope, child_scope: { ...child, control_lineage: 'foreign' } }] }));
  assert.equal(invalid.error?.kind, 'browser_busy');
  assert.equal((await f.control.dispatch(f.command('transfer', f.scope, { child_agent_id: 'child', attachments: [{ parent_scope: f.scope, child_scope: child }] }))).error, undefined);
  assert.equal((await f.control.dispatch(f.command('begin'))).error?.kind, 'attachment_revoked');
  f.control.retire({ root_id: 'root', provider_id: 'provider', provider_epoch: 'provider-gen', scopes: [f.scope] });
  assert.equal((await f.control.dispatch(f.command('begin', child, {}, 'child'))).error, undefined);
  f.control.retire({ root_id: 'root', provider_id: 'provider', provider_epoch: 'provider-gen', scopes: [child] });
  assert.equal((await f.control.dispatch(f.command('end', child, {}, 'child'))).error?.kind, 'attachment_revoked');
  assert.equal(f.tabs.length, 1); assert.equal(f.debuggerAPI.isAttached(), false); f.control.dispose();
});

test('native revocation interrupts a dispatched effect without replaying it or exposing raw diagnostics', async () => {
  const f = fixture(); await f.control.select(f.input);
  await f.control.dispatch(f.command('attach')); await f.control.dispatch(f.command('begin'));
  const command = f.command('cdp', f.scope, { method: 'Input.insertText', params: { text: 'once' } });
  const pending = f.control.dispatch(command); await new Promise(resolve => setImmediate(resolve));
  assert.equal(f.sends, 1);
  f.control.cancel({ command_id: command.command_id, root_id: 'root', provider_epoch: 'provider-gen', attachment_generation: 'attachment-gen' });
  assert.equal((await pending).error?.kind, 'outcome_unknown');
  f.finish(); assert.equal((await f.control.dispatch(command)).error?.kind, 'outcome_unknown'); assert.equal(f.sends, 1);
  f.control.dispose(); assert.equal(f.tabs.length, 1);
});

test('native screenshots carry only bounded bytes and trusted page metadata', async () => {
  const f = fixture(); await f.control.select(f.input);
  await f.control.dispatch(f.command('attach')); await f.control.dispatch(f.command('begin'));
  const shot = await f.control.dispatch(f.command('cdp', f.scope, { method: 'Page.captureScreenshot', params: { format: 'jpeg' } }));
  assert.deepEqual(shot.screenshotBytes, Uint8Array.from([255, 216, 255, 217])); assert.equal(shot.document_revision, '1'); assert.equal(shot.title, 'Human page');
  f.screenshot(Buffer.alloc((4 << 20) + 1));
  const large = await f.control.dispatch(f.command('cdp', f.scope, { method: 'Page.captureScreenshot', params: { format: 'jpeg' } }));
  assert.equal(large.error?.kind, 'unsupported_operation'); assert.equal(large.screenshotBytes, undefined); f.control.dispose();
});

test('created preview pages retain their creation profile through detach and explicit reattachment', async () => {
  const f = fixture(true); await f.control.select(f.input);
  const created = { ...f.scope, tab_id: 'created', tab_generation: 'created-gen', profile_id: f.input.offer.create_profile_id, attachment_id: 'created-attachment', attachment_generation: 'created-attachment-gen' };
  assert.equal((await f.control.dispatch(f.command('open', created, { url: 'http://127.0.0.1:3000/' }))).error, undefined);
  assert.equal((await f.control.dispatch(f.command('detach', created))).error, undefined);
  const reopened = { ...created, attachment_id: 'reopened', attachment_generation: 'reopened-gen' };
  assert.equal((await f.control.dispatch(f.command('attach', reopened))).error, undefined);
  assert.equal((await f.control.dispatch(f.command('allow_preview_port', reopened, { port: 4000 }))).error, undefined);
  f.control.retire({ root_id: 'root', provider_id: 'provider', provider_epoch: 'provider-gen', scopes: [reopened] });
  const expanded = { ...reopened, preview: { ...reopened.preview!, ports: [3000, 4000] } };
  assert.equal((await f.control.dispatch(f.command('detach', expanded))).error, undefined);
  assert.equal(f.tabs.length, 2); f.control.dispose();
});
