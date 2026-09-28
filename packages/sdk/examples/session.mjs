import { Client } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';

const socket = process.argv[2];
if (!socket) throw new Error('Usage: node packages/sdk/examples/session.mjs /private/runtime/runtime.sock');
const signal = AbortSignal.timeout(15_000);
const client = await Client.connect(unixSocket(socket), { clientID: 'example', signal });
const { root } = await client.call('trees.create', {
  metadata: { title: 'First v4 session', archived: false, pinned: false },
  engine: 'starlark',
  policy: { max_depth: 8, max_sessions: 100, max_queued_inputs_per_session: 100 },
  definition: client.builtins[0],
  overrides: { model: { provider: 'scripted', name: 'scripted', effort: '' } },
  working_directory: process.cwd(),
}, { signal });
const requestID = 'hello-' + crypto.randomUUID();
const accepted = await client.submit(root.id, [{ type: 'text', text: 'hello' }], requestID, { signal });
const completed = await client.wait(requestID, { signal });
const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, { signal });
console.log(JSON.stringify({ runtime_id: client.runtimeID, accepted, completed, history }, null, 2));
