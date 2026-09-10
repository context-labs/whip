import assert from 'node:assert/strict';
import test from 'node:test';
import type { ToolContext } from '@whip/sdk/agents';
import { lookupTicket, supportTriage } from './ticket-lookup.js';

const context: ToolContext = { invocationId: 'inv', rootId: 'root', agentId: 'agent', turnId: 'turn', deadline: Date.now() + 1000, signal: new AbortController().signal, progress: () => {} };

test('the definition catalogs its tool and keeps the handler local', () => {
  assert.deepEqual(supportTriage.document.tools, [{
    name: 'lookup_ticket', description: 'Fetch a support ticket by id. Returns its title and status.',
    input_schema: { type: 'object', properties: { id: { type: 'string', description: 'The ticket id' } }, required: ['id'], additionalProperties: false },
    timeout_millis: 30_000,
  }]);
  assert.equal(supportTriage.handlers.get('lookup_ticket'), lookupTicket.handler);
});

test('the handler resolves known tickets and rejects unknown ones', async () => {
  assert.deepEqual(await lookupTicket.handler({ id: '42' }, context), { id: '42', title: 'Login page times out on mobile', status: 'open' });
  await assert.rejects(Promise.resolve(lookupTicket.handler({ id: '99' }, context)), /ticket 99 not found/);
});
