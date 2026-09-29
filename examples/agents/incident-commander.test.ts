import assert from 'node:assert/strict';
import test from 'node:test';
import type { HookContext, ToolContext } from '@whip/sdk/agents';
import { createIncidentCommander } from './incident-commander.js';
const context: ToolContext = { invocationID: 'inv-1', sessionID: 'agent', turnID: 'turn', operationID: 'operation', origin: 'cell', deadline: Date.now() + 1000, signal: new AbortController().signal, progress: async () => {} };
const hook: HookContext = { ...context, permissionMode: 'automatic' };
const refs = { investigator: { id: 'investigator', revision: 'a'.repeat(64) }, scribe: { id: 'scribe', revision: 'b'.repeat(64) } };

test('native documents preserve model, compaction, named children, schemas and required hooks', () => {
  const example = createIncidentCommander({ model: { name: 'm', provider: 'p', effort: 'high' } });
  const agent = example.agent(refs), defaults = agent.document.defaults;
  assert.deepEqual(defaults.instructions?.project_files, ['RUNBOOK.md', 'AGENTS.md']);
  assert.deepEqual(defaults.model, { name: 'm', provider: 'p', effort: 'high' });
  assert.equal(defaults.compaction?.threshold_percent, 75);
  assert.deepEqual(defaults.mcp_servers, { all: false, servers: ['statuspage'] });
  assert.deepEqual(Object.keys(defaults.tools!), ['search_incidents', 'fetch_runbook', 'page_oncall', 'record_timeline']);
  assert.deepEqual(defaults.tools!.page_oncall!.input_schema, {
    $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object',
    properties: { team: { type: 'string' }, message: { type: 'string', maxLength: 500 } }, required: ['team', 'message'],
  });
  assert.deepEqual(defaults.children, refs);
  assert.deepEqual(Object.keys(example.investigator.handlers), ['search_incidents', 'fetch_runbook']);
  assert.equal(example.investigator.document.defaults.report_mode, 'message');
  assert.deepEqual(defaults.hooks, example.investigator.document.defaults.hooks);
  assert.deepEqual(defaults.hooks, example.scribe.document.defaults.hooks);
});

test('typed tools keep business receipts and hooks rewrite or deny before effects', async () => {
  const { tools, state, hooks } = createIncidentCommander();
  assert.deepEqual((await tools.searchIncidents.execute({ query: 'auth', status: 'open' }, context)).map(incident => incident.id), ['INC-101']);
  await tools.pageOncall.execute({ team: 'auth', message: 'sev2' }, context);
  await tools.pageOncall.execute({ team: 'auth', message: 'sev2' }, context);
  assert.equal(state.pages.size, 1);
  await assert.rejects(Promise.resolve(tools.recordTimeline.execute({ incident: 'INC-999', entry: 'x' }, context)), /unknown incident/);
  assert.deepEqual(await hooks.beforeTool({ ...hook, operation: 'shell.run', arguments: { command: 'git push --force' } }), { decision: 'deny', reason: 'destructive shell commands are not allowed during incidents' });
  assert.deepEqual(await hooks.beforeTool({ ...hook, operation: 'files.read', arguments: { path: 'config/.env' } }), { arguments: { path: 'config/.env.example' }, reason: 'secrets are redacted' });
  assert.equal(await hooks.beforeTool({ ...hook, operation: 'files.list', arguments: { path: '.' } }), undefined);
  assert.equal(state.auditLog.length, 3);
  assert.deepEqual(await hooks.turnStart({ ...hook, input: 'hello' }), { context: 'On call: Sam. Open incidents: 1. Timeline entries: 0.' });
});
