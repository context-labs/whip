import { DurableCommand, type Client, type CreateTreeResult, type Operations, type RecoveryRecord } from '@whip/sdk';
import type { AppRuntime } from './runtime';
import { compositionKey } from './compositions';
import { submitChatInput } from './chat-submission';
import { welcomeDraftKey } from './session-tabs';

type NewChatParams = Omit<Operations['trees.create']['params'], 'creation_id'>;
function acceptCreatedChat(runtime: AppRuntime, client: Client, tabId: string, params: NewChatParams, created: CreateTreeResult) {
  if (!runtime.connections.isAttached(client)) throw new Error('Reconnect the original host before restoring this session.');
  const runtimeId = client.runtimeID, source = welcomeDraftKey(tabId);
  const { root, tree, creation } = created;
  if (creation.id !== tabId || created.deleted || !root || !tree || root.parent_id !== null
    || root.id !== creation.root_id || root.tree_id !== tree.id || creation.tree_id !== tree.id
    || tree.engine !== params.engine
    || root.definition.id !== params.definition.id || root.definition.revision !== params.definition.revision)
    throw new Error('The accepted session does not match this creation. Inspect its saved receipt.');
  const destination = compositionKey(runtimeId, root.id, root.id);
  const workspace = runtime.tabs.workspace();
  const tab = workspace.tabs.find(tab => tab.id === tabId) ?? workspace.closed.find(item => item.tab.id === tabId)?.tab;
  if (!tab) throw new Error('The original draft tab is no longer available.');
  if (tab.kind === 'chat' && tab.runtimeId === runtimeId && tab.rootId === root.id)
    return { rootId: root.id, destination, uploading: Promise.resolve() };
  if (tab.kind !== 'new' || tab.runtimeId && tab.runtimeId !== runtimeId)
    throw new Error('The original draft now belongs to a different session or host. Its contents are preserved.');
  if (runtime.compositions.get(destination).attachments.length || runtime.compositions.get(destination).sending)
    throw new Error('The created session already has attachments or a pending submission. Both drafts are preserved.');
  const currentDraft = runtime.draft(source);
  if (runtime.draft(destination) && runtime.draft(destination) !== currentDraft)
    throw new Error('The created session already has a different draft. Both drafts are preserved.');
  runtime.setDraft(destination, currentDraft);
  const upload = runtime.compositions.adopt(source, client.session(root.id), runtimeId);
  const uploadToken = runtime.compositions.beginSubmission(destination);
  const uploading = upload.finally(() => { if (uploadToken) runtime.compositions.finishSubmission(destination, uploadToken); });
  // Recovery can hand over after this invocation has returned its initial error.
  void uploading.catch(() => {});
  if (!runtime.tabs.promoteNew(tabId, runtimeId, root.id)) throw new Error('The original draft tab is no longer available.');
  if (runtime.draft(source) === currentDraft) runtime.setDraft(source, '');
  return { rootId: root.id, destination, uploading };
}

/** Explicit local handover after a fresh, exact saved-command check. Never submits input. */
export async function restoreCreatedChat(runtime: AppRuntime, client: Client, record: RecoveryRecord, signal?: AbortSignal) {
  const command = DurableCommand.recover(client, record, { journal: runtime.recovery });
  const request = JSON.parse(record.request) as { method: string; params: Operations['trees.create']['params'] };
  if (request.method !== 'trees.create') throw new Error('This saved command did not create a session.');
  const check = await command.check({ signal });
  signal?.throwIfAborted();
  if (check.state !== 'found' || !('creation' in check.evidence))
    throw new Error('This creation has not been verified against the saved request. Check or explicitly retry it first.');
  const source = welcomeDraftKey(request.params.creation_id);
  const token = runtime.compositions.beginSubmission(source);
  if (!token) throw new Error('This draft is already being submitted or restored.');
  try {
    const handover = acceptCreatedChat(runtime, client, request.params.creation_id, request.params, check.evidence);
    await handover.uploading;
    await command.forget();
  } finally { runtime.compositions.finishSubmission(source, token); }
}

/** Creation acceptance moves the authored draft once. Explicit receipt recovery
 * can finish that handover, but never starts a first message by itself. */
export async function startNewChat(runtime: AppRuntime, client: Client, tabId: string,
  params: NewChatParams) {
  const source = welcomeDraftKey(tabId), runtimeId = client.runtimeID;
  if (!runtime.connections.isAttached(client) || runtime.getSnapshot().commands.some(command => command.draftKey === source && command.delivery)) return;
  const token = runtime.compositions.beginSubmission(source);
  if (!token) return;
  const text = runtime.draft(source);
  const attachments = runtime.compositions.get(source).attachments;
  const creationId = tabId;
  let handover: { rootId: string; destination: string; uploading: Promise<void> } | undefined;
  function accept(created: CreateTreeResult) {
    if (handover) return;
    handover = acceptCreatedChat(runtime, client, tabId, params, created);
  }
  try {
    const saved = (await runtime.recovery.list()).find(record => {
      const request = JSON.parse(record.request);
      return record.runtimeID === runtimeId && request.method === 'trees.create' && request.params.creation_id === creationId;
    });
    if (saved) throw new Error('This draft has a saved session creation. Review its receipt in Settings → Saved commands before sending again.');
    const created = await runtime.run(runtime.command(client, 'trees.create', { ...params, creation_id: creationId }), 'Create session', accept, source);
    accept(created);
    const { rootId, destination, uploading } = handover!;
    await uploading;
    if (!runtime.connections.isAttached(client)) return;
    const ready = runtime.compositions.get(destination).attachments;
    if (ready.length !== attachments.length || ready.some((item, index) => item.id !== attachments[index]?.id || !item.value)) return;
    const result = await submitChatInput({ runtime, session: client.session(rootId), runtimeId, rootId, agentId: rootId,
      compositionKey: destination, connected: true, text, attachments: ready, delivery: 'queued',
      onAccepted: () => { if (runtime.draft(destination) === text) runtime.setDraft(destination, ''); } });
    if (result.status === 'failed' && !result.delivery) runtime.report(result.error);
  } finally { runtime.compositions.finishSubmission(source, token); }
}
