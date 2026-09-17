import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import type { BrowserWindow } from 'electron';
import type { BrowserCommand } from '@whip/protocol';
import type { BrowserAgentIdentity, BrowserAgentScope, BrowserAgentResult, BrowserInventory, BrowserAgentEvent } from '@whip/app/desktop-bridge';
import { BrowserManager } from '../src/browser-manager';
import { BrowserControl } from '../src/browser-control';
import { installBrowserIPC } from '../src/browser-ipc';

/** Actual production preload/IPC/control/debugger path, not an acceptance substitute for the daemon/SDK. */
export async function testBrowserControl(window: BrowserWindow, origin: string) {
  let control: BrowserControl;
  const events: BrowserAgentEvent[] = [];
  const manager = new BrowserManager(window, event => { control?.observe(event); window.webContents.send('whip:browser:event', event); }, {
    admissionTimeoutMs: 150, invalidateControl: (id, reason) => control?.invalidate(id, reason),
  });
  control = new BrowserControl(manager, event => { events.push(event); window.webContents.send('whip:browser-agent:event', event); });
  const cleanup = installBrowserIPC(window, manager, event => {
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || event.senderFrame.url !== origin + '/shell') throw new Error('Untrusted browser request');
  }, control);
  const call = <T>(method: string, value?: unknown): Promise<T> => window.webContents.executeJavaScript(`whipDesktop.browserAgent.${method}(${value === undefined ? '' : JSON.stringify(value)})`);
  try {
    assert.match(await window.webContents.mainFrame.frames[0].executeJavaScript('whipDesktop.browserAgent.identity().then(()=>"bad",e=>String(e))'), /Untrusted/);
    await window.webContents.executeJavaScript(`window.acceptBrowserAdmission = true; window.stopAdmissions = whipDesktop.browserAgent.onEvent(event => {
      if (event.kind === 'admission' && window.acceptBrowserAdmission) void whipDesktop.browser.admitted({ epoch: event.epoch, tabId: event.tab.id, generation: event.tab.generation });
    }); void 0;`);
    const identity = await call<BrowserAgentIdentity>('identity');
    const rootId = randomUUID(), agentId = randomUUID(), provider = { version: 1, provider_id: randomUUID(), provider_epoch: randomUUID() };
    const offer = { version: 1, root_id: rootId, desktop_id: identity.desktopId, window_id: identity.windowId, create_profile_id: identity.createProfileId,
      offer_revision: randomUUID(), offered_tabs: [], offered_preview_hosts: [] };
    await assert.rejects(call('select', { offer: { ...offer, desktop_id: 'different' }, provider }), /identity changed/);
    await call('select', { offer, provider });
    let scope: BrowserAgentScope = { provider_id: provider.provider_id, provider_epoch: provider.provider_epoch, tab_id: randomUUID(), tab_generation: randomUUID(),
      profile_id: identity.createProfileId, attachment_id: randomUUID(), attachment_generation: randomUUID(), rights: ['create', 'control'] };
    let operationId = randomUUID(), holder = agentId;
    const command = (kind: string, args: unknown = {}): BrowserCommand => ({ command_id: randomUUID(), operation_id: operationId, root_id: rootId, agent_id: holder,
      provider_epoch: provider.provider_epoch, deadline_millis: String(Date.now() + 5000), kind, scope: structuredClone(scope), arguments: args });
    const dispatch = (kind: string, args: unknown = {}) => call<BrowserAgentResult>('dispatch', command(kind, args));
    const opened = await dispatch('open', { url: origin + '/control' }); assert.equal(opened.error, undefined, 'open result');
    assert.equal(manager.snapshot().tabs[0].id, scope.tab_id); assert.equal(manager.snapshot().tabs[0].generation, scope.tab_generation);
    const target = { epoch: manager.snapshot().epoch, tabId: scope.tab_id, generation: scope.tab_generation };
    const guest = await manager.controlledContents(target);
    const duplicate = command('begin'); assert.equal((await call<BrowserAgentResult>('dispatch', duplicate)).error, undefined);
    assert.equal((await call<BrowserAgentResult>('dispatch', duplicate)).error?.kind, 'outcome_unknown');
    const answer = await dispatch('cdp', { method: 'Runtime.evaluate', params: { expression: '6 * 7', returnByValue: true } });
    assert.equal((answer.result as { result: { value: number } }).result.value, 42);
    assert.equal((await dispatch('cdp', { method: 'Target.getTargets', params: {} })).error?.kind, 'unsupported_operation');
    assert.ok((await dispatch('cdp', { method: 'Network.getAllCookies', params: {} })).error);
    const screenshot = await dispatch('cdp', { method: 'Page.captureScreenshot', params: { format: 'jpeg', quality: 50 } });
    assert.equal(screenshot.error, undefined, 'screenshot result'); assert.ok(screenshot.screenshotBytes instanceof Uint8Array); assert.ok(screenshot.screenshotBytes.length > 100);
    console.log('PASS production BrowserControl initial admission, bounded hidden viewport screenshot and method denials');
    const long = command('cdp', { method: 'Runtime.evaluate', params: { expression: 'new Promise(r=>setTimeout(()=>{window.finishedAfterCancel=true;r(1)},150))', awaitPromise: true, returnByValue: true } });
    const pending = call<BrowserAgentResult>('dispatch', long); await new Promise(resolve => setTimeout(resolve, 25));
    await call('cancel', { command_id: long.command_id, root_id: rootId, provider_epoch: provider.provider_epoch, attachment_generation: scope.attachment_generation, reason: 'Test cancellation' });
    assert.equal((await pending).error?.kind, 'outcome_unknown'); await new Promise(resolve => setTimeout(resolve, 200));
    for (let attempt = 0; attempt < 30 && !await guest.executeJavaScript('window.finishedAfterCancel === true'); attempt++) await new Promise(resolve => setTimeout(resolve, 100));
    assert.equal(await guest.executeJavaScript('window.finishedAfterCancel'), true, 'delivered cancellation side effect');
    assert.equal((await dispatch('end')).error, undefined);
    operationId = randomUUID(); assert.equal((await dispatch('begin')).error, undefined);
    const navigated = await dispatch('cdp', { method: 'Page.navigate', params: { url: origin + '/control-next' } }); assert.equal(navigated.error, undefined);
    await new Promise(resolve => setTimeout(resolve, 100));
    assert.ok(events.some(event => event.kind === 'provider' && event.event.kind === 'document' && event.event.operation_id === operationId));
    const expected = command('cdp', { method: 'Runtime.evaluate', params: { expression: '1' } }); expected.expected_document = 'stale';
    assert.equal((await call<BrowserAgentResult>('dispatch', expected)).error?.kind, 'stale_document');
    assert.equal((await dispatch('end')).error, undefined);
    operationId = randomUUID(); assert.equal((await dispatch('begin')).error, undefined);
    await guest.loadURL(origin + '/human-navigation');
    assert.ok(events.some(event => event.kind === 'provider' && event.event.kind === 'document' && !event.event.operation_id));
    assert.equal((await dispatch('cdp', { method: 'Runtime.evaluate', params: { expression: '1' } })).error?.kind, 'attachment_revoked');
    assert.equal((await dispatch('end')).error, undefined);
    const parent = structuredClone(scope), childId = randomUUID(), child = { ...scope, attachment_id: randomUUID(), attachment_generation: randomUUID() };
    const badMove = await dispatch('transfer', { child_agent_id: childId, attachments: [{ parent_scope: parent, child_scope: child }, { parent_scope: { ...parent, tab_id: randomUUID() }, child_scope: child }] });
    assert.ok(badMove.error);
    assert.equal((await dispatch('transfer', { child_agent_id: childId, attachments: [{ parent_scope: parent, child_scope: child }] })).error, undefined);
    assert.equal((await dispatch('begin')).error?.kind, 'attachment_revoked');
    scope = child; holder = childId; operationId = randomUUID(); assert.equal((await dispatch('begin')).error, undefined);
    assert.equal((await dispatch('cdp', { method: 'Runtime.evaluate', params: { expression: '21 * 2', returnByValue: true } })).error, undefined);
    assert.equal((await dispatch('end')).error, undefined);
    assert.equal((await dispatch('detach')).error, undefined); assert.ok(!guest.isDestroyed());
    scope = { ...scope, tab_id: randomUUID(), tab_generation: randomUUID(), attachment_id: randomUUID(), attachment_generation: randomUUID() };
    await window.webContents.executeJavaScript('window.acceptBrowserAdmission = false');
    assert.ok((await dispatch('open', { url: origin + '/unadmitted' })).error);
    assert.equal(manager.snapshot().tabs.length, 1); assert.ok(!manager.snapshot().tabs.some(tab => tab.id === scope.tab_id));
    await call('release', { rootId, providerEpoch: provider.provider_epoch });
    assert.equal((await dispatch('open', { url: origin + '/released' })).error?.kind, 'attachment_revoked');
    const providerEvents = events.filter(event => event.kind === 'provider').map(event => event.event);
    for (const attachment of [parent.attachment_id, child.attachment_id]) {
      const sequences = providerEvents.filter(event => event.attachment_id === attachment).map(event => Number(event.sequence));
      assert.deepEqual(sequences, sequences.map((_, index) => index + 1));
    }
    console.log('PASS production BrowserControl preload/IPC: reserved IDs, admission, CDP/screenshot, duplicate/cancel uncertainty, navigation attribution, atomic transfer, detach/release, event ordering');
  } finally {
    await window.webContents.executeJavaScript('window.stopAdmissions?.()');
    control.dispose(); manager.dispose(); cleanup();
  }
}
