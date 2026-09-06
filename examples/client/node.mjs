import { createWhipClient } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
const [endpoint, cwd, ...words] = process.argv.slice(2);
if (!endpoint || !cwd || !words.length) throw new Error('Usage: node examples/client/node.mjs <socket-path|http-url> <host-cwd> <prompt>');
const client = createWhipClient({ endpoint: /^https?:|^wss?:/.test(endpoint) ? endpoint : unixSocket(endpoint), clientId: process.env.WHIP_CLIENT_ID ?? crypto.randomUUID() });
try {
  await client.connect();
  const creation = await client.sessions.create({ cwd }).result();
  if (creation.status !== 'succeeded' || !creation.result) throw new Error(creation.failure?.message ?? 'Session creation failed');
  const command = client.session(creation.result.root_id).submit({ text: words.join(' ') });
  console.log('Recovery identity:', command.record);
  await command.accepted();
  const outcome = await command.result();
  console.log(JSON.stringify(outcome, null, 2));
  if (outcome.status !== 'succeeded') process.exitCode = 1;
} finally { client.close(); }
