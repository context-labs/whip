import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = type => structuredClone(fixtures.find(value => value.type === type && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });

test('MCP methods perform only explicit actions and preserve safe typed observations', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(structuredClone(request));
    const types = { 'mcp.configuration':'MCPConfiguration', 'mcp.configure':'MCPConfiguration', 'mcp.import.candidates':'MCPImportCandidatesResult', 'mcp.status':'MCPStatusResult', 'mcp.tools':'MCPToolsResult', 'mcp.instructions':'MCPInstructionsResult', 'mcp.brand.icons':'MCPBrandIconsResult' };
    if (request.method === 'mcp.import.apply') return success(request, {configuration: fixture('MCPConfiguration'), added: [], skipped: {}});
    return success(request, fixture(types[request.method] ?? 'MCPRefreshResult'));
  }, { clientID:'mcp-controls' });
  assert.deepEqual(calls, []);
  const configuration = await client.mcpConfiguration();
  assert.deepEqual(configuration.servers, []);
  await client.configureMCP(fixture('ConfigureMCPParams'));
  const candidates = await client.mcpImportCandidates();
  assert.equal(candidates.candidates[0].fingerprint.length, 64);
  await client.importMCP({session_id:null, revision:configuration.revision, fingerprints:{}});
  await client.mcpStatus('root');
  await client.refreshMCP('root');
  await client.reloadMCP('root');
  await client.reconnectMCP('root','fixture');
  await client.enableMCP('root','fixture');
  await client.disableMCP('root','fixture');
  await client.attachMCP({session_id:'root', servers:{fixture:fixture('ConfigureMCPParams').server}});
  assert.equal((await client.mcpTools('root','fixture')).items[0].capability,'mcp.call.trusted');
  assert.equal((await client.mcpInstructions('root','fixture')).bytes,'11');
  await client.mcpBrandIcons(['example.com']);
  assert.deepEqual(calls.map(value=>value.method), ['configuration','configure','import.candidates','import.apply','status','refresh','reload','reconnect','enable','disable','attach','tools','instructions','brand.icons'].map(value=>'mcp.'+value));
  const before=calls.length;
  const forged=fixture('ConfigureMCPParams');forged.server.trusted=true;
  await assert.rejects(client.configureMCP(forged),TypeError);
  await assert.rejects(client.mcpBrandIcons(Array(65).fill('example.com')),TypeError);
  await assert.rejects(client.refreshMCP('root',{signal:AbortSignal.abort()}),error=>error.name==='AbortError');
  assert.equal(calls.length,before);
});

test('lost MCP acknowledgements never replay connect, import, reload or configuration edits', async () => {
  const calls=[];
  const client=await Client.connect(async request=>{
    if(request.method==='initialize')return success(request,initial);
    calls.push(request.method);
    if(request.method==='mcp.status')return success(request,fixture('MCPStatusResult'));
    if(request.method==='mcp.configuration')return success(request,fixture('MCPConfiguration'));
    throw new DeliveryError('lost after acceptance');
  },{clientID:'mcp-recovery'});
  await assert.rejects(client.configureMCP(fixture('ConfigureMCPParams')),DeliveryError);
  await assert.rejects(client.importMCP(fixture('MCPImportParams')),DeliveryError);
  await assert.rejects(client.refreshMCP('root'),DeliveryError);
  await assert.rejects(client.reloadMCP('root'),DeliveryError);
  await assert.rejects(client.reconnectMCP('root','fixture'),DeliveryError);
  await client.mcpStatus('root');await client.mcpConfiguration();
  assert.deepEqual(calls,['mcp.configure','mcp.import.apply','mcp.refresh','mcp.reload','mcp.reconnect','mcp.status','mcp.configuration']);
});

test('MCP selection retains inherit versus none and rejects widening-shaped and duplicate fields', async()=>{
  const calls=[];const session=fixture('Session');session.configuration.mcp_servers={all:false,servers:[]};
  const client=await Client.connect(async request=>{if(request.method==='initialize')return success(request,initial);calls.push(request);return success(request,session)},{clientID:'mcp-selection'});
  const params={session_id:session.id,expected_revision:session.config_revision};
  await client.call('sessions.configure',{...params,patch:{}});
  const result=await client.call('sessions.configure',{...params,patch:{mcp_servers:{all:false,servers:[]}}});
  assert.deepEqual(result.configuration.mcp_servers,{all:false,servers:[]});
  assert.deepEqual(calls.map(value=>value.params.patch),[{}, {mcp_servers:{all:false,servers:[]}}]);
  for(const value of [{all:true,servers:['hidden']},{all:false,servers:['one','one']},{all:false,servers:null}])await assert.rejects(client.call('sessions.configure',{...params,patch:{mcp_servers:value}}),TypeError);
  assert.equal(calls.length,2);
});
