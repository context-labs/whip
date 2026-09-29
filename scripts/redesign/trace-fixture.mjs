import assert from 'node:assert/strict';
export async function traceAcceptance(runtime,client,createParams,evidence,deadline) {
 const {root}=await client.call('trees.create',{...createParams,creation_id:'trace-evidence'},deadline());
 await client.submit(root.id,[{type:'text',text:'trace output'}],'trace-output',deadline());
 const accepted=await client.wait('trace-output',deadline());assert.equal(accepted.turn.state,'succeeded');
 const params={root_id:root.id,after:'0',expected_revision:null,trace_id:'',roots_only:false,limit:1,max_bytes:524288};
 const first=await client.tracePage(params,deadline());assert.equal(first.has_more,true);
 const second=await client.tracePage({...params,after:first.next,expected_revision:first.revision},deadline());assert.equal(second.has_more,false);
 const rows=[...first.items,...second.items];assert.deepEqual(rows.map(row=>row.source_kind).sort(),['attempt','turn']);
 const attempt=rows.find(row=>row.source_kind==='attempt');assert.equal(attempt.span.attributes.find(attribute=>attribute.key==='whip.input.body_available').flag,false);
 const exported=await client.exportTrace({root_id:root.id,trace_id:'',expected_revision:second.revision},deadline());
 assert.equal(exported.spans,2);assert.equal(exported.reference.session_id,root.id);
 const content=await client.call('content.read',{session_id:root.id,reference_id:exported.reference.id},deadline());
 const decoded=JSON.parse(Buffer.from(content.data_base64,'base64').toString('utf8'));
 const spans=decoded.resourceSpans[0].scopeSpans[0].spans;assert.equal(spans.length,2);
 assert.ok(spans.find(span=>span.kind===3).attributes.find(attribute=>attribute.key==='output.value').value.stringValue.includes('ack: trace output'));
 assert.equal(spans.some(span=>span.attributes.some(attribute=>attribute.key==='gen_ai.input.messages')),false);
 await runtime.stop();await runtime.start('0');
 assert.deepEqual(await client.exportTrace({root_id:root.id,trace_id:'',expected_revision:second.revision},deadline()),exported);
 await client.call('sessions.delete',{session_id:root.id},deadline());
 const deleted=await client.tracePage({...params,after:second.next,limit:10},deadline());assert.equal(deleted.items.length,2);assert.ok(deleted.items.every(row=>row.span===null));
 await assert.rejects(client.exportTrace({root_id:root.id,trace_id:'',expected_revision:null},deadline()),error=>error.kind==='NOT_FOUND');
 evidence.push({trace:{revision:second.revision,exported,deleted}});
}
