// Opt-in acceptance driver: production SDK + production desktop transport + real native bridge.
import { createWhipClient } from '@whip/sdk';
import type { DesktopBridge, BrowserPlatform, BrowserAgentBridge } from '@whip/app/desktop-bridge';
import { desktopTransport } from '../../web/src/platform/desktop';

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
const check = (ok: unknown, message: string) => { if (!ok) throw new Error(message); };
(globalThis as any).runNativeSDK = async (input: { connectionId: string; runtimeId: string; cwd: string; url: string; marker: string }) => {
  const bridge = (globalThis as any).whipDesktop as DesktopBridge & { browser: BrowserPlatform; browserAgent: BrowserAgentBridge };
  const client = createWhipClient({ endpoint: desktopTransport(bridge, input.connectionId), clientId: 'owned-native-sdk-' + crypto.randomUUID(),
    browserProvider: true, expectedRuntimeId: input.runtimeId, reconnect: false, queryTimeoutMs: 15000 });
  let rootId = '', tabId = '', selection: any;
  const reports: string[] = [], commands: any[] = [], cancellations: any[] = [], errors: string[] = [];
  client.onNotification('browser.command', command => commands.push(command));
  client.onNotification('browser.command.cancel', command => cancellations.push(command));
  const result = async (handle: any) => {
    const outcome = await handle.result();
    check(outcome.status === 'succeeded', 'Command failed: ' + JSON.stringify(outcome));
    return outcome.result;
  };
  try {
    await client.connect();
    const created = await result(client.sessions.create({ kind: 'tool_host', cwd: input.cwd, permission_mode: 'prompt' }));
    rootId = created.root_id;
    check(rootId, 'Tool host root identity missing');
    const session = client.session(rootId);
    await result(session.setPermissionMode(true));
    const identity = await bridge.browserAgent.identity();
    const projectId = 'cwd:' + input.cwd;
    const preview = await bridge.browserAgent.preview({ connectionId: input.connectionId, runtimeId: input.runtimeId, projectId, loopback: '127.0.0.1' });
    check(preview.host_id && preview.environment_id, 'Native preview offer missing exact host/environment');
    selection = await client.browser.select({ version: 1, root_id: rootId, desktop_id: identity.desktopId, window_id: identity.windowId,
      create_profile_id: identity.createProfileId, offer_revision: crypto.randomUUID(), offered_tabs: [], offered_preview_hosts: [preview] },
      bridge.browserAgent, { connectionId: input.connectionId, projectId, onError: error => errors.push(error.message) });
    const tool = (name: string, args: any) => session.command('tool.call', { tool: name, arguments: args });
    const decide = async (allow: boolean) => {
      for (let count = 0; count < 160; count++) {
        const snapshot = await session.snapshot();
        const prompt = snapshot.permissions?.find((item: any) => item.status === 'pending' || !item.status) as any;
        if (prompt) {
          check(JSON.stringify(prompt).includes('browser'), 'Expected scoped browser permission');
          await client.permissions.decide({ root_id: rootId, permission_id: prompt.id, allow });
          return prompt;
        }
        await sleep(50);
      }
      throw new Error('Scoped browser permission not offered');
    };
    const parse = async (handle: any) => JSON.parse((await result(handle)).text);
    const before = (await bridge.browser.snapshot()).tabs.length;
    const denied = tool('browser_open', { url: input.url, preview_host_id: preview.host_id });
    const deniedPrompt = await decide(false);
    const denial = await parse(denied);
    check(denial.error, 'Permission denial did not fail browser_open');
    check((await bridge.browser.snapshot()).tabs.length === before, 'Denied permission created native descriptor');
    reports.push('actual external scoped permission denial causes no native tab');
    const pending = tool('browser_open', { url: input.url, preview_host_id: preview.host_id });
    const allowedPrompt = await decide(true);
    check(allowedPrompt.id !== deniedPrompt.id, 'Permission denial was replayed');
    const opened = await parse(pending); tabId = opened.tab_id ?? '';
    check(!opened.error && opened.attachment_id && opened.tab_id, 'Native open failed: ' + JSON.stringify(opened));
    check(opened.network.kind === 'ssh' && opened.network.host_id === preview.host_id, 'Approved preview did not retain SSH network provenance: ' + JSON.stringify(opened));
    const openCommand = commands.find(command => command.kind === 'open');
    check(openCommand && /^[a-f0-9]{64}$/.test(allowedPrompt.request_digest), 'Durable permission scope digest missing');
    for (const value of [openCommand.scope.provider_id, openCommand.scope.provider_epoch, openCommand.scope.tab_id, openCommand.scope.tab_generation,
      preview.host_id, preview.host_identity, preview.connection_generation, preview.environment_id])
      check(allowedPrompt.command.includes(value), 'Permission omitted exact immutable native scope: ' + value);
    check(allowedPrompt.command.includes('Once') && allowedPrompt.command.includes('independent lifetime'), 'Permission obscured attachment/network lifetimes');
    reports.push('actual approved browser_open reaches selected SSH guest via SDK provider and native ACK; durable prompt names exact immutable scope');
    const run = await parse(tool('browser_run', { attachment_id: opened.attachment_id,
      code: 'print(js("document.body.textContent")); screenshot()', timeout: 10 }));
    check(!run.error && run.output.includes(input.marker) && run.media?.length === 1, 'Remote helper/screenshot failed: ' + JSON.stringify(run));
    const first = await client.call('content.read', { root_id: rootId, agent_id: rootId, reference_id: run.media[0], offset: '0', limit: 4096 });
    const bytes = await client.content(first.content, { rootId, agentId: rootId }).readBytes({ maxBytes: 8 << 20 });
    check(bytes[0] === 255 && bytes[1] === 216 && bytes.length > 1000, 'Screenshot upload/read did not produce JPEG');
    const foreign = await result(client.sessions.create({ kind: 'tool_host', cwd: input.cwd, permission_mode: 'prompt' }));
    try {
      const deniedRead = await client.call('content.read', { root_id: foreign.root_id, agent_id: foreign.root_id, reference_id: run.media[0], offset: '0', limit: 4096 }).then(() => false, () => true);
      check(deniedRead, 'Foreign root borrowed screenshot content authority');
    } finally { await result(client.sessions.delete(foreign.root_id)); }
    reports.push('real Go helper/CDP screenshot uploaded through owning framed SDK connection; content digest verified and foreign root denied');
    const delayedExpression = 'new Promise(resolve => setTimeout(() => { window.ownedSdkCancelledCount = (window.ownedSdkCancelledCount || 0) + 1; resolve(window.ownedSdkCancelledCount); }, 1800))';
    const timed = await parse(tool('browser_run', { attachment_id: opened.attachment_id, code: 'print(js(' + JSON.stringify(delayedExpression) + '))', timeout: 1 }));
    check(timed.error?.kind === 'outcome_unknown', 'Timed-out dispatched effect was not outcome_unknown: ' + JSON.stringify(timed));
    await sleep(2200);
    const delayedCommands = commands.filter(command => command.kind === 'cdp' && command.arguments?.method === 'Runtime.evaluate' && command.arguments.params?.expression.includes('ownedSdkCancelledCount'));
    check(delayedCommands.length === 1 && cancellations.some(command => command.command_id === delayedCommands[0].command_id), 'Timed-out effect was replayed or not cancelled over SDK');
    reports.push('real daemon timeout sends SDK/native cancellation, reports outcome_unknown and never replays dispatched delayed effect');
    const detached = await parse(tool('browser_detach', { attachment_id: opened.attachment_id }));
    const humanTabPreserved = (await bridge.browser.snapshot()).tabs.some(tab => tab.id === opened.tab_id);
    check(!detached.error && humanTabPreserved, 'Detach failed: ' + JSON.stringify({ detached, humanTabPreserved }));
    reports.push('real daemon browser_detach preserves native human tab');
    check(commands.length > 6 && new Set(commands.map(command => command.command_id)).size === commands.length, 'Command identities repeated');
    check(commands.every(command => command.root_id === rootId && command.agent_id === rootId), 'Daemon command borrowed root/agent authority');
    await selection.release(); selection = undefined;
    check((await bridge.browser.snapshot()).tabs.some((tab: any) => tab.id === opened.tab_id), 'SDK provider release closed human tab');
    check(errors.length === 0, 'SDK provider lifecycle errors: ' + errors.join('; '));
    return { ok: true, rootId, tabId: opened.tab_id, reports, errors, screenshotBytes: bytes.length, commands: commands.length, cancellations: cancellations.length };
  } catch (error) {
    return { ok: false, tabId, error: String(error), stack: (error as Error).stack, reports, errors,
      operations: client.getSnapshot().info?.operations?.filter(item => item.name.includes('browser')), commands };
  } finally {
    await selection?.release().catch((error: Error) => errors.push('cleanup release: ' + error.message));
    if (rootId) await client.sessions.delete(rootId).result().catch(error => errors.push('cleanup root: ' + error.message));
    client.close();
  }
};
