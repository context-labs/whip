// Opt-in acceptance driver: native SDK, production desktop transport and real bridge.
import { Client, BrowserProviderClient, browserProviderFramed, framedTransport, selectBrowserProvider, type BrowserSelection } from '@whip/sdk';
import type { DesktopBridge, BrowserPlatform, BrowserAgentBridge } from '@whip/app/desktop-bridge';
import { desktopTransport } from '../../web/src/platform/desktop';

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
const check = (ok: unknown, message: string) => { if (!ok) throw new Error(message); };
const deadline = () => ({ signal: AbortSignal.timeout(15000) });
(globalThis as any).runNativeSDK = async (input: { connectionId: string; runtimeId: string; cwd: string; url: string; marker: string }) => {
  const bridge = (globalThis as any).whipDesktop as DesktopBridge & { browser: BrowserPlatform; browserAgent: BrowserAgentBridge };
  const open = desktopTransport(bridge, input.connectionId);
  const client = await Client.connect(framedTransport(open), { clientID: 'owned-native-sdk-' + crypto.randomUUID(), expectedRuntimeID: input.runtimeId, ...deadline() });
  const pin = { expectedRuntimeID: client.runtimeID, expectedProcessEpoch: client.processEpoch };
  let rootId = '', tabId = '', selection: BrowserSelection | undefined;
  const reports: string[] = [], commands: any[] = [], cancellations: any[] = [], errors: string[] = [];
  const create = async () => { const created = await client.createTree({ engine: 'starlark', definition: client.builtins[0]!, working_directory: input.cwd,
    overrides: { modules: ['browser'], model: { provider: '', name: '', effort: '' }, automatic_title: false },
    metadata: { title: null, pinned: false, archived: false } }, crypto.randomUUID(), deadline());
    if (!created.root) throw new Error('New fixture root unexpectedly deleted');
    return created.root;
  };
  try {
    rootId = (await create()).id;
    const session = client.session(rootId);
    check((await session.permissions.policy(deadline())).mode === 'prompt', 'New browser root must require explicit scoped consent');
    const identity = await bridge.browserAgent.identity();
    const projectId = 'cwd:' + input.cwd;
    const preview = await bridge.browserAgent.preview({ connectionId: input.connectionId, runtimeId: input.runtimeId, projectId, loopback: '127.0.0.1' });
    check(preview.host_id && preview.environment_id, 'Native preview offer missing exact host/environment');
    const peer = await BrowserProviderClient.connect(await browserProviderFramed(open, { ...pin, ...deadline() }), { ...pin, ...deadline() });
    selection = await selectBrowserProvider(peer, { version: 2, root_id: rootId, desktop_id: identity.desktopId, window_id: identity.windowId,
      create_profile_id: identity.createProfileId, offer_revision: crypto.randomUUID(), offered_tabs: [], offered_preview_hosts: [preview] },
      { ...bridge.browserAgent,
        dispatch: command => { commands.push(command); return bridge.browserAgent.dispatch(command); },
        cancel: command => { cancellations.push(command); return bridge.browserAgent.cancel(command); },
      }, { connectionId: input.connectionId, projectId, onError: error => errors.push(error.message) });
    const tool = async (name: string, args: Record<string, unknown>) => {
      const requestID = crypto.randomUUID();
      await client.callTool(rootId, { module: 'browser', name, arguments_base64: btoa(JSON.stringify(args)) }, requestID, deadline());
      const outcome = await client.wait(requestID, deadline());
      check(outcome.turn, 'Native operation has no owning turn');
      const operations = await session.turns.operations(outcome.turn!.id, { limit: 10 }, deadline());
      check(operations.items?.length === 1, 'Direct browser operation must retain one canonical result');
      const operation = operations.items![0]!;
      return { turn: outcome.turn!, operation, value: operation.result?.value as any };
    };
    const decide = async (allow: boolean) => {
      for (let count = 0; count < 160; count++) {
        const pending = (await session.permissions.list({ pending_only: true, limit: 10 }, deadline())).items?.find(item => item.state === 'pending');
        if (pending) {
          const operation = await client.call('operations.get', { operation_id: pending.operation_id }, deadline());
          check(operation.capability === 'browser.control' && operation.session_id === rootId, 'Expected exact-owner browser permission');
          await session.permissions.resolve(pending.operation_id, allow, deadline());
          return operation;
        }
        await sleep(50);
      }
      throw new Error('Scoped browser permission not offered');
    };
    const before = (await bridge.browser.snapshot()).tabs.length;
    const denied = tool('open', { url: input.url, preview_host_id: preview.host_id });
    const deniedOperation = await decide(false);
    check((await denied).operation.state === 'denied', 'Permission denial did not fail browser open');
    check((await bridge.browser.snapshot()).tabs.length === before, 'Denied permission created native descriptor');
    reports.push('actual external scoped permission denial causes no native tab');
    const pending = tool('open', { url: input.url, preview_host_id: preview.host_id });
    const allowedOperation = await decide(true);
    check(allowedOperation.id !== deniedOperation.id, 'Permission denial was replayed');
    const openOutcome = await pending, opened = openOutcome.value; tabId = opened?.tab_id ?? '';
    check(openOutcome.operation.state === 'succeeded' && opened.attachment_id && opened.tab_id, 'Native open failed: ' + JSON.stringify(openOutcome));
    check(opened.network.kind === 'ssh' && opened.network.host_id === preview.host_id, 'Approved preview lost SSH network provenance');
    const openCommand = commands.find(command => command.kind === 'open');
    check(openCommand?.operation_id === allowedOperation.id, 'Dispatched command differs from the approved canonical operation');
    for (const value of [openCommand.scope.provider_id, openCommand.scope.provider_epoch, openCommand.scope.tab_id, openCommand.scope.tab_generation,
      preview.host_id, preview.host_identity, preview.connection_generation, preview.environment_id])
      check(JSON.stringify(allowedOperation.arguments).includes(value), 'Permission omitted immutable native scope: ' + value);
    check(allowedOperation.state === 'waiting' && allowedOperation.dispatched_at === null, 'Browser effect preceded scoped consent');
    reports.push('approved native operation reaches the selected SSH guest; durable arguments bind exact immutable scope before dispatch');
    const run = await tool('run', { attachment_id: opened.attachment_id, code: 'print(js("document.body.textContent")); screenshot()', timeout: 10 });
    const references = run.operation.result?.content_references;
    check(run.operation.state === 'succeeded' && run.value.output.includes(input.marker) && references?.length === 1, 'Remote helper/screenshot failed: ' + JSON.stringify(run));
    const bytes = await session.content.readBytes(references![0]!, { maxBytes: 4 << 20, ...deadline() });
    check(bytes[0] === 255 && bytes[1] === 216 && bytes.length > 1000, 'Screenshot upload/read did not produce JPEG');
    const foreign = await create();
    try {
      const deniedRead = await client.session(foreign.id).content.read(references![0]!, deadline()).then(() => false, () => true);
      check(deniedRead, 'Foreign root borrowed screenshot content authority');
    } finally { await client.session(foreign.id).delete(deadline()); }
    reports.push('Go helper/CDP screenshot uploaded through the owning framed peer; scoped digest-verified content rejects a foreign root');
    const delayedExpression = 'new Promise(resolve => setTimeout(() => { window.ownedSdkCancelledCount = (window.ownedSdkCancelledCount || 0) + 1; resolve(window.ownedSdkCancelledCount); }, 1800))';
    const timed = await tool('run', { attachment_id: opened.attachment_id, code: 'print(js(' + JSON.stringify(delayedExpression) + '))', timeout: 1 });
    check(timed.operation.state === 'uncertain', 'Dispatched timeout was not uncertain: ' + JSON.stringify(timed));
    await sleep(2200);
    const delayedCommands = commands.filter(command => command.kind === 'cdp' && command.arguments?.method === 'Runtime.evaluate' && command.arguments.params?.expression.includes('ownedSdkCancelledCount'));
    check(delayedCommands.length === 1 && cancellations.some(command => command.command_id === delayedCommands[0].command_id), 'Timed-out effect was replayed or not cancelled');
    reports.push('real runtime timeout sends native cancellation, records uncertainty and never replays the dispatched effect');
    const detached = await tool('detach', { attachment_id: opened.attachment_id });
    check(detached.operation.state === 'succeeded' && (await bridge.browser.snapshot()).tabs.some(tab => tab.id === opened.tab_id), 'Detach failed or closed the human tab');
    reports.push('native detach preserves the human-owned page');
    check(commands.length > 6 && new Set(commands.map(command => command.command_id)).size === commands.length, 'Command identities repeated');
    check(commands.every(command => command.root_id === rootId && command.agent_id === rootId), 'Native command borrowed another owner');
    await selection.release(); selection = undefined;
    check((await bridge.browser.snapshot()).tabs.some(tab => tab.id === opened.tab_id), 'Provider release closed the human tab');
    check(errors.length === 0, 'Provider lifecycle errors: ' + errors.join('; '));
    return { ok: true, rootId, tabId, reports, errors, screenshotBytes: bytes.length, commands: commands.length, cancellations: cancellations.length };
  } catch (error) {
    return { ok: false, tabId, error: String(error), stack: (error as Error).stack, reports, errors, commands };
  } finally {
    await selection?.release().catch(error => errors.push('cleanup release: ' + error.message));
    if (rootId) await client.session(rootId).delete(deadline()).catch(error => errors.push('cleanup root: ' + error.message));
  }
};
