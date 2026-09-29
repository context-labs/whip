// A command-line client for an already running native host. This program never
// starts a runtime or automatically retries an uncertain mutation.
import { Client } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import { browserSocket, discoverGateway } from '@whip/sdk/browser';
const [endpoint, cwd, ...words] = process.argv.slice(2);
if (!endpoint || !cwd || !words.length) throw new Error('Usage: node examples/client/node.mjs <socket-path|http-url> <absolute-host-cwd> <prompt>');
const clientID = process.env.WHIP_CLIENT_ID ?? crypto.randomUUID();
const signal = AbortSignal.timeout(120000);
const network = /^https?:/.test(endpoint);
const info = network ? await discoverGateway(endpoint, { signal, expectedRuntimeID: process.env.WHIP_RUNTIME_ID }) : undefined;
const transport = info ? browserSocket(endpoint, { expectedRuntimeID: info.runtime_id, expectedProcessEpoch: info.process_epoch }) : unixSocket(endpoint);
const client = await Client.connect(transport, { clientID, expectedRuntimeID: process.env.WHIP_RUNTIME_ID ?? info?.runtime_id, signal });
const definition = client.builtins.find(item => item.id === 'coding');
if (!definition) throw new Error('Coding definition unavailable');
const creation = client.command('trees.create', { creation_id: crypto.randomUUID(), definition, working_directory: cwd, metadata: { title: null, pinned: false, archived: false }, overrides: { automatic_title: false } });
// Preserve these exact records in caller-owned durable storage for real workloads.
console.log('Creation recovery:', JSON.stringify(creation.record));
const created = await creation.send({ signal });
if (!created.root) throw new Error('Previously created root was deleted');
const command = client.session(created.root.id).submission([{ type: 'text', text: words.join(' ') }], crypto.randomUUID());
console.log('Submission recovery:', JSON.stringify(command.record));
await command.send({ signal });
const result = await command.wait({ signal });
console.log(JSON.stringify(result, null, 2));
if (result.turn?.state !== 'succeeded') process.exitCode = 1;
// Ordinary native calls own and close their connections; there is no Client.close.
