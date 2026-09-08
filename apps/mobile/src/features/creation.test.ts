/** @jest-environment node */
import type { CommandOutcome } from '@whip/sdk';
import type { CommandState, MobileRuntime } from '../runtime/runtime';
import { advanceCreation, creationDraftKey, creationModels, creationResultRecorded, nextCreationStep, reconcileCreation, validateWorkflow, type CreationWorkflow } from './creation';

const workflow = (): CreationWorkflow => ({ version: 1, id: 'workflow', runtimeId: 'runtime', clientId: 'client', cwd: '/host/project', model: 'model', provider: 'provider', effort: 'high', effortDone: false, promptSent: false });
function outcome(operation: string, result: unknown, status = 'succeeded'): CommandOutcome {
  return { operation, command_id: `id-${operation}`, ingress_seq: '1', status, result } as CommandOutcome;
}
function command(step: 'create' | 'effort' | 'submit', result: unknown, status = 'succeeded'): CommandState {
  const operation = step === 'create' ? 'session.create' : step === 'effort' ? 'session.effort' : 'submit';
  return {
    record: { version: 1, runtimeId: 'runtime', clientId: 'client', commandId: `id-${step}`, operation, ...(step === 'create' ? {} : { rootId: 'root' }) },
    intent: { workflowId: 'workflow', step }, status, accepted: true, outcome: outcome(operation, result, status),
  };
}
function runner() {
  const saves: CreationWorkflow[] = [];
  const operations: string[] = [];
  let current = true;
  let failStep: string | undefined;
  let failSave = false;
  let saveCount = 0;
  const run = jest.fn(async (operation: string) => {
    operations.push(operation);
    return outcome(operation, operation === 'session.create' ? { root_id: 'root' } : {}, operation === failStep ? 'failed' : 'succeeded');
  });
  const save = jest.fn(async (next: CreationWorkflow) => {
    saveCount++;
    if (failSave) throw new Error('disk full');
    saves.push({ ...next });
  });
  return {
    operations, saves, run, save,
    deps: { run: run as MobileRuntime['run'], save, saveDraft: async () => {}, current: () => current, draft: () => ({ text: 'First message', revision: 'revision-1' }) },
    detach: () => { current = false; },
    fail: (step: string) => { failStep = step; },
    failSave: () => { failSave = true; },
    get saveCount() { return saveCount; },
  };
}

test('creates, applies nondefault-persisting effort, and submits with independent journaled intents', async () => {
  const fixture = runner();
  const result = await advanceCreation(workflow(), fixture.deps);
  expect(fixture.operations).toEqual(['session.create', 'session.effort', 'submit']);
  expect(fixture.saves.map(save => [save.pendingStep, save.rootId])).toEqual([
    ['create', undefined], [undefined, 'root'], ['effort', 'root'], [undefined, 'root'], ['submit', 'root'], [undefined, 'root'],
  ]);
  expect(fixture.run).toHaveBeenNthCalledWith(1, 'session.create', { cwd: '/host/project', kind: 'agent', model: 'model', provider: 'provider' }, { intent: { workflowId: 'workflow', step: 'create' } });
  expect(fixture.run).toHaveBeenNthCalledWith(2, 'session.effort', { effort: 'high', persist_default: false }, { rootId: 'root', intent: { workflowId: 'workflow', step: 'effort', agentId: 'root' } });
  expect(fixture.run).toHaveBeenNthCalledWith(3, 'submit', { text: 'First message' }, {
    rootId: 'root', intent: { workflowId: 'workflow', step: 'submit', agentId: 'root', draftKey: creationDraftKey(workflow()), draftRevision: 'revision-1' },
    preview: { agentId: 'root', text: 'First message', queued: false },
  });
  expect(result).toMatchObject({ rootId: 'root', effortDone: true, promptSent: true, pendingStep: undefined });
});

test('structured failure after creation retains its root and never proceeds to first input', async () => {
  const fixture = runner(); fixture.fail('session.effort');
  await expect(advanceCreation(workflow(), fixture.deps)).rejects.toThrow('Reasoning selection failed');
  expect(fixture.operations).toEqual(['session.create', 'session.effort']);
  expect(fixture.saves.at(-1)).toMatchObject({ rootId: 'root', pendingStep: 'effort', promptSent: false });
  const resume = runner();
  await expect(advanceCreation(fixture.saves.at(-1)!, resume.deps)).rejects.toThrow('Resolve the previous');
  expect(resume.run).not.toHaveBeenCalled();
  const resumed = await advanceCreation({ ...fixture.saves.at(-1)!, pendingStep: undefined }, resume.deps);
  expect(resume.operations).toEqual(['session.effort', 'submit']);
  expect(resumed.rootId).toBe('root');
});

test('create failure and successful outcome without root identity cannot manufacture a session', async () => {
  const failure = runner(); failure.fail('session.create');
  await expect(advanceCreation(workflow(), failure.deps)).rejects.toThrow('Session creation failed');
  expect(failure.operations).toEqual(['session.create']);
  expect(failure.saves.at(-1)?.rootId).toBeUndefined();
  const missing = runner(); missing.run.mockImplementation(async op => outcome(op, {}));
  await expect(advanceCreation(workflow(), missing.deps)).rejects.toThrow('without a session identity');
  expect(missing.operations).toEqual([]);
  expect(missing.run).toHaveBeenCalledTimes(1);
});

test('every step waits for durable preparation and leaving during a step prevents the next mutation', async () => {
  const failure = runner(); failure.failSave();
  await expect(advanceCreation(workflow(), failure.deps)).rejects.toThrow('disk full');
  expect(failure.run).not.toHaveBeenCalled();
  const departed = runner();
  departed.run.mockImplementation(async op => { departed.detach(); return outcome(op, { root_id: 'root' }); });
  const result = await advanceCreation(workflow(), departed.deps);
  expect(result.rootId).toBe('root');
  expect(departed.run).toHaveBeenCalledTimes(1);
  expect(departed.saves.at(-1)?.rootId).toBe('root');
});

test('restart reconciliation recovers root and completed steps but never runs more work or clears failures', () => {
  const pending = { ...workflow(), pendingStep: 'create' as const };
  const recovered = reconcileCreation(pending, [command('create', { root_id: 'root' })]);
  expect(recovered).toMatchObject({ rootId: 'root', pendingStep: undefined, effortDone: false, promptSent: false });
  expect(nextCreationStep(recovered, { text: 'draft', revision: 'r' })).toBe('effort');
  const failed = { ...recovered, pendingStep: 'effort' as const };
  expect(reconcileCreation(failed, [command('effort', {}, 'failed')])).toBe(failed);
  const completed = reconcileCreation(failed, [command('effort', {}), command('submit', {})]);
  expect(completed).toMatchObject({ rootId: 'root', effortDone: true, promptSent: true, pendingStep: undefined });
});

test('recovery rejects root conflicts and ignores another client, runtime, workflow, or child root', () => {
  const initial = workflow();
  for (const foreign of [
    { ...command('create', { root_id: 'wrong' }), intent: { workflowId: 'other', step: 'create' } },
    { ...command('create', { root_id: 'wrong' }), record: { ...command('create', {}).record, clientId: 'other' } },
    { ...command('create', { root_id: 'wrong' }), record: { ...command('create', {}).record, runtimeId: 'other' } },
  ]) expect(reconcileCreation(initial, [foreign])).toBe(initial);
  const created = { ...initial, rootId: 'root' };
  expect(() => reconcileCreation(created, [command('create', { root_id: 'wrong' })])).toThrow('conflicting session');
  expect(reconcileCreation(created, [{ ...command('effort', {}), record: { ...command('effort', {}).record, rootId: 'other-root' } }])).toBe(created);
});

test('host defaults omit optional steps and only catalog-supported reasoning levels appear', async () => {
  const fixture = runner();
  await advanceCreation({ ...workflow(), model: undefined, provider: undefined, effort: undefined }, { ...fixture.deps, draft: () => ({ text: '', revision: '' }) });
  expect(fixture.run).toHaveBeenCalledTimes(1);
  expect(fixture.run).toHaveBeenCalledWith('session.create', { cwd: '/host/project', kind: 'agent', model: '', provider: '' }, expect.anything());
  expect(creationModels({ models: { model: { providers: ['provider'] } }, providers: {}, catalogs: { provider: { fetched_at: '', base_url: '', models: [{ id: 'model', reasoning_efforts: ['low', 'high', 'high'] }] } } })).toEqual([{ model: 'model', provider: 'provider', efforts: ['low', 'high'] }]);
  expect(() => validateWorkflow(workflow(), 'another-runtime', 'client')).toThrow('unavailable identity');
});


test('unsaved first drafts block creation and successful recovery cannot be forgotten before its journal', async () => {
  const fixture = runner();
  await expect(advanceCreation(workflow(), { ...fixture.deps, saveDraft: async () => { throw new Error('draft quota'); } })).rejects.toThrow('draft quota');
  expect(fixture.run).not.toHaveBeenCalled();
  const created = command('create', { root_id: 'root' });
  expect(creationResultRecorded(workflow(), created)).toBe(false);
  expect(creationResultRecorded({ ...workflow(), rootId: 'root' }, created)).toBe(true);
  expect(creationResultRecorded({ ...workflow(), rootId: 'root' }, command('effort', {}))).toBe(false);
  expect(creationResultRecorded({ ...workflow(), rootId: 'root', effortDone: true }, command('effort', {}))).toBe(true);
  expect(creationResultRecorded({ ...workflow(), rootId: 'root' }, command('submit', {}))).toBe(false);
  expect(creationResultRecorded({ ...workflow(), rootId: 'root', promptSent: true }, command('submit', {}))).toBe(true);
});
