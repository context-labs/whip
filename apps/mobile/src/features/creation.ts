import type { CommandOutcome } from '@whip/sdk';
import type { ProviderCatalogsResult } from '@whip/protocol';
import type { CommandState, MobileRuntime } from '../runtime/runtime';
import type { Draft } from '../runtime/storage';

export type CreationStep = 'create' | 'effort' | 'submit';
export interface CreationWorkflow {
  version: 1;
  id: string;
  runtimeId: string;
  clientId: string;
  cwd: string;
  model?: string;
  provider?: string;
  effort?: string;
  rootId?: string;
  pendingStep?: CreationStep;
  effortDone: boolean;
  promptSent: boolean;
}
export const creationSettingsKey = (hostId: string) => `creation:${hostId}`;
export const creationDraftKey = (workflow: CreationWorkflow) => JSON.stringify(['create', workflow.runtimeId, workflow.id]);
const operations = { create: 'session.create', effort: 'session.effort', submit: 'submit' } as const;
export function validateWorkflow(value: CreationWorkflow, runtimeId: string, clientId: string): CreationWorkflow {
  if (!value || value.version !== 1 || value.runtimeId !== runtimeId || value.clientId !== clientId || !value.id || typeof value.cwd !== 'string' || typeof value.effortDone !== 'boolean' || typeof value.promptSent !== 'boolean') {
    throw new Error('The saved creation workflow belongs to an unavailable identity or version. Its draft and recovery have been preserved.');
  }
  for (const key of ['id', 'model', 'provider', 'effort', 'rootId'] as const) {
    if (value[key] !== undefined && (typeof value[key] !== 'string' || value[key]!.length > 2048)) throw new Error('The saved creation settings are unreadable; existing data has been preserved.');
  }
  if (value.pendingStep && !['create', 'effort', 'submit'].includes(value.pendingStep)) throw new Error('The saved creation step is unreadable; existing data has been preserved.');
  return value;
}
export function creationCommands(workflow: CreationWorkflow, commands: readonly CommandState[]) {
  return commands.filter(command => command.record.runtimeId === workflow.runtimeId && command.record.clientId === workflow.clientId && command.intent?.workflowId === workflow.id);
}
export function stepCommand(workflow: CreationWorkflow, commands: readonly CommandState[], step: CreationStep) {
  return creationCommands(workflow, commands).findLast(command => command.intent?.step === step && command.record.operation === operations[step]);
}
/** Recover facts only. Rendering/restoration never advances another mutation. */
export function reconcileCreation(workflow: CreationWorkflow, commands: readonly CommandState[]): CreationWorkflow {
  let next = workflow;
  for (const step of ['create', 'effort', 'submit'] as const) {
    const command = stepCommand(next, commands, step);
    if (command?.status !== 'succeeded' || command.outcome?.status !== 'succeeded') continue;
    if (step === 'create') {
      const rootId = (command.outcome.result as { root_id?: string } | undefined)?.root_id;
      if (!rootId) continue;
      if (next.rootId && next.rootId !== rootId) throw new Error('Creation recovery returned a conflicting session. Existing records have been preserved.');
      if (!next.rootId || next.pendingStep === step) next = { ...next, rootId, ...(next.pendingStep === step ? { pendingStep: undefined } : {}) };
    } else if (next.rootId && command.record.rootId === next.rootId) {
      const changed = step === 'effort' ? !next.effortDone : !next.promptSent;
      if (changed || next.pendingStep === step) next = { ...next, [step === 'effort' ? 'effortDone' : 'promptSent']: true, ...(next.pendingStep === step ? { pendingStep: undefined } : {}) };
    }
  }
  return next;
}

/** A successful recovery record can be removed only after its workflow fact is durable. */
export function creationResultRecorded(workflow: CreationWorkflow, command: CommandState): boolean {
  if (command.record.runtimeId !== workflow.runtimeId || command.record.clientId !== workflow.clientId || command.intent?.workflowId !== workflow.id) return false;
  if (command.status !== 'succeeded') return true;
  const step = command.intent.step;
  if (step === 'create') return !!workflow.rootId && (command.outcome?.result as { root_id?: string } | undefined)?.root_id === workflow.rootId;
  if (command.record.rootId !== workflow.rootId) return false;
  if (step === 'effort') return workflow.effortDone;
  if (step === 'submit') return workflow.promptSent;
  return false;
}

export function nextCreationStep(workflow: CreationWorkflow, draft: Draft): CreationStep | undefined {
  if (workflow.pendingStep) return workflow.pendingStep;
  if (!workflow.rootId) return 'create';
  if (workflow.effort && !workflow.effortDone) return 'effort';
  if (draft.text.trim() && !workflow.promptSent) return 'submit';
  return undefined;
}
export function requireCreationSuccess(step: CreationStep, outcome: CommandOutcome): void {
  if (outcome.status !== 'succeeded') throw new Error(`${step === 'create' ? 'Session creation' : step === 'effort' ? 'Reasoning selection' : 'First message'} ${outcome.status}${outcome.failure?.message ? `: ${outcome.failure.message}` : '.'}`);
}
interface CreationRunner {
  run: MobileRuntime['run'];
  current(): boolean;
  save(workflow: CreationWorkflow): Promise<void>;
  saveDraft(key: string, draft: Draft): Promise<void>;
  draft(key: string): Draft;
}
/** One explicit user gesture may perform up to three independently journaled steps. */
export async function advanceCreation(initial: CreationWorkflow, runner: CreationRunner): Promise<CreationWorkflow> {
  if (initial.pendingStep) throw new Error('Resolve the previous creation step before continuing.');
  if (!runner.current()) return initial;
  let workflow = initial;
  const firstDraft = { ...runner.draft(creationDraftKey(initial)) };
  if (firstDraft.text) await runner.saveDraft(creationDraftKey(initial), firstDraft);
  for (let count = 0; count < 3; count++) {
    if (!runner.current()) return workflow;
    const draftKey = creationDraftKey(workflow);
    const draft = { ...runner.draft(draftKey) };
    const step = nextCreationStep(workflow, draft);
    if (!step) return workflow;
    workflow = { ...workflow, pendingStep: step };
    await runner.save(workflow);
    if (!runner.current()) return workflow;
    const intent = { workflowId: workflow.id, step };
    if (step === 'create') {
      const outcome = await runner.run('session.create', { cwd: workflow.cwd.trim(), kind: 'agent', model: workflow.model ?? '', provider: workflow.provider ?? '' }, { intent });
      requireCreationSuccess(step, outcome);
      if (!outcome.result?.root_id) throw new Error('Session creation succeeded without a session identity. Check the original command before continuing.');
      workflow = { ...workflow, rootId: outcome.result.root_id, pendingStep: undefined };
    } else if (step === 'effort') {
      const outcome = await runner.run('session.effort', { effort: workflow.effort!, persist_default: false }, { rootId: workflow.rootId, intent: { ...intent, agentId: workflow.rootId } });
      requireCreationSuccess(step, outcome);
      workflow = { ...workflow, effortDone: true, pendingStep: undefined };
    } else {
      await runner.saveDraft(draftKey, draft);
      if (!runner.current()) return workflow;
      const outcome = await runner.run('submit', { text: draft.text }, {
        rootId: workflow.rootId, intent: { ...intent, agentId: workflow.rootId, draftKey, draftRevision: draft.revision },
        preview: { agentId: workflow.rootId!, text: draft.text, queued: false },
      });
      requireCreationSuccess(step, outcome);
      workflow = { ...workflow, promptSent: true, pendingStep: undefined };
    }
    // Commit the created root before any next step. A late completion may save
    // this journal, but cannot navigate or initiate more work after leaving.
    await runner.save(workflow);
  }
  return workflow;
}

export interface CreationModel { model: string; provider: string; efforts: string[] }
export function creationModels(catalog?: ProviderCatalogsResult): CreationModel[] {
  const models = new Map<string, CreationModel>();
  for (const [model, detail] of Object.entries(catalog?.models ?? {})) {
    for (const provider of detail.providers ?? []) models.set(JSON.stringify([provider, model]), { model, provider, efforts: [] });
  }
  for (const [provider, entry] of Object.entries(catalog?.catalogs ?? {})) {
    for (const model of entry.models ?? []) models.set(JSON.stringify([provider, model.id]), {
      model: model.id, provider, efforts: [...new Set(model.reasoning_efforts ?? [])].filter(Boolean),
    });
  }
  return [...models.values()].sort((a, b) => a.model.localeCompare(b.model) || a.provider.localeCompare(b.provider));
}
