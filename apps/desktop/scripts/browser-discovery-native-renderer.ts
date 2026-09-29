// Production app association, native SDK, isolated runtime and real preload/IPC.
import { Client, BrowserProviderClient } from '@whip/sdk';
import { browserSocket, browserProviderDuplex } from '@whip/sdk/browser';
import { BrowserAssociations } from '../../../packages/app/src/browser-provider';
import { BrowserWorkspace } from '../../../packages/app/src/browser-workspace';
import { SessionTabs } from '../../../packages/app/src/session-tabs';
import type { HostConnections } from '../../../packages/app/src/hosts';
import type { DesktopBridge } from '@whip/app/desktop-bridge';

const check = (value: unknown, message: string) => { if (!value) throw new Error(message); };
const sleep = () => new Promise(resolve => setTimeout(resolve, 25));
(globalThis as any).runBrowserDiscovery = async (input: { endpoint: string; runtimeID: string; processEpoch: string; cwd: string }) => {
  const bridge = (globalThis as any).whipDesktop as DesktopBridge;
  const lifetime = new AbortController();
  const deadline = () => ({ signal: AbortSignal.any([lifetime.signal, AbortSignal.timeout(15000)]) });
  const pin = { expectedRuntimeID: input.runtimeID, expectedProcessEpoch: input.processEpoch };
  const client = await Client.connect(browserSocket(input.endpoint, pin), { clientID: 'native-discovery', ...pin, ...deadline() });
  const tabs = new SessionTabs(), errors: string[] = [];
  const workspace = new BrowserWorkspace(bridge.browser, tabs, error => errors.push(String(error)));
  let associations: BrowserAssociations | undefined, rootId = '';
  const commands: any[] = [];
  try {
    await workspace.start();
    const created = await client.createTree({ engine: 'starlark', definition: client.builtins[0]!, working_directory: input.cwd,
      overrides: { modules: ['browser'], automatic_title: false }, metadata: { title: null, pinned: false, archived: false } }, crypto.randomUUID(), deadline());
    if (!created.root) throw new Error('New fixture root unexpectedly deleted');
    rootId = created.root.id;
    const session = client.session(rootId);
    check((await session.permissions.policy(deadline())).mode === 'prompt', 'Discovery root must require scoped consent');
    const host = { id: 'fixture', name: 'Isolated runtime', runtimeId: client.runtimeID, state: 'connected', client, profile: { id: 'fixture', target: { kind: 'url', url: input.endpoint } } };
    const hosts = { getSnapshot: () => ({ hosts: [host] }), subscribe: () => () => {}, signal: () => lifetime.signal,
      browserProvider: async () => BrowserProviderClient.connect(await browserProviderDuplex(input.endpoint, { ...pin, ...deadline() }), { ...pin, ...deadline() }),
    } as unknown as HostConnections;
    associations = new BrowserAssociations({ ...bridge.browserAgent!, dispatch: command => { commands.push(command); return bridge.browserAgent!.dispatch(command); } }, hosts, tabs, workspace, error => errors.push(String(error)));
    tabs.open(client.runtimeID, rootId, 'Native discovery fixture');
    for (let i = 0; i < 200 && associations.getSnapshot()[0]?.status !== 'available'; i++) await sleep();
    check(associations.getSnapshot()[0]?.status === 'available', 'Zero-tab availability failed: ' + JSON.stringify(associations.getSnapshot()));
    // Exercise a real model dispatch and Starlark cell for every browser operation.
    const tool = async (name: string, args: Record<string, unknown> = {}) => {
      const parameters = Object.keys(args).length ? `**json.decode(${JSON.stringify(JSON.stringify(args))})` : '';
      const code = `print(json.encode(browser.${name}(${parameters})))`;
      const requestID = crypto.randomUUID();
      await session.submit([{ type: 'text', text: '```starlark\n' + code + '\n```' }], requestID, deadline());
      const outcome = await client.wait(requestID, deadline());
      check(outcome.turn?.state === 'succeeded', 'Browser agent turn failed: ' + JSON.stringify(outcome));
      const cells = await session.turns.cells(outcome.turn!.id, { limit: 10 }, deadline());
      check(cells.items.length === 1, 'Expected one real interpreter cell for ' + name + ': ' + JSON.stringify(cells));
      const operations = await session.turns.operations(outcome.turn!.id, { limit: 10 }, deadline());
      check(operations.items.length === 1, 'Expected one canonical browser operation');
      const operation = operations.items[0]!;
      check(operation.origin === 'cell' && operation.cell_id === cells.items[0]!.id, 'Browser operation lost its real cell provenance');
      check(operation.state === 'succeeded' || operation.state === 'denied', 'Unexpected browser operation outcome: ' + JSON.stringify(operation));
      check(cells.items[0]!.state === (operation.state === 'denied' ? 'failed' : 'succeeded'), 'Cell must retain permission denial as a failure');
      return operation;
    };
    const decide = async (allow: boolean) => {
      for (let i = 0; i < 200; i++) {
        const prompt = (await session.permissions.list({ pending_only: true, limit: 10 }, deadline())).items?.find(item => item.state === 'pending');
        if (prompt) {
          const operation = await session.operations.get(prompt.operation_id, deadline());
          await session.permissions.resolve(prompt.operation_id, allow, deadline());
          return operation;
        }
        await sleep();
      }
      throw new Error('Browser permission prompt missing');
    };
    const empty = (await tool('list_tabs')).result?.value as any;
    check(empty.availability === 'available' && empty.tabs.length === 0 && commands.length === 0, 'Discovery created a page or dispatched control: ' + JSON.stringify(empty));
    const denied = tool('open', { url: 'about:blank' }); await decide(false);
    check((await denied).state === 'denied' && commands.length === 0 && (await bridge.browser!.snapshot()).tabs.length === 0, 'Denied create had native effects');
    const opening = tool('open', { url: 'about:blank' }); const prompt = await decide(true); const openingOperation = await opening, opened = openingOperation.result?.value as any;
    check(openingOperation.state === 'succeeded' && opened.attachment_id && tabs.workspace().tabs.some(tab => tab.kind === 'browser' && tab.id === opened.tab_id), 'Approved page not admitted into originating workspace: ' + JSON.stringify(opened));
    check(commands.filter(command => command.kind === 'open').length === 1 && JSON.stringify(prompt.arguments).includes(opened.tab_id), 'Create scope/dispatch mismatch');
    check((await tool('run', { attachment_id: opened.attachment_id, code: `js("document.title = 'Fresh native title'")` })).state === 'succeeded', 'Native run failed');
    const listed = (await tool('list_tabs')).result?.value as any;
    check(listed.tabs.length === 1 && listed.tabs[0].title === 'Fresh native title' && listed.tabs[0].attachment_id === opened.attachment_id && listed.tabs[0].state === 'attached', 'Inventory not fresh/scoped: ' + JSON.stringify(listed));
    check((await tool('detach', { attachment_id: opened.attachment_id })).state === 'succeeded', 'Detach failed');
    const detached = (await tool('list_tabs')).result?.value as any;
    check(detached.tabs[0].requestable && !detached.tabs[0].attachment_id, 'Detached created tab not rediscoverable');
    const attaching = tool('attach', { tab_id: opened.tab_id }); await decide(true); const attachedOperation = await attaching, attached = attachedOperation.result?.value as any;
    check(attachedOperation.state === 'succeeded' && attached.attachment_id !== opened.attachment_id, 'Reattach did not require a fresh scoped handle');
    await associations.release(associations.getSnapshot()[0]!.key);
    check((await bridge.browser!.snapshot()).tabs.some(tab => tab.id === opened.tab_id), 'Release closed the human tab');
    check(errors.length === 0, errors.join('; '));
    return { ok: true, reports: ['recursive Starlark agent dispatch', 'zero-tab inert availability', 'on-demand scoped inventory without control', 'prompt denial without native effect', 'approved native create and app admission', 'fresh title via real CDP', 'detach/discover/permission-gated reattach', 'release preserves human page'], commands: commands.length };
  } finally {
    associations?.dispose(); workspace.dispose();
    if (rootId) await client.session(rootId).delete(deadline());
    lifetime.abort();
  }
};
