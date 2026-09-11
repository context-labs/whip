import assert from 'node:assert/strict';
import test from 'node:test';
import type { HookContext, ToolContext } from '@whip/sdk/agents';
import { createIncidentCommander } from './incident-commander.js';

const toolContext: ToolContext = { invocationId: 'inv-1', rootId: 'root', agentId: 'agent', turnId: 'turn', deadline: Date.now() + 1000, signal: new AbortController().signal, progress: () => {} };
const hookContext: HookContext = { invocationId: 'hook', rootId: 'root', agentId: 'agent', turnId: 'turn', permissionMode: 'automatic', deadline: Date.now() + 1000, signal: new AbortController().signal };

test('the document declares every primitive', () => {
  const { agent } = createIncidentCommander({ model: { model: 'm', provider: 'p', effort: 'high' } });
  const document = agent.document;
  assert.equal(document.id, 'incident-commander');
  assert.deepEqual(document.instructions.project_files, ['RUNBOOK.md', 'AGENTS.md']);
  assert.deepEqual(document.capabilities, ['read', 'write', 'shell', 'mcp']);
  assert.deepEqual(document.model, { model: 'm', provider: 'p', effort: 'high' });
  assert.equal(document.compaction.threshold, 0.75);
  assert.deepEqual(document.mcp, { servers: ['statuspage'] });
  assert.deepEqual(document.tools?.map(tool => tool.name), ['search_incidents', 'fetch_runbook', 'page_oncall', 'record_timeline']);
  // The wire schema is derived from zod at 2020-12; a zod upgrade that changes it changes the revision, and shows here first.
  assert.deepEqual(document.tools?.[2]?.input_schema, {
    $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object',
    properties: { team: { type: 'string' }, message: { type: 'string', maxLength: 500 } }, required: ['team', 'message'],
  });
  assert.deepEqual(Object.keys(document.children), ['investigator', 'scribe']);
  assert.deepEqual(document.children.investigator.tools, ['search_incidents', 'fetch_runbook']);
  assert.equal(document.children.investigator.report, 'message');
  assert.deepEqual(document.hooks, {
    before_tool: { operations: null, optional: false, timeout_millis: 10_000 },
    before_spawn: { operations: null, optional: false, timeout_millis: 0 },
    turn_start: { operations: null, optional: false, timeout_millis: 0 },
  });
  assert.deepEqual(document.surface, { auto_title: true, goal_loop: false });
  assert.equal(agent.handlers.size, 4);
});

test('tools and hooks behave in-process', async () => {
  const { agent, state } = createIncidentCommander();
  const search = agent.handlers.get('search_incidents')!;
  assert.deepEqual(((await search.execute({ query: 'auth', status: 'open' }, toolContext)) as { id: string }[]).map(incident => incident.id), ['INC-101']);
  const page = agent.handlers.get('page_oncall')!;
  await page.execute({ team: 'auth', message: 'sev2' }, toolContext);
  await page.execute({ team: 'auth', message: 'sev2' }, toolContext);
  assert.equal(state.pages.size, 1, 'a retried invocation pages once');
  await assert.rejects(Promise.resolve(agent.handlers.get('record_timeline')!.execute({ incident: 'INC-999', entry: 'x' }, toolContext)), /unknown incident/);
  const beforeTool = agent.hooks.before_tool!;
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'shell.run', arguments: { command: 'git push --force' } }), { decision: 'deny', reason: 'destructive shell commands are not allowed during incidents' });
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'files.read', arguments: { path: 'config/.env' } }), { arguments: { path: 'config/.env.example' }, reason: 'secrets are redacted' });
  assert.equal(await beforeTool({ ...hookContext, operation: 'files.list', arguments: { path: '.' } }), undefined);
  assert.equal(state.auditLog.length, 3);
  const spawn = { prompt: 'go', name: 'kid', definition: '', capabilities: null, tools: null, budgets: {}, report: '', model: '', provider: '', effort: '' };
  const resolved = { definition: 'incident-commander', modules: ['context'], capabilities: ['read'], tools: null, budgets: {}, report: 'notice' };
  assert.deepEqual(await agent.hooks.before_spawn!({ ...hookContext, spawn, resolved }), { spawn: { ...spawn, definition: 'investigator' }, reason: 'unnamed children investigate' });
  assert.deepEqual(await agent.hooks.before_spawn!({ ...hookContext, spawn: { ...spawn, capabilities: ['shell'] }, resolved: { ...resolved, capabilities: ['shell'] } }), { decision: 'deny', reason: 'children may not hold shell' });
  assert.deepEqual(await agent.hooks.before_spawn!({ ...hookContext, spawn: { ...spawn, definition: 'scribe' }, resolved: { ...resolved, definition: 'incident-commander/scribe', capabilities: ['read'] } }), undefined);
  assert.deepEqual(await agent.hooks.turn_start!({ ...hookContext, input: 'hello' }), { context: 'On call: Sam. Open incidents: 1. Timeline entries: 0.' });
});
