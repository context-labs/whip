import { randomUUID } from 'node:crypto';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import http from 'node:http';
import { join } from 'node:path';

export async function filesLSPAcceptance(runtime, client, createParams, evidence, deadline) {
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    const last = body.messages.at(-1);
    const engine = body.messages.findLast(value => value.role === 'user').content;
    const code = engine === 'starlark'
      ? 'print(files.write(path="main.go",content="// needle"))\nprint(files.list(path="."))\nprint(files.search(path=".",query="needle"))\nprint(files.diagnostics(path="main.go"))'
      : 'print(await files.write({path:"main.go",content:"// needle"}));print(await files.list({path:"."}));print(await files.search({path:".",query:"needle"}));print(await files.diagnostics({path:"main.go"}));';
    const message = last.role === 'tool' ? { role: 'assistant', content: last.content }
      : { role: 'assistant', content: null, tool_calls: [{ id: 'file-lsp', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason: last.role === 'tool' ? 'stop' : 'tool_calls' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    await runtime.stop();
    const languageServer = join(runtime.directory, 'language-server.mjs');
    const pids = join(runtime.directory, 'language-server-pids');
    await writeFile(languageServer, `import { appendFileSync } from 'node:fs';
appendFileSync(process.env.PID_FILE, process.pid + '\\n');
let pending = Buffer.alloc(0);
process.stdin.on('data', data => {
  pending = Buffer.concat([pending, data]);
  if (pending.length > 2 << 20) process.exit(1);
  for (;;) {
    const end = pending.indexOf('\\r\\n\\r\\n'); if (end < 0) return;
    const match = /^Content-Length: ([0-9]+)$/.exec(pending.subarray(0, end).toString()); if (!match) process.exit(1);
    const size = Number(match[1]); if (size > 1 << 20) process.exit(1);
    if (pending.length < end + 4 + size) return;
    const message = JSON.parse(pending.subarray(end + 4, end + 4 + size)); pending = pending.subarray(end + 4 + size);
    let result;
    if (message.method === 'textDocument/didOpen' || message.method === 'textDocument/didChange') result = {jsonrpc:'2.0', method:'textDocument/publishDiagnostics', params:{uri:message.params.textDocument.uri,version:message.params.textDocument.version,diagnostics:[{range:{start:{line:0,character:0}},severity:1,message:'fixture diagnostic'}]}};
    else if (message.id !== undefined) result = {jsonrpc:'2.0',id:message.id,result:{}};
    if (result) { const body = JSON.stringify(result); process.stdout.write('Content-Length: ' + Buffer.byteLength(body) + '\\r\\n\\r\\n' + body); }
  }
});
`);
    const hostPath = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(hostPath, 'utf8'));
    host.lsp = { gopls: { enabled: false }, fixture: { command: [process.execPath, languageServer], extensions: ['.go'], rootMarkers: ['go.mod'], env: { PID_FILE: pids } } };
    host.providers.file_lsp = { kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '', models: { fixture: { max_output_tokens: 128, timeout_millis: 10000, max_attempts: 1 } } };
    await writeFile(hostPath, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const roots = [];
    for (const engine of ['starlark', 'quickjs']) {
      const workspace = join(runtime.directory, `files-lsp-${engine}`); await mkdir(workspace);
      const { root } = await client.call('trees.create', { creation_id: randomUUID(), ...createParams, engine, working_directory: workspace, overrides: { model: { provider: 'file_lsp', name: 'fixture', effort: '' } } }, deadline());
      roots.push(root.id);
      assert.equal((await client.languageServerStatus(root.id, deadline())).items[0].state, 'not_started');
      for (const capability of ['files.write', 'files.list', 'files.search', 'lsp.diagnostics']) await client.call('grants.create', { id: `${engine}-${capability}`, session_id: root.id, capability, resource: root.working_directory }, deadline());
      const requestID = `files-lsp-${engine}`;
      await client.submit(root.id, [{ type: 'text', text: engine }], requestID, deadline());
      const done = await client.wait(requestID, deadline()); assert.equal(done.turn.state, 'succeeded', done.turn.failure);
      const operations = await client.call('turns.operations', { turn_id: done.turn.id, limit: 10 }, deadline());
      assert.deepEqual(operations.items.map(value => value.capability).sort(), ['files.list', 'files.search', 'files.write', 'lsp.diagnostics', 'lsp.diagnostics']);
      assert.ok(operations.items.every(value => value.state === 'succeeded'), JSON.stringify(operations));
      assert.equal((await client.languageServerStatus(root.id, deadline())).items[0].state, 'connected');
      evidence.push({ filesLSP: { engine, done, operations } });
    }
    await runtime.stop();
    for (const pid of (await readFile(pids, 'utf8')).trim().split('\n').map(Number)) assert.throws(() => process.kill(pid, 0), error => error.code === 'ESRCH');
    await runtime.start(null);
    for (const root of roots) assert.equal((await client.languageServerStatus(root, deadline())).items[0].state, 'not_started');
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}
