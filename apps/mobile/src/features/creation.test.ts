/** @jest-environment node */
import type { CommandState, MobileRuntime } from '../runtime/runtime';
import { advanceCreation, creationModels, creationResultRecorded, nextCreationStep, reconcileCreation, validateWorkflow, type CreationWorkflow } from './creation';
const workflow = (): CreationWorkflow => ({ version: 2, id: 'workflow', runtimeId: 'runtime', clientId: 'phone', cwd: '/host/project', definition: { id: 'assistant', revision: 'a'.repeat(64) }, model: { name: 'model', provider: 'provider', effort: 'high' }, executionEngine: 'quickjs', promptSent: false });
const result = { creation: { root_id: 'root', tree_id: 'tree' }, root: { id: 'root' }, deleted: false };
function command(step: 'create' | 'submit', knownAccepted = true): CommandState {
  return { record: { version: 4, runtimeId: 'runtime', clientId: 'phone', commandId: step, operation: step === 'create' ? 'trees.create' : 'sessions.submit', requestHash: 'b'.repeat(64), ...(step === 'submit' ? { rootId: 'root', sessionId: 'root' } : {}) }, intent: { workflowId: 'workflow', step }, status: knownAccepted ? 'accepted' : 'identity_only', knownAccepted, retryable: false, ...(step === 'create' ? { destination: { rootId: 'root', treeId: 'tree', deleted: false } } : {}) };
}
function runner() {
  let current = true;
  const saves: CreationWorkflow[] = [];
  const run = jest.fn(async () => result);
  const save = jest.fn(async (next: CreationWorkflow) => { saves.push(structuredClone(next)); });
  const deps = { run: run as unknown as MobileRuntime['run'], save, current: () => current, saveDraft: jest.fn(async () => {}), draft: () => ({ text: 'First message', revision: 'r1' }) };
  return { run, save, saves, deps, leave() { current = false; } };
}
test('one immutable creation captures definition, engine and complete model before independently journaled first input', async () => {
  const f = runner(); const value = await advanceCreation(workflow(), f.deps);
  expect(value).toMatchObject({ rootId: 'root', promptSent: true });
  expect(f.saves.map(s => [s.pendingStep, s.rootId])).toEqual([['create', undefined], [undefined, 'root'], ['submit', 'root'], [undefined, 'root']]);
  expect(f.run).toHaveBeenNthCalledWith(1, 'trees.create', expect.objectContaining({ creation_id: 'workflow', definition: workflow().definition, engine: 'quickjs', overrides: { model: workflow().model } }), { intent: { workflowId: 'workflow', step: 'create' } });
  expect(f.run).toHaveBeenNthCalledWith(2, 'sessions.submit', expect.objectContaining({ session_id: 'root', identity: { client_id: 'phone', request_id: 'workflow:input' }, parts: [{ type: 'text', text: 'First message' }] }), expect.objectContaining({ rootId: 'root', intent: expect.objectContaining({ draftRevision: 'r1' }) }));
});
test('failed or deleted creation never fabricates a root or advances input', async () => {
  for (const deleted of [false, true]) { const f = runner(); if (deleted) f.run.mockResolvedValue({ ...result, root: null, deleted: true } as never); else f.run.mockRejectedValue(new Error('lost acknowledgement'));
    await expect(advanceCreation(workflow(), f.deps)).rejects.toThrow(); expect(f.run).toHaveBeenCalledTimes(1); expect(f.saves.at(-1)?.rootId).toBeUndefined(); }
});
test('saved root survives failed first message and recovery never advances another mutation', async () => {
  const f = runner(); f.run.mockResolvedValueOnce(result).mockRejectedValueOnce(new Error('lost input'));
  await expect(advanceCreation(workflow(), f.deps)).rejects.toThrow('lost input'); expect(f.saves.at(-1)).toMatchObject({ rootId: 'root', pendingStep: 'submit' });
  const recovered = reconcileCreation(f.saves.at(-1)!, [command('submit')]); expect(recovered).toMatchObject({ rootId: 'root', promptSent: true, pendingStep: undefined }); expect(f.run).toHaveBeenCalledTimes(2);
});
test('durable preparation and original draft precede every effect; leaving during create only saves its known result', async () => {
  const f = runner(); f.deps.saveDraft.mockRejectedValueOnce(new Error('unsaved draft')); await expect(advanceCreation(workflow(), f.deps)).rejects.toThrow('unsaved draft'); expect(f.run).not.toHaveBeenCalled();
  const g = runner(); g.save.mockRejectedValueOnce(new Error('disk full')); await expect(advanceCreation(workflow(), g.deps)).rejects.toThrow('disk full'); expect(g.run).not.toHaveBeenCalled();
  const h = runner(); h.run.mockImplementationOnce(async () => { h.leave(); return result; }); expect(await advanceCreation(workflow(), h.deps)).toMatchObject({ rootId: 'root', promptSent: false }); expect(h.run).toHaveBeenCalledTimes(1);
});
test('metadata-only inspection cannot turn an identity into payload proof; scope and conflicting roots are checked', () => {
  expect(reconcileCreation(workflow(), [command('create', false)])).toEqual(workflow());
  const accepted = reconcileCreation(workflow(), [command('create')]); expect(accepted.rootId).toBe('root');
  expect(creationResultRecorded(workflow(), command('create'))).toBe(false); expect(creationResultRecorded(accepted, command('create'))).toBe(true);
  expect(() => reconcileCreation({ ...workflow(), rootId: 'different' }, [command('create')])).toThrow('conflicting');
  for (const field of ['runtimeId', 'clientId'] as const) { const c = command('create'); c.record[field] = 'other'; expect(reconcileCreation(workflow(), [c])).toEqual(workflow()); }
  const child = command('submit'); child.record.sessionId = 'child'; expect(reconcileCreation(accepted, [child]).promptSent).toBe(false);
});
test('older workflows are preserved without normalization, exact definition revision and model fields validated', () => {
  for (const patch of [{ version: 1 }, { definition: { id: 'assistant', revision: '' } }, { executionEngine: 'unknown' }, { model: { name: 'incomplete' } }]) expect(() => validateWorkflow({ ...workflow(), ...patch } as CreationWorkflow, 'runtime', 'phone')).toThrow();
  expect(nextCreationStep({ ...workflow(), rootId: 'root' }, { text: '', revision: '' })).toBeUndefined();
});
test('uncatalogued configured models remain selectable and null pricing never enters model choice arithmetic', () => {
  const data = { inventory: { routes: [{ id: 'provider', models: { configured: {} } }], defaults: { provider: 'provider', name: 'default' } }, catalogs: [{ provider: 'provider', models: [{ id: 'catalogued', reasoning_efforts: ['low', 'high'], prices: { input: null, output: '9007199254740993' } }] }] };
  expect(creationModels(data as never)).toEqual([{ model: 'catalogued', provider: 'provider', efforts: ['low', 'high'] }, { model: 'configured', provider: 'provider', efforts: [] }, { model: 'default', provider: 'provider', efforts: [] }]);
});
