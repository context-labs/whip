// Production app association + SDK + isolated real daemon + native preload/IPC.
import { createWhipClient } from '@whip/sdk';
import { BrowserAssociations } from '../../../packages/app/src/browser-provider';
import { BrowserWorkspace } from '../../../packages/app/src/browser-workspace';
import { SessionTabs } from '../../../packages/app/src/session-tabs';
import type { HostConnections } from '../../../packages/app/src/hosts';
import type { DesktopBridge } from '@whip/app/desktop-bridge';

const check = (value: unknown, message: string) => { if (!value) throw new Error(message); };
const sleep = () => new Promise(resolve => setTimeout(resolve, 25));
(globalThis as any).runBrowserDiscovery = async (input: { endpoint: string; cwd: string }) => {
  const bridge = (globalThis as any).whipDesktop as DesktopBridge;
  const client = createWhipClient({ endpoint: input.endpoint, clientId: 'native-discovery', browserProvider: true, reconnect: false });
  const tabs = new SessionTabs(), errors: string[] = [];
  const workspace = new BrowserWorkspace(bridge.browser, tabs, error => errors.push(String(error)));
  let associations: BrowserAssociations | undefined, rootId = '';
  const commands: any[] = []; client.onNotification('browser.command', command => commands.push(command));
  const result = async (handle: any) => { const value = await handle.result(); check(value.status === 'succeeded', JSON.stringify(value)); return value.result; };
  try {
    await client.connect(); await workspace.start();
    rootId = (await result(client.sessions.create({ kind: 'agent', model: 'model', provider: 'provider', cwd: input.cwd, permission_mode: 'prompt' }))).root_id;
    const session = client.session(rootId); await result(session.setPermissionMode(true));
    const runtimeId = client.getSnapshot().info!.runtime_id;
    const host = { id: 'fixture', name: 'Isolated daemon', runtimeId, state: 'connected', client, profile: { id: 'fixture', target: { kind: 'local' } } };
    const lifetime = new AbortController();
    const hosts = { getSnapshot: () => ({ hosts: [host] }), subscribe: () => () => {}, signal: () => lifetime.signal } as unknown as HostConnections;
    associations = new BrowserAssociations(bridge.browserAgent, hosts, tabs, workspace, error => errors.push(String(error)));
    tabs.open(runtimeId, rootId, 'Native discovery fixture');
    for (let i = 0; i < 200 && associations.getSnapshot()[0]?.status !== 'available'; i++) await sleep();
    check(associations.getSnapshot()[0]?.status === 'available', 'Zero-tab availability failed: ' + JSON.stringify(associations.getSnapshot()));
    // Exercise the model's real recursive host path, not just the tool-host API.
    const tool = async (name: string, args: any = {}) => {
      const operation = name.replace('browser_', '');
      const parameters = Object.keys(args).length ? `**json.decode(${JSON.stringify(JSON.stringify(args))})` : '';
      const code = `print(json.encode(browser.${operation}(${parameters})))`;
      const turn = await session.run('```cell\n' + code + '\n```').result();
      check(turn.status === 'succeeded', JSON.stringify(turn));
      if (turn.status !== 'succeeded') throw new Error('Browser agent turn failed');
      check(turn.text.startsWith('done: '), turn.text);
      const execution = JSON.parse(turn.text.slice('done: '.length));
      return JSON.parse(execution.output);
    };
    const decide = async (allow: boolean) => {
      for (let i = 0; i < 200; i++) {
        const prompt = (await session.snapshot()).permissions?.find((item: any) => item.status === 'pending' || !item.status);
        if (prompt) { await client.permissions.decide({ root_id: rootId, permission_id: prompt.id, allow }); return prompt; }
        await sleep();
      }
      throw new Error('Browser permission prompt missing');
    };
    const empty = await tool('browser_list_tabs');
    check(!empty.error && empty.availability === 'available' && empty.tabs.length === 0 && commands.length === 0, 'Discovery created a page or dispatched control: ' + JSON.stringify(empty));
    const denied = tool('browser_open', { url: 'about:blank' }); await decide(false);
    check((await denied).error?.kind === 'permission_denied' && commands.length === 0 && (await bridge.browser!.snapshot()).tabs.length === 0, 'Denied create had native effects');
    const opening = tool('browser_open', { url: 'about:blank' }); const prompt = await decide(true); const opened = await opening;
    check(!opened.error && opened.attachment_id && tabs.workspace().tabs.some(tab => tab.kind === 'browser' && tab.id === opened.tab_id), 'Approved page not admitted into originating workspace: ' + JSON.stringify(opened));
    check(commands.filter(command => command.kind === 'open').length === 1 && JSON.stringify(prompt).includes(opened.tab_id), 'Create scope/dispatch mismatch');
    const edited = await tool('browser_run', { attachment_id: opened.attachment_id, code: `js("document.title = 'Fresh native title'")` });
    check(!edited.error, 'Native run failed: ' + JSON.stringify(edited));
    const listed = await tool('browser_list_tabs');
    check(!listed.error && listed.tabs.length === 1 && listed.tabs[0].title === 'Fresh native title' && listed.tabs[0].attachment_id === opened.attachment_id && listed.tabs[0].state === 'attached', 'Inventory not fresh/scoped: ' + JSON.stringify(listed));
    check(!(await tool('browser_detach', { attachment_id: opened.attachment_id })).error, 'Detach failed');
    const detached = await tool('browser_list_tabs');
    check(detached.tabs[0].requestable && !detached.tabs[0].attachment_id, 'Detached created tab not rediscoverable');
    const attaching = tool('browser_attach', { tab_id: opened.tab_id }); await decide(true); const attached = await attaching;
    check(!attached.error && attached.attachment_id !== opened.attachment_id, 'Reattach did not require a fresh scoped handle');
    await associations.release(associations.getSnapshot()[0]!.key);
    check((await bridge.browser!.snapshot()).tabs.some(tab => tab.id === opened.tab_id), 'Release closed the human tab');
    check(errors.length === 0, errors.join('; '));
    return { ok: true, reports: ['recursive Starlark agent dispatch', 'zero-tab inert availability', 'on-demand scoped inventory without control', 'prompt denial without native effect', 'approved native create and app admission', 'fresh title via real CDP', 'detach/discover/permission-gated reattach', 'release preserves human page'], commands: commands.length };
  } finally {
    associations?.dispose(); workspace.dispose();
    if (rootId) await client.sessions.delete(rootId).result();
    client.close();
  }
};
