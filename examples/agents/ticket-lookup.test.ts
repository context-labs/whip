import assert from 'node:assert/strict';
import test from 'node:test';
import type { HookContext, ToolContext } from '@whip/sdk/agents';
import { auditLog, lookupTicket, supportTriage } from './ticket-lookup.js';

const context: ToolContext = { invocationId: 'inv', rootId: 'root', agentId: 'agent', turnId: 'turn', deadline: Date.now() + 1000, signal: new AbortController().signal, progress: () => {} };

test('the definition catalogs its tool and keeps the handler local', () => {
  // Derived from the zod input at draft 2020-12; a zod upgrade that changes this changes the definition's revision.
  assert.deepEqual(supportTriage.document.tools, [{
    name: 'lookup_ticket', description: 'Fetch a support ticket by id. Returns its title and status.',
    input_schema: { $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object', properties: { id: { type: 'string', description: 'The ticket id' } }, required: ['id'] },
    timeout_millis: 30_000,
  }]);
  assert.equal(supportTriage.handlers.get('lookup_ticket'), lookupTicket);
  assert.equal(lookupTicket.validate, true);
});

test('the handler resolves known tickets and rejects unknown ones', async () => {
  assert.deepEqual(await lookupTicket.execute({ id: '42' }, context), { id: '42', title: 'Login page times out on mobile', status: 'open' });
  await assert.rejects(Promise.resolve(lookupTicket.execute({ id: '99' }, context)), /ticket 99 not found/);
});

const hookContext: HookContext = { invocationId: 'hook', rootId: 'root', agentId: 'agent', turnId: 'turn', permissionMode: 'automatic', deadline: Date.now() + 1000, signal: new AbortController().signal };

test('the hooks observe, deny, rewrite, and contribute', async () => {
  assert.deepEqual(supportTriage.document.hooks, {
    before_tool: { operations: null, optional: false, timeout_millis: 0 },
    before_spawn: { operations: null, optional: false, timeout_millis: 0 },
    turn_start: { operations: null, optional: false, timeout_millis: 0 },
  });
  const beforeTool = supportTriage.hooks.before_tool!;
  assert.equal(await beforeTool({ ...hookContext, operation: 'files.list', arguments: { path: '.' } }), undefined);
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'shell.run', arguments: { command: 'rm -rf build' } }), { decision: 'deny', reason: 'destructive shell commands are not allowed' });
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'files.read', arguments: { path: 'config/.env' } }), { arguments: { path: 'config/.env.example' }, reason: 'secrets are redacted' });
  assert.deepEqual(auditLog, ['agent files.list', 'agent shell.run', 'agent files.read']);
  const beforeSpawn = supportTriage.hooks.before_spawn!;
  const spawn = { prompt: 'go', name: 'kid', definition: '', capabilities: null, tools: null, budgets: {}, report: '', model: '', provider: '', effort: '' };
  assert.deepEqual(await beforeSpawn({ ...hookContext, spawn, resolved: { definition: 'support-triage', modules: ['context'], capabilities: ['read'], tools: null, budgets: {}, report: 'notice' } }), { spawn: { ...spawn, definition: 'researcher' }, reason: 'all children run as researcher' });
  assert.deepEqual(await beforeSpawn({ ...hookContext, spawn, resolved: { definition: 'support-triage', modules: ['context'], capabilities: ['shell'], tools: null, budgets: {}, report: 'notice' } }), { decision: 'deny', reason: 'children may not hold shell' });
  const turnStart = supportTriage.hooks.turn_start!;
  assert.deepEqual(await turnStart({ ...hookContext, input: 'hello' }), { context: 'On call: Sam. Open tickets: 1.' });
});
