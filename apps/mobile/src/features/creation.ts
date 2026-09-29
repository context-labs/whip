import { assertValid } from '@whip/protocol';
import type { Client, DefinitionRef, ProviderCatalog, ProviderInventory } from '@whip/sdk';
import type { CommandState, MobileRuntime } from '../runtime/runtime';
import type { Draft } from '../runtime/storage';

export type ModelSelection = import('@whip/protocol').Session['configuration']['model'];
export type CreationStep = 'create' | 'submit';
export interface CreationWorkflow {
  version: 2; id: string; runtimeId: string; clientId: string; cwd: string;
  definition: DefinitionRef; model?: ModelSelection; executionEngine: 'starlark' | 'quickjs';
  rootId?: string; pendingStep?: CreationStep; promptSent: boolean;
}
export const creationSettingsKey = (hostId: string) => `creation:${hostId}`;
export const creationDraftKey = (workflow: CreationWorkflow) => JSON.stringify(['create', workflow.runtimeId, workflow.id]);
const operations = { create: 'trees.create', submit: 'sessions.submit' } as const;
export function validateWorkflow(value: CreationWorkflow, runtimeId: string, clientId: string): CreationWorkflow {
  if (!value || value.version !== 2 || value.runtimeId !== runtimeId || value.clientId !== clientId || !/^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$/.test(value.id) || typeof value.cwd !== 'string' || value.cwd.length > 2048 || typeof value.promptSent !== 'boolean' || !['starlark', 'quickjs'].includes(value.executionEngine) || value.pendingStep && !['create', 'submit'].includes(value.pendingStep)) throw new Error('The saved creation workflow belongs to an unavailable identity or version. Its draft and recovery have been preserved.');
  assertValid('DefinitionRef', value.definition);
  assertValid('CreateTreeParams', { creation_id: value.id, metadata: { title: null, archived: false, pinned: false }, definition: value.definition, working_directory: value.cwd, overrides: value.model ? { model: value.model } : {} });
  if (value.rootId !== undefined && (typeof value.rootId !== 'string' || !value.rootId || value.rootId.length > 128)) throw new Error('The saved session identity is unreadable. Existing data has been preserved.');
  return value;
}
export function creationCommands(workflow: CreationWorkflow, commands: readonly CommandState[]) {
  return commands.filter(command => command.record.runtimeId === workflow.runtimeId && command.record.clientId === workflow.clientId && command.intent.workflowId === workflow.id);
}
export function stepCommand(workflow: CreationWorkflow, commands: readonly CommandState[], step: CreationStep) {
  return creationCommands(workflow, commands).findLast(command => command.intent.step === step && command.record.operation === operations[step]);
}
/** Restore known facts only. Receipt identity alone cannot confirm the request after restart. */
export function reconcileCreation(workflow: CreationWorkflow, commands: readonly CommandState[]): CreationWorkflow {
  let next = workflow;
  for (const step of ['create', 'submit'] as const) {
    const command = stepCommand(next, commands, step);
    if (!command?.knownAccepted) continue;
    if (step === 'create') {
      const destination = command.destination;
      if (!destination || destination.deleted) continue;
      if (next.rootId && next.rootId !== destination.rootId) throw new Error('Creation recovery returned a conflicting session. Existing records have been preserved.');
      if (!next.rootId || next.pendingStep === step) next = { ...next, rootId: destination.rootId, pendingStep: undefined };
    } else if (next.rootId && command.record.sessionId === next.rootId && (!next.promptSent || next.pendingStep === step)) next = { ...next, promptSent: true, pendingStep: undefined };
  }
  return next;
}
export function creationResultRecorded(workflow: CreationWorkflow, command: CommandState): boolean {
  if (command.record.runtimeId !== workflow.runtimeId || command.record.clientId !== workflow.clientId || command.intent.workflowId !== workflow.id) return false;
  if (!command.knownAccepted) return true;
  if (command.intent.step === 'create') return !!workflow.rootId && command.destination?.rootId === workflow.rootId;
  return command.intent.step === 'submit' && command.record.sessionId === workflow.rootId && workflow.promptSent;
}
export function nextCreationStep(workflow: CreationWorkflow, draft: Draft): CreationStep | undefined {
  return workflow.pendingStep ?? (!workflow.rootId ? 'create' : draft.text.trim() && !workflow.promptSent ? 'submit' : undefined);
}
interface CreationRunner {
  run: MobileRuntime['run']; current(): boolean; save(workflow: CreationWorkflow): Promise<void>;
  saveDraft(key: string, draft: Draft): Promise<void>; draft(key: string): Draft;
}
/** One explicit gesture creates one immutable configuration, then optionally submits.
 * Stable identities never change when a prepared step is revisited. */
export async function advanceCreation(initial: CreationWorkflow, runner: CreationRunner): Promise<CreationWorkflow> {
  if (initial.pendingStep) throw new Error('Resolve the previous creation step before continuing.');
  let workflow = { ...validateWorkflow(initial, initial.runtimeId, initial.clientId) };
  for (let count = 0; count < 2 && runner.current(); count++) {
    const key = creationDraftKey(workflow), draft = { ...runner.draft(key) }, step = nextCreationStep(workflow, draft);
    if (!step) break;
    if (draft.text) await runner.saveDraft(key, draft);
    workflow = { ...workflow, pendingStep: step }; await runner.save(workflow);
    if (!runner.current()) return workflow;
    const intent = { workflowId: workflow.id, step };
    if (step === 'create') {
      const result = await runner.run('trees.create', { creation_id: workflow.id, metadata: { title: null, archived: false, pinned: false }, definition: workflow.definition,
        working_directory: workflow.cwd.trim(), engine: workflow.executionEngine, overrides: workflow.model ? { model: workflow.model } : {} }, { intent });
      if (result.deleted || !result.root) throw new Error('The created session has been deleted. Its original identity will not be reused.');
      workflow = { ...workflow, rootId: result.creation.root_id, pendingStep: undefined };
    } else {
      await runner.run('sessions.submit', { session_id: workflow.rootId!, source: 'user', parts: [{ type: 'text', text: draft.text }], identity: { client_id: workflow.clientId, request_id: workflow.id + ':input' } }, {
        rootId: workflow.rootId, intent: { ...intent, draftKey: key, draftRevision: draft.revision },
      });
      workflow = { ...workflow, promptSent: true, pendingStep: undefined };
    }
    // Save the root before any submission. Late completion may save its fact,
    // but cannot initiate more work after this screen leaves the foreground.
    await runner.save(workflow);
  }
  return workflow;
}
export interface CreationModel { model: string; provider: string; efforts: string[] }
export interface MobileCatalog { inventory: ProviderInventory; catalogs: ProviderCatalog[] }
export function creationModels(data?: MobileCatalog): CreationModel[] {
  const models = new Map<string, CreationModel>();
  const add = (model: string, provider: string, efforts: string[] = []) => models.set(JSON.stringify([provider, model]), { model, provider, efforts });
  for (const route of data?.inventory.routes ?? []) for (const name of Object.keys(route.models ?? {})) add(name, route.id);
  for (const catalog of data?.catalogs ?? []) for (const model of catalog.models) add(model.id, catalog.provider, model.reasoning_efforts ?? []);
  const defaults = data?.inventory.defaults; if (defaults && !models.has(JSON.stringify([defaults.provider, defaults.name]))) add(defaults.name, defaults.provider);
  return [...models.values()].sort((a, b) => a.model.localeCompare(b.model) || a.provider.localeCompare(b.provider));
}
/** Cached catalogs only. This never refreshes remote accounts or executes a credential command. */
export async function readModels(client: Client, signal?: AbortSignal): Promise<MobileCatalog> {
  const options = { signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000) };
  const inventory = await client.listProviders(options), catalogs: ProviderCatalog[] = [];
  let bytes = new TextEncoder().encode(JSON.stringify(inventory)).byteLength;
  if (bytes > 1 << 20) throw new Error('Provider configuration exceeds the 1 MiB mobile limit.');
  for (let start = 0; start < inventory.routes.length; start += 4) {
    const batch = await Promise.all(inventory.routes.slice(start, start + 4).map(route => client.providerCatalog(route.id, options)));
    bytes += new TextEncoder().encode(JSON.stringify(batch)).byteLength;
    if (bytes > 1 << 20) throw new Error('Provider catalogs exceed the 1 MiB mobile limit.');
    catalogs.push(...batch);
  }
  return { inventory, catalogs };
}
