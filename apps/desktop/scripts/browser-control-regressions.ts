import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { session, type BrowserWindow, type WebContents } from 'electron';
import type { BrowserCommand } from '@whip/protocol';
import type { BrowserAgentScope } from '@whip/app/desktop-bridge';
import { BrowserManager } from '../src/browser-manager';
import { BrowserControl } from '../src/browser-control';

/** Native resource fault + injected expansion interleaving; not remote SSH acceptance. */
export async function testBrowserControlRegressions(window: BrowserWindow, origin: string) {
  let closes = 0, failedContents: WebContents | undefined;
  const failed = new BrowserManager(window, () => {}, { environment: async () => ({ session: session.fromPartition(randomUUID()),
    bind: contents => { failedContents = contents; throw new Error('Injected native bind failure'); }, close: async () => { closes++; } }) });
  const initial = failed.snapshot(), tab = failed.create({ epoch: initial.epoch, environmentId: randomUUID(), url: origin });
  failed.admitted({ epoch: initial.epoch, tabId: tab.id, generation: tab.generation });
  await assert.rejects(failed.controlledContents({ epoch: initial.epoch, tabId: tab.id, generation: tab.generation }), /Injected/);
  await new Promise(resolve => setTimeout(resolve, 50));
  assert.equal(closes, 1); assert.ok(failedContents?.isDestroyed()); assert.equal(window.contentView.children.length, 0);
  assert.equal(failed.snapshot().tabs[0].status, 'unavailable'); failed.dispose(); assert.equal(closes, 1);
  console.log('PASS production native realization fault releases lease once and destroys partial guest');

  let control: BrowserControl, continueExpansion!: () => void, enteredExpansion!: () => void;
  const gate = new Promise<void>(resolve => { continueExpansion = resolve; }), entered = new Promise<void>(resolve => { enteredExpansion = resolve; });
  const environmentId = randomUUID(), profile = session.fromPartition(randomUUID());
  const preview = { host_id: randomUUID(), host_identity: randomUUID(), connection_generation: randomUUID(), environment_id: environmentId, loopback: '127.0.0.1', ports: [] as number[] };
  let currentPreview = { ...preview, ports: [Number(new URL(origin).port)] };
  const manager = new BrowserManager(window, event => control?.observe(event), { environment: async () => ({ session: profile, close: async () => {} }),
    invalidateControl: (id, reason) => control?.invalidate(id, reason) });
  control = new BrowserControl(manager, event => { if (event.kind === 'admission') manager.admitted({ epoch: event.epoch, tabId: event.tab.id, generation: event.tab.generation }); }, {
    prepare: async () => {}, preview: async () => environmentId, previewState: () => structuredClone(currentPreview),
    expand: async (_selection, _scope, port) => { enteredExpansion(); await gate; control.invalidateEnvironment(environmentId, 'Preview destination scope changed'); currentPreview = { ...currentPreview, ports: [...currentPreview.ports, port].sort((a, b) => a - b) }; },
  });
  try {
    const identity = control.identity(), root = randomUUID(), agent = randomUUID(), provider = { version: 1, provider_id: randomUUID(), provider_epoch: randomUUID() };
    await control.select({ provider, offer: { version: 1, root_id: root, desktop_id: identity.desktopId, window_id: identity.windowId, create_profile_id: identity.createProfileId,
      offer_revision: randomUUID(), offered_tabs: [], offered_preview_hosts: [preview] } });
    const scope = (): BrowserAgentScope => ({ provider_id: provider.provider_id, provider_epoch: provider.provider_epoch, tab_id: randomUUID(), tab_generation: randomUUID(),
      profile_id: identity.createProfileId, attachment_id: randomUUID(), attachment_generation: randomUUID(), rights: ['create', 'control', 'route'],
      preview: { ...preview, ports: [Number(new URL(origin).port)] } });
    const a = scope(), b = scope();
    const command = (scope: BrowserAgentScope, kind: string, args: unknown): BrowserCommand => ({ root_id: root, agent_id: agent, scope, kind, arguments: args,
      command_id: randomUUID(), operation_id: randomUUID(), provider_epoch: provider.provider_epoch, deadline_millis: String(Date.now() + 5000) });
    assert.equal((await control.dispatch(command(a, 'open', { url: origin }))).error, undefined);
    assert.equal((await control.dispatch(command(b, 'open', { url: origin }))).error, undefined);
    const inventory = control.identity(); provider.provider_epoch = randomUUID();
    for (const scope of [a, b]) { scope.provider_epoch = provider.provider_epoch; scope.profile_id = environmentId; scope.attachment_id = randomUUID(); scope.attachment_generation = randomUUID(); }
    await control.select({ provider, offer: { version: 1, root_id: root, desktop_id: identity.desktopId, window_id: identity.windowId, create_profile_id: identity.createProfileId,
      offer_revision: randomUUID(), offered_tabs: inventory.tabs, offered_preview_hosts: [currentPreview] } });
    assert.equal((await control.dispatch(command(a, 'attach', { tab_id: a.tab_id }))).error, undefined);
    assert.equal((await control.dispatch(command(b, 'attach', { tab_id: b.tab_id }))).error, undefined);
    const expanding = control.dispatch(command(a, 'allow_preview_port', { port: 3001 })); await entered;
    assert.equal((await control.dispatch(command(b, 'allow_preview_port', { port: 3002 }))).error?.kind, 'browser_busy');
    control.invalidateEnvironment(environmentId, 'Preview destination scope changed');
    const reattach = { ...b, attachment_id: randomUUID(), attachment_generation: randomUUID() };
    assert.equal((await control.dispatch(command(reattach, 'attach', { tab_id: b.tab_id }))).error?.kind, 'browser_busy');
    assert.equal((await control.dispatch(command(a, 'transfer', { child_agent_id: randomUUID(), attachments: [{ parent_scope: a, child_scope: { ...a, attachment_id: randomUUID(), attachment_generation: randomUUID() } }] }))).error?.kind, 'browser_busy');
    continueExpansion(); assert.equal((await expanding).error, undefined);
    assert.equal((await control.dispatch(command(reattach, 'attach', { tab_id: b.tab_id }))).error?.kind, 'attachment_revoked');
    assert.equal((await control.dispatch(command(b, 'begin', {}))).error?.kind, 'attachment_revoked');
    const current = { ...a, preview: { ...a.preview!, ports: [...a.preview!.ports!, 3001].sort((a, b) => a - b) } };
    assert.equal((await control.dispatch(command(current, 'begin', {}))).error, undefined);
    console.log('PASS concurrent same-environment expansion exempts only its one exact initiator and revokes incompatible controller');
  } finally { continueExpansion(); control.dispose(); manager.dispose(); }
}
