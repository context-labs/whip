// Generated standalone validators. Run npm run generate.
"use strict";
export const Accepted = validate10;
const schema11 = {"type":"object","properties":{"accepted":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/Accepted","$schema":"http://json-schema.org/draft-07/schema#","title":"Accepted","required":["accepted"],"additionalProperties":true};

function validate10(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/Accepted" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.accepted === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "accepted"},message:"must have required property '"+"accepted"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.accepted !== undefined){
if(typeof data.accepted !== "boolean"){
const err1 = {instancePath:instancePath+"/accepted",schemaPath:"#/properties/accepted/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate10.errors = vErrors;
return errors === 0;
}

export const AgentCancelParams = validate11;
const schema12 = {"type":"object","properties":{"id":{"type":"string"},"turn_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/AgentCancelParams","$schema":"http://json-schema.org/draft-07/schema#","title":"AgentCancelParams","required":["id","turn_id"],"additionalProperties":true};

function validate11(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/AgentCancelParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.turn_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err2 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.turn_id !== undefined){
if(typeof data.turn_id !== "string"){
const err3 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate11.errors = vErrors;
return errors === 0;
}

export const AgentInputParams = validate12;
const schema13 = {"type":"object","properties":{"id":{"type":"string"},"text":{"type":"string"},"delivery":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/AgentInputParams","$schema":"http://json-schema.org/draft-07/schema#","title":"AgentInputParams","required":["id","text"],"additionalProperties":true};

function validate12(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/AgentInputParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.text === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err2 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.text !== undefined){
if(typeof data.text !== "string"){
const err3 = {instancePath:instancePath+"/text",schemaPath:"#/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.delivery !== undefined){
if(typeof data.delivery !== "string"){
const err4 = {instancePath:instancePath+"/delivery",schemaPath:"#/properties/delivery/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
}
else {
const err5 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
validate12.errors = vErrors;
return errors === 0;
}

export const AgentListResult = validate13;
const schema14 = {"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"root_id":{"type":"string"},"parent_id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"effort":{"type":"string"},"cwd":{"type":"string"},"report":{"type":"string"},"status":{"type":"string"},"pending_mail":{"type":"integer"},"lifecycle_phase":{"type":"string"},"blocking_reason":{"type":"string"},"terminal_cause":{"type":"string"},"allowed_controls":{"type":["null","array"],"items":{"type":"string"}}},"required":["id","root_id","parent_id","name","model","provider","effort","cwd","report","status","pending_mail","lifecycle_phase","blocking_reason","terminal_cause","allowed_controls"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/AgentListResult","$schema":"http://json-schema.org/draft-07/schema#","title":"AgentListResult"};

function validate13(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/AgentListResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema14.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.id === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.root_id === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.parent_id === undefined){
const err3 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.name === undefined){
const err4 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.model === undefined){
const err5 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.provider === undefined){
const err6 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data0.effort === undefined){
const err7 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data0.cwd === undefined){
const err8 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data0.report === undefined){
const err9 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "report"},message:"must have required property '"+"report"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data0.status === undefined){
const err10 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data0.pending_mail === undefined){
const err11 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "pending_mail"},message:"must have required property '"+"pending_mail"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data0.lifecycle_phase === undefined){
const err12 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "lifecycle_phase"},message:"must have required property '"+"lifecycle_phase"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data0.blocking_reason === undefined){
const err13 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "blocking_reason"},message:"must have required property '"+"blocking_reason"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data0.terminal_cause === undefined){
const err14 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "terminal_cause"},message:"must have required property '"+"terminal_cause"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data0.allowed_controls === undefined){
const err15 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "allowed_controls"},message:"must have required property '"+"allowed_controls"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data0.id !== undefined){
if(typeof data0.id !== "string"){
const err16 = {instancePath:instancePath+"/" + i0+"/id",schemaPath:"#/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data0.root_id !== undefined){
if(typeof data0.root_id !== "string"){
const err17 = {instancePath:instancePath+"/" + i0+"/root_id",schemaPath:"#/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data0.parent_id !== undefined){
if(typeof data0.parent_id !== "string"){
const err18 = {instancePath:instancePath+"/" + i0+"/parent_id",schemaPath:"#/items/properties/parent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data0.name !== undefined){
if(typeof data0.name !== "string"){
const err19 = {instancePath:instancePath+"/" + i0+"/name",schemaPath:"#/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data0.model !== undefined){
if(typeof data0.model !== "string"){
const err20 = {instancePath:instancePath+"/" + i0+"/model",schemaPath:"#/items/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data0.provider !== undefined){
if(typeof data0.provider !== "string"){
const err21 = {instancePath:instancePath+"/" + i0+"/provider",schemaPath:"#/items/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data0.effort !== undefined){
if(typeof data0.effort !== "string"){
const err22 = {instancePath:instancePath+"/" + i0+"/effort",schemaPath:"#/items/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data0.cwd !== undefined){
if(typeof data0.cwd !== "string"){
const err23 = {instancePath:instancePath+"/" + i0+"/cwd",schemaPath:"#/items/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data0.report !== undefined){
if(typeof data0.report !== "string"){
const err24 = {instancePath:instancePath+"/" + i0+"/report",schemaPath:"#/items/properties/report/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data0.status !== undefined){
if(typeof data0.status !== "string"){
const err25 = {instancePath:instancePath+"/" + i0+"/status",schemaPath:"#/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data0.pending_mail !== undefined){
let data11 = data0.pending_mail;
if(!((typeof data11 == "number") && (!(data11 % 1) && !isNaN(data11)))){
const err26 = {instancePath:instancePath+"/" + i0+"/pending_mail",schemaPath:"#/items/properties/pending_mail/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data0.lifecycle_phase !== undefined){
if(typeof data0.lifecycle_phase !== "string"){
const err27 = {instancePath:instancePath+"/" + i0+"/lifecycle_phase",schemaPath:"#/items/properties/lifecycle_phase/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data0.blocking_reason !== undefined){
if(typeof data0.blocking_reason !== "string"){
const err28 = {instancePath:instancePath+"/" + i0+"/blocking_reason",schemaPath:"#/items/properties/blocking_reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data0.terminal_cause !== undefined){
if(typeof data0.terminal_cause !== "string"){
const err29 = {instancePath:instancePath+"/" + i0+"/terminal_cause",schemaPath:"#/items/properties/terminal_cause/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data0.allowed_controls !== undefined){
let data15 = data0.allowed_controls;
if((data15 !== null) && (!(Array.isArray(data15)))){
const err30 = {instancePath:instancePath+"/" + i0+"/allowed_controls",schemaPath:"#/items/properties/allowed_controls/type",keyword:"type",params:{type: schema14.items.properties.allowed_controls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(Array.isArray(data15)){
const len1 = data15.length;
for(let i1=0; i1<len1; i1++){
if(typeof data15[i1] !== "string"){
const err31 = {instancePath:instancePath+"/" + i0+"/allowed_controls/" + i1,schemaPath:"#/items/properties/allowed_controls/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
}
}
}
else {
const err32 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
}
validate13.errors = vErrors;
return errors === 0;
}

export const AgentSubmitResult = validate14;
const schema15 = {"type":"object","properties":{"agent_id":{"type":"string"},"inbox_seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"status":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/AgentSubmitResult","$schema":"http://json-schema.org/draft-07/schema#","title":"AgentSubmitResult","required":["agent_id","inbox_seq","status"],"additionalProperties":true};
const formats0 = {int64: {type: 'string', validate: value => {
  if (!/^-?(0|[1-9][0-9]*)$/.test(value) || value.length > 20) return false;
  const number = BigInt(value);
  return number >= -9223372036854775808n && number <= 9223372036854775807n;
}}}.int64;
const pattern0 = new RegExp("^-?(0|[1-9][0-9]*)$", "u");

function validate14(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/AgentSubmitResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.agent_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.inbox_seq === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "inbox_seq"},message:"must have required property '"+"inbox_seq"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.status === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err3 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.inbox_seq !== undefined){
let data1 = data.inbox_seq;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err4 = {instancePath:instancePath+"/inbox_seq",schemaPath:"#/properties/inbox_seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(!(formats0.validate(data1))){
const err5 = {instancePath:instancePath+"/inbox_seq",schemaPath:"#/properties/inbox_seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
else {
const err6 = {instancePath:instancePath+"/inbox_seq",schemaPath:"#/properties/inbox_seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err7 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.status !== undefined){
if(typeof data.status !== "string"){
const err8 = {instancePath:instancePath+"/status",schemaPath:"#/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
else {
const err9 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
validate14.errors = vErrors;
return errors === 0;
}

export const AgentTranscriptResult = validate15;
const schema16 = {"type":"object","properties":{"cursor":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"agent":{"type":"object","properties":{"id":{"type":"string"},"root_id":{"type":"string"},"parent_id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"effort":{"type":"string"},"cwd":{"type":"string"},"report":{"type":"string"},"status":{"type":"string"},"pending_mail":{"type":"integer"},"lifecycle_phase":{"type":"string"},"blocking_reason":{"type":"string"},"terminal_cause":{"type":"string"},"allowed_controls":{"type":["null","array"],"items":{"type":"string"}}},"required":["id","root_id","parent_id","name","model","provider","effort","cwd","report","status","pending_mail","lifecycle_phase","blocking_reason","terminal_cause","allowed_controls"],"additionalProperties":true},"page":{"type":"object","properties":{"history_revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"through_seq":{"type":"integer"},"next_seq":{"type":"integer"},"has_more":{"type":"boolean"},"messages":{"type":["null","array"],"items":{"type":"object","properties":{"role":{"type":"string"},"authored":{"type":"boolean"},"sent_at":{"type":["null","string"]},"seq":{"type":"integer"},"message":{"type":["null","object"],"properties":{"role":{"type":"string"},"content":{"type":"string"},"tool_calls":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"function":{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"string"}},"required":["name","arguments"],"additionalProperties":true},"duration_ms":{"type":"integer"},"exit_code":{"type":"integer"}},"required":["id","type","function"],"additionalProperties":true}},"tool_call_id":{"type":"string"},"name":{"type":"string"},"authored":{"type":"boolean"},"sent_at":{"type":["null","string"]},"usage":{"type":["null","object"],"properties":{"prompt_tokens":{"type":"integer"},"completion_tokens":{"type":"integer"},"prompt_tokens_details":{"type":["null","object"],"properties":{"cached_tokens":{"type":"integer"}},"required":["cached_tokens"],"additionalProperties":true}},"required":["prompt_tokens","completion_tokens"],"additionalProperties":true},"model":{"type":"string"},"rewound_from":{"type":"string"}},"required":["role","content"],"additionalProperties":true},"body":{"type":["null","object"],"properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["seq"],"additionalProperties":true}}},"required":["history_revision","through_seq","next_seq","has_more","messages"],"additionalProperties":true},"presentation":{"type":["null","array"],"items":{"type":"object","properties":{"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"payload":true},"required":["seq","kind","payload"],"additionalProperties":true}},"inbox":{"type":["null","array"],"items":{"type":"object","properties":{"root_id":{"type":"string"},"agent_id":{"type":"string"},"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"status":{"type":"string"},"payload":{"type":"object","properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["root_id","agent_id","seq","kind","status","payload"],"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/AgentTranscriptResult","$schema":"http://json-schema.org/draft-07/schema#","title":"AgentTranscriptResult","required":["cursor","agent","page"],"additionalProperties":true};

function validate15(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/AgentTranscriptResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.cursor === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cursor"},message:"must have required property '"+"cursor"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.agent === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "agent"},message:"must have required property '"+"agent"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.page === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "page"},message:"must have required property '"+"page"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.cursor !== undefined){
let data0 = data.cursor;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(!(formats0.validate(data0))){
const err4 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
else {
const err5 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.agent !== undefined){
let data1 = data.agent;
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.id === undefined){
const err6 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.root_id === undefined){
const err7 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data1.parent_id === undefined){
const err8 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data1.name === undefined){
const err9 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data1.model === undefined){
const err10 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data1.provider === undefined){
const err11 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data1.effort === undefined){
const err12 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data1.cwd === undefined){
const err13 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data1.report === undefined){
const err14 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "report"},message:"must have required property '"+"report"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data1.status === undefined){
const err15 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data1.pending_mail === undefined){
const err16 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "pending_mail"},message:"must have required property '"+"pending_mail"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data1.lifecycle_phase === undefined){
const err17 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "lifecycle_phase"},message:"must have required property '"+"lifecycle_phase"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data1.blocking_reason === undefined){
const err18 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "blocking_reason"},message:"must have required property '"+"blocking_reason"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data1.terminal_cause === undefined){
const err19 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "terminal_cause"},message:"must have required property '"+"terminal_cause"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data1.allowed_controls === undefined){
const err20 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/required",keyword:"required",params:{missingProperty: "allowed_controls"},message:"must have required property '"+"allowed_controls"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data1.id !== undefined){
if(typeof data1.id !== "string"){
const err21 = {instancePath:instancePath+"/agent/id",schemaPath:"#/properties/agent/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data1.root_id !== undefined){
if(typeof data1.root_id !== "string"){
const err22 = {instancePath:instancePath+"/agent/root_id",schemaPath:"#/properties/agent/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data1.parent_id !== undefined){
if(typeof data1.parent_id !== "string"){
const err23 = {instancePath:instancePath+"/agent/parent_id",schemaPath:"#/properties/agent/properties/parent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data1.name !== undefined){
if(typeof data1.name !== "string"){
const err24 = {instancePath:instancePath+"/agent/name",schemaPath:"#/properties/agent/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data1.model !== undefined){
if(typeof data1.model !== "string"){
const err25 = {instancePath:instancePath+"/agent/model",schemaPath:"#/properties/agent/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data1.provider !== undefined){
if(typeof data1.provider !== "string"){
const err26 = {instancePath:instancePath+"/agent/provider",schemaPath:"#/properties/agent/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data1.effort !== undefined){
if(typeof data1.effort !== "string"){
const err27 = {instancePath:instancePath+"/agent/effort",schemaPath:"#/properties/agent/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data1.cwd !== undefined){
if(typeof data1.cwd !== "string"){
const err28 = {instancePath:instancePath+"/agent/cwd",schemaPath:"#/properties/agent/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data1.report !== undefined){
if(typeof data1.report !== "string"){
const err29 = {instancePath:instancePath+"/agent/report",schemaPath:"#/properties/agent/properties/report/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data1.status !== undefined){
if(typeof data1.status !== "string"){
const err30 = {instancePath:instancePath+"/agent/status",schemaPath:"#/properties/agent/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data1.pending_mail !== undefined){
let data12 = data1.pending_mail;
if(!((typeof data12 == "number") && (!(data12 % 1) && !isNaN(data12)))){
const err31 = {instancePath:instancePath+"/agent/pending_mail",schemaPath:"#/properties/agent/properties/pending_mail/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data1.lifecycle_phase !== undefined){
if(typeof data1.lifecycle_phase !== "string"){
const err32 = {instancePath:instancePath+"/agent/lifecycle_phase",schemaPath:"#/properties/agent/properties/lifecycle_phase/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data1.blocking_reason !== undefined){
if(typeof data1.blocking_reason !== "string"){
const err33 = {instancePath:instancePath+"/agent/blocking_reason",schemaPath:"#/properties/agent/properties/blocking_reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data1.terminal_cause !== undefined){
if(typeof data1.terminal_cause !== "string"){
const err34 = {instancePath:instancePath+"/agent/terminal_cause",schemaPath:"#/properties/agent/properties/terminal_cause/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
if(data1.allowed_controls !== undefined){
let data16 = data1.allowed_controls;
if((data16 !== null) && (!(Array.isArray(data16)))){
const err35 = {instancePath:instancePath+"/agent/allowed_controls",schemaPath:"#/properties/agent/properties/allowed_controls/type",keyword:"type",params:{type: schema16.properties.agent.properties.allowed_controls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(Array.isArray(data16)){
const len0 = data16.length;
for(let i0=0; i0<len0; i0++){
if(typeof data16[i0] !== "string"){
const err36 = {instancePath:instancePath+"/agent/allowed_controls/" + i0,schemaPath:"#/properties/agent/properties/allowed_controls/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
}
}
}
else {
const err37 = {instancePath:instancePath+"/agent",schemaPath:"#/properties/agent/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data.page !== undefined){
let data18 = data.page;
if(data18 && typeof data18 == "object" && !Array.isArray(data18)){
if(data18.history_revision === undefined){
const err38 = {instancePath:instancePath+"/page",schemaPath:"#/properties/page/required",keyword:"required",params:{missingProperty: "history_revision"},message:"must have required property '"+"history_revision"+"'"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(data18.through_seq === undefined){
const err39 = {instancePath:instancePath+"/page",schemaPath:"#/properties/page/required",keyword:"required",params:{missingProperty: "through_seq"},message:"must have required property '"+"through_seq"+"'"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
if(data18.next_seq === undefined){
const err40 = {instancePath:instancePath+"/page",schemaPath:"#/properties/page/required",keyword:"required",params:{missingProperty: "next_seq"},message:"must have required property '"+"next_seq"+"'"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data18.has_more === undefined){
const err41 = {instancePath:instancePath+"/page",schemaPath:"#/properties/page/required",keyword:"required",params:{missingProperty: "has_more"},message:"must have required property '"+"has_more"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
if(data18.messages === undefined){
const err42 = {instancePath:instancePath+"/page",schemaPath:"#/properties/page/required",keyword:"required",params:{missingProperty: "messages"},message:"must have required property '"+"messages"+"'"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data18.history_revision !== undefined){
let data19 = data18.history_revision;
if(typeof data19 === "string"){
if(!pattern0.test(data19)){
const err43 = {instancePath:instancePath+"/page/history_revision",schemaPath:"#/properties/page/properties/history_revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
if(!(formats0.validate(data19))){
const err44 = {instancePath:instancePath+"/page/history_revision",schemaPath:"#/properties/page/properties/history_revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
else {
const err45 = {instancePath:instancePath+"/page/history_revision",schemaPath:"#/properties/page/properties/history_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
if(data18.through_seq !== undefined){
let data20 = data18.through_seq;
if(!((typeof data20 == "number") && (!(data20 % 1) && !isNaN(data20)))){
const err46 = {instancePath:instancePath+"/page/through_seq",schemaPath:"#/properties/page/properties/through_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data18.next_seq !== undefined){
let data21 = data18.next_seq;
if(!((typeof data21 == "number") && (!(data21 % 1) && !isNaN(data21)))){
const err47 = {instancePath:instancePath+"/page/next_seq",schemaPath:"#/properties/page/properties/next_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data18.has_more !== undefined){
if(typeof data18.has_more !== "boolean"){
const err48 = {instancePath:instancePath+"/page/has_more",schemaPath:"#/properties/page/properties/has_more/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data18.messages !== undefined){
let data23 = data18.messages;
if((data23 !== null) && (!(Array.isArray(data23)))){
const err49 = {instancePath:instancePath+"/page/messages",schemaPath:"#/properties/page/properties/messages/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
if(Array.isArray(data23)){
const len1 = data23.length;
for(let i1=0; i1<len1; i1++){
let data24 = data23[i1];
if(data24 && typeof data24 == "object" && !Array.isArray(data24)){
if(data24.seq === undefined){
const err50 = {instancePath:instancePath+"/page/messages/" + i1,schemaPath:"#/properties/page/properties/messages/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
if(data24.role !== undefined){
if(typeof data24.role !== "string"){
const err51 = {instancePath:instancePath+"/page/messages/" + i1+"/role",schemaPath:"#/properties/page/properties/messages/items/properties/role/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
if(data24.authored !== undefined){
if(typeof data24.authored !== "boolean"){
const err52 = {instancePath:instancePath+"/page/messages/" + i1+"/authored",schemaPath:"#/properties/page/properties/messages/items/properties/authored/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data24.sent_at !== undefined){
let data27 = data24.sent_at;
if((data27 !== null) && (typeof data27 !== "string")){
const err53 = {instancePath:instancePath+"/page/messages/" + i1+"/sent_at",schemaPath:"#/properties/page/properties/messages/items/properties/sent_at/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.sent_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
if(data24.seq !== undefined){
let data28 = data24.seq;
if(!((typeof data28 == "number") && (!(data28 % 1) && !isNaN(data28)))){
const err54 = {instancePath:instancePath+"/page/messages/" + i1+"/seq",schemaPath:"#/properties/page/properties/messages/items/properties/seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data24.message !== undefined){
let data29 = data24.message;
if((data29 !== null) && (!(data29 && typeof data29 == "object" && !Array.isArray(data29)))){
const err55 = {instancePath:instancePath+"/page/messages/" + i1+"/message",schemaPath:"#/properties/page/properties/messages/items/properties/message/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.message.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(data29 && typeof data29 == "object" && !Array.isArray(data29)){
if(data29.role === undefined){
const err56 = {instancePath:instancePath+"/page/messages/" + i1+"/message",schemaPath:"#/properties/page/properties/messages/items/properties/message/required",keyword:"required",params:{missingProperty: "role"},message:"must have required property '"+"role"+"'"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
if(data29.content === undefined){
const err57 = {instancePath:instancePath+"/page/messages/" + i1+"/message",schemaPath:"#/properties/page/properties/messages/items/properties/message/required",keyword:"required",params:{missingProperty: "content"},message:"must have required property '"+"content"+"'"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
if(data29.role !== undefined){
if(typeof data29.role !== "string"){
const err58 = {instancePath:instancePath+"/page/messages/" + i1+"/message/role",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/role/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
if(data29.content !== undefined){
if(typeof data29.content !== "string"){
const err59 = {instancePath:instancePath+"/page/messages/" + i1+"/message/content",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/content/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data29.tool_calls !== undefined){
let data32 = data29.tool_calls;
if((data32 !== null) && (!(Array.isArray(data32)))){
const err60 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.message.properties.tool_calls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(Array.isArray(data32)){
const len2 = data32.length;
for(let i2=0; i2<len2; i2++){
let data33 = data32[i2];
if(data33 && typeof data33 == "object" && !Array.isArray(data33)){
if(data33.id === undefined){
const err61 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2,schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
if(data33.type === undefined){
const err62 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2,schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
if(data33.function === undefined){
const err63 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2,schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "function"},message:"must have required property '"+"function"+"'"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
if(data33.id !== undefined){
if(typeof data33.id !== "string"){
const err64 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/id",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data33.type !== undefined){
if(typeof data33.type !== "string"){
const err65 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/type",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
}
if(data33.function !== undefined){
let data36 = data33.function;
if(data36 && typeof data36 == "object" && !Array.isArray(data36)){
if(data36.name === undefined){
const err66 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/function",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
if(data36.arguments === undefined){
const err67 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/function",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/required",keyword:"required",params:{missingProperty: "arguments"},message:"must have required property '"+"arguments"+"'"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
if(data36.name !== undefined){
if(typeof data36.name !== "string"){
const err68 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/function/name",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
}
if(data36.arguments !== undefined){
if(typeof data36.arguments !== "string"){
const err69 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/function/arguments",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/properties/arguments/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
}
}
else {
const err70 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/function",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
}
if(data33.duration_ms !== undefined){
let data39 = data33.duration_ms;
if(!((typeof data39 == "number") && (!(data39 % 1) && !isNaN(data39)))){
const err71 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/duration_ms",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/duration_ms/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
}
if(data33.exit_code !== undefined){
let data40 = data33.exit_code;
if(!((typeof data40 == "number") && (!(data40 % 1) && !isNaN(data40)))){
const err72 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2+"/exit_code",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/properties/exit_code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
}
}
else {
const err73 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_calls/" + i2,schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_calls/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
}
}
}
if(data29.tool_call_id !== undefined){
if(typeof data29.tool_call_id !== "string"){
const err74 = {instancePath:instancePath+"/page/messages/" + i1+"/message/tool_call_id",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/tool_call_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
}
if(data29.name !== undefined){
if(typeof data29.name !== "string"){
const err75 = {instancePath:instancePath+"/page/messages/" + i1+"/message/name",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
if(data29.authored !== undefined){
if(typeof data29.authored !== "boolean"){
const err76 = {instancePath:instancePath+"/page/messages/" + i1+"/message/authored",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/authored/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
}
if(data29.sent_at !== undefined){
let data44 = data29.sent_at;
if((data44 !== null) && (typeof data44 !== "string")){
const err77 = {instancePath:instancePath+"/page/messages/" + i1+"/message/sent_at",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/sent_at/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.message.properties.sent_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
}
if(data29.usage !== undefined){
let data45 = data29.usage;
if((data45 !== null) && (!(data45 && typeof data45 == "object" && !Array.isArray(data45)))){
const err78 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.message.properties.usage.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
if(data45 && typeof data45 == "object" && !Array.isArray(data45)){
if(data45.prompt_tokens === undefined){
const err79 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/required",keyword:"required",params:{missingProperty: "prompt_tokens"},message:"must have required property '"+"prompt_tokens"+"'"};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
if(data45.completion_tokens === undefined){
const err80 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/required",keyword:"required",params:{missingProperty: "completion_tokens"},message:"must have required property '"+"completion_tokens"+"'"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
if(data45.prompt_tokens !== undefined){
let data46 = data45.prompt_tokens;
if(!((typeof data46 == "number") && (!(data46 % 1) && !isNaN(data46)))){
const err81 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage/prompt_tokens",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
}
if(data45.completion_tokens !== undefined){
let data47 = data45.completion_tokens;
if(!((typeof data47 == "number") && (!(data47 % 1) && !isNaN(data47)))){
const err82 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage/completion_tokens",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/properties/completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
if(data45.prompt_tokens_details !== undefined){
let data48 = data45.prompt_tokens_details;
if((data48 !== null) && (!(data48 && typeof data48 == "object" && !Array.isArray(data48)))){
const err83 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage/prompt_tokens_details",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens_details/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.message.properties.usage.properties.prompt_tokens_details.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
if(data48 && typeof data48 == "object" && !Array.isArray(data48)){
if(data48.cached_tokens === undefined){
const err84 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage/prompt_tokens_details",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens_details/required",keyword:"required",params:{missingProperty: "cached_tokens"},message:"must have required property '"+"cached_tokens"+"'"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
if(data48.cached_tokens !== undefined){
let data49 = data48.cached_tokens;
if(!((typeof data49 == "number") && (!(data49 % 1) && !isNaN(data49)))){
const err85 = {instancePath:instancePath+"/page/messages/" + i1+"/message/usage/prompt_tokens_details/cached_tokens",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens_details/properties/cached_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err85];
}
else {
vErrors.push(err85);
}
errors++;
}
}
}
}
}
}
if(data29.model !== undefined){
if(typeof data29.model !== "string"){
const err86 = {instancePath:instancePath+"/page/messages/" + i1+"/message/model",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
}
if(data29.rewound_from !== undefined){
if(typeof data29.rewound_from !== "string"){
const err87 = {instancePath:instancePath+"/page/messages/" + i1+"/message/rewound_from",schemaPath:"#/properties/page/properties/messages/items/properties/message/properties/rewound_from/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
}
}
}
if(data24.body !== undefined){
let data52 = data24.body;
if((data52 !== null) && (!(data52 && typeof data52 == "object" && !Array.isArray(data52)))){
const err88 = {instancePath:instancePath+"/page/messages/" + i1+"/body",schemaPath:"#/properties/page/properties/messages/items/properties/body/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.body.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
if(data52 && typeof data52 == "object" && !Array.isArray(data52)){
if(data52.reference_id === undefined){
const err89 = {instancePath:instancePath+"/page/messages/" + i1+"/body",schemaPath:"#/properties/page/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
if(data52.digest === undefined){
const err90 = {instancePath:instancePath+"/page/messages/" + i1+"/body",schemaPath:"#/properties/page/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
if(data52.size === undefined){
const err91 = {instancePath:instancePath+"/page/messages/" + i1+"/body",schemaPath:"#/properties/page/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err91];
}
else {
vErrors.push(err91);
}
errors++;
}
if(data52.media_type === undefined){
const err92 = {instancePath:instancePath+"/page/messages/" + i1+"/body",schemaPath:"#/properties/page/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err92];
}
else {
vErrors.push(err92);
}
errors++;
}
if(data52.source === undefined){
const err93 = {instancePath:instancePath+"/page/messages/" + i1+"/body",schemaPath:"#/properties/page/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err93];
}
else {
vErrors.push(err93);
}
errors++;
}
if(data52.text !== undefined){
let data53 = data52.text;
if((data53 !== null) && (typeof data53 !== "string")){
const err94 = {instancePath:instancePath+"/page/messages/" + i1+"/body/text",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/text/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.body.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err94];
}
else {
vErrors.push(err94);
}
errors++;
}
}
if(data52.binary !== undefined){
let data54 = data52.binary;
if((typeof data54 !== "string") && (data54 !== null)){
const err95 = {instancePath:instancePath+"/page/messages/" + i1+"/body/binary",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/binary/type",keyword:"type",params:{type: schema16.properties.page.properties.messages.items.properties.body.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err95];
}
else {
vErrors.push(err95);
}
errors++;
}
}
if(data52.reference_id !== undefined){
if(typeof data52.reference_id !== "string"){
const err96 = {instancePath:instancePath+"/page/messages/" + i1+"/body/reference_id",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err96];
}
else {
vErrors.push(err96);
}
errors++;
}
}
if(data52.digest !== undefined){
if(typeof data52.digest !== "string"){
const err97 = {instancePath:instancePath+"/page/messages/" + i1+"/body/digest",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err97];
}
else {
vErrors.push(err97);
}
errors++;
}
}
if(data52.size !== undefined){
let data57 = data52.size;
if(typeof data57 === "string"){
if(!pattern0.test(data57)){
const err98 = {instancePath:instancePath+"/page/messages/" + i1+"/body/size",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err98];
}
else {
vErrors.push(err98);
}
errors++;
}
if(!(formats0.validate(data57))){
const err99 = {instancePath:instancePath+"/page/messages/" + i1+"/body/size",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err99];
}
else {
vErrors.push(err99);
}
errors++;
}
}
else {
const err100 = {instancePath:instancePath+"/page/messages/" + i1+"/body/size",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err100];
}
else {
vErrors.push(err100);
}
errors++;
}
}
if(data52.media_type !== undefined){
if(typeof data52.media_type !== "string"){
const err101 = {instancePath:instancePath+"/page/messages/" + i1+"/body/media_type",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err101];
}
else {
vErrors.push(err101);
}
errors++;
}
}
if(data52.source !== undefined){
if(typeof data52.source !== "string"){
const err102 = {instancePath:instancePath+"/page/messages/" + i1+"/body/source",schemaPath:"#/properties/page/properties/messages/items/properties/body/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err102];
}
else {
vErrors.push(err102);
}
errors++;
}
}
}
}
}
else {
const err103 = {instancePath:instancePath+"/page/messages/" + i1,schemaPath:"#/properties/page/properties/messages/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err103];
}
else {
vErrors.push(err103);
}
errors++;
}
}
}
}
}
else {
const err104 = {instancePath:instancePath+"/page",schemaPath:"#/properties/page/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err104];
}
else {
vErrors.push(err104);
}
errors++;
}
}
if(data.presentation !== undefined){
let data60 = data.presentation;
if((data60 !== null) && (!(Array.isArray(data60)))){
const err105 = {instancePath:instancePath+"/presentation",schemaPath:"#/properties/presentation/type",keyword:"type",params:{type: schema16.properties.presentation.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err105];
}
else {
vErrors.push(err105);
}
errors++;
}
if(Array.isArray(data60)){
const len3 = data60.length;
for(let i3=0; i3<len3; i3++){
let data61 = data60[i3];
if(data61 && typeof data61 == "object" && !Array.isArray(data61)){
if(data61.seq === undefined){
const err106 = {instancePath:instancePath+"/presentation/" + i3,schemaPath:"#/properties/presentation/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err106];
}
else {
vErrors.push(err106);
}
errors++;
}
if(data61.kind === undefined){
const err107 = {instancePath:instancePath+"/presentation/" + i3,schemaPath:"#/properties/presentation/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err107];
}
else {
vErrors.push(err107);
}
errors++;
}
if(data61.payload === undefined){
const err108 = {instancePath:instancePath+"/presentation/" + i3,schemaPath:"#/properties/presentation/items/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err108];
}
else {
vErrors.push(err108);
}
errors++;
}
if(data61.seq !== undefined){
let data62 = data61.seq;
if(typeof data62 === "string"){
if(!pattern0.test(data62)){
const err109 = {instancePath:instancePath+"/presentation/" + i3+"/seq",schemaPath:"#/properties/presentation/items/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err109];
}
else {
vErrors.push(err109);
}
errors++;
}
if(!(formats0.validate(data62))){
const err110 = {instancePath:instancePath+"/presentation/" + i3+"/seq",schemaPath:"#/properties/presentation/items/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err110];
}
else {
vErrors.push(err110);
}
errors++;
}
}
else {
const err111 = {instancePath:instancePath+"/presentation/" + i3+"/seq",schemaPath:"#/properties/presentation/items/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err111];
}
else {
vErrors.push(err111);
}
errors++;
}
}
if(data61.kind !== undefined){
if(typeof data61.kind !== "string"){
const err112 = {instancePath:instancePath+"/presentation/" + i3+"/kind",schemaPath:"#/properties/presentation/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err112];
}
else {
vErrors.push(err112);
}
errors++;
}
}
}
else {
const err113 = {instancePath:instancePath+"/presentation/" + i3,schemaPath:"#/properties/presentation/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err113];
}
else {
vErrors.push(err113);
}
errors++;
}
}
}
}
if(data.inbox !== undefined){
let data64 = data.inbox;
if((data64 !== null) && (!(Array.isArray(data64)))){
const err114 = {instancePath:instancePath+"/inbox",schemaPath:"#/properties/inbox/type",keyword:"type",params:{type: schema16.properties.inbox.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err114];
}
else {
vErrors.push(err114);
}
errors++;
}
if(Array.isArray(data64)){
const len4 = data64.length;
for(let i4=0; i4<len4; i4++){
let data65 = data64[i4];
if(data65 && typeof data65 == "object" && !Array.isArray(data65)){
if(data65.root_id === undefined){
const err115 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err115];
}
else {
vErrors.push(err115);
}
errors++;
}
if(data65.agent_id === undefined){
const err116 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err116];
}
else {
vErrors.push(err116);
}
errors++;
}
if(data65.seq === undefined){
const err117 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err117];
}
else {
vErrors.push(err117);
}
errors++;
}
if(data65.kind === undefined){
const err118 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err118];
}
else {
vErrors.push(err118);
}
errors++;
}
if(data65.status === undefined){
const err119 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err119];
}
else {
vErrors.push(err119);
}
errors++;
}
if(data65.payload === undefined){
const err120 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err120];
}
else {
vErrors.push(err120);
}
errors++;
}
if(data65.root_id !== undefined){
if(typeof data65.root_id !== "string"){
const err121 = {instancePath:instancePath+"/inbox/" + i4+"/root_id",schemaPath:"#/properties/inbox/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err121];
}
else {
vErrors.push(err121);
}
errors++;
}
}
if(data65.agent_id !== undefined){
if(typeof data65.agent_id !== "string"){
const err122 = {instancePath:instancePath+"/inbox/" + i4+"/agent_id",schemaPath:"#/properties/inbox/items/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err122];
}
else {
vErrors.push(err122);
}
errors++;
}
}
if(data65.seq !== undefined){
let data68 = data65.seq;
if(typeof data68 === "string"){
if(!pattern0.test(data68)){
const err123 = {instancePath:instancePath+"/inbox/" + i4+"/seq",schemaPath:"#/properties/inbox/items/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err123];
}
else {
vErrors.push(err123);
}
errors++;
}
if(!(formats0.validate(data68))){
const err124 = {instancePath:instancePath+"/inbox/" + i4+"/seq",schemaPath:"#/properties/inbox/items/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err124];
}
else {
vErrors.push(err124);
}
errors++;
}
}
else {
const err125 = {instancePath:instancePath+"/inbox/" + i4+"/seq",schemaPath:"#/properties/inbox/items/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err125];
}
else {
vErrors.push(err125);
}
errors++;
}
}
if(data65.kind !== undefined){
if(typeof data65.kind !== "string"){
const err126 = {instancePath:instancePath+"/inbox/" + i4+"/kind",schemaPath:"#/properties/inbox/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err126];
}
else {
vErrors.push(err126);
}
errors++;
}
}
if(data65.status !== undefined){
if(typeof data65.status !== "string"){
const err127 = {instancePath:instancePath+"/inbox/" + i4+"/status",schemaPath:"#/properties/inbox/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err127];
}
else {
vErrors.push(err127);
}
errors++;
}
}
if(data65.payload !== undefined){
let data71 = data65.payload;
if(data71 && typeof data71 == "object" && !Array.isArray(data71)){
if(data71.reference_id === undefined){
const err128 = {instancePath:instancePath+"/inbox/" + i4+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err128];
}
else {
vErrors.push(err128);
}
errors++;
}
if(data71.digest === undefined){
const err129 = {instancePath:instancePath+"/inbox/" + i4+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err129];
}
else {
vErrors.push(err129);
}
errors++;
}
if(data71.size === undefined){
const err130 = {instancePath:instancePath+"/inbox/" + i4+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err130];
}
else {
vErrors.push(err130);
}
errors++;
}
if(data71.media_type === undefined){
const err131 = {instancePath:instancePath+"/inbox/" + i4+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err131];
}
else {
vErrors.push(err131);
}
errors++;
}
if(data71.source === undefined){
const err132 = {instancePath:instancePath+"/inbox/" + i4+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err132];
}
else {
vErrors.push(err132);
}
errors++;
}
if(data71.text !== undefined){
let data72 = data71.text;
if((data72 !== null) && (typeof data72 !== "string")){
const err133 = {instancePath:instancePath+"/inbox/" + i4+"/payload/text",schemaPath:"#/properties/inbox/items/properties/payload/properties/text/type",keyword:"type",params:{type: schema16.properties.inbox.items.properties.payload.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err133];
}
else {
vErrors.push(err133);
}
errors++;
}
}
if(data71.binary !== undefined){
let data73 = data71.binary;
if((typeof data73 !== "string") && (data73 !== null)){
const err134 = {instancePath:instancePath+"/inbox/" + i4+"/payload/binary",schemaPath:"#/properties/inbox/items/properties/payload/properties/binary/type",keyword:"type",params:{type: schema16.properties.inbox.items.properties.payload.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err134];
}
else {
vErrors.push(err134);
}
errors++;
}
}
if(data71.reference_id !== undefined){
if(typeof data71.reference_id !== "string"){
const err135 = {instancePath:instancePath+"/inbox/" + i4+"/payload/reference_id",schemaPath:"#/properties/inbox/items/properties/payload/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err135];
}
else {
vErrors.push(err135);
}
errors++;
}
}
if(data71.digest !== undefined){
if(typeof data71.digest !== "string"){
const err136 = {instancePath:instancePath+"/inbox/" + i4+"/payload/digest",schemaPath:"#/properties/inbox/items/properties/payload/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err136];
}
else {
vErrors.push(err136);
}
errors++;
}
}
if(data71.size !== undefined){
let data76 = data71.size;
if(typeof data76 === "string"){
if(!pattern0.test(data76)){
const err137 = {instancePath:instancePath+"/inbox/" + i4+"/payload/size",schemaPath:"#/properties/inbox/items/properties/payload/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err137];
}
else {
vErrors.push(err137);
}
errors++;
}
if(!(formats0.validate(data76))){
const err138 = {instancePath:instancePath+"/inbox/" + i4+"/payload/size",schemaPath:"#/properties/inbox/items/properties/payload/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err138];
}
else {
vErrors.push(err138);
}
errors++;
}
}
else {
const err139 = {instancePath:instancePath+"/inbox/" + i4+"/payload/size",schemaPath:"#/properties/inbox/items/properties/payload/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err139];
}
else {
vErrors.push(err139);
}
errors++;
}
}
if(data71.media_type !== undefined){
if(typeof data71.media_type !== "string"){
const err140 = {instancePath:instancePath+"/inbox/" + i4+"/payload/media_type",schemaPath:"#/properties/inbox/items/properties/payload/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err140];
}
else {
vErrors.push(err140);
}
errors++;
}
}
if(data71.source !== undefined){
if(typeof data71.source !== "string"){
const err141 = {instancePath:instancePath+"/inbox/" + i4+"/payload/source",schemaPath:"#/properties/inbox/items/properties/payload/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err141];
}
else {
vErrors.push(err141);
}
errors++;
}
}
}
else {
const err142 = {instancePath:instancePath+"/inbox/" + i4+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err142];
}
else {
vErrors.push(err142);
}
errors++;
}
}
}
else {
const err143 = {instancePath:instancePath+"/inbox/" + i4,schemaPath:"#/properties/inbox/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err143];
}
else {
vErrors.push(err143);
}
errors++;
}
}
}
}
}
else {
const err144 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err144];
}
else {
vErrors.push(err144);
}
errors++;
}
validate15.errors = vErrors;
return errors === 0;
}

export const BoundedTranscriptPage = validate16;
const schema17 = {"type":"object","properties":{"history_revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"through_seq":{"type":"integer"},"next_seq":{"type":"integer"},"has_more":{"type":"boolean"},"messages":{"type":["null","array"],"items":{"type":"object","properties":{"role":{"type":"string"},"authored":{"type":"boolean"},"sent_at":{"type":["null","string"]},"seq":{"type":"integer"},"message":{"type":["null","object"],"properties":{"role":{"type":"string"},"content":{"type":"string"},"tool_calls":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"function":{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"string"}},"required":["name","arguments"],"additionalProperties":true},"duration_ms":{"type":"integer"},"exit_code":{"type":"integer"}},"required":["id","type","function"],"additionalProperties":true}},"tool_call_id":{"type":"string"},"name":{"type":"string"},"authored":{"type":"boolean"},"sent_at":{"type":["null","string"]},"usage":{"type":["null","object"],"properties":{"prompt_tokens":{"type":"integer"},"completion_tokens":{"type":"integer"},"prompt_tokens_details":{"type":["null","object"],"properties":{"cached_tokens":{"type":"integer"}},"required":["cached_tokens"],"additionalProperties":true}},"required":["prompt_tokens","completion_tokens"],"additionalProperties":true},"model":{"type":"string"},"rewound_from":{"type":"string"}},"required":["role","content"],"additionalProperties":true},"body":{"type":["null","object"],"properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["seq"],"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/BoundedTranscriptPage","$schema":"http://json-schema.org/draft-07/schema#","title":"BoundedTranscriptPage","required":["history_revision","through_seq","next_seq","has_more","messages"],"additionalProperties":true};

function validate16(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/BoundedTranscriptPage" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.history_revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "history_revision"},message:"must have required property '"+"history_revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.through_seq === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "through_seq"},message:"must have required property '"+"through_seq"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.next_seq === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "next_seq"},message:"must have required property '"+"next_seq"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.has_more === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "has_more"},message:"must have required property '"+"has_more"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.messages === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "messages"},message:"must have required property '"+"messages"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.history_revision !== undefined){
let data0 = data.history_revision;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err5 = {instancePath:instancePath+"/history_revision",schemaPath:"#/properties/history_revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(formats0.validate(data0))){
const err6 = {instancePath:instancePath+"/history_revision",schemaPath:"#/properties/history_revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
else {
const err7 = {instancePath:instancePath+"/history_revision",schemaPath:"#/properties/history_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.through_seq !== undefined){
let data1 = data.through_seq;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err8 = {instancePath:instancePath+"/through_seq",schemaPath:"#/properties/through_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.next_seq !== undefined){
let data2 = data.next_seq;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err9 = {instancePath:instancePath+"/next_seq",schemaPath:"#/properties/next_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.has_more !== undefined){
if(typeof data.has_more !== "boolean"){
const err10 = {instancePath:instancePath+"/has_more",schemaPath:"#/properties/has_more/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.messages !== undefined){
let data4 = data.messages;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err11 = {instancePath:instancePath+"/messages",schemaPath:"#/properties/messages/type",keyword:"type",params:{type: schema17.properties.messages.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(Array.isArray(data4)){
const len0 = data4.length;
for(let i0=0; i0<len0; i0++){
let data5 = data4[i0];
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.seq === undefined){
const err12 = {instancePath:instancePath+"/messages/" + i0,schemaPath:"#/properties/messages/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data5.role !== undefined){
if(typeof data5.role !== "string"){
const err13 = {instancePath:instancePath+"/messages/" + i0+"/role",schemaPath:"#/properties/messages/items/properties/role/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data5.authored !== undefined){
if(typeof data5.authored !== "boolean"){
const err14 = {instancePath:instancePath+"/messages/" + i0+"/authored",schemaPath:"#/properties/messages/items/properties/authored/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data5.sent_at !== undefined){
let data8 = data5.sent_at;
if((data8 !== null) && (typeof data8 !== "string")){
const err15 = {instancePath:instancePath+"/messages/" + i0+"/sent_at",schemaPath:"#/properties/messages/items/properties/sent_at/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.sent_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data5.seq !== undefined){
let data9 = data5.seq;
if(!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9)))){
const err16 = {instancePath:instancePath+"/messages/" + i0+"/seq",schemaPath:"#/properties/messages/items/properties/seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data5.message !== undefined){
let data10 = data5.message;
if((data10 !== null) && (!(data10 && typeof data10 == "object" && !Array.isArray(data10)))){
const err17 = {instancePath:instancePath+"/messages/" + i0+"/message",schemaPath:"#/properties/messages/items/properties/message/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.message.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data10 && typeof data10 == "object" && !Array.isArray(data10)){
if(data10.role === undefined){
const err18 = {instancePath:instancePath+"/messages/" + i0+"/message",schemaPath:"#/properties/messages/items/properties/message/required",keyword:"required",params:{missingProperty: "role"},message:"must have required property '"+"role"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data10.content === undefined){
const err19 = {instancePath:instancePath+"/messages/" + i0+"/message",schemaPath:"#/properties/messages/items/properties/message/required",keyword:"required",params:{missingProperty: "content"},message:"must have required property '"+"content"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data10.role !== undefined){
if(typeof data10.role !== "string"){
const err20 = {instancePath:instancePath+"/messages/" + i0+"/message/role",schemaPath:"#/properties/messages/items/properties/message/properties/role/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data10.content !== undefined){
if(typeof data10.content !== "string"){
const err21 = {instancePath:instancePath+"/messages/" + i0+"/message/content",schemaPath:"#/properties/messages/items/properties/message/properties/content/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data10.tool_calls !== undefined){
let data13 = data10.tool_calls;
if((data13 !== null) && (!(Array.isArray(data13)))){
const err22 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.message.properties.tool_calls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(Array.isArray(data13)){
const len1 = data13.length;
for(let i1=0; i1<len1; i1++){
let data14 = data13[i1];
if(data14 && typeof data14 == "object" && !Array.isArray(data14)){
if(data14.id === undefined){
const err23 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1,schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(data14.type === undefined){
const err24 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1,schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data14.function === undefined){
const err25 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1,schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "function"},message:"must have required property '"+"function"+"'"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data14.id !== undefined){
if(typeof data14.id !== "string"){
const err26 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/id",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data14.type !== undefined){
if(typeof data14.type !== "string"){
const err27 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/type",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data14.function !== undefined){
let data17 = data14.function;
if(data17 && typeof data17 == "object" && !Array.isArray(data17)){
if(data17.name === undefined){
const err28 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/function",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data17.arguments === undefined){
const err29 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/function",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/required",keyword:"required",params:{missingProperty: "arguments"},message:"must have required property '"+"arguments"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data17.name !== undefined){
if(typeof data17.name !== "string"){
const err30 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/function/name",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data17.arguments !== undefined){
if(typeof data17.arguments !== "string"){
const err31 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/function/arguments",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/properties/arguments/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
}
else {
const err32 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/function",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/function/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data14.duration_ms !== undefined){
let data20 = data14.duration_ms;
if(!((typeof data20 == "number") && (!(data20 % 1) && !isNaN(data20)))){
const err33 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/duration_ms",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/duration_ms/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data14.exit_code !== undefined){
let data21 = data14.exit_code;
if(!((typeof data21 == "number") && (!(data21 % 1) && !isNaN(data21)))){
const err34 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1+"/exit_code",schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/properties/exit_code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
}
else {
const err35 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_calls/" + i1,schemaPath:"#/properties/messages/items/properties/message/properties/tool_calls/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
}
}
if(data10.tool_call_id !== undefined){
if(typeof data10.tool_call_id !== "string"){
const err36 = {instancePath:instancePath+"/messages/" + i0+"/message/tool_call_id",schemaPath:"#/properties/messages/items/properties/message/properties/tool_call_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
if(data10.name !== undefined){
if(typeof data10.name !== "string"){
const err37 = {instancePath:instancePath+"/messages/" + i0+"/message/name",schemaPath:"#/properties/messages/items/properties/message/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data10.authored !== undefined){
if(typeof data10.authored !== "boolean"){
const err38 = {instancePath:instancePath+"/messages/" + i0+"/message/authored",schemaPath:"#/properties/messages/items/properties/message/properties/authored/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
if(data10.sent_at !== undefined){
let data25 = data10.sent_at;
if((data25 !== null) && (typeof data25 !== "string")){
const err39 = {instancePath:instancePath+"/messages/" + i0+"/message/sent_at",schemaPath:"#/properties/messages/items/properties/message/properties/sent_at/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.message.properties.sent_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
if(data10.usage !== undefined){
let data26 = data10.usage;
if((data26 !== null) && (!(data26 && typeof data26 == "object" && !Array.isArray(data26)))){
const err40 = {instancePath:instancePath+"/messages/" + i0+"/message/usage",schemaPath:"#/properties/messages/items/properties/message/properties/usage/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.message.properties.usage.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data26 && typeof data26 == "object" && !Array.isArray(data26)){
if(data26.prompt_tokens === undefined){
const err41 = {instancePath:instancePath+"/messages/" + i0+"/message/usage",schemaPath:"#/properties/messages/items/properties/message/properties/usage/required",keyword:"required",params:{missingProperty: "prompt_tokens"},message:"must have required property '"+"prompt_tokens"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
if(data26.completion_tokens === undefined){
const err42 = {instancePath:instancePath+"/messages/" + i0+"/message/usage",schemaPath:"#/properties/messages/items/properties/message/properties/usage/required",keyword:"required",params:{missingProperty: "completion_tokens"},message:"must have required property '"+"completion_tokens"+"'"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data26.prompt_tokens !== undefined){
let data27 = data26.prompt_tokens;
if(!((typeof data27 == "number") && (!(data27 % 1) && !isNaN(data27)))){
const err43 = {instancePath:instancePath+"/messages/" + i0+"/message/usage/prompt_tokens",schemaPath:"#/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
}
if(data26.completion_tokens !== undefined){
let data28 = data26.completion_tokens;
if(!((typeof data28 == "number") && (!(data28 % 1) && !isNaN(data28)))){
const err44 = {instancePath:instancePath+"/messages/" + i0+"/message/usage/completion_tokens",schemaPath:"#/properties/messages/items/properties/message/properties/usage/properties/completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data26.prompt_tokens_details !== undefined){
let data29 = data26.prompt_tokens_details;
if((data29 !== null) && (!(data29 && typeof data29 == "object" && !Array.isArray(data29)))){
const err45 = {instancePath:instancePath+"/messages/" + i0+"/message/usage/prompt_tokens_details",schemaPath:"#/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens_details/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.message.properties.usage.properties.prompt_tokens_details.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(data29 && typeof data29 == "object" && !Array.isArray(data29)){
if(data29.cached_tokens === undefined){
const err46 = {instancePath:instancePath+"/messages/" + i0+"/message/usage/prompt_tokens_details",schemaPath:"#/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens_details/required",keyword:"required",params:{missingProperty: "cached_tokens"},message:"must have required property '"+"cached_tokens"+"'"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
if(data29.cached_tokens !== undefined){
let data30 = data29.cached_tokens;
if(!((typeof data30 == "number") && (!(data30 % 1) && !isNaN(data30)))){
const err47 = {instancePath:instancePath+"/messages/" + i0+"/message/usage/prompt_tokens_details/cached_tokens",schemaPath:"#/properties/messages/items/properties/message/properties/usage/properties/prompt_tokens_details/properties/cached_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
}
}
}
}
if(data10.model !== undefined){
if(typeof data10.model !== "string"){
const err48 = {instancePath:instancePath+"/messages/" + i0+"/message/model",schemaPath:"#/properties/messages/items/properties/message/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data10.rewound_from !== undefined){
if(typeof data10.rewound_from !== "string"){
const err49 = {instancePath:instancePath+"/messages/" + i0+"/message/rewound_from",schemaPath:"#/properties/messages/items/properties/message/properties/rewound_from/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
}
}
if(data5.body !== undefined){
let data33 = data5.body;
if((data33 !== null) && (!(data33 && typeof data33 == "object" && !Array.isArray(data33)))){
const err50 = {instancePath:instancePath+"/messages/" + i0+"/body",schemaPath:"#/properties/messages/items/properties/body/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.body.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
if(data33 && typeof data33 == "object" && !Array.isArray(data33)){
if(data33.reference_id === undefined){
const err51 = {instancePath:instancePath+"/messages/" + i0+"/body",schemaPath:"#/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
if(data33.digest === undefined){
const err52 = {instancePath:instancePath+"/messages/" + i0+"/body",schemaPath:"#/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
if(data33.size === undefined){
const err53 = {instancePath:instancePath+"/messages/" + i0+"/body",schemaPath:"#/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
if(data33.media_type === undefined){
const err54 = {instancePath:instancePath+"/messages/" + i0+"/body",schemaPath:"#/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
if(data33.source === undefined){
const err55 = {instancePath:instancePath+"/messages/" + i0+"/body",schemaPath:"#/properties/messages/items/properties/body/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(data33.text !== undefined){
let data34 = data33.text;
if((data34 !== null) && (typeof data34 !== "string")){
const err56 = {instancePath:instancePath+"/messages/" + i0+"/body/text",schemaPath:"#/properties/messages/items/properties/body/properties/text/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.body.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
if(data33.binary !== undefined){
let data35 = data33.binary;
if((typeof data35 !== "string") && (data35 !== null)){
const err57 = {instancePath:instancePath+"/messages/" + i0+"/body/binary",schemaPath:"#/properties/messages/items/properties/body/properties/binary/type",keyword:"type",params:{type: schema17.properties.messages.items.properties.body.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
}
if(data33.reference_id !== undefined){
if(typeof data33.reference_id !== "string"){
const err58 = {instancePath:instancePath+"/messages/" + i0+"/body/reference_id",schemaPath:"#/properties/messages/items/properties/body/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
if(data33.digest !== undefined){
if(typeof data33.digest !== "string"){
const err59 = {instancePath:instancePath+"/messages/" + i0+"/body/digest",schemaPath:"#/properties/messages/items/properties/body/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data33.size !== undefined){
let data38 = data33.size;
if(typeof data38 === "string"){
if(!pattern0.test(data38)){
const err60 = {instancePath:instancePath+"/messages/" + i0+"/body/size",schemaPath:"#/properties/messages/items/properties/body/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(!(formats0.validate(data38))){
const err61 = {instancePath:instancePath+"/messages/" + i0+"/body/size",schemaPath:"#/properties/messages/items/properties/body/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
}
else {
const err62 = {instancePath:instancePath+"/messages/" + i0+"/body/size",schemaPath:"#/properties/messages/items/properties/body/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
if(data33.media_type !== undefined){
if(typeof data33.media_type !== "string"){
const err63 = {instancePath:instancePath+"/messages/" + i0+"/body/media_type",schemaPath:"#/properties/messages/items/properties/body/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
}
if(data33.source !== undefined){
if(typeof data33.source !== "string"){
const err64 = {instancePath:instancePath+"/messages/" + i0+"/body/source",schemaPath:"#/properties/messages/items/properties/body/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
}
}
}
else {
const err65 = {instancePath:instancePath+"/messages/" + i0,schemaPath:"#/properties/messages/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
}
}
}
}
else {
const err66 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
validate16.errors = vErrors;
return errors === 0;
}

export const BrowserDriverParams = validate17;
const schema18 = {"type":"object","properties":{"driver":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/BrowserDriverParams","$schema":"http://json-schema.org/draft-07/schema#","title":"BrowserDriverParams","required":["driver"],"additionalProperties":true};

function validate17(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/BrowserDriverParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.driver === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "driver"},message:"must have required property '"+"driver"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.driver !== undefined){
if(typeof data.driver !== "string"){
const err1 = {instancePath:instancePath+"/driver",schemaPath:"#/properties/driver/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate17.errors = vErrors;
return errors === 0;
}

export const BrowserStatusResult = validate18;
const schema19 = {"type":"object","properties":{"enabled":{"type":"boolean"},"driver":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/BrowserStatusResult","$schema":"http://json-schema.org/draft-07/schema#","title":"BrowserStatusResult","required":["enabled"],"additionalProperties":true};

function validate18(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/BrowserStatusResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.enabled === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "enabled"},message:"must have required property '"+"enabled"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.enabled !== undefined){
if(typeof data.enabled !== "boolean"){
const err1 = {instancePath:instancePath+"/enabled",schemaPath:"#/properties/enabled/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.driver !== undefined){
if(typeof data.driver !== "string"){
const err2 = {instancePath:instancePath+"/driver",schemaPath:"#/properties/driver/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
}
else {
const err3 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
validate18.errors = vErrors;
return errors === 0;
}

export const BudgetCapParams = validate19;
const schema20 = {"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string"},"limit":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"$id":"https://whip.dev/protocol/v2/BudgetCapParams","$schema":"http://json-schema.org/draft-07/schema#","title":"BudgetCapParams","required":["id","kind","limit"],"additionalProperties":true};

function validate19(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/BudgetCapParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.kind === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.limit === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err3 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err4 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.limit !== undefined){
let data2 = data.limit;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err5 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(formats0.validate(data2))){
const err6 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
else {
const err7 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
validate19.errors = vErrors;
return errors === 0;
}

export const BudgetState = validate20;
const schema21 = {"type":"object","properties":{"kind":{"type":"string"},"limit":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"used":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"reserved":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"remaining":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"$id":"https://whip.dev/protocol/v2/BudgetState","$schema":"http://json-schema.org/draft-07/schema#","title":"BudgetState","required":["kind","limit","used","reserved","remaining"],"additionalProperties":true};

function validate20(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/BudgetState" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.kind === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.limit === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.used === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "used"},message:"must have required property '"+"used"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.reserved === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "reserved"},message:"must have required property '"+"reserved"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.remaining === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "remaining"},message:"must have required property '"+"remaining"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err5 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.limit !== undefined){
let data1 = data.limit;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err6 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!(formats0.validate(data1))){
const err7 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
else {
const err8 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.used !== undefined){
let data2 = data.used;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err9 = {instancePath:instancePath+"/used",schemaPath:"#/properties/used/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(!(formats0.validate(data2))){
const err10 = {instancePath:instancePath+"/used",schemaPath:"#/properties/used/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
else {
const err11 = {instancePath:instancePath+"/used",schemaPath:"#/properties/used/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.reserved !== undefined){
let data3 = data.reserved;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err12 = {instancePath:instancePath+"/reserved",schemaPath:"#/properties/reserved/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(!(formats0.validate(data3))){
const err13 = {instancePath:instancePath+"/reserved",schemaPath:"#/properties/reserved/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
else {
const err14 = {instancePath:instancePath+"/reserved",schemaPath:"#/properties/reserved/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.remaining !== undefined){
let data4 = data.remaining;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err15 = {instancePath:instancePath+"/remaining",schemaPath:"#/properties/remaining/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(!(formats0.validate(data4))){
const err16 = {instancePath:instancePath+"/remaining",schemaPath:"#/properties/remaining/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
else {
const err17 = {instancePath:instancePath+"/remaining",schemaPath:"#/properties/remaining/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
}
else {
const err18 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
validate20.errors = vErrors;
return errors === 0;
}

export const CancelParams = validate21;
const schema22 = {"type":"object","properties":{"turn_id":{"type":"string"},"target_command_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CancelParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CancelParams","additionalProperties":true};

function validate21(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CancelParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.turn_id !== undefined){
if(typeof data.turn_id !== "string"){
const err0 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
if(data.target_command_id !== undefined){
if(typeof data.target_command_id !== "string"){
const err1 = {instancePath:instancePath+"/target_command_id",schemaPath:"#/properties/target_command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate21.errors = vErrors;
return errors === 0;
}

export const CapabilityRecord = validate22;
const schema23 = {"type":"object","properties":{"id":{"type":"string"},"root_id":{"type":"string"},"agent_id":{"type":"string"},"issuer_agent_id":{"type":"string"},"operations":{"type":["null","array"],"items":{"type":"string"}},"scopes":{"type":["null","array"],"items":{"type":"string"}},"mcp":{"type":["null","array"],"items":{"type":"object","properties":{"server":{"type":"string"},"tool":{"type":"string"},"definition":{"type":"string"}},"required":["server","tool","definition"],"additionalProperties":true}},"mcp_all":{"type":"boolean"},"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"},"expires_at":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CapabilityRecord","$schema":"http://json-schema.org/draft-07/schema#","title":"CapabilityRecord","required":["id","root_id","agent_id","issuer_agent_id","operations","scopes","mcp","mcp_all","generation","status","expires_at","created_at","updated_at"],"additionalProperties":true};

function validate22(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CapabilityRecord" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.agent_id === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.issuer_agent_id === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "issuer_agent_id"},message:"must have required property '"+"issuer_agent_id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.operations === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.scopes === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "scopes"},message:"must have required property '"+"scopes"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.mcp === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "mcp"},message:"must have required property '"+"mcp"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data.mcp_all === undefined){
const err7 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "mcp_all"},message:"must have required property '"+"mcp_all"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data.generation === undefined){
const err8 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data.status === undefined){
const err9 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data.expires_at === undefined){
const err10 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expires_at"},message:"must have required property '"+"expires_at"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data.created_at === undefined){
const err11 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data.updated_at === undefined){
const err12 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "updated_at"},message:"must have required property '"+"updated_at"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err13 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err14 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err15 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.issuer_agent_id !== undefined){
if(typeof data.issuer_agent_id !== "string"){
const err16 = {instancePath:instancePath+"/issuer_agent_id",schemaPath:"#/properties/issuer_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.operations !== undefined){
let data4 = data.operations;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err17 = {instancePath:instancePath+"/operations",schemaPath:"#/properties/operations/type",keyword:"type",params:{type: schema23.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(Array.isArray(data4)){
const len0 = data4.length;
for(let i0=0; i0<len0; i0++){
if(typeof data4[i0] !== "string"){
const err18 = {instancePath:instancePath+"/operations/" + i0,schemaPath:"#/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
}
}
if(data.scopes !== undefined){
let data6 = data.scopes;
if((data6 !== null) && (!(Array.isArray(data6)))){
const err19 = {instancePath:instancePath+"/scopes",schemaPath:"#/properties/scopes/type",keyword:"type",params:{type: schema23.properties.scopes.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(Array.isArray(data6)){
const len1 = data6.length;
for(let i1=0; i1<len1; i1++){
if(typeof data6[i1] !== "string"){
const err20 = {instancePath:instancePath+"/scopes/" + i1,schemaPath:"#/properties/scopes/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
}
}
if(data.mcp !== undefined){
let data8 = data.mcp;
if((data8 !== null) && (!(Array.isArray(data8)))){
const err21 = {instancePath:instancePath+"/mcp",schemaPath:"#/properties/mcp/type",keyword:"type",params:{type: schema23.properties.mcp.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(Array.isArray(data8)){
const len2 = data8.length;
for(let i2=0; i2<len2; i2++){
let data9 = data8[i2];
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.server === undefined){
const err22 = {instancePath:instancePath+"/mcp/" + i2,schemaPath:"#/properties/mcp/items/required",keyword:"required",params:{missingProperty: "server"},message:"must have required property '"+"server"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(data9.tool === undefined){
const err23 = {instancePath:instancePath+"/mcp/" + i2,schemaPath:"#/properties/mcp/items/required",keyword:"required",params:{missingProperty: "tool"},message:"must have required property '"+"tool"+"'"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(data9.definition === undefined){
const err24 = {instancePath:instancePath+"/mcp/" + i2,schemaPath:"#/properties/mcp/items/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data9.server !== undefined){
if(typeof data9.server !== "string"){
const err25 = {instancePath:instancePath+"/mcp/" + i2+"/server",schemaPath:"#/properties/mcp/items/properties/server/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data9.tool !== undefined){
if(typeof data9.tool !== "string"){
const err26 = {instancePath:instancePath+"/mcp/" + i2+"/tool",schemaPath:"#/properties/mcp/items/properties/tool/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data9.definition !== undefined){
if(typeof data9.definition !== "string"){
const err27 = {instancePath:instancePath+"/mcp/" + i2+"/definition",schemaPath:"#/properties/mcp/items/properties/definition/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
}
else {
const err28 = {instancePath:instancePath+"/mcp/" + i2,schemaPath:"#/properties/mcp/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
}
}
if(data.mcp_all !== undefined){
if(typeof data.mcp_all !== "boolean"){
const err29 = {instancePath:instancePath+"/mcp_all",schemaPath:"#/properties/mcp_all/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data.generation !== undefined){
let data14 = data.generation;
if(typeof data14 === "string"){
if(!pattern0.test(data14)){
const err30 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(!(formats0.validate(data14))){
const err31 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
else {
const err32 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data.status !== undefined){
if(typeof data.status !== "string"){
const err33 = {instancePath:instancePath+"/status",schemaPath:"#/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data.expires_at !== undefined){
if(typeof data.expires_at !== "string"){
const err34 = {instancePath:instancePath+"/expires_at",schemaPath:"#/properties/expires_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
if(data.created_at !== undefined){
if(typeof data.created_at !== "string"){
const err35 = {instancePath:instancePath+"/created_at",schemaPath:"#/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data.updated_at !== undefined){
if(typeof data.updated_at !== "string"){
const err36 = {instancePath:instancePath+"/updated_at",schemaPath:"#/properties/updated_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
}
else {
const err37 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
validate22.errors = vErrors;
return errors === 0;
}

export const CatalogRevision = validate23;
const schema24 = {"type":"object","properties":{"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"$id":"https://whip.dev/protocol/v2/CatalogRevision","$schema":"http://json-schema.org/draft-07/schema#","title":"CatalogRevision","required":["revision"],"additionalProperties":true};

function validate23(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CatalogRevision" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.revision !== undefined){
let data0 = data.revision;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err1 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(!(formats0.validate(data0))){
const err2 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
else {
const err3 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate23.errors = vErrors;
return errors === 0;
}

export const CheckpointParams = validate24;
const schema25 = {"type":"object","properties":{"reason":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CheckpointParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CheckpointParams","additionalProperties":true};

function validate24(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CheckpointParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.reason !== undefined){
if(typeof data.reason !== "string"){
const err0 = {instancePath:instancePath+"/reason",schemaPath:"#/properties/reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
}
else {
const err1 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
validate24.errors = vErrors;
return errors === 0;
}

export const CommandParams = validate25;
const schema26 = {"type":"object","properties":{"command_id":{"type":"string"},"scope":{"type":"string"},"root_id":{"type":"string"},"operation":{"type":"string"},"payload":true},"$id":"https://whip.dev/protocol/v2/CommandParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CommandParams","required":["command_id","scope","operation"],"additionalProperties":true};

function validate25(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CommandParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.command_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command_id"},message:"must have required property '"+"command_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.scope === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "scope"},message:"must have required property '"+"scope"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.operation === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.command_id !== undefined){
if(typeof data.command_id !== "string"){
const err3 = {instancePath:instancePath+"/command_id",schemaPath:"#/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.scope !== undefined){
if(typeof data.scope !== "string"){
const err4 = {instancePath:instancePath+"/scope",schemaPath:"#/properties/scope/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err5 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.operation !== undefined){
if(typeof data.operation !== "string"){
const err6 = {instancePath:instancePath+"/operation",schemaPath:"#/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate25.errors = vErrors;
return errors === 0;
}

export const CommandResult = validate26;
const schema27 = {"type":"object","properties":{"content":{"type":["null","object"],"properties":{"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size"],"additionalProperties":true},"operation":{"type":"string"},"result":true,"failure":{"type":["null","object"],"properties":{"data":{"type":["null","object"],"properties":{"kind":{"type":"string"}},"required":["kind"],"additionalProperties":true},"code":{"type":"integer"},"message":{"type":"string"}},"required":["code","message"],"additionalProperties":true},"command_id":{"type":"string"},"ingress_seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CommandResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CommandResult","required":["operation","command_id","ingress_seq","status"],"additionalProperties":true};

function validate26(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CommandResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.operation === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.command_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command_id"},message:"must have required property '"+"command_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.ingress_seq === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "ingress_seq"},message:"must have required property '"+"ingress_seq"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.status === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.content !== undefined){
let data0 = data.content;
if((data0 !== null) && (!(data0 && typeof data0 == "object" && !Array.isArray(data0)))){
const err4 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/type",keyword:"type",params:{type: schema27.properties.content.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.reference_id === undefined){
const err5 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.digest === undefined){
const err6 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data0.size === undefined){
const err7 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data0.reference_id !== undefined){
if(typeof data0.reference_id !== "string"){
const err8 = {instancePath:instancePath+"/content/reference_id",schemaPath:"#/properties/content/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data0.digest !== undefined){
if(typeof data0.digest !== "string"){
const err9 = {instancePath:instancePath+"/content/digest",schemaPath:"#/properties/content/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.size !== undefined){
let data3 = data0.size;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err10 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(!(formats0.validate(data3))){
const err11 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
else {
const err12 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data0.media_type !== undefined){
if(typeof data0.media_type !== "string"){
const err13 = {instancePath:instancePath+"/content/media_type",schemaPath:"#/properties/content/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data0.source !== undefined){
if(typeof data0.source !== "string"){
const err14 = {instancePath:instancePath+"/content/source",schemaPath:"#/properties/content/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
}
if(data.operation !== undefined){
if(typeof data.operation !== "string"){
const err15 = {instancePath:instancePath+"/operation",schemaPath:"#/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.failure !== undefined){
let data7 = data.failure;
if((data7 !== null) && (!(data7 && typeof data7 == "object" && !Array.isArray(data7)))){
const err16 = {instancePath:instancePath+"/failure",schemaPath:"#/properties/failure/type",keyword:"type",params:{type: schema27.properties.failure.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.code === undefined){
const err17 = {instancePath:instancePath+"/failure",schemaPath:"#/properties/failure/required",keyword:"required",params:{missingProperty: "code"},message:"must have required property '"+"code"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data7.message === undefined){
const err18 = {instancePath:instancePath+"/failure",schemaPath:"#/properties/failure/required",keyword:"required",params:{missingProperty: "message"},message:"must have required property '"+"message"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data7.data !== undefined){
let data8 = data7.data;
if((data8 !== null) && (!(data8 && typeof data8 == "object" && !Array.isArray(data8)))){
const err19 = {instancePath:instancePath+"/failure/data",schemaPath:"#/properties/failure/properties/data/type",keyword:"type",params:{type: schema27.properties.failure.properties.data.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
if(data8.kind === undefined){
const err20 = {instancePath:instancePath+"/failure/data",schemaPath:"#/properties/failure/properties/data/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data8.kind !== undefined){
if(typeof data8.kind !== "string"){
const err21 = {instancePath:instancePath+"/failure/data/kind",schemaPath:"#/properties/failure/properties/data/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
}
}
if(data7.code !== undefined){
let data10 = data7.code;
if(!((typeof data10 == "number") && (!(data10 % 1) && !isNaN(data10)))){
const err22 = {instancePath:instancePath+"/failure/code",schemaPath:"#/properties/failure/properties/code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data7.message !== undefined){
if(typeof data7.message !== "string"){
const err23 = {instancePath:instancePath+"/failure/message",schemaPath:"#/properties/failure/properties/message/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
}
}
if(data.command_id !== undefined){
if(typeof data.command_id !== "string"){
const err24 = {instancePath:instancePath+"/command_id",schemaPath:"#/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data.ingress_seq !== undefined){
let data13 = data.ingress_seq;
if(typeof data13 === "string"){
if(!pattern0.test(data13)){
const err25 = {instancePath:instancePath+"/ingress_seq",schemaPath:"#/properties/ingress_seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(!(formats0.validate(data13))){
const err26 = {instancePath:instancePath+"/ingress_seq",schemaPath:"#/properties/ingress_seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
else {
const err27 = {instancePath:instancePath+"/ingress_seq",schemaPath:"#/properties/ingress_seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data.status !== undefined){
if(typeof data.status !== "string"){
const err28 = {instancePath:instancePath+"/status",schemaPath:"#/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
}
else {
const err29 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
validate26.errors = vErrors;
return errors === 0;
}

export const CommandStatusParams = validate27;
const schema28 = {"type":"object","properties":{"command_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CommandStatusParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CommandStatusParams","required":["command_id"],"additionalProperties":true};

function validate27(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CommandStatusParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.command_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command_id"},message:"must have required property '"+"command_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.command_id !== undefined){
if(typeof data.command_id !== "string"){
const err1 = {instancePath:instancePath+"/command_id",schemaPath:"#/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate27.errors = vErrors;
return errors === 0;
}

export const CompactionListResult = validate28;
const schema29 = {"type":["null","array"],"items":{"type":"object","properties":{"seq":{"type":"integer"},"cutoff":{"type":"integer"},"summary":{"type":"string"}},"required":["seq","cutoff","summary"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/CompactionListResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CompactionListResult"};

function validate28(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompactionListResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema29.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.seq === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.cutoff === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "cutoff"},message:"must have required property '"+"cutoff"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.summary === undefined){
const err3 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "summary"},message:"must have required property '"+"summary"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.seq !== undefined){
let data1 = data0.seq;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err4 = {instancePath:instancePath+"/" + i0+"/seq",schemaPath:"#/items/properties/seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data0.cutoff !== undefined){
let data2 = data0.cutoff;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err5 = {instancePath:instancePath+"/" + i0+"/cutoff",schemaPath:"#/items/properties/cutoff/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.summary !== undefined){
if(typeof data0.summary !== "string"){
const err6 = {instancePath:instancePath+"/" + i0+"/summary",schemaPath:"#/items/properties/summary/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
validate28.errors = vErrors;
return errors === 0;
}

export const CompactionParams = validate29;
const schema30 = {"type":"object","properties":{"model":{"type":"string"},"provider":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CompactionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CompactionParams","additionalProperties":true};

function validate29(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompactionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err0 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err1 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate29.errors = vErrors;
return errors === 0;
}

export const CompactionResult = validate30;
const schema31 = {"type":"object","properties":{"cutoff":{"type":"integer"},"model":{"type":"string"},"usage":{"type":"object","properties":{"prompt_tokens":{"type":"integer"},"completion_tokens":{"type":"integer"},"prompt_tokens_details":{"type":["null","object"],"properties":{"cached_tokens":{"type":"integer"}},"required":["cached_tokens"],"additionalProperties":true}},"required":["prompt_tokens","completion_tokens"],"additionalProperties":true}},"$id":"https://whip.dev/protocol/v2/CompactionResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CompactionResult","required":["cutoff","usage"],"additionalProperties":true};

function validate30(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompactionResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.cutoff === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cutoff"},message:"must have required property '"+"cutoff"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.usage === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "usage"},message:"must have required property '"+"usage"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.cutoff !== undefined){
let data0 = data.cutoff;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err2 = {instancePath:instancePath+"/cutoff",schemaPath:"#/properties/cutoff/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err3 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.usage !== undefined){
let data2 = data.usage;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.prompt_tokens === undefined){
const err4 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/required",keyword:"required",params:{missingProperty: "prompt_tokens"},message:"must have required property '"+"prompt_tokens"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data2.completion_tokens === undefined){
const err5 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/required",keyword:"required",params:{missingProperty: "completion_tokens"},message:"must have required property '"+"completion_tokens"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data2.prompt_tokens !== undefined){
let data3 = data2.prompt_tokens;
if(!((typeof data3 == "number") && (!(data3 % 1) && !isNaN(data3)))){
const err6 = {instancePath:instancePath+"/usage/prompt_tokens",schemaPath:"#/properties/usage/properties/prompt_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data2.completion_tokens !== undefined){
let data4 = data2.completion_tokens;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err7 = {instancePath:instancePath+"/usage/completion_tokens",schemaPath:"#/properties/usage/properties/completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data2.prompt_tokens_details !== undefined){
let data5 = data2.prompt_tokens_details;
if((data5 !== null) && (!(data5 && typeof data5 == "object" && !Array.isArray(data5)))){
const err8 = {instancePath:instancePath+"/usage/prompt_tokens_details",schemaPath:"#/properties/usage/properties/prompt_tokens_details/type",keyword:"type",params:{type: schema31.properties.usage.properties.prompt_tokens_details.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.cached_tokens === undefined){
const err9 = {instancePath:instancePath+"/usage/prompt_tokens_details",schemaPath:"#/properties/usage/properties/prompt_tokens_details/required",keyword:"required",params:{missingProperty: "cached_tokens"},message:"must have required property '"+"cached_tokens"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data5.cached_tokens !== undefined){
let data6 = data5.cached_tokens;
if(!((typeof data6 == "number") && (!(data6 % 1) && !isNaN(data6)))){
const err10 = {instancePath:instancePath+"/usage/prompt_tokens_details/cached_tokens",schemaPath:"#/properties/usage/properties/prompt_tokens_details/properties/cached_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
}
}
else {
const err11 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
else {
const err12 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
validate30.errors = vErrors;
return errors === 0;
}

export const CompactionRetryResult = validate31;
const schema32 = {"type":"object","properties":{"undone":{"type":"boolean"},"sequence":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/CompactionRetryResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CompactionRetryResult","required":["undone"],"additionalProperties":true};

function validate31(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompactionRetryResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.undone === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "undone"},message:"must have required property '"+"undone"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.undone !== undefined){
if(typeof data.undone !== "boolean"){
const err1 = {instancePath:instancePath+"/undone",schemaPath:"#/properties/undone/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.sequence !== undefined){
let data1 = data.sequence;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err2 = {instancePath:instancePath+"/sequence",schemaPath:"#/properties/sequence/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
}
else {
const err3 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
validate31.errors = vErrors;
return errors === 0;
}

export const CompactionSettingsResult = validate32;
const schema33 = {"type":"object","properties":{"model":{"type":"string"},"provider":{"type":"string"},"builtin_default":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/CompactionSettingsResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CompactionSettingsResult","required":["builtin_default"],"additionalProperties":true};

function validate32(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompactionSettingsResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.builtin_default === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "builtin_default"},message:"must have required property '"+"builtin_default"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err1 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err2 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.builtin_default !== undefined){
if(typeof data.builtin_default !== "boolean"){
const err3 = {instancePath:instancePath+"/builtin_default",schemaPath:"#/properties/builtin_default/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate32.errors = vErrors;
return errors === 0;
}

export const CompletionParams = validate33;
const schema34 = {"type":"object","properties":{"root_id":{"type":"string"},"agent_id":{"type":"string"},"kind":{"type":"string"},"prefix":{"type":"string"},"limit":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/CompletionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CompletionParams","required":["root_id","kind","prefix","limit"],"additionalProperties":true};

function validate33(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompletionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.kind === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.prefix === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "prefix"},message:"must have required property '"+"prefix"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.limit === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err4 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err5 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err6 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.prefix !== undefined){
if(typeof data.prefix !== "string"){
const err7 = {instancePath:instancePath+"/prefix",schemaPath:"#/properties/prefix/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.limit !== undefined){
let data4 = data.limit;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err8 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
else {
const err9 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
validate33.errors = vErrors;
return errors === 0;
}

export const CompletionResult = validate34;
const schema35 = {"type":"object","properties":{"warnings":{"type":["null","array"],"items":{"type":"string"}},"candidates":{"type":["null","array"],"items":{"type":"object","properties":{"text":{"type":"string"},"description":{"type":"string"}},"required":["text","description"],"additionalProperties":true}},"truncated":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/CompletionResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CompletionResult","required":["candidates","truncated"],"additionalProperties":true};

function validate34(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CompletionResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.candidates === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "candidates"},message:"must have required property '"+"candidates"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.truncated === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "truncated"},message:"must have required property '"+"truncated"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.warnings !== undefined){
let data0 = data.warnings;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/warnings",schemaPath:"#/properties/warnings/type",keyword:"type",params:{type: schema35.properties.warnings.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(Array.isArray(data0)){
const len0 = data0.length;
for(let i0=0; i0<len0; i0++){
if(typeof data0[i0] !== "string"){
const err3 = {instancePath:instancePath+"/warnings/" + i0,schemaPath:"#/properties/warnings/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
}
if(data.candidates !== undefined){
let data2 = data.candidates;
if((data2 !== null) && (!(Array.isArray(data2)))){
const err4 = {instancePath:instancePath+"/candidates",schemaPath:"#/properties/candidates/type",keyword:"type",params:{type: schema35.properties.candidates.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(Array.isArray(data2)){
const len1 = data2.length;
for(let i1=0; i1<len1; i1++){
let data3 = data2[i1];
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.text === undefined){
const err5 = {instancePath:instancePath+"/candidates/" + i1,schemaPath:"#/properties/candidates/items/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data3.description === undefined){
const err6 = {instancePath:instancePath+"/candidates/" + i1,schemaPath:"#/properties/candidates/items/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data3.text !== undefined){
if(typeof data3.text !== "string"){
const err7 = {instancePath:instancePath+"/candidates/" + i1+"/text",schemaPath:"#/properties/candidates/items/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data3.description !== undefined){
if(typeof data3.description !== "string"){
const err8 = {instancePath:instancePath+"/candidates/" + i1+"/description",schemaPath:"#/properties/candidates/items/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
else {
const err9 = {instancePath:instancePath+"/candidates/" + i1,schemaPath:"#/properties/candidates/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
}
if(data.truncated !== undefined){
if(typeof data.truncated !== "boolean"){
const err10 = {instancePath:instancePath+"/truncated",schemaPath:"#/properties/truncated/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
else {
const err11 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
validate34.errors = vErrors;
return errors === 0;
}

export const ComputerAppParams = validate35;
const schema36 = {"type":"object","properties":{"app":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ComputerAppParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ComputerAppParams","required":["app"],"additionalProperties":true};

function validate35(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ComputerAppParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.app === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "app"},message:"must have required property '"+"app"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.app !== undefined){
if(typeof data.app !== "string"){
const err1 = {instancePath:instancePath+"/app",schemaPath:"#/properties/app/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate35.errors = vErrors;
return errors === 0;
}

export const ComputerStatusResult = validate36;
const schema37 = {"type":"object","properties":{"enabled":{"type":"boolean"},"default_deny":{"type":"boolean"},"allowed":{"type":["null","array"],"items":{"type":"string"}},"denied":{"type":["null","array"],"items":{"type":"string"}},"session_allowed":{"type":["null","array"],"items":{"type":"string"}},"session_denied":{"type":["null","array"],"items":{"type":"string"}}},"$id":"https://whip.dev/protocol/v2/ComputerStatusResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ComputerStatusResult","required":["enabled","default_deny","allowed","denied","session_allowed","session_denied"],"additionalProperties":true};

function validate36(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ComputerStatusResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.enabled === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "enabled"},message:"must have required property '"+"enabled"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.default_deny === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "default_deny"},message:"must have required property '"+"default_deny"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.allowed === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "allowed"},message:"must have required property '"+"allowed"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.denied === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "denied"},message:"must have required property '"+"denied"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.session_allowed === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_allowed"},message:"must have required property '"+"session_allowed"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.session_denied === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_denied"},message:"must have required property '"+"session_denied"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.enabled !== undefined){
if(typeof data.enabled !== "boolean"){
const err6 = {instancePath:instancePath+"/enabled",schemaPath:"#/properties/enabled/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.default_deny !== undefined){
if(typeof data.default_deny !== "boolean"){
const err7 = {instancePath:instancePath+"/default_deny",schemaPath:"#/properties/default_deny/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.allowed !== undefined){
let data2 = data.allowed;
if((data2 !== null) && (!(Array.isArray(data2)))){
const err8 = {instancePath:instancePath+"/allowed",schemaPath:"#/properties/allowed/type",keyword:"type",params:{type: schema37.properties.allowed.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(Array.isArray(data2)){
const len0 = data2.length;
for(let i0=0; i0<len0; i0++){
if(typeof data2[i0] !== "string"){
const err9 = {instancePath:instancePath+"/allowed/" + i0,schemaPath:"#/properties/allowed/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
}
if(data.denied !== undefined){
let data4 = data.denied;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err10 = {instancePath:instancePath+"/denied",schemaPath:"#/properties/denied/type",keyword:"type",params:{type: schema37.properties.denied.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(Array.isArray(data4)){
const len1 = data4.length;
for(let i1=0; i1<len1; i1++){
if(typeof data4[i1] !== "string"){
const err11 = {instancePath:instancePath+"/denied/" + i1,schemaPath:"#/properties/denied/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
}
if(data.session_allowed !== undefined){
let data6 = data.session_allowed;
if((data6 !== null) && (!(Array.isArray(data6)))){
const err12 = {instancePath:instancePath+"/session_allowed",schemaPath:"#/properties/session_allowed/type",keyword:"type",params:{type: schema37.properties.session_allowed.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(Array.isArray(data6)){
const len2 = data6.length;
for(let i2=0; i2<len2; i2++){
if(typeof data6[i2] !== "string"){
const err13 = {instancePath:instancePath+"/session_allowed/" + i2,schemaPath:"#/properties/session_allowed/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
}
}
if(data.session_denied !== undefined){
let data8 = data.session_denied;
if((data8 !== null) && (!(Array.isArray(data8)))){
const err14 = {instancePath:instancePath+"/session_denied",schemaPath:"#/properties/session_denied/type",keyword:"type",params:{type: schema37.properties.session_denied.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(Array.isArray(data8)){
const len3 = data8.length;
for(let i3=0; i3<len3; i3++){
if(typeof data8[i3] !== "string"){
const err15 = {instancePath:instancePath+"/session_denied/" + i3,schemaPath:"#/properties/session_denied/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
}
}
}
else {
const err16 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
validate36.errors = vErrors;
return errors === 0;
}

export const ConfigurationUpdate = validate37;
const schema38 = {"type":"object","properties":{"import_claude":{"type":["null","boolean"]},"import_codex":{"type":["null","boolean"]},"revision":{"type":"string"},"default_model":{"type":["null","string"]},"default_provider":{"type":["null","string"]},"default_effort":{"type":["null","string"]},"compact_model":{"type":["null","string"]},"compact_provider":{"type":["null","string"]},"compact_percent":{"type":["null","integer"]},"goal_max_rounds":{"type":["null","integer"]},"max_retries":{"type":["null","integer"]}},"$id":"https://whip.dev/protocol/v2/ConfigurationUpdate","$schema":"http://json-schema.org/draft-07/schema#","title":"ConfigurationUpdate","required":["revision"],"additionalProperties":true};

function validate37(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ConfigurationUpdate" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.import_claude !== undefined){
let data0 = data.import_claude;
if((data0 !== null) && (typeof data0 !== "boolean")){
const err1 = {instancePath:instancePath+"/import_claude",schemaPath:"#/properties/import_claude/type",keyword:"type",params:{type: schema38.properties.import_claude.type},message:"must be null,boolean"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.import_codex !== undefined){
let data1 = data.import_codex;
if((data1 !== null) && (typeof data1 !== "boolean")){
const err2 = {instancePath:instancePath+"/import_codex",schemaPath:"#/properties/import_codex/type",keyword:"type",params:{type: schema38.properties.import_codex.type},message:"must be null,boolean"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.revision !== undefined){
if(typeof data.revision !== "string"){
const err3 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.default_model !== undefined){
let data3 = data.default_model;
if((data3 !== null) && (typeof data3 !== "string")){
const err4 = {instancePath:instancePath+"/default_model",schemaPath:"#/properties/default_model/type",keyword:"type",params:{type: schema38.properties.default_model.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.default_provider !== undefined){
let data4 = data.default_provider;
if((data4 !== null) && (typeof data4 !== "string")){
const err5 = {instancePath:instancePath+"/default_provider",schemaPath:"#/properties/default_provider/type",keyword:"type",params:{type: schema38.properties.default_provider.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.default_effort !== undefined){
let data5 = data.default_effort;
if((data5 !== null) && (typeof data5 !== "string")){
const err6 = {instancePath:instancePath+"/default_effort",schemaPath:"#/properties/default_effort/type",keyword:"type",params:{type: schema38.properties.default_effort.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.compact_model !== undefined){
let data6 = data.compact_model;
if((data6 !== null) && (typeof data6 !== "string")){
const err7 = {instancePath:instancePath+"/compact_model",schemaPath:"#/properties/compact_model/type",keyword:"type",params:{type: schema38.properties.compact_model.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.compact_provider !== undefined){
let data7 = data.compact_provider;
if((data7 !== null) && (typeof data7 !== "string")){
const err8 = {instancePath:instancePath+"/compact_provider",schemaPath:"#/properties/compact_provider/type",keyword:"type",params:{type: schema38.properties.compact_provider.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.compact_percent !== undefined){
let data8 = data.compact_percent;
if((data8 !== null) && (!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8))))){
const err9 = {instancePath:instancePath+"/compact_percent",schemaPath:"#/properties/compact_percent/type",keyword:"type",params:{type: schema38.properties.compact_percent.type},message:"must be null,integer"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.goal_max_rounds !== undefined){
let data9 = data.goal_max_rounds;
if((data9 !== null) && (!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9))))){
const err10 = {instancePath:instancePath+"/goal_max_rounds",schemaPath:"#/properties/goal_max_rounds/type",keyword:"type",params:{type: schema38.properties.goal_max_rounds.type},message:"must be null,integer"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.max_retries !== undefined){
let data10 = data.max_retries;
if((data10 !== null) && (!((typeof data10 == "number") && (!(data10 % 1) && !isNaN(data10))))){
const err11 = {instancePath:instancePath+"/max_retries",schemaPath:"#/properties/max_retries/type",keyword:"type",params:{type: schema38.properties.max_retries.type},message:"must be null,integer"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
else {
const err12 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
validate37.errors = vErrors;
return errors === 0;
}

export const ContentEventPayload = validate38;
const schema39 = {"type":"object","properties":{"content":{"type":"object","properties":{"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size"],"additionalProperties":true},"truncated":{"type":"boolean","const":true}},"$id":"https://whip.dev/protocol/v2/ContentEventPayload","$schema":"http://json-schema.org/draft-07/schema#","title":"ContentEventPayload","required":["content","truncated"],"additionalProperties":true};

function validate38(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ContentEventPayload" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.content === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "content"},message:"must have required property '"+"content"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.truncated === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "truncated"},message:"must have required property '"+"truncated"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.content !== undefined){
let data0 = data.content;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.reference_id === undefined){
const err2 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.digest === undefined){
const err3 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.size === undefined){
const err4 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.reference_id !== undefined){
if(typeof data0.reference_id !== "string"){
const err5 = {instancePath:instancePath+"/content/reference_id",schemaPath:"#/properties/content/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.digest !== undefined){
if(typeof data0.digest !== "string"){
const err6 = {instancePath:instancePath+"/content/digest",schemaPath:"#/properties/content/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data0.size !== undefined){
let data3 = data0.size;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err7 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(!(formats0.validate(data3))){
const err8 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
else {
const err9 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.media_type !== undefined){
if(typeof data0.media_type !== "string"){
const err10 = {instancePath:instancePath+"/content/media_type",schemaPath:"#/properties/content/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data0.source !== undefined){
if(typeof data0.source !== "string"){
const err11 = {instancePath:instancePath+"/content/source",schemaPath:"#/properties/content/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
else {
const err12 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data.truncated !== undefined){
let data6 = data.truncated;
if(typeof data6 !== "boolean"){
const err13 = {instancePath:instancePath+"/truncated",schemaPath:"#/properties/truncated/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(true !== data6){
const err14 = {instancePath:instancePath+"/truncated",schemaPath:"#/properties/truncated/const",keyword:"const",params:{allowedValue: true},message:"must be equal to constant"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
else {
const err15 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
validate38.errors = vErrors;
return errors === 0;
}

export const ContentHandle = validate39;
const schema40 = {"type":"object","properties":{"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ContentHandle","$schema":"http://json-schema.org/draft-07/schema#","title":"ContentHandle","required":["reference_id","digest","size"],"additionalProperties":true};

function validate39(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ContentHandle" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.reference_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.digest === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.size === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.reference_id !== undefined){
if(typeof data.reference_id !== "string"){
const err3 = {instancePath:instancePath+"/reference_id",schemaPath:"#/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.digest !== undefined){
if(typeof data.digest !== "string"){
const err4 = {instancePath:instancePath+"/digest",schemaPath:"#/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.size !== undefined){
let data2 = data.size;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err5 = {instancePath:instancePath+"/size",schemaPath:"#/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(formats0.validate(data2))){
const err6 = {instancePath:instancePath+"/size",schemaPath:"#/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
else {
const err7 = {instancePath:instancePath+"/size",schemaPath:"#/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.media_type !== undefined){
if(typeof data.media_type !== "string"){
const err8 = {instancePath:instancePath+"/media_type",schemaPath:"#/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.source !== undefined){
if(typeof data.source !== "string"){
const err9 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
else {
const err10 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
validate39.errors = vErrors;
return errors === 0;
}

export const ContentReadParams = validate40;
const schema41 = {"type":"object","properties":{"root_id":{"type":"string"},"agent_id":{"type":"string"},"reference_id":{"type":"string"},"offset":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"limit":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/ContentReadParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ContentReadParams","required":["root_id","reference_id","offset","limit"],"additionalProperties":true};

function validate40(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ContentReadParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.reference_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.offset === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "offset"},message:"must have required property '"+"offset"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.limit === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err4 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err5 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.reference_id !== undefined){
if(typeof data.reference_id !== "string"){
const err6 = {instancePath:instancePath+"/reference_id",schemaPath:"#/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.offset !== undefined){
let data3 = data.offset;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err7 = {instancePath:instancePath+"/offset",schemaPath:"#/properties/offset/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(!(formats0.validate(data3))){
const err8 = {instancePath:instancePath+"/offset",schemaPath:"#/properties/offset/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
else {
const err9 = {instancePath:instancePath+"/offset",schemaPath:"#/properties/offset/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.limit !== undefined){
let data4 = data.limit;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err10 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
else {
const err11 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
validate40.errors = vErrors;
return errors === 0;
}

export const ContentReadResult = validate41;
const schema42 = {"type":"object","properties":{"data":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"},"content":{"type":"object","properties":{"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size"],"additionalProperties":true}},"$id":"https://whip.dev/protocol/v2/ContentReadResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ContentReadResult","required":["data","content"],"additionalProperties":true};
const pattern21 = new RegExp("^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$", "u");

function validate41(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ContentReadResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.data === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "data"},message:"must have required property '"+"data"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.content === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "content"},message:"must have required property '"+"content"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.data !== undefined){
let data0 = data.data;
if((typeof data0 !== "string") && (data0 !== null)){
const err2 = {instancePath:instancePath+"/data",schemaPath:"#/properties/data/type",keyword:"type",params:{type: schema42.properties.data.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(typeof data0 === "string"){
if(!pattern21.test(data0)){
const err3 = {instancePath:instancePath+"/data",schemaPath:"#/properties/data/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
if(data.content !== undefined){
let data1 = data.content;
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.reference_id === undefined){
const err4 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.digest === undefined){
const err5 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.size === undefined){
const err6 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.reference_id !== undefined){
if(typeof data1.reference_id !== "string"){
const err7 = {instancePath:instancePath+"/content/reference_id",schemaPath:"#/properties/content/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data1.digest !== undefined){
if(typeof data1.digest !== "string"){
const err8 = {instancePath:instancePath+"/content/digest",schemaPath:"#/properties/content/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data1.size !== undefined){
let data4 = data1.size;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err9 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(!(formats0.validate(data4))){
const err10 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
else {
const err11 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data1.media_type !== undefined){
if(typeof data1.media_type !== "string"){
const err12 = {instancePath:instancePath+"/content/media_type",schemaPath:"#/properties/content/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data1.source !== undefined){
if(typeof data1.source !== "string"){
const err13 = {instancePath:instancePath+"/content/source",schemaPath:"#/properties/content/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
}
else {
const err14 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
else {
const err15 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
validate41.errors = vErrors;
return errors === 0;
}

export const ContextAuditResult = validate42;
const schema43 = {"type":"object","properties":{"working_directory":{"type":"string"},"rows":{"type":["null","array"],"items":{"type":"object","properties":{"label":{"type":"string"},"bytes":{"type":"integer"},"note":{"type":"string"}},"required":["label"],"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/ContextAuditResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ContextAuditResult","required":["working_directory","rows"],"additionalProperties":true};

function validate42(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ContextAuditResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.working_directory === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "working_directory"},message:"must have required property '"+"working_directory"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.rows === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "rows"},message:"must have required property '"+"rows"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.working_directory !== undefined){
if(typeof data.working_directory !== "string"){
const err2 = {instancePath:instancePath+"/working_directory",schemaPath:"#/properties/working_directory/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.rows !== undefined){
let data1 = data.rows;
if((data1 !== null) && (!(Array.isArray(data1)))){
const err3 = {instancePath:instancePath+"/rows",schemaPath:"#/properties/rows/type",keyword:"type",params:{type: schema43.properties.rows.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(Array.isArray(data1)){
const len0 = data1.length;
for(let i0=0; i0<len0; i0++){
let data2 = data1[i0];
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.label === undefined){
const err4 = {instancePath:instancePath+"/rows/" + i0,schemaPath:"#/properties/rows/items/required",keyword:"required",params:{missingProperty: "label"},message:"must have required property '"+"label"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data2.label !== undefined){
if(typeof data2.label !== "string"){
const err5 = {instancePath:instancePath+"/rows/" + i0+"/label",schemaPath:"#/properties/rows/items/properties/label/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data2.bytes !== undefined){
let data4 = data2.bytes;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err6 = {instancePath:instancePath+"/rows/" + i0+"/bytes",schemaPath:"#/properties/rows/items/properties/bytes/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data2.note !== undefined){
if(typeof data2.note !== "string"){
const err7 = {instancePath:instancePath+"/rows/" + i0+"/note",schemaPath:"#/properties/rows/items/properties/note/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath:instancePath+"/rows/" + i0,schemaPath:"#/properties/rows/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
}
}
else {
const err9 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
validate42.errors = vErrors;
return errors === 0;
}

export const CreateSessionParams = validate43;
const schema44 = {"type":"object","properties":{"kind":{"type":"string"},"cwd":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/CreateSessionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CreateSessionParams","required":["kind","cwd","model","provider"],"additionalProperties":true};

function validate43(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/CreateSessionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.kind === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.cwd === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.model === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.provider === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err4 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.cwd !== undefined){
if(typeof data.cwd !== "string"){
const err5 = {instancePath:instancePath+"/cwd",schemaPath:"#/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err6 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err7 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
validate43.errors = vErrors;
return errors === 0;
}

export const EffortParams = validate44;
const schema45 = {"type":"object","properties":{"effort":{"type":"string"},"persist_default":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/EffortParams","$schema":"http://json-schema.org/draft-07/schema#","title":"EffortParams","required":["effort","persist_default"],"additionalProperties":true};

function validate44(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/EffortParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.effort === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.persist_default === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "persist_default"},message:"must have required property '"+"persist_default"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.effort !== undefined){
if(typeof data.effort !== "string"){
const err2 = {instancePath:instancePath+"/effort",schemaPath:"#/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.persist_default !== undefined){
if(typeof data.persist_default !== "boolean"){
const err3 = {instancePath:instancePath+"/persist_default",schemaPath:"#/properties/persist_default/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate44.errors = vErrors;
return errors === 0;
}

export const EffortResult = validate45;
const schema46 = {"type":"object","properties":{"effort":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/EffortResult","$schema":"http://json-schema.org/draft-07/schema#","title":"EffortResult","required":["effort"],"additionalProperties":true};

function validate45(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/EffortResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.effort === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.effort !== undefined){
if(typeof data.effort !== "string"){
const err1 = {instancePath:instancePath+"/effort",schemaPath:"#/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate45.errors = vErrors;
return errors === 0;
}

export const Empty = validate46;
const schema47 = {"type":"object","$id":"https://whip.dev/protocol/v2/Empty","$schema":"http://json-schema.org/draft-07/schema#","title":"Empty","additionalProperties":true};

function validate46(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/Empty" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
}
else {
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
validate46.errors = vErrors;
return errors === 0;
}

export const EmptyParams = validate47;
const schema48 = {"type":"object","$id":"https://whip.dev/protocol/v2/EmptyParams","$schema":"http://json-schema.org/draft-07/schema#","title":"EmptyParams","additionalProperties":true};

function validate47(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/EmptyParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
}
else {
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
validate47.errors = vErrors;
return errors === 0;
}

export const EnrollIdentityParams = validate48;
const schema49 = {"type":"object","properties":{"public_key":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"},"tty_confirmed":{"type":"boolean"},"authorized_by":{"type":"string"},"signature":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/EnrollIdentityParams","$schema":"http://json-schema.org/draft-07/schema#","title":"EnrollIdentityParams","required":["public_key"],"additionalProperties":true};

function validate48(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/EnrollIdentityParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.public_key === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "public_key"},message:"must have required property '"+"public_key"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.public_key !== undefined){
let data0 = data.public_key;
if((typeof data0 !== "string") && (data0 !== null)){
const err1 = {instancePath:instancePath+"/public_key",schemaPath:"#/properties/public_key/type",keyword:"type",params:{type: schema49.properties.public_key.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(typeof data0 === "string"){
if(!pattern21.test(data0)){
const err2 = {instancePath:instancePath+"/public_key",schemaPath:"#/properties/public_key/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
}
if(data.tty_confirmed !== undefined){
if(typeof data.tty_confirmed !== "boolean"){
const err3 = {instancePath:instancePath+"/tty_confirmed",schemaPath:"#/properties/tty_confirmed/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.authorized_by !== undefined){
if(typeof data.authorized_by !== "string"){
const err4 = {instancePath:instancePath+"/authorized_by",schemaPath:"#/properties/authorized_by/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.signature !== undefined){
let data3 = data.signature;
if((typeof data3 !== "string") && (data3 !== null)){
const err5 = {instancePath:instancePath+"/signature",schemaPath:"#/properties/signature/type",keyword:"type",params:{type: schema49.properties.signature.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data3 === "string"){
if(!pattern21.test(data3)){
const err6 = {instancePath:instancePath+"/signature",schemaPath:"#/properties/signature/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate48.errors = vErrors;
return errors === 0;
}

export const EventNotification = validate49;
const schema50 = {"type":"object","properties":{"event":{"type":"object","properties":{"subscription_id":{"type":"string"},"root_id":{"type":"string"},"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"payload":true},"required":["root_id","seq","kind"],"additionalProperties":true}},"$id":"https://whip.dev/protocol/v2/EventNotification","$schema":"http://json-schema.org/draft-07/schema#","title":"EventNotification","required":["event"],"additionalProperties":true};

function validate49(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/EventNotification" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.event === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "event"},message:"must have required property '"+"event"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.event !== undefined){
let data0 = data.event;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.root_id === undefined){
const err1 = {instancePath:instancePath+"/event",schemaPath:"#/properties/event/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.seq === undefined){
const err2 = {instancePath:instancePath+"/event",schemaPath:"#/properties/event/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.kind === undefined){
const err3 = {instancePath:instancePath+"/event",schemaPath:"#/properties/event/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.subscription_id !== undefined){
if(typeof data0.subscription_id !== "string"){
const err4 = {instancePath:instancePath+"/event/subscription_id",schemaPath:"#/properties/event/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data0.root_id !== undefined){
if(typeof data0.root_id !== "string"){
const err5 = {instancePath:instancePath+"/event/root_id",schemaPath:"#/properties/event/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.seq !== undefined){
let data3 = data0.seq;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err6 = {instancePath:instancePath+"/event/seq",schemaPath:"#/properties/event/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!(formats0.validate(data3))){
const err7 = {instancePath:instancePath+"/event/seq",schemaPath:"#/properties/event/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
else {
const err8 = {instancePath:instancePath+"/event/seq",schemaPath:"#/properties/event/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data0.kind !== undefined){
if(typeof data0.kind !== "string"){
const err9 = {instancePath:instancePath+"/event/kind",schemaPath:"#/properties/event/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
else {
const err10 = {instancePath:instancePath+"/event",schemaPath:"#/properties/event/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
else {
const err11 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
validate49.errors = vErrors;
return errors === 0;
}

export const ForkParams = validate50;
const schema51 = {"type":"object","properties":{"expected_revision":{"type":["string","null"],"pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"title":{"type":"string"},"cut":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/ForkParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ForkParams","required":["expected_revision"],"additionalProperties":true};

function validate50(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ForkParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.expected_revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expected_revision"},message:"must have required property '"+"expected_revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.expected_revision !== undefined){
let data0 = data.expected_revision;
if((typeof data0 !== "string") && (data0 !== null)){
const err1 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/type",keyword:"type",params:{type: schema51.properties.expected_revision.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(!(formats0.validate(data0))){
const err3 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
if(data.title !== undefined){
if(typeof data.title !== "string"){
const err4 = {instancePath:instancePath+"/title",schemaPath:"#/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.cut !== undefined){
let data2 = data.cut;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err5 = {instancePath:instancePath+"/cut",schemaPath:"#/properties/cut/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate50.errors = vErrors;
return errors === 0;
}

export const GoalContextParams = validate51;
const schema52 = {"type":"object","properties":{"window":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/GoalContextParams","$schema":"http://json-schema.org/draft-07/schema#","title":"GoalContextParams","additionalProperties":true};

function validate51(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/GoalContextParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.window !== undefined){
let data0 = data.window;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err0 = {instancePath:instancePath+"/window",schemaPath:"#/properties/window/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
}
else {
const err1 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
validate51.errors = vErrors;
return errors === 0;
}

export const GoalResult = validate52;
const schema53 = {"type":"object","properties":{"goal":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/GoalResult","$schema":"http://json-schema.org/draft-07/schema#","title":"GoalResult","required":["goal"],"additionalProperties":true};

function validate52(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/GoalResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.goal === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "goal"},message:"must have required property '"+"goal"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.goal !== undefined){
if(typeof data.goal !== "string"){
const err1 = {instancePath:instancePath+"/goal",schemaPath:"#/properties/goal/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate52.errors = vErrors;
return errors === 0;
}

export const HistoryPageParams = validate53;
const schema54 = {"type":"object","properties":{"root_id":{"type":"string"},"agent_id":{"type":"string"},"after_seq":{"type":"integer"},"before_seq":{"type":"integer"},"through_seq":{"type":"integer"},"revision":{"type":["string","null"],"pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"limit":{"type":"integer"},"max_bytes":{"type":"integer"},"recent":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/HistoryPageParams","$schema":"http://json-schema.org/draft-07/schema#","title":"HistoryPageParams","required":["root_id","agent_id","through_seq","limit","max_bytes"],"additionalProperties":true};

function validate53(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/HistoryPageParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.agent_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.through_seq === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "through_seq"},message:"must have required property '"+"through_seq"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.limit === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.max_bytes === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "max_bytes"},message:"must have required property '"+"max_bytes"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err5 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err6 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.after_seq !== undefined){
let data2 = data.after_seq;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err7 = {instancePath:instancePath+"/after_seq",schemaPath:"#/properties/after_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.before_seq !== undefined){
let data3 = data.before_seq;
if(!((typeof data3 == "number") && (!(data3 % 1) && !isNaN(data3)))){
const err8 = {instancePath:instancePath+"/before_seq",schemaPath:"#/properties/before_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.through_seq !== undefined){
let data4 = data.through_seq;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err9 = {instancePath:instancePath+"/through_seq",schemaPath:"#/properties/through_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.revision !== undefined){
let data5 = data.revision;
if((typeof data5 !== "string") && (data5 !== null)){
const err10 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: schema54.properties.revision.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(typeof data5 === "string"){
if(!pattern0.test(data5)){
const err11 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(!(formats0.validate(data5))){
const err12 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
}
if(data.limit !== undefined){
let data6 = data.limit;
if(!((typeof data6 == "number") && (!(data6 % 1) && !isNaN(data6)))){
const err13 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.max_bytes !== undefined){
let data7 = data.max_bytes;
if(!((typeof data7 == "number") && (!(data7 % 1) && !isNaN(data7)))){
const err14 = {instancePath:instancePath+"/max_bytes",schemaPath:"#/properties/max_bytes/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.recent !== undefined){
if(typeof data.recent !== "boolean"){
const err15 = {instancePath:instancePath+"/recent",schemaPath:"#/properties/recent/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
}
else {
const err16 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
validate53.errors = vErrors;
return errors === 0;
}

export const IDParams = validate54;
const schema55 = {"type":"object","properties":{"id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/IDParams","$schema":"http://json-schema.org/draft-07/schema#","title":"IDParams","required":["id"],"additionalProperties":true};

function validate54(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/IDParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err1 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate54.errors = vErrors;
return errors === 0;
}

export const IdentityResult = validate55;
const schema56 = {"type":"object","properties":{"client_id":{"type":"string"},"kind":{"type":"string"},"nonce":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/IdentityResult","$schema":"http://json-schema.org/draft-07/schema#","title":"IdentityResult","required":["client_id","kind","nonce"],"additionalProperties":true};

function validate55(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/IdentityResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.client_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "client_id"},message:"must have required property '"+"client_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.kind === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.nonce === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "nonce"},message:"must have required property '"+"nonce"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.client_id !== undefined){
if(typeof data.client_id !== "string"){
const err3 = {instancePath:instancePath+"/client_id",schemaPath:"#/properties/client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err4 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.nonce !== undefined){
let data2 = data.nonce;
if((typeof data2 !== "string") && (data2 !== null)){
const err5 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/type",keyword:"type",params:{type: schema56.properties.nonce.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data2 === "string"){
if(!pattern21.test(data2)){
const err6 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate55.errors = vErrors;
return errors === 0;
}

export const IdentityStatusResult = validate56;
const schema57 = {"type":"object","properties":{"client_id":{"type":"string"},"kind":{"type":"string"},"paired":{"type":"boolean"},"enrollment_open":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/IdentityStatusResult","$schema":"http://json-schema.org/draft-07/schema#","title":"IdentityStatusResult","required":["client_id","kind","paired","enrollment_open"],"additionalProperties":true};

function validate56(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/IdentityStatusResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.client_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "client_id"},message:"must have required property '"+"client_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.kind === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.paired === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "paired"},message:"must have required property '"+"paired"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.enrollment_open === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "enrollment_open"},message:"must have required property '"+"enrollment_open"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.client_id !== undefined){
if(typeof data.client_id !== "string"){
const err4 = {instancePath:instancePath+"/client_id",schemaPath:"#/properties/client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.kind !== undefined){
if(typeof data.kind !== "string"){
const err5 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.paired !== undefined){
if(typeof data.paired !== "boolean"){
const err6 = {instancePath:instancePath+"/paired",schemaPath:"#/properties/paired/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.enrollment_open !== undefined){
if(typeof data.enrollment_open !== "boolean"){
const err7 = {instancePath:instancePath+"/enrollment_open",schemaPath:"#/properties/enrollment_open/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
validate56.errors = vErrors;
return errors === 0;
}

export const InitializeParams = validate57;
const schema58 = {"type":"object","properties":{"protocol_major":{"type":"integer"},"build_id":{"type":"string"},"client_kind":{"type":"string"},"client_id":{"type":"string"},"capabilities":{"type":["null","array"],"items":{"type":"string"}}},"$id":"https://whip.dev/protocol/v2/InitializeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"InitializeParams","required":["protocol_major","build_id","client_kind","client_id"],"additionalProperties":true};

function validate57(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/InitializeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.protocol_major === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "protocol_major"},message:"must have required property '"+"protocol_major"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.build_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "build_id"},message:"must have required property '"+"build_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.client_kind === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "client_kind"},message:"must have required property '"+"client_kind"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.client_id === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "client_id"},message:"must have required property '"+"client_id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.protocol_major !== undefined){
let data0 = data.protocol_major;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err4 = {instancePath:instancePath+"/protocol_major",schemaPath:"#/properties/protocol_major/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.build_id !== undefined){
if(typeof data.build_id !== "string"){
const err5 = {instancePath:instancePath+"/build_id",schemaPath:"#/properties/build_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.client_kind !== undefined){
if(typeof data.client_kind !== "string"){
const err6 = {instancePath:instancePath+"/client_kind",schemaPath:"#/properties/client_kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.client_id !== undefined){
if(typeof data.client_id !== "string"){
const err7 = {instancePath:instancePath+"/client_id",schemaPath:"#/properties/client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.capabilities !== undefined){
let data4 = data.capabilities;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err8 = {instancePath:instancePath+"/capabilities",schemaPath:"#/properties/capabilities/type",keyword:"type",params:{type: schema58.properties.capabilities.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(Array.isArray(data4)){
const len0 = data4.length;
for(let i0=0; i0<len0; i0++){
if(typeof data4[i0] !== "string"){
const err9 = {instancePath:instancePath+"/capabilities/" + i0,schemaPath:"#/properties/capabilities/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
}
}
else {
const err10 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
validate57.errors = vErrors;
return errors === 0;
}

export const InitializeResult = validate58;
const schema59 = {"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"object","properties":{"name":{"type":"string"},"surface":{"type":"string"},"execution":{"type":"string"},"permission":{"type":"string"},"sensitive":{"type":"boolean"}},"required":["name","surface","execution","permission"],"additionalProperties":true}},"limits":{"type":"object","properties":{"frame_bytes":{"type":"integer"},"connections":{"type":"integer"},"in_flight_requests":{"type":"integer"},"outbound_messages":{"type":"integer"},"outbound_bytes":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"root_subscriptions":{"type":"integer"},"content_chunk_bytes":{"type":"integer"},"upload_bytes":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["frame_bytes","connections","in_flight_requests","outbound_messages","outbound_bytes","root_subscriptions","content_chunk_bytes","upload_bytes"],"additionalProperties":true},"negotiated_capabilities":{"type":["null","array"],"items":{"type":"string"}},"protocol_minor":{"type":"integer"},"runtime_id":{"type":"string"},"connection_id":{"type":"string"},"host_platform":{"type":"string"},"host_architecture":{"type":"string"},"network_endpoint":{"type":"string"},"protocol_major":{"type":"integer"},"build_id":{"type":"string"},"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"pid":{"type":"integer"},"started_at":{"type":"string"},"capabilities":{"type":["null","array"],"items":{"type":"string"}},"nonce":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/InitializeResult","$schema":"http://json-schema.org/draft-07/schema#","title":"InitializeResult","required":["operations","limits","negotiated_capabilities","protocol_minor","runtime_id","connection_id","host_platform","host_architecture","protocol_major","build_id","generation","capabilities","nonce"],"additionalProperties":true};

function validate58(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/InitializeResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.operations === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.limits === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limits"},message:"must have required property '"+"limits"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.negotiated_capabilities === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "negotiated_capabilities"},message:"must have required property '"+"negotiated_capabilities"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.protocol_minor === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "protocol_minor"},message:"must have required property '"+"protocol_minor"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.runtime_id === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "runtime_id"},message:"must have required property '"+"runtime_id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.connection_id === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "connection_id"},message:"must have required property '"+"connection_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.host_platform === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "host_platform"},message:"must have required property '"+"host_platform"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data.host_architecture === undefined){
const err7 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "host_architecture"},message:"must have required property '"+"host_architecture"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data.protocol_major === undefined){
const err8 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "protocol_major"},message:"must have required property '"+"protocol_major"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data.build_id === undefined){
const err9 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "build_id"},message:"must have required property '"+"build_id"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data.generation === undefined){
const err10 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data.capabilities === undefined){
const err11 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "capabilities"},message:"must have required property '"+"capabilities"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data.nonce === undefined){
const err12 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "nonce"},message:"must have required property '"+"nonce"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data.operations !== undefined){
let data0 = data.operations;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err13 = {instancePath:instancePath+"/operations",schemaPath:"#/properties/operations/type",keyword:"type",params:{type: schema59.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(Array.isArray(data0)){
const len0 = data0.length;
for(let i0=0; i0<len0; i0++){
let data1 = data0[i0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.name === undefined){
const err14 = {instancePath:instancePath+"/operations/" + i0,schemaPath:"#/properties/operations/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data1.surface === undefined){
const err15 = {instancePath:instancePath+"/operations/" + i0,schemaPath:"#/properties/operations/items/required",keyword:"required",params:{missingProperty: "surface"},message:"must have required property '"+"surface"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data1.execution === undefined){
const err16 = {instancePath:instancePath+"/operations/" + i0,schemaPath:"#/properties/operations/items/required",keyword:"required",params:{missingProperty: "execution"},message:"must have required property '"+"execution"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data1.permission === undefined){
const err17 = {instancePath:instancePath+"/operations/" + i0,schemaPath:"#/properties/operations/items/required",keyword:"required",params:{missingProperty: "permission"},message:"must have required property '"+"permission"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data1.name !== undefined){
if(typeof data1.name !== "string"){
const err18 = {instancePath:instancePath+"/operations/" + i0+"/name",schemaPath:"#/properties/operations/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data1.surface !== undefined){
if(typeof data1.surface !== "string"){
const err19 = {instancePath:instancePath+"/operations/" + i0+"/surface",schemaPath:"#/properties/operations/items/properties/surface/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data1.execution !== undefined){
if(typeof data1.execution !== "string"){
const err20 = {instancePath:instancePath+"/operations/" + i0+"/execution",schemaPath:"#/properties/operations/items/properties/execution/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data1.permission !== undefined){
if(typeof data1.permission !== "string"){
const err21 = {instancePath:instancePath+"/operations/" + i0+"/permission",schemaPath:"#/properties/operations/items/properties/permission/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data1.sensitive !== undefined){
if(typeof data1.sensitive !== "boolean"){
const err22 = {instancePath:instancePath+"/operations/" + i0+"/sensitive",schemaPath:"#/properties/operations/items/properties/sensitive/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
}
else {
const err23 = {instancePath:instancePath+"/operations/" + i0,schemaPath:"#/properties/operations/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
}
}
if(data.limits !== undefined){
let data7 = data.limits;
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.frame_bytes === undefined){
const err24 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "frame_bytes"},message:"must have required property '"+"frame_bytes"+"'"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data7.connections === undefined){
const err25 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "connections"},message:"must have required property '"+"connections"+"'"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data7.in_flight_requests === undefined){
const err26 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "in_flight_requests"},message:"must have required property '"+"in_flight_requests"+"'"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(data7.outbound_messages === undefined){
const err27 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "outbound_messages"},message:"must have required property '"+"outbound_messages"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data7.outbound_bytes === undefined){
const err28 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "outbound_bytes"},message:"must have required property '"+"outbound_bytes"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data7.root_subscriptions === undefined){
const err29 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "root_subscriptions"},message:"must have required property '"+"root_subscriptions"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data7.content_chunk_bytes === undefined){
const err30 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "content_chunk_bytes"},message:"must have required property '"+"content_chunk_bytes"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data7.upload_bytes === undefined){
const err31 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/required",keyword:"required",params:{missingProperty: "upload_bytes"},message:"must have required property '"+"upload_bytes"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data7.frame_bytes !== undefined){
let data8 = data7.frame_bytes;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err32 = {instancePath:instancePath+"/limits/frame_bytes",schemaPath:"#/properties/limits/properties/frame_bytes/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data7.connections !== undefined){
let data9 = data7.connections;
if(!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9)))){
const err33 = {instancePath:instancePath+"/limits/connections",schemaPath:"#/properties/limits/properties/connections/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data7.in_flight_requests !== undefined){
let data10 = data7.in_flight_requests;
if(!((typeof data10 == "number") && (!(data10 % 1) && !isNaN(data10)))){
const err34 = {instancePath:instancePath+"/limits/in_flight_requests",schemaPath:"#/properties/limits/properties/in_flight_requests/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
if(data7.outbound_messages !== undefined){
let data11 = data7.outbound_messages;
if(!((typeof data11 == "number") && (!(data11 % 1) && !isNaN(data11)))){
const err35 = {instancePath:instancePath+"/limits/outbound_messages",schemaPath:"#/properties/limits/properties/outbound_messages/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data7.outbound_bytes !== undefined){
let data12 = data7.outbound_bytes;
if(typeof data12 === "string"){
if(!pattern0.test(data12)){
const err36 = {instancePath:instancePath+"/limits/outbound_bytes",schemaPath:"#/properties/limits/properties/outbound_bytes/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(!(formats0.validate(data12))){
const err37 = {instancePath:instancePath+"/limits/outbound_bytes",schemaPath:"#/properties/limits/properties/outbound_bytes/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
else {
const err38 = {instancePath:instancePath+"/limits/outbound_bytes",schemaPath:"#/properties/limits/properties/outbound_bytes/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
if(data7.root_subscriptions !== undefined){
let data13 = data7.root_subscriptions;
if(!((typeof data13 == "number") && (!(data13 % 1) && !isNaN(data13)))){
const err39 = {instancePath:instancePath+"/limits/root_subscriptions",schemaPath:"#/properties/limits/properties/root_subscriptions/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
if(data7.content_chunk_bytes !== undefined){
let data14 = data7.content_chunk_bytes;
if(!((typeof data14 == "number") && (!(data14 % 1) && !isNaN(data14)))){
const err40 = {instancePath:instancePath+"/limits/content_chunk_bytes",schemaPath:"#/properties/limits/properties/content_chunk_bytes/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data7.upload_bytes !== undefined){
let data15 = data7.upload_bytes;
if(typeof data15 === "string"){
if(!pattern0.test(data15)){
const err41 = {instancePath:instancePath+"/limits/upload_bytes",schemaPath:"#/properties/limits/properties/upload_bytes/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
if(!(formats0.validate(data15))){
const err42 = {instancePath:instancePath+"/limits/upload_bytes",schemaPath:"#/properties/limits/properties/upload_bytes/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
else {
const err43 = {instancePath:instancePath+"/limits/upload_bytes",schemaPath:"#/properties/limits/properties/upload_bytes/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
}
}
else {
const err44 = {instancePath:instancePath+"/limits",schemaPath:"#/properties/limits/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data.negotiated_capabilities !== undefined){
let data16 = data.negotiated_capabilities;
if((data16 !== null) && (!(Array.isArray(data16)))){
const err45 = {instancePath:instancePath+"/negotiated_capabilities",schemaPath:"#/properties/negotiated_capabilities/type",keyword:"type",params:{type: schema59.properties.negotiated_capabilities.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(Array.isArray(data16)){
const len1 = data16.length;
for(let i1=0; i1<len1; i1++){
if(typeof data16[i1] !== "string"){
const err46 = {instancePath:instancePath+"/negotiated_capabilities/" + i1,schemaPath:"#/properties/negotiated_capabilities/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
}
}
if(data.protocol_minor !== undefined){
let data18 = data.protocol_minor;
if(!((typeof data18 == "number") && (!(data18 % 1) && !isNaN(data18)))){
const err47 = {instancePath:instancePath+"/protocol_minor",schemaPath:"#/properties/protocol_minor/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data.runtime_id !== undefined){
if(typeof data.runtime_id !== "string"){
const err48 = {instancePath:instancePath+"/runtime_id",schemaPath:"#/properties/runtime_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data.connection_id !== undefined){
if(typeof data.connection_id !== "string"){
const err49 = {instancePath:instancePath+"/connection_id",schemaPath:"#/properties/connection_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
if(data.host_platform !== undefined){
if(typeof data.host_platform !== "string"){
const err50 = {instancePath:instancePath+"/host_platform",schemaPath:"#/properties/host_platform/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data.host_architecture !== undefined){
if(typeof data.host_architecture !== "string"){
const err51 = {instancePath:instancePath+"/host_architecture",schemaPath:"#/properties/host_architecture/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
if(data.network_endpoint !== undefined){
if(typeof data.network_endpoint !== "string"){
const err52 = {instancePath:instancePath+"/network_endpoint",schemaPath:"#/properties/network_endpoint/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data.protocol_major !== undefined){
let data24 = data.protocol_major;
if(!((typeof data24 == "number") && (!(data24 % 1) && !isNaN(data24)))){
const err53 = {instancePath:instancePath+"/protocol_major",schemaPath:"#/properties/protocol_major/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
if(data.build_id !== undefined){
if(typeof data.build_id !== "string"){
const err54 = {instancePath:instancePath+"/build_id",schemaPath:"#/properties/build_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data.generation !== undefined){
let data26 = data.generation;
if(typeof data26 === "string"){
if(!pattern0.test(data26)){
const err55 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(!(formats0.validate(data26))){
const err56 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
else {
const err57 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
}
if(data.pid !== undefined){
let data27 = data.pid;
if(!((typeof data27 == "number") && (!(data27 % 1) && !isNaN(data27)))){
const err58 = {instancePath:instancePath+"/pid",schemaPath:"#/properties/pid/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
if(data.started_at !== undefined){
if(typeof data.started_at !== "string"){
const err59 = {instancePath:instancePath+"/started_at",schemaPath:"#/properties/started_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data.capabilities !== undefined){
let data29 = data.capabilities;
if((data29 !== null) && (!(Array.isArray(data29)))){
const err60 = {instancePath:instancePath+"/capabilities",schemaPath:"#/properties/capabilities/type",keyword:"type",params:{type: schema59.properties.capabilities.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(Array.isArray(data29)){
const len2 = data29.length;
for(let i2=0; i2<len2; i2++){
if(typeof data29[i2] !== "string"){
const err61 = {instancePath:instancePath+"/capabilities/" + i2,schemaPath:"#/properties/capabilities/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
}
}
}
if(data.nonce !== undefined){
let data31 = data.nonce;
if((typeof data31 !== "string") && (data31 !== null)){
const err62 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/type",keyword:"type",params:{type: schema59.properties.nonce.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
if(typeof data31 === "string"){
if(!pattern21.test(data31)){
const err63 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
}
}
}
else {
const err64 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
validate58.errors = vErrors;
return errors === 0;
}

export const LSPListResult = validate59;
const schema60 = {"type":["null","array"],"items":{"type":"object","properties":{"name":{"type":"string"},"root":{"type":"string"},"state":{"type":"string"},"error":{"type":"string"}},"required":["name","state"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/LSPListResult","$schema":"http://json-schema.org/draft-07/schema#","title":"LSPListResult"};

function validate59(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/LSPListResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema60.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.name === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.state === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.name !== undefined){
if(typeof data0.name !== "string"){
const err3 = {instancePath:instancePath+"/" + i0+"/name",schemaPath:"#/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data0.root !== undefined){
if(typeof data0.root !== "string"){
const err4 = {instancePath:instancePath+"/" + i0+"/root",schemaPath:"#/items/properties/root/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data0.state !== undefined){
if(typeof data0.state !== "string"){
const err5 = {instancePath:instancePath+"/" + i0+"/state",schemaPath:"#/items/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.error !== undefined){
if(typeof data0.error !== "string"){
const err6 = {instancePath:instancePath+"/" + i0+"/error",schemaPath:"#/items/properties/error/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
validate59.errors = vErrors;
return errors === 0;
}

export const LifecycleEvent = validate60;
const schema61 = {"type":"object","properties":{"turn_id":{"type":"string"},"root_id":{"type":"string"},"agent_id":{"type":"string"},"sender_agent_id":{"type":"string"},"inbox_seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"inbox_kind":{"type":"string"},"delivery":{"type":"string"},"message_id":{"type":"string"},"phase":{"type":"string"},"status":{"type":"string"},"terminal_cause":{"type":"string"},"command_client_id":{"type":"string"},"command_id":{"type":"string"},"operation_id":{"type":"string"},"trace_id":{"type":"string"},"schedule_id":{"type":"integer"},"slot":{"type":"string"},"error":{"type":"string"},"acknowledged_inbox":{"type":"array","items":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"subscription_id":{"type":"string"},"key":{"type":"string"},"version":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"expected_version":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"restored":{"type":["null","array"],"items":{"type":"string"}},"not_restored":{"type":["null","array"],"items":{"type":"object","properties":{"name":{"type":"string"},"reason":{"type":"string"}},"required":["name","reason"],"additionalProperties":true}},"attempt":{"type":"string"},"budget_kind":{"type":"string"},"amount":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"limit":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"used":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"reserved":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"capability_id":{"type":"string"},"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"permission_id":{"type":"string"},"operation":{"type":"string"},"canonical_path":{"type":"string"},"request_digest":{"type":"string"},"command":{"type":"string"},"rule":{"type":"string"},"rule_source":{"type":"string"},"question_id":{"type":"string"},"question":{"type":"string"},"options":{"type":["null","array"],"items":{"type":"object","properties":{"label":{"type":"string"},"description":{"type":"string"}},"required":["label"],"additionalProperties":true}},"multiple":{"type":"boolean"},"answer":{"type":["null","array"],"items":{"type":"string"}},"dismissed":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/LifecycleEvent","$schema":"http://json-schema.org/draft-07/schema#","title":"LifecycleEvent","additionalProperties":true};

function validate60(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/LifecycleEvent" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.turn_id !== undefined){
if(typeof data.turn_id !== "string"){
const err0 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err1 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err2 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.sender_agent_id !== undefined){
if(typeof data.sender_agent_id !== "string"){
const err3 = {instancePath:instancePath+"/sender_agent_id",schemaPath:"#/properties/sender_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.inbox_seq !== undefined){
let data4 = data.inbox_seq;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err4 = {instancePath:instancePath+"/inbox_seq",schemaPath:"#/properties/inbox_seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(!(formats0.validate(data4))){
const err5 = {instancePath:instancePath+"/inbox_seq",schemaPath:"#/properties/inbox_seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
else {
const err6 = {instancePath:instancePath+"/inbox_seq",schemaPath:"#/properties/inbox_seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.inbox_kind !== undefined){
if(typeof data.inbox_kind !== "string"){
const err7 = {instancePath:instancePath+"/inbox_kind",schemaPath:"#/properties/inbox_kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.delivery !== undefined){
if(typeof data.delivery !== "string"){
const err8 = {instancePath:instancePath+"/delivery",schemaPath:"#/properties/delivery/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.message_id !== undefined){
if(typeof data.message_id !== "string"){
const err9 = {instancePath:instancePath+"/message_id",schemaPath:"#/properties/message_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.phase !== undefined){
if(typeof data.phase !== "string"){
const err10 = {instancePath:instancePath+"/phase",schemaPath:"#/properties/phase/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.status !== undefined){
if(typeof data.status !== "string"){
const err11 = {instancePath:instancePath+"/status",schemaPath:"#/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.terminal_cause !== undefined){
if(typeof data.terminal_cause !== "string"){
const err12 = {instancePath:instancePath+"/terminal_cause",schemaPath:"#/properties/terminal_cause/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data.command_client_id !== undefined){
if(typeof data.command_client_id !== "string"){
const err13 = {instancePath:instancePath+"/command_client_id",schemaPath:"#/properties/command_client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.command_id !== undefined){
if(typeof data.command_id !== "string"){
const err14 = {instancePath:instancePath+"/command_id",schemaPath:"#/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.operation_id !== undefined){
if(typeof data.operation_id !== "string"){
const err15 = {instancePath:instancePath+"/operation_id",schemaPath:"#/properties/operation_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.trace_id !== undefined){
if(typeof data.trace_id !== "string"){
const err16 = {instancePath:instancePath+"/trace_id",schemaPath:"#/properties/trace_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.schedule_id !== undefined){
let data15 = data.schedule_id;
if(!((typeof data15 == "number") && (!(data15 % 1) && !isNaN(data15)))){
const err17 = {instancePath:instancePath+"/schedule_id",schemaPath:"#/properties/schedule_id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data.slot !== undefined){
if(typeof data.slot !== "string"){
const err18 = {instancePath:instancePath+"/slot",schemaPath:"#/properties/slot/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data.error !== undefined){
if(typeof data.error !== "string"){
const err19 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data.acknowledged_inbox !== undefined){
let data18 = data.acknowledged_inbox;
if(Array.isArray(data18)){
const len0 = data18.length;
for(let i0=0; i0<len0; i0++){
let data19 = data18[i0];
if(typeof data19 === "string"){
if(!pattern0.test(data19)){
const err20 = {instancePath:instancePath+"/acknowledged_inbox/" + i0,schemaPath:"#/properties/acknowledged_inbox/items/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(!(formats0.validate(data19))){
const err21 = {instancePath:instancePath+"/acknowledged_inbox/" + i0,schemaPath:"#/properties/acknowledged_inbox/items/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
else {
const err22 = {instancePath:instancePath+"/acknowledged_inbox/" + i0,schemaPath:"#/properties/acknowledged_inbox/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
}
else {
const err23 = {instancePath:instancePath+"/acknowledged_inbox",schemaPath:"#/properties/acknowledged_inbox/type",keyword:"type",params:{type: "array"},message:"must be array"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data.subscription_id !== undefined){
if(typeof data.subscription_id !== "string"){
const err24 = {instancePath:instancePath+"/subscription_id",schemaPath:"#/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data.key !== undefined){
if(typeof data.key !== "string"){
const err25 = {instancePath:instancePath+"/key",schemaPath:"#/properties/key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data.version !== undefined){
let data22 = data.version;
if(typeof data22 === "string"){
if(!pattern0.test(data22)){
const err26 = {instancePath:instancePath+"/version",schemaPath:"#/properties/version/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(!(formats0.validate(data22))){
const err27 = {instancePath:instancePath+"/version",schemaPath:"#/properties/version/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
else {
const err28 = {instancePath:instancePath+"/version",schemaPath:"#/properties/version/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data.expected_version !== undefined){
let data23 = data.expected_version;
if(typeof data23 === "string"){
if(!pattern0.test(data23)){
const err29 = {instancePath:instancePath+"/expected_version",schemaPath:"#/properties/expected_version/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(!(formats0.validate(data23))){
const err30 = {instancePath:instancePath+"/expected_version",schemaPath:"#/properties/expected_version/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
else {
const err31 = {instancePath:instancePath+"/expected_version",schemaPath:"#/properties/expected_version/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data.restored !== undefined){
let data24 = data.restored;
if((data24 !== null) && (!(Array.isArray(data24)))){
const err32 = {instancePath:instancePath+"/restored",schemaPath:"#/properties/restored/type",keyword:"type",params:{type: schema61.properties.restored.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(Array.isArray(data24)){
const len1 = data24.length;
for(let i1=0; i1<len1; i1++){
if(typeof data24[i1] !== "string"){
const err33 = {instancePath:instancePath+"/restored/" + i1,schemaPath:"#/properties/restored/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
}
}
if(data.not_restored !== undefined){
let data26 = data.not_restored;
if((data26 !== null) && (!(Array.isArray(data26)))){
const err34 = {instancePath:instancePath+"/not_restored",schemaPath:"#/properties/not_restored/type",keyword:"type",params:{type: schema61.properties.not_restored.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(Array.isArray(data26)){
const len2 = data26.length;
for(let i2=0; i2<len2; i2++){
let data27 = data26[i2];
if(data27 && typeof data27 == "object" && !Array.isArray(data27)){
if(data27.name === undefined){
const err35 = {instancePath:instancePath+"/not_restored/" + i2,schemaPath:"#/properties/not_restored/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(data27.reason === undefined){
const err36 = {instancePath:instancePath+"/not_restored/" + i2,schemaPath:"#/properties/not_restored/items/required",keyword:"required",params:{missingProperty: "reason"},message:"must have required property '"+"reason"+"'"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(data27.name !== undefined){
if(typeof data27.name !== "string"){
const err37 = {instancePath:instancePath+"/not_restored/" + i2+"/name",schemaPath:"#/properties/not_restored/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data27.reason !== undefined){
if(typeof data27.reason !== "string"){
const err38 = {instancePath:instancePath+"/not_restored/" + i2+"/reason",schemaPath:"#/properties/not_restored/items/properties/reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
}
else {
const err39 = {instancePath:instancePath+"/not_restored/" + i2,schemaPath:"#/properties/not_restored/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
}
}
if(data.attempt !== undefined){
if(typeof data.attempt !== "string"){
const err40 = {instancePath:instancePath+"/attempt",schemaPath:"#/properties/attempt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data.budget_kind !== undefined){
if(typeof data.budget_kind !== "string"){
const err41 = {instancePath:instancePath+"/budget_kind",schemaPath:"#/properties/budget_kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
if(data.amount !== undefined){
let data32 = data.amount;
if(typeof data32 === "string"){
if(!pattern0.test(data32)){
const err42 = {instancePath:instancePath+"/amount",schemaPath:"#/properties/amount/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(!(formats0.validate(data32))){
const err43 = {instancePath:instancePath+"/amount",schemaPath:"#/properties/amount/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
}
else {
const err44 = {instancePath:instancePath+"/amount",schemaPath:"#/properties/amount/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data.limit !== undefined){
let data33 = data.limit;
if(typeof data33 === "string"){
if(!pattern0.test(data33)){
const err45 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(!(formats0.validate(data33))){
const err46 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
else {
const err47 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data.used !== undefined){
let data34 = data.used;
if(typeof data34 === "string"){
if(!pattern0.test(data34)){
const err48 = {instancePath:instancePath+"/used",schemaPath:"#/properties/used/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
if(!(formats0.validate(data34))){
const err49 = {instancePath:instancePath+"/used",schemaPath:"#/properties/used/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
else {
const err50 = {instancePath:instancePath+"/used",schemaPath:"#/properties/used/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data.reserved !== undefined){
let data35 = data.reserved;
if(typeof data35 === "string"){
if(!pattern0.test(data35)){
const err51 = {instancePath:instancePath+"/reserved",schemaPath:"#/properties/reserved/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
if(!(formats0.validate(data35))){
const err52 = {instancePath:instancePath+"/reserved",schemaPath:"#/properties/reserved/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
else {
const err53 = {instancePath:instancePath+"/reserved",schemaPath:"#/properties/reserved/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
if(data.capability_id !== undefined){
if(typeof data.capability_id !== "string"){
const err54 = {instancePath:instancePath+"/capability_id",schemaPath:"#/properties/capability_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data.generation !== undefined){
let data37 = data.generation;
if(typeof data37 === "string"){
if(!pattern0.test(data37)){
const err55 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(!(formats0.validate(data37))){
const err56 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
else {
const err57 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
}
if(data.permission_id !== undefined){
if(typeof data.permission_id !== "string"){
const err58 = {instancePath:instancePath+"/permission_id",schemaPath:"#/properties/permission_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
if(data.operation !== undefined){
if(typeof data.operation !== "string"){
const err59 = {instancePath:instancePath+"/operation",schemaPath:"#/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data.canonical_path !== undefined){
if(typeof data.canonical_path !== "string"){
const err60 = {instancePath:instancePath+"/canonical_path",schemaPath:"#/properties/canonical_path/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
if(data.request_digest !== undefined){
if(typeof data.request_digest !== "string"){
const err61 = {instancePath:instancePath+"/request_digest",schemaPath:"#/properties/request_digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
}
if(data.command !== undefined){
if(typeof data.command !== "string"){
const err62 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
if(data.rule !== undefined){
if(typeof data.rule !== "string"){
const err63 = {instancePath:instancePath+"/rule",schemaPath:"#/properties/rule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
}
if(data.rule_source !== undefined){
if(typeof data.rule_source !== "string"){
const err64 = {instancePath:instancePath+"/rule_source",schemaPath:"#/properties/rule_source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data.question_id !== undefined){
if(typeof data.question_id !== "string"){
const err65 = {instancePath:instancePath+"/question_id",schemaPath:"#/properties/question_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
}
if(data.question !== undefined){
if(typeof data.question !== "string"){
const err66 = {instancePath:instancePath+"/question",schemaPath:"#/properties/question/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
}
if(data.options !== undefined){
let data47 = data.options;
if((data47 !== null) && (!(Array.isArray(data47)))){
const err67 = {instancePath:instancePath+"/options",schemaPath:"#/properties/options/type",keyword:"type",params:{type: schema61.properties.options.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
if(Array.isArray(data47)){
const len3 = data47.length;
for(let i3=0; i3<len3; i3++){
let data48 = data47[i3];
if(data48 && typeof data48 == "object" && !Array.isArray(data48)){
if(data48.label === undefined){
const err68 = {instancePath:instancePath+"/options/" + i3,schemaPath:"#/properties/options/items/required",keyword:"required",params:{missingProperty: "label"},message:"must have required property '"+"label"+"'"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
if(data48.label !== undefined){
if(typeof data48.label !== "string"){
const err69 = {instancePath:instancePath+"/options/" + i3+"/label",schemaPath:"#/properties/options/items/properties/label/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
}
if(data48.description !== undefined){
if(typeof data48.description !== "string"){
const err70 = {instancePath:instancePath+"/options/" + i3+"/description",schemaPath:"#/properties/options/items/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
}
}
else {
const err71 = {instancePath:instancePath+"/options/" + i3,schemaPath:"#/properties/options/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
}
}
}
if(data.multiple !== undefined){
if(typeof data.multiple !== "boolean"){
const err72 = {instancePath:instancePath+"/multiple",schemaPath:"#/properties/multiple/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
}
if(data.answer !== undefined){
let data52 = data.answer;
if((data52 !== null) && (!(Array.isArray(data52)))){
const err73 = {instancePath:instancePath+"/answer",schemaPath:"#/properties/answer/type",keyword:"type",params:{type: schema61.properties.answer.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
if(Array.isArray(data52)){
const len4 = data52.length;
for(let i4=0; i4<len4; i4++){
if(typeof data52[i4] !== "string"){
const err74 = {instancePath:instancePath+"/answer/" + i4,schemaPath:"#/properties/answer/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
}
}
}
if(data.dismissed !== undefined){
if(typeof data.dismissed !== "boolean"){
const err75 = {instancePath:instancePath+"/dismissed",schemaPath:"#/properties/dismissed/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
}
else {
const err76 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
validate60.errors = vErrors;
return errors === 0;
}

export const ListParams = validate61;
const schema62 = {"type":"object","properties":{"limit":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/ListParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ListParams","additionalProperties":true};

function validate61(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ListParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.limit !== undefined){
let data0 = data.limit;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err0 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
}
else {
const err1 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
validate61.errors = vErrors;
return errors === 0;
}

export const MCPAttachParams = validate62;
const schema63 = {"type":"object","properties":{"servers":{"type":"object","additionalProperties":{"type":"object","properties":{"command":{"type":["null","array"],"items":{"type":"string"}},"env":{"type":"object","additionalProperties":{"type":"string"}},"cwd":{"type":"string"},"url":{"type":"string"},"headers":{"type":"object","additionalProperties":{"type":"string"}},"enabled":{"type":["null","boolean"]},"note":{"type":"string"},"startup_timeout":{"type":"integer"},"tool_timeout":{"type":"integer"},"source":{"type":"string"},"origin":{"type":"string"}},"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/MCPAttachParams","$schema":"http://json-schema.org/draft-07/schema#","title":"MCPAttachParams","required":["servers"],"additionalProperties":true};

function validate62(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/MCPAttachParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.servers === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "servers"},message:"must have required property '"+"servers"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.servers !== undefined){
let data0 = data.servers;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
for(const key0 in data0){
let data1 = data0[key0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.command !== undefined){
let data2 = data1.command;
if((data2 !== null) && (!(Array.isArray(data2)))){
const err1 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/command",schemaPath:"#/properties/servers/additionalProperties/properties/command/type",keyword:"type",params:{type: schema63.properties.servers.additionalProperties.properties.command.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(Array.isArray(data2)){
const len0 = data2.length;
for(let i0=0; i0<len0; i0++){
if(typeof data2[i0] !== "string"){
const err2 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/command/" + i0,schemaPath:"#/properties/servers/additionalProperties/properties/command/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
}
}
if(data1.env !== undefined){
let data4 = data1.env;
if(data4 && typeof data4 == "object" && !Array.isArray(data4)){
for(const key1 in data4){
if(typeof data4[key1] !== "string"){
const err3 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/env/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/servers/additionalProperties/properties/env/additionalProperties/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/env",schemaPath:"#/properties/servers/additionalProperties/properties/env/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data1.cwd !== undefined){
if(typeof data1.cwd !== "string"){
const err5 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/cwd",schemaPath:"#/properties/servers/additionalProperties/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data1.url !== undefined){
if(typeof data1.url !== "string"){
const err6 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/url",schemaPath:"#/properties/servers/additionalProperties/properties/url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data1.headers !== undefined){
let data8 = data1.headers;
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
for(const key2 in data8){
if(typeof data8[key2] !== "string"){
const err7 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/headers/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/servers/additionalProperties/properties/headers/additionalProperties/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/headers",schemaPath:"#/properties/servers/additionalProperties/properties/headers/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data1.enabled !== undefined){
let data10 = data1.enabled;
if((data10 !== null) && (typeof data10 !== "boolean")){
const err9 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/enabled",schemaPath:"#/properties/servers/additionalProperties/properties/enabled/type",keyword:"type",params:{type: schema63.properties.servers.additionalProperties.properties.enabled.type},message:"must be null,boolean"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data1.note !== undefined){
if(typeof data1.note !== "string"){
const err10 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/note",schemaPath:"#/properties/servers/additionalProperties/properties/note/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data1.startup_timeout !== undefined){
let data12 = data1.startup_timeout;
if(!((typeof data12 == "number") && (!(data12 % 1) && !isNaN(data12)))){
const err11 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/startup_timeout",schemaPath:"#/properties/servers/additionalProperties/properties/startup_timeout/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data1.tool_timeout !== undefined){
let data13 = data1.tool_timeout;
if(!((typeof data13 == "number") && (!(data13 % 1) && !isNaN(data13)))){
const err12 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/tool_timeout",schemaPath:"#/properties/servers/additionalProperties/properties/tool_timeout/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data1.source !== undefined){
if(typeof data1.source !== "string"){
const err13 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/source",schemaPath:"#/properties/servers/additionalProperties/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data1.origin !== undefined){
if(typeof data1.origin !== "string"){
const err14 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/origin",schemaPath:"#/properties/servers/additionalProperties/properties/origin/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
else {
const err15 = {instancePath:instancePath+"/servers/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/servers/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
}
else {
const err16 = {instancePath:instancePath+"/servers",schemaPath:"#/properties/servers/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
}
else {
const err17 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
validate62.errors = vErrors;
return errors === 0;
}

export const MCPImportParams = validate63;
const schema64 = {"type":"object","properties":{"source":{"type":"string"},"enabled":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/MCPImportParams","$schema":"http://json-schema.org/draft-07/schema#","title":"MCPImportParams","required":["source","enabled"],"additionalProperties":true};

function validate63(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/MCPImportParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.source === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.enabled === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "enabled"},message:"must have required property '"+"enabled"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.source !== undefined){
if(typeof data.source !== "string"){
const err2 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.enabled !== undefined){
if(typeof data.enabled !== "boolean"){
const err3 = {instancePath:instancePath+"/enabled",schemaPath:"#/properties/enabled/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate63.errors = vErrors;
return errors === 0;
}

export const MCPImportStatusResult = validate64;
const schema65 = {"type":"object","properties":{"claude":{"type":"boolean"},"codex":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/MCPImportStatusResult","$schema":"http://json-schema.org/draft-07/schema#","title":"MCPImportStatusResult","required":["claude","codex"],"additionalProperties":true};

function validate64(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/MCPImportStatusResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.claude === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "claude"},message:"must have required property '"+"claude"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.codex === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "codex"},message:"must have required property '"+"codex"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.claude !== undefined){
if(typeof data.claude !== "boolean"){
const err2 = {instancePath:instancePath+"/claude",schemaPath:"#/properties/claude/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.codex !== undefined){
if(typeof data.codex !== "boolean"){
const err3 = {instancePath:instancePath+"/codex",schemaPath:"#/properties/codex/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate64.errors = vErrors;
return errors === 0;
}

export const MCPListResult = validate65;
const schema66 = {"type":["null","array"],"items":{"type":"object","properties":{"name":{"type":"string"},"status":{"type":"string"},"note":{"type":"string"},"error":{"type":"string"},"tools":{"type":"integer"},"source":{"type":"string"}},"required":["name","status"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/MCPListResult","$schema":"http://json-schema.org/draft-07/schema#","title":"MCPListResult"};

function validate65(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/MCPListResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema66.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.name === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.status === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.name !== undefined){
if(typeof data0.name !== "string"){
const err3 = {instancePath:instancePath+"/" + i0+"/name",schemaPath:"#/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data0.status !== undefined){
if(typeof data0.status !== "string"){
const err4 = {instancePath:instancePath+"/" + i0+"/status",schemaPath:"#/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data0.note !== undefined){
if(typeof data0.note !== "string"){
const err5 = {instancePath:instancePath+"/" + i0+"/note",schemaPath:"#/items/properties/note/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.error !== undefined){
if(typeof data0.error !== "string"){
const err6 = {instancePath:instancePath+"/" + i0+"/error",schemaPath:"#/items/properties/error/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data0.tools !== undefined){
let data5 = data0.tools;
if(!((typeof data5 == "number") && (!(data5 % 1) && !isNaN(data5)))){
const err7 = {instancePath:instancePath+"/" + i0+"/tools",schemaPath:"#/items/properties/tools/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data0.source !== undefined){
if(typeof data0.source !== "string"){
const err8 = {instancePath:instancePath+"/" + i0+"/source",schemaPath:"#/items/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
else {
const err9 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
validate65.errors = vErrors;
return errors === 0;
}

export const MCPServerParams = validate66;
const schema67 = {"type":"object","properties":{"name":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/MCPServerParams","$schema":"http://json-schema.org/draft-07/schema#","title":"MCPServerParams","required":["name"],"additionalProperties":true};

function validate66(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/MCPServerParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.name === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.name !== undefined){
if(typeof data.name !== "string"){
const err1 = {instancePath:instancePath+"/name",schemaPath:"#/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate66.errors = vErrors;
return errors === 0;
}

export const ModelParams = validate67;
const schema68 = {"type":"object","properties":{"model":{"type":"string"},"provider":{"type":"string"},"persist_default":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/ModelParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ModelParams","required":["model","persist_default"],"additionalProperties":true};

function validate67(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ModelParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.model === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.persist_default === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "persist_default"},message:"must have required property '"+"persist_default"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err2 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err3 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.persist_default !== undefined){
if(typeof data.persist_default !== "boolean"){
const err4 = {instancePath:instancePath+"/persist_default",schemaPath:"#/properties/persist_default/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
}
else {
const err5 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
validate67.errors = vErrors;
return errors === 0;
}

export const ModelResult = validate68;
const schema69 = {"type":"object","properties":{"reload_pending":{"type":"boolean"},"model":{"type":"string"},"provider":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ModelResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ModelResult","required":["model","provider"],"additionalProperties":true};

function validate68(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ModelResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.model === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.provider === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.reload_pending !== undefined){
if(typeof data.reload_pending !== "boolean"){
const err2 = {instancePath:instancePath+"/reload_pending",schemaPath:"#/properties/reload_pending/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err3 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err4 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
}
else {
const err5 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
validate68.errors = vErrors;
return errors === 0;
}

export const PathParams = validate69;
const schema70 = {"type":"object","properties":{"path":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/PathParams","$schema":"http://json-schema.org/draft-07/schema#","title":"PathParams","required":["path"],"additionalProperties":true};

function validate69(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PathParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.path === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "path"},message:"must have required property '"+"path"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.path !== undefined){
if(typeof data.path !== "string"){
const err1 = {instancePath:instancePath+"/path",schemaPath:"#/properties/path/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate69.errors = vErrors;
return errors === 0;
}

export const PathResult = validate70;
const schema71 = {"type":"object","properties":{"path":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/PathResult","$schema":"http://json-schema.org/draft-07/schema#","title":"PathResult","required":["path"],"additionalProperties":true};

function validate70(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PathResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.path === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "path"},message:"must have required property '"+"path"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.path !== undefined){
if(typeof data.path !== "string"){
const err1 = {instancePath:instancePath+"/path",schemaPath:"#/properties/path/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate70.errors = vErrors;
return errors === 0;
}

export const PermissionConfigureParams = validate71;
const schema72 = {"type":"object","properties":{"external_permissions":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/PermissionConfigureParams","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionConfigureParams","required":["external_permissions"],"additionalProperties":true};

function validate71(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionConfigureParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.external_permissions === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "external_permissions"},message:"must have required property '"+"external_permissions"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.external_permissions !== undefined){
if(typeof data.external_permissions !== "boolean"){
const err1 = {instancePath:instancePath+"/external_permissions",schemaPath:"#/properties/external_permissions/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate71.errors = vErrors;
return errors === 0;
}

export const PermissionDecision = validate72;
const schema73 = {"type":"object","properties":{"command_id":{"type":"string"},"root_id":{"type":"string"},"permission_id":{"type":"string"},"allow":{"type":"boolean"},"reason":{"type":"string"},"remember":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/PermissionDecision","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionDecision","required":["command_id","root_id","permission_id","allow"],"additionalProperties":true};

function validate72(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionDecision" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.command_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command_id"},message:"must have required property '"+"command_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.permission_id === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "permission_id"},message:"must have required property '"+"permission_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.allow === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "allow"},message:"must have required property '"+"allow"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.command_id !== undefined){
if(typeof data.command_id !== "string"){
const err4 = {instancePath:instancePath+"/command_id",schemaPath:"#/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err5 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.permission_id !== undefined){
if(typeof data.permission_id !== "string"){
const err6 = {instancePath:instancePath+"/permission_id",schemaPath:"#/properties/permission_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.allow !== undefined){
if(typeof data.allow !== "boolean"){
const err7 = {instancePath:instancePath+"/allow",schemaPath:"#/properties/allow/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.reason !== undefined){
if(typeof data.reason !== "string"){
const err8 = {instancePath:instancePath+"/reason",schemaPath:"#/properties/reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.remember !== undefined){
if(typeof data.remember !== "string"){
const err9 = {instancePath:instancePath+"/remember",schemaPath:"#/properties/remember/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
else {
const err10 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
validate72.errors = vErrors;
return errors === 0;
}

export const PermissionDecisionParams = validate73;
const schema74 = {"type":"object","properties":{"decision":true,"signature":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/PermissionDecisionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionDecisionParams","required":["decision","signature"],"additionalProperties":true};

function validate73(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionDecisionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.decision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "decision"},message:"must have required property '"+"decision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.signature === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "signature"},message:"must have required property '"+"signature"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.signature !== undefined){
let data0 = data.signature;
if((typeof data0 !== "string") && (data0 !== null)){
const err2 = {instancePath:instancePath+"/signature",schemaPath:"#/properties/signature/type",keyword:"type",params:{type: schema74.properties.signature.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(typeof data0 === "string"){
if(!pattern21.test(data0)){
const err3 = {instancePath:instancePath+"/signature",schemaPath:"#/properties/signature/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate73.errors = vErrors;
return errors === 0;
}

export const PermissionDecisionResult = validate74;
const schema75 = {"type":"object","properties":{"operation_id":{"type":"string"},"lease_id":{"type":"string"},"nonce":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/PermissionDecisionResult","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionDecisionResult","required":["operation_id","lease_id","nonce"],"additionalProperties":true};

function validate74(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionDecisionResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.operation_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "operation_id"},message:"must have required property '"+"operation_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.lease_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "lease_id"},message:"must have required property '"+"lease_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.nonce === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "nonce"},message:"must have required property '"+"nonce"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.operation_id !== undefined){
if(typeof data.operation_id !== "string"){
const err3 = {instancePath:instancePath+"/operation_id",schemaPath:"#/properties/operation_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.lease_id !== undefined){
if(typeof data.lease_id !== "string"){
const err4 = {instancePath:instancePath+"/lease_id",schemaPath:"#/properties/lease_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.nonce !== undefined){
let data2 = data.nonce;
if((typeof data2 !== "string") && (data2 !== null)){
const err5 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/type",keyword:"type",params:{type: schema75.properties.nonce.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data2 === "string"){
if(!pattern21.test(data2)){
const err6 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate74.errors = vErrors;
return errors === 0;
}

export const PermissionModeParams = validate75;
const schema76 = {"type":"object","properties":{"command":true,"signature":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/PermissionModeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionModeParams","required":["command","signature"],"additionalProperties":true};

function validate75(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionModeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.command === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command"},message:"must have required property '"+"command"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.signature === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "signature"},message:"must have required property '"+"signature"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.signature !== undefined){
let data0 = data.signature;
if((typeof data0 !== "string") && (data0 !== null)){
const err2 = {instancePath:instancePath+"/signature",schemaPath:"#/properties/signature/type",keyword:"type",params:{type: schema76.properties.signature.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(typeof data0 === "string"){
if(!pattern21.test(data0)){
const err3 = {instancePath:instancePath+"/signature",schemaPath:"#/properties/signature/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate75.errors = vErrors;
return errors === 0;
}

export const PermissionModeResult = validate76;
const schema77 = {"type":"object","properties":{"command":{"type":"object","properties":{"content":{"type":["null","object"],"properties":{"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size"],"additionalProperties":true},"operation":{"type":"string"},"result":true,"failure":{"type":["null","object"],"properties":{"data":{"type":["null","object"],"properties":{"kind":{"type":"string"}},"required":["kind"],"additionalProperties":true},"code":{"type":"integer"},"message":{"type":"string"}},"required":["code","message"],"additionalProperties":true},"command_id":{"type":"string"},"ingress_seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"}},"required":["operation","command_id","ingress_seq","status"],"additionalProperties":true},"nonce":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/PermissionModeResult","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionModeResult","required":["command","nonce"],"additionalProperties":true};

function validate76(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionModeResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.command === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command"},message:"must have required property '"+"command"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.nonce === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "nonce"},message:"must have required property '"+"nonce"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.command !== undefined){
let data0 = data.command;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.operation === undefined){
const err2 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.command_id === undefined){
const err3 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/required",keyword:"required",params:{missingProperty: "command_id"},message:"must have required property '"+"command_id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.ingress_seq === undefined){
const err4 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/required",keyword:"required",params:{missingProperty: "ingress_seq"},message:"must have required property '"+"ingress_seq"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.status === undefined){
const err5 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.content !== undefined){
let data1 = data0.content;
if((data1 !== null) && (!(data1 && typeof data1 == "object" && !Array.isArray(data1)))){
const err6 = {instancePath:instancePath+"/command/content",schemaPath:"#/properties/command/properties/content/type",keyword:"type",params:{type: schema77.properties.command.properties.content.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.reference_id === undefined){
const err7 = {instancePath:instancePath+"/command/content",schemaPath:"#/properties/command/properties/content/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data1.digest === undefined){
const err8 = {instancePath:instancePath+"/command/content",schemaPath:"#/properties/command/properties/content/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data1.size === undefined){
const err9 = {instancePath:instancePath+"/command/content",schemaPath:"#/properties/command/properties/content/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data1.reference_id !== undefined){
if(typeof data1.reference_id !== "string"){
const err10 = {instancePath:instancePath+"/command/content/reference_id",schemaPath:"#/properties/command/properties/content/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data1.digest !== undefined){
if(typeof data1.digest !== "string"){
const err11 = {instancePath:instancePath+"/command/content/digest",schemaPath:"#/properties/command/properties/content/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data1.size !== undefined){
let data4 = data1.size;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err12 = {instancePath:instancePath+"/command/content/size",schemaPath:"#/properties/command/properties/content/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(!(formats0.validate(data4))){
const err13 = {instancePath:instancePath+"/command/content/size",schemaPath:"#/properties/command/properties/content/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
else {
const err14 = {instancePath:instancePath+"/command/content/size",schemaPath:"#/properties/command/properties/content/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data1.media_type !== undefined){
if(typeof data1.media_type !== "string"){
const err15 = {instancePath:instancePath+"/command/content/media_type",schemaPath:"#/properties/command/properties/content/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data1.source !== undefined){
if(typeof data1.source !== "string"){
const err16 = {instancePath:instancePath+"/command/content/source",schemaPath:"#/properties/command/properties/content/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
}
}
if(data0.operation !== undefined){
if(typeof data0.operation !== "string"){
const err17 = {instancePath:instancePath+"/command/operation",schemaPath:"#/properties/command/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data0.failure !== undefined){
let data8 = data0.failure;
if((data8 !== null) && (!(data8 && typeof data8 == "object" && !Array.isArray(data8)))){
const err18 = {instancePath:instancePath+"/command/failure",schemaPath:"#/properties/command/properties/failure/type",keyword:"type",params:{type: schema77.properties.command.properties.failure.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
if(data8.code === undefined){
const err19 = {instancePath:instancePath+"/command/failure",schemaPath:"#/properties/command/properties/failure/required",keyword:"required",params:{missingProperty: "code"},message:"must have required property '"+"code"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data8.message === undefined){
const err20 = {instancePath:instancePath+"/command/failure",schemaPath:"#/properties/command/properties/failure/required",keyword:"required",params:{missingProperty: "message"},message:"must have required property '"+"message"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data8.data !== undefined){
let data9 = data8.data;
if((data9 !== null) && (!(data9 && typeof data9 == "object" && !Array.isArray(data9)))){
const err21 = {instancePath:instancePath+"/command/failure/data",schemaPath:"#/properties/command/properties/failure/properties/data/type",keyword:"type",params:{type: schema77.properties.command.properties.failure.properties.data.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.kind === undefined){
const err22 = {instancePath:instancePath+"/command/failure/data",schemaPath:"#/properties/command/properties/failure/properties/data/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(data9.kind !== undefined){
if(typeof data9.kind !== "string"){
const err23 = {instancePath:instancePath+"/command/failure/data/kind",schemaPath:"#/properties/command/properties/failure/properties/data/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
}
}
if(data8.code !== undefined){
let data11 = data8.code;
if(!((typeof data11 == "number") && (!(data11 % 1) && !isNaN(data11)))){
const err24 = {instancePath:instancePath+"/command/failure/code",schemaPath:"#/properties/command/properties/failure/properties/code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data8.message !== undefined){
if(typeof data8.message !== "string"){
const err25 = {instancePath:instancePath+"/command/failure/message",schemaPath:"#/properties/command/properties/failure/properties/message/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
}
}
if(data0.command_id !== undefined){
if(typeof data0.command_id !== "string"){
const err26 = {instancePath:instancePath+"/command/command_id",schemaPath:"#/properties/command/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data0.ingress_seq !== undefined){
let data14 = data0.ingress_seq;
if(typeof data14 === "string"){
if(!pattern0.test(data14)){
const err27 = {instancePath:instancePath+"/command/ingress_seq",schemaPath:"#/properties/command/properties/ingress_seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(!(formats0.validate(data14))){
const err28 = {instancePath:instancePath+"/command/ingress_seq",schemaPath:"#/properties/command/properties/ingress_seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
else {
const err29 = {instancePath:instancePath+"/command/ingress_seq",schemaPath:"#/properties/command/properties/ingress_seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data0.status !== undefined){
if(typeof data0.status !== "string"){
const err30 = {instancePath:instancePath+"/command/status",schemaPath:"#/properties/command/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
}
else {
const err31 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data.nonce !== undefined){
let data16 = data.nonce;
if((typeof data16 !== "string") && (data16 !== null)){
const err32 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/type",keyword:"type",params:{type: schema77.properties.nonce.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(typeof data16 === "string"){
if(!pattern21.test(data16)){
const err33 = {instancePath:instancePath+"/nonce",schemaPath:"#/properties/nonce/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
}
}
else {
const err34 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
validate76.errors = vErrors;
return errors === 0;
}

export const PermissionRulesResult = validate77;
const schema78 = {"type":"object","properties":{"rules":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"root_id":{"type":"string"},"operation":{"type":"string"},"rule":{"type":"string"},"principal_id":{"type":"string"},"created_at":{"type":"string"}},"required":["id","root_id","operation","rule","principal_id","created_at"],"additionalProperties":true}},"global":{"type":["null","array"],"items":{"type":"string"}}},"$id":"https://whip.dev/protocol/v2/PermissionRulesResult","$schema":"http://json-schema.org/draft-07/schema#","title":"PermissionRulesResult","required":["rules","global"],"additionalProperties":true};

function validate77(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PermissionRulesResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.rules === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "rules"},message:"must have required property '"+"rules"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.global === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "global"},message:"must have required property '"+"global"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.rules !== undefined){
let data0 = data.rules;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/rules",schemaPath:"#/properties/rules/type",keyword:"type",params:{type: schema78.properties.rules.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(Array.isArray(data0)){
const len0 = data0.length;
for(let i0=0; i0<len0; i0++){
let data1 = data0[i0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.id === undefined){
const err3 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.root_id === undefined){
const err4 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.operation === undefined){
const err5 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.rule === undefined){
const err6 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/required",keyword:"required",params:{missingProperty: "rule"},message:"must have required property '"+"rule"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.principal_id === undefined){
const err7 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/required",keyword:"required",params:{missingProperty: "principal_id"},message:"must have required property '"+"principal_id"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data1.created_at === undefined){
const err8 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data1.id !== undefined){
if(typeof data1.id !== "string"){
const err9 = {instancePath:instancePath+"/rules/" + i0+"/id",schemaPath:"#/properties/rules/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data1.root_id !== undefined){
if(typeof data1.root_id !== "string"){
const err10 = {instancePath:instancePath+"/rules/" + i0+"/root_id",schemaPath:"#/properties/rules/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data1.operation !== undefined){
if(typeof data1.operation !== "string"){
const err11 = {instancePath:instancePath+"/rules/" + i0+"/operation",schemaPath:"#/properties/rules/items/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data1.rule !== undefined){
if(typeof data1.rule !== "string"){
const err12 = {instancePath:instancePath+"/rules/" + i0+"/rule",schemaPath:"#/properties/rules/items/properties/rule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data1.principal_id !== undefined){
if(typeof data1.principal_id !== "string"){
const err13 = {instancePath:instancePath+"/rules/" + i0+"/principal_id",schemaPath:"#/properties/rules/items/properties/principal_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data1.created_at !== undefined){
if(typeof data1.created_at !== "string"){
const err14 = {instancePath:instancePath+"/rules/" + i0+"/created_at",schemaPath:"#/properties/rules/items/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
else {
const err15 = {instancePath:instancePath+"/rules/" + i0,schemaPath:"#/properties/rules/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
}
}
if(data.global !== undefined){
let data8 = data.global;
if((data8 !== null) && (!(Array.isArray(data8)))){
const err16 = {instancePath:instancePath+"/global",schemaPath:"#/properties/global/type",keyword:"type",params:{type: schema78.properties.global.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(Array.isArray(data8)){
const len1 = data8.length;
for(let i1=0; i1<len1; i1++){
if(typeof data8[i1] !== "string"){
const err17 = {instancePath:instancePath+"/global/" + i1,schemaPath:"#/properties/global/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
}
}
}
else {
const err18 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
validate77.errors = vErrors;
return errors === 0;
}

export const PingResult = validate78;
const schema79 = {"type":"object","properties":{"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"build_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/PingResult","$schema":"http://json-schema.org/draft-07/schema#","title":"PingResult","required":["generation","build_id"],"additionalProperties":true};

function validate78(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/PingResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.generation === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.build_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "build_id"},message:"must have required property '"+"build_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.generation !== undefined){
let data0 = data.generation;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(!(formats0.validate(data0))){
const err3 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
else {
const err4 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.build_id !== undefined){
if(typeof data.build_id !== "string"){
const err5 = {instancePath:instancePath+"/build_id",schemaPath:"#/properties/build_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate78.errors = vErrors;
return errors === 0;
}

export const ProviderCatalogsResult = validate79;
const schema80 = {"type":"object","properties":{"models":{"type":"object","additionalProperties":{"type":"object","properties":{"name":{"type":"string"},"id":{"type":"string"},"providers":{"type":["null","array"],"items":{"type":"string"}},"context":{"type":"integer"},"vision":{"type":"boolean"}},"required":["providers"],"additionalProperties":true}},"providers":{"type":"object","additionalProperties":{"type":"object","properties":{"base_url":{"type":"string"}},"required":["base_url"],"additionalProperties":true}},"catalogs":{"type":"object","additionalProperties":{"type":"object","properties":{"fetched_at":{"type":"string"},"base_url":{"type":"string"},"models":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"context_length":{"type":"integer"},"max_completion_tokens":{"type":"integer"},"reasoning_efforts":{"type":["null","array"],"items":{"type":"string"}},"in_price":{"type":"number"},"out_price":{"type":"number"},"cache_read_price":{"type":"number"},"input_modalities":{"type":["null","array"],"items":{"type":"string"}}},"required":["id"],"additionalProperties":true}}},"required":["fetched_at","base_url","models"],"additionalProperties":true}},"errors":{"type":"object","additionalProperties":{"type":"string"}}},"$id":"https://whip.dev/protocol/v2/ProviderCatalogsResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderCatalogsResult","required":["models","providers","catalogs"],"additionalProperties":true};

function validate79(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderCatalogsResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.models === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "models"},message:"must have required property '"+"models"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.providers === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "providers"},message:"must have required property '"+"providers"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.catalogs === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "catalogs"},message:"must have required property '"+"catalogs"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.models !== undefined){
let data0 = data.models;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
for(const key0 in data0){
let data1 = data0[key0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.providers === undefined){
const err3 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/models/additionalProperties/required",keyword:"required",params:{missingProperty: "providers"},message:"must have required property '"+"providers"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.name !== undefined){
if(typeof data1.name !== "string"){
const err4 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/name",schemaPath:"#/properties/models/additionalProperties/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data1.id !== undefined){
if(typeof data1.id !== "string"){
const err5 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/models/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data1.providers !== undefined){
let data4 = data1.providers;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err6 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/providers",schemaPath:"#/properties/models/additionalProperties/properties/providers/type",keyword:"type",params:{type: schema80.properties.models.additionalProperties.properties.providers.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(Array.isArray(data4)){
const len0 = data4.length;
for(let i0=0; i0<len0; i0++){
if(typeof data4[i0] !== "string"){
const err7 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/providers/" + i0,schemaPath:"#/properties/models/additionalProperties/properties/providers/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
}
if(data1.context !== undefined){
let data6 = data1.context;
if(!((typeof data6 == "number") && (!(data6 % 1) && !isNaN(data6)))){
const err8 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/context",schemaPath:"#/properties/models/additionalProperties/properties/context/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data1.vision !== undefined){
if(typeof data1.vision !== "boolean"){
const err9 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1")+"/vision",schemaPath:"#/properties/models/additionalProperties/properties/vision/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
else {
const err10 = {instancePath:instancePath+"/models/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/models/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
else {
const err11 = {instancePath:instancePath+"/models",schemaPath:"#/properties/models/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.providers !== undefined){
let data8 = data.providers;
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
for(const key1 in data8){
let data9 = data8[key1];
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.base_url === undefined){
const err12 = {instancePath:instancePath+"/providers/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/providers/additionalProperties/required",keyword:"required",params:{missingProperty: "base_url"},message:"must have required property '"+"base_url"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data9.base_url !== undefined){
if(typeof data9.base_url !== "string"){
const err13 = {instancePath:instancePath+"/providers/" + key1.replace(/~/g, "~0").replace(/\//g, "~1")+"/base_url",schemaPath:"#/properties/providers/additionalProperties/properties/base_url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
}
else {
const err14 = {instancePath:instancePath+"/providers/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/providers/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
else {
const err15 = {instancePath:instancePath+"/providers",schemaPath:"#/properties/providers/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.catalogs !== undefined){
let data11 = data.catalogs;
if(data11 && typeof data11 == "object" && !Array.isArray(data11)){
for(const key2 in data11){
let data12 = data11[key2];
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
if(data12.fetched_at === undefined){
const err16 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/catalogs/additionalProperties/required",keyword:"required",params:{missingProperty: "fetched_at"},message:"must have required property '"+"fetched_at"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data12.base_url === undefined){
const err17 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/catalogs/additionalProperties/required",keyword:"required",params:{missingProperty: "base_url"},message:"must have required property '"+"base_url"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data12.models === undefined){
const err18 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/catalogs/additionalProperties/required",keyword:"required",params:{missingProperty: "models"},message:"must have required property '"+"models"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data12.fetched_at !== undefined){
if(typeof data12.fetched_at !== "string"){
const err19 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/fetched_at",schemaPath:"#/properties/catalogs/additionalProperties/properties/fetched_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data12.base_url !== undefined){
if(typeof data12.base_url !== "string"){
const err20 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/base_url",schemaPath:"#/properties/catalogs/additionalProperties/properties/base_url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data12.models !== undefined){
let data15 = data12.models;
if((data15 !== null) && (!(Array.isArray(data15)))){
const err21 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/type",keyword:"type",params:{type: schema80.properties.catalogs.additionalProperties.properties.models.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(Array.isArray(data15)){
const len1 = data15.length;
for(let i1=0; i1<len1; i1++){
let data16 = data15[i1];
if(data16 && typeof data16 == "object" && !Array.isArray(data16)){
if(data16.id === undefined){
const err22 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1,schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(data16.id !== undefined){
if(typeof data16.id !== "string"){
const err23 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/id",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data16.context_length !== undefined){
let data18 = data16.context_length;
if(!((typeof data18 == "number") && (!(data18 % 1) && !isNaN(data18)))){
const err24 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/context_length",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/context_length/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data16.max_completion_tokens !== undefined){
let data19 = data16.max_completion_tokens;
if(!((typeof data19 == "number") && (!(data19 % 1) && !isNaN(data19)))){
const err25 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/max_completion_tokens",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/max_completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data16.reasoning_efforts !== undefined){
let data20 = data16.reasoning_efforts;
if((data20 !== null) && (!(Array.isArray(data20)))){
const err26 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/reasoning_efforts",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/reasoning_efforts/type",keyword:"type",params:{type: schema80.properties.catalogs.additionalProperties.properties.models.items.properties.reasoning_efforts.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(Array.isArray(data20)){
const len2 = data20.length;
for(let i2=0; i2<len2; i2++){
if(typeof data20[i2] !== "string"){
const err27 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/reasoning_efforts/" + i2,schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/reasoning_efforts/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
}
}
if(data16.in_price !== undefined){
if(!(typeof data16.in_price == "number")){
const err28 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/in_price",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/in_price/type",keyword:"type",params:{type: "number"},message:"must be number"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data16.out_price !== undefined){
if(!(typeof data16.out_price == "number")){
const err29 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/out_price",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/out_price/type",keyword:"type",params:{type: "number"},message:"must be number"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data16.cache_read_price !== undefined){
if(!(typeof data16.cache_read_price == "number")){
const err30 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/cache_read_price",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/cache_read_price/type",keyword:"type",params:{type: "number"},message:"must be number"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data16.input_modalities !== undefined){
let data25 = data16.input_modalities;
if((data25 !== null) && (!(Array.isArray(data25)))){
const err31 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/input_modalities",schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/input_modalities/type",keyword:"type",params:{type: schema80.properties.catalogs.additionalProperties.properties.models.items.properties.input_modalities.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(Array.isArray(data25)){
const len3 = data25.length;
for(let i3=0; i3<len3; i3++){
if(typeof data25[i3] !== "string"){
const err32 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1+"/input_modalities/" + i3,schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/properties/input_modalities/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
}
}
}
else {
const err33 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/models/" + i1,schemaPath:"#/properties/catalogs/additionalProperties/properties/models/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
}
}
}
else {
const err34 = {instancePath:instancePath+"/catalogs/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/catalogs/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
}
else {
const err35 = {instancePath:instancePath+"/catalogs",schemaPath:"#/properties/catalogs/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data.errors !== undefined){
let data27 = data.errors;
if(data27 && typeof data27 == "object" && !Array.isArray(data27)){
for(const key3 in data27){
if(typeof data27[key3] !== "string"){
const err36 = {instancePath:instancePath+"/errors/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/errors/additionalProperties/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
}
else {
const err37 = {instancePath:instancePath+"/errors",schemaPath:"#/properties/errors/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
}
else {
const err38 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
validate79.errors = vErrors;
return errors === 0;
}

export const ProviderKeySetup = validate80;
const schema81 = {"type":"object","properties":{"revision":{"type":"string"},"provider":{"type":"string"},"key":{"type":"string"},"environment":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/ProviderKeySetup","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderKeySetup","required":["revision","provider","environment"],"additionalProperties":true};

function validate80(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderKeySetup" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.provider === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.environment === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "environment"},message:"must have required property '"+"environment"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.revision !== undefined){
if(typeof data.revision !== "string"){
const err3 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err4 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.key !== undefined){
if(typeof data.key !== "string"){
const err5 = {instancePath:instancePath+"/key",schemaPath:"#/properties/key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.environment !== undefined){
if(typeof data.environment !== "boolean"){
const err6 = {instancePath:instancePath+"/environment",schemaPath:"#/properties/environment/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate80.errors = vErrors;
return errors === 0;
}

export const ProviderLoginCreateParams = validate81;
const schema82 = {"type":"object","properties":{"flow_id":{"type":"string"},"name":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderLoginCreateParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderLoginCreateParams","required":["flow_id","name"],"additionalProperties":true};

function validate81(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderLoginCreateParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.flow_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "flow_id"},message:"must have required property '"+"flow_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.name === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.flow_id !== undefined){
if(typeof data.flow_id !== "string"){
const err2 = {instancePath:instancePath+"/flow_id",schemaPath:"#/properties/flow_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.name !== undefined){
if(typeof data.name !== "string"){
const err3 = {instancePath:instancePath+"/name",schemaPath:"#/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate81.errors = vErrors;
return errors === 0;
}

export const ProviderLoginList = validate82;
const schema83 = {"type":"object","properties":{"flows":{"type":["null","array"],"items":{"type":"object","properties":{"flow_id":{"type":"string"},"state":{"type":"string"},"verification_url":{"type":"string"},"user_code":{"type":"string"},"email":{"type":"string"},"teams":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"additionalProperties":true}},"projects":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"additionalProperties":true}},"team_id":{"type":"string"},"project_id":{"type":"string"},"expires_at":{"type":"string"},"error":{"type":"string"}},"required":["flow_id","state","teams","projects","expires_at"],"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/ProviderLoginList","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderLoginList","required":["flows"],"additionalProperties":true};

function validate82(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderLoginList" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.flows === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "flows"},message:"must have required property '"+"flows"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.flows !== undefined){
let data0 = data.flows;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err1 = {instancePath:instancePath+"/flows",schemaPath:"#/properties/flows/type",keyword:"type",params:{type: schema83.properties.flows.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(Array.isArray(data0)){
const len0 = data0.length;
for(let i0=0; i0<len0; i0++){
let data1 = data0[i0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.flow_id === undefined){
const err2 = {instancePath:instancePath+"/flows/" + i0,schemaPath:"#/properties/flows/items/required",keyword:"required",params:{missingProperty: "flow_id"},message:"must have required property '"+"flow_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data1.state === undefined){
const err3 = {instancePath:instancePath+"/flows/" + i0,schemaPath:"#/properties/flows/items/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.teams === undefined){
const err4 = {instancePath:instancePath+"/flows/" + i0,schemaPath:"#/properties/flows/items/required",keyword:"required",params:{missingProperty: "teams"},message:"must have required property '"+"teams"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.projects === undefined){
const err5 = {instancePath:instancePath+"/flows/" + i0,schemaPath:"#/properties/flows/items/required",keyword:"required",params:{missingProperty: "projects"},message:"must have required property '"+"projects"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.expires_at === undefined){
const err6 = {instancePath:instancePath+"/flows/" + i0,schemaPath:"#/properties/flows/items/required",keyword:"required",params:{missingProperty: "expires_at"},message:"must have required property '"+"expires_at"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.flow_id !== undefined){
if(typeof data1.flow_id !== "string"){
const err7 = {instancePath:instancePath+"/flows/" + i0+"/flow_id",schemaPath:"#/properties/flows/items/properties/flow_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data1.state !== undefined){
if(typeof data1.state !== "string"){
const err8 = {instancePath:instancePath+"/flows/" + i0+"/state",schemaPath:"#/properties/flows/items/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data1.verification_url !== undefined){
if(typeof data1.verification_url !== "string"){
const err9 = {instancePath:instancePath+"/flows/" + i0+"/verification_url",schemaPath:"#/properties/flows/items/properties/verification_url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data1.user_code !== undefined){
if(typeof data1.user_code !== "string"){
const err10 = {instancePath:instancePath+"/flows/" + i0+"/user_code",schemaPath:"#/properties/flows/items/properties/user_code/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data1.email !== undefined){
if(typeof data1.email !== "string"){
const err11 = {instancePath:instancePath+"/flows/" + i0+"/email",schemaPath:"#/properties/flows/items/properties/email/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data1.teams !== undefined){
let data7 = data1.teams;
if((data7 !== null) && (!(Array.isArray(data7)))){
const err12 = {instancePath:instancePath+"/flows/" + i0+"/teams",schemaPath:"#/properties/flows/items/properties/teams/type",keyword:"type",params:{type: schema83.properties.flows.items.properties.teams.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(Array.isArray(data7)){
const len1 = data7.length;
for(let i1=0; i1<len1; i1++){
let data8 = data7[i1];
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
if(data8.id === undefined){
const err13 = {instancePath:instancePath+"/flows/" + i0+"/teams/" + i1,schemaPath:"#/properties/flows/items/properties/teams/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data8.name === undefined){
const err14 = {instancePath:instancePath+"/flows/" + i0+"/teams/" + i1,schemaPath:"#/properties/flows/items/properties/teams/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data8.id !== undefined){
if(typeof data8.id !== "string"){
const err15 = {instancePath:instancePath+"/flows/" + i0+"/teams/" + i1+"/id",schemaPath:"#/properties/flows/items/properties/teams/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data8.name !== undefined){
if(typeof data8.name !== "string"){
const err16 = {instancePath:instancePath+"/flows/" + i0+"/teams/" + i1+"/name",schemaPath:"#/properties/flows/items/properties/teams/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
}
else {
const err17 = {instancePath:instancePath+"/flows/" + i0+"/teams/" + i1,schemaPath:"#/properties/flows/items/properties/teams/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
}
}
if(data1.projects !== undefined){
let data11 = data1.projects;
if((data11 !== null) && (!(Array.isArray(data11)))){
const err18 = {instancePath:instancePath+"/flows/" + i0+"/projects",schemaPath:"#/properties/flows/items/properties/projects/type",keyword:"type",params:{type: schema83.properties.flows.items.properties.projects.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(Array.isArray(data11)){
const len2 = data11.length;
for(let i2=0; i2<len2; i2++){
let data12 = data11[i2];
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
if(data12.id === undefined){
const err19 = {instancePath:instancePath+"/flows/" + i0+"/projects/" + i2,schemaPath:"#/properties/flows/items/properties/projects/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data12.name === undefined){
const err20 = {instancePath:instancePath+"/flows/" + i0+"/projects/" + i2,schemaPath:"#/properties/flows/items/properties/projects/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data12.id !== undefined){
if(typeof data12.id !== "string"){
const err21 = {instancePath:instancePath+"/flows/" + i0+"/projects/" + i2+"/id",schemaPath:"#/properties/flows/items/properties/projects/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data12.name !== undefined){
if(typeof data12.name !== "string"){
const err22 = {instancePath:instancePath+"/flows/" + i0+"/projects/" + i2+"/name",schemaPath:"#/properties/flows/items/properties/projects/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
}
else {
const err23 = {instancePath:instancePath+"/flows/" + i0+"/projects/" + i2,schemaPath:"#/properties/flows/items/properties/projects/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
}
}
if(data1.team_id !== undefined){
if(typeof data1.team_id !== "string"){
const err24 = {instancePath:instancePath+"/flows/" + i0+"/team_id",schemaPath:"#/properties/flows/items/properties/team_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data1.project_id !== undefined){
if(typeof data1.project_id !== "string"){
const err25 = {instancePath:instancePath+"/flows/" + i0+"/project_id",schemaPath:"#/properties/flows/items/properties/project_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data1.expires_at !== undefined){
if(typeof data1.expires_at !== "string"){
const err26 = {instancePath:instancePath+"/flows/" + i0+"/expires_at",schemaPath:"#/properties/flows/items/properties/expires_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data1.error !== undefined){
if(typeof data1.error !== "string"){
const err27 = {instancePath:instancePath+"/flows/" + i0+"/error",schemaPath:"#/properties/flows/items/properties/error/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
}
else {
const err28 = {instancePath:instancePath+"/flows/" + i0,schemaPath:"#/properties/flows/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
}
}
}
else {
const err29 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
validate82.errors = vErrors;
return errors === 0;
}

export const ProviderLoginParams = validate83;
const schema84 = {"type":"object","properties":{"flow_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderLoginParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderLoginParams","required":["flow_id"],"additionalProperties":true};

function validate83(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderLoginParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.flow_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "flow_id"},message:"must have required property '"+"flow_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.flow_id !== undefined){
if(typeof data.flow_id !== "string"){
const err1 = {instancePath:instancePath+"/flow_id",schemaPath:"#/properties/flow_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate83.errors = vErrors;
return errors === 0;
}

export const ProviderLoginProjectParams = validate84;
const schema85 = {"type":"object","properties":{"flow_id":{"type":"string"},"project_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderLoginProjectParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderLoginProjectParams","required":["flow_id","project_id"],"additionalProperties":true};

function validate84(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderLoginProjectParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.flow_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "flow_id"},message:"must have required property '"+"flow_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.project_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "project_id"},message:"must have required property '"+"project_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.flow_id !== undefined){
if(typeof data.flow_id !== "string"){
const err2 = {instancePath:instancePath+"/flow_id",schemaPath:"#/properties/flow_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.project_id !== undefined){
if(typeof data.project_id !== "string"){
const err3 = {instancePath:instancePath+"/project_id",schemaPath:"#/properties/project_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate84.errors = vErrors;
return errors === 0;
}

export const ProviderLoginStatus = validate85;
const schema86 = {"type":"object","properties":{"flow_id":{"type":"string"},"state":{"type":"string"},"verification_url":{"type":"string"},"user_code":{"type":"string"},"email":{"type":"string"},"teams":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"additionalProperties":true}},"projects":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"additionalProperties":true}},"team_id":{"type":"string"},"project_id":{"type":"string"},"expires_at":{"type":"string"},"error":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderLoginStatus","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderLoginStatus","required":["flow_id","state","teams","projects","expires_at"],"additionalProperties":true};

function validate85(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderLoginStatus" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.flow_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "flow_id"},message:"must have required property '"+"flow_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.state === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.teams === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "teams"},message:"must have required property '"+"teams"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.projects === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "projects"},message:"must have required property '"+"projects"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.expires_at === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expires_at"},message:"must have required property '"+"expires_at"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.flow_id !== undefined){
if(typeof data.flow_id !== "string"){
const err5 = {instancePath:instancePath+"/flow_id",schemaPath:"#/properties/flow_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.state !== undefined){
if(typeof data.state !== "string"){
const err6 = {instancePath:instancePath+"/state",schemaPath:"#/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.verification_url !== undefined){
if(typeof data.verification_url !== "string"){
const err7 = {instancePath:instancePath+"/verification_url",schemaPath:"#/properties/verification_url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.user_code !== undefined){
if(typeof data.user_code !== "string"){
const err8 = {instancePath:instancePath+"/user_code",schemaPath:"#/properties/user_code/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.email !== undefined){
if(typeof data.email !== "string"){
const err9 = {instancePath:instancePath+"/email",schemaPath:"#/properties/email/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.teams !== undefined){
let data5 = data.teams;
if((data5 !== null) && (!(Array.isArray(data5)))){
const err10 = {instancePath:instancePath+"/teams",schemaPath:"#/properties/teams/type",keyword:"type",params:{type: schema86.properties.teams.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(Array.isArray(data5)){
const len0 = data5.length;
for(let i0=0; i0<len0; i0++){
let data6 = data5[i0];
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.id === undefined){
const err11 = {instancePath:instancePath+"/teams/" + i0,schemaPath:"#/properties/teams/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data6.name === undefined){
const err12 = {instancePath:instancePath+"/teams/" + i0,schemaPath:"#/properties/teams/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data6.id !== undefined){
if(typeof data6.id !== "string"){
const err13 = {instancePath:instancePath+"/teams/" + i0+"/id",schemaPath:"#/properties/teams/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data6.name !== undefined){
if(typeof data6.name !== "string"){
const err14 = {instancePath:instancePath+"/teams/" + i0+"/name",schemaPath:"#/properties/teams/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
}
else {
const err15 = {instancePath:instancePath+"/teams/" + i0,schemaPath:"#/properties/teams/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
}
}
if(data.projects !== undefined){
let data9 = data.projects;
if((data9 !== null) && (!(Array.isArray(data9)))){
const err16 = {instancePath:instancePath+"/projects",schemaPath:"#/properties/projects/type",keyword:"type",params:{type: schema86.properties.projects.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(Array.isArray(data9)){
const len1 = data9.length;
for(let i1=0; i1<len1; i1++){
let data10 = data9[i1];
if(data10 && typeof data10 == "object" && !Array.isArray(data10)){
if(data10.id === undefined){
const err17 = {instancePath:instancePath+"/projects/" + i1,schemaPath:"#/properties/projects/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data10.name === undefined){
const err18 = {instancePath:instancePath+"/projects/" + i1,schemaPath:"#/properties/projects/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data10.id !== undefined){
if(typeof data10.id !== "string"){
const err19 = {instancePath:instancePath+"/projects/" + i1+"/id",schemaPath:"#/properties/projects/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data10.name !== undefined){
if(typeof data10.name !== "string"){
const err20 = {instancePath:instancePath+"/projects/" + i1+"/name",schemaPath:"#/properties/projects/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
}
else {
const err21 = {instancePath:instancePath+"/projects/" + i1,schemaPath:"#/properties/projects/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
}
}
if(data.team_id !== undefined){
if(typeof data.team_id !== "string"){
const err22 = {instancePath:instancePath+"/team_id",schemaPath:"#/properties/team_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data.project_id !== undefined){
if(typeof data.project_id !== "string"){
const err23 = {instancePath:instancePath+"/project_id",schemaPath:"#/properties/project_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data.expires_at !== undefined){
if(typeof data.expires_at !== "string"){
const err24 = {instancePath:instancePath+"/expires_at",schemaPath:"#/properties/expires_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data.error !== undefined){
if(typeof data.error !== "string"){
const err25 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
}
else {
const err26 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
validate85.errors = vErrors;
return errors === 0;
}

export const ProviderLoginTeamParams = validate86;
const schema87 = {"type":"object","properties":{"flow_id":{"type":"string"},"team_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderLoginTeamParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderLoginTeamParams","required":["flow_id","team_id"],"additionalProperties":true};

function validate86(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderLoginTeamParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.flow_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "flow_id"},message:"must have required property '"+"flow_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.team_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "team_id"},message:"must have required property '"+"team_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.flow_id !== undefined){
if(typeof data.flow_id !== "string"){
const err2 = {instancePath:instancePath+"/flow_id",schemaPath:"#/properties/flow_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.team_id !== undefined){
if(typeof data.team_id !== "string"){
const err3 = {instancePath:instancePath+"/team_id",schemaPath:"#/properties/team_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate86.errors = vErrors;
return errors === 0;
}

export const ProviderNameParams = validate87;
const schema88 = {"type":"object","properties":{"provider":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderNameParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderNameParams","required":["provider"],"additionalProperties":true};

function validate87(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderNameParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.provider === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err1 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate87.errors = vErrors;
return errors === 0;
}

export const ProviderStatus = validate88;
const schema89 = {"type":"object","properties":{"provider":{"type":"string"},"configured":{"type":"boolean"},"key_source":{"type":"string"},"email":{"type":"string"},"project_id":{"type":"string"},"project_name":{"type":"string"},"machine_key_name":{"type":"string"},"warnings":{"type":["null","array"],"items":{"type":"string"}}},"$id":"https://whip.dev/protocol/v2/ProviderStatus","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderStatus","required":["provider","configured","key_source","warnings"],"additionalProperties":true};

function validate88(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderStatus" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.provider === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.configured === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "configured"},message:"must have required property '"+"configured"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.key_source === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "key_source"},message:"must have required property '"+"key_source"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.warnings === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "warnings"},message:"must have required property '"+"warnings"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err4 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.configured !== undefined){
if(typeof data.configured !== "boolean"){
const err5 = {instancePath:instancePath+"/configured",schemaPath:"#/properties/configured/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.key_source !== undefined){
if(typeof data.key_source !== "string"){
const err6 = {instancePath:instancePath+"/key_source",schemaPath:"#/properties/key_source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.email !== undefined){
if(typeof data.email !== "string"){
const err7 = {instancePath:instancePath+"/email",schemaPath:"#/properties/email/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.project_id !== undefined){
if(typeof data.project_id !== "string"){
const err8 = {instancePath:instancePath+"/project_id",schemaPath:"#/properties/project_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.project_name !== undefined){
if(typeof data.project_name !== "string"){
const err9 = {instancePath:instancePath+"/project_name",schemaPath:"#/properties/project_name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.machine_key_name !== undefined){
if(typeof data.machine_key_name !== "string"){
const err10 = {instancePath:instancePath+"/machine_key_name",schemaPath:"#/properties/machine_key_name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.warnings !== undefined){
let data7 = data.warnings;
if((data7 !== null) && (!(Array.isArray(data7)))){
const err11 = {instancePath:instancePath+"/warnings",schemaPath:"#/properties/warnings/type",keyword:"type",params:{type: schema89.properties.warnings.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(Array.isArray(data7)){
const len0 = data7.length;
for(let i0=0; i0<len0; i0++){
if(typeof data7[i0] !== "string"){
const err12 = {instancePath:instancePath+"/warnings/" + i0,schemaPath:"#/properties/warnings/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
}
}
}
else {
const err13 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
validate88.errors = vErrors;
return errors === 0;
}

export const ProviderValidateParams = validate89;
const schema90 = {"type":"object","properties":{"name":{"type":"string"},"base_url":{"type":"string"},"key":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ProviderValidateParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderValidateParams","required":["name","base_url","key"],"additionalProperties":true};

function validate89(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderValidateParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.name === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.base_url === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "base_url"},message:"must have required property '"+"base_url"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.key === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "key"},message:"must have required property '"+"key"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.name !== undefined){
if(typeof data.name !== "string"){
const err3 = {instancePath:instancePath+"/name",schemaPath:"#/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.base_url !== undefined){
if(typeof data.base_url !== "string"){
const err4 = {instancePath:instancePath+"/base_url",schemaPath:"#/properties/base_url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.key !== undefined){
if(typeof data.key !== "string"){
const err5 = {instancePath:instancePath+"/key",schemaPath:"#/properties/key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate89.errors = vErrors;
return errors === 0;
}

export const ProviderValidateResult = validate90;
const schema91 = {"type":"object","properties":{"models":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"context_length":{"type":"integer"},"max_completion_tokens":{"type":"integer"},"reasoning_efforts":{"type":["null","array"],"items":{"type":"string"}},"pricing":{"type":["null","object"],"properties":{"prompt":{"type":"string"},"completion":{"type":"string"},"input_cache_read":{"type":"string"}},"required":["prompt","completion"],"additionalProperties":true},"input_modalities":{"type":["null","array"],"items":{"type":"string"}}},"required":["id"],"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/ProviderValidateResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ProviderValidateResult","required":["models"],"additionalProperties":true};

function validate90(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ProviderValidateResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.models === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "models"},message:"must have required property '"+"models"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.models !== undefined){
let data0 = data.models;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err1 = {instancePath:instancePath+"/models",schemaPath:"#/properties/models/type",keyword:"type",params:{type: schema91.properties.models.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(Array.isArray(data0)){
const len0 = data0.length;
for(let i0=0; i0<len0; i0++){
let data1 = data0[i0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.id === undefined){
const err2 = {instancePath:instancePath+"/models/" + i0,schemaPath:"#/properties/models/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data1.id !== undefined){
if(typeof data1.id !== "string"){
const err3 = {instancePath:instancePath+"/models/" + i0+"/id",schemaPath:"#/properties/models/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data1.context_length !== undefined){
let data3 = data1.context_length;
if(!((typeof data3 == "number") && (!(data3 % 1) && !isNaN(data3)))){
const err4 = {instancePath:instancePath+"/models/" + i0+"/context_length",schemaPath:"#/properties/models/items/properties/context_length/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data1.max_completion_tokens !== undefined){
let data4 = data1.max_completion_tokens;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err5 = {instancePath:instancePath+"/models/" + i0+"/max_completion_tokens",schemaPath:"#/properties/models/items/properties/max_completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data1.reasoning_efforts !== undefined){
let data5 = data1.reasoning_efforts;
if((data5 !== null) && (!(Array.isArray(data5)))){
const err6 = {instancePath:instancePath+"/models/" + i0+"/reasoning_efforts",schemaPath:"#/properties/models/items/properties/reasoning_efforts/type",keyword:"type",params:{type: schema91.properties.models.items.properties.reasoning_efforts.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(Array.isArray(data5)){
const len1 = data5.length;
for(let i1=0; i1<len1; i1++){
if(typeof data5[i1] !== "string"){
const err7 = {instancePath:instancePath+"/models/" + i0+"/reasoning_efforts/" + i1,schemaPath:"#/properties/models/items/properties/reasoning_efforts/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
}
if(data1.pricing !== undefined){
let data7 = data1.pricing;
if((data7 !== null) && (!(data7 && typeof data7 == "object" && !Array.isArray(data7)))){
const err8 = {instancePath:instancePath+"/models/" + i0+"/pricing",schemaPath:"#/properties/models/items/properties/pricing/type",keyword:"type",params:{type: schema91.properties.models.items.properties.pricing.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.prompt === undefined){
const err9 = {instancePath:instancePath+"/models/" + i0+"/pricing",schemaPath:"#/properties/models/items/properties/pricing/required",keyword:"required",params:{missingProperty: "prompt"},message:"must have required property '"+"prompt"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data7.completion === undefined){
const err10 = {instancePath:instancePath+"/models/" + i0+"/pricing",schemaPath:"#/properties/models/items/properties/pricing/required",keyword:"required",params:{missingProperty: "completion"},message:"must have required property '"+"completion"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data7.prompt !== undefined){
if(typeof data7.prompt !== "string"){
const err11 = {instancePath:instancePath+"/models/" + i0+"/pricing/prompt",schemaPath:"#/properties/models/items/properties/pricing/properties/prompt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data7.completion !== undefined){
if(typeof data7.completion !== "string"){
const err12 = {instancePath:instancePath+"/models/" + i0+"/pricing/completion",schemaPath:"#/properties/models/items/properties/pricing/properties/completion/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data7.input_cache_read !== undefined){
if(typeof data7.input_cache_read !== "string"){
const err13 = {instancePath:instancePath+"/models/" + i0+"/pricing/input_cache_read",schemaPath:"#/properties/models/items/properties/pricing/properties/input_cache_read/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
}
}
if(data1.input_modalities !== undefined){
let data11 = data1.input_modalities;
if((data11 !== null) && (!(Array.isArray(data11)))){
const err14 = {instancePath:instancePath+"/models/" + i0+"/input_modalities",schemaPath:"#/properties/models/items/properties/input_modalities/type",keyword:"type",params:{type: schema91.properties.models.items.properties.input_modalities.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(Array.isArray(data11)){
const len2 = data11.length;
for(let i2=0; i2<len2; i2++){
if(typeof data11[i2] !== "string"){
const err15 = {instancePath:instancePath+"/models/" + i0+"/input_modalities/" + i2,schemaPath:"#/properties/models/items/properties/input_modalities/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
}
}
}
else {
const err16 = {instancePath:instancePath+"/models/" + i0,schemaPath:"#/properties/models/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
}
}
}
else {
const err17 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
validate90.errors = vErrors;
return errors === 0;
}

export const QueryParams = validate91;
const schema92 = {"type":"object","properties":{"root_id":{"type":"string"},"operation":{"type":"string"},"payload":true},"$id":"https://whip.dev/protocol/v2/QueryParams","$schema":"http://json-schema.org/draft-07/schema#","title":"QueryParams","required":["operation"],"additionalProperties":true};

function validate91(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/QueryParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.operation === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err1 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.operation !== undefined){
if(typeof data.operation !== "string"){
const err2 = {instancePath:instancePath+"/operation",schemaPath:"#/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
}
else {
const err3 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
validate91.errors = vErrors;
return errors === 0;
}

export const QueryResult = validate92;
const schema93 = {"type":"object","properties":{"result":true,"content":{"type":["null","object"],"properties":{"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size"],"additionalProperties":true},"root_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/QueryResult","$schema":"http://json-schema.org/draft-07/schema#","title":"QueryResult","additionalProperties":true};

function validate92(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/QueryResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.content !== undefined){
let data0 = data.content;
if((data0 !== null) && (!(data0 && typeof data0 == "object" && !Array.isArray(data0)))){
const err0 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/type",keyword:"type",params:{type: schema93.properties.content.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.reference_id === undefined){
const err1 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.digest === undefined){
const err2 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.size === undefined){
const err3 = {instancePath:instancePath+"/content",schemaPath:"#/properties/content/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.reference_id !== undefined){
if(typeof data0.reference_id !== "string"){
const err4 = {instancePath:instancePath+"/content/reference_id",schemaPath:"#/properties/content/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data0.digest !== undefined){
if(typeof data0.digest !== "string"){
const err5 = {instancePath:instancePath+"/content/digest",schemaPath:"#/properties/content/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.size !== undefined){
let data3 = data0.size;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err6 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!(formats0.validate(data3))){
const err7 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
else {
const err8 = {instancePath:instancePath+"/content/size",schemaPath:"#/properties/content/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data0.media_type !== undefined){
if(typeof data0.media_type !== "string"){
const err9 = {instancePath:instancePath+"/content/media_type",schemaPath:"#/properties/content/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.source !== undefined){
if(typeof data0.source !== "string"){
const err10 = {instancePath:instancePath+"/content/source",schemaPath:"#/properties/content/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err11 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
else {
const err12 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
validate92.errors = vErrors;
return errors === 0;
}

export const QuestionAnswerParams = validate93;
const schema94 = {"type":"object","properties":{"id":{"type":"string"},"answer":{"type":["null","array"],"items":{"type":"string"}},"dismissed":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/QuestionAnswerParams","$schema":"http://json-schema.org/draft-07/schema#","title":"QuestionAnswerParams","required":["id","answer","dismissed"],"additionalProperties":true};

function validate93(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/QuestionAnswerParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.answer === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "answer"},message:"must have required property '"+"answer"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.dismissed === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "dismissed"},message:"must have required property '"+"dismissed"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err3 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.answer !== undefined){
let data1 = data.answer;
if((data1 !== null) && (!(Array.isArray(data1)))){
const err4 = {instancePath:instancePath+"/answer",schemaPath:"#/properties/answer/type",keyword:"type",params:{type: schema94.properties.answer.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(Array.isArray(data1)){
const len0 = data1.length;
for(let i0=0; i0<len0; i0++){
if(typeof data1[i0] !== "string"){
const err5 = {instancePath:instancePath+"/answer/" + i0,schemaPath:"#/properties/answer/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
}
if(data.dismissed !== undefined){
if(typeof data.dismissed !== "boolean"){
const err6 = {instancePath:instancePath+"/dismissed",schemaPath:"#/properties/dismissed/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate93.errors = vErrors;
return errors === 0;
}

export const RPCError = validate94;
const schema95 = {"type":"object","properties":{"data":{"type":["null","object"],"properties":{"kind":{"type":"string"}},"required":["kind"],"additionalProperties":true},"code":{"type":"integer"},"message":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/RPCError","$schema":"http://json-schema.org/draft-07/schema#","title":"RPCError","required":["code","message"],"additionalProperties":true};

function validate94(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RPCError" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.code === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "code"},message:"must have required property '"+"code"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.message === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "message"},message:"must have required property '"+"message"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.data !== undefined){
let data0 = data.data;
if((data0 !== null) && (!(data0 && typeof data0 == "object" && !Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/data",schemaPath:"#/properties/data/type",keyword:"type",params:{type: schema95.properties.data.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.kind === undefined){
const err3 = {instancePath:instancePath+"/data",schemaPath:"#/properties/data/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.kind !== undefined){
if(typeof data0.kind !== "string"){
const err4 = {instancePath:instancePath+"/data/kind",schemaPath:"#/properties/data/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
}
}
if(data.code !== undefined){
let data2 = data.code;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err5 = {instancePath:instancePath+"/code",schemaPath:"#/properties/code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.message !== undefined){
if(typeof data.message !== "string"){
const err6 = {instancePath:instancePath+"/message",schemaPath:"#/properties/message/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate94.errors = vErrors;
return errors === 0;
}

export const ReplayParams = validate95;
const schema96 = {"type":"object","properties":{"root_id":{"type":"string"},"cursor":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"limit":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/ReplayParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ReplayParams","required":["root_id","cursor"],"additionalProperties":true};

function validate95(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ReplayParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.cursor === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cursor"},message:"must have required property '"+"cursor"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err2 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.cursor !== undefined){
let data1 = data.cursor;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err3 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(!(formats0.validate(data1))){
const err4 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
else {
const err5 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.limit !== undefined){
let data2 = data.limit;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err6 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
}
else {
const err7 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
validate95.errors = vErrors;
return errors === 0;
}

export const ReplayResult = validate96;
const schema97 = {"type":"object","properties":{"events":{"type":["null","array"],"items":{"type":"object","properties":{"subscription_id":{"type":"string"},"root_id":{"type":"string"},"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"payload":true},"required":["root_id","seq","kind"],"additionalProperties":true}},"latest":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"expired":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/ReplayResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ReplayResult","required":["events","latest"],"additionalProperties":true};

function validate96(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ReplayResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.events === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "events"},message:"must have required property '"+"events"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.latest === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "latest"},message:"must have required property '"+"latest"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.events !== undefined){
let data0 = data.events;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/events",schemaPath:"#/properties/events/type",keyword:"type",params:{type: schema97.properties.events.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(Array.isArray(data0)){
const len0 = data0.length;
for(let i0=0; i0<len0; i0++){
let data1 = data0[i0];
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.root_id === undefined){
const err3 = {instancePath:instancePath+"/events/" + i0,schemaPath:"#/properties/events/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.seq === undefined){
const err4 = {instancePath:instancePath+"/events/" + i0,schemaPath:"#/properties/events/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.kind === undefined){
const err5 = {instancePath:instancePath+"/events/" + i0,schemaPath:"#/properties/events/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.subscription_id !== undefined){
if(typeof data1.subscription_id !== "string"){
const err6 = {instancePath:instancePath+"/events/" + i0+"/subscription_id",schemaPath:"#/properties/events/items/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data1.root_id !== undefined){
if(typeof data1.root_id !== "string"){
const err7 = {instancePath:instancePath+"/events/" + i0+"/root_id",schemaPath:"#/properties/events/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data1.seq !== undefined){
let data4 = data1.seq;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err8 = {instancePath:instancePath+"/events/" + i0+"/seq",schemaPath:"#/properties/events/items/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(!(formats0.validate(data4))){
const err9 = {instancePath:instancePath+"/events/" + i0+"/seq",schemaPath:"#/properties/events/items/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
else {
const err10 = {instancePath:instancePath+"/events/" + i0+"/seq",schemaPath:"#/properties/events/items/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data1.kind !== undefined){
if(typeof data1.kind !== "string"){
const err11 = {instancePath:instancePath+"/events/" + i0+"/kind",schemaPath:"#/properties/events/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
else {
const err12 = {instancePath:instancePath+"/events/" + i0,schemaPath:"#/properties/events/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
}
}
if(data.latest !== undefined){
let data6 = data.latest;
if(typeof data6 === "string"){
if(!pattern0.test(data6)){
const err13 = {instancePath:instancePath+"/latest",schemaPath:"#/properties/latest/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(!(formats0.validate(data6))){
const err14 = {instancePath:instancePath+"/latest",schemaPath:"#/properties/latest/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
else {
const err15 = {instancePath:instancePath+"/latest",schemaPath:"#/properties/latest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.expired !== undefined){
if(typeof data.expired !== "boolean"){
const err16 = {instancePath:instancePath+"/expired",schemaPath:"#/properties/expired/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
}
else {
const err17 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
validate96.errors = vErrors;
return errors === 0;
}

export const RestartNotice = validate97;
const schema98 = {"type":"object","properties":{"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"cursors":{"type":"object","additionalProperties":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}}},"$id":"https://whip.dev/protocol/v2/RestartNotice","$schema":"http://json-schema.org/draft-07/schema#","title":"RestartNotice","required":["generation","cursors"],"additionalProperties":true};

function validate97(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RestartNotice" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.generation === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.cursors === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cursors"},message:"must have required property '"+"cursors"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.generation !== undefined){
let data0 = data.generation;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(!(formats0.validate(data0))){
const err3 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
else {
const err4 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.cursors !== undefined){
let data1 = data.cursors;
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
for(const key0 in data1){
let data2 = data1[key0];
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err5 = {instancePath:instancePath+"/cursors/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/cursors/additionalProperties/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(formats0.validate(data2))){
const err6 = {instancePath:instancePath+"/cursors/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/cursors/additionalProperties/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
else {
const err7 = {instancePath:instancePath+"/cursors/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/cursors/additionalProperties/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath:instancePath+"/cursors",schemaPath:"#/properties/cursors/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
else {
const err9 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
validate97.errors = vErrors;
return errors === 0;
}

export const RestartParams = validate98;
const schema99 = {"type":"object","properties":{"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"$id":"https://whip.dev/protocol/v2/RestartParams","$schema":"http://json-schema.org/draft-07/schema#","title":"RestartParams","required":["generation"],"additionalProperties":true};

function validate98(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RestartParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.generation === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.generation !== undefined){
let data0 = data.generation;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err1 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(!(formats0.validate(data0))){
const err2 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
else {
const err3 = {instancePath:instancePath+"/generation",schemaPath:"#/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate98.errors = vErrors;
return errors === 0;
}

export const RewindParams = validate99;
const schema100 = {"type":"object","properties":{"expected_revision":{"type":["string","null"],"pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"cut":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/RewindParams","$schema":"http://json-schema.org/draft-07/schema#","title":"RewindParams","required":["expected_revision","cut"],"additionalProperties":true};

function validate99(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RewindParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.expected_revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expected_revision"},message:"must have required property '"+"expected_revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.cut === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cut"},message:"must have required property '"+"cut"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.expected_revision !== undefined){
let data0 = data.expected_revision;
if((typeof data0 !== "string") && (data0 !== null)){
const err2 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/type",keyword:"type",params:{type: schema100.properties.expected_revision.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(!(formats0.validate(data0))){
const err4 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
}
if(data.cut !== undefined){
let data1 = data.cut;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err5 = {instancePath:instancePath+"/cut",schemaPath:"#/properties/cut/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate99.errors = vErrors;
return errors === 0;
}

export const RewindResult = validate100;
const schema101 = {"type":"object","properties":{"cut":{"type":"integer"},"restored_files":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/RewindResult","$schema":"http://json-schema.org/draft-07/schema#","title":"RewindResult","required":["cut","restored_files"],"additionalProperties":true};

function validate100(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RewindResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.cut === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cut"},message:"must have required property '"+"cut"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.restored_files === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "restored_files"},message:"must have required property '"+"restored_files"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.cut !== undefined){
let data0 = data.cut;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err2 = {instancePath:instancePath+"/cut",schemaPath:"#/properties/cut/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.restored_files !== undefined){
let data1 = data.restored_files;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err3 = {instancePath:instancePath+"/restored_files",schemaPath:"#/properties/restored_files/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate100.errors = vErrors;
return errors === 0;
}

export const RootCollectionPage = validate101;
const schema102 = {"type":"object","properties":{"root_id":{"type":"string"},"collection":{"type":"string"},"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"event_cursor":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"items":{"type":["null","array"],"items":{"type":"object","properties":{"agent":{"type":["null","object"],"properties":{"id":{"type":"string"},"root_id":{"type":"string"},"parent_id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"effort":{"type":"string"},"cwd":{"type":"string"},"report":{"type":"string"},"status":{"type":"string"},"pending_mail":{"type":"integer"},"lifecycle_phase":{"type":"string"},"blocking_reason":{"type":"string"},"terminal_cause":{"type":"string"},"allowed_controls":{"type":["null","array"],"items":{"type":"string"}}},"required":["id","root_id","parent_id","name","model","provider","effort","cwd","report","status","pending_mail","lifecycle_phase","blocking_reason","terminal_cause","allowed_controls"],"additionalProperties":true},"inbox":{"type":["null","object"],"properties":{"root_id":{"type":"string"},"agent_id":{"type":"string"},"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"status":{"type":"string"},"payload":{"type":"object","properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["root_id","agent_id","seq","kind","status","payload"],"additionalProperties":true},"blackboard":{"type":["null","object"],"properties":{"key":{"type":"string"},"version":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"author_agent_id":{"type":"string"},"payload":{"type":"object","properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["key","version","author_agent_id","payload"],"additionalProperties":true},"budget":{"type":["null","object"],"properties":{"agent_id":{"type":"string"},"state":{"type":"object","properties":{"kind":{"type":"string"},"limit":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"used":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"reserved":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"remaining":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["kind","limit","used","reserved","remaining"],"additionalProperties":true}},"required":["agent_id","state"],"additionalProperties":true},"capability":{"type":["null","object"],"properties":{"id":{"type":"string"},"root_id":{"type":"string"},"agent_id":{"type":"string"},"issuer_agent_id":{"type":"string"},"operations":{"type":["null","array"],"items":{"type":"string"}},"scopes":{"type":["null","array"],"items":{"type":"string"}},"mcp":{"type":["null","array"],"items":{"type":"object","properties":{"server":{"type":"string"},"tool":{"type":"string"},"definition":{"type":"string"}},"required":["server","tool","definition"],"additionalProperties":true}},"mcp_all":{"type":"boolean"},"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"},"expires_at":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"}},"required":["id","root_id","agent_id","issuer_agent_id","operations","scopes","mcp","mcp_all","generation","status","expires_at","created_at","updated_at"],"additionalProperties":true},"schedule":{"type":["null","object"],"properties":{"id":{"type":"integer"},"schedule":{"type":"string"},"prompt":{"type":"string"},"anchor":{"type":"string"},"last_fire":{"type":"string"}},"required":["id","schedule","prompt","anchor","last_fire"],"additionalProperties":true},"permission":{"type":["null","object"],"properties":{"id":{"type":"string"},"agent_id":{"type":"string"},"operation_id":{"type":"string"},"operation":{"type":"string"},"canonical_path":{"type":"string"},"request_digest":{"type":"string"},"capability_id":{"type":"string"},"capability_generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"},"command":{"type":"string"},"rule":{"type":"string"}},"required":["id","agent_id","operation_id","operation","canonical_path","request_digest","capability_id","capability_generation","status","command","rule"],"additionalProperties":true},"body":{"type":["null","object"],"properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"additionalProperties":true,"oneOf":[{"required":["agent"]},{"required":["inbox"]},{"required":["blackboard"]},{"required":["budget"]},{"required":["capability"]},{"required":["schedule"]},{"required":["permission"]},{"required":["body"]}]}},"next_cursor":{"type":["null","object"],"properties":{"root_id":{"type":"string"},"collection":{"type":"string"},"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"offset":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["root_id","collection","revision","offset"],"additionalProperties":true},"has_more":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/RootCollectionPage","$schema":"http://json-schema.org/draft-07/schema#","title":"RootCollectionPage","required":["root_id","collection","revision","event_cursor","items","has_more"],"additionalProperties":true};

function validate101(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RootCollectionPage" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.collection === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "collection"},message:"must have required property '"+"collection"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.revision === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.event_cursor === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "event_cursor"},message:"must have required property '"+"event_cursor"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.items === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "items"},message:"must have required property '"+"items"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.has_more === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "has_more"},message:"must have required property '"+"has_more"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err6 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.collection !== undefined){
if(typeof data.collection !== "string"){
const err7 = {instancePath:instancePath+"/collection",schemaPath:"#/properties/collection/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.revision !== undefined){
let data2 = data.revision;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err8 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(!(formats0.validate(data2))){
const err9 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
else {
const err10 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.event_cursor !== undefined){
let data3 = data.event_cursor;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err11 = {instancePath:instancePath+"/event_cursor",schemaPath:"#/properties/event_cursor/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(!(formats0.validate(data3))){
const err12 = {instancePath:instancePath+"/event_cursor",schemaPath:"#/properties/event_cursor/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
else {
const err13 = {instancePath:instancePath+"/event_cursor",schemaPath:"#/properties/event_cursor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.items !== undefined){
let data4 = data.items;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err14 = {instancePath:instancePath+"/items",schemaPath:"#/properties/items/type",keyword:"type",params:{type: schema102.properties.items.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(Array.isArray(data4)){
const len0 = data4.length;
for(let i0=0; i0<len0; i0++){
let data5 = data4[i0];
const _errs14 = errors;
let valid3 = false;
let passing0 = null;
const _errs15 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.agent === undefined){
const err15 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/0/required",keyword:"required",params:{missingProperty: "agent"},message:"must have required property '"+"agent"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
var _valid0 = _errs15 === errors;
if(_valid0){
valid3 = true;
passing0 = 0;
}
const _errs16 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.inbox === undefined){
const err16 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/1/required",keyword:"required",params:{missingProperty: "inbox"},message:"must have required property '"+"inbox"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
var _valid0 = _errs16 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 1];
}
else {
if(_valid0){
valid3 = true;
passing0 = 1;
}
const _errs17 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.blackboard === undefined){
const err17 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/2/required",keyword:"required",params:{missingProperty: "blackboard"},message:"must have required property '"+"blackboard"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
var _valid0 = _errs17 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 2];
}
else {
if(_valid0){
valid3 = true;
passing0 = 2;
}
const _errs18 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.budget === undefined){
const err18 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/3/required",keyword:"required",params:{missingProperty: "budget"},message:"must have required property '"+"budget"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
var _valid0 = _errs18 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 3];
}
else {
if(_valid0){
valid3 = true;
passing0 = 3;
}
const _errs19 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.capability === undefined){
const err19 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/4/required",keyword:"required",params:{missingProperty: "capability"},message:"must have required property '"+"capability"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
var _valid0 = _errs19 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 4];
}
else {
if(_valid0){
valid3 = true;
passing0 = 4;
}
const _errs20 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.schedule === undefined){
const err20 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/5/required",keyword:"required",params:{missingProperty: "schedule"},message:"must have required property '"+"schedule"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
var _valid0 = _errs20 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 5];
}
else {
if(_valid0){
valid3 = true;
passing0 = 5;
}
const _errs21 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.permission === undefined){
const err21 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/6/required",keyword:"required",params:{missingProperty: "permission"},message:"must have required property '"+"permission"+"'"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
var _valid0 = _errs21 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 6];
}
else {
if(_valid0){
valid3 = true;
passing0 = 6;
}
const _errs22 = errors;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.body === undefined){
const err22 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf/7/required",keyword:"required",params:{missingProperty: "body"},message:"must have required property '"+"body"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
var _valid0 = _errs22 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 7];
}
else {
if(_valid0){
valid3 = true;
passing0 = 7;
}
}
}
}
}
}
}
}
if(!valid3){
const err23 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/oneOf",keyword:"oneOf",params:{passingSchemas: passing0},message:"must match exactly one schema in oneOf"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
else {
errors = _errs14;
if(vErrors !== null){
if(_errs14){
vErrors.length = _errs14;
}
else {
vErrors = null;
}
}
}
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.agent !== undefined){
let data6 = data5.agent;
if((data6 !== null) && (!(data6 && typeof data6 == "object" && !Array.isArray(data6)))){
const err24 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/type",keyword:"type",params:{type: schema102.properties.items.items.properties.agent.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.id === undefined){
const err25 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data6.root_id === undefined){
const err26 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(data6.parent_id === undefined){
const err27 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data6.name === undefined){
const err28 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data6.model === undefined){
const err29 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data6.provider === undefined){
const err30 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data6.effort === undefined){
const err31 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data6.cwd === undefined){
const err32 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(data6.report === undefined){
const err33 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "report"},message:"must have required property '"+"report"+"'"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data6.status === undefined){
const err34 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(data6.pending_mail === undefined){
const err35 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "pending_mail"},message:"must have required property '"+"pending_mail"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(data6.lifecycle_phase === undefined){
const err36 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "lifecycle_phase"},message:"must have required property '"+"lifecycle_phase"+"'"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(data6.blocking_reason === undefined){
const err37 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "blocking_reason"},message:"must have required property '"+"blocking_reason"+"'"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
if(data6.terminal_cause === undefined){
const err38 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "terminal_cause"},message:"must have required property '"+"terminal_cause"+"'"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(data6.allowed_controls === undefined){
const err39 = {instancePath:instancePath+"/items/" + i0+"/agent",schemaPath:"#/properties/items/items/properties/agent/required",keyword:"required",params:{missingProperty: "allowed_controls"},message:"must have required property '"+"allowed_controls"+"'"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
if(data6.id !== undefined){
if(typeof data6.id !== "string"){
const err40 = {instancePath:instancePath+"/items/" + i0+"/agent/id",schemaPath:"#/properties/items/items/properties/agent/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data6.root_id !== undefined){
if(typeof data6.root_id !== "string"){
const err41 = {instancePath:instancePath+"/items/" + i0+"/agent/root_id",schemaPath:"#/properties/items/items/properties/agent/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
if(data6.parent_id !== undefined){
if(typeof data6.parent_id !== "string"){
const err42 = {instancePath:instancePath+"/items/" + i0+"/agent/parent_id",schemaPath:"#/properties/items/items/properties/agent/properties/parent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
if(data6.name !== undefined){
if(typeof data6.name !== "string"){
const err43 = {instancePath:instancePath+"/items/" + i0+"/agent/name",schemaPath:"#/properties/items/items/properties/agent/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
}
if(data6.model !== undefined){
if(typeof data6.model !== "string"){
const err44 = {instancePath:instancePath+"/items/" + i0+"/agent/model",schemaPath:"#/properties/items/items/properties/agent/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data6.provider !== undefined){
if(typeof data6.provider !== "string"){
const err45 = {instancePath:instancePath+"/items/" + i0+"/agent/provider",schemaPath:"#/properties/items/items/properties/agent/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
if(data6.effort !== undefined){
if(typeof data6.effort !== "string"){
const err46 = {instancePath:instancePath+"/items/" + i0+"/agent/effort",schemaPath:"#/properties/items/items/properties/agent/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data6.cwd !== undefined){
if(typeof data6.cwd !== "string"){
const err47 = {instancePath:instancePath+"/items/" + i0+"/agent/cwd",schemaPath:"#/properties/items/items/properties/agent/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data6.report !== undefined){
if(typeof data6.report !== "string"){
const err48 = {instancePath:instancePath+"/items/" + i0+"/agent/report",schemaPath:"#/properties/items/items/properties/agent/properties/report/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data6.status !== undefined){
if(typeof data6.status !== "string"){
const err49 = {instancePath:instancePath+"/items/" + i0+"/agent/status",schemaPath:"#/properties/items/items/properties/agent/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
if(data6.pending_mail !== undefined){
let data17 = data6.pending_mail;
if(!((typeof data17 == "number") && (!(data17 % 1) && !isNaN(data17)))){
const err50 = {instancePath:instancePath+"/items/" + i0+"/agent/pending_mail",schemaPath:"#/properties/items/items/properties/agent/properties/pending_mail/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data6.lifecycle_phase !== undefined){
if(typeof data6.lifecycle_phase !== "string"){
const err51 = {instancePath:instancePath+"/items/" + i0+"/agent/lifecycle_phase",schemaPath:"#/properties/items/items/properties/agent/properties/lifecycle_phase/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
if(data6.blocking_reason !== undefined){
if(typeof data6.blocking_reason !== "string"){
const err52 = {instancePath:instancePath+"/items/" + i0+"/agent/blocking_reason",schemaPath:"#/properties/items/items/properties/agent/properties/blocking_reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data6.terminal_cause !== undefined){
if(typeof data6.terminal_cause !== "string"){
const err53 = {instancePath:instancePath+"/items/" + i0+"/agent/terminal_cause",schemaPath:"#/properties/items/items/properties/agent/properties/terminal_cause/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
if(data6.allowed_controls !== undefined){
let data21 = data6.allowed_controls;
if((data21 !== null) && (!(Array.isArray(data21)))){
const err54 = {instancePath:instancePath+"/items/" + i0+"/agent/allowed_controls",schemaPath:"#/properties/items/items/properties/agent/properties/allowed_controls/type",keyword:"type",params:{type: schema102.properties.items.items.properties.agent.properties.allowed_controls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
if(Array.isArray(data21)){
const len1 = data21.length;
for(let i1=0; i1<len1; i1++){
if(typeof data21[i1] !== "string"){
const err55 = {instancePath:instancePath+"/items/" + i0+"/agent/allowed_controls/" + i1,schemaPath:"#/properties/items/items/properties/agent/properties/allowed_controls/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
}
}
}
}
}
if(data5.inbox !== undefined){
let data23 = data5.inbox;
if((data23 !== null) && (!(data23 && typeof data23 == "object" && !Array.isArray(data23)))){
const err56 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/type",keyword:"type",params:{type: schema102.properties.items.items.properties.inbox.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
if(data23 && typeof data23 == "object" && !Array.isArray(data23)){
if(data23.root_id === undefined){
const err57 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
if(data23.agent_id === undefined){
const err58 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
if(data23.seq === undefined){
const err59 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
if(data23.kind === undefined){
const err60 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(data23.status === undefined){
const err61 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
if(data23.payload === undefined){
const err62 = {instancePath:instancePath+"/items/" + i0+"/inbox",schemaPath:"#/properties/items/items/properties/inbox/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
if(data23.root_id !== undefined){
if(typeof data23.root_id !== "string"){
const err63 = {instancePath:instancePath+"/items/" + i0+"/inbox/root_id",schemaPath:"#/properties/items/items/properties/inbox/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
}
if(data23.agent_id !== undefined){
if(typeof data23.agent_id !== "string"){
const err64 = {instancePath:instancePath+"/items/" + i0+"/inbox/agent_id",schemaPath:"#/properties/items/items/properties/inbox/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data23.seq !== undefined){
let data26 = data23.seq;
if(typeof data26 === "string"){
if(!pattern0.test(data26)){
const err65 = {instancePath:instancePath+"/items/" + i0+"/inbox/seq",schemaPath:"#/properties/items/items/properties/inbox/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
if(!(formats0.validate(data26))){
const err66 = {instancePath:instancePath+"/items/" + i0+"/inbox/seq",schemaPath:"#/properties/items/items/properties/inbox/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
}
else {
const err67 = {instancePath:instancePath+"/items/" + i0+"/inbox/seq",schemaPath:"#/properties/items/items/properties/inbox/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
}
if(data23.kind !== undefined){
if(typeof data23.kind !== "string"){
const err68 = {instancePath:instancePath+"/items/" + i0+"/inbox/kind",schemaPath:"#/properties/items/items/properties/inbox/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
}
if(data23.status !== undefined){
if(typeof data23.status !== "string"){
const err69 = {instancePath:instancePath+"/items/" + i0+"/inbox/status",schemaPath:"#/properties/items/items/properties/inbox/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
}
if(data23.payload !== undefined){
let data29 = data23.payload;
if(data29 && typeof data29 == "object" && !Array.isArray(data29)){
if(data29.reference_id === undefined){
const err70 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
if(data29.digest === undefined){
const err71 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
if(data29.size === undefined){
const err72 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
if(data29.media_type === undefined){
const err73 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
if(data29.source === undefined){
const err74 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
if(data29.text !== undefined){
let data30 = data29.text;
if((data30 !== null) && (typeof data30 !== "string")){
const err75 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/text",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/text/type",keyword:"type",params:{type: schema102.properties.items.items.properties.inbox.properties.payload.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
if(data29.binary !== undefined){
let data31 = data29.binary;
if((typeof data31 !== "string") && (data31 !== null)){
const err76 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/binary",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/binary/type",keyword:"type",params:{type: schema102.properties.items.items.properties.inbox.properties.payload.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
}
if(data29.reference_id !== undefined){
if(typeof data29.reference_id !== "string"){
const err77 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/reference_id",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
}
if(data29.digest !== undefined){
if(typeof data29.digest !== "string"){
const err78 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/digest",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
}
if(data29.size !== undefined){
let data34 = data29.size;
if(typeof data34 === "string"){
if(!pattern0.test(data34)){
const err79 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/size",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
if(!(formats0.validate(data34))){
const err80 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/size",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
}
else {
const err81 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/size",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
}
if(data29.media_type !== undefined){
if(typeof data29.media_type !== "string"){
const err82 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/media_type",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
if(data29.source !== undefined){
if(typeof data29.source !== "string"){
const err83 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload/source",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
}
}
else {
const err84 = {instancePath:instancePath+"/items/" + i0+"/inbox/payload",schemaPath:"#/properties/items/items/properties/inbox/properties/payload/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
}
}
}
if(data5.blackboard !== undefined){
let data37 = data5.blackboard;
if((data37 !== null) && (!(data37 && typeof data37 == "object" && !Array.isArray(data37)))){
const err85 = {instancePath:instancePath+"/items/" + i0+"/blackboard",schemaPath:"#/properties/items/items/properties/blackboard/type",keyword:"type",params:{type: schema102.properties.items.items.properties.blackboard.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err85];
}
else {
vErrors.push(err85);
}
errors++;
}
if(data37 && typeof data37 == "object" && !Array.isArray(data37)){
if(data37.key === undefined){
const err86 = {instancePath:instancePath+"/items/" + i0+"/blackboard",schemaPath:"#/properties/items/items/properties/blackboard/required",keyword:"required",params:{missingProperty: "key"},message:"must have required property '"+"key"+"'"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
if(data37.version === undefined){
const err87 = {instancePath:instancePath+"/items/" + i0+"/blackboard",schemaPath:"#/properties/items/items/properties/blackboard/required",keyword:"required",params:{missingProperty: "version"},message:"must have required property '"+"version"+"'"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
if(data37.author_agent_id === undefined){
const err88 = {instancePath:instancePath+"/items/" + i0+"/blackboard",schemaPath:"#/properties/items/items/properties/blackboard/required",keyword:"required",params:{missingProperty: "author_agent_id"},message:"must have required property '"+"author_agent_id"+"'"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
if(data37.payload === undefined){
const err89 = {instancePath:instancePath+"/items/" + i0+"/blackboard",schemaPath:"#/properties/items/items/properties/blackboard/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
if(data37.key !== undefined){
if(typeof data37.key !== "string"){
const err90 = {instancePath:instancePath+"/items/" + i0+"/blackboard/key",schemaPath:"#/properties/items/items/properties/blackboard/properties/key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
}
if(data37.version !== undefined){
let data39 = data37.version;
if(typeof data39 === "string"){
if(!pattern0.test(data39)){
const err91 = {instancePath:instancePath+"/items/" + i0+"/blackboard/version",schemaPath:"#/properties/items/items/properties/blackboard/properties/version/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err91];
}
else {
vErrors.push(err91);
}
errors++;
}
if(!(formats0.validate(data39))){
const err92 = {instancePath:instancePath+"/items/" + i0+"/blackboard/version",schemaPath:"#/properties/items/items/properties/blackboard/properties/version/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err92];
}
else {
vErrors.push(err92);
}
errors++;
}
}
else {
const err93 = {instancePath:instancePath+"/items/" + i0+"/blackboard/version",schemaPath:"#/properties/items/items/properties/blackboard/properties/version/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err93];
}
else {
vErrors.push(err93);
}
errors++;
}
}
if(data37.author_agent_id !== undefined){
if(typeof data37.author_agent_id !== "string"){
const err94 = {instancePath:instancePath+"/items/" + i0+"/blackboard/author_agent_id",schemaPath:"#/properties/items/items/properties/blackboard/properties/author_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err94];
}
else {
vErrors.push(err94);
}
errors++;
}
}
if(data37.payload !== undefined){
let data41 = data37.payload;
if(data41 && typeof data41 == "object" && !Array.isArray(data41)){
if(data41.reference_id === undefined){
const err95 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err95];
}
else {
vErrors.push(err95);
}
errors++;
}
if(data41.digest === undefined){
const err96 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err96];
}
else {
vErrors.push(err96);
}
errors++;
}
if(data41.size === undefined){
const err97 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err97];
}
else {
vErrors.push(err97);
}
errors++;
}
if(data41.media_type === undefined){
const err98 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err98];
}
else {
vErrors.push(err98);
}
errors++;
}
if(data41.source === undefined){
const err99 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err99];
}
else {
vErrors.push(err99);
}
errors++;
}
if(data41.text !== undefined){
let data42 = data41.text;
if((data42 !== null) && (typeof data42 !== "string")){
const err100 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/text",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/text/type",keyword:"type",params:{type: schema102.properties.items.items.properties.blackboard.properties.payload.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err100];
}
else {
vErrors.push(err100);
}
errors++;
}
}
if(data41.binary !== undefined){
let data43 = data41.binary;
if((typeof data43 !== "string") && (data43 !== null)){
const err101 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/binary",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/binary/type",keyword:"type",params:{type: schema102.properties.items.items.properties.blackboard.properties.payload.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err101];
}
else {
vErrors.push(err101);
}
errors++;
}
}
if(data41.reference_id !== undefined){
if(typeof data41.reference_id !== "string"){
const err102 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/reference_id",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err102];
}
else {
vErrors.push(err102);
}
errors++;
}
}
if(data41.digest !== undefined){
if(typeof data41.digest !== "string"){
const err103 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/digest",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err103];
}
else {
vErrors.push(err103);
}
errors++;
}
}
if(data41.size !== undefined){
let data46 = data41.size;
if(typeof data46 === "string"){
if(!pattern0.test(data46)){
const err104 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/size",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err104];
}
else {
vErrors.push(err104);
}
errors++;
}
if(!(formats0.validate(data46))){
const err105 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/size",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err105];
}
else {
vErrors.push(err105);
}
errors++;
}
}
else {
const err106 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/size",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err106];
}
else {
vErrors.push(err106);
}
errors++;
}
}
if(data41.media_type !== undefined){
if(typeof data41.media_type !== "string"){
const err107 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/media_type",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err107];
}
else {
vErrors.push(err107);
}
errors++;
}
}
if(data41.source !== undefined){
if(typeof data41.source !== "string"){
const err108 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload/source",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err108];
}
else {
vErrors.push(err108);
}
errors++;
}
}
}
else {
const err109 = {instancePath:instancePath+"/items/" + i0+"/blackboard/payload",schemaPath:"#/properties/items/items/properties/blackboard/properties/payload/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err109];
}
else {
vErrors.push(err109);
}
errors++;
}
}
}
}
if(data5.budget !== undefined){
let data49 = data5.budget;
if((data49 !== null) && (!(data49 && typeof data49 == "object" && !Array.isArray(data49)))){
const err110 = {instancePath:instancePath+"/items/" + i0+"/budget",schemaPath:"#/properties/items/items/properties/budget/type",keyword:"type",params:{type: schema102.properties.items.items.properties.budget.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err110];
}
else {
vErrors.push(err110);
}
errors++;
}
if(data49 && typeof data49 == "object" && !Array.isArray(data49)){
if(data49.agent_id === undefined){
const err111 = {instancePath:instancePath+"/items/" + i0+"/budget",schemaPath:"#/properties/items/items/properties/budget/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err111];
}
else {
vErrors.push(err111);
}
errors++;
}
if(data49.state === undefined){
const err112 = {instancePath:instancePath+"/items/" + i0+"/budget",schemaPath:"#/properties/items/items/properties/budget/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err112];
}
else {
vErrors.push(err112);
}
errors++;
}
if(data49.agent_id !== undefined){
if(typeof data49.agent_id !== "string"){
const err113 = {instancePath:instancePath+"/items/" + i0+"/budget/agent_id",schemaPath:"#/properties/items/items/properties/budget/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err113];
}
else {
vErrors.push(err113);
}
errors++;
}
}
if(data49.state !== undefined){
let data51 = data49.state;
if(data51 && typeof data51 == "object" && !Array.isArray(data51)){
if(data51.kind === undefined){
const err114 = {instancePath:instancePath+"/items/" + i0+"/budget/state",schemaPath:"#/properties/items/items/properties/budget/properties/state/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err114];
}
else {
vErrors.push(err114);
}
errors++;
}
if(data51.limit === undefined){
const err115 = {instancePath:instancePath+"/items/" + i0+"/budget/state",schemaPath:"#/properties/items/items/properties/budget/properties/state/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err115];
}
else {
vErrors.push(err115);
}
errors++;
}
if(data51.used === undefined){
const err116 = {instancePath:instancePath+"/items/" + i0+"/budget/state",schemaPath:"#/properties/items/items/properties/budget/properties/state/required",keyword:"required",params:{missingProperty: "used"},message:"must have required property '"+"used"+"'"};
if(vErrors === null){
vErrors = [err116];
}
else {
vErrors.push(err116);
}
errors++;
}
if(data51.reserved === undefined){
const err117 = {instancePath:instancePath+"/items/" + i0+"/budget/state",schemaPath:"#/properties/items/items/properties/budget/properties/state/required",keyword:"required",params:{missingProperty: "reserved"},message:"must have required property '"+"reserved"+"'"};
if(vErrors === null){
vErrors = [err117];
}
else {
vErrors.push(err117);
}
errors++;
}
if(data51.remaining === undefined){
const err118 = {instancePath:instancePath+"/items/" + i0+"/budget/state",schemaPath:"#/properties/items/items/properties/budget/properties/state/required",keyword:"required",params:{missingProperty: "remaining"},message:"must have required property '"+"remaining"+"'"};
if(vErrors === null){
vErrors = [err118];
}
else {
vErrors.push(err118);
}
errors++;
}
if(data51.kind !== undefined){
if(typeof data51.kind !== "string"){
const err119 = {instancePath:instancePath+"/items/" + i0+"/budget/state/kind",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err119];
}
else {
vErrors.push(err119);
}
errors++;
}
}
if(data51.limit !== undefined){
let data53 = data51.limit;
if(typeof data53 === "string"){
if(!pattern0.test(data53)){
const err120 = {instancePath:instancePath+"/items/" + i0+"/budget/state/limit",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/limit/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err120];
}
else {
vErrors.push(err120);
}
errors++;
}
if(!(formats0.validate(data53))){
const err121 = {instancePath:instancePath+"/items/" + i0+"/budget/state/limit",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/limit/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err121];
}
else {
vErrors.push(err121);
}
errors++;
}
}
else {
const err122 = {instancePath:instancePath+"/items/" + i0+"/budget/state/limit",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/limit/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err122];
}
else {
vErrors.push(err122);
}
errors++;
}
}
if(data51.used !== undefined){
let data54 = data51.used;
if(typeof data54 === "string"){
if(!pattern0.test(data54)){
const err123 = {instancePath:instancePath+"/items/" + i0+"/budget/state/used",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/used/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err123];
}
else {
vErrors.push(err123);
}
errors++;
}
if(!(formats0.validate(data54))){
const err124 = {instancePath:instancePath+"/items/" + i0+"/budget/state/used",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/used/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err124];
}
else {
vErrors.push(err124);
}
errors++;
}
}
else {
const err125 = {instancePath:instancePath+"/items/" + i0+"/budget/state/used",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/used/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err125];
}
else {
vErrors.push(err125);
}
errors++;
}
}
if(data51.reserved !== undefined){
let data55 = data51.reserved;
if(typeof data55 === "string"){
if(!pattern0.test(data55)){
const err126 = {instancePath:instancePath+"/items/" + i0+"/budget/state/reserved",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/reserved/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err126];
}
else {
vErrors.push(err126);
}
errors++;
}
if(!(formats0.validate(data55))){
const err127 = {instancePath:instancePath+"/items/" + i0+"/budget/state/reserved",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/reserved/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err127];
}
else {
vErrors.push(err127);
}
errors++;
}
}
else {
const err128 = {instancePath:instancePath+"/items/" + i0+"/budget/state/reserved",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/reserved/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err128];
}
else {
vErrors.push(err128);
}
errors++;
}
}
if(data51.remaining !== undefined){
let data56 = data51.remaining;
if(typeof data56 === "string"){
if(!pattern0.test(data56)){
const err129 = {instancePath:instancePath+"/items/" + i0+"/budget/state/remaining",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/remaining/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err129];
}
else {
vErrors.push(err129);
}
errors++;
}
if(!(formats0.validate(data56))){
const err130 = {instancePath:instancePath+"/items/" + i0+"/budget/state/remaining",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/remaining/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err130];
}
else {
vErrors.push(err130);
}
errors++;
}
}
else {
const err131 = {instancePath:instancePath+"/items/" + i0+"/budget/state/remaining",schemaPath:"#/properties/items/items/properties/budget/properties/state/properties/remaining/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err131];
}
else {
vErrors.push(err131);
}
errors++;
}
}
}
else {
const err132 = {instancePath:instancePath+"/items/" + i0+"/budget/state",schemaPath:"#/properties/items/items/properties/budget/properties/state/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err132];
}
else {
vErrors.push(err132);
}
errors++;
}
}
}
}
if(data5.capability !== undefined){
let data57 = data5.capability;
if((data57 !== null) && (!(data57 && typeof data57 == "object" && !Array.isArray(data57)))){
const err133 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/type",keyword:"type",params:{type: schema102.properties.items.items.properties.capability.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err133];
}
else {
vErrors.push(err133);
}
errors++;
}
if(data57 && typeof data57 == "object" && !Array.isArray(data57)){
if(data57.id === undefined){
const err134 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err134];
}
else {
vErrors.push(err134);
}
errors++;
}
if(data57.root_id === undefined){
const err135 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err135];
}
else {
vErrors.push(err135);
}
errors++;
}
if(data57.agent_id === undefined){
const err136 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err136];
}
else {
vErrors.push(err136);
}
errors++;
}
if(data57.issuer_agent_id === undefined){
const err137 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "issuer_agent_id"},message:"must have required property '"+"issuer_agent_id"+"'"};
if(vErrors === null){
vErrors = [err137];
}
else {
vErrors.push(err137);
}
errors++;
}
if(data57.operations === undefined){
const err138 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err138];
}
else {
vErrors.push(err138);
}
errors++;
}
if(data57.scopes === undefined){
const err139 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "scopes"},message:"must have required property '"+"scopes"+"'"};
if(vErrors === null){
vErrors = [err139];
}
else {
vErrors.push(err139);
}
errors++;
}
if(data57.mcp === undefined){
const err140 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "mcp"},message:"must have required property '"+"mcp"+"'"};
if(vErrors === null){
vErrors = [err140];
}
else {
vErrors.push(err140);
}
errors++;
}
if(data57.mcp_all === undefined){
const err141 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "mcp_all"},message:"must have required property '"+"mcp_all"+"'"};
if(vErrors === null){
vErrors = [err141];
}
else {
vErrors.push(err141);
}
errors++;
}
if(data57.generation === undefined){
const err142 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err142];
}
else {
vErrors.push(err142);
}
errors++;
}
if(data57.status === undefined){
const err143 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err143];
}
else {
vErrors.push(err143);
}
errors++;
}
if(data57.expires_at === undefined){
const err144 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "expires_at"},message:"must have required property '"+"expires_at"+"'"};
if(vErrors === null){
vErrors = [err144];
}
else {
vErrors.push(err144);
}
errors++;
}
if(data57.created_at === undefined){
const err145 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err145];
}
else {
vErrors.push(err145);
}
errors++;
}
if(data57.updated_at === undefined){
const err146 = {instancePath:instancePath+"/items/" + i0+"/capability",schemaPath:"#/properties/items/items/properties/capability/required",keyword:"required",params:{missingProperty: "updated_at"},message:"must have required property '"+"updated_at"+"'"};
if(vErrors === null){
vErrors = [err146];
}
else {
vErrors.push(err146);
}
errors++;
}
if(data57.id !== undefined){
if(typeof data57.id !== "string"){
const err147 = {instancePath:instancePath+"/items/" + i0+"/capability/id",schemaPath:"#/properties/items/items/properties/capability/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err147];
}
else {
vErrors.push(err147);
}
errors++;
}
}
if(data57.root_id !== undefined){
if(typeof data57.root_id !== "string"){
const err148 = {instancePath:instancePath+"/items/" + i0+"/capability/root_id",schemaPath:"#/properties/items/items/properties/capability/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err148];
}
else {
vErrors.push(err148);
}
errors++;
}
}
if(data57.agent_id !== undefined){
if(typeof data57.agent_id !== "string"){
const err149 = {instancePath:instancePath+"/items/" + i0+"/capability/agent_id",schemaPath:"#/properties/items/items/properties/capability/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err149];
}
else {
vErrors.push(err149);
}
errors++;
}
}
if(data57.issuer_agent_id !== undefined){
if(typeof data57.issuer_agent_id !== "string"){
const err150 = {instancePath:instancePath+"/items/" + i0+"/capability/issuer_agent_id",schemaPath:"#/properties/items/items/properties/capability/properties/issuer_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err150];
}
else {
vErrors.push(err150);
}
errors++;
}
}
if(data57.operations !== undefined){
let data62 = data57.operations;
if((data62 !== null) && (!(Array.isArray(data62)))){
const err151 = {instancePath:instancePath+"/items/" + i0+"/capability/operations",schemaPath:"#/properties/items/items/properties/capability/properties/operations/type",keyword:"type",params:{type: schema102.properties.items.items.properties.capability.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err151];
}
else {
vErrors.push(err151);
}
errors++;
}
if(Array.isArray(data62)){
const len2 = data62.length;
for(let i2=0; i2<len2; i2++){
if(typeof data62[i2] !== "string"){
const err152 = {instancePath:instancePath+"/items/" + i0+"/capability/operations/" + i2,schemaPath:"#/properties/items/items/properties/capability/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err152];
}
else {
vErrors.push(err152);
}
errors++;
}
}
}
}
if(data57.scopes !== undefined){
let data64 = data57.scopes;
if((data64 !== null) && (!(Array.isArray(data64)))){
const err153 = {instancePath:instancePath+"/items/" + i0+"/capability/scopes",schemaPath:"#/properties/items/items/properties/capability/properties/scopes/type",keyword:"type",params:{type: schema102.properties.items.items.properties.capability.properties.scopes.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err153];
}
else {
vErrors.push(err153);
}
errors++;
}
if(Array.isArray(data64)){
const len3 = data64.length;
for(let i3=0; i3<len3; i3++){
if(typeof data64[i3] !== "string"){
const err154 = {instancePath:instancePath+"/items/" + i0+"/capability/scopes/" + i3,schemaPath:"#/properties/items/items/properties/capability/properties/scopes/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err154];
}
else {
vErrors.push(err154);
}
errors++;
}
}
}
}
if(data57.mcp !== undefined){
let data66 = data57.mcp;
if((data66 !== null) && (!(Array.isArray(data66)))){
const err155 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp",schemaPath:"#/properties/items/items/properties/capability/properties/mcp/type",keyword:"type",params:{type: schema102.properties.items.items.properties.capability.properties.mcp.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err155];
}
else {
vErrors.push(err155);
}
errors++;
}
if(Array.isArray(data66)){
const len4 = data66.length;
for(let i4=0; i4<len4; i4++){
let data67 = data66[i4];
if(data67 && typeof data67 == "object" && !Array.isArray(data67)){
if(data67.server === undefined){
const err156 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4,schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/required",keyword:"required",params:{missingProperty: "server"},message:"must have required property '"+"server"+"'"};
if(vErrors === null){
vErrors = [err156];
}
else {
vErrors.push(err156);
}
errors++;
}
if(data67.tool === undefined){
const err157 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4,schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/required",keyword:"required",params:{missingProperty: "tool"},message:"must have required property '"+"tool"+"'"};
if(vErrors === null){
vErrors = [err157];
}
else {
vErrors.push(err157);
}
errors++;
}
if(data67.definition === undefined){
const err158 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4,schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err158];
}
else {
vErrors.push(err158);
}
errors++;
}
if(data67.server !== undefined){
if(typeof data67.server !== "string"){
const err159 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4+"/server",schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/properties/server/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err159];
}
else {
vErrors.push(err159);
}
errors++;
}
}
if(data67.tool !== undefined){
if(typeof data67.tool !== "string"){
const err160 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4+"/tool",schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/properties/tool/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err160];
}
else {
vErrors.push(err160);
}
errors++;
}
}
if(data67.definition !== undefined){
if(typeof data67.definition !== "string"){
const err161 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4+"/definition",schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/properties/definition/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err161];
}
else {
vErrors.push(err161);
}
errors++;
}
}
}
else {
const err162 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp/" + i4,schemaPath:"#/properties/items/items/properties/capability/properties/mcp/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err162];
}
else {
vErrors.push(err162);
}
errors++;
}
}
}
}
if(data57.mcp_all !== undefined){
if(typeof data57.mcp_all !== "boolean"){
const err163 = {instancePath:instancePath+"/items/" + i0+"/capability/mcp_all",schemaPath:"#/properties/items/items/properties/capability/properties/mcp_all/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err163];
}
else {
vErrors.push(err163);
}
errors++;
}
}
if(data57.generation !== undefined){
let data72 = data57.generation;
if(typeof data72 === "string"){
if(!pattern0.test(data72)){
const err164 = {instancePath:instancePath+"/items/" + i0+"/capability/generation",schemaPath:"#/properties/items/items/properties/capability/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err164];
}
else {
vErrors.push(err164);
}
errors++;
}
if(!(formats0.validate(data72))){
const err165 = {instancePath:instancePath+"/items/" + i0+"/capability/generation",schemaPath:"#/properties/items/items/properties/capability/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err165];
}
else {
vErrors.push(err165);
}
errors++;
}
}
else {
const err166 = {instancePath:instancePath+"/items/" + i0+"/capability/generation",schemaPath:"#/properties/items/items/properties/capability/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err166];
}
else {
vErrors.push(err166);
}
errors++;
}
}
if(data57.status !== undefined){
if(typeof data57.status !== "string"){
const err167 = {instancePath:instancePath+"/items/" + i0+"/capability/status",schemaPath:"#/properties/items/items/properties/capability/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err167];
}
else {
vErrors.push(err167);
}
errors++;
}
}
if(data57.expires_at !== undefined){
if(typeof data57.expires_at !== "string"){
const err168 = {instancePath:instancePath+"/items/" + i0+"/capability/expires_at",schemaPath:"#/properties/items/items/properties/capability/properties/expires_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err168];
}
else {
vErrors.push(err168);
}
errors++;
}
}
if(data57.created_at !== undefined){
if(typeof data57.created_at !== "string"){
const err169 = {instancePath:instancePath+"/items/" + i0+"/capability/created_at",schemaPath:"#/properties/items/items/properties/capability/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err169];
}
else {
vErrors.push(err169);
}
errors++;
}
}
if(data57.updated_at !== undefined){
if(typeof data57.updated_at !== "string"){
const err170 = {instancePath:instancePath+"/items/" + i0+"/capability/updated_at",schemaPath:"#/properties/items/items/properties/capability/properties/updated_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err170];
}
else {
vErrors.push(err170);
}
errors++;
}
}
}
}
if(data5.schedule !== undefined){
let data77 = data5.schedule;
if((data77 !== null) && (!(data77 && typeof data77 == "object" && !Array.isArray(data77)))){
const err171 = {instancePath:instancePath+"/items/" + i0+"/schedule",schemaPath:"#/properties/items/items/properties/schedule/type",keyword:"type",params:{type: schema102.properties.items.items.properties.schedule.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err171];
}
else {
vErrors.push(err171);
}
errors++;
}
if(data77 && typeof data77 == "object" && !Array.isArray(data77)){
if(data77.id === undefined){
const err172 = {instancePath:instancePath+"/items/" + i0+"/schedule",schemaPath:"#/properties/items/items/properties/schedule/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err172];
}
else {
vErrors.push(err172);
}
errors++;
}
if(data77.schedule === undefined){
const err173 = {instancePath:instancePath+"/items/" + i0+"/schedule",schemaPath:"#/properties/items/items/properties/schedule/required",keyword:"required",params:{missingProperty: "schedule"},message:"must have required property '"+"schedule"+"'"};
if(vErrors === null){
vErrors = [err173];
}
else {
vErrors.push(err173);
}
errors++;
}
if(data77.prompt === undefined){
const err174 = {instancePath:instancePath+"/items/" + i0+"/schedule",schemaPath:"#/properties/items/items/properties/schedule/required",keyword:"required",params:{missingProperty: "prompt"},message:"must have required property '"+"prompt"+"'"};
if(vErrors === null){
vErrors = [err174];
}
else {
vErrors.push(err174);
}
errors++;
}
if(data77.anchor === undefined){
const err175 = {instancePath:instancePath+"/items/" + i0+"/schedule",schemaPath:"#/properties/items/items/properties/schedule/required",keyword:"required",params:{missingProperty: "anchor"},message:"must have required property '"+"anchor"+"'"};
if(vErrors === null){
vErrors = [err175];
}
else {
vErrors.push(err175);
}
errors++;
}
if(data77.last_fire === undefined){
const err176 = {instancePath:instancePath+"/items/" + i0+"/schedule",schemaPath:"#/properties/items/items/properties/schedule/required",keyword:"required",params:{missingProperty: "last_fire"},message:"must have required property '"+"last_fire"+"'"};
if(vErrors === null){
vErrors = [err176];
}
else {
vErrors.push(err176);
}
errors++;
}
if(data77.id !== undefined){
let data78 = data77.id;
if(!((typeof data78 == "number") && (!(data78 % 1) && !isNaN(data78)))){
const err177 = {instancePath:instancePath+"/items/" + i0+"/schedule/id",schemaPath:"#/properties/items/items/properties/schedule/properties/id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err177];
}
else {
vErrors.push(err177);
}
errors++;
}
}
if(data77.schedule !== undefined){
if(typeof data77.schedule !== "string"){
const err178 = {instancePath:instancePath+"/items/" + i0+"/schedule/schedule",schemaPath:"#/properties/items/items/properties/schedule/properties/schedule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err178];
}
else {
vErrors.push(err178);
}
errors++;
}
}
if(data77.prompt !== undefined){
if(typeof data77.prompt !== "string"){
const err179 = {instancePath:instancePath+"/items/" + i0+"/schedule/prompt",schemaPath:"#/properties/items/items/properties/schedule/properties/prompt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err179];
}
else {
vErrors.push(err179);
}
errors++;
}
}
if(data77.anchor !== undefined){
if(typeof data77.anchor !== "string"){
const err180 = {instancePath:instancePath+"/items/" + i0+"/schedule/anchor",schemaPath:"#/properties/items/items/properties/schedule/properties/anchor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err180];
}
else {
vErrors.push(err180);
}
errors++;
}
}
if(data77.last_fire !== undefined){
if(typeof data77.last_fire !== "string"){
const err181 = {instancePath:instancePath+"/items/" + i0+"/schedule/last_fire",schemaPath:"#/properties/items/items/properties/schedule/properties/last_fire/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err181];
}
else {
vErrors.push(err181);
}
errors++;
}
}
}
}
if(data5.permission !== undefined){
let data83 = data5.permission;
if((data83 !== null) && (!(data83 && typeof data83 == "object" && !Array.isArray(data83)))){
const err182 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/type",keyword:"type",params:{type: schema102.properties.items.items.properties.permission.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err182];
}
else {
vErrors.push(err182);
}
errors++;
}
if(data83 && typeof data83 == "object" && !Array.isArray(data83)){
if(data83.id === undefined){
const err183 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err183];
}
else {
vErrors.push(err183);
}
errors++;
}
if(data83.agent_id === undefined){
const err184 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err184];
}
else {
vErrors.push(err184);
}
errors++;
}
if(data83.operation_id === undefined){
const err185 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "operation_id"},message:"must have required property '"+"operation_id"+"'"};
if(vErrors === null){
vErrors = [err185];
}
else {
vErrors.push(err185);
}
errors++;
}
if(data83.operation === undefined){
const err186 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err186];
}
else {
vErrors.push(err186);
}
errors++;
}
if(data83.canonical_path === undefined){
const err187 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "canonical_path"},message:"must have required property '"+"canonical_path"+"'"};
if(vErrors === null){
vErrors = [err187];
}
else {
vErrors.push(err187);
}
errors++;
}
if(data83.request_digest === undefined){
const err188 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "request_digest"},message:"must have required property '"+"request_digest"+"'"};
if(vErrors === null){
vErrors = [err188];
}
else {
vErrors.push(err188);
}
errors++;
}
if(data83.capability_id === undefined){
const err189 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "capability_id"},message:"must have required property '"+"capability_id"+"'"};
if(vErrors === null){
vErrors = [err189];
}
else {
vErrors.push(err189);
}
errors++;
}
if(data83.capability_generation === undefined){
const err190 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "capability_generation"},message:"must have required property '"+"capability_generation"+"'"};
if(vErrors === null){
vErrors = [err190];
}
else {
vErrors.push(err190);
}
errors++;
}
if(data83.status === undefined){
const err191 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err191];
}
else {
vErrors.push(err191);
}
errors++;
}
if(data83.command === undefined){
const err192 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "command"},message:"must have required property '"+"command"+"'"};
if(vErrors === null){
vErrors = [err192];
}
else {
vErrors.push(err192);
}
errors++;
}
if(data83.rule === undefined){
const err193 = {instancePath:instancePath+"/items/" + i0+"/permission",schemaPath:"#/properties/items/items/properties/permission/required",keyword:"required",params:{missingProperty: "rule"},message:"must have required property '"+"rule"+"'"};
if(vErrors === null){
vErrors = [err193];
}
else {
vErrors.push(err193);
}
errors++;
}
if(data83.id !== undefined){
if(typeof data83.id !== "string"){
const err194 = {instancePath:instancePath+"/items/" + i0+"/permission/id",schemaPath:"#/properties/items/items/properties/permission/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err194];
}
else {
vErrors.push(err194);
}
errors++;
}
}
if(data83.agent_id !== undefined){
if(typeof data83.agent_id !== "string"){
const err195 = {instancePath:instancePath+"/items/" + i0+"/permission/agent_id",schemaPath:"#/properties/items/items/properties/permission/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err195];
}
else {
vErrors.push(err195);
}
errors++;
}
}
if(data83.operation_id !== undefined){
if(typeof data83.operation_id !== "string"){
const err196 = {instancePath:instancePath+"/items/" + i0+"/permission/operation_id",schemaPath:"#/properties/items/items/properties/permission/properties/operation_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err196];
}
else {
vErrors.push(err196);
}
errors++;
}
}
if(data83.operation !== undefined){
if(typeof data83.operation !== "string"){
const err197 = {instancePath:instancePath+"/items/" + i0+"/permission/operation",schemaPath:"#/properties/items/items/properties/permission/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err197];
}
else {
vErrors.push(err197);
}
errors++;
}
}
if(data83.canonical_path !== undefined){
if(typeof data83.canonical_path !== "string"){
const err198 = {instancePath:instancePath+"/items/" + i0+"/permission/canonical_path",schemaPath:"#/properties/items/items/properties/permission/properties/canonical_path/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err198];
}
else {
vErrors.push(err198);
}
errors++;
}
}
if(data83.request_digest !== undefined){
if(typeof data83.request_digest !== "string"){
const err199 = {instancePath:instancePath+"/items/" + i0+"/permission/request_digest",schemaPath:"#/properties/items/items/properties/permission/properties/request_digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err199];
}
else {
vErrors.push(err199);
}
errors++;
}
}
if(data83.capability_id !== undefined){
if(typeof data83.capability_id !== "string"){
const err200 = {instancePath:instancePath+"/items/" + i0+"/permission/capability_id",schemaPath:"#/properties/items/items/properties/permission/properties/capability_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err200];
}
else {
vErrors.push(err200);
}
errors++;
}
}
if(data83.capability_generation !== undefined){
let data91 = data83.capability_generation;
if(typeof data91 === "string"){
if(!pattern0.test(data91)){
const err201 = {instancePath:instancePath+"/items/" + i0+"/permission/capability_generation",schemaPath:"#/properties/items/items/properties/permission/properties/capability_generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err201];
}
else {
vErrors.push(err201);
}
errors++;
}
if(!(formats0.validate(data91))){
const err202 = {instancePath:instancePath+"/items/" + i0+"/permission/capability_generation",schemaPath:"#/properties/items/items/properties/permission/properties/capability_generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err202];
}
else {
vErrors.push(err202);
}
errors++;
}
}
else {
const err203 = {instancePath:instancePath+"/items/" + i0+"/permission/capability_generation",schemaPath:"#/properties/items/items/properties/permission/properties/capability_generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err203];
}
else {
vErrors.push(err203);
}
errors++;
}
}
if(data83.status !== undefined){
if(typeof data83.status !== "string"){
const err204 = {instancePath:instancePath+"/items/" + i0+"/permission/status",schemaPath:"#/properties/items/items/properties/permission/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err204];
}
else {
vErrors.push(err204);
}
errors++;
}
}
if(data83.command !== undefined){
if(typeof data83.command !== "string"){
const err205 = {instancePath:instancePath+"/items/" + i0+"/permission/command",schemaPath:"#/properties/items/items/properties/permission/properties/command/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err205];
}
else {
vErrors.push(err205);
}
errors++;
}
}
if(data83.rule !== undefined){
if(typeof data83.rule !== "string"){
const err206 = {instancePath:instancePath+"/items/" + i0+"/permission/rule",schemaPath:"#/properties/items/items/properties/permission/properties/rule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err206];
}
else {
vErrors.push(err206);
}
errors++;
}
}
}
}
if(data5.body !== undefined){
let data95 = data5.body;
if((data95 !== null) && (!(data95 && typeof data95 == "object" && !Array.isArray(data95)))){
const err207 = {instancePath:instancePath+"/items/" + i0+"/body",schemaPath:"#/properties/items/items/properties/body/type",keyword:"type",params:{type: schema102.properties.items.items.properties.body.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err207];
}
else {
vErrors.push(err207);
}
errors++;
}
if(data95 && typeof data95 == "object" && !Array.isArray(data95)){
if(data95.reference_id === undefined){
const err208 = {instancePath:instancePath+"/items/" + i0+"/body",schemaPath:"#/properties/items/items/properties/body/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err208];
}
else {
vErrors.push(err208);
}
errors++;
}
if(data95.digest === undefined){
const err209 = {instancePath:instancePath+"/items/" + i0+"/body",schemaPath:"#/properties/items/items/properties/body/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err209];
}
else {
vErrors.push(err209);
}
errors++;
}
if(data95.size === undefined){
const err210 = {instancePath:instancePath+"/items/" + i0+"/body",schemaPath:"#/properties/items/items/properties/body/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err210];
}
else {
vErrors.push(err210);
}
errors++;
}
if(data95.media_type === undefined){
const err211 = {instancePath:instancePath+"/items/" + i0+"/body",schemaPath:"#/properties/items/items/properties/body/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err211];
}
else {
vErrors.push(err211);
}
errors++;
}
if(data95.source === undefined){
const err212 = {instancePath:instancePath+"/items/" + i0+"/body",schemaPath:"#/properties/items/items/properties/body/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err212];
}
else {
vErrors.push(err212);
}
errors++;
}
if(data95.text !== undefined){
let data96 = data95.text;
if((data96 !== null) && (typeof data96 !== "string")){
const err213 = {instancePath:instancePath+"/items/" + i0+"/body/text",schemaPath:"#/properties/items/items/properties/body/properties/text/type",keyword:"type",params:{type: schema102.properties.items.items.properties.body.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err213];
}
else {
vErrors.push(err213);
}
errors++;
}
}
if(data95.binary !== undefined){
let data97 = data95.binary;
if((typeof data97 !== "string") && (data97 !== null)){
const err214 = {instancePath:instancePath+"/items/" + i0+"/body/binary",schemaPath:"#/properties/items/items/properties/body/properties/binary/type",keyword:"type",params:{type: schema102.properties.items.items.properties.body.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err214];
}
else {
vErrors.push(err214);
}
errors++;
}
}
if(data95.reference_id !== undefined){
if(typeof data95.reference_id !== "string"){
const err215 = {instancePath:instancePath+"/items/" + i0+"/body/reference_id",schemaPath:"#/properties/items/items/properties/body/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err215];
}
else {
vErrors.push(err215);
}
errors++;
}
}
if(data95.digest !== undefined){
if(typeof data95.digest !== "string"){
const err216 = {instancePath:instancePath+"/items/" + i0+"/body/digest",schemaPath:"#/properties/items/items/properties/body/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err216];
}
else {
vErrors.push(err216);
}
errors++;
}
}
if(data95.size !== undefined){
let data100 = data95.size;
if(typeof data100 === "string"){
if(!pattern0.test(data100)){
const err217 = {instancePath:instancePath+"/items/" + i0+"/body/size",schemaPath:"#/properties/items/items/properties/body/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err217];
}
else {
vErrors.push(err217);
}
errors++;
}
if(!(formats0.validate(data100))){
const err218 = {instancePath:instancePath+"/items/" + i0+"/body/size",schemaPath:"#/properties/items/items/properties/body/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err218];
}
else {
vErrors.push(err218);
}
errors++;
}
}
else {
const err219 = {instancePath:instancePath+"/items/" + i0+"/body/size",schemaPath:"#/properties/items/items/properties/body/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err219];
}
else {
vErrors.push(err219);
}
errors++;
}
}
if(data95.media_type !== undefined){
if(typeof data95.media_type !== "string"){
const err220 = {instancePath:instancePath+"/items/" + i0+"/body/media_type",schemaPath:"#/properties/items/items/properties/body/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err220];
}
else {
vErrors.push(err220);
}
errors++;
}
}
if(data95.source !== undefined){
if(typeof data95.source !== "string"){
const err221 = {instancePath:instancePath+"/items/" + i0+"/body/source",schemaPath:"#/properties/items/items/properties/body/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err221];
}
else {
vErrors.push(err221);
}
errors++;
}
}
}
}
}
else {
const err222 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err222];
}
else {
vErrors.push(err222);
}
errors++;
}
}
}
}
if(data.next_cursor !== undefined){
let data103 = data.next_cursor;
if((data103 !== null) && (!(data103 && typeof data103 == "object" && !Array.isArray(data103)))){
const err223 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/type",keyword:"type",params:{type: schema102.properties.next_cursor.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err223];
}
else {
vErrors.push(err223);
}
errors++;
}
if(data103 && typeof data103 == "object" && !Array.isArray(data103)){
if(data103.root_id === undefined){
const err224 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err224];
}
else {
vErrors.push(err224);
}
errors++;
}
if(data103.collection === undefined){
const err225 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/required",keyword:"required",params:{missingProperty: "collection"},message:"must have required property '"+"collection"+"'"};
if(vErrors === null){
vErrors = [err225];
}
else {
vErrors.push(err225);
}
errors++;
}
if(data103.revision === undefined){
const err226 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err226];
}
else {
vErrors.push(err226);
}
errors++;
}
if(data103.offset === undefined){
const err227 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/required",keyword:"required",params:{missingProperty: "offset"},message:"must have required property '"+"offset"+"'"};
if(vErrors === null){
vErrors = [err227];
}
else {
vErrors.push(err227);
}
errors++;
}
if(data103.root_id !== undefined){
if(typeof data103.root_id !== "string"){
const err228 = {instancePath:instancePath+"/next_cursor/root_id",schemaPath:"#/properties/next_cursor/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err228];
}
else {
vErrors.push(err228);
}
errors++;
}
}
if(data103.collection !== undefined){
if(typeof data103.collection !== "string"){
const err229 = {instancePath:instancePath+"/next_cursor/collection",schemaPath:"#/properties/next_cursor/properties/collection/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err229];
}
else {
vErrors.push(err229);
}
errors++;
}
}
if(data103.revision !== undefined){
let data106 = data103.revision;
if(typeof data106 === "string"){
if(!pattern0.test(data106)){
const err230 = {instancePath:instancePath+"/next_cursor/revision",schemaPath:"#/properties/next_cursor/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err230];
}
else {
vErrors.push(err230);
}
errors++;
}
if(!(formats0.validate(data106))){
const err231 = {instancePath:instancePath+"/next_cursor/revision",schemaPath:"#/properties/next_cursor/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err231];
}
else {
vErrors.push(err231);
}
errors++;
}
}
else {
const err232 = {instancePath:instancePath+"/next_cursor/revision",schemaPath:"#/properties/next_cursor/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err232];
}
else {
vErrors.push(err232);
}
errors++;
}
}
if(data103.offset !== undefined){
let data107 = data103.offset;
if(typeof data107 === "string"){
if(!pattern0.test(data107)){
const err233 = {instancePath:instancePath+"/next_cursor/offset",schemaPath:"#/properties/next_cursor/properties/offset/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err233];
}
else {
vErrors.push(err233);
}
errors++;
}
if(!(formats0.validate(data107))){
const err234 = {instancePath:instancePath+"/next_cursor/offset",schemaPath:"#/properties/next_cursor/properties/offset/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err234];
}
else {
vErrors.push(err234);
}
errors++;
}
}
else {
const err235 = {instancePath:instancePath+"/next_cursor/offset",schemaPath:"#/properties/next_cursor/properties/offset/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err235];
}
else {
vErrors.push(err235);
}
errors++;
}
}
}
}
if(data.has_more !== undefined){
if(typeof data.has_more !== "boolean"){
const err236 = {instancePath:instancePath+"/has_more",schemaPath:"#/properties/has_more/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err236];
}
else {
vErrors.push(err236);
}
errors++;
}
}
}
else {
const err237 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err237];
}
else {
vErrors.push(err237);
}
errors++;
}
validate101.errors = vErrors;
return errors === 0;
}

export const RootCollectionParams = validate102;
const schema103 = {"type":"object","properties":{"root_id":{"type":"string"},"collection":{"type":"string"},"cursor":{"type":["null","object"],"properties":{"root_id":{"type":"string"},"collection":{"type":"string"},"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"offset":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["root_id","collection","revision","offset"],"additionalProperties":true},"limit":{"type":"integer"},"max_bytes":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/RootCollectionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"RootCollectionParams","required":["root_id","collection","limit","max_bytes"],"additionalProperties":true};

function validate102(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RootCollectionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.collection === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "collection"},message:"must have required property '"+"collection"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.limit === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.max_bytes === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "max_bytes"},message:"must have required property '"+"max_bytes"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err4 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.collection !== undefined){
if(typeof data.collection !== "string"){
const err5 = {instancePath:instancePath+"/collection",schemaPath:"#/properties/collection/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.cursor !== undefined){
let data2 = data.cursor;
if((data2 !== null) && (!(data2 && typeof data2 == "object" && !Array.isArray(data2)))){
const err6 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: schema103.properties.cursor.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.root_id === undefined){
const err7 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data2.collection === undefined){
const err8 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/required",keyword:"required",params:{missingProperty: "collection"},message:"must have required property '"+"collection"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data2.revision === undefined){
const err9 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data2.offset === undefined){
const err10 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/required",keyword:"required",params:{missingProperty: "offset"},message:"must have required property '"+"offset"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data2.root_id !== undefined){
if(typeof data2.root_id !== "string"){
const err11 = {instancePath:instancePath+"/cursor/root_id",schemaPath:"#/properties/cursor/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data2.collection !== undefined){
if(typeof data2.collection !== "string"){
const err12 = {instancePath:instancePath+"/cursor/collection",schemaPath:"#/properties/cursor/properties/collection/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data2.revision !== undefined){
let data5 = data2.revision;
if(typeof data5 === "string"){
if(!pattern0.test(data5)){
const err13 = {instancePath:instancePath+"/cursor/revision",schemaPath:"#/properties/cursor/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(!(formats0.validate(data5))){
const err14 = {instancePath:instancePath+"/cursor/revision",schemaPath:"#/properties/cursor/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
else {
const err15 = {instancePath:instancePath+"/cursor/revision",schemaPath:"#/properties/cursor/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data2.offset !== undefined){
let data6 = data2.offset;
if(typeof data6 === "string"){
if(!pattern0.test(data6)){
const err16 = {instancePath:instancePath+"/cursor/offset",schemaPath:"#/properties/cursor/properties/offset/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(!(formats0.validate(data6))){
const err17 = {instancePath:instancePath+"/cursor/offset",schemaPath:"#/properties/cursor/properties/offset/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
else {
const err18 = {instancePath:instancePath+"/cursor/offset",schemaPath:"#/properties/cursor/properties/offset/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
}
}
if(data.limit !== undefined){
let data7 = data.limit;
if(!((typeof data7 == "number") && (!(data7 % 1) && !isNaN(data7)))){
const err19 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data.max_bytes !== undefined){
let data8 = data.max_bytes;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err20 = {instancePath:instancePath+"/max_bytes",schemaPath:"#/properties/max_bytes/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
}
else {
const err21 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
validate102.errors = vErrors;
return errors === 0;
}

export const RootIDResult = validate103;
const schema104 = {"type":"object","properties":{"root_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/RootIDResult","$schema":"http://json-schema.org/draft-07/schema#","title":"RootIDResult","required":["root_id"],"additionalProperties":true};

function validate103(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RootIDResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err1 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate103.errors = vErrors;
return errors === 0;
}

export const RootParams = validate104;
const schema105 = {"type":"object","properties":{"root_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/RootParams","$schema":"http://json-schema.org/draft-07/schema#","title":"RootParams","required":["root_id"],"additionalProperties":true};

function validate104(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RootParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err1 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate104.errors = vErrors;
return errors === 0;
}

export const RootSnapshot = validate105;
const schema106 = {"type":"object","properties":{"active_turns":{"type":"object","additionalProperties":{"type":"string"}},"omitted":{"type":"object","additionalProperties":{"type":"boolean"}},"message_seqs":{"type":["null","array"],"items":{"type":"integer"}},"first_message_seq":{"type":"integer"},"history_revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"root_id":{"type":"string"},"cursor":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"meta":{"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string"},"title":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"cwd":{"type":"string"},"goal":{"type":"string"},"forked_from":{"type":"string"},"fork_seq":{"type":"integer"},"tags":{"type":["null","array"],"items":{"type":"string"}},"pinned":{"type":"boolean"},"effort":{"type":"string"},"usage_in":{"type":"integer"},"usage_cached":{"type":"integer"},"usage_out":{"type":"integer"},"updated_at":{"type":"string"}},"required":["id","kind","title","model","provider","cwd","goal","forked_from","fork_seq","tags","pinned","effort","usage_in","usage_cached","usage_out","updated_at"],"additionalProperties":true},"messages":{"type":["null","array"],"items":{"type":"object","properties":{"role":{"type":"string"},"content":{"type":"string"},"tool_calls":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"function":{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"string"}},"required":["name","arguments"],"additionalProperties":true},"duration_ms":{"type":"integer"},"exit_code":{"type":"integer"}},"required":["id","type","function"],"additionalProperties":true}},"tool_call_id":{"type":"string"},"name":{"type":"string"},"authored":{"type":"boolean"},"sent_at":{"type":["null","string"]},"usage":{"type":["null","object"],"properties":{"prompt_tokens":{"type":"integer"},"completion_tokens":{"type":"integer"},"prompt_tokens_details":{"type":["null","object"],"properties":{"cached_tokens":{"type":"integer"}},"required":["cached_tokens"],"additionalProperties":true}},"required":["prompt_tokens","completion_tokens"],"additionalProperties":true},"model":{"type":"string"},"rewound_from":{"type":"string"}},"required":["role","content"],"additionalProperties":true}},"presentation":{"type":["null","array"],"items":{"type":"object","properties":{"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"payload":true},"required":["seq","kind","payload"],"additionalProperties":true}},"agent_presentations":{"type":"object","additionalProperties":{"type":["null","array"],"items":{"type":"object","properties":{"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"payload":true},"required":["seq","kind","payload"],"additionalProperties":true}}},"agents":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"root_id":{"type":"string"},"parent_id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"effort":{"type":"string"},"cwd":{"type":"string"},"report":{"type":"string"},"status":{"type":"string"},"pending_mail":{"type":"integer"},"lifecycle_phase":{"type":"string"},"blocking_reason":{"type":"string"},"terminal_cause":{"type":"string"},"allowed_controls":{"type":["null","array"],"items":{"type":"string"}}},"required":["id","root_id","parent_id","name","model","provider","effort","cwd","report","status","pending_mail","lifecycle_phase","blocking_reason","terminal_cause","allowed_controls"],"additionalProperties":true}},"inbox":{"type":["null","array"],"items":{"type":"object","properties":{"root_id":{"type":"string"},"agent_id":{"type":"string"},"seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"kind":{"type":"string"},"status":{"type":"string"},"payload":{"type":"object","properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["root_id","agent_id","seq","kind","status","payload"],"additionalProperties":true}},"blackboard":{"type":["null","array"],"items":{"type":"object","properties":{"key":{"type":"string"},"version":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"author_agent_id":{"type":"string"},"payload":{"type":"object","properties":{"inline":true,"text":{"type":["null","string"]},"binary":{"type":["string","null"],"contentEncoding":"base64"},"reference_id":{"type":"string"},"digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"required":["reference_id","digest","size","media_type","source"],"additionalProperties":true}},"required":["key","version","author_agent_id","payload"],"additionalProperties":true}},"budgets":{"type":["null","array"],"items":{"type":"object","properties":{"agent_id":{"type":"string"},"state":{"type":"object","properties":{"kind":{"type":"string"},"limit":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"used":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"reserved":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"remaining":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["kind","limit","used","reserved","remaining"],"additionalProperties":true}},"required":["agent_id","state"],"additionalProperties":true}},"capabilities":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"root_id":{"type":"string"},"agent_id":{"type":"string"},"issuer_agent_id":{"type":"string"},"operations":{"type":["null","array"],"items":{"type":"string"}},"scopes":{"type":["null","array"],"items":{"type":"string"}},"mcp":{"type":["null","array"],"items":{"type":"object","properties":{"server":{"type":"string"},"tool":{"type":"string"},"definition":{"type":"string"}},"required":["server","tool","definition"],"additionalProperties":true}},"mcp_all":{"type":"boolean"},"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"},"expires_at":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"}},"required":["id","root_id","agent_id","issuer_agent_id","operations","scopes","mcp","mcp_all","generation","status","expires_at","created_at","updated_at"],"additionalProperties":true}},"schedules":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"integer"},"schedule":{"type":"string"},"prompt":{"type":"string"},"anchor":{"type":"string"},"last_fire":{"type":"string"}},"required":["id","schedule","prompt","anchor","last_fire"],"additionalProperties":true}},"permissions":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"agent_id":{"type":"string"},"operation_id":{"type":"string"},"operation":{"type":"string"},"canonical_path":{"type":"string"},"request_digest":{"type":"string"},"capability_id":{"type":"string"},"capability_generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"status":{"type":"string"},"command":{"type":"string"},"rule":{"type":"string"}},"required":["id","agent_id","operation_id","operation","canonical_path","request_digest","capability_id","capability_generation","status","command","rule"],"additionalProperties":true}},"questions":{"type":["null","array"],"items":{"type":"object","properties":{"turn_id":{"type":"string"},"root_id":{"type":"string"},"agent_id":{"type":"string"},"sender_agent_id":{"type":"string"},"inbox_seq":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"inbox_kind":{"type":"string"},"delivery":{"type":"string"},"message_id":{"type":"string"},"phase":{"type":"string"},"status":{"type":"string"},"terminal_cause":{"type":"string"},"command_client_id":{"type":"string"},"command_id":{"type":"string"},"operation_id":{"type":"string"},"trace_id":{"type":"string"},"schedule_id":{"type":"integer"},"slot":{"type":"string"},"error":{"type":"string"},"acknowledged_inbox":{"type":"array","items":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"subscription_id":{"type":"string"},"key":{"type":"string"},"version":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"expected_version":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"restored":{"type":["null","array"],"items":{"type":"string"}},"not_restored":{"type":["null","array"],"items":{"type":"object","properties":{"name":{"type":"string"},"reason":{"type":"string"}},"required":["name","reason"],"additionalProperties":true}},"attempt":{"type":"string"},"budget_kind":{"type":"string"},"amount":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"limit":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"used":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"reserved":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"capability_id":{"type":"string"},"generation":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"permission_id":{"type":"string"},"operation":{"type":"string"},"canonical_path":{"type":"string"},"request_digest":{"type":"string"},"command":{"type":"string"},"rule":{"type":"string"},"rule_source":{"type":"string"},"question_id":{"type":"string"},"question":{"type":"string"},"options":{"type":["null","array"],"items":{"type":"object","properties":{"label":{"type":"string"},"description":{"type":"string"}},"required":["label"],"additionalProperties":true}},"multiple":{"type":"boolean"},"answer":{"type":["null","array"],"items":{"type":"string"}},"dismissed":{"type":"boolean"}},"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/RootSnapshot","$schema":"http://json-schema.org/draft-07/schema#","title":"RootSnapshot","required":["active_turns","message_seqs","history_revision","root_id","cursor","meta","messages","presentation","agent_presentations","agents","inbox","blackboard","budgets","capabilities","schedules","permissions","questions"],"additionalProperties":true};

function validate105(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RootSnapshot" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.active_turns === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "active_turns"},message:"must have required property '"+"active_turns"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.message_seqs === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "message_seqs"},message:"must have required property '"+"message_seqs"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.history_revision === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "history_revision"},message:"must have required property '"+"history_revision"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.root_id === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.cursor === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cursor"},message:"must have required property '"+"cursor"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.meta === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "meta"},message:"must have required property '"+"meta"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.messages === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "messages"},message:"must have required property '"+"messages"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data.presentation === undefined){
const err7 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "presentation"},message:"must have required property '"+"presentation"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data.agent_presentations === undefined){
const err8 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "agent_presentations"},message:"must have required property '"+"agent_presentations"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data.agents === undefined){
const err9 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "agents"},message:"must have required property '"+"agents"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data.inbox === undefined){
const err10 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "inbox"},message:"must have required property '"+"inbox"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data.blackboard === undefined){
const err11 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "blackboard"},message:"must have required property '"+"blackboard"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data.budgets === undefined){
const err12 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "budgets"},message:"must have required property '"+"budgets"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data.capabilities === undefined){
const err13 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "capabilities"},message:"must have required property '"+"capabilities"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data.schedules === undefined){
const err14 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "schedules"},message:"must have required property '"+"schedules"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data.permissions === undefined){
const err15 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "permissions"},message:"must have required property '"+"permissions"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data.questions === undefined){
const err16 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "questions"},message:"must have required property '"+"questions"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data.active_turns !== undefined){
let data0 = data.active_turns;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
for(const key0 in data0){
if(typeof data0[key0] !== "string"){
const err17 = {instancePath:instancePath+"/active_turns/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/active_turns/additionalProperties/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
}
else {
const err18 = {instancePath:instancePath+"/active_turns",schemaPath:"#/properties/active_turns/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data.omitted !== undefined){
let data2 = data.omitted;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
for(const key1 in data2){
if(typeof data2[key1] !== "boolean"){
const err19 = {instancePath:instancePath+"/omitted/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/omitted/additionalProperties/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
}
else {
const err20 = {instancePath:instancePath+"/omitted",schemaPath:"#/properties/omitted/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data.message_seqs !== undefined){
let data4 = data.message_seqs;
if((data4 !== null) && (!(Array.isArray(data4)))){
const err21 = {instancePath:instancePath+"/message_seqs",schemaPath:"#/properties/message_seqs/type",keyword:"type",params:{type: schema106.properties.message_seqs.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(Array.isArray(data4)){
const len0 = data4.length;
for(let i0=0; i0<len0; i0++){
let data5 = data4[i0];
if(!((typeof data5 == "number") && (!(data5 % 1) && !isNaN(data5)))){
const err22 = {instancePath:instancePath+"/message_seqs/" + i0,schemaPath:"#/properties/message_seqs/items/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
}
}
if(data.first_message_seq !== undefined){
let data6 = data.first_message_seq;
if(!((typeof data6 == "number") && (!(data6 % 1) && !isNaN(data6)))){
const err23 = {instancePath:instancePath+"/first_message_seq",schemaPath:"#/properties/first_message_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data.history_revision !== undefined){
let data7 = data.history_revision;
if(typeof data7 === "string"){
if(!pattern0.test(data7)){
const err24 = {instancePath:instancePath+"/history_revision",schemaPath:"#/properties/history_revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(!(formats0.validate(data7))){
const err25 = {instancePath:instancePath+"/history_revision",schemaPath:"#/properties/history_revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
else {
const err26 = {instancePath:instancePath+"/history_revision",schemaPath:"#/properties/history_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err27 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data.cursor !== undefined){
let data9 = data.cursor;
if(typeof data9 === "string"){
if(!pattern0.test(data9)){
const err28 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(!(formats0.validate(data9))){
const err29 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
else {
const err30 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data.meta !== undefined){
let data10 = data.meta;
if(data10 && typeof data10 == "object" && !Array.isArray(data10)){
if(data10.id === undefined){
const err31 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data10.kind === undefined){
const err32 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(data10.title === undefined){
const err33 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data10.model === undefined){
const err34 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(data10.provider === undefined){
const err35 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(data10.cwd === undefined){
const err36 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(data10.goal === undefined){
const err37 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "goal"},message:"must have required property '"+"goal"+"'"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
if(data10.forked_from === undefined){
const err38 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "forked_from"},message:"must have required property '"+"forked_from"+"'"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(data10.fork_seq === undefined){
const err39 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "fork_seq"},message:"must have required property '"+"fork_seq"+"'"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
if(data10.tags === undefined){
const err40 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "tags"},message:"must have required property '"+"tags"+"'"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data10.pinned === undefined){
const err41 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
if(data10.effort === undefined){
const err42 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data10.usage_in === undefined){
const err43 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "usage_in"},message:"must have required property '"+"usage_in"+"'"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
if(data10.usage_cached === undefined){
const err44 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "usage_cached"},message:"must have required property '"+"usage_cached"+"'"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
if(data10.usage_out === undefined){
const err45 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "usage_out"},message:"must have required property '"+"usage_out"+"'"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(data10.updated_at === undefined){
const err46 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/required",keyword:"required",params:{missingProperty: "updated_at"},message:"must have required property '"+"updated_at"+"'"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
if(data10.id !== undefined){
if(typeof data10.id !== "string"){
const err47 = {instancePath:instancePath+"/meta/id",schemaPath:"#/properties/meta/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data10.kind !== undefined){
if(typeof data10.kind !== "string"){
const err48 = {instancePath:instancePath+"/meta/kind",schemaPath:"#/properties/meta/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data10.title !== undefined){
if(typeof data10.title !== "string"){
const err49 = {instancePath:instancePath+"/meta/title",schemaPath:"#/properties/meta/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
if(data10.model !== undefined){
if(typeof data10.model !== "string"){
const err50 = {instancePath:instancePath+"/meta/model",schemaPath:"#/properties/meta/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data10.provider !== undefined){
if(typeof data10.provider !== "string"){
const err51 = {instancePath:instancePath+"/meta/provider",schemaPath:"#/properties/meta/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
if(data10.cwd !== undefined){
if(typeof data10.cwd !== "string"){
const err52 = {instancePath:instancePath+"/meta/cwd",schemaPath:"#/properties/meta/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data10.goal !== undefined){
if(typeof data10.goal !== "string"){
const err53 = {instancePath:instancePath+"/meta/goal",schemaPath:"#/properties/meta/properties/goal/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
if(data10.forked_from !== undefined){
if(typeof data10.forked_from !== "string"){
const err54 = {instancePath:instancePath+"/meta/forked_from",schemaPath:"#/properties/meta/properties/forked_from/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data10.fork_seq !== undefined){
let data19 = data10.fork_seq;
if(!((typeof data19 == "number") && (!(data19 % 1) && !isNaN(data19)))){
const err55 = {instancePath:instancePath+"/meta/fork_seq",schemaPath:"#/properties/meta/properties/fork_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
}
if(data10.tags !== undefined){
let data20 = data10.tags;
if((data20 !== null) && (!(Array.isArray(data20)))){
const err56 = {instancePath:instancePath+"/meta/tags",schemaPath:"#/properties/meta/properties/tags/type",keyword:"type",params:{type: schema106.properties.meta.properties.tags.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
if(Array.isArray(data20)){
const len1 = data20.length;
for(let i1=0; i1<len1; i1++){
if(typeof data20[i1] !== "string"){
const err57 = {instancePath:instancePath+"/meta/tags/" + i1,schemaPath:"#/properties/meta/properties/tags/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
}
}
}
if(data10.pinned !== undefined){
if(typeof data10.pinned !== "boolean"){
const err58 = {instancePath:instancePath+"/meta/pinned",schemaPath:"#/properties/meta/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
if(data10.effort !== undefined){
if(typeof data10.effort !== "string"){
const err59 = {instancePath:instancePath+"/meta/effort",schemaPath:"#/properties/meta/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data10.usage_in !== undefined){
let data24 = data10.usage_in;
if(!((typeof data24 == "number") && (!(data24 % 1) && !isNaN(data24)))){
const err60 = {instancePath:instancePath+"/meta/usage_in",schemaPath:"#/properties/meta/properties/usage_in/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
if(data10.usage_cached !== undefined){
let data25 = data10.usage_cached;
if(!((typeof data25 == "number") && (!(data25 % 1) && !isNaN(data25)))){
const err61 = {instancePath:instancePath+"/meta/usage_cached",schemaPath:"#/properties/meta/properties/usage_cached/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
}
if(data10.usage_out !== undefined){
let data26 = data10.usage_out;
if(!((typeof data26 == "number") && (!(data26 % 1) && !isNaN(data26)))){
const err62 = {instancePath:instancePath+"/meta/usage_out",schemaPath:"#/properties/meta/properties/usage_out/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
if(data10.updated_at !== undefined){
if(typeof data10.updated_at !== "string"){
const err63 = {instancePath:instancePath+"/meta/updated_at",schemaPath:"#/properties/meta/properties/updated_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
}
}
else {
const err64 = {instancePath:instancePath+"/meta",schemaPath:"#/properties/meta/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data.messages !== undefined){
let data28 = data.messages;
if((data28 !== null) && (!(Array.isArray(data28)))){
const err65 = {instancePath:instancePath+"/messages",schemaPath:"#/properties/messages/type",keyword:"type",params:{type: schema106.properties.messages.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
if(Array.isArray(data28)){
const len2 = data28.length;
for(let i2=0; i2<len2; i2++){
let data29 = data28[i2];
if(data29 && typeof data29 == "object" && !Array.isArray(data29)){
if(data29.role === undefined){
const err66 = {instancePath:instancePath+"/messages/" + i2,schemaPath:"#/properties/messages/items/required",keyword:"required",params:{missingProperty: "role"},message:"must have required property '"+"role"+"'"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
if(data29.content === undefined){
const err67 = {instancePath:instancePath+"/messages/" + i2,schemaPath:"#/properties/messages/items/required",keyword:"required",params:{missingProperty: "content"},message:"must have required property '"+"content"+"'"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
if(data29.role !== undefined){
if(typeof data29.role !== "string"){
const err68 = {instancePath:instancePath+"/messages/" + i2+"/role",schemaPath:"#/properties/messages/items/properties/role/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
}
if(data29.content !== undefined){
if(typeof data29.content !== "string"){
const err69 = {instancePath:instancePath+"/messages/" + i2+"/content",schemaPath:"#/properties/messages/items/properties/content/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
}
if(data29.tool_calls !== undefined){
let data32 = data29.tool_calls;
if((data32 !== null) && (!(Array.isArray(data32)))){
const err70 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls",schemaPath:"#/properties/messages/items/properties/tool_calls/type",keyword:"type",params:{type: schema106.properties.messages.items.properties.tool_calls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
if(Array.isArray(data32)){
const len3 = data32.length;
for(let i3=0; i3<len3; i3++){
let data33 = data32[i3];
if(data33 && typeof data33 == "object" && !Array.isArray(data33)){
if(data33.id === undefined){
const err71 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3,schemaPath:"#/properties/messages/items/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
if(data33.type === undefined){
const err72 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3,schemaPath:"#/properties/messages/items/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
if(data33.function === undefined){
const err73 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3,schemaPath:"#/properties/messages/items/properties/tool_calls/items/required",keyword:"required",params:{missingProperty: "function"},message:"must have required property '"+"function"+"'"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
if(data33.id !== undefined){
if(typeof data33.id !== "string"){
const err74 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/id",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
}
if(data33.type !== undefined){
if(typeof data33.type !== "string"){
const err75 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/type",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
if(data33.function !== undefined){
let data36 = data33.function;
if(data36 && typeof data36 == "object" && !Array.isArray(data36)){
if(data36.name === undefined){
const err76 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/function",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/function/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
if(data36.arguments === undefined){
const err77 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/function",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/function/required",keyword:"required",params:{missingProperty: "arguments"},message:"must have required property '"+"arguments"+"'"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
if(data36.name !== undefined){
if(typeof data36.name !== "string"){
const err78 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/function/name",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/function/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
}
if(data36.arguments !== undefined){
if(typeof data36.arguments !== "string"){
const err79 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/function/arguments",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/function/properties/arguments/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
}
}
else {
const err80 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/function",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/function/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
}
if(data33.duration_ms !== undefined){
let data39 = data33.duration_ms;
if(!((typeof data39 == "number") && (!(data39 % 1) && !isNaN(data39)))){
const err81 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/duration_ms",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/duration_ms/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
}
if(data33.exit_code !== undefined){
let data40 = data33.exit_code;
if(!((typeof data40 == "number") && (!(data40 % 1) && !isNaN(data40)))){
const err82 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3+"/exit_code",schemaPath:"#/properties/messages/items/properties/tool_calls/items/properties/exit_code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
}
else {
const err83 = {instancePath:instancePath+"/messages/" + i2+"/tool_calls/" + i3,schemaPath:"#/properties/messages/items/properties/tool_calls/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
}
}
}
if(data29.tool_call_id !== undefined){
if(typeof data29.tool_call_id !== "string"){
const err84 = {instancePath:instancePath+"/messages/" + i2+"/tool_call_id",schemaPath:"#/properties/messages/items/properties/tool_call_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
}
if(data29.name !== undefined){
if(typeof data29.name !== "string"){
const err85 = {instancePath:instancePath+"/messages/" + i2+"/name",schemaPath:"#/properties/messages/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err85];
}
else {
vErrors.push(err85);
}
errors++;
}
}
if(data29.authored !== undefined){
if(typeof data29.authored !== "boolean"){
const err86 = {instancePath:instancePath+"/messages/" + i2+"/authored",schemaPath:"#/properties/messages/items/properties/authored/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
}
if(data29.sent_at !== undefined){
let data44 = data29.sent_at;
if((data44 !== null) && (typeof data44 !== "string")){
const err87 = {instancePath:instancePath+"/messages/" + i2+"/sent_at",schemaPath:"#/properties/messages/items/properties/sent_at/type",keyword:"type",params:{type: schema106.properties.messages.items.properties.sent_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
}
if(data29.usage !== undefined){
let data45 = data29.usage;
if((data45 !== null) && (!(data45 && typeof data45 == "object" && !Array.isArray(data45)))){
const err88 = {instancePath:instancePath+"/messages/" + i2+"/usage",schemaPath:"#/properties/messages/items/properties/usage/type",keyword:"type",params:{type: schema106.properties.messages.items.properties.usage.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
if(data45 && typeof data45 == "object" && !Array.isArray(data45)){
if(data45.prompt_tokens === undefined){
const err89 = {instancePath:instancePath+"/messages/" + i2+"/usage",schemaPath:"#/properties/messages/items/properties/usage/required",keyword:"required",params:{missingProperty: "prompt_tokens"},message:"must have required property '"+"prompt_tokens"+"'"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
if(data45.completion_tokens === undefined){
const err90 = {instancePath:instancePath+"/messages/" + i2+"/usage",schemaPath:"#/properties/messages/items/properties/usage/required",keyword:"required",params:{missingProperty: "completion_tokens"},message:"must have required property '"+"completion_tokens"+"'"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
if(data45.prompt_tokens !== undefined){
let data46 = data45.prompt_tokens;
if(!((typeof data46 == "number") && (!(data46 % 1) && !isNaN(data46)))){
const err91 = {instancePath:instancePath+"/messages/" + i2+"/usage/prompt_tokens",schemaPath:"#/properties/messages/items/properties/usage/properties/prompt_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err91];
}
else {
vErrors.push(err91);
}
errors++;
}
}
if(data45.completion_tokens !== undefined){
let data47 = data45.completion_tokens;
if(!((typeof data47 == "number") && (!(data47 % 1) && !isNaN(data47)))){
const err92 = {instancePath:instancePath+"/messages/" + i2+"/usage/completion_tokens",schemaPath:"#/properties/messages/items/properties/usage/properties/completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err92];
}
else {
vErrors.push(err92);
}
errors++;
}
}
if(data45.prompt_tokens_details !== undefined){
let data48 = data45.prompt_tokens_details;
if((data48 !== null) && (!(data48 && typeof data48 == "object" && !Array.isArray(data48)))){
const err93 = {instancePath:instancePath+"/messages/" + i2+"/usage/prompt_tokens_details",schemaPath:"#/properties/messages/items/properties/usage/properties/prompt_tokens_details/type",keyword:"type",params:{type: schema106.properties.messages.items.properties.usage.properties.prompt_tokens_details.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err93];
}
else {
vErrors.push(err93);
}
errors++;
}
if(data48 && typeof data48 == "object" && !Array.isArray(data48)){
if(data48.cached_tokens === undefined){
const err94 = {instancePath:instancePath+"/messages/" + i2+"/usage/prompt_tokens_details",schemaPath:"#/properties/messages/items/properties/usage/properties/prompt_tokens_details/required",keyword:"required",params:{missingProperty: "cached_tokens"},message:"must have required property '"+"cached_tokens"+"'"};
if(vErrors === null){
vErrors = [err94];
}
else {
vErrors.push(err94);
}
errors++;
}
if(data48.cached_tokens !== undefined){
let data49 = data48.cached_tokens;
if(!((typeof data49 == "number") && (!(data49 % 1) && !isNaN(data49)))){
const err95 = {instancePath:instancePath+"/messages/" + i2+"/usage/prompt_tokens_details/cached_tokens",schemaPath:"#/properties/messages/items/properties/usage/properties/prompt_tokens_details/properties/cached_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err95];
}
else {
vErrors.push(err95);
}
errors++;
}
}
}
}
}
}
if(data29.model !== undefined){
if(typeof data29.model !== "string"){
const err96 = {instancePath:instancePath+"/messages/" + i2+"/model",schemaPath:"#/properties/messages/items/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err96];
}
else {
vErrors.push(err96);
}
errors++;
}
}
if(data29.rewound_from !== undefined){
if(typeof data29.rewound_from !== "string"){
const err97 = {instancePath:instancePath+"/messages/" + i2+"/rewound_from",schemaPath:"#/properties/messages/items/properties/rewound_from/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err97];
}
else {
vErrors.push(err97);
}
errors++;
}
}
}
else {
const err98 = {instancePath:instancePath+"/messages/" + i2,schemaPath:"#/properties/messages/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err98];
}
else {
vErrors.push(err98);
}
errors++;
}
}
}
}
if(data.presentation !== undefined){
let data52 = data.presentation;
if((data52 !== null) && (!(Array.isArray(data52)))){
const err99 = {instancePath:instancePath+"/presentation",schemaPath:"#/properties/presentation/type",keyword:"type",params:{type: schema106.properties.presentation.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err99];
}
else {
vErrors.push(err99);
}
errors++;
}
if(Array.isArray(data52)){
const len4 = data52.length;
for(let i4=0; i4<len4; i4++){
let data53 = data52[i4];
if(data53 && typeof data53 == "object" && !Array.isArray(data53)){
if(data53.seq === undefined){
const err100 = {instancePath:instancePath+"/presentation/" + i4,schemaPath:"#/properties/presentation/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err100];
}
else {
vErrors.push(err100);
}
errors++;
}
if(data53.kind === undefined){
const err101 = {instancePath:instancePath+"/presentation/" + i4,schemaPath:"#/properties/presentation/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err101];
}
else {
vErrors.push(err101);
}
errors++;
}
if(data53.payload === undefined){
const err102 = {instancePath:instancePath+"/presentation/" + i4,schemaPath:"#/properties/presentation/items/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err102];
}
else {
vErrors.push(err102);
}
errors++;
}
if(data53.seq !== undefined){
let data54 = data53.seq;
if(typeof data54 === "string"){
if(!pattern0.test(data54)){
const err103 = {instancePath:instancePath+"/presentation/" + i4+"/seq",schemaPath:"#/properties/presentation/items/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err103];
}
else {
vErrors.push(err103);
}
errors++;
}
if(!(formats0.validate(data54))){
const err104 = {instancePath:instancePath+"/presentation/" + i4+"/seq",schemaPath:"#/properties/presentation/items/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err104];
}
else {
vErrors.push(err104);
}
errors++;
}
}
else {
const err105 = {instancePath:instancePath+"/presentation/" + i4+"/seq",schemaPath:"#/properties/presentation/items/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err105];
}
else {
vErrors.push(err105);
}
errors++;
}
}
if(data53.kind !== undefined){
if(typeof data53.kind !== "string"){
const err106 = {instancePath:instancePath+"/presentation/" + i4+"/kind",schemaPath:"#/properties/presentation/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err106];
}
else {
vErrors.push(err106);
}
errors++;
}
}
}
else {
const err107 = {instancePath:instancePath+"/presentation/" + i4,schemaPath:"#/properties/presentation/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err107];
}
else {
vErrors.push(err107);
}
errors++;
}
}
}
}
if(data.agent_presentations !== undefined){
let data56 = data.agent_presentations;
if(data56 && typeof data56 == "object" && !Array.isArray(data56)){
for(const key2 in data56){
let data57 = data56[key2];
if((data57 !== null) && (!(Array.isArray(data57)))){
const err108 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/agent_presentations/additionalProperties/type",keyword:"type",params:{type: schema106.properties.agent_presentations.additionalProperties.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err108];
}
else {
vErrors.push(err108);
}
errors++;
}
if(Array.isArray(data57)){
const len5 = data57.length;
for(let i5=0; i5<len5; i5++){
let data58 = data57[i5];
if(data58 && typeof data58 == "object" && !Array.isArray(data58)){
if(data58.seq === undefined){
const err109 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5,schemaPath:"#/properties/agent_presentations/additionalProperties/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err109];
}
else {
vErrors.push(err109);
}
errors++;
}
if(data58.kind === undefined){
const err110 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5,schemaPath:"#/properties/agent_presentations/additionalProperties/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err110];
}
else {
vErrors.push(err110);
}
errors++;
}
if(data58.payload === undefined){
const err111 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5,schemaPath:"#/properties/agent_presentations/additionalProperties/items/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err111];
}
else {
vErrors.push(err111);
}
errors++;
}
if(data58.seq !== undefined){
let data59 = data58.seq;
if(typeof data59 === "string"){
if(!pattern0.test(data59)){
const err112 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5+"/seq",schemaPath:"#/properties/agent_presentations/additionalProperties/items/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err112];
}
else {
vErrors.push(err112);
}
errors++;
}
if(!(formats0.validate(data59))){
const err113 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5+"/seq",schemaPath:"#/properties/agent_presentations/additionalProperties/items/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err113];
}
else {
vErrors.push(err113);
}
errors++;
}
}
else {
const err114 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5+"/seq",schemaPath:"#/properties/agent_presentations/additionalProperties/items/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err114];
}
else {
vErrors.push(err114);
}
errors++;
}
}
if(data58.kind !== undefined){
if(typeof data58.kind !== "string"){
const err115 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5+"/kind",schemaPath:"#/properties/agent_presentations/additionalProperties/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err115];
}
else {
vErrors.push(err115);
}
errors++;
}
}
}
else {
const err116 = {instancePath:instancePath+"/agent_presentations/" + key2.replace(/~/g, "~0").replace(/\//g, "~1")+"/" + i5,schemaPath:"#/properties/agent_presentations/additionalProperties/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err116];
}
else {
vErrors.push(err116);
}
errors++;
}
}
}
}
}
else {
const err117 = {instancePath:instancePath+"/agent_presentations",schemaPath:"#/properties/agent_presentations/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err117];
}
else {
vErrors.push(err117);
}
errors++;
}
}
if(data.agents !== undefined){
let data61 = data.agents;
if((data61 !== null) && (!(Array.isArray(data61)))){
const err118 = {instancePath:instancePath+"/agents",schemaPath:"#/properties/agents/type",keyword:"type",params:{type: schema106.properties.agents.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err118];
}
else {
vErrors.push(err118);
}
errors++;
}
if(Array.isArray(data61)){
const len6 = data61.length;
for(let i6=0; i6<len6; i6++){
let data62 = data61[i6];
if(data62 && typeof data62 == "object" && !Array.isArray(data62)){
if(data62.id === undefined){
const err119 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err119];
}
else {
vErrors.push(err119);
}
errors++;
}
if(data62.root_id === undefined){
const err120 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err120];
}
else {
vErrors.push(err120);
}
errors++;
}
if(data62.parent_id === undefined){
const err121 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err121];
}
else {
vErrors.push(err121);
}
errors++;
}
if(data62.name === undefined){
const err122 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err122];
}
else {
vErrors.push(err122);
}
errors++;
}
if(data62.model === undefined){
const err123 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err123];
}
else {
vErrors.push(err123);
}
errors++;
}
if(data62.provider === undefined){
const err124 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err124];
}
else {
vErrors.push(err124);
}
errors++;
}
if(data62.effort === undefined){
const err125 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err125];
}
else {
vErrors.push(err125);
}
errors++;
}
if(data62.cwd === undefined){
const err126 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err126];
}
else {
vErrors.push(err126);
}
errors++;
}
if(data62.report === undefined){
const err127 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "report"},message:"must have required property '"+"report"+"'"};
if(vErrors === null){
vErrors = [err127];
}
else {
vErrors.push(err127);
}
errors++;
}
if(data62.status === undefined){
const err128 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err128];
}
else {
vErrors.push(err128);
}
errors++;
}
if(data62.pending_mail === undefined){
const err129 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "pending_mail"},message:"must have required property '"+"pending_mail"+"'"};
if(vErrors === null){
vErrors = [err129];
}
else {
vErrors.push(err129);
}
errors++;
}
if(data62.lifecycle_phase === undefined){
const err130 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "lifecycle_phase"},message:"must have required property '"+"lifecycle_phase"+"'"};
if(vErrors === null){
vErrors = [err130];
}
else {
vErrors.push(err130);
}
errors++;
}
if(data62.blocking_reason === undefined){
const err131 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "blocking_reason"},message:"must have required property '"+"blocking_reason"+"'"};
if(vErrors === null){
vErrors = [err131];
}
else {
vErrors.push(err131);
}
errors++;
}
if(data62.terminal_cause === undefined){
const err132 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "terminal_cause"},message:"must have required property '"+"terminal_cause"+"'"};
if(vErrors === null){
vErrors = [err132];
}
else {
vErrors.push(err132);
}
errors++;
}
if(data62.allowed_controls === undefined){
const err133 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/required",keyword:"required",params:{missingProperty: "allowed_controls"},message:"must have required property '"+"allowed_controls"+"'"};
if(vErrors === null){
vErrors = [err133];
}
else {
vErrors.push(err133);
}
errors++;
}
if(data62.id !== undefined){
if(typeof data62.id !== "string"){
const err134 = {instancePath:instancePath+"/agents/" + i6+"/id",schemaPath:"#/properties/agents/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err134];
}
else {
vErrors.push(err134);
}
errors++;
}
}
if(data62.root_id !== undefined){
if(typeof data62.root_id !== "string"){
const err135 = {instancePath:instancePath+"/agents/" + i6+"/root_id",schemaPath:"#/properties/agents/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err135];
}
else {
vErrors.push(err135);
}
errors++;
}
}
if(data62.parent_id !== undefined){
if(typeof data62.parent_id !== "string"){
const err136 = {instancePath:instancePath+"/agents/" + i6+"/parent_id",schemaPath:"#/properties/agents/items/properties/parent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err136];
}
else {
vErrors.push(err136);
}
errors++;
}
}
if(data62.name !== undefined){
if(typeof data62.name !== "string"){
const err137 = {instancePath:instancePath+"/agents/" + i6+"/name",schemaPath:"#/properties/agents/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err137];
}
else {
vErrors.push(err137);
}
errors++;
}
}
if(data62.model !== undefined){
if(typeof data62.model !== "string"){
const err138 = {instancePath:instancePath+"/agents/" + i6+"/model",schemaPath:"#/properties/agents/items/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err138];
}
else {
vErrors.push(err138);
}
errors++;
}
}
if(data62.provider !== undefined){
if(typeof data62.provider !== "string"){
const err139 = {instancePath:instancePath+"/agents/" + i6+"/provider",schemaPath:"#/properties/agents/items/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err139];
}
else {
vErrors.push(err139);
}
errors++;
}
}
if(data62.effort !== undefined){
if(typeof data62.effort !== "string"){
const err140 = {instancePath:instancePath+"/agents/" + i6+"/effort",schemaPath:"#/properties/agents/items/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err140];
}
else {
vErrors.push(err140);
}
errors++;
}
}
if(data62.cwd !== undefined){
if(typeof data62.cwd !== "string"){
const err141 = {instancePath:instancePath+"/agents/" + i6+"/cwd",schemaPath:"#/properties/agents/items/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err141];
}
else {
vErrors.push(err141);
}
errors++;
}
}
if(data62.report !== undefined){
if(typeof data62.report !== "string"){
const err142 = {instancePath:instancePath+"/agents/" + i6+"/report",schemaPath:"#/properties/agents/items/properties/report/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err142];
}
else {
vErrors.push(err142);
}
errors++;
}
}
if(data62.status !== undefined){
if(typeof data62.status !== "string"){
const err143 = {instancePath:instancePath+"/agents/" + i6+"/status",schemaPath:"#/properties/agents/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err143];
}
else {
vErrors.push(err143);
}
errors++;
}
}
if(data62.pending_mail !== undefined){
let data73 = data62.pending_mail;
if(!((typeof data73 == "number") && (!(data73 % 1) && !isNaN(data73)))){
const err144 = {instancePath:instancePath+"/agents/" + i6+"/pending_mail",schemaPath:"#/properties/agents/items/properties/pending_mail/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err144];
}
else {
vErrors.push(err144);
}
errors++;
}
}
if(data62.lifecycle_phase !== undefined){
if(typeof data62.lifecycle_phase !== "string"){
const err145 = {instancePath:instancePath+"/agents/" + i6+"/lifecycle_phase",schemaPath:"#/properties/agents/items/properties/lifecycle_phase/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err145];
}
else {
vErrors.push(err145);
}
errors++;
}
}
if(data62.blocking_reason !== undefined){
if(typeof data62.blocking_reason !== "string"){
const err146 = {instancePath:instancePath+"/agents/" + i6+"/blocking_reason",schemaPath:"#/properties/agents/items/properties/blocking_reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err146];
}
else {
vErrors.push(err146);
}
errors++;
}
}
if(data62.terminal_cause !== undefined){
if(typeof data62.terminal_cause !== "string"){
const err147 = {instancePath:instancePath+"/agents/" + i6+"/terminal_cause",schemaPath:"#/properties/agents/items/properties/terminal_cause/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err147];
}
else {
vErrors.push(err147);
}
errors++;
}
}
if(data62.allowed_controls !== undefined){
let data77 = data62.allowed_controls;
if((data77 !== null) && (!(Array.isArray(data77)))){
const err148 = {instancePath:instancePath+"/agents/" + i6+"/allowed_controls",schemaPath:"#/properties/agents/items/properties/allowed_controls/type",keyword:"type",params:{type: schema106.properties.agents.items.properties.allowed_controls.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err148];
}
else {
vErrors.push(err148);
}
errors++;
}
if(Array.isArray(data77)){
const len7 = data77.length;
for(let i7=0; i7<len7; i7++){
if(typeof data77[i7] !== "string"){
const err149 = {instancePath:instancePath+"/agents/" + i6+"/allowed_controls/" + i7,schemaPath:"#/properties/agents/items/properties/allowed_controls/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err149];
}
else {
vErrors.push(err149);
}
errors++;
}
}
}
}
}
else {
const err150 = {instancePath:instancePath+"/agents/" + i6,schemaPath:"#/properties/agents/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err150];
}
else {
vErrors.push(err150);
}
errors++;
}
}
}
}
if(data.inbox !== undefined){
let data79 = data.inbox;
if((data79 !== null) && (!(Array.isArray(data79)))){
const err151 = {instancePath:instancePath+"/inbox",schemaPath:"#/properties/inbox/type",keyword:"type",params:{type: schema106.properties.inbox.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err151];
}
else {
vErrors.push(err151);
}
errors++;
}
if(Array.isArray(data79)){
const len8 = data79.length;
for(let i8=0; i8<len8; i8++){
let data80 = data79[i8];
if(data80 && typeof data80 == "object" && !Array.isArray(data80)){
if(data80.root_id === undefined){
const err152 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err152];
}
else {
vErrors.push(err152);
}
errors++;
}
if(data80.agent_id === undefined){
const err153 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err153];
}
else {
vErrors.push(err153);
}
errors++;
}
if(data80.seq === undefined){
const err154 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "seq"},message:"must have required property '"+"seq"+"'"};
if(vErrors === null){
vErrors = [err154];
}
else {
vErrors.push(err154);
}
errors++;
}
if(data80.kind === undefined){
const err155 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err155];
}
else {
vErrors.push(err155);
}
errors++;
}
if(data80.status === undefined){
const err156 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err156];
}
else {
vErrors.push(err156);
}
errors++;
}
if(data80.payload === undefined){
const err157 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err157];
}
else {
vErrors.push(err157);
}
errors++;
}
if(data80.root_id !== undefined){
if(typeof data80.root_id !== "string"){
const err158 = {instancePath:instancePath+"/inbox/" + i8+"/root_id",schemaPath:"#/properties/inbox/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err158];
}
else {
vErrors.push(err158);
}
errors++;
}
}
if(data80.agent_id !== undefined){
if(typeof data80.agent_id !== "string"){
const err159 = {instancePath:instancePath+"/inbox/" + i8+"/agent_id",schemaPath:"#/properties/inbox/items/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err159];
}
else {
vErrors.push(err159);
}
errors++;
}
}
if(data80.seq !== undefined){
let data83 = data80.seq;
if(typeof data83 === "string"){
if(!pattern0.test(data83)){
const err160 = {instancePath:instancePath+"/inbox/" + i8+"/seq",schemaPath:"#/properties/inbox/items/properties/seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err160];
}
else {
vErrors.push(err160);
}
errors++;
}
if(!(formats0.validate(data83))){
const err161 = {instancePath:instancePath+"/inbox/" + i8+"/seq",schemaPath:"#/properties/inbox/items/properties/seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err161];
}
else {
vErrors.push(err161);
}
errors++;
}
}
else {
const err162 = {instancePath:instancePath+"/inbox/" + i8+"/seq",schemaPath:"#/properties/inbox/items/properties/seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err162];
}
else {
vErrors.push(err162);
}
errors++;
}
}
if(data80.kind !== undefined){
if(typeof data80.kind !== "string"){
const err163 = {instancePath:instancePath+"/inbox/" + i8+"/kind",schemaPath:"#/properties/inbox/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err163];
}
else {
vErrors.push(err163);
}
errors++;
}
}
if(data80.status !== undefined){
if(typeof data80.status !== "string"){
const err164 = {instancePath:instancePath+"/inbox/" + i8+"/status",schemaPath:"#/properties/inbox/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err164];
}
else {
vErrors.push(err164);
}
errors++;
}
}
if(data80.payload !== undefined){
let data86 = data80.payload;
if(data86 && typeof data86 == "object" && !Array.isArray(data86)){
if(data86.reference_id === undefined){
const err165 = {instancePath:instancePath+"/inbox/" + i8+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err165];
}
else {
vErrors.push(err165);
}
errors++;
}
if(data86.digest === undefined){
const err166 = {instancePath:instancePath+"/inbox/" + i8+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err166];
}
else {
vErrors.push(err166);
}
errors++;
}
if(data86.size === undefined){
const err167 = {instancePath:instancePath+"/inbox/" + i8+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err167];
}
else {
vErrors.push(err167);
}
errors++;
}
if(data86.media_type === undefined){
const err168 = {instancePath:instancePath+"/inbox/" + i8+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err168];
}
else {
vErrors.push(err168);
}
errors++;
}
if(data86.source === undefined){
const err169 = {instancePath:instancePath+"/inbox/" + i8+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err169];
}
else {
vErrors.push(err169);
}
errors++;
}
if(data86.text !== undefined){
let data87 = data86.text;
if((data87 !== null) && (typeof data87 !== "string")){
const err170 = {instancePath:instancePath+"/inbox/" + i8+"/payload/text",schemaPath:"#/properties/inbox/items/properties/payload/properties/text/type",keyword:"type",params:{type: schema106.properties.inbox.items.properties.payload.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err170];
}
else {
vErrors.push(err170);
}
errors++;
}
}
if(data86.binary !== undefined){
let data88 = data86.binary;
if((typeof data88 !== "string") && (data88 !== null)){
const err171 = {instancePath:instancePath+"/inbox/" + i8+"/payload/binary",schemaPath:"#/properties/inbox/items/properties/payload/properties/binary/type",keyword:"type",params:{type: schema106.properties.inbox.items.properties.payload.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err171];
}
else {
vErrors.push(err171);
}
errors++;
}
}
if(data86.reference_id !== undefined){
if(typeof data86.reference_id !== "string"){
const err172 = {instancePath:instancePath+"/inbox/" + i8+"/payload/reference_id",schemaPath:"#/properties/inbox/items/properties/payload/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err172];
}
else {
vErrors.push(err172);
}
errors++;
}
}
if(data86.digest !== undefined){
if(typeof data86.digest !== "string"){
const err173 = {instancePath:instancePath+"/inbox/" + i8+"/payload/digest",schemaPath:"#/properties/inbox/items/properties/payload/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err173];
}
else {
vErrors.push(err173);
}
errors++;
}
}
if(data86.size !== undefined){
let data91 = data86.size;
if(typeof data91 === "string"){
if(!pattern0.test(data91)){
const err174 = {instancePath:instancePath+"/inbox/" + i8+"/payload/size",schemaPath:"#/properties/inbox/items/properties/payload/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err174];
}
else {
vErrors.push(err174);
}
errors++;
}
if(!(formats0.validate(data91))){
const err175 = {instancePath:instancePath+"/inbox/" + i8+"/payload/size",schemaPath:"#/properties/inbox/items/properties/payload/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err175];
}
else {
vErrors.push(err175);
}
errors++;
}
}
else {
const err176 = {instancePath:instancePath+"/inbox/" + i8+"/payload/size",schemaPath:"#/properties/inbox/items/properties/payload/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err176];
}
else {
vErrors.push(err176);
}
errors++;
}
}
if(data86.media_type !== undefined){
if(typeof data86.media_type !== "string"){
const err177 = {instancePath:instancePath+"/inbox/" + i8+"/payload/media_type",schemaPath:"#/properties/inbox/items/properties/payload/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err177];
}
else {
vErrors.push(err177);
}
errors++;
}
}
if(data86.source !== undefined){
if(typeof data86.source !== "string"){
const err178 = {instancePath:instancePath+"/inbox/" + i8+"/payload/source",schemaPath:"#/properties/inbox/items/properties/payload/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err178];
}
else {
vErrors.push(err178);
}
errors++;
}
}
}
else {
const err179 = {instancePath:instancePath+"/inbox/" + i8+"/payload",schemaPath:"#/properties/inbox/items/properties/payload/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err179];
}
else {
vErrors.push(err179);
}
errors++;
}
}
}
else {
const err180 = {instancePath:instancePath+"/inbox/" + i8,schemaPath:"#/properties/inbox/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err180];
}
else {
vErrors.push(err180);
}
errors++;
}
}
}
}
if(data.blackboard !== undefined){
let data94 = data.blackboard;
if((data94 !== null) && (!(Array.isArray(data94)))){
const err181 = {instancePath:instancePath+"/blackboard",schemaPath:"#/properties/blackboard/type",keyword:"type",params:{type: schema106.properties.blackboard.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err181];
}
else {
vErrors.push(err181);
}
errors++;
}
if(Array.isArray(data94)){
const len9 = data94.length;
for(let i9=0; i9<len9; i9++){
let data95 = data94[i9];
if(data95 && typeof data95 == "object" && !Array.isArray(data95)){
if(data95.key === undefined){
const err182 = {instancePath:instancePath+"/blackboard/" + i9,schemaPath:"#/properties/blackboard/items/required",keyword:"required",params:{missingProperty: "key"},message:"must have required property '"+"key"+"'"};
if(vErrors === null){
vErrors = [err182];
}
else {
vErrors.push(err182);
}
errors++;
}
if(data95.version === undefined){
const err183 = {instancePath:instancePath+"/blackboard/" + i9,schemaPath:"#/properties/blackboard/items/required",keyword:"required",params:{missingProperty: "version"},message:"must have required property '"+"version"+"'"};
if(vErrors === null){
vErrors = [err183];
}
else {
vErrors.push(err183);
}
errors++;
}
if(data95.author_agent_id === undefined){
const err184 = {instancePath:instancePath+"/blackboard/" + i9,schemaPath:"#/properties/blackboard/items/required",keyword:"required",params:{missingProperty: "author_agent_id"},message:"must have required property '"+"author_agent_id"+"'"};
if(vErrors === null){
vErrors = [err184];
}
else {
vErrors.push(err184);
}
errors++;
}
if(data95.payload === undefined){
const err185 = {instancePath:instancePath+"/blackboard/" + i9,schemaPath:"#/properties/blackboard/items/required",keyword:"required",params:{missingProperty: "payload"},message:"must have required property '"+"payload"+"'"};
if(vErrors === null){
vErrors = [err185];
}
else {
vErrors.push(err185);
}
errors++;
}
if(data95.key !== undefined){
if(typeof data95.key !== "string"){
const err186 = {instancePath:instancePath+"/blackboard/" + i9+"/key",schemaPath:"#/properties/blackboard/items/properties/key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err186];
}
else {
vErrors.push(err186);
}
errors++;
}
}
if(data95.version !== undefined){
let data97 = data95.version;
if(typeof data97 === "string"){
if(!pattern0.test(data97)){
const err187 = {instancePath:instancePath+"/blackboard/" + i9+"/version",schemaPath:"#/properties/blackboard/items/properties/version/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err187];
}
else {
vErrors.push(err187);
}
errors++;
}
if(!(formats0.validate(data97))){
const err188 = {instancePath:instancePath+"/blackboard/" + i9+"/version",schemaPath:"#/properties/blackboard/items/properties/version/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err188];
}
else {
vErrors.push(err188);
}
errors++;
}
}
else {
const err189 = {instancePath:instancePath+"/blackboard/" + i9+"/version",schemaPath:"#/properties/blackboard/items/properties/version/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err189];
}
else {
vErrors.push(err189);
}
errors++;
}
}
if(data95.author_agent_id !== undefined){
if(typeof data95.author_agent_id !== "string"){
const err190 = {instancePath:instancePath+"/blackboard/" + i9+"/author_agent_id",schemaPath:"#/properties/blackboard/items/properties/author_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err190];
}
else {
vErrors.push(err190);
}
errors++;
}
}
if(data95.payload !== undefined){
let data99 = data95.payload;
if(data99 && typeof data99 == "object" && !Array.isArray(data99)){
if(data99.reference_id === undefined){
const err191 = {instancePath:instancePath+"/blackboard/" + i9+"/payload",schemaPath:"#/properties/blackboard/items/properties/payload/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err191];
}
else {
vErrors.push(err191);
}
errors++;
}
if(data99.digest === undefined){
const err192 = {instancePath:instancePath+"/blackboard/" + i9+"/payload",schemaPath:"#/properties/blackboard/items/properties/payload/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err192];
}
else {
vErrors.push(err192);
}
errors++;
}
if(data99.size === undefined){
const err193 = {instancePath:instancePath+"/blackboard/" + i9+"/payload",schemaPath:"#/properties/blackboard/items/properties/payload/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err193];
}
else {
vErrors.push(err193);
}
errors++;
}
if(data99.media_type === undefined){
const err194 = {instancePath:instancePath+"/blackboard/" + i9+"/payload",schemaPath:"#/properties/blackboard/items/properties/payload/required",keyword:"required",params:{missingProperty: "media_type"},message:"must have required property '"+"media_type"+"'"};
if(vErrors === null){
vErrors = [err194];
}
else {
vErrors.push(err194);
}
errors++;
}
if(data99.source === undefined){
const err195 = {instancePath:instancePath+"/blackboard/" + i9+"/payload",schemaPath:"#/properties/blackboard/items/properties/payload/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err195];
}
else {
vErrors.push(err195);
}
errors++;
}
if(data99.text !== undefined){
let data100 = data99.text;
if((data100 !== null) && (typeof data100 !== "string")){
const err196 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/text",schemaPath:"#/properties/blackboard/items/properties/payload/properties/text/type",keyword:"type",params:{type: schema106.properties.blackboard.items.properties.payload.properties.text.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err196];
}
else {
vErrors.push(err196);
}
errors++;
}
}
if(data99.binary !== undefined){
let data101 = data99.binary;
if((typeof data101 !== "string") && (data101 !== null)){
const err197 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/binary",schemaPath:"#/properties/blackboard/items/properties/payload/properties/binary/type",keyword:"type",params:{type: schema106.properties.blackboard.items.properties.payload.properties.binary.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err197];
}
else {
vErrors.push(err197);
}
errors++;
}
}
if(data99.reference_id !== undefined){
if(typeof data99.reference_id !== "string"){
const err198 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/reference_id",schemaPath:"#/properties/blackboard/items/properties/payload/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err198];
}
else {
vErrors.push(err198);
}
errors++;
}
}
if(data99.digest !== undefined){
if(typeof data99.digest !== "string"){
const err199 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/digest",schemaPath:"#/properties/blackboard/items/properties/payload/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err199];
}
else {
vErrors.push(err199);
}
errors++;
}
}
if(data99.size !== undefined){
let data104 = data99.size;
if(typeof data104 === "string"){
if(!pattern0.test(data104)){
const err200 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/size",schemaPath:"#/properties/blackboard/items/properties/payload/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err200];
}
else {
vErrors.push(err200);
}
errors++;
}
if(!(formats0.validate(data104))){
const err201 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/size",schemaPath:"#/properties/blackboard/items/properties/payload/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err201];
}
else {
vErrors.push(err201);
}
errors++;
}
}
else {
const err202 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/size",schemaPath:"#/properties/blackboard/items/properties/payload/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err202];
}
else {
vErrors.push(err202);
}
errors++;
}
}
if(data99.media_type !== undefined){
if(typeof data99.media_type !== "string"){
const err203 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/media_type",schemaPath:"#/properties/blackboard/items/properties/payload/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err203];
}
else {
vErrors.push(err203);
}
errors++;
}
}
if(data99.source !== undefined){
if(typeof data99.source !== "string"){
const err204 = {instancePath:instancePath+"/blackboard/" + i9+"/payload/source",schemaPath:"#/properties/blackboard/items/properties/payload/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err204];
}
else {
vErrors.push(err204);
}
errors++;
}
}
}
else {
const err205 = {instancePath:instancePath+"/blackboard/" + i9+"/payload",schemaPath:"#/properties/blackboard/items/properties/payload/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err205];
}
else {
vErrors.push(err205);
}
errors++;
}
}
}
else {
const err206 = {instancePath:instancePath+"/blackboard/" + i9,schemaPath:"#/properties/blackboard/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err206];
}
else {
vErrors.push(err206);
}
errors++;
}
}
}
}
if(data.budgets !== undefined){
let data107 = data.budgets;
if((data107 !== null) && (!(Array.isArray(data107)))){
const err207 = {instancePath:instancePath+"/budgets",schemaPath:"#/properties/budgets/type",keyword:"type",params:{type: schema106.properties.budgets.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err207];
}
else {
vErrors.push(err207);
}
errors++;
}
if(Array.isArray(data107)){
const len10 = data107.length;
for(let i10=0; i10<len10; i10++){
let data108 = data107[i10];
if(data108 && typeof data108 == "object" && !Array.isArray(data108)){
if(data108.agent_id === undefined){
const err208 = {instancePath:instancePath+"/budgets/" + i10,schemaPath:"#/properties/budgets/items/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err208];
}
else {
vErrors.push(err208);
}
errors++;
}
if(data108.state === undefined){
const err209 = {instancePath:instancePath+"/budgets/" + i10,schemaPath:"#/properties/budgets/items/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err209];
}
else {
vErrors.push(err209);
}
errors++;
}
if(data108.agent_id !== undefined){
if(typeof data108.agent_id !== "string"){
const err210 = {instancePath:instancePath+"/budgets/" + i10+"/agent_id",schemaPath:"#/properties/budgets/items/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err210];
}
else {
vErrors.push(err210);
}
errors++;
}
}
if(data108.state !== undefined){
let data110 = data108.state;
if(data110 && typeof data110 == "object" && !Array.isArray(data110)){
if(data110.kind === undefined){
const err211 = {instancePath:instancePath+"/budgets/" + i10+"/state",schemaPath:"#/properties/budgets/items/properties/state/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err211];
}
else {
vErrors.push(err211);
}
errors++;
}
if(data110.limit === undefined){
const err212 = {instancePath:instancePath+"/budgets/" + i10+"/state",schemaPath:"#/properties/budgets/items/properties/state/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err212];
}
else {
vErrors.push(err212);
}
errors++;
}
if(data110.used === undefined){
const err213 = {instancePath:instancePath+"/budgets/" + i10+"/state",schemaPath:"#/properties/budgets/items/properties/state/required",keyword:"required",params:{missingProperty: "used"},message:"must have required property '"+"used"+"'"};
if(vErrors === null){
vErrors = [err213];
}
else {
vErrors.push(err213);
}
errors++;
}
if(data110.reserved === undefined){
const err214 = {instancePath:instancePath+"/budgets/" + i10+"/state",schemaPath:"#/properties/budgets/items/properties/state/required",keyword:"required",params:{missingProperty: "reserved"},message:"must have required property '"+"reserved"+"'"};
if(vErrors === null){
vErrors = [err214];
}
else {
vErrors.push(err214);
}
errors++;
}
if(data110.remaining === undefined){
const err215 = {instancePath:instancePath+"/budgets/" + i10+"/state",schemaPath:"#/properties/budgets/items/properties/state/required",keyword:"required",params:{missingProperty: "remaining"},message:"must have required property '"+"remaining"+"'"};
if(vErrors === null){
vErrors = [err215];
}
else {
vErrors.push(err215);
}
errors++;
}
if(data110.kind !== undefined){
if(typeof data110.kind !== "string"){
const err216 = {instancePath:instancePath+"/budgets/" + i10+"/state/kind",schemaPath:"#/properties/budgets/items/properties/state/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err216];
}
else {
vErrors.push(err216);
}
errors++;
}
}
if(data110.limit !== undefined){
let data112 = data110.limit;
if(typeof data112 === "string"){
if(!pattern0.test(data112)){
const err217 = {instancePath:instancePath+"/budgets/" + i10+"/state/limit",schemaPath:"#/properties/budgets/items/properties/state/properties/limit/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err217];
}
else {
vErrors.push(err217);
}
errors++;
}
if(!(formats0.validate(data112))){
const err218 = {instancePath:instancePath+"/budgets/" + i10+"/state/limit",schemaPath:"#/properties/budgets/items/properties/state/properties/limit/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err218];
}
else {
vErrors.push(err218);
}
errors++;
}
}
else {
const err219 = {instancePath:instancePath+"/budgets/" + i10+"/state/limit",schemaPath:"#/properties/budgets/items/properties/state/properties/limit/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err219];
}
else {
vErrors.push(err219);
}
errors++;
}
}
if(data110.used !== undefined){
let data113 = data110.used;
if(typeof data113 === "string"){
if(!pattern0.test(data113)){
const err220 = {instancePath:instancePath+"/budgets/" + i10+"/state/used",schemaPath:"#/properties/budgets/items/properties/state/properties/used/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err220];
}
else {
vErrors.push(err220);
}
errors++;
}
if(!(formats0.validate(data113))){
const err221 = {instancePath:instancePath+"/budgets/" + i10+"/state/used",schemaPath:"#/properties/budgets/items/properties/state/properties/used/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err221];
}
else {
vErrors.push(err221);
}
errors++;
}
}
else {
const err222 = {instancePath:instancePath+"/budgets/" + i10+"/state/used",schemaPath:"#/properties/budgets/items/properties/state/properties/used/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err222];
}
else {
vErrors.push(err222);
}
errors++;
}
}
if(data110.reserved !== undefined){
let data114 = data110.reserved;
if(typeof data114 === "string"){
if(!pattern0.test(data114)){
const err223 = {instancePath:instancePath+"/budgets/" + i10+"/state/reserved",schemaPath:"#/properties/budgets/items/properties/state/properties/reserved/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err223];
}
else {
vErrors.push(err223);
}
errors++;
}
if(!(formats0.validate(data114))){
const err224 = {instancePath:instancePath+"/budgets/" + i10+"/state/reserved",schemaPath:"#/properties/budgets/items/properties/state/properties/reserved/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err224];
}
else {
vErrors.push(err224);
}
errors++;
}
}
else {
const err225 = {instancePath:instancePath+"/budgets/" + i10+"/state/reserved",schemaPath:"#/properties/budgets/items/properties/state/properties/reserved/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err225];
}
else {
vErrors.push(err225);
}
errors++;
}
}
if(data110.remaining !== undefined){
let data115 = data110.remaining;
if(typeof data115 === "string"){
if(!pattern0.test(data115)){
const err226 = {instancePath:instancePath+"/budgets/" + i10+"/state/remaining",schemaPath:"#/properties/budgets/items/properties/state/properties/remaining/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err226];
}
else {
vErrors.push(err226);
}
errors++;
}
if(!(formats0.validate(data115))){
const err227 = {instancePath:instancePath+"/budgets/" + i10+"/state/remaining",schemaPath:"#/properties/budgets/items/properties/state/properties/remaining/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err227];
}
else {
vErrors.push(err227);
}
errors++;
}
}
else {
const err228 = {instancePath:instancePath+"/budgets/" + i10+"/state/remaining",schemaPath:"#/properties/budgets/items/properties/state/properties/remaining/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err228];
}
else {
vErrors.push(err228);
}
errors++;
}
}
}
else {
const err229 = {instancePath:instancePath+"/budgets/" + i10+"/state",schemaPath:"#/properties/budgets/items/properties/state/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err229];
}
else {
vErrors.push(err229);
}
errors++;
}
}
}
else {
const err230 = {instancePath:instancePath+"/budgets/" + i10,schemaPath:"#/properties/budgets/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err230];
}
else {
vErrors.push(err230);
}
errors++;
}
}
}
}
if(data.capabilities !== undefined){
let data116 = data.capabilities;
if((data116 !== null) && (!(Array.isArray(data116)))){
const err231 = {instancePath:instancePath+"/capabilities",schemaPath:"#/properties/capabilities/type",keyword:"type",params:{type: schema106.properties.capabilities.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err231];
}
else {
vErrors.push(err231);
}
errors++;
}
if(Array.isArray(data116)){
const len11 = data116.length;
for(let i11=0; i11<len11; i11++){
let data117 = data116[i11];
if(data117 && typeof data117 == "object" && !Array.isArray(data117)){
if(data117.id === undefined){
const err232 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err232];
}
else {
vErrors.push(err232);
}
errors++;
}
if(data117.root_id === undefined){
const err233 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err233];
}
else {
vErrors.push(err233);
}
errors++;
}
if(data117.agent_id === undefined){
const err234 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err234];
}
else {
vErrors.push(err234);
}
errors++;
}
if(data117.issuer_agent_id === undefined){
const err235 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "issuer_agent_id"},message:"must have required property '"+"issuer_agent_id"+"'"};
if(vErrors === null){
vErrors = [err235];
}
else {
vErrors.push(err235);
}
errors++;
}
if(data117.operations === undefined){
const err236 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err236];
}
else {
vErrors.push(err236);
}
errors++;
}
if(data117.scopes === undefined){
const err237 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "scopes"},message:"must have required property '"+"scopes"+"'"};
if(vErrors === null){
vErrors = [err237];
}
else {
vErrors.push(err237);
}
errors++;
}
if(data117.mcp === undefined){
const err238 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "mcp"},message:"must have required property '"+"mcp"+"'"};
if(vErrors === null){
vErrors = [err238];
}
else {
vErrors.push(err238);
}
errors++;
}
if(data117.mcp_all === undefined){
const err239 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "mcp_all"},message:"must have required property '"+"mcp_all"+"'"};
if(vErrors === null){
vErrors = [err239];
}
else {
vErrors.push(err239);
}
errors++;
}
if(data117.generation === undefined){
const err240 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "generation"},message:"must have required property '"+"generation"+"'"};
if(vErrors === null){
vErrors = [err240];
}
else {
vErrors.push(err240);
}
errors++;
}
if(data117.status === undefined){
const err241 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err241];
}
else {
vErrors.push(err241);
}
errors++;
}
if(data117.expires_at === undefined){
const err242 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "expires_at"},message:"must have required property '"+"expires_at"+"'"};
if(vErrors === null){
vErrors = [err242];
}
else {
vErrors.push(err242);
}
errors++;
}
if(data117.created_at === undefined){
const err243 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err243];
}
else {
vErrors.push(err243);
}
errors++;
}
if(data117.updated_at === undefined){
const err244 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/required",keyword:"required",params:{missingProperty: "updated_at"},message:"must have required property '"+"updated_at"+"'"};
if(vErrors === null){
vErrors = [err244];
}
else {
vErrors.push(err244);
}
errors++;
}
if(data117.id !== undefined){
if(typeof data117.id !== "string"){
const err245 = {instancePath:instancePath+"/capabilities/" + i11+"/id",schemaPath:"#/properties/capabilities/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err245];
}
else {
vErrors.push(err245);
}
errors++;
}
}
if(data117.root_id !== undefined){
if(typeof data117.root_id !== "string"){
const err246 = {instancePath:instancePath+"/capabilities/" + i11+"/root_id",schemaPath:"#/properties/capabilities/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err246];
}
else {
vErrors.push(err246);
}
errors++;
}
}
if(data117.agent_id !== undefined){
if(typeof data117.agent_id !== "string"){
const err247 = {instancePath:instancePath+"/capabilities/" + i11+"/agent_id",schemaPath:"#/properties/capabilities/items/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err247];
}
else {
vErrors.push(err247);
}
errors++;
}
}
if(data117.issuer_agent_id !== undefined){
if(typeof data117.issuer_agent_id !== "string"){
const err248 = {instancePath:instancePath+"/capabilities/" + i11+"/issuer_agent_id",schemaPath:"#/properties/capabilities/items/properties/issuer_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err248];
}
else {
vErrors.push(err248);
}
errors++;
}
}
if(data117.operations !== undefined){
let data122 = data117.operations;
if((data122 !== null) && (!(Array.isArray(data122)))){
const err249 = {instancePath:instancePath+"/capabilities/" + i11+"/operations",schemaPath:"#/properties/capabilities/items/properties/operations/type",keyword:"type",params:{type: schema106.properties.capabilities.items.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err249];
}
else {
vErrors.push(err249);
}
errors++;
}
if(Array.isArray(data122)){
const len12 = data122.length;
for(let i12=0; i12<len12; i12++){
if(typeof data122[i12] !== "string"){
const err250 = {instancePath:instancePath+"/capabilities/" + i11+"/operations/" + i12,schemaPath:"#/properties/capabilities/items/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err250];
}
else {
vErrors.push(err250);
}
errors++;
}
}
}
}
if(data117.scopes !== undefined){
let data124 = data117.scopes;
if((data124 !== null) && (!(Array.isArray(data124)))){
const err251 = {instancePath:instancePath+"/capabilities/" + i11+"/scopes",schemaPath:"#/properties/capabilities/items/properties/scopes/type",keyword:"type",params:{type: schema106.properties.capabilities.items.properties.scopes.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err251];
}
else {
vErrors.push(err251);
}
errors++;
}
if(Array.isArray(data124)){
const len13 = data124.length;
for(let i13=0; i13<len13; i13++){
if(typeof data124[i13] !== "string"){
const err252 = {instancePath:instancePath+"/capabilities/" + i11+"/scopes/" + i13,schemaPath:"#/properties/capabilities/items/properties/scopes/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err252];
}
else {
vErrors.push(err252);
}
errors++;
}
}
}
}
if(data117.mcp !== undefined){
let data126 = data117.mcp;
if((data126 !== null) && (!(Array.isArray(data126)))){
const err253 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp",schemaPath:"#/properties/capabilities/items/properties/mcp/type",keyword:"type",params:{type: schema106.properties.capabilities.items.properties.mcp.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err253];
}
else {
vErrors.push(err253);
}
errors++;
}
if(Array.isArray(data126)){
const len14 = data126.length;
for(let i14=0; i14<len14; i14++){
let data127 = data126[i14];
if(data127 && typeof data127 == "object" && !Array.isArray(data127)){
if(data127.server === undefined){
const err254 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14,schemaPath:"#/properties/capabilities/items/properties/mcp/items/required",keyword:"required",params:{missingProperty: "server"},message:"must have required property '"+"server"+"'"};
if(vErrors === null){
vErrors = [err254];
}
else {
vErrors.push(err254);
}
errors++;
}
if(data127.tool === undefined){
const err255 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14,schemaPath:"#/properties/capabilities/items/properties/mcp/items/required",keyword:"required",params:{missingProperty: "tool"},message:"must have required property '"+"tool"+"'"};
if(vErrors === null){
vErrors = [err255];
}
else {
vErrors.push(err255);
}
errors++;
}
if(data127.definition === undefined){
const err256 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14,schemaPath:"#/properties/capabilities/items/properties/mcp/items/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err256];
}
else {
vErrors.push(err256);
}
errors++;
}
if(data127.server !== undefined){
if(typeof data127.server !== "string"){
const err257 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14+"/server",schemaPath:"#/properties/capabilities/items/properties/mcp/items/properties/server/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err257];
}
else {
vErrors.push(err257);
}
errors++;
}
}
if(data127.tool !== undefined){
if(typeof data127.tool !== "string"){
const err258 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14+"/tool",schemaPath:"#/properties/capabilities/items/properties/mcp/items/properties/tool/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err258];
}
else {
vErrors.push(err258);
}
errors++;
}
}
if(data127.definition !== undefined){
if(typeof data127.definition !== "string"){
const err259 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14+"/definition",schemaPath:"#/properties/capabilities/items/properties/mcp/items/properties/definition/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err259];
}
else {
vErrors.push(err259);
}
errors++;
}
}
}
else {
const err260 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp/" + i14,schemaPath:"#/properties/capabilities/items/properties/mcp/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err260];
}
else {
vErrors.push(err260);
}
errors++;
}
}
}
}
if(data117.mcp_all !== undefined){
if(typeof data117.mcp_all !== "boolean"){
const err261 = {instancePath:instancePath+"/capabilities/" + i11+"/mcp_all",schemaPath:"#/properties/capabilities/items/properties/mcp_all/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err261];
}
else {
vErrors.push(err261);
}
errors++;
}
}
if(data117.generation !== undefined){
let data132 = data117.generation;
if(typeof data132 === "string"){
if(!pattern0.test(data132)){
const err262 = {instancePath:instancePath+"/capabilities/" + i11+"/generation",schemaPath:"#/properties/capabilities/items/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err262];
}
else {
vErrors.push(err262);
}
errors++;
}
if(!(formats0.validate(data132))){
const err263 = {instancePath:instancePath+"/capabilities/" + i11+"/generation",schemaPath:"#/properties/capabilities/items/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err263];
}
else {
vErrors.push(err263);
}
errors++;
}
}
else {
const err264 = {instancePath:instancePath+"/capabilities/" + i11+"/generation",schemaPath:"#/properties/capabilities/items/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err264];
}
else {
vErrors.push(err264);
}
errors++;
}
}
if(data117.status !== undefined){
if(typeof data117.status !== "string"){
const err265 = {instancePath:instancePath+"/capabilities/" + i11+"/status",schemaPath:"#/properties/capabilities/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err265];
}
else {
vErrors.push(err265);
}
errors++;
}
}
if(data117.expires_at !== undefined){
if(typeof data117.expires_at !== "string"){
const err266 = {instancePath:instancePath+"/capabilities/" + i11+"/expires_at",schemaPath:"#/properties/capabilities/items/properties/expires_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err266];
}
else {
vErrors.push(err266);
}
errors++;
}
}
if(data117.created_at !== undefined){
if(typeof data117.created_at !== "string"){
const err267 = {instancePath:instancePath+"/capabilities/" + i11+"/created_at",schemaPath:"#/properties/capabilities/items/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err267];
}
else {
vErrors.push(err267);
}
errors++;
}
}
if(data117.updated_at !== undefined){
if(typeof data117.updated_at !== "string"){
const err268 = {instancePath:instancePath+"/capabilities/" + i11+"/updated_at",schemaPath:"#/properties/capabilities/items/properties/updated_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err268];
}
else {
vErrors.push(err268);
}
errors++;
}
}
}
else {
const err269 = {instancePath:instancePath+"/capabilities/" + i11,schemaPath:"#/properties/capabilities/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err269];
}
else {
vErrors.push(err269);
}
errors++;
}
}
}
}
if(data.schedules !== undefined){
let data137 = data.schedules;
if((data137 !== null) && (!(Array.isArray(data137)))){
const err270 = {instancePath:instancePath+"/schedules",schemaPath:"#/properties/schedules/type",keyword:"type",params:{type: schema106.properties.schedules.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err270];
}
else {
vErrors.push(err270);
}
errors++;
}
if(Array.isArray(data137)){
const len15 = data137.length;
for(let i15=0; i15<len15; i15++){
let data138 = data137[i15];
if(data138 && typeof data138 == "object" && !Array.isArray(data138)){
if(data138.id === undefined){
const err271 = {instancePath:instancePath+"/schedules/" + i15,schemaPath:"#/properties/schedules/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err271];
}
else {
vErrors.push(err271);
}
errors++;
}
if(data138.schedule === undefined){
const err272 = {instancePath:instancePath+"/schedules/" + i15,schemaPath:"#/properties/schedules/items/required",keyword:"required",params:{missingProperty: "schedule"},message:"must have required property '"+"schedule"+"'"};
if(vErrors === null){
vErrors = [err272];
}
else {
vErrors.push(err272);
}
errors++;
}
if(data138.prompt === undefined){
const err273 = {instancePath:instancePath+"/schedules/" + i15,schemaPath:"#/properties/schedules/items/required",keyword:"required",params:{missingProperty: "prompt"},message:"must have required property '"+"prompt"+"'"};
if(vErrors === null){
vErrors = [err273];
}
else {
vErrors.push(err273);
}
errors++;
}
if(data138.anchor === undefined){
const err274 = {instancePath:instancePath+"/schedules/" + i15,schemaPath:"#/properties/schedules/items/required",keyword:"required",params:{missingProperty: "anchor"},message:"must have required property '"+"anchor"+"'"};
if(vErrors === null){
vErrors = [err274];
}
else {
vErrors.push(err274);
}
errors++;
}
if(data138.last_fire === undefined){
const err275 = {instancePath:instancePath+"/schedules/" + i15,schemaPath:"#/properties/schedules/items/required",keyword:"required",params:{missingProperty: "last_fire"},message:"must have required property '"+"last_fire"+"'"};
if(vErrors === null){
vErrors = [err275];
}
else {
vErrors.push(err275);
}
errors++;
}
if(data138.id !== undefined){
let data139 = data138.id;
if(!((typeof data139 == "number") && (!(data139 % 1) && !isNaN(data139)))){
const err276 = {instancePath:instancePath+"/schedules/" + i15+"/id",schemaPath:"#/properties/schedules/items/properties/id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err276];
}
else {
vErrors.push(err276);
}
errors++;
}
}
if(data138.schedule !== undefined){
if(typeof data138.schedule !== "string"){
const err277 = {instancePath:instancePath+"/schedules/" + i15+"/schedule",schemaPath:"#/properties/schedules/items/properties/schedule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err277];
}
else {
vErrors.push(err277);
}
errors++;
}
}
if(data138.prompt !== undefined){
if(typeof data138.prompt !== "string"){
const err278 = {instancePath:instancePath+"/schedules/" + i15+"/prompt",schemaPath:"#/properties/schedules/items/properties/prompt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err278];
}
else {
vErrors.push(err278);
}
errors++;
}
}
if(data138.anchor !== undefined){
if(typeof data138.anchor !== "string"){
const err279 = {instancePath:instancePath+"/schedules/" + i15+"/anchor",schemaPath:"#/properties/schedules/items/properties/anchor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err279];
}
else {
vErrors.push(err279);
}
errors++;
}
}
if(data138.last_fire !== undefined){
if(typeof data138.last_fire !== "string"){
const err280 = {instancePath:instancePath+"/schedules/" + i15+"/last_fire",schemaPath:"#/properties/schedules/items/properties/last_fire/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err280];
}
else {
vErrors.push(err280);
}
errors++;
}
}
}
else {
const err281 = {instancePath:instancePath+"/schedules/" + i15,schemaPath:"#/properties/schedules/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err281];
}
else {
vErrors.push(err281);
}
errors++;
}
}
}
}
if(data.permissions !== undefined){
let data144 = data.permissions;
if((data144 !== null) && (!(Array.isArray(data144)))){
const err282 = {instancePath:instancePath+"/permissions",schemaPath:"#/properties/permissions/type",keyword:"type",params:{type: schema106.properties.permissions.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err282];
}
else {
vErrors.push(err282);
}
errors++;
}
if(Array.isArray(data144)){
const len16 = data144.length;
for(let i16=0; i16<len16; i16++){
let data145 = data144[i16];
if(data145 && typeof data145 == "object" && !Array.isArray(data145)){
if(data145.id === undefined){
const err283 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err283];
}
else {
vErrors.push(err283);
}
errors++;
}
if(data145.agent_id === undefined){
const err284 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "agent_id"},message:"must have required property '"+"agent_id"+"'"};
if(vErrors === null){
vErrors = [err284];
}
else {
vErrors.push(err284);
}
errors++;
}
if(data145.operation_id === undefined){
const err285 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "operation_id"},message:"must have required property '"+"operation_id"+"'"};
if(vErrors === null){
vErrors = [err285];
}
else {
vErrors.push(err285);
}
errors++;
}
if(data145.operation === undefined){
const err286 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "operation"},message:"must have required property '"+"operation"+"'"};
if(vErrors === null){
vErrors = [err286];
}
else {
vErrors.push(err286);
}
errors++;
}
if(data145.canonical_path === undefined){
const err287 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "canonical_path"},message:"must have required property '"+"canonical_path"+"'"};
if(vErrors === null){
vErrors = [err287];
}
else {
vErrors.push(err287);
}
errors++;
}
if(data145.request_digest === undefined){
const err288 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "request_digest"},message:"must have required property '"+"request_digest"+"'"};
if(vErrors === null){
vErrors = [err288];
}
else {
vErrors.push(err288);
}
errors++;
}
if(data145.capability_id === undefined){
const err289 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "capability_id"},message:"must have required property '"+"capability_id"+"'"};
if(vErrors === null){
vErrors = [err289];
}
else {
vErrors.push(err289);
}
errors++;
}
if(data145.capability_generation === undefined){
const err290 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "capability_generation"},message:"must have required property '"+"capability_generation"+"'"};
if(vErrors === null){
vErrors = [err290];
}
else {
vErrors.push(err290);
}
errors++;
}
if(data145.status === undefined){
const err291 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "status"},message:"must have required property '"+"status"+"'"};
if(vErrors === null){
vErrors = [err291];
}
else {
vErrors.push(err291);
}
errors++;
}
if(data145.command === undefined){
const err292 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "command"},message:"must have required property '"+"command"+"'"};
if(vErrors === null){
vErrors = [err292];
}
else {
vErrors.push(err292);
}
errors++;
}
if(data145.rule === undefined){
const err293 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/required",keyword:"required",params:{missingProperty: "rule"},message:"must have required property '"+"rule"+"'"};
if(vErrors === null){
vErrors = [err293];
}
else {
vErrors.push(err293);
}
errors++;
}
if(data145.id !== undefined){
if(typeof data145.id !== "string"){
const err294 = {instancePath:instancePath+"/permissions/" + i16+"/id",schemaPath:"#/properties/permissions/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err294];
}
else {
vErrors.push(err294);
}
errors++;
}
}
if(data145.agent_id !== undefined){
if(typeof data145.agent_id !== "string"){
const err295 = {instancePath:instancePath+"/permissions/" + i16+"/agent_id",schemaPath:"#/properties/permissions/items/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err295];
}
else {
vErrors.push(err295);
}
errors++;
}
}
if(data145.operation_id !== undefined){
if(typeof data145.operation_id !== "string"){
const err296 = {instancePath:instancePath+"/permissions/" + i16+"/operation_id",schemaPath:"#/properties/permissions/items/properties/operation_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err296];
}
else {
vErrors.push(err296);
}
errors++;
}
}
if(data145.operation !== undefined){
if(typeof data145.operation !== "string"){
const err297 = {instancePath:instancePath+"/permissions/" + i16+"/operation",schemaPath:"#/properties/permissions/items/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err297];
}
else {
vErrors.push(err297);
}
errors++;
}
}
if(data145.canonical_path !== undefined){
if(typeof data145.canonical_path !== "string"){
const err298 = {instancePath:instancePath+"/permissions/" + i16+"/canonical_path",schemaPath:"#/properties/permissions/items/properties/canonical_path/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err298];
}
else {
vErrors.push(err298);
}
errors++;
}
}
if(data145.request_digest !== undefined){
if(typeof data145.request_digest !== "string"){
const err299 = {instancePath:instancePath+"/permissions/" + i16+"/request_digest",schemaPath:"#/properties/permissions/items/properties/request_digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err299];
}
else {
vErrors.push(err299);
}
errors++;
}
}
if(data145.capability_id !== undefined){
if(typeof data145.capability_id !== "string"){
const err300 = {instancePath:instancePath+"/permissions/" + i16+"/capability_id",schemaPath:"#/properties/permissions/items/properties/capability_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err300];
}
else {
vErrors.push(err300);
}
errors++;
}
}
if(data145.capability_generation !== undefined){
let data153 = data145.capability_generation;
if(typeof data153 === "string"){
if(!pattern0.test(data153)){
const err301 = {instancePath:instancePath+"/permissions/" + i16+"/capability_generation",schemaPath:"#/properties/permissions/items/properties/capability_generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err301];
}
else {
vErrors.push(err301);
}
errors++;
}
if(!(formats0.validate(data153))){
const err302 = {instancePath:instancePath+"/permissions/" + i16+"/capability_generation",schemaPath:"#/properties/permissions/items/properties/capability_generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err302];
}
else {
vErrors.push(err302);
}
errors++;
}
}
else {
const err303 = {instancePath:instancePath+"/permissions/" + i16+"/capability_generation",schemaPath:"#/properties/permissions/items/properties/capability_generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err303];
}
else {
vErrors.push(err303);
}
errors++;
}
}
if(data145.status !== undefined){
if(typeof data145.status !== "string"){
const err304 = {instancePath:instancePath+"/permissions/" + i16+"/status",schemaPath:"#/properties/permissions/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err304];
}
else {
vErrors.push(err304);
}
errors++;
}
}
if(data145.command !== undefined){
if(typeof data145.command !== "string"){
const err305 = {instancePath:instancePath+"/permissions/" + i16+"/command",schemaPath:"#/properties/permissions/items/properties/command/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err305];
}
else {
vErrors.push(err305);
}
errors++;
}
}
if(data145.rule !== undefined){
if(typeof data145.rule !== "string"){
const err306 = {instancePath:instancePath+"/permissions/" + i16+"/rule",schemaPath:"#/properties/permissions/items/properties/rule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err306];
}
else {
vErrors.push(err306);
}
errors++;
}
}
}
else {
const err307 = {instancePath:instancePath+"/permissions/" + i16,schemaPath:"#/properties/permissions/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err307];
}
else {
vErrors.push(err307);
}
errors++;
}
}
}
}
if(data.questions !== undefined){
let data157 = data.questions;
if((data157 !== null) && (!(Array.isArray(data157)))){
const err308 = {instancePath:instancePath+"/questions",schemaPath:"#/properties/questions/type",keyword:"type",params:{type: schema106.properties.questions.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err308];
}
else {
vErrors.push(err308);
}
errors++;
}
if(Array.isArray(data157)){
const len17 = data157.length;
for(let i17=0; i17<len17; i17++){
let data158 = data157[i17];
if(data158 && typeof data158 == "object" && !Array.isArray(data158)){
if(data158.turn_id !== undefined){
if(typeof data158.turn_id !== "string"){
const err309 = {instancePath:instancePath+"/questions/" + i17+"/turn_id",schemaPath:"#/properties/questions/items/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err309];
}
else {
vErrors.push(err309);
}
errors++;
}
}
if(data158.root_id !== undefined){
if(typeof data158.root_id !== "string"){
const err310 = {instancePath:instancePath+"/questions/" + i17+"/root_id",schemaPath:"#/properties/questions/items/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err310];
}
else {
vErrors.push(err310);
}
errors++;
}
}
if(data158.agent_id !== undefined){
if(typeof data158.agent_id !== "string"){
const err311 = {instancePath:instancePath+"/questions/" + i17+"/agent_id",schemaPath:"#/properties/questions/items/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err311];
}
else {
vErrors.push(err311);
}
errors++;
}
}
if(data158.sender_agent_id !== undefined){
if(typeof data158.sender_agent_id !== "string"){
const err312 = {instancePath:instancePath+"/questions/" + i17+"/sender_agent_id",schemaPath:"#/properties/questions/items/properties/sender_agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err312];
}
else {
vErrors.push(err312);
}
errors++;
}
}
if(data158.inbox_seq !== undefined){
let data163 = data158.inbox_seq;
if(typeof data163 === "string"){
if(!pattern0.test(data163)){
const err313 = {instancePath:instancePath+"/questions/" + i17+"/inbox_seq",schemaPath:"#/properties/questions/items/properties/inbox_seq/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err313];
}
else {
vErrors.push(err313);
}
errors++;
}
if(!(formats0.validate(data163))){
const err314 = {instancePath:instancePath+"/questions/" + i17+"/inbox_seq",schemaPath:"#/properties/questions/items/properties/inbox_seq/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err314];
}
else {
vErrors.push(err314);
}
errors++;
}
}
else {
const err315 = {instancePath:instancePath+"/questions/" + i17+"/inbox_seq",schemaPath:"#/properties/questions/items/properties/inbox_seq/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err315];
}
else {
vErrors.push(err315);
}
errors++;
}
}
if(data158.inbox_kind !== undefined){
if(typeof data158.inbox_kind !== "string"){
const err316 = {instancePath:instancePath+"/questions/" + i17+"/inbox_kind",schemaPath:"#/properties/questions/items/properties/inbox_kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err316];
}
else {
vErrors.push(err316);
}
errors++;
}
}
if(data158.delivery !== undefined){
if(typeof data158.delivery !== "string"){
const err317 = {instancePath:instancePath+"/questions/" + i17+"/delivery",schemaPath:"#/properties/questions/items/properties/delivery/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err317];
}
else {
vErrors.push(err317);
}
errors++;
}
}
if(data158.message_id !== undefined){
if(typeof data158.message_id !== "string"){
const err318 = {instancePath:instancePath+"/questions/" + i17+"/message_id",schemaPath:"#/properties/questions/items/properties/message_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err318];
}
else {
vErrors.push(err318);
}
errors++;
}
}
if(data158.phase !== undefined){
if(typeof data158.phase !== "string"){
const err319 = {instancePath:instancePath+"/questions/" + i17+"/phase",schemaPath:"#/properties/questions/items/properties/phase/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err319];
}
else {
vErrors.push(err319);
}
errors++;
}
}
if(data158.status !== undefined){
if(typeof data158.status !== "string"){
const err320 = {instancePath:instancePath+"/questions/" + i17+"/status",schemaPath:"#/properties/questions/items/properties/status/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err320];
}
else {
vErrors.push(err320);
}
errors++;
}
}
if(data158.terminal_cause !== undefined){
if(typeof data158.terminal_cause !== "string"){
const err321 = {instancePath:instancePath+"/questions/" + i17+"/terminal_cause",schemaPath:"#/properties/questions/items/properties/terminal_cause/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err321];
}
else {
vErrors.push(err321);
}
errors++;
}
}
if(data158.command_client_id !== undefined){
if(typeof data158.command_client_id !== "string"){
const err322 = {instancePath:instancePath+"/questions/" + i17+"/command_client_id",schemaPath:"#/properties/questions/items/properties/command_client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err322];
}
else {
vErrors.push(err322);
}
errors++;
}
}
if(data158.command_id !== undefined){
if(typeof data158.command_id !== "string"){
const err323 = {instancePath:instancePath+"/questions/" + i17+"/command_id",schemaPath:"#/properties/questions/items/properties/command_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err323];
}
else {
vErrors.push(err323);
}
errors++;
}
}
if(data158.operation_id !== undefined){
if(typeof data158.operation_id !== "string"){
const err324 = {instancePath:instancePath+"/questions/" + i17+"/operation_id",schemaPath:"#/properties/questions/items/properties/operation_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err324];
}
else {
vErrors.push(err324);
}
errors++;
}
}
if(data158.trace_id !== undefined){
if(typeof data158.trace_id !== "string"){
const err325 = {instancePath:instancePath+"/questions/" + i17+"/trace_id",schemaPath:"#/properties/questions/items/properties/trace_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err325];
}
else {
vErrors.push(err325);
}
errors++;
}
}
if(data158.schedule_id !== undefined){
let data174 = data158.schedule_id;
if(!((typeof data174 == "number") && (!(data174 % 1) && !isNaN(data174)))){
const err326 = {instancePath:instancePath+"/questions/" + i17+"/schedule_id",schemaPath:"#/properties/questions/items/properties/schedule_id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err326];
}
else {
vErrors.push(err326);
}
errors++;
}
}
if(data158.slot !== undefined){
if(typeof data158.slot !== "string"){
const err327 = {instancePath:instancePath+"/questions/" + i17+"/slot",schemaPath:"#/properties/questions/items/properties/slot/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err327];
}
else {
vErrors.push(err327);
}
errors++;
}
}
if(data158.error !== undefined){
if(typeof data158.error !== "string"){
const err328 = {instancePath:instancePath+"/questions/" + i17+"/error",schemaPath:"#/properties/questions/items/properties/error/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err328];
}
else {
vErrors.push(err328);
}
errors++;
}
}
if(data158.acknowledged_inbox !== undefined){
let data177 = data158.acknowledged_inbox;
if(Array.isArray(data177)){
const len18 = data177.length;
for(let i18=0; i18<len18; i18++){
let data178 = data177[i18];
if(typeof data178 === "string"){
if(!pattern0.test(data178)){
const err329 = {instancePath:instancePath+"/questions/" + i17+"/acknowledged_inbox/" + i18,schemaPath:"#/properties/questions/items/properties/acknowledged_inbox/items/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err329];
}
else {
vErrors.push(err329);
}
errors++;
}
if(!(formats0.validate(data178))){
const err330 = {instancePath:instancePath+"/questions/" + i17+"/acknowledged_inbox/" + i18,schemaPath:"#/properties/questions/items/properties/acknowledged_inbox/items/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err330];
}
else {
vErrors.push(err330);
}
errors++;
}
}
else {
const err331 = {instancePath:instancePath+"/questions/" + i17+"/acknowledged_inbox/" + i18,schemaPath:"#/properties/questions/items/properties/acknowledged_inbox/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err331];
}
else {
vErrors.push(err331);
}
errors++;
}
}
}
else {
const err332 = {instancePath:instancePath+"/questions/" + i17+"/acknowledged_inbox",schemaPath:"#/properties/questions/items/properties/acknowledged_inbox/type",keyword:"type",params:{type: "array"},message:"must be array"};
if(vErrors === null){
vErrors = [err332];
}
else {
vErrors.push(err332);
}
errors++;
}
}
if(data158.subscription_id !== undefined){
if(typeof data158.subscription_id !== "string"){
const err333 = {instancePath:instancePath+"/questions/" + i17+"/subscription_id",schemaPath:"#/properties/questions/items/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err333];
}
else {
vErrors.push(err333);
}
errors++;
}
}
if(data158.key !== undefined){
if(typeof data158.key !== "string"){
const err334 = {instancePath:instancePath+"/questions/" + i17+"/key",schemaPath:"#/properties/questions/items/properties/key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err334];
}
else {
vErrors.push(err334);
}
errors++;
}
}
if(data158.version !== undefined){
let data181 = data158.version;
if(typeof data181 === "string"){
if(!pattern0.test(data181)){
const err335 = {instancePath:instancePath+"/questions/" + i17+"/version",schemaPath:"#/properties/questions/items/properties/version/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err335];
}
else {
vErrors.push(err335);
}
errors++;
}
if(!(formats0.validate(data181))){
const err336 = {instancePath:instancePath+"/questions/" + i17+"/version",schemaPath:"#/properties/questions/items/properties/version/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err336];
}
else {
vErrors.push(err336);
}
errors++;
}
}
else {
const err337 = {instancePath:instancePath+"/questions/" + i17+"/version",schemaPath:"#/properties/questions/items/properties/version/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err337];
}
else {
vErrors.push(err337);
}
errors++;
}
}
if(data158.expected_version !== undefined){
let data182 = data158.expected_version;
if(typeof data182 === "string"){
if(!pattern0.test(data182)){
const err338 = {instancePath:instancePath+"/questions/" + i17+"/expected_version",schemaPath:"#/properties/questions/items/properties/expected_version/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err338];
}
else {
vErrors.push(err338);
}
errors++;
}
if(!(formats0.validate(data182))){
const err339 = {instancePath:instancePath+"/questions/" + i17+"/expected_version",schemaPath:"#/properties/questions/items/properties/expected_version/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err339];
}
else {
vErrors.push(err339);
}
errors++;
}
}
else {
const err340 = {instancePath:instancePath+"/questions/" + i17+"/expected_version",schemaPath:"#/properties/questions/items/properties/expected_version/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err340];
}
else {
vErrors.push(err340);
}
errors++;
}
}
if(data158.restored !== undefined){
let data183 = data158.restored;
if((data183 !== null) && (!(Array.isArray(data183)))){
const err341 = {instancePath:instancePath+"/questions/" + i17+"/restored",schemaPath:"#/properties/questions/items/properties/restored/type",keyword:"type",params:{type: schema106.properties.questions.items.properties.restored.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err341];
}
else {
vErrors.push(err341);
}
errors++;
}
if(Array.isArray(data183)){
const len19 = data183.length;
for(let i19=0; i19<len19; i19++){
if(typeof data183[i19] !== "string"){
const err342 = {instancePath:instancePath+"/questions/" + i17+"/restored/" + i19,schemaPath:"#/properties/questions/items/properties/restored/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err342];
}
else {
vErrors.push(err342);
}
errors++;
}
}
}
}
if(data158.not_restored !== undefined){
let data185 = data158.not_restored;
if((data185 !== null) && (!(Array.isArray(data185)))){
const err343 = {instancePath:instancePath+"/questions/" + i17+"/not_restored",schemaPath:"#/properties/questions/items/properties/not_restored/type",keyword:"type",params:{type: schema106.properties.questions.items.properties.not_restored.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err343];
}
else {
vErrors.push(err343);
}
errors++;
}
if(Array.isArray(data185)){
const len20 = data185.length;
for(let i20=0; i20<len20; i20++){
let data186 = data185[i20];
if(data186 && typeof data186 == "object" && !Array.isArray(data186)){
if(data186.name === undefined){
const err344 = {instancePath:instancePath+"/questions/" + i17+"/not_restored/" + i20,schemaPath:"#/properties/questions/items/properties/not_restored/items/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err344];
}
else {
vErrors.push(err344);
}
errors++;
}
if(data186.reason === undefined){
const err345 = {instancePath:instancePath+"/questions/" + i17+"/not_restored/" + i20,schemaPath:"#/properties/questions/items/properties/not_restored/items/required",keyword:"required",params:{missingProperty: "reason"},message:"must have required property '"+"reason"+"'"};
if(vErrors === null){
vErrors = [err345];
}
else {
vErrors.push(err345);
}
errors++;
}
if(data186.name !== undefined){
if(typeof data186.name !== "string"){
const err346 = {instancePath:instancePath+"/questions/" + i17+"/not_restored/" + i20+"/name",schemaPath:"#/properties/questions/items/properties/not_restored/items/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err346];
}
else {
vErrors.push(err346);
}
errors++;
}
}
if(data186.reason !== undefined){
if(typeof data186.reason !== "string"){
const err347 = {instancePath:instancePath+"/questions/" + i17+"/not_restored/" + i20+"/reason",schemaPath:"#/properties/questions/items/properties/not_restored/items/properties/reason/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err347];
}
else {
vErrors.push(err347);
}
errors++;
}
}
}
else {
const err348 = {instancePath:instancePath+"/questions/" + i17+"/not_restored/" + i20,schemaPath:"#/properties/questions/items/properties/not_restored/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err348];
}
else {
vErrors.push(err348);
}
errors++;
}
}
}
}
if(data158.attempt !== undefined){
if(typeof data158.attempt !== "string"){
const err349 = {instancePath:instancePath+"/questions/" + i17+"/attempt",schemaPath:"#/properties/questions/items/properties/attempt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err349];
}
else {
vErrors.push(err349);
}
errors++;
}
}
if(data158.budget_kind !== undefined){
if(typeof data158.budget_kind !== "string"){
const err350 = {instancePath:instancePath+"/questions/" + i17+"/budget_kind",schemaPath:"#/properties/questions/items/properties/budget_kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err350];
}
else {
vErrors.push(err350);
}
errors++;
}
}
if(data158.amount !== undefined){
let data191 = data158.amount;
if(typeof data191 === "string"){
if(!pattern0.test(data191)){
const err351 = {instancePath:instancePath+"/questions/" + i17+"/amount",schemaPath:"#/properties/questions/items/properties/amount/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err351];
}
else {
vErrors.push(err351);
}
errors++;
}
if(!(formats0.validate(data191))){
const err352 = {instancePath:instancePath+"/questions/" + i17+"/amount",schemaPath:"#/properties/questions/items/properties/amount/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err352];
}
else {
vErrors.push(err352);
}
errors++;
}
}
else {
const err353 = {instancePath:instancePath+"/questions/" + i17+"/amount",schemaPath:"#/properties/questions/items/properties/amount/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err353];
}
else {
vErrors.push(err353);
}
errors++;
}
}
if(data158.limit !== undefined){
let data192 = data158.limit;
if(typeof data192 === "string"){
if(!pattern0.test(data192)){
const err354 = {instancePath:instancePath+"/questions/" + i17+"/limit",schemaPath:"#/properties/questions/items/properties/limit/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err354];
}
else {
vErrors.push(err354);
}
errors++;
}
if(!(formats0.validate(data192))){
const err355 = {instancePath:instancePath+"/questions/" + i17+"/limit",schemaPath:"#/properties/questions/items/properties/limit/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err355];
}
else {
vErrors.push(err355);
}
errors++;
}
}
else {
const err356 = {instancePath:instancePath+"/questions/" + i17+"/limit",schemaPath:"#/properties/questions/items/properties/limit/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err356];
}
else {
vErrors.push(err356);
}
errors++;
}
}
if(data158.used !== undefined){
let data193 = data158.used;
if(typeof data193 === "string"){
if(!pattern0.test(data193)){
const err357 = {instancePath:instancePath+"/questions/" + i17+"/used",schemaPath:"#/properties/questions/items/properties/used/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err357];
}
else {
vErrors.push(err357);
}
errors++;
}
if(!(formats0.validate(data193))){
const err358 = {instancePath:instancePath+"/questions/" + i17+"/used",schemaPath:"#/properties/questions/items/properties/used/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err358];
}
else {
vErrors.push(err358);
}
errors++;
}
}
else {
const err359 = {instancePath:instancePath+"/questions/" + i17+"/used",schemaPath:"#/properties/questions/items/properties/used/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err359];
}
else {
vErrors.push(err359);
}
errors++;
}
}
if(data158.reserved !== undefined){
let data194 = data158.reserved;
if(typeof data194 === "string"){
if(!pattern0.test(data194)){
const err360 = {instancePath:instancePath+"/questions/" + i17+"/reserved",schemaPath:"#/properties/questions/items/properties/reserved/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err360];
}
else {
vErrors.push(err360);
}
errors++;
}
if(!(formats0.validate(data194))){
const err361 = {instancePath:instancePath+"/questions/" + i17+"/reserved",schemaPath:"#/properties/questions/items/properties/reserved/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err361];
}
else {
vErrors.push(err361);
}
errors++;
}
}
else {
const err362 = {instancePath:instancePath+"/questions/" + i17+"/reserved",schemaPath:"#/properties/questions/items/properties/reserved/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err362];
}
else {
vErrors.push(err362);
}
errors++;
}
}
if(data158.capability_id !== undefined){
if(typeof data158.capability_id !== "string"){
const err363 = {instancePath:instancePath+"/questions/" + i17+"/capability_id",schemaPath:"#/properties/questions/items/properties/capability_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err363];
}
else {
vErrors.push(err363);
}
errors++;
}
}
if(data158.generation !== undefined){
let data196 = data158.generation;
if(typeof data196 === "string"){
if(!pattern0.test(data196)){
const err364 = {instancePath:instancePath+"/questions/" + i17+"/generation",schemaPath:"#/properties/questions/items/properties/generation/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err364];
}
else {
vErrors.push(err364);
}
errors++;
}
if(!(formats0.validate(data196))){
const err365 = {instancePath:instancePath+"/questions/" + i17+"/generation",schemaPath:"#/properties/questions/items/properties/generation/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err365];
}
else {
vErrors.push(err365);
}
errors++;
}
}
else {
const err366 = {instancePath:instancePath+"/questions/" + i17+"/generation",schemaPath:"#/properties/questions/items/properties/generation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err366];
}
else {
vErrors.push(err366);
}
errors++;
}
}
if(data158.permission_id !== undefined){
if(typeof data158.permission_id !== "string"){
const err367 = {instancePath:instancePath+"/questions/" + i17+"/permission_id",schemaPath:"#/properties/questions/items/properties/permission_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err367];
}
else {
vErrors.push(err367);
}
errors++;
}
}
if(data158.operation !== undefined){
if(typeof data158.operation !== "string"){
const err368 = {instancePath:instancePath+"/questions/" + i17+"/operation",schemaPath:"#/properties/questions/items/properties/operation/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err368];
}
else {
vErrors.push(err368);
}
errors++;
}
}
if(data158.canonical_path !== undefined){
if(typeof data158.canonical_path !== "string"){
const err369 = {instancePath:instancePath+"/questions/" + i17+"/canonical_path",schemaPath:"#/properties/questions/items/properties/canonical_path/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err369];
}
else {
vErrors.push(err369);
}
errors++;
}
}
if(data158.request_digest !== undefined){
if(typeof data158.request_digest !== "string"){
const err370 = {instancePath:instancePath+"/questions/" + i17+"/request_digest",schemaPath:"#/properties/questions/items/properties/request_digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err370];
}
else {
vErrors.push(err370);
}
errors++;
}
}
if(data158.command !== undefined){
if(typeof data158.command !== "string"){
const err371 = {instancePath:instancePath+"/questions/" + i17+"/command",schemaPath:"#/properties/questions/items/properties/command/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err371];
}
else {
vErrors.push(err371);
}
errors++;
}
}
if(data158.rule !== undefined){
if(typeof data158.rule !== "string"){
const err372 = {instancePath:instancePath+"/questions/" + i17+"/rule",schemaPath:"#/properties/questions/items/properties/rule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err372];
}
else {
vErrors.push(err372);
}
errors++;
}
}
if(data158.rule_source !== undefined){
if(typeof data158.rule_source !== "string"){
const err373 = {instancePath:instancePath+"/questions/" + i17+"/rule_source",schemaPath:"#/properties/questions/items/properties/rule_source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err373];
}
else {
vErrors.push(err373);
}
errors++;
}
}
if(data158.question_id !== undefined){
if(typeof data158.question_id !== "string"){
const err374 = {instancePath:instancePath+"/questions/" + i17+"/question_id",schemaPath:"#/properties/questions/items/properties/question_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err374];
}
else {
vErrors.push(err374);
}
errors++;
}
}
if(data158.question !== undefined){
if(typeof data158.question !== "string"){
const err375 = {instancePath:instancePath+"/questions/" + i17+"/question",schemaPath:"#/properties/questions/items/properties/question/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err375];
}
else {
vErrors.push(err375);
}
errors++;
}
}
if(data158.options !== undefined){
let data206 = data158.options;
if((data206 !== null) && (!(Array.isArray(data206)))){
const err376 = {instancePath:instancePath+"/questions/" + i17+"/options",schemaPath:"#/properties/questions/items/properties/options/type",keyword:"type",params:{type: schema106.properties.questions.items.properties.options.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err376];
}
else {
vErrors.push(err376);
}
errors++;
}
if(Array.isArray(data206)){
const len21 = data206.length;
for(let i21=0; i21<len21; i21++){
let data207 = data206[i21];
if(data207 && typeof data207 == "object" && !Array.isArray(data207)){
if(data207.label === undefined){
const err377 = {instancePath:instancePath+"/questions/" + i17+"/options/" + i21,schemaPath:"#/properties/questions/items/properties/options/items/required",keyword:"required",params:{missingProperty: "label"},message:"must have required property '"+"label"+"'"};
if(vErrors === null){
vErrors = [err377];
}
else {
vErrors.push(err377);
}
errors++;
}
if(data207.label !== undefined){
if(typeof data207.label !== "string"){
const err378 = {instancePath:instancePath+"/questions/" + i17+"/options/" + i21+"/label",schemaPath:"#/properties/questions/items/properties/options/items/properties/label/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err378];
}
else {
vErrors.push(err378);
}
errors++;
}
}
if(data207.description !== undefined){
if(typeof data207.description !== "string"){
const err379 = {instancePath:instancePath+"/questions/" + i17+"/options/" + i21+"/description",schemaPath:"#/properties/questions/items/properties/options/items/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err379];
}
else {
vErrors.push(err379);
}
errors++;
}
}
}
else {
const err380 = {instancePath:instancePath+"/questions/" + i17+"/options/" + i21,schemaPath:"#/properties/questions/items/properties/options/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err380];
}
else {
vErrors.push(err380);
}
errors++;
}
}
}
}
if(data158.multiple !== undefined){
if(typeof data158.multiple !== "boolean"){
const err381 = {instancePath:instancePath+"/questions/" + i17+"/multiple",schemaPath:"#/properties/questions/items/properties/multiple/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err381];
}
else {
vErrors.push(err381);
}
errors++;
}
}
if(data158.answer !== undefined){
let data211 = data158.answer;
if((data211 !== null) && (!(Array.isArray(data211)))){
const err382 = {instancePath:instancePath+"/questions/" + i17+"/answer",schemaPath:"#/properties/questions/items/properties/answer/type",keyword:"type",params:{type: schema106.properties.questions.items.properties.answer.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err382];
}
else {
vErrors.push(err382);
}
errors++;
}
if(Array.isArray(data211)){
const len22 = data211.length;
for(let i22=0; i22<len22; i22++){
if(typeof data211[i22] !== "string"){
const err383 = {instancePath:instancePath+"/questions/" + i17+"/answer/" + i22,schemaPath:"#/properties/questions/items/properties/answer/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err383];
}
else {
vErrors.push(err383);
}
errors++;
}
}
}
}
if(data158.dismissed !== undefined){
if(typeof data158.dismissed !== "boolean"){
const err384 = {instancePath:instancePath+"/questions/" + i17+"/dismissed",schemaPath:"#/properties/questions/items/properties/dismissed/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err384];
}
else {
vErrors.push(err384);
}
errors++;
}
}
}
else {
const err385 = {instancePath:instancePath+"/questions/" + i17,schemaPath:"#/properties/questions/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err385];
}
else {
vErrors.push(err385);
}
errors++;
}
}
}
}
}
else {
const err386 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err386];
}
else {
vErrors.push(err386);
}
errors++;
}
validate105.errors = vErrors;
return errors === 0;
}

export const RunConfigureParams = validate106;
const schema107 = {"type":"object","properties":{"system":{"type":"string"},"max_turns":{"type":"integer"},"headless":{"type":"boolean"},"cache_key":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/RunConfigureParams","$schema":"http://json-schema.org/draft-07/schema#","title":"RunConfigureParams","additionalProperties":true};

function validate106(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RunConfigureParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.system !== undefined){
if(typeof data.system !== "string"){
const err0 = {instancePath:instancePath+"/system",schemaPath:"#/properties/system/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
if(data.max_turns !== undefined){
let data1 = data.max_turns;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err1 = {instancePath:instancePath+"/max_turns",schemaPath:"#/properties/max_turns/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.headless !== undefined){
if(typeof data.headless !== "boolean"){
const err2 = {instancePath:instancePath+"/headless",schemaPath:"#/properties/headless/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.cache_key !== undefined){
if(typeof data.cache_key !== "string"){
const err3 = {instancePath:instancePath+"/cache_key",schemaPath:"#/properties/cache_key/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate106.errors = vErrors;
return errors === 0;
}

export const RuntimeConfiguration = validate107;
const schema108 = {"type":"object","properties":{"import_claude":{"type":"boolean"},"import_codex":{"type":"boolean"},"revision":{"type":"string"},"default_model":{"type":"string"},"default_provider":{"type":"string"},"default_effort":{"type":"string"},"compact_model":{"type":"string"},"compact_provider":{"type":"string"},"compact_percent":{"type":"integer"},"goal_max_rounds":{"type":"integer"},"max_retries":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/RuntimeConfiguration","$schema":"http://json-schema.org/draft-07/schema#","title":"RuntimeConfiguration","required":["import_claude","import_codex","revision","default_model","default_provider","default_effort","compact_model","compact_provider","compact_percent","goal_max_rounds","max_retries"],"additionalProperties":true};

function validate107(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/RuntimeConfiguration" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.import_claude === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "import_claude"},message:"must have required property '"+"import_claude"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.import_codex === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "import_codex"},message:"must have required property '"+"import_codex"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.revision === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.default_model === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "default_model"},message:"must have required property '"+"default_model"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.default_provider === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "default_provider"},message:"must have required property '"+"default_provider"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.default_effort === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "default_effort"},message:"must have required property '"+"default_effort"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.compact_model === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "compact_model"},message:"must have required property '"+"compact_model"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data.compact_provider === undefined){
const err7 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "compact_provider"},message:"must have required property '"+"compact_provider"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data.compact_percent === undefined){
const err8 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "compact_percent"},message:"must have required property '"+"compact_percent"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data.goal_max_rounds === undefined){
const err9 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "goal_max_rounds"},message:"must have required property '"+"goal_max_rounds"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data.max_retries === undefined){
const err10 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "max_retries"},message:"must have required property '"+"max_retries"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data.import_claude !== undefined){
if(typeof data.import_claude !== "boolean"){
const err11 = {instancePath:instancePath+"/import_claude",schemaPath:"#/properties/import_claude/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.import_codex !== undefined){
if(typeof data.import_codex !== "boolean"){
const err12 = {instancePath:instancePath+"/import_codex",schemaPath:"#/properties/import_codex/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data.revision !== undefined){
if(typeof data.revision !== "string"){
const err13 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.default_model !== undefined){
if(typeof data.default_model !== "string"){
const err14 = {instancePath:instancePath+"/default_model",schemaPath:"#/properties/default_model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.default_provider !== undefined){
if(typeof data.default_provider !== "string"){
const err15 = {instancePath:instancePath+"/default_provider",schemaPath:"#/properties/default_provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.default_effort !== undefined){
if(typeof data.default_effort !== "string"){
const err16 = {instancePath:instancePath+"/default_effort",schemaPath:"#/properties/default_effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.compact_model !== undefined){
if(typeof data.compact_model !== "string"){
const err17 = {instancePath:instancePath+"/compact_model",schemaPath:"#/properties/compact_model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data.compact_provider !== undefined){
if(typeof data.compact_provider !== "string"){
const err18 = {instancePath:instancePath+"/compact_provider",schemaPath:"#/properties/compact_provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data.compact_percent !== undefined){
let data8 = data.compact_percent;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err19 = {instancePath:instancePath+"/compact_percent",schemaPath:"#/properties/compact_percent/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data.goal_max_rounds !== undefined){
let data9 = data.goal_max_rounds;
if(!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9)))){
const err20 = {instancePath:instancePath+"/goal_max_rounds",schemaPath:"#/properties/goal_max_rounds/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data.max_retries !== undefined){
let data10 = data.max_retries;
if(!((typeof data10 == "number") && (!(data10 % 1) && !isNaN(data10)))){
const err21 = {instancePath:instancePath+"/max_retries",schemaPath:"#/properties/max_retries/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
}
else {
const err22 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
validate107.errors = vErrors;
return errors === 0;
}

export const ScheduleCreateParams = validate108;
const schema109 = {"type":"object","properties":{"schedule":{"type":"string"},"prompt":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ScheduleCreateParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ScheduleCreateParams","required":["schedule","prompt"],"additionalProperties":true};

function validate108(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ScheduleCreateParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.schedule === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "schedule"},message:"must have required property '"+"schedule"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.prompt === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "prompt"},message:"must have required property '"+"prompt"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.schedule !== undefined){
if(typeof data.schedule !== "string"){
const err2 = {instancePath:instancePath+"/schedule",schemaPath:"#/properties/schedule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.prompt !== undefined){
if(typeof data.prompt !== "string"){
const err3 = {instancePath:instancePath+"/prompt",schemaPath:"#/properties/prompt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
}
else {
const err4 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
validate108.errors = vErrors;
return errors === 0;
}

export const ScheduleDeleteParams = validate109;
const schema110 = {"type":"object","properties":{"schedule_id":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/ScheduleDeleteParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ScheduleDeleteParams","required":["schedule_id"],"additionalProperties":true};

function validate109(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ScheduleDeleteParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.schedule_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "schedule_id"},message:"must have required property '"+"schedule_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.schedule_id !== undefined){
let data0 = data.schedule_id;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err1 = {instancePath:instancePath+"/schedule_id",schemaPath:"#/properties/schedule_id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate109.errors = vErrors;
return errors === 0;
}

export const ScheduleListResult = validate110;
const schema111 = {"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"integer"},"schedule":{"type":"string"},"prompt":{"type":"string"},"anchor":{"type":"string"},"last_fire":{"type":"string"}},"required":["id","schedule","prompt","anchor","last_fire"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/ScheduleListResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ScheduleListResult"};

function validate110(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ScheduleListResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema111.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.id === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.schedule === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "schedule"},message:"must have required property '"+"schedule"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.prompt === undefined){
const err3 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "prompt"},message:"must have required property '"+"prompt"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.anchor === undefined){
const err4 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "anchor"},message:"must have required property '"+"anchor"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.last_fire === undefined){
const err5 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "last_fire"},message:"must have required property '"+"last_fire"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.id !== undefined){
let data1 = data0.id;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err6 = {instancePath:instancePath+"/" + i0+"/id",schemaPath:"#/items/properties/id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data0.schedule !== undefined){
if(typeof data0.schedule !== "string"){
const err7 = {instancePath:instancePath+"/" + i0+"/schedule",schemaPath:"#/items/properties/schedule/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data0.prompt !== undefined){
if(typeof data0.prompt !== "string"){
const err8 = {instancePath:instancePath+"/" + i0+"/prompt",schemaPath:"#/items/properties/prompt/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data0.anchor !== undefined){
if(typeof data0.anchor !== "string"){
const err9 = {instancePath:instancePath+"/" + i0+"/anchor",schemaPath:"#/items/properties/anchor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.last_fire !== undefined){
if(typeof data0.last_fire !== "string"){
const err10 = {instancePath:instancePath+"/" + i0+"/last_fire",schemaPath:"#/items/properties/last_fire/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
else {
const err11 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
validate110.errors = vErrors;
return errors === 0;
}

export const ScheduleResult = validate111;
const schema112 = {"type":"object","properties":{"schedule_id":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/ScheduleResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ScheduleResult","required":["schedule_id"],"additionalProperties":true};

function validate111(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ScheduleResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.schedule_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "schedule_id"},message:"must have required property '"+"schedule_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.schedule_id !== undefined){
let data0 = data.schedule_id;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err1 = {instancePath:instancePath+"/schedule_id",schemaPath:"#/properties/schedule_id/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate111.errors = vErrors;
return errors === 0;
}

export const SessionCatalogPage = validate112;
const schema113 = {"type":"object","properties":{"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"items":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string"},"title":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"cwd":{"type":"string"},"pinned":{"type":"boolean"},"updated_at":{"type":"string"},"truncated":{"type":"boolean"}},"required":["id","kind","title","model","provider","cwd","pinned","updated_at","truncated"],"additionalProperties":true}},"next_cursor":{"type":["null","object"],"properties":{"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"offset":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["revision","offset"],"additionalProperties":true},"has_more":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/SessionCatalogPage","$schema":"http://json-schema.org/draft-07/schema#","title":"SessionCatalogPage","required":["revision","items","has_more"],"additionalProperties":true};

function validate112(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SessionCatalogPage" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.revision === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.items === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "items"},message:"must have required property '"+"items"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.has_more === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "has_more"},message:"must have required property '"+"has_more"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.revision !== undefined){
let data0 = data.revision;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(!(formats0.validate(data0))){
const err4 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
else {
const err5 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.items !== undefined){
let data1 = data.items;
if((data1 !== null) && (!(Array.isArray(data1)))){
const err6 = {instancePath:instancePath+"/items",schemaPath:"#/properties/items/type",keyword:"type",params:{type: schema113.properties.items.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(Array.isArray(data1)){
const len0 = data1.length;
for(let i0=0; i0<len0; i0++){
let data2 = data1[i0];
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.id === undefined){
const err7 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data2.kind === undefined){
const err8 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data2.title === undefined){
const err9 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data2.model === undefined){
const err10 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data2.provider === undefined){
const err11 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data2.cwd === undefined){
const err12 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data2.pinned === undefined){
const err13 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data2.updated_at === undefined){
const err14 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "updated_at"},message:"must have required property '"+"updated_at"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data2.truncated === undefined){
const err15 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "truncated"},message:"must have required property '"+"truncated"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data2.id !== undefined){
if(typeof data2.id !== "string"){
const err16 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data2.kind !== undefined){
if(typeof data2.kind !== "string"){
const err17 = {instancePath:instancePath+"/items/" + i0+"/kind",schemaPath:"#/properties/items/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data2.title !== undefined){
if(typeof data2.title !== "string"){
const err18 = {instancePath:instancePath+"/items/" + i0+"/title",schemaPath:"#/properties/items/items/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data2.model !== undefined){
if(typeof data2.model !== "string"){
const err19 = {instancePath:instancePath+"/items/" + i0+"/model",schemaPath:"#/properties/items/items/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data2.provider !== undefined){
if(typeof data2.provider !== "string"){
const err20 = {instancePath:instancePath+"/items/" + i0+"/provider",schemaPath:"#/properties/items/items/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data2.cwd !== undefined){
if(typeof data2.cwd !== "string"){
const err21 = {instancePath:instancePath+"/items/" + i0+"/cwd",schemaPath:"#/properties/items/items/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data2.pinned !== undefined){
if(typeof data2.pinned !== "boolean"){
const err22 = {instancePath:instancePath+"/items/" + i0+"/pinned",schemaPath:"#/properties/items/items/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data2.updated_at !== undefined){
if(typeof data2.updated_at !== "string"){
const err23 = {instancePath:instancePath+"/items/" + i0+"/updated_at",schemaPath:"#/properties/items/items/properties/updated_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data2.truncated !== undefined){
if(typeof data2.truncated !== "boolean"){
const err24 = {instancePath:instancePath+"/items/" + i0+"/truncated",schemaPath:"#/properties/items/items/properties/truncated/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
}
else {
const err25 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
}
}
if(data.next_cursor !== undefined){
let data12 = data.next_cursor;
if((data12 !== null) && (!(data12 && typeof data12 == "object" && !Array.isArray(data12)))){
const err26 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/type",keyword:"type",params:{type: schema113.properties.next_cursor.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
if(data12.revision === undefined){
const err27 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data12.offset === undefined){
const err28 = {instancePath:instancePath+"/next_cursor",schemaPath:"#/properties/next_cursor/required",keyword:"required",params:{missingProperty: "offset"},message:"must have required property '"+"offset"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data12.revision !== undefined){
let data13 = data12.revision;
if(typeof data13 === "string"){
if(!pattern0.test(data13)){
const err29 = {instancePath:instancePath+"/next_cursor/revision",schemaPath:"#/properties/next_cursor/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(!(formats0.validate(data13))){
const err30 = {instancePath:instancePath+"/next_cursor/revision",schemaPath:"#/properties/next_cursor/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
else {
const err31 = {instancePath:instancePath+"/next_cursor/revision",schemaPath:"#/properties/next_cursor/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data12.offset !== undefined){
let data14 = data12.offset;
if(typeof data14 === "string"){
if(!pattern0.test(data14)){
const err32 = {instancePath:instancePath+"/next_cursor/offset",schemaPath:"#/properties/next_cursor/properties/offset/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(!(formats0.validate(data14))){
const err33 = {instancePath:instancePath+"/next_cursor/offset",schemaPath:"#/properties/next_cursor/properties/offset/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
else {
const err34 = {instancePath:instancePath+"/next_cursor/offset",schemaPath:"#/properties/next_cursor/properties/offset/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
}
}
if(data.has_more !== undefined){
if(typeof data.has_more !== "boolean"){
const err35 = {instancePath:instancePath+"/has_more",schemaPath:"#/properties/has_more/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
}
else {
const err36 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
validate112.errors = vErrors;
return errors === 0;
}

export const SessionCatalogParams = validate113;
const schema114 = {"type":"object","properties":{"cursor":{"type":["null","object"],"properties":{"revision":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"offset":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"required":["revision","offset"],"additionalProperties":true},"limit":{"type":"integer"},"max_bytes":{"type":"integer"}},"$id":"https://whip.dev/protocol/v2/SessionCatalogParams","$schema":"http://json-schema.org/draft-07/schema#","title":"SessionCatalogParams","required":["limit","max_bytes"],"additionalProperties":true};

function validate113(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SessionCatalogParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.limit === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "limit"},message:"must have required property '"+"limit"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.max_bytes === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "max_bytes"},message:"must have required property '"+"max_bytes"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.cursor !== undefined){
let data0 = data.cursor;
if((data0 !== null) && (!(data0 && typeof data0 == "object" && !Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: schema114.properties.cursor.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.revision === undefined){
const err3 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.offset === undefined){
const err4 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/required",keyword:"required",params:{missingProperty: "offset"},message:"must have required property '"+"offset"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.revision !== undefined){
let data1 = data0.revision;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err5 = {instancePath:instancePath+"/cursor/revision",schemaPath:"#/properties/cursor/properties/revision/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(formats0.validate(data1))){
const err6 = {instancePath:instancePath+"/cursor/revision",schemaPath:"#/properties/cursor/properties/revision/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
else {
const err7 = {instancePath:instancePath+"/cursor/revision",schemaPath:"#/properties/cursor/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data0.offset !== undefined){
let data2 = data0.offset;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err8 = {instancePath:instancePath+"/cursor/offset",schemaPath:"#/properties/cursor/properties/offset/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(!(formats0.validate(data2))){
const err9 = {instancePath:instancePath+"/cursor/offset",schemaPath:"#/properties/cursor/properties/offset/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
else {
const err10 = {instancePath:instancePath+"/cursor/offset",schemaPath:"#/properties/cursor/properties/offset/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
}
if(data.limit !== undefined){
let data3 = data.limit;
if(!((typeof data3 == "number") && (!(data3 % 1) && !isNaN(data3)))){
const err11 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.max_bytes !== undefined){
let data4 = data.max_bytes;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err12 = {instancePath:instancePath+"/max_bytes",schemaPath:"#/properties/max_bytes/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
}
else {
const err13 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
validate113.errors = vErrors;
return errors === 0;
}

export const SessionListResult = validate114;
const schema115 = {"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string"},"title":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"cwd":{"type":"string"},"goal":{"type":"string"},"forked_from":{"type":"string"},"fork_seq":{"type":"integer"},"tags":{"type":["null","array"],"items":{"type":"string"}},"pinned":{"type":"boolean"},"effort":{"type":"string"},"usage_in":{"type":"integer"},"usage_cached":{"type":"integer"},"usage_out":{"type":"integer"},"updated_at":{"type":"string"}},"required":["id","kind","title","model","provider","cwd","goal","forked_from","fork_seq","tags","pinned","effort","usage_in","usage_cached","usage_out","updated_at"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/SessionListResult","$schema":"http://json-schema.org/draft-07/schema#","title":"SessionListResult"};

function validate114(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SessionListResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema115.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.id === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.kind === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.title === undefined){
const err3 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.model === undefined){
const err4 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.provider === undefined){
const err5 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.cwd === undefined){
const err6 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "cwd"},message:"must have required property '"+"cwd"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data0.goal === undefined){
const err7 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "goal"},message:"must have required property '"+"goal"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data0.forked_from === undefined){
const err8 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "forked_from"},message:"must have required property '"+"forked_from"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data0.fork_seq === undefined){
const err9 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "fork_seq"},message:"must have required property '"+"fork_seq"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data0.tags === undefined){
const err10 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "tags"},message:"must have required property '"+"tags"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data0.pinned === undefined){
const err11 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data0.effort === undefined){
const err12 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data0.usage_in === undefined){
const err13 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "usage_in"},message:"must have required property '"+"usage_in"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data0.usage_cached === undefined){
const err14 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "usage_cached"},message:"must have required property '"+"usage_cached"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data0.usage_out === undefined){
const err15 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "usage_out"},message:"must have required property '"+"usage_out"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data0.updated_at === undefined){
const err16 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "updated_at"},message:"must have required property '"+"updated_at"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data0.id !== undefined){
if(typeof data0.id !== "string"){
const err17 = {instancePath:instancePath+"/" + i0+"/id",schemaPath:"#/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data0.kind !== undefined){
if(typeof data0.kind !== "string"){
const err18 = {instancePath:instancePath+"/" + i0+"/kind",schemaPath:"#/items/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data0.title !== undefined){
if(typeof data0.title !== "string"){
const err19 = {instancePath:instancePath+"/" + i0+"/title",schemaPath:"#/items/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data0.model !== undefined){
if(typeof data0.model !== "string"){
const err20 = {instancePath:instancePath+"/" + i0+"/model",schemaPath:"#/items/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data0.provider !== undefined){
if(typeof data0.provider !== "string"){
const err21 = {instancePath:instancePath+"/" + i0+"/provider",schemaPath:"#/items/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data0.cwd !== undefined){
if(typeof data0.cwd !== "string"){
const err22 = {instancePath:instancePath+"/" + i0+"/cwd",schemaPath:"#/items/properties/cwd/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data0.goal !== undefined){
if(typeof data0.goal !== "string"){
const err23 = {instancePath:instancePath+"/" + i0+"/goal",schemaPath:"#/items/properties/goal/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data0.forked_from !== undefined){
if(typeof data0.forked_from !== "string"){
const err24 = {instancePath:instancePath+"/" + i0+"/forked_from",schemaPath:"#/items/properties/forked_from/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data0.fork_seq !== undefined){
let data9 = data0.fork_seq;
if(!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9)))){
const err25 = {instancePath:instancePath+"/" + i0+"/fork_seq",schemaPath:"#/items/properties/fork_seq/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data0.tags !== undefined){
let data10 = data0.tags;
if((data10 !== null) && (!(Array.isArray(data10)))){
const err26 = {instancePath:instancePath+"/" + i0+"/tags",schemaPath:"#/items/properties/tags/type",keyword:"type",params:{type: schema115.items.properties.tags.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(Array.isArray(data10)){
const len1 = data10.length;
for(let i1=0; i1<len1; i1++){
if(typeof data10[i1] !== "string"){
const err27 = {instancePath:instancePath+"/" + i0+"/tags/" + i1,schemaPath:"#/items/properties/tags/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
}
}
if(data0.pinned !== undefined){
if(typeof data0.pinned !== "boolean"){
const err28 = {instancePath:instancePath+"/" + i0+"/pinned",schemaPath:"#/items/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data0.effort !== undefined){
if(typeof data0.effort !== "string"){
const err29 = {instancePath:instancePath+"/" + i0+"/effort",schemaPath:"#/items/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data0.usage_in !== undefined){
let data14 = data0.usage_in;
if(!((typeof data14 == "number") && (!(data14 % 1) && !isNaN(data14)))){
const err30 = {instancePath:instancePath+"/" + i0+"/usage_in",schemaPath:"#/items/properties/usage_in/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data0.usage_cached !== undefined){
let data15 = data0.usage_cached;
if(!((typeof data15 == "number") && (!(data15 % 1) && !isNaN(data15)))){
const err31 = {instancePath:instancePath+"/" + i0+"/usage_cached",schemaPath:"#/items/properties/usage_cached/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data0.usage_out !== undefined){
let data16 = data0.usage_out;
if(!((typeof data16 == "number") && (!(data16 % 1) && !isNaN(data16)))){
const err32 = {instancePath:instancePath+"/" + i0+"/usage_out",schemaPath:"#/items/properties/usage_out/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data0.updated_at !== undefined){
if(typeof data0.updated_at !== "string"){
const err33 = {instancePath:instancePath+"/" + i0+"/updated_at",schemaPath:"#/items/properties/updated_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
}
else {
const err34 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
}
validate114.errors = vErrors;
return errors === 0;
}

export const SessionPreviewResult = validate115;
const schema116 = {"type":"object","properties":{"root_id":{"type":"string"},"user":{"type":"string"},"assistant":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/SessionPreviewResult","$schema":"http://json-schema.org/draft-07/schema#","title":"SessionPreviewResult","required":["root_id","user","assistant"],"additionalProperties":true};

function validate115(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SessionPreviewResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.user === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "user"},message:"must have required property '"+"user"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.assistant === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "assistant"},message:"must have required property '"+"assistant"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err3 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.user !== undefined){
if(typeof data.user !== "string"){
const err4 = {instancePath:instancePath+"/user",schemaPath:"#/properties/user/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.assistant !== undefined){
if(typeof data.assistant !== "string"){
const err5 = {instancePath:instancePath+"/assistant",schemaPath:"#/properties/assistant/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate115.errors = vErrors;
return errors === 0;
}

export const SessionUpdateEvent = validate116;
const schema117 = {"type":"object","properties":{"title":{"type":"string"},"model":{"type":"string"},"provider":{"type":"string"},"effort":{"type":"string"},"effort_changed":{"type":"boolean"},"working_directory":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/SessionUpdateEvent","$schema":"http://json-schema.org/draft-07/schema#","title":"SessionUpdateEvent","additionalProperties":true};

function validate116(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SessionUpdateEvent" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.title !== undefined){
if(typeof data.title !== "string"){
const err0 = {instancePath:instancePath+"/title",schemaPath:"#/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
if(data.model !== undefined){
if(typeof data.model !== "string"){
const err1 = {instancePath:instancePath+"/model",schemaPath:"#/properties/model/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.provider !== undefined){
if(typeof data.provider !== "string"){
const err2 = {instancePath:instancePath+"/provider",schemaPath:"#/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.effort !== undefined){
if(typeof data.effort !== "string"){
const err3 = {instancePath:instancePath+"/effort",schemaPath:"#/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.effort_changed !== undefined){
if(typeof data.effort_changed !== "boolean"){
const err4 = {instancePath:instancePath+"/effort_changed",schemaPath:"#/properties/effort_changed/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.working_directory !== undefined){
if(typeof data.working_directory !== "string"){
const err5 = {instancePath:instancePath+"/working_directory",schemaPath:"#/properties/working_directory/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate116.errors = vErrors;
return errors === 0;
}

export const ShellParams = validate117;
const schema118 = {"type":"object","properties":{"command":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/ShellParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ShellParams","required":["command"],"additionalProperties":true};

function validate117(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ShellParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.command === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "command"},message:"must have required property '"+"command"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.command !== undefined){
if(typeof data.command !== "string"){
const err1 = {instancePath:instancePath+"/command",schemaPath:"#/properties/command/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate117.errors = vErrors;
return errors === 0;
}

export const SnapshotParams = validate118;
const schema119 = {"type":"object","properties":{"root_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/SnapshotParams","$schema":"http://json-schema.org/draft-07/schema#","title":"SnapshotParams","required":["root_id"],"additionalProperties":true};

function validate118(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SnapshotParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err1 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate118.errors = vErrors;
return errors === 0;
}

export const StreamEvent = validate119;
const schema120 = {"type":"object","properties":{"usage":{"type":["null","object"],"properties":{"used":{"type":"integer"},"size":{"type":"integer"},"usage":{"type":"object","properties":{"prompt_tokens":{"type":"integer"},"completion_tokens":{"type":"integer"},"prompt_tokens_details":{"type":["null","object"],"properties":{"cached_tokens":{"type":"integer"}},"required":["cached_tokens"],"additionalProperties":true}},"required":["prompt_tokens","completion_tokens"],"additionalProperties":true}},"required":["used","size","usage"],"additionalProperties":true},"agent_id":{"type":"string"},"id":{"type":"string"},"name":{"type":"string"},"text":{"type":"string"},"args":{"type":"string"},"result":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/StreamEvent","$schema":"http://json-schema.org/draft-07/schema#","title":"StreamEvent","additionalProperties":true};

function validate119(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/StreamEvent" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.usage !== undefined){
let data0 = data.usage;
if((data0 !== null) && (!(data0 && typeof data0 == "object" && !Array.isArray(data0)))){
const err0 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/type",keyword:"type",params:{type: schema120.properties.usage.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.used === undefined){
const err1 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/required",keyword:"required",params:{missingProperty: "used"},message:"must have required property '"+"used"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.size === undefined){
const err2 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.usage === undefined){
const err3 = {instancePath:instancePath+"/usage",schemaPath:"#/properties/usage/required",keyword:"required",params:{missingProperty: "usage"},message:"must have required property '"+"usage"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.used !== undefined){
let data1 = data0.used;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err4 = {instancePath:instancePath+"/usage/used",schemaPath:"#/properties/usage/properties/used/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data0.size !== undefined){
let data2 = data0.size;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err5 = {instancePath:instancePath+"/usage/size",schemaPath:"#/properties/usage/properties/size/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data0.usage !== undefined){
let data3 = data0.usage;
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.prompt_tokens === undefined){
const err6 = {instancePath:instancePath+"/usage/usage",schemaPath:"#/properties/usage/properties/usage/required",keyword:"required",params:{missingProperty: "prompt_tokens"},message:"must have required property '"+"prompt_tokens"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data3.completion_tokens === undefined){
const err7 = {instancePath:instancePath+"/usage/usage",schemaPath:"#/properties/usage/properties/usage/required",keyword:"required",params:{missingProperty: "completion_tokens"},message:"must have required property '"+"completion_tokens"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data3.prompt_tokens !== undefined){
let data4 = data3.prompt_tokens;
if(!((typeof data4 == "number") && (!(data4 % 1) && !isNaN(data4)))){
const err8 = {instancePath:instancePath+"/usage/usage/prompt_tokens",schemaPath:"#/properties/usage/properties/usage/properties/prompt_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data3.completion_tokens !== undefined){
let data5 = data3.completion_tokens;
if(!((typeof data5 == "number") && (!(data5 % 1) && !isNaN(data5)))){
const err9 = {instancePath:instancePath+"/usage/usage/completion_tokens",schemaPath:"#/properties/usage/properties/usage/properties/completion_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data3.prompt_tokens_details !== undefined){
let data6 = data3.prompt_tokens_details;
if((data6 !== null) && (!(data6 && typeof data6 == "object" && !Array.isArray(data6)))){
const err10 = {instancePath:instancePath+"/usage/usage/prompt_tokens_details",schemaPath:"#/properties/usage/properties/usage/properties/prompt_tokens_details/type",keyword:"type",params:{type: schema120.properties.usage.properties.usage.properties.prompt_tokens_details.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.cached_tokens === undefined){
const err11 = {instancePath:instancePath+"/usage/usage/prompt_tokens_details",schemaPath:"#/properties/usage/properties/usage/properties/prompt_tokens_details/required",keyword:"required",params:{missingProperty: "cached_tokens"},message:"must have required property '"+"cached_tokens"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data6.cached_tokens !== undefined){
let data7 = data6.cached_tokens;
if(!((typeof data7 == "number") && (!(data7 % 1) && !isNaN(data7)))){
const err12 = {instancePath:instancePath+"/usage/usage/prompt_tokens_details/cached_tokens",schemaPath:"#/properties/usage/properties/usage/properties/prompt_tokens_details/properties/cached_tokens/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
}
}
}
else {
const err13 = {instancePath:instancePath+"/usage/usage",schemaPath:"#/properties/usage/properties/usage/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
}
}
if(data.agent_id !== undefined){
if(typeof data.agent_id !== "string"){
const err14 = {instancePath:instancePath+"/agent_id",schemaPath:"#/properties/agent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err15 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data.name !== undefined){
if(typeof data.name !== "string"){
const err16 = {instancePath:instancePath+"/name",schemaPath:"#/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.text !== undefined){
if(typeof data.text !== "string"){
const err17 = {instancePath:instancePath+"/text",schemaPath:"#/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data.args !== undefined){
if(typeof data.args !== "string"){
const err18 = {instancePath:instancePath+"/args",schemaPath:"#/properties/args/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data.result !== undefined){
if(typeof data.result !== "string"){
const err19 = {instancePath:instancePath+"/result",schemaPath:"#/properties/result/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
}
else {
const err20 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
validate119.errors = vErrors;
return errors === 0;
}

export const SubmitPayload = validate120;
const schema121 = {"type":"object","properties":{"text":{"type":"string"},"parts":{"type":["null","array"],"items":{"type":"object","properties":{"type":{"type":"string"},"text":{"type":"string"},"image_url":{"type":["null","object"],"properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":true},"w":{"type":"integer"},"h":{"type":"integer"}},"required":["type"],"additionalProperties":true}}},"$id":"https://whip.dev/protocol/v2/SubmitPayload","$schema":"http://json-schema.org/draft-07/schema#","title":"SubmitPayload","required":["text"],"additionalProperties":true};

function validate120(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SubmitPayload" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.text === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.text !== undefined){
if(typeof data.text !== "string"){
const err1 = {instancePath:instancePath+"/text",schemaPath:"#/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.parts !== undefined){
let data1 = data.parts;
if((data1 !== null) && (!(Array.isArray(data1)))){
const err2 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/type",keyword:"type",params:{type: schema121.properties.parts.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(Array.isArray(data1)){
const len0 = data1.length;
for(let i0=0; i0<len0; i0++){
let data2 = data1[i0];
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.type === undefined){
const err3 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data2.type !== undefined){
if(typeof data2.type !== "string"){
const err4 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data2.text !== undefined){
if(typeof data2.text !== "string"){
const err5 = {instancePath:instancePath+"/parts/" + i0+"/text",schemaPath:"#/properties/parts/items/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data2.image_url !== undefined){
let data5 = data2.image_url;
if((data5 !== null) && (!(data5 && typeof data5 == "object" && !Array.isArray(data5)))){
const err6 = {instancePath:instancePath+"/parts/" + i0+"/image_url",schemaPath:"#/properties/parts/items/properties/image_url/type",keyword:"type",params:{type: schema121.properties.parts.items.properties.image_url.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.url === undefined){
const err7 = {instancePath:instancePath+"/parts/" + i0+"/image_url",schemaPath:"#/properties/parts/items/properties/image_url/required",keyword:"required",params:{missingProperty: "url"},message:"must have required property '"+"url"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data5.url !== undefined){
if(typeof data5.url !== "string"){
const err8 = {instancePath:instancePath+"/parts/" + i0+"/image_url/url",schemaPath:"#/properties/parts/items/properties/image_url/properties/url/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
}
if(data2.w !== undefined){
let data7 = data2.w;
if(!((typeof data7 == "number") && (!(data7 % 1) && !isNaN(data7)))){
const err9 = {instancePath:instancePath+"/parts/" + i0+"/w",schemaPath:"#/properties/parts/items/properties/w/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data2.h !== undefined){
let data8 = data2.h;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err10 = {instancePath:instancePath+"/parts/" + i0+"/h",schemaPath:"#/properties/parts/items/properties/h/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
else {
const err11 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
}
}
else {
const err12 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
validate120.errors = vErrors;
return errors === 0;
}

export const SubscribeParams = validate121;
const schema122 = {"type":"object","properties":{"root_id":{"type":"string"},"subscription_id":{"type":"string"},"cursor":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"$id":"https://whip.dev/protocol/v2/SubscribeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"SubscribeParams","required":["root_id","subscription_id","cursor"],"additionalProperties":true};

function validate121(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SubscribeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.root_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.subscription_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "subscription_id"},message:"must have required property '"+"subscription_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.cursor === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cursor"},message:"must have required property '"+"cursor"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err3 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.subscription_id !== undefined){
if(typeof data.subscription_id !== "string"){
const err4 = {instancePath:instancePath+"/subscription_id",schemaPath:"#/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.cursor !== undefined){
let data2 = data.cursor;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err5 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(formats0.validate(data2))){
const err6 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
else {
const err7 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
}
else {
const err8 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
validate121.errors = vErrors;
return errors === 0;
}

export const SubscribeResult = validate122;
const schema123 = {"type":"object","properties":{"subscription_id":{"type":"string"},"cursor":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"}},"$id":"https://whip.dev/protocol/v2/SubscribeResult","$schema":"http://json-schema.org/draft-07/schema#","title":"SubscribeResult","required":["subscription_id","cursor"],"additionalProperties":true};

function validate122(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SubscribeResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.subscription_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "subscription_id"},message:"must have required property '"+"subscription_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.cursor === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "cursor"},message:"must have required property '"+"cursor"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.subscription_id !== undefined){
if(typeof data.subscription_id !== "string"){
const err2 = {instancePath:instancePath+"/subscription_id",schemaPath:"#/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.cursor !== undefined){
let data1 = data.cursor;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err3 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(!(formats0.validate(data1))){
const err4 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
else {
const err5 = {instancePath:instancePath+"/cursor",schemaPath:"#/properties/cursor/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
}
else {
const err6 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
validate122.errors = vErrors;
return errors === 0;
}

export const SubscriptionFailure = validate123;
const schema124 = {"type":"object","properties":{"subscription_id":{"type":"string"},"root_id":{"type":"string"},"error":{"type":["null","object"],"properties":{"data":{"type":["null","object"],"properties":{"kind":{"type":"string"}},"required":["kind"],"additionalProperties":true},"code":{"type":"integer"},"message":{"type":"string"}},"required":["code","message"],"additionalProperties":true}},"$id":"https://whip.dev/protocol/v2/SubscriptionFailure","$schema":"http://json-schema.org/draft-07/schema#","title":"SubscriptionFailure","required":["subscription_id","root_id","error"],"additionalProperties":true};

function validate123(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/SubscriptionFailure" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.subscription_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "subscription_id"},message:"must have required property '"+"subscription_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.error === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "error"},message:"must have required property '"+"error"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.subscription_id !== undefined){
if(typeof data.subscription_id !== "string"){
const err3 = {instancePath:instancePath+"/subscription_id",schemaPath:"#/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err4 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.error !== undefined){
let data2 = data.error;
if((data2 !== null) && (!(data2 && typeof data2 == "object" && !Array.isArray(data2)))){
const err5 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/type",keyword:"type",params:{type: schema124.properties.error.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.code === undefined){
const err6 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/required",keyword:"required",params:{missingProperty: "code"},message:"must have required property '"+"code"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data2.message === undefined){
const err7 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/required",keyword:"required",params:{missingProperty: "message"},message:"must have required property '"+"message"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data2.data !== undefined){
let data3 = data2.data;
if((data3 !== null) && (!(data3 && typeof data3 == "object" && !Array.isArray(data3)))){
const err8 = {instancePath:instancePath+"/error/data",schemaPath:"#/properties/error/properties/data/type",keyword:"type",params:{type: schema124.properties.error.properties.data.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.kind === undefined){
const err9 = {instancePath:instancePath+"/error/data",schemaPath:"#/properties/error/properties/data/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data3.kind !== undefined){
if(typeof data3.kind !== "string"){
const err10 = {instancePath:instancePath+"/error/data/kind",schemaPath:"#/properties/error/properties/data/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
}
if(data2.code !== undefined){
let data5 = data2.code;
if(!((typeof data5 == "number") && (!(data5 % 1) && !isNaN(data5)))){
const err11 = {instancePath:instancePath+"/error/code",schemaPath:"#/properties/error/properties/code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data2.message !== undefined){
if(typeof data2.message !== "string"){
const err12 = {instancePath:instancePath+"/error/message",schemaPath:"#/properties/error/properties/message/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
}
}
}
else {
const err13 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
validate123.errors = vErrors;
return errors === 0;
}

export const TerminalInputParams = validate124;
const schema125 = {"type":"object","properties":{"id":{"type":"string"},"bytes":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/TerminalInputParams","$schema":"http://json-schema.org/draft-07/schema#","title":"TerminalInputParams","required":["id","bytes"],"additionalProperties":true};

function validate124(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/TerminalInputParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.bytes === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "bytes"},message:"must have required property '"+"bytes"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.id !== undefined){
if(typeof data.id !== "string"){
const err2 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.bytes !== undefined){
let data1 = data.bytes;
if((typeof data1 !== "string") && (data1 !== null)){
const err3 = {instancePath:instancePath+"/bytes",schemaPath:"#/properties/bytes/type",keyword:"type",params:{type: schema125.properties.bytes.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(typeof data1 === "string"){
if(!pattern21.test(data1)){
const err4 = {instancePath:instancePath+"/bytes",schemaPath:"#/properties/bytes/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
}
}
else {
const err5 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
validate124.errors = vErrors;
return errors === 0;
}

export const TextParams = validate125;
const schema126 = {"type":"object","properties":{"text":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/TextParams","$schema":"http://json-schema.org/draft-07/schema#","title":"TextParams","required":["text"],"additionalProperties":true};

function validate125(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/TextParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.text === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.text !== undefined){
if(typeof data.text !== "string"){
const err1 = {instancePath:instancePath+"/text",schemaPath:"#/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate125.errors = vErrors;
return errors === 0;
}

export const TextResult = validate126;
const schema127 = {"type":"object","properties":{"text":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/TextResult","$schema":"http://json-schema.org/draft-07/schema#","title":"TextResult","required":["text"],"additionalProperties":true};

function validate126(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/TextResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.text === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.text !== undefined){
if(typeof data.text !== "string"){
const err1 = {instancePath:instancePath+"/text",schemaPath:"#/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate126.errors = vErrors;
return errors === 0;
}

export const TitleParams = validate127;
const schema128 = {"type":"object","properties":{"title":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/TitleParams","$schema":"http://json-schema.org/draft-07/schema#","title":"TitleParams","required":["title"],"additionalProperties":true};

function validate127(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/TitleParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.title === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.title !== undefined){
if(typeof data.title !== "string"){
const err1 = {instancePath:instancePath+"/title",schemaPath:"#/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate127.errors = vErrors;
return errors === 0;
}

export const TitleResult = validate128;
const schema129 = {"type":"object","properties":{"title":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/TitleResult","$schema":"http://json-schema.org/draft-07/schema#","title":"TitleResult","required":["title"],"additionalProperties":true};

function validate128(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/TitleResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.title === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.title !== undefined){
if(typeof data.title !== "string"){
const err1 = {instancePath:instancePath+"/title",schemaPath:"#/properties/title/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate128.errors = vErrors;
return errors === 0;
}

export const ToolCallParams = validate129;
const schema130 = {"type":"object","properties":{"tool":{"type":"string"},"arguments":true},"$id":"https://whip.dev/protocol/v2/ToolCallParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ToolCallParams","required":["tool","arguments"],"additionalProperties":true};

function validate129(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ToolCallParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.tool === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "tool"},message:"must have required property '"+"tool"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.arguments === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "arguments"},message:"must have required property '"+"arguments"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.tool !== undefined){
if(typeof data.tool !== "string"){
const err2 = {instancePath:instancePath+"/tool",schemaPath:"#/properties/tool/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
}
else {
const err3 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
validate129.errors = vErrors;
return errors === 0;
}

export const ToolConfigureParams = validate130;
const schema131 = {"type":"object","properties":{"deny_permissions":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v2/ToolConfigureParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ToolConfigureParams","required":["deny_permissions"],"additionalProperties":true};

function validate130(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ToolConfigureParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.deny_permissions === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "deny_permissions"},message:"must have required property '"+"deny_permissions"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.deny_permissions !== undefined){
if(typeof data.deny_permissions !== "boolean"){
const err1 = {instancePath:instancePath+"/deny_permissions",schemaPath:"#/properties/deny_permissions/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate130.errors = vErrors;
return errors === 0;
}

export const ToolSchemaResult = validate131;
const schema132 = {"type":["null","array"],"items":{"type":"object","properties":{"type":{"type":"string"},"function":{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"parameters":true},"required":["name","description","parameters"],"additionalProperties":true}},"required":["type","function"],"additionalProperties":true},"$id":"https://whip.dev/protocol/v2/ToolSchemaResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ToolSchemaResult"};

function validate131(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/ToolSchemaResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema132.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
let data0 = data[i0];
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.type === undefined){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data0.function === undefined){
const err2 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/required",keyword:"required",params:{missingProperty: "function"},message:"must have required property '"+"function"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data0.type !== undefined){
if(typeof data0.type !== "string"){
const err3 = {instancePath:instancePath+"/" + i0+"/type",schemaPath:"#/items/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data0.function !== undefined){
let data2 = data0.function;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.name === undefined){
const err4 = {instancePath:instancePath+"/" + i0+"/function",schemaPath:"#/items/properties/function/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data2.description === undefined){
const err5 = {instancePath:instancePath+"/" + i0+"/function",schemaPath:"#/items/properties/function/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data2.parameters === undefined){
const err6 = {instancePath:instancePath+"/" + i0+"/function",schemaPath:"#/items/properties/function/required",keyword:"required",params:{missingProperty: "parameters"},message:"must have required property '"+"parameters"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data2.name !== undefined){
if(typeof data2.name !== "string"){
const err7 = {instancePath:instancePath+"/" + i0+"/function/name",schemaPath:"#/items/properties/function/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data2.description !== undefined){
if(typeof data2.description !== "string"){
const err8 = {instancePath:instancePath+"/" + i0+"/function/description",schemaPath:"#/items/properties/function/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
else {
const err9 = {instancePath:instancePath+"/" + i0+"/function",schemaPath:"#/items/properties/function/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
}
else {
const err10 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
}
validate131.errors = vErrors;
return errors === 0;
}

export const UnsubscribeParams = validate132;
const schema133 = {"type":"object","properties":{"subscription_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/UnsubscribeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"UnsubscribeParams","required":["subscription_id"],"additionalProperties":true};

function validate132(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/UnsubscribeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.subscription_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "subscription_id"},message:"must have required property '"+"subscription_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.subscription_id !== undefined){
if(typeof data.subscription_id !== "string"){
const err1 = {instancePath:instancePath+"/subscription_id",schemaPath:"#/properties/subscription_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate132.errors = vErrors;
return errors === 0;
}

export const UploadBeginParams = validate133;
const schema134 = {"type":"object","properties":{"upload_id":{"type":"string"},"root_id":{"type":"string"},"expected_digest":{"type":"string"},"size":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"media_type":{"type":"string"},"source":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/UploadBeginParams","$schema":"http://json-schema.org/draft-07/schema#","title":"UploadBeginParams","required":["upload_id","root_id","expected_digest","size"],"additionalProperties":true};

function validate133(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/UploadBeginParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.upload_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "upload_id"},message:"must have required property '"+"upload_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root_id"},message:"must have required property '"+"root_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.expected_digest === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expected_digest"},message:"must have required property '"+"expected_digest"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.size === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "size"},message:"must have required property '"+"size"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.upload_id !== undefined){
if(typeof data.upload_id !== "string"){
const err4 = {instancePath:instancePath+"/upload_id",schemaPath:"#/properties/upload_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.root_id !== undefined){
if(typeof data.root_id !== "string"){
const err5 = {instancePath:instancePath+"/root_id",schemaPath:"#/properties/root_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.expected_digest !== undefined){
if(typeof data.expected_digest !== "string"){
const err6 = {instancePath:instancePath+"/expected_digest",schemaPath:"#/properties/expected_digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.size !== undefined){
let data3 = data.size;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err7 = {instancePath:instancePath+"/size",schemaPath:"#/properties/size/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(!(formats0.validate(data3))){
const err8 = {instancePath:instancePath+"/size",schemaPath:"#/properties/size/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
else {
const err9 = {instancePath:instancePath+"/size",schemaPath:"#/properties/size/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.media_type !== undefined){
if(typeof data.media_type !== "string"){
const err10 = {instancePath:instancePath+"/media_type",schemaPath:"#/properties/media_type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.source !== undefined){
if(typeof data.source !== "string"){
const err11 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
}
else {
const err12 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
validate133.errors = vErrors;
return errors === 0;
}

export const UploadChunkParams = validate134;
const schema135 = {"type":"object","properties":{"upload_id":{"type":"string"},"offset":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)$","format":"int64"},"data":{"type":["string","null"],"pattern":"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$","contentEncoding":"base64"}},"$id":"https://whip.dev/protocol/v2/UploadChunkParams","$schema":"http://json-schema.org/draft-07/schema#","title":"UploadChunkParams","required":["upload_id","offset","data"],"additionalProperties":true};

function validate134(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/UploadChunkParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.upload_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "upload_id"},message:"must have required property '"+"upload_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.offset === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "offset"},message:"must have required property '"+"offset"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.data === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "data"},message:"must have required property '"+"data"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.upload_id !== undefined){
if(typeof data.upload_id !== "string"){
const err3 = {instancePath:instancePath+"/upload_id",schemaPath:"#/properties/upload_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.offset !== undefined){
let data1 = data.offset;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err4 = {instancePath:instancePath+"/offset",schemaPath:"#/properties/offset/pattern",keyword:"pattern",params:{pattern: "^-?(0|[1-9][0-9]*)$"},message:"must match pattern \""+"^-?(0|[1-9][0-9]*)$"+"\""};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(!(formats0.validate(data1))){
const err5 = {instancePath:instancePath+"/offset",schemaPath:"#/properties/offset/format",keyword:"format",params:{format: "int64"},message:"must match format \""+"int64"+"\""};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
else {
const err6 = {instancePath:instancePath+"/offset",schemaPath:"#/properties/offset/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.data !== undefined){
let data2 = data.data;
if((typeof data2 !== "string") && (data2 !== null)){
const err7 = {instancePath:instancePath+"/data",schemaPath:"#/properties/data/type",keyword:"type",params:{type: schema135.properties.data.type},message:"must be string,null"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(typeof data2 === "string"){
if(!pattern21.test(data2)){
const err8 = {instancePath:instancePath+"/data",schemaPath:"#/properties/data/pattern",keyword:"pattern",params:{pattern: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"},message:"must match pattern \""+"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"+"\""};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
}
}
else {
const err9 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
validate134.errors = vErrors;
return errors === 0;
}

export const UploadFinishParams = validate135;
const schema136 = {"type":"object","properties":{"upload_id":{"type":"string"}},"$id":"https://whip.dev/protocol/v2/UploadFinishParams","$schema":"http://json-schema.org/draft-07/schema#","title":"UploadFinishParams","required":["upload_id"],"additionalProperties":true};

function validate135(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/UploadFinishParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.upload_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "upload_id"},message:"must have required property '"+"upload_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.upload_id !== undefined){
if(typeof data.upload_id !== "string"){
const err1 = {instancePath:instancePath+"/upload_id",schemaPath:"#/properties/upload_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
else {
const err2 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
validate135.errors = vErrors;
return errors === 0;
}

export const UserHistoryResult = validate136;
const schema137 = {"type":["null","array"],"items":{"type":"string"},"$id":"https://whip.dev/protocol/v2/UserHistoryResult","$schema":"http://json-schema.org/draft-07/schema#","title":"UserHistoryResult"};

function validate136(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v2/UserHistoryResult" */;
let vErrors = null;
let errors = 0;
if((data !== null) && (!(Array.isArray(data)))){
const err0 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: schema137.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(Array.isArray(data)){
const len0 = data.length;
for(let i0=0; i0<len0; i0++){
if(typeof data[i0] !== "string"){
const err1 = {instancePath:instancePath+"/" + i0,schemaPath:"#/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
}
validate136.errors = vErrors;
return errors === 0;
}
