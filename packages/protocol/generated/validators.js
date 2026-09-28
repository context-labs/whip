"use strict";
export const Admission = validate10;
const schema11 = {"type":"object","properties":{"receipt":{"type":"object","properties":{"identity":{"type":"object","properties":{"client_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"request_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"required":["client_id","request_id"],"additionalProperties":false},"digest":{"type":"string","pattern":"^[a-f0-9]{64}$"},"input_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"deleted_at":{"type":["null","string"]},"created_at":{"type":"string"}},"required":["identity","digest","input_id","deleted_at","created_at"],"additionalProperties":false},"input":{"type":["null","object"],"properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"source":{"type":"string","enum":["user","agent","schedule"]},"parts":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"text":{"type":"string","pattern":"^[\\s\\S]+$"},"type":{"type":"string","enum":["text"]}},"required":["type","text"],"additionalProperties":false},{"type":"object","properties":{"reference_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"type":{"type":"string","enum":["content"]}},"required":["type","reference_id"],"additionalProperties":false}]},"minItems":1,"maxItems":128},"state":{"type":"string","enum":["queued","claimed","cancelled"]},"turn_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"created_at":{"type":"string"}},"required":["id","session_id","source","parts","state","turn_id","created_at"],"additionalProperties":false},"turn":{"type":["null","object"],"properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"config_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"state":{"type":"string","enum":["running","cancelling","succeeded","failed","cancelled","interrupted"]},"failure":{"type":["null","string"]},"started_at":{"type":"string"},"finished_at":{"type":["null","string"]}},"required":["id","session_id","config_revision","state","failure","started_at","finished_at"],"additionalProperties":false}},"$id":"https://whip.dev/protocol/v4/Admission","$schema":"http://json-schema.org/draft-07/schema#","title":"Admission","required":["receipt","input","turn"],"additionalProperties":false};
const pattern0 = new RegExp("^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$", "u");
const pattern2 = new RegExp("^[a-f0-9]{64}$", "u");
const pattern6 = new RegExp("^[\\s\\S]+$", "u");
const pattern11 = new RegExp("^(0|[1-9][0-9]{0,18})$", "u");
const formats0 = {counter: {type: 'string', validate: value =>
  /^(0|[1-9][0-9]{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n}}.counter;

function validate10(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Admission" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.receipt === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "receipt"},message:"must have required property '"+"receipt"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.input === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "input"},message:"must have required property '"+"input"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.turn === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "turn"},message:"must have required property '"+"turn"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
for(const key0 in data){
if(!(((key0 === "receipt") || (key0 === "input")) || (key0 === "turn"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.receipt !== undefined){
let data0 = data.receipt;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.identity === undefined){
const err4 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/required",keyword:"required",params:{missingProperty: "identity"},message:"must have required property '"+"identity"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.digest === undefined){
const err5 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/required",keyword:"required",params:{missingProperty: "digest"},message:"must have required property '"+"digest"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.input_id === undefined){
const err6 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/required",keyword:"required",params:{missingProperty: "input_id"},message:"must have required property '"+"input_id"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data0.deleted_at === undefined){
const err7 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/required",keyword:"required",params:{missingProperty: "deleted_at"},message:"must have required property '"+"deleted_at"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data0.created_at === undefined){
const err8 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
for(const key1 in data0){
if(!(((((key1 === "identity") || (key1 === "digest")) || (key1 === "input_id")) || (key1 === "deleted_at")) || (key1 === "created_at"))){
const err9 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.identity !== undefined){
let data1 = data0.identity;
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.client_id === undefined){
const err10 = {instancePath:instancePath+"/receipt/identity",schemaPath:"#/properties/receipt/properties/identity/required",keyword:"required",params:{missingProperty: "client_id"},message:"must have required property '"+"client_id"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data1.request_id === undefined){
const err11 = {instancePath:instancePath+"/receipt/identity",schemaPath:"#/properties/receipt/properties/identity/required",keyword:"required",params:{missingProperty: "request_id"},message:"must have required property '"+"request_id"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
for(const key2 in data1){
if(!((key2 === "client_id") || (key2 === "request_id"))){
const err12 = {instancePath:instancePath+"/receipt/identity",schemaPath:"#/properties/receipt/properties/identity/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data1.client_id !== undefined){
let data2 = data1.client_id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err13 = {instancePath:instancePath+"/receipt/identity/client_id",schemaPath:"#/properties/receipt/properties/identity/properties/client_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err14 = {instancePath:instancePath+"/receipt/identity/client_id",schemaPath:"#/properties/receipt/properties/identity/properties/client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data1.request_id !== undefined){
let data3 = data1.request_id;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err15 = {instancePath:instancePath+"/receipt/identity/request_id",schemaPath:"#/properties/receipt/properties/identity/properties/request_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
else {
const err16 = {instancePath:instancePath+"/receipt/identity/request_id",schemaPath:"#/properties/receipt/properties/identity/properties/request_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err17 = {instancePath:instancePath+"/receipt/identity",schemaPath:"#/properties/receipt/properties/identity/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data0.digest !== undefined){
let data4 = data0.digest;
if(typeof data4 === "string"){
if(!pattern2.test(data4)){
const err18 = {instancePath:instancePath+"/receipt/digest",schemaPath:"#/properties/receipt/properties/digest/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
else {
const err19 = {instancePath:instancePath+"/receipt/digest",schemaPath:"#/properties/receipt/properties/digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data0.input_id !== undefined){
let data5 = data0.input_id;
if((data5 !== null) && (typeof data5 !== "string")){
const err20 = {instancePath:instancePath+"/receipt/input_id",schemaPath:"#/properties/receipt/properties/input_id/type",keyword:"type",params:{type: schema11.properties.receipt.properties.input_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(typeof data5 === "string"){
if(!pattern0.test(data5)){
const err21 = {instancePath:instancePath+"/receipt/input_id",schemaPath:"#/properties/receipt/properties/input_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data0.deleted_at !== undefined){
let data6 = data0.deleted_at;
if((data6 !== null) && (typeof data6 !== "string")){
const err22 = {instancePath:instancePath+"/receipt/deleted_at",schemaPath:"#/properties/receipt/properties/deleted_at/type",keyword:"type",params:{type: schema11.properties.receipt.properties.deleted_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data0.created_at !== undefined){
if(typeof data0.created_at !== "string"){
const err23 = {instancePath:instancePath+"/receipt/created_at",schemaPath:"#/properties/receipt/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err24 = {instancePath:instancePath+"/receipt",schemaPath:"#/properties/receipt/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data.input !== undefined){
let data8 = data.input;
if((data8 !== null) && (!(data8 && typeof data8 == "object" && !Array.isArray(data8)))){
const err25 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/type",keyword:"type",params:{type: schema11.properties.input.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
if(data8.id === undefined){
const err26 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(data8.session_id === undefined){
const err27 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data8.source === undefined){
const err28 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data8.parts === undefined){
const err29 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "parts"},message:"must have required property '"+"parts"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data8.state === undefined){
const err30 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data8.turn_id === undefined){
const err31 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data8.created_at === undefined){
const err32 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
for(const key3 in data8){
if(!(((((((key3 === "id") || (key3 === "session_id")) || (key3 === "source")) || (key3 === "parts")) || (key3 === "state")) || (key3 === "turn_id")) || (key3 === "created_at"))){
const err33 = {instancePath:instancePath+"/input",schemaPath:"#/properties/input/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data8.id !== undefined){
let data9 = data8.id;
if(typeof data9 === "string"){
if(!pattern0.test(data9)){
const err34 = {instancePath:instancePath+"/input/id",schemaPath:"#/properties/input/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
else {
const err35 = {instancePath:instancePath+"/input/id",schemaPath:"#/properties/input/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data8.session_id !== undefined){
let data10 = data8.session_id;
if(typeof data10 === "string"){
if(!pattern0.test(data10)){
const err36 = {instancePath:instancePath+"/input/session_id",schemaPath:"#/properties/input/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
else {
const err37 = {instancePath:instancePath+"/input/session_id",schemaPath:"#/properties/input/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data8.source !== undefined){
let data11 = data8.source;
if(typeof data11 !== "string"){
const err38 = {instancePath:instancePath+"/input/source",schemaPath:"#/properties/input/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(!(((data11 === "user") || (data11 === "agent")) || (data11 === "schedule"))){
const err39 = {instancePath:instancePath+"/input/source",schemaPath:"#/properties/input/properties/source/enum",keyword:"enum",params:{allowedValues: schema11.properties.input.properties.source.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
if(data8.parts !== undefined){
let data12 = data8.parts;
if(Array.isArray(data12)){
if(data12.length > 128){
const err40 = {instancePath:instancePath+"/input/parts",schemaPath:"#/properties/input/properties/parts/maxItems",keyword:"maxItems",params:{limit: 128},message:"must NOT have more than 128 items"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data12.length < 1){
const err41 = {instancePath:instancePath+"/input/parts",schemaPath:"#/properties/input/properties/parts/minItems",keyword:"minItems",params:{limit: 1},message:"must NOT have fewer than 1 items"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
const len0 = data12.length;
for(let i0=0; i0<len0; i0++){
let data13 = data12[i0];
const _errs32 = errors;
let valid6 = false;
let passing0 = null;
const _errs33 = errors;
if(data13 && typeof data13 == "object" && !Array.isArray(data13)){
if(data13.type === undefined){
const err42 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data13.text === undefined){
const err43 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
for(const key4 in data13){
if(!((key4 === "text") || (key4 === "type"))){
const err44 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/0/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data13.text !== undefined){
let data14 = data13.text;
if(typeof data14 === "string"){
if(!pattern6.test(data14)){
const err45 = {instancePath:instancePath+"/input/parts/" + i0+"/text",schemaPath:"#/properties/input/properties/parts/items/oneOf/0/properties/text/pattern",keyword:"pattern",params:{pattern: "^[\\s\\S]+$"},message:"must match pattern \""+"^[\\s\\S]+$"+"\""};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
else {
const err46 = {instancePath:instancePath+"/input/parts/" + i0+"/text",schemaPath:"#/properties/input/properties/parts/items/oneOf/0/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data13.type !== undefined){
let data15 = data13.type;
if(typeof data15 !== "string"){
const err47 = {instancePath:instancePath+"/input/parts/" + i0+"/type",schemaPath:"#/properties/input/properties/parts/items/oneOf/0/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
if(!(data15 === "text")){
const err48 = {instancePath:instancePath+"/input/parts/" + i0+"/type",schemaPath:"#/properties/input/properties/parts/items/oneOf/0/properties/type/enum",keyword:"enum",params:{allowedValues: schema11.properties.input.properties.parts.items.oneOf[0].properties.type.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
}
else {
const err49 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/0/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
var _valid0 = _errs33 === errors;
if(_valid0){
valid6 = true;
passing0 = 0;
}
const _errs40 = errors;
if(data13 && typeof data13 == "object" && !Array.isArray(data13)){
if(data13.type === undefined){
const err50 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
if(data13.reference_id === undefined){
const err51 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
for(const key5 in data13){
if(!((key5 === "reference_id") || (key5 === "type"))){
const err52 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/1/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data13.reference_id !== undefined){
let data16 = data13.reference_id;
if(typeof data16 === "string"){
if(!pattern0.test(data16)){
const err53 = {instancePath:instancePath+"/input/parts/" + i0+"/reference_id",schemaPath:"#/properties/input/properties/parts/items/oneOf/1/properties/reference_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
else {
const err54 = {instancePath:instancePath+"/input/parts/" + i0+"/reference_id",schemaPath:"#/properties/input/properties/parts/items/oneOf/1/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data13.type !== undefined){
let data17 = data13.type;
if(typeof data17 !== "string"){
const err55 = {instancePath:instancePath+"/input/parts/" + i0+"/type",schemaPath:"#/properties/input/properties/parts/items/oneOf/1/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(!(data17 === "content")){
const err56 = {instancePath:instancePath+"/input/parts/" + i0+"/type",schemaPath:"#/properties/input/properties/parts/items/oneOf/1/properties/type/enum",keyword:"enum",params:{allowedValues: schema11.properties.input.properties.parts.items.oneOf[1].properties.type.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
}
else {
const err57 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf/1/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
var _valid0 = _errs40 === errors;
if(_valid0 && valid6){
valid6 = false;
passing0 = [passing0, 1];
}
else {
if(_valid0){
valid6 = true;
passing0 = 1;
}
}
if(!valid6){
const err58 = {instancePath:instancePath+"/input/parts/" + i0,schemaPath:"#/properties/input/properties/parts/items/oneOf",keyword:"oneOf",params:{passingSchemas: passing0},message:"must match exactly one schema in oneOf"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
else {
errors = _errs32;
if(vErrors !== null){
if(_errs32){
vErrors.length = _errs32;
}
else {
vErrors = null;
}
}
}
}
}
else {
const err59 = {instancePath:instancePath+"/input/parts",schemaPath:"#/properties/input/properties/parts/type",keyword:"type",params:{type: "array"},message:"must be array"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data8.state !== undefined){
let data18 = data8.state;
if(typeof data18 !== "string"){
const err60 = {instancePath:instancePath+"/input/state",schemaPath:"#/properties/input/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(!(((data18 === "queued") || (data18 === "claimed")) || (data18 === "cancelled"))){
const err61 = {instancePath:instancePath+"/input/state",schemaPath:"#/properties/input/properties/state/enum",keyword:"enum",params:{allowedValues: schema11.properties.input.properties.state.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
}
if(data8.turn_id !== undefined){
let data19 = data8.turn_id;
if((data19 !== null) && (typeof data19 !== "string")){
const err62 = {instancePath:instancePath+"/input/turn_id",schemaPath:"#/properties/input/properties/turn_id/type",keyword:"type",params:{type: schema11.properties.input.properties.turn_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
if(typeof data19 === "string"){
if(!pattern0.test(data19)){
const err63 = {instancePath:instancePath+"/input/turn_id",schemaPath:"#/properties/input/properties/turn_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data8.created_at !== undefined){
if(typeof data8.created_at !== "string"){
const err64 = {instancePath:instancePath+"/input/created_at",schemaPath:"#/properties/input/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data.turn !== undefined){
let data21 = data.turn;
if((data21 !== null) && (!(data21 && typeof data21 == "object" && !Array.isArray(data21)))){
const err65 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/type",keyword:"type",params:{type: schema11.properties.turn.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
if(data21 && typeof data21 == "object" && !Array.isArray(data21)){
if(data21.id === undefined){
const err66 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
if(data21.session_id === undefined){
const err67 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
if(data21.config_revision === undefined){
const err68 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "config_revision"},message:"must have required property '"+"config_revision"+"'"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
if(data21.state === undefined){
const err69 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
if(data21.failure === undefined){
const err70 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "failure"},message:"must have required property '"+"failure"+"'"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
if(data21.started_at === undefined){
const err71 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "started_at"},message:"must have required property '"+"started_at"+"'"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
if(data21.finished_at === undefined){
const err72 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/required",keyword:"required",params:{missingProperty: "finished_at"},message:"must have required property '"+"finished_at"+"'"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
for(const key6 in data21){
if(!(((((((key6 === "id") || (key6 === "session_id")) || (key6 === "config_revision")) || (key6 === "state")) || (key6 === "failure")) || (key6 === "started_at")) || (key6 === "finished_at"))){
const err73 = {instancePath:instancePath+"/turn",schemaPath:"#/properties/turn/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key6},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
}
if(data21.id !== undefined){
let data22 = data21.id;
if(typeof data22 === "string"){
if(!pattern0.test(data22)){
const err74 = {instancePath:instancePath+"/turn/id",schemaPath:"#/properties/turn/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
}
else {
const err75 = {instancePath:instancePath+"/turn/id",schemaPath:"#/properties/turn/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
if(data21.session_id !== undefined){
let data23 = data21.session_id;
if(typeof data23 === "string"){
if(!pattern0.test(data23)){
const err76 = {instancePath:instancePath+"/turn/session_id",schemaPath:"#/properties/turn/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
}
else {
const err77 = {instancePath:instancePath+"/turn/session_id",schemaPath:"#/properties/turn/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
}
if(data21.config_revision !== undefined){
let data24 = data21.config_revision;
if(typeof data24 === "string"){
if(!pattern11.test(data24)){
const err78 = {instancePath:instancePath+"/turn/config_revision",schemaPath:"#/properties/turn/properties/config_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
if(!(formats0.validate(data24))){
const err79 = {instancePath:instancePath+"/turn/config_revision",schemaPath:"#/properties/turn/properties/config_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
}
else {
const err80 = {instancePath:instancePath+"/turn/config_revision",schemaPath:"#/properties/turn/properties/config_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
}
if(data21.state !== undefined){
let data25 = data21.state;
if(typeof data25 !== "string"){
const err81 = {instancePath:instancePath+"/turn/state",schemaPath:"#/properties/turn/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
if(!((((((data25 === "running") || (data25 === "cancelling")) || (data25 === "succeeded")) || (data25 === "failed")) || (data25 === "cancelled")) || (data25 === "interrupted"))){
const err82 = {instancePath:instancePath+"/turn/state",schemaPath:"#/properties/turn/properties/state/enum",keyword:"enum",params:{allowedValues: schema11.properties.turn.properties.state.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
if(data21.failure !== undefined){
let data26 = data21.failure;
if((data26 !== null) && (typeof data26 !== "string")){
const err83 = {instancePath:instancePath+"/turn/failure",schemaPath:"#/properties/turn/properties/failure/type",keyword:"type",params:{type: schema11.properties.turn.properties.failure.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
}
if(data21.started_at !== undefined){
if(typeof data21.started_at !== "string"){
const err84 = {instancePath:instancePath+"/turn/started_at",schemaPath:"#/properties/turn/properties/started_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
}
if(data21.finished_at !== undefined){
let data28 = data21.finished_at;
if((data28 !== null) && (typeof data28 !== "string")){
const err85 = {instancePath:instancePath+"/turn/finished_at",schemaPath:"#/properties/turn/properties/finished_at/type",keyword:"type",params:{type: schema11.properties.turn.properties.finished_at.type},message:"must be null,string"};
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
else {
const err86 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
validate10.errors = vErrors;
return errors === 0;
}

export const CreateTreeParams = validate11;
const schema12 = {"type":"object","properties":{"metadata":{"type":"object","properties":{"title":{"type":["null","string"]},"archived":{"type":"boolean"},"pinned":{"type":"boolean"}},"required":["title","archived","pinned"],"additionalProperties":false},"engine":{"type":"string","enum":["starlark","quickjs"]},"policy":{"type":"object","properties":{"max_depth":{"type":"integer","minimum":0,"maximum":128},"max_sessions":{"type":"integer","minimum":1,"maximum":10000},"max_queued_inputs_per_session":{"type":"integer","minimum":1,"maximum":10000}},"required":["max_depth","max_sessions","max_queued_inputs_per_session"],"additionalProperties":false},"definition":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false},"overrides":{"type":"object","properties":{"model":{"type":["null","object"],"properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":["null","object"],"properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output":{"type":["null","object"],"properties":{"schema":true},"required":["schema"],"additionalProperties":false}},"additionalProperties":false},"working_directory":{"type":"string"}},"$id":"https://whip.dev/protocol/v4/CreateTreeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"CreateTreeParams","required":["metadata","engine","policy","definition","overrides","working_directory"],"additionalProperties":false};

function validate11(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/CreateTreeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.metadata === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "metadata"},message:"must have required property '"+"metadata"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.engine === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "engine"},message:"must have required property '"+"engine"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.policy === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "policy"},message:"must have required property '"+"policy"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.definition === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.overrides === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "overrides"},message:"must have required property '"+"overrides"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.working_directory === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "working_directory"},message:"must have required property '"+"working_directory"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
for(const key0 in data){
if(!((((((key0 === "metadata") || (key0 === "engine")) || (key0 === "policy")) || (key0 === "definition")) || (key0 === "overrides")) || (key0 === "working_directory"))){
const err6 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.metadata !== undefined){
let data0 = data.metadata;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.title === undefined){
const err7 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data0.archived === undefined){
const err8 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "archived"},message:"must have required property '"+"archived"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data0.pinned === undefined){
const err9 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
for(const key1 in data0){
if(!(((key1 === "title") || (key1 === "archived")) || (key1 === "pinned"))){
const err10 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data0.title !== undefined){
let data1 = data0.title;
if((data1 !== null) && (typeof data1 !== "string")){
const err11 = {instancePath:instancePath+"/metadata/title",schemaPath:"#/properties/metadata/properties/title/type",keyword:"type",params:{type: schema12.properties.metadata.properties.title.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data0.archived !== undefined){
if(typeof data0.archived !== "boolean"){
const err12 = {instancePath:instancePath+"/metadata/archived",schemaPath:"#/properties/metadata/properties/archived/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data0.pinned !== undefined){
if(typeof data0.pinned !== "boolean"){
const err13 = {instancePath:instancePath+"/metadata/pinned",schemaPath:"#/properties/metadata/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
const err14 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.engine !== undefined){
let data4 = data.engine;
if(typeof data4 !== "string"){
const err15 = {instancePath:instancePath+"/engine",schemaPath:"#/properties/engine/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(!((data4 === "starlark") || (data4 === "quickjs"))){
const err16 = {instancePath:instancePath+"/engine",schemaPath:"#/properties/engine/enum",keyword:"enum",params:{allowedValues: schema12.properties.engine.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.policy !== undefined){
let data5 = data.policy;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.max_depth === undefined){
const err17 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/required",keyword:"required",params:{missingProperty: "max_depth"},message:"must have required property '"+"max_depth"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data5.max_sessions === undefined){
const err18 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/required",keyword:"required",params:{missingProperty: "max_sessions"},message:"must have required property '"+"max_sessions"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data5.max_queued_inputs_per_session === undefined){
const err19 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/required",keyword:"required",params:{missingProperty: "max_queued_inputs_per_session"},message:"must have required property '"+"max_queued_inputs_per_session"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
for(const key2 in data5){
if(!(((key2 === "max_depth") || (key2 === "max_sessions")) || (key2 === "max_queued_inputs_per_session"))){
const err20 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data5.max_depth !== undefined){
let data6 = data5.max_depth;
if(!((typeof data6 == "number") && (!(data6 % 1) && !isNaN(data6)))){
const err21 = {instancePath:instancePath+"/policy/max_depth",schemaPath:"#/properties/policy/properties/max_depth/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(typeof data6 == "number"){
if(data6 > 128 || isNaN(data6)){
const err22 = {instancePath:instancePath+"/policy/max_depth",schemaPath:"#/properties/policy/properties/max_depth/maximum",keyword:"maximum",params:{comparison: "<=", limit: 128},message:"must be <= 128"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(data6 < 0 || isNaN(data6)){
const err23 = {instancePath:instancePath+"/policy/max_depth",schemaPath:"#/properties/policy/properties/max_depth/minimum",keyword:"minimum",params:{comparison: ">=", limit: 0},message:"must be >= 0"};
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
if(data5.max_sessions !== undefined){
let data7 = data5.max_sessions;
if(!((typeof data7 == "number") && (!(data7 % 1) && !isNaN(data7)))){
const err24 = {instancePath:instancePath+"/policy/max_sessions",schemaPath:"#/properties/policy/properties/max_sessions/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(typeof data7 == "number"){
if(data7 > 10000 || isNaN(data7)){
const err25 = {instancePath:instancePath+"/policy/max_sessions",schemaPath:"#/properties/policy/properties/max_sessions/maximum",keyword:"maximum",params:{comparison: "<=", limit: 10000},message:"must be <= 10000"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data7 < 1 || isNaN(data7)){
const err26 = {instancePath:instancePath+"/policy/max_sessions",schemaPath:"#/properties/policy/properties/max_sessions/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
}
if(data5.max_queued_inputs_per_session !== undefined){
let data8 = data5.max_queued_inputs_per_session;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err27 = {instancePath:instancePath+"/policy/max_queued_inputs_per_session",schemaPath:"#/properties/policy/properties/max_queued_inputs_per_session/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(typeof data8 == "number"){
if(data8 > 10000 || isNaN(data8)){
const err28 = {instancePath:instancePath+"/policy/max_queued_inputs_per_session",schemaPath:"#/properties/policy/properties/max_queued_inputs_per_session/maximum",keyword:"maximum",params:{comparison: "<=", limit: 10000},message:"must be <= 10000"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data8 < 1 || isNaN(data8)){
const err29 = {instancePath:instancePath+"/policy/max_queued_inputs_per_session",schemaPath:"#/properties/policy/properties/max_queued_inputs_per_session/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
}
}
else {
const err30 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data.definition !== undefined){
let data9 = data.definition;
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.id === undefined){
const err31 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data9.revision === undefined){
const err32 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
for(const key3 in data9){
if(!((key3 === "id") || (key3 === "revision"))){
const err33 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data9.id !== undefined){
let data10 = data9.id;
if(typeof data10 === "string"){
if(!pattern0.test(data10)){
const err34 = {instancePath:instancePath+"/definition/id",schemaPath:"#/properties/definition/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
else {
const err35 = {instancePath:instancePath+"/definition/id",schemaPath:"#/properties/definition/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data9.revision !== undefined){
let data11 = data9.revision;
if(typeof data11 === "string"){
if(!pattern2.test(data11)){
const err36 = {instancePath:instancePath+"/definition/revision",schemaPath:"#/properties/definition/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
else {
const err37 = {instancePath:instancePath+"/definition/revision",schemaPath:"#/properties/definition/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err38 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
if(data.overrides !== undefined){
let data12 = data.overrides;
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
for(const key4 in data12){
if(!((((((key4 === "model") || (key4 === "instructions")) || (key4 === "tools")) || (key4 === "children")) || (key4 === "hooks")) || (key4 === "output"))){
const err39 = {instancePath:instancePath+"/overrides",schemaPath:"#/properties/overrides/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
if(data12.model !== undefined){
let data13 = data12.model;
if((data13 !== null) && (!(data13 && typeof data13 == "object" && !Array.isArray(data13)))){
const err40 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/type",keyword:"type",params:{type: schema12.properties.overrides.properties.model.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data13 && typeof data13 == "object" && !Array.isArray(data13)){
if(data13.provider === undefined){
const err41 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
if(data13.name === undefined){
const err42 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data13.effort === undefined){
const err43 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
for(const key5 in data13){
if(!(((key5 === "provider") || (key5 === "name")) || (key5 === "effort"))){
const err44 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data13.provider !== undefined){
let data14 = data13.provider;
if(typeof data14 === "string"){
if(!pattern0.test(data14)){
const err45 = {instancePath:instancePath+"/overrides/model/provider",schemaPath:"#/properties/overrides/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
else {
const err46 = {instancePath:instancePath+"/overrides/model/provider",schemaPath:"#/properties/overrides/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data13.name !== undefined){
if(typeof data13.name !== "string"){
const err47 = {instancePath:instancePath+"/overrides/model/name",schemaPath:"#/properties/overrides/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data13.effort !== undefined){
if(typeof data13.effort !== "string"){
const err48 = {instancePath:instancePath+"/overrides/model/effort",schemaPath:"#/properties/overrides/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
}
}
if(data12.instructions !== undefined){
let data17 = data12.instructions;
if((data17 !== null) && (!(data17 && typeof data17 == "object" && !Array.isArray(data17)))){
const err49 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/type",keyword:"type",params:{type: schema12.properties.overrides.properties.instructions.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
if(data17 && typeof data17 == "object" && !Array.isArray(data17)){
if(data17.text === undefined){
const err50 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
if(data17.project_files === undefined){
const err51 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
if(data17.discover_skills === undefined){
const err52 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
for(const key6 in data17){
if(!(((key6 === "text") || (key6 === "project_files")) || (key6 === "discover_skills"))){
const err53 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key6},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
if(data17.text !== undefined){
if(typeof data17.text !== "string"){
const err54 = {instancePath:instancePath+"/overrides/instructions/text",schemaPath:"#/properties/overrides/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data17.project_files !== undefined){
let data19 = data17.project_files;
if((data19 !== null) && (!(Array.isArray(data19)))){
const err55 = {instancePath:instancePath+"/overrides/instructions/project_files",schemaPath:"#/properties/overrides/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema12.properties.overrides.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(Array.isArray(data19)){
const len0 = data19.length;
for(let i0=0; i0<len0; i0++){
if(typeof data19[i0] !== "string"){
const err56 = {instancePath:instancePath+"/overrides/instructions/project_files/" + i0,schemaPath:"#/properties/overrides/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
}
}
if(data17.discover_skills !== undefined){
if(typeof data17.discover_skills !== "boolean"){
const err57 = {instancePath:instancePath+"/overrides/instructions/discover_skills",schemaPath:"#/properties/overrides/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
if(data12.tools !== undefined){
let data22 = data12.tools;
if((!(data22 && typeof data22 == "object" && !Array.isArray(data22))) && (data22 !== null)){
const err58 = {instancePath:instancePath+"/overrides/tools",schemaPath:"#/properties/overrides/properties/tools/type",keyword:"type",params:{type: schema12.properties.overrides.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
if(data22 && typeof data22 == "object" && !Array.isArray(data22)){
for(const key7 in data22){
let data23 = data22[key7];
if(data23 && typeof data23 == "object" && !Array.isArray(data23)){
if(data23.description === undefined){
const err59 = {instancePath:instancePath+"/overrides/tools/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
if(data23.input_schema === undefined){
const err60 = {instancePath:instancePath+"/overrides/tools/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(data23.output_schema === undefined){
const err61 = {instancePath:instancePath+"/overrides/tools/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
for(const key8 in data23){
if(!(((key8 === "description") || (key8 === "input_schema")) || (key8 === "output_schema"))){
const err62 = {instancePath:instancePath+"/overrides/tools/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key8},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
if(data23.description !== undefined){
if(typeof data23.description !== "string"){
const err63 = {instancePath:instancePath+"/overrides/tools/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/overrides/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err64 = {instancePath:instancePath+"/overrides/tools/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
if(data12.children !== undefined){
let data25 = data12.children;
if((!(data25 && typeof data25 == "object" && !Array.isArray(data25))) && (data25 !== null)){
const err65 = {instancePath:instancePath+"/overrides/children",schemaPath:"#/properties/overrides/properties/children/type",keyword:"type",params:{type: schema12.properties.overrides.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
if(data25 && typeof data25 == "object" && !Array.isArray(data25)){
for(const key9 in data25){
let data26 = data25[key9];
if(data26 && typeof data26 == "object" && !Array.isArray(data26)){
if(data26.id === undefined){
const err66 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
if(data26.revision === undefined){
const err67 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
for(const key10 in data26){
if(!((key10 === "id") || (key10 === "revision"))){
const err68 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key10},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
}
if(data26.id !== undefined){
let data27 = data26.id;
if(typeof data27 === "string"){
if(!pattern0.test(data27)){
const err69 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
}
else {
const err70 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
}
if(data26.revision !== undefined){
let data28 = data26.revision;
if(typeof data28 === "string"){
if(!pattern2.test(data28)){
const err71 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
}
else {
const err72 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err73 = {instancePath:instancePath+"/overrides/children/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
if(data12.hooks !== undefined){
let data29 = data12.hooks;
if((!(data29 && typeof data29 == "object" && !Array.isArray(data29))) && (data29 !== null)){
const err74 = {instancePath:instancePath+"/overrides/hooks",schemaPath:"#/properties/overrides/properties/hooks/type",keyword:"type",params:{type: schema12.properties.overrides.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
if(data29 && typeof data29 == "object" && !Array.isArray(data29)){
for(const key11 in data29){
let data30 = data29[key11];
if(data30 && typeof data30 == "object" && !Array.isArray(data30)){
if(data30.operations === undefined){
const err75 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
if(data30.optional === undefined){
const err76 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
if(data30.timeout_millis === undefined){
const err77 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
for(const key12 in data30){
if(!(((key12 === "operations") || (key12 === "optional")) || (key12 === "timeout_millis"))){
const err78 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key12},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
}
if(data30.operations !== undefined){
let data31 = data30.operations;
if((data31 !== null) && (!(Array.isArray(data31)))){
const err79 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema12.properties.overrides.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
if(Array.isArray(data31)){
const len1 = data31.length;
for(let i1=0; i1<len1; i1++){
let data32 = data31[i1];
if(typeof data32 === "string"){
if(!pattern0.test(data32)){
const err80 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err81 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
}
}
}
if(data30.optional !== undefined){
if(typeof data30.optional !== "boolean"){
const err82 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
if(data30.timeout_millis !== undefined){
let data34 = data30.timeout_millis;
if(!((typeof data34 == "number") && (!(data34 % 1) && !isNaN(data34)))){
const err83 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
if(typeof data34 == "number"){
if(data34 > 60000 || isNaN(data34)){
const err84 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
if(data34 < 1 || isNaN(data34)){
const err85 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
else {
const err86 = {instancePath:instancePath+"/overrides/hooks/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
}
}
}
if(data12.output !== undefined){
let data35 = data12.output;
if((data35 !== null) && (!(data35 && typeof data35 == "object" && !Array.isArray(data35)))){
const err87 = {instancePath:instancePath+"/overrides/output",schemaPath:"#/properties/overrides/properties/output/type",keyword:"type",params:{type: schema12.properties.overrides.properties.output.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
if(data35 && typeof data35 == "object" && !Array.isArray(data35)){
if(data35.schema === undefined){
const err88 = {instancePath:instancePath+"/overrides/output",schemaPath:"#/properties/overrides/properties/output/required",keyword:"required",params:{missingProperty: "schema"},message:"must have required property '"+"schema"+"'"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
for(const key13 in data35){
if(!(key13 === "schema")){
const err89 = {instancePath:instancePath+"/overrides/output",schemaPath:"#/properties/overrides/properties/output/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key13},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
}
}
}
}
else {
const err90 = {instancePath:instancePath+"/overrides",schemaPath:"#/properties/overrides/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
}
if(data.working_directory !== undefined){
if(typeof data.working_directory !== "string"){
const err91 = {instancePath:instancePath+"/working_directory",schemaPath:"#/properties/working_directory/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err91];
}
else {
vErrors.push(err91);
}
errors++;
}
}
}
else {
const err92 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err92];
}
else {
vErrors.push(err92);
}
errors++;
}
validate11.errors = vErrors;
return errors === 0;
}

export const CreateTreeResult = validate12;
const schema13 = {"type":"object","properties":{"tree":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"metadata":{"type":"object","properties":{"title":{"type":["null","string"]},"archived":{"type":"boolean"},"pinned":{"type":"boolean"}},"required":["title","archived","pinned"],"additionalProperties":false},"engine":{"type":"string","enum":["starlark","quickjs"]},"policy":{"type":"object","properties":{"max_depth":{"type":"integer","minimum":0,"maximum":128},"max_sessions":{"type":"integer","minimum":1,"maximum":10000},"max_queued_inputs_per_session":{"type":"integer","minimum":1,"maximum":10000}},"required":["max_depth","max_sessions","max_queued_inputs_per_session"],"additionalProperties":false},"revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"created_at":{"type":"string"}},"required":["id","metadata","engine","policy","revision","created_at"],"additionalProperties":false},"root":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"tree_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"parent_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"definition":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false},"config_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"configuration":{"type":"object","properties":{"model":{"type":"object","properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":"object","properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output_schema":true},"required":["model","instructions","tools","children","hooks","output_schema"],"additionalProperties":false},"working_directory":{"type":"string"},"lifecycle":{"type":"string","enum":["active","stopped"]},"created_at":{"type":"string"}},"required":["id","tree_id","parent_id","definition","config_revision","configuration","working_directory","lifecycle","created_at"],"additionalProperties":false}},"$id":"https://whip.dev/protocol/v4/CreateTreeResult","$schema":"http://json-schema.org/draft-07/schema#","title":"CreateTreeResult","required":["tree","root"],"additionalProperties":false};
const func2 = Object.prototype.hasOwnProperty;

function validate12(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/CreateTreeResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.tree === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "tree"},message:"must have required property '"+"tree"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.root === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "root"},message:"must have required property '"+"root"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
for(const key0 in data){
if(!((key0 === "tree") || (key0 === "root"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.tree !== undefined){
let data0 = data.tree;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.id === undefined){
const err3 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0.metadata === undefined){
const err4 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/required",keyword:"required",params:{missingProperty: "metadata"},message:"must have required property '"+"metadata"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.engine === undefined){
const err5 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/required",keyword:"required",params:{missingProperty: "engine"},message:"must have required property '"+"engine"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.policy === undefined){
const err6 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/required",keyword:"required",params:{missingProperty: "policy"},message:"must have required property '"+"policy"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data0.revision === undefined){
const err7 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data0.created_at === undefined){
const err8 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
for(const key1 in data0){
if(!((((((key1 === "id") || (key1 === "metadata")) || (key1 === "engine")) || (key1 === "policy")) || (key1 === "revision")) || (key1 === "created_at"))){
const err9 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.id !== undefined){
let data1 = data0.id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err10 = {instancePath:instancePath+"/tree/id",schemaPath:"#/properties/tree/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err11 = {instancePath:instancePath+"/tree/id",schemaPath:"#/properties/tree/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data0.metadata !== undefined){
let data2 = data0.metadata;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.title === undefined){
const err12 = {instancePath:instancePath+"/tree/metadata",schemaPath:"#/properties/tree/properties/metadata/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data2.archived === undefined){
const err13 = {instancePath:instancePath+"/tree/metadata",schemaPath:"#/properties/tree/properties/metadata/required",keyword:"required",params:{missingProperty: "archived"},message:"must have required property '"+"archived"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data2.pinned === undefined){
const err14 = {instancePath:instancePath+"/tree/metadata",schemaPath:"#/properties/tree/properties/metadata/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
for(const key2 in data2){
if(!(((key2 === "title") || (key2 === "archived")) || (key2 === "pinned"))){
const err15 = {instancePath:instancePath+"/tree/metadata",schemaPath:"#/properties/tree/properties/metadata/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data2.title !== undefined){
let data3 = data2.title;
if((data3 !== null) && (typeof data3 !== "string")){
const err16 = {instancePath:instancePath+"/tree/metadata/title",schemaPath:"#/properties/tree/properties/metadata/properties/title/type",keyword:"type",params:{type: schema13.properties.tree.properties.metadata.properties.title.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data2.archived !== undefined){
if(typeof data2.archived !== "boolean"){
const err17 = {instancePath:instancePath+"/tree/metadata/archived",schemaPath:"#/properties/tree/properties/metadata/properties/archived/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data2.pinned !== undefined){
if(typeof data2.pinned !== "boolean"){
const err18 = {instancePath:instancePath+"/tree/metadata/pinned",schemaPath:"#/properties/tree/properties/metadata/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
else {
const err19 = {instancePath:instancePath+"/tree/metadata",schemaPath:"#/properties/tree/properties/metadata/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data0.engine !== undefined){
let data6 = data0.engine;
if(typeof data6 !== "string"){
const err20 = {instancePath:instancePath+"/tree/engine",schemaPath:"#/properties/tree/properties/engine/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(!((data6 === "starlark") || (data6 === "quickjs"))){
const err21 = {instancePath:instancePath+"/tree/engine",schemaPath:"#/properties/tree/properties/engine/enum",keyword:"enum",params:{allowedValues: schema13.properties.tree.properties.engine.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data0.policy !== undefined){
let data7 = data0.policy;
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.max_depth === undefined){
const err22 = {instancePath:instancePath+"/tree/policy",schemaPath:"#/properties/tree/properties/policy/required",keyword:"required",params:{missingProperty: "max_depth"},message:"must have required property '"+"max_depth"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(data7.max_sessions === undefined){
const err23 = {instancePath:instancePath+"/tree/policy",schemaPath:"#/properties/tree/properties/policy/required",keyword:"required",params:{missingProperty: "max_sessions"},message:"must have required property '"+"max_sessions"+"'"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(data7.max_queued_inputs_per_session === undefined){
const err24 = {instancePath:instancePath+"/tree/policy",schemaPath:"#/properties/tree/properties/policy/required",keyword:"required",params:{missingProperty: "max_queued_inputs_per_session"},message:"must have required property '"+"max_queued_inputs_per_session"+"'"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
for(const key3 in data7){
if(!(((key3 === "max_depth") || (key3 === "max_sessions")) || (key3 === "max_queued_inputs_per_session"))){
const err25 = {instancePath:instancePath+"/tree/policy",schemaPath:"#/properties/tree/properties/policy/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
}
if(data7.max_depth !== undefined){
let data8 = data7.max_depth;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err26 = {instancePath:instancePath+"/tree/policy/max_depth",schemaPath:"#/properties/tree/properties/policy/properties/max_depth/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(typeof data8 == "number"){
if(data8 > 128 || isNaN(data8)){
const err27 = {instancePath:instancePath+"/tree/policy/max_depth",schemaPath:"#/properties/tree/properties/policy/properties/max_depth/maximum",keyword:"maximum",params:{comparison: "<=", limit: 128},message:"must be <= 128"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data8 < 0 || isNaN(data8)){
const err28 = {instancePath:instancePath+"/tree/policy/max_depth",schemaPath:"#/properties/tree/properties/policy/properties/max_depth/minimum",keyword:"minimum",params:{comparison: ">=", limit: 0},message:"must be >= 0"};
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
if(data7.max_sessions !== undefined){
let data9 = data7.max_sessions;
if(!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9)))){
const err29 = {instancePath:instancePath+"/tree/policy/max_sessions",schemaPath:"#/properties/tree/properties/policy/properties/max_sessions/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(typeof data9 == "number"){
if(data9 > 10000 || isNaN(data9)){
const err30 = {instancePath:instancePath+"/tree/policy/max_sessions",schemaPath:"#/properties/tree/properties/policy/properties/max_sessions/maximum",keyword:"maximum",params:{comparison: "<=", limit: 10000},message:"must be <= 10000"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data9 < 1 || isNaN(data9)){
const err31 = {instancePath:instancePath+"/tree/policy/max_sessions",schemaPath:"#/properties/tree/properties/policy/properties/max_sessions/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
if(data7.max_queued_inputs_per_session !== undefined){
let data10 = data7.max_queued_inputs_per_session;
if(!((typeof data10 == "number") && (!(data10 % 1) && !isNaN(data10)))){
const err32 = {instancePath:instancePath+"/tree/policy/max_queued_inputs_per_session",schemaPath:"#/properties/tree/properties/policy/properties/max_queued_inputs_per_session/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(typeof data10 == "number"){
if(data10 > 10000 || isNaN(data10)){
const err33 = {instancePath:instancePath+"/tree/policy/max_queued_inputs_per_session",schemaPath:"#/properties/tree/properties/policy/properties/max_queued_inputs_per_session/maximum",keyword:"maximum",params:{comparison: "<=", limit: 10000},message:"must be <= 10000"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data10 < 1 || isNaN(data10)){
const err34 = {instancePath:instancePath+"/tree/policy/max_queued_inputs_per_session",schemaPath:"#/properties/tree/properties/policy/properties/max_queued_inputs_per_session/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
else {
const err35 = {instancePath:instancePath+"/tree/policy",schemaPath:"#/properties/tree/properties/policy/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data0.revision !== undefined){
let data11 = data0.revision;
if(typeof data11 === "string"){
if(!pattern11.test(data11)){
const err36 = {instancePath:instancePath+"/tree/revision",schemaPath:"#/properties/tree/properties/revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(!(formats0.validate(data11))){
const err37 = {instancePath:instancePath+"/tree/revision",schemaPath:"#/properties/tree/properties/revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err38 = {instancePath:instancePath+"/tree/revision",schemaPath:"#/properties/tree/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
if(data0.created_at !== undefined){
if(typeof data0.created_at !== "string"){
const err39 = {instancePath:instancePath+"/tree/created_at",schemaPath:"#/properties/tree/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err40 = {instancePath:instancePath+"/tree",schemaPath:"#/properties/tree/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data.root !== undefined){
let data13 = data.root;
if(data13 && typeof data13 == "object" && !Array.isArray(data13)){
if(data13.id === undefined){
const err41 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
if(data13.tree_id === undefined){
const err42 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "tree_id"},message:"must have required property '"+"tree_id"+"'"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data13.parent_id === undefined){
const err43 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
if(data13.definition === undefined){
const err44 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
if(data13.config_revision === undefined){
const err45 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "config_revision"},message:"must have required property '"+"config_revision"+"'"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(data13.configuration === undefined){
const err46 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "configuration"},message:"must have required property '"+"configuration"+"'"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
if(data13.working_directory === undefined){
const err47 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "working_directory"},message:"must have required property '"+"working_directory"+"'"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
if(data13.lifecycle === undefined){
const err48 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "lifecycle"},message:"must have required property '"+"lifecycle"+"'"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
if(data13.created_at === undefined){
const err49 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
for(const key4 in data13){
if(!(func2.call(schema13.properties.root.properties, key4))){
const err50 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data13.id !== undefined){
let data14 = data13.id;
if(typeof data14 === "string"){
if(!pattern0.test(data14)){
const err51 = {instancePath:instancePath+"/root/id",schemaPath:"#/properties/root/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
else {
const err52 = {instancePath:instancePath+"/root/id",schemaPath:"#/properties/root/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data13.tree_id !== undefined){
let data15 = data13.tree_id;
if(typeof data15 === "string"){
if(!pattern0.test(data15)){
const err53 = {instancePath:instancePath+"/root/tree_id",schemaPath:"#/properties/root/properties/tree_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
else {
const err54 = {instancePath:instancePath+"/root/tree_id",schemaPath:"#/properties/root/properties/tree_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data13.parent_id !== undefined){
let data16 = data13.parent_id;
if((data16 !== null) && (typeof data16 !== "string")){
const err55 = {instancePath:instancePath+"/root/parent_id",schemaPath:"#/properties/root/properties/parent_id/type",keyword:"type",params:{type: schema13.properties.root.properties.parent_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(typeof data16 === "string"){
if(!pattern0.test(data16)){
const err56 = {instancePath:instancePath+"/root/parent_id",schemaPath:"#/properties/root/properties/parent_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
}
if(data13.definition !== undefined){
let data17 = data13.definition;
if(data17 && typeof data17 == "object" && !Array.isArray(data17)){
if(data17.id === undefined){
const err57 = {instancePath:instancePath+"/root/definition",schemaPath:"#/properties/root/properties/definition/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
if(data17.revision === undefined){
const err58 = {instancePath:instancePath+"/root/definition",schemaPath:"#/properties/root/properties/definition/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
for(const key5 in data17){
if(!((key5 === "id") || (key5 === "revision"))){
const err59 = {instancePath:instancePath+"/root/definition",schemaPath:"#/properties/root/properties/definition/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data17.id !== undefined){
let data18 = data17.id;
if(typeof data18 === "string"){
if(!pattern0.test(data18)){
const err60 = {instancePath:instancePath+"/root/definition/id",schemaPath:"#/properties/root/properties/definition/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
else {
const err61 = {instancePath:instancePath+"/root/definition/id",schemaPath:"#/properties/root/properties/definition/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
}
if(data17.revision !== undefined){
let data19 = data17.revision;
if(typeof data19 === "string"){
if(!pattern2.test(data19)){
const err62 = {instancePath:instancePath+"/root/definition/revision",schemaPath:"#/properties/root/properties/definition/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
else {
const err63 = {instancePath:instancePath+"/root/definition/revision",schemaPath:"#/properties/root/properties/definition/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err64 = {instancePath:instancePath+"/root/definition",schemaPath:"#/properties/root/properties/definition/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data13.config_revision !== undefined){
let data20 = data13.config_revision;
if(typeof data20 === "string"){
if(!pattern11.test(data20)){
const err65 = {instancePath:instancePath+"/root/config_revision",schemaPath:"#/properties/root/properties/config_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
if(!(formats0.validate(data20))){
const err66 = {instancePath:instancePath+"/root/config_revision",schemaPath:"#/properties/root/properties/config_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err67 = {instancePath:instancePath+"/root/config_revision",schemaPath:"#/properties/root/properties/config_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
}
if(data13.configuration !== undefined){
let data21 = data13.configuration;
if(data21 && typeof data21 == "object" && !Array.isArray(data21)){
if(data21.model === undefined){
const err68 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
if(data21.instructions === undefined){
const err69 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/required",keyword:"required",params:{missingProperty: "instructions"},message:"must have required property '"+"instructions"+"'"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
if(data21.tools === undefined){
const err70 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/required",keyword:"required",params:{missingProperty: "tools"},message:"must have required property '"+"tools"+"'"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
if(data21.children === undefined){
const err71 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/required",keyword:"required",params:{missingProperty: "children"},message:"must have required property '"+"children"+"'"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
if(data21.hooks === undefined){
const err72 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/required",keyword:"required",params:{missingProperty: "hooks"},message:"must have required property '"+"hooks"+"'"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
if(data21.output_schema === undefined){
const err73 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
for(const key6 in data21){
if(!((((((key6 === "model") || (key6 === "instructions")) || (key6 === "tools")) || (key6 === "children")) || (key6 === "hooks")) || (key6 === "output_schema"))){
const err74 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key6},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
}
if(data21.model !== undefined){
let data22 = data21.model;
if(data22 && typeof data22 == "object" && !Array.isArray(data22)){
if(data22.provider === undefined){
const err75 = {instancePath:instancePath+"/root/configuration/model",schemaPath:"#/properties/root/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
if(data22.name === undefined){
const err76 = {instancePath:instancePath+"/root/configuration/model",schemaPath:"#/properties/root/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
if(data22.effort === undefined){
const err77 = {instancePath:instancePath+"/root/configuration/model",schemaPath:"#/properties/root/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
for(const key7 in data22){
if(!(((key7 === "provider") || (key7 === "name")) || (key7 === "effort"))){
const err78 = {instancePath:instancePath+"/root/configuration/model",schemaPath:"#/properties/root/properties/configuration/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key7},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
}
if(data22.provider !== undefined){
let data23 = data22.provider;
if(typeof data23 === "string"){
if(!pattern0.test(data23)){
const err79 = {instancePath:instancePath+"/root/configuration/model/provider",schemaPath:"#/properties/root/properties/configuration/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
}
else {
const err80 = {instancePath:instancePath+"/root/configuration/model/provider",schemaPath:"#/properties/root/properties/configuration/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
}
if(data22.name !== undefined){
if(typeof data22.name !== "string"){
const err81 = {instancePath:instancePath+"/root/configuration/model/name",schemaPath:"#/properties/root/properties/configuration/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
}
if(data22.effort !== undefined){
if(typeof data22.effort !== "string"){
const err82 = {instancePath:instancePath+"/root/configuration/model/effort",schemaPath:"#/properties/root/properties/configuration/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err83 = {instancePath:instancePath+"/root/configuration/model",schemaPath:"#/properties/root/properties/configuration/properties/model/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
}
if(data21.instructions !== undefined){
let data26 = data21.instructions;
if(data26 && typeof data26 == "object" && !Array.isArray(data26)){
if(data26.text === undefined){
const err84 = {instancePath:instancePath+"/root/configuration/instructions",schemaPath:"#/properties/root/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
if(data26.project_files === undefined){
const err85 = {instancePath:instancePath+"/root/configuration/instructions",schemaPath:"#/properties/root/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err85];
}
else {
vErrors.push(err85);
}
errors++;
}
if(data26.discover_skills === undefined){
const err86 = {instancePath:instancePath+"/root/configuration/instructions",schemaPath:"#/properties/root/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
for(const key8 in data26){
if(!(((key8 === "text") || (key8 === "project_files")) || (key8 === "discover_skills"))){
const err87 = {instancePath:instancePath+"/root/configuration/instructions",schemaPath:"#/properties/root/properties/configuration/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key8},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
}
if(data26.text !== undefined){
if(typeof data26.text !== "string"){
const err88 = {instancePath:instancePath+"/root/configuration/instructions/text",schemaPath:"#/properties/root/properties/configuration/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
}
if(data26.project_files !== undefined){
let data28 = data26.project_files;
if((data28 !== null) && (!(Array.isArray(data28)))){
const err89 = {instancePath:instancePath+"/root/configuration/instructions/project_files",schemaPath:"#/properties/root/properties/configuration/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema13.properties.root.properties.configuration.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
if(Array.isArray(data28)){
const len0 = data28.length;
for(let i0=0; i0<len0; i0++){
if(typeof data28[i0] !== "string"){
const err90 = {instancePath:instancePath+"/root/configuration/instructions/project_files/" + i0,schemaPath:"#/properties/root/properties/configuration/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
}
}
}
if(data26.discover_skills !== undefined){
if(typeof data26.discover_skills !== "boolean"){
const err91 = {instancePath:instancePath+"/root/configuration/instructions/discover_skills",schemaPath:"#/properties/root/properties/configuration/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err91];
}
else {
vErrors.push(err91);
}
errors++;
}
}
}
else {
const err92 = {instancePath:instancePath+"/root/configuration/instructions",schemaPath:"#/properties/root/properties/configuration/properties/instructions/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err92];
}
else {
vErrors.push(err92);
}
errors++;
}
}
if(data21.tools !== undefined){
let data31 = data21.tools;
if((!(data31 && typeof data31 == "object" && !Array.isArray(data31))) && (data31 !== null)){
const err93 = {instancePath:instancePath+"/root/configuration/tools",schemaPath:"#/properties/root/properties/configuration/properties/tools/type",keyword:"type",params:{type: schema13.properties.root.properties.configuration.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err93];
}
else {
vErrors.push(err93);
}
errors++;
}
if(data31 && typeof data31 == "object" && !Array.isArray(data31)){
for(const key9 in data31){
let data32 = data31[key9];
if(data32 && typeof data32 == "object" && !Array.isArray(data32)){
if(data32.description === undefined){
const err94 = {instancePath:instancePath+"/root/configuration/tools/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err94];
}
else {
vErrors.push(err94);
}
errors++;
}
if(data32.input_schema === undefined){
const err95 = {instancePath:instancePath+"/root/configuration/tools/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err95];
}
else {
vErrors.push(err95);
}
errors++;
}
if(data32.output_schema === undefined){
const err96 = {instancePath:instancePath+"/root/configuration/tools/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err96];
}
else {
vErrors.push(err96);
}
errors++;
}
for(const key10 in data32){
if(!(((key10 === "description") || (key10 === "input_schema")) || (key10 === "output_schema"))){
const err97 = {instancePath:instancePath+"/root/configuration/tools/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key10},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err97];
}
else {
vErrors.push(err97);
}
errors++;
}
}
if(data32.description !== undefined){
if(typeof data32.description !== "string"){
const err98 = {instancePath:instancePath+"/root/configuration/tools/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/root/properties/configuration/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err99 = {instancePath:instancePath+"/root/configuration/tools/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err99];
}
else {
vErrors.push(err99);
}
errors++;
}
}
}
}
if(data21.children !== undefined){
let data34 = data21.children;
if((!(data34 && typeof data34 == "object" && !Array.isArray(data34))) && (data34 !== null)){
const err100 = {instancePath:instancePath+"/root/configuration/children",schemaPath:"#/properties/root/properties/configuration/properties/children/type",keyword:"type",params:{type: schema13.properties.root.properties.configuration.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err100];
}
else {
vErrors.push(err100);
}
errors++;
}
if(data34 && typeof data34 == "object" && !Array.isArray(data34)){
for(const key11 in data34){
let data35 = data34[key11];
if(data35 && typeof data35 == "object" && !Array.isArray(data35)){
if(data35.id === undefined){
const err101 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err101];
}
else {
vErrors.push(err101);
}
errors++;
}
if(data35.revision === undefined){
const err102 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err102];
}
else {
vErrors.push(err102);
}
errors++;
}
for(const key12 in data35){
if(!((key12 === "id") || (key12 === "revision"))){
const err103 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key12},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err103];
}
else {
vErrors.push(err103);
}
errors++;
}
}
if(data35.id !== undefined){
let data36 = data35.id;
if(typeof data36 === "string"){
if(!pattern0.test(data36)){
const err104 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err105 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err105];
}
else {
vErrors.push(err105);
}
errors++;
}
}
if(data35.revision !== undefined){
let data37 = data35.revision;
if(typeof data37 === "string"){
if(!pattern2.test(data37)){
const err106 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err106];
}
else {
vErrors.push(err106);
}
errors++;
}
}
else {
const err107 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err108 = {instancePath:instancePath+"/root/configuration/children/" + key11.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
}
if(data21.hooks !== undefined){
let data38 = data21.hooks;
if((!(data38 && typeof data38 == "object" && !Array.isArray(data38))) && (data38 !== null)){
const err109 = {instancePath:instancePath+"/root/configuration/hooks",schemaPath:"#/properties/root/properties/configuration/properties/hooks/type",keyword:"type",params:{type: schema13.properties.root.properties.configuration.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err109];
}
else {
vErrors.push(err109);
}
errors++;
}
if(data38 && typeof data38 == "object" && !Array.isArray(data38)){
for(const key13 in data38){
let data39 = data38[key13];
if(data39 && typeof data39 == "object" && !Array.isArray(data39)){
if(data39.operations === undefined){
const err110 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err110];
}
else {
vErrors.push(err110);
}
errors++;
}
if(data39.optional === undefined){
const err111 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err111];
}
else {
vErrors.push(err111);
}
errors++;
}
if(data39.timeout_millis === undefined){
const err112 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err112];
}
else {
vErrors.push(err112);
}
errors++;
}
for(const key14 in data39){
if(!(((key14 === "operations") || (key14 === "optional")) || (key14 === "timeout_millis"))){
const err113 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key14},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err113];
}
else {
vErrors.push(err113);
}
errors++;
}
}
if(data39.operations !== undefined){
let data40 = data39.operations;
if((data40 !== null) && (!(Array.isArray(data40)))){
const err114 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema13.properties.root.properties.configuration.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err114];
}
else {
vErrors.push(err114);
}
errors++;
}
if(Array.isArray(data40)){
const len1 = data40.length;
for(let i1=0; i1<len1; i1++){
let data41 = data40[i1];
if(typeof data41 === "string"){
if(!pattern0.test(data41)){
const err115 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err115];
}
else {
vErrors.push(err115);
}
errors++;
}
}
else {
const err116 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data39.optional !== undefined){
if(typeof data39.optional !== "boolean"){
const err117 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err117];
}
else {
vErrors.push(err117);
}
errors++;
}
}
if(data39.timeout_millis !== undefined){
let data43 = data39.timeout_millis;
if(!((typeof data43 == "number") && (!(data43 % 1) && !isNaN(data43)))){
const err118 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err118];
}
else {
vErrors.push(err118);
}
errors++;
}
if(typeof data43 == "number"){
if(data43 > 60000 || isNaN(data43)){
const err119 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err119];
}
else {
vErrors.push(err119);
}
errors++;
}
if(data43 < 1 || isNaN(data43)){
const err120 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
if(vErrors === null){
vErrors = [err120];
}
else {
vErrors.push(err120);
}
errors++;
}
}
}
}
else {
const err121 = {instancePath:instancePath+"/root/configuration/hooks/" + key13.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/root/properties/configuration/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err121];
}
else {
vErrors.push(err121);
}
errors++;
}
}
}
}
}
else {
const err122 = {instancePath:instancePath+"/root/configuration",schemaPath:"#/properties/root/properties/configuration/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err122];
}
else {
vErrors.push(err122);
}
errors++;
}
}
if(data13.working_directory !== undefined){
if(typeof data13.working_directory !== "string"){
const err123 = {instancePath:instancePath+"/root/working_directory",schemaPath:"#/properties/root/properties/working_directory/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err123];
}
else {
vErrors.push(err123);
}
errors++;
}
}
if(data13.lifecycle !== undefined){
let data45 = data13.lifecycle;
if(typeof data45 !== "string"){
const err124 = {instancePath:instancePath+"/root/lifecycle",schemaPath:"#/properties/root/properties/lifecycle/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err124];
}
else {
vErrors.push(err124);
}
errors++;
}
if(!((data45 === "active") || (data45 === "stopped"))){
const err125 = {instancePath:instancePath+"/root/lifecycle",schemaPath:"#/properties/root/properties/lifecycle/enum",keyword:"enum",params:{allowedValues: schema13.properties.root.properties.lifecycle.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err125];
}
else {
vErrors.push(err125);
}
errors++;
}
}
if(data13.created_at !== undefined){
if(typeof data13.created_at !== "string"){
const err126 = {instancePath:instancePath+"/root/created_at",schemaPath:"#/properties/root/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err126];
}
else {
vErrors.push(err126);
}
errors++;
}
}
}
else {
const err127 = {instancePath:instancePath+"/root",schemaPath:"#/properties/root/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err127];
}
else {
vErrors.push(err127);
}
errors++;
}
}
}
else {
const err128 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err128];
}
else {
vErrors.push(err128);
}
errors++;
}
validate12.errors = vErrors;
return errors === 0;
}

export const Definition = validate13;
const schema14 = {"type":"object","properties":{"ref":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false},"document":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"defaults":{"type":"object","properties":{"model":{"type":["null","object"],"properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":["null","object"],"properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output":{"type":["null","object"],"properties":{"schema":true},"required":["schema"],"additionalProperties":false}},"additionalProperties":false}},"required":["id","name","defaults"],"additionalProperties":false},"created_at":{"type":"string"}},"$id":"https://whip.dev/protocol/v4/Definition","$schema":"http://json-schema.org/draft-07/schema#","title":"Definition","required":["ref","document","created_at"],"additionalProperties":false};

function validate13(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Definition" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.ref === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "ref"},message:"must have required property '"+"ref"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.document === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "document"},message:"must have required property '"+"document"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.created_at === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
for(const key0 in data){
if(!(((key0 === "ref") || (key0 === "document")) || (key0 === "created_at"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.ref !== undefined){
let data0 = data.ref;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.id === undefined){
const err4 = {instancePath:instancePath+"/ref",schemaPath:"#/properties/ref/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data0.revision === undefined){
const err5 = {instancePath:instancePath+"/ref",schemaPath:"#/properties/ref/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
for(const key1 in data0){
if(!((key1 === "id") || (key1 === "revision"))){
const err6 = {instancePath:instancePath+"/ref",schemaPath:"#/properties/ref/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data0.id !== undefined){
let data1 = data0.id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err7 = {instancePath:instancePath+"/ref/id",schemaPath:"#/properties/ref/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err8 = {instancePath:instancePath+"/ref/id",schemaPath:"#/properties/ref/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data0.revision !== undefined){
let data2 = data0.revision;
if(typeof data2 === "string"){
if(!pattern2.test(data2)){
const err9 = {instancePath:instancePath+"/ref/revision",schemaPath:"#/properties/ref/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
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
const err10 = {instancePath:instancePath+"/ref/revision",schemaPath:"#/properties/ref/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err11 = {instancePath:instancePath+"/ref",schemaPath:"#/properties/ref/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.document !== undefined){
let data3 = data.document;
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.id === undefined){
const err12 = {instancePath:instancePath+"/document",schemaPath:"#/properties/document/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data3.name === undefined){
const err13 = {instancePath:instancePath+"/document",schemaPath:"#/properties/document/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data3.defaults === undefined){
const err14 = {instancePath:instancePath+"/document",schemaPath:"#/properties/document/required",keyword:"required",params:{missingProperty: "defaults"},message:"must have required property '"+"defaults"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
for(const key2 in data3){
if(!(((key2 === "id") || (key2 === "name")) || (key2 === "defaults"))){
const err15 = {instancePath:instancePath+"/document",schemaPath:"#/properties/document/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data3.id !== undefined){
let data4 = data3.id;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err16 = {instancePath:instancePath+"/document/id",schemaPath:"#/properties/document/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err17 = {instancePath:instancePath+"/document/id",schemaPath:"#/properties/document/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data3.name !== undefined){
if(typeof data3.name !== "string"){
const err18 = {instancePath:instancePath+"/document/name",schemaPath:"#/properties/document/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data3.defaults !== undefined){
let data6 = data3.defaults;
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
for(const key3 in data6){
if(!((((((key3 === "model") || (key3 === "instructions")) || (key3 === "tools")) || (key3 === "children")) || (key3 === "hooks")) || (key3 === "output"))){
const err19 = {instancePath:instancePath+"/document/defaults",schemaPath:"#/properties/document/properties/defaults/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data6.model !== undefined){
let data7 = data6.model;
if((data7 !== null) && (!(data7 && typeof data7 == "object" && !Array.isArray(data7)))){
const err20 = {instancePath:instancePath+"/document/defaults/model",schemaPath:"#/properties/document/properties/defaults/properties/model/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.model.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.provider === undefined){
const err21 = {instancePath:instancePath+"/document/defaults/model",schemaPath:"#/properties/document/properties/defaults/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(data7.name === undefined){
const err22 = {instancePath:instancePath+"/document/defaults/model",schemaPath:"#/properties/document/properties/defaults/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
if(data7.effort === undefined){
const err23 = {instancePath:instancePath+"/document/defaults/model",schemaPath:"#/properties/document/properties/defaults/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
for(const key4 in data7){
if(!(((key4 === "provider") || (key4 === "name")) || (key4 === "effort"))){
const err24 = {instancePath:instancePath+"/document/defaults/model",schemaPath:"#/properties/document/properties/defaults/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data7.provider !== undefined){
let data8 = data7.provider;
if(typeof data8 === "string"){
if(!pattern0.test(data8)){
const err25 = {instancePath:instancePath+"/document/defaults/model/provider",schemaPath:"#/properties/document/properties/defaults/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err26 = {instancePath:instancePath+"/document/defaults/model/provider",schemaPath:"#/properties/document/properties/defaults/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data7.name !== undefined){
if(typeof data7.name !== "string"){
const err27 = {instancePath:instancePath+"/document/defaults/model/name",schemaPath:"#/properties/document/properties/defaults/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data7.effort !== undefined){
if(typeof data7.effort !== "string"){
const err28 = {instancePath:instancePath+"/document/defaults/model/effort",schemaPath:"#/properties/document/properties/defaults/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data6.instructions !== undefined){
let data11 = data6.instructions;
if((data11 !== null) && (!(data11 && typeof data11 == "object" && !Array.isArray(data11)))){
const err29 = {instancePath:instancePath+"/document/defaults/instructions",schemaPath:"#/properties/document/properties/defaults/properties/instructions/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.instructions.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data11 && typeof data11 == "object" && !Array.isArray(data11)){
if(data11.text === undefined){
const err30 = {instancePath:instancePath+"/document/defaults/instructions",schemaPath:"#/properties/document/properties/defaults/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data11.project_files === undefined){
const err31 = {instancePath:instancePath+"/document/defaults/instructions",schemaPath:"#/properties/document/properties/defaults/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data11.discover_skills === undefined){
const err32 = {instancePath:instancePath+"/document/defaults/instructions",schemaPath:"#/properties/document/properties/defaults/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
for(const key5 in data11){
if(!(((key5 === "text") || (key5 === "project_files")) || (key5 === "discover_skills"))){
const err33 = {instancePath:instancePath+"/document/defaults/instructions",schemaPath:"#/properties/document/properties/defaults/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data11.text !== undefined){
if(typeof data11.text !== "string"){
const err34 = {instancePath:instancePath+"/document/defaults/instructions/text",schemaPath:"#/properties/document/properties/defaults/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
if(data11.project_files !== undefined){
let data13 = data11.project_files;
if((data13 !== null) && (!(Array.isArray(data13)))){
const err35 = {instancePath:instancePath+"/document/defaults/instructions/project_files",schemaPath:"#/properties/document/properties/defaults/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(Array.isArray(data13)){
const len0 = data13.length;
for(let i0=0; i0<len0; i0++){
if(typeof data13[i0] !== "string"){
const err36 = {instancePath:instancePath+"/document/defaults/instructions/project_files/" + i0,schemaPath:"#/properties/document/properties/defaults/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data11.discover_skills !== undefined){
if(typeof data11.discover_skills !== "boolean"){
const err37 = {instancePath:instancePath+"/document/defaults/instructions/discover_skills",schemaPath:"#/properties/document/properties/defaults/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
}
if(data6.tools !== undefined){
let data16 = data6.tools;
if((!(data16 && typeof data16 == "object" && !Array.isArray(data16))) && (data16 !== null)){
const err38 = {instancePath:instancePath+"/document/defaults/tools",schemaPath:"#/properties/document/properties/defaults/properties/tools/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(data16 && typeof data16 == "object" && !Array.isArray(data16)){
for(const key6 in data16){
let data17 = data16[key6];
if(data17 && typeof data17 == "object" && !Array.isArray(data17)){
if(data17.description === undefined){
const err39 = {instancePath:instancePath+"/document/defaults/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
if(data17.input_schema === undefined){
const err40 = {instancePath:instancePath+"/document/defaults/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data17.output_schema === undefined){
const err41 = {instancePath:instancePath+"/document/defaults/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
for(const key7 in data17){
if(!(((key7 === "description") || (key7 === "input_schema")) || (key7 === "output_schema"))){
const err42 = {instancePath:instancePath+"/document/defaults/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key7},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
if(data17.description !== undefined){
if(typeof data17.description !== "string"){
const err43 = {instancePath:instancePath+"/document/defaults/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/document/properties/defaults/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err44 = {instancePath:instancePath+"/document/defaults/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
}
}
if(data6.children !== undefined){
let data19 = data6.children;
if((!(data19 && typeof data19 == "object" && !Array.isArray(data19))) && (data19 !== null)){
const err45 = {instancePath:instancePath+"/document/defaults/children",schemaPath:"#/properties/document/properties/defaults/properties/children/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(data19 && typeof data19 == "object" && !Array.isArray(data19)){
for(const key8 in data19){
let data20 = data19[key8];
if(data20 && typeof data20 == "object" && !Array.isArray(data20)){
if(data20.id === undefined){
const err46 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
if(data20.revision === undefined){
const err47 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
for(const key9 in data20){
if(!((key9 === "id") || (key9 === "revision"))){
const err48 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key9},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data20.id !== undefined){
let data21 = data20.id;
if(typeof data21 === "string"){
if(!pattern0.test(data21)){
const err49 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err50 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data20.revision !== undefined){
let data22 = data20.revision;
if(typeof data22 === "string"){
if(!pattern2.test(data22)){
const err51 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
else {
const err52 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
}
else {
const err53 = {instancePath:instancePath+"/document/defaults/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
}
}
if(data6.hooks !== undefined){
let data23 = data6.hooks;
if((!(data23 && typeof data23 == "object" && !Array.isArray(data23))) && (data23 !== null)){
const err54 = {instancePath:instancePath+"/document/defaults/hooks",schemaPath:"#/properties/document/properties/defaults/properties/hooks/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
if(data23 && typeof data23 == "object" && !Array.isArray(data23)){
for(const key10 in data23){
let data24 = data23[key10];
if(data24 && typeof data24 == "object" && !Array.isArray(data24)){
if(data24.operations === undefined){
const err55 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(data24.optional === undefined){
const err56 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
if(data24.timeout_millis === undefined){
const err57 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
for(const key11 in data24){
if(!(((key11 === "operations") || (key11 === "optional")) || (key11 === "timeout_millis"))){
const err58 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key11},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
if(data24.operations !== undefined){
let data25 = data24.operations;
if((data25 !== null) && (!(Array.isArray(data25)))){
const err59 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
if(Array.isArray(data25)){
const len1 = data25.length;
for(let i1=0; i1<len1; i1++){
let data26 = data25[i1];
if(typeof data26 === "string"){
if(!pattern0.test(data26)){
const err60 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
else {
const err61 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data24.optional !== undefined){
if(typeof data24.optional !== "boolean"){
const err62 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
if(data24.timeout_millis !== undefined){
let data28 = data24.timeout_millis;
if(!((typeof data28 == "number") && (!(data28 % 1) && !isNaN(data28)))){
const err63 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
if(typeof data28 == "number"){
if(data28 > 60000 || isNaN(data28)){
const err64 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
if(data28 < 1 || isNaN(data28)){
const err65 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
else {
const err66 = {instancePath:instancePath+"/document/defaults/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/document/properties/defaults/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
}
}
}
if(data6.output !== undefined){
let data29 = data6.output;
if((data29 !== null) && (!(data29 && typeof data29 == "object" && !Array.isArray(data29)))){
const err67 = {instancePath:instancePath+"/document/defaults/output",schemaPath:"#/properties/document/properties/defaults/properties/output/type",keyword:"type",params:{type: schema14.properties.document.properties.defaults.properties.output.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
if(data29 && typeof data29 == "object" && !Array.isArray(data29)){
if(data29.schema === undefined){
const err68 = {instancePath:instancePath+"/document/defaults/output",schemaPath:"#/properties/document/properties/defaults/properties/output/required",keyword:"required",params:{missingProperty: "schema"},message:"must have required property '"+"schema"+"'"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
for(const key12 in data29){
if(!(key12 === "schema")){
const err69 = {instancePath:instancePath+"/document/defaults/output",schemaPath:"#/properties/document/properties/defaults/properties/output/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key12},message:"must NOT have additional properties"};
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
}
}
else {
const err70 = {instancePath:instancePath+"/document/defaults",schemaPath:"#/properties/document/properties/defaults/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
const err71 = {instancePath:instancePath+"/document",schemaPath:"#/properties/document/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
}
if(data.created_at !== undefined){
if(typeof data.created_at !== "string"){
const err72 = {instancePath:instancePath+"/created_at",schemaPath:"#/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err73 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
validate13.errors = vErrors;
return errors === 0;
}

export const DefinitionDocument = validate14;
const schema15 = {"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"defaults":{"type":"object","properties":{"model":{"type":["null","object"],"properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":["null","object"],"properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output":{"type":["null","object"],"properties":{"schema":true},"required":["schema"],"additionalProperties":false}},"additionalProperties":false}},"$id":"https://whip.dev/protocol/v4/DefinitionDocument","$schema":"http://json-schema.org/draft-07/schema#","title":"DefinitionDocument","required":["id","name","defaults"],"additionalProperties":false};

function validate14(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/DefinitionDocument" */;
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
if(data.defaults === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "defaults"},message:"must have required property '"+"defaults"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
for(const key0 in data){
if(!(((key0 === "id") || (key0 === "name")) || (key0 === "defaults"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.id !== undefined){
let data0 = data.id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err4 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err5 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.name !== undefined){
if(typeof data.name !== "string"){
const err6 = {instancePath:instancePath+"/name",schemaPath:"#/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.defaults !== undefined){
let data2 = data.defaults;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
for(const key1 in data2){
if(!((((((key1 === "model") || (key1 === "instructions")) || (key1 === "tools")) || (key1 === "children")) || (key1 === "hooks")) || (key1 === "output"))){
const err7 = {instancePath:instancePath+"/defaults",schemaPath:"#/properties/defaults/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data2.model !== undefined){
let data3 = data2.model;
if((data3 !== null) && (!(data3 && typeof data3 == "object" && !Array.isArray(data3)))){
const err8 = {instancePath:instancePath+"/defaults/model",schemaPath:"#/properties/defaults/properties/model/type",keyword:"type",params:{type: schema15.properties.defaults.properties.model.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.provider === undefined){
const err9 = {instancePath:instancePath+"/defaults/model",schemaPath:"#/properties/defaults/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data3.name === undefined){
const err10 = {instancePath:instancePath+"/defaults/model",schemaPath:"#/properties/defaults/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data3.effort === undefined){
const err11 = {instancePath:instancePath+"/defaults/model",schemaPath:"#/properties/defaults/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
for(const key2 in data3){
if(!(((key2 === "provider") || (key2 === "name")) || (key2 === "effort"))){
const err12 = {instancePath:instancePath+"/defaults/model",schemaPath:"#/properties/defaults/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data3.provider !== undefined){
let data4 = data3.provider;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err13 = {instancePath:instancePath+"/defaults/model/provider",schemaPath:"#/properties/defaults/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err14 = {instancePath:instancePath+"/defaults/model/provider",schemaPath:"#/properties/defaults/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data3.name !== undefined){
if(typeof data3.name !== "string"){
const err15 = {instancePath:instancePath+"/defaults/model/name",schemaPath:"#/properties/defaults/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data3.effort !== undefined){
if(typeof data3.effort !== "string"){
const err16 = {instancePath:instancePath+"/defaults/model/effort",schemaPath:"#/properties/defaults/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data2.instructions !== undefined){
let data7 = data2.instructions;
if((data7 !== null) && (!(data7 && typeof data7 == "object" && !Array.isArray(data7)))){
const err17 = {instancePath:instancePath+"/defaults/instructions",schemaPath:"#/properties/defaults/properties/instructions/type",keyword:"type",params:{type: schema15.properties.defaults.properties.instructions.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.text === undefined){
const err18 = {instancePath:instancePath+"/defaults/instructions",schemaPath:"#/properties/defaults/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(data7.project_files === undefined){
const err19 = {instancePath:instancePath+"/defaults/instructions",schemaPath:"#/properties/defaults/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data7.discover_skills === undefined){
const err20 = {instancePath:instancePath+"/defaults/instructions",schemaPath:"#/properties/defaults/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
for(const key3 in data7){
if(!(((key3 === "text") || (key3 === "project_files")) || (key3 === "discover_skills"))){
const err21 = {instancePath:instancePath+"/defaults/instructions",schemaPath:"#/properties/defaults/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data7.text !== undefined){
if(typeof data7.text !== "string"){
const err22 = {instancePath:instancePath+"/defaults/instructions/text",schemaPath:"#/properties/defaults/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data7.project_files !== undefined){
let data9 = data7.project_files;
if((data9 !== null) && (!(Array.isArray(data9)))){
const err23 = {instancePath:instancePath+"/defaults/instructions/project_files",schemaPath:"#/properties/defaults/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema15.properties.defaults.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(Array.isArray(data9)){
const len0 = data9.length;
for(let i0=0; i0<len0; i0++){
if(typeof data9[i0] !== "string"){
const err24 = {instancePath:instancePath+"/defaults/instructions/project_files/" + i0,schemaPath:"#/properties/defaults/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
}
if(data7.discover_skills !== undefined){
if(typeof data7.discover_skills !== "boolean"){
const err25 = {instancePath:instancePath+"/defaults/instructions/discover_skills",schemaPath:"#/properties/defaults/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
if(data2.tools !== undefined){
let data12 = data2.tools;
if((!(data12 && typeof data12 == "object" && !Array.isArray(data12))) && (data12 !== null)){
const err26 = {instancePath:instancePath+"/defaults/tools",schemaPath:"#/properties/defaults/properties/tools/type",keyword:"type",params:{type: schema15.properties.defaults.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
for(const key4 in data12){
let data13 = data12[key4];
if(data13 && typeof data13 == "object" && !Array.isArray(data13)){
if(data13.description === undefined){
const err27 = {instancePath:instancePath+"/defaults/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data13.input_schema === undefined){
const err28 = {instancePath:instancePath+"/defaults/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data13.output_schema === undefined){
const err29 = {instancePath:instancePath+"/defaults/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
for(const key5 in data13){
if(!(((key5 === "description") || (key5 === "input_schema")) || (key5 === "output_schema"))){
const err30 = {instancePath:instancePath+"/defaults/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
}
if(data13.description !== undefined){
if(typeof data13.description !== "string"){
const err31 = {instancePath:instancePath+"/defaults/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/defaults/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err32 = {instancePath:instancePath+"/defaults/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
if(data2.children !== undefined){
let data15 = data2.children;
if((!(data15 && typeof data15 == "object" && !Array.isArray(data15))) && (data15 !== null)){
const err33 = {instancePath:instancePath+"/defaults/children",schemaPath:"#/properties/defaults/properties/children/type",keyword:"type",params:{type: schema15.properties.defaults.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data15 && typeof data15 == "object" && !Array.isArray(data15)){
for(const key6 in data15){
let data16 = data15[key6];
if(data16 && typeof data16 == "object" && !Array.isArray(data16)){
if(data16.id === undefined){
const err34 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(data16.revision === undefined){
const err35 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
for(const key7 in data16){
if(!((key7 === "id") || (key7 === "revision"))){
const err36 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key7},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
if(data16.id !== undefined){
let data17 = data16.id;
if(typeof data17 === "string"){
if(!pattern0.test(data17)){
const err37 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/defaults/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err38 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/defaults/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
if(data16.revision !== undefined){
let data18 = data16.revision;
if(typeof data18 === "string"){
if(!pattern2.test(data18)){
const err39 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/defaults/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
else {
const err40 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/defaults/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
}
else {
const err41 = {instancePath:instancePath+"/defaults/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
}
}
if(data2.hooks !== undefined){
let data19 = data2.hooks;
if((!(data19 && typeof data19 == "object" && !Array.isArray(data19))) && (data19 !== null)){
const err42 = {instancePath:instancePath+"/defaults/hooks",schemaPath:"#/properties/defaults/properties/hooks/type",keyword:"type",params:{type: schema15.properties.defaults.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
if(data19 && typeof data19 == "object" && !Array.isArray(data19)){
for(const key8 in data19){
let data20 = data19[key8];
if(data20 && typeof data20 == "object" && !Array.isArray(data20)){
if(data20.operations === undefined){
const err43 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
if(data20.optional === undefined){
const err44 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
if(data20.timeout_millis === undefined){
const err45 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
for(const key9 in data20){
if(!(((key9 === "operations") || (key9 === "optional")) || (key9 === "timeout_millis"))){
const err46 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key9},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data20.operations !== undefined){
let data21 = data20.operations;
if((data21 !== null) && (!(Array.isArray(data21)))){
const err47 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema15.properties.defaults.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
if(Array.isArray(data21)){
const len1 = data21.length;
for(let i1=0; i1<len1; i1++){
let data22 = data21[i1];
if(typeof data22 === "string"){
if(!pattern0.test(data22)){
const err48 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
else {
const err49 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data20.optional !== undefined){
if(typeof data20.optional !== "boolean"){
const err50 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data20.timeout_millis !== undefined){
let data24 = data20.timeout_millis;
if(!((typeof data24 == "number") && (!(data24 % 1) && !isNaN(data24)))){
const err51 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
if(typeof data24 == "number"){
if(data24 > 60000 || isNaN(data24)){
const err52 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
if(data24 < 1 || isNaN(data24)){
const err53 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
}
}
else {
const err54 = {instancePath:instancePath+"/defaults/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/defaults/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
}
}
if(data2.output !== undefined){
let data25 = data2.output;
if((data25 !== null) && (!(data25 && typeof data25 == "object" && !Array.isArray(data25)))){
const err55 = {instancePath:instancePath+"/defaults/output",schemaPath:"#/properties/defaults/properties/output/type",keyword:"type",params:{type: schema15.properties.defaults.properties.output.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(data25 && typeof data25 == "object" && !Array.isArray(data25)){
if(data25.schema === undefined){
const err56 = {instancePath:instancePath+"/defaults/output",schemaPath:"#/properties/defaults/properties/output/required",keyword:"required",params:{missingProperty: "schema"},message:"must have required property '"+"schema"+"'"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
for(const key10 in data25){
if(!(key10 === "schema")){
const err57 = {instancePath:instancePath+"/defaults/output",schemaPath:"#/properties/defaults/properties/output/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key10},message:"must NOT have additional properties"};
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
}
else {
const err58 = {instancePath:instancePath+"/defaults",schemaPath:"#/properties/defaults/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
}
else {
const err59 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
validate14.errors = vErrors;
return errors === 0;
}

export const DefinitionRef = validate15;
const schema16 = {"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"$id":"https://whip.dev/protocol/v4/DefinitionRef","$schema":"http://json-schema.org/draft-07/schema#","title":"DefinitionRef","required":["id","revision"],"additionalProperties":false};

function validate15(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/DefinitionRef" */;
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
if(data.revision === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
for(const key0 in data){
if(!((key0 === "id") || (key0 === "revision"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.id !== undefined){
let data0 = data.id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err4 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.revision !== undefined){
let data1 = data.revision;
if(typeof data1 === "string"){
if(!pattern2.test(data1)){
const err5 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
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
const err6 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate15.errors = vErrors;
return errors === 0;
}

export const DeleteResult = validate16;
const schema17 = {"type":"object","properties":{"deleted":{"type":"boolean"}},"$id":"https://whip.dev/protocol/v4/DeleteResult","$schema":"http://json-schema.org/draft-07/schema#","title":"DeleteResult","required":["deleted"],"additionalProperties":false};

function validate16(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/DeleteResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.deleted === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "deleted"},message:"must have required property '"+"deleted"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "deleted")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.deleted !== undefined){
if(typeof data.deleted !== "boolean"){
const err2 = {instancePath:instancePath+"/deleted",schemaPath:"#/properties/deleted/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
validate16.errors = vErrors;
return errors === 0;
}

export const HistoryParams = validate17;
const schema18 = {"type":"object","properties":{"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"after":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"limit":{"type":"integer","minimum":1,"maximum":100}},"$id":"https://whip.dev/protocol/v4/HistoryParams","$schema":"http://json-schema.org/draft-07/schema#","title":"HistoryParams","required":["session_id","after","limit"],"additionalProperties":false};

function validate17(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/HistoryParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.session_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.after === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "after"},message:"must have required property '"+"after"+"'"};
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
for(const key0 in data){
if(!(((key0 === "session_id") || (key0 === "after")) || (key0 === "limit"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.session_id !== undefined){
let data0 = data.session_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err4 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err5 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.after !== undefined){
let data1 = data.after;
if(typeof data1 === "string"){
if(!pattern11.test(data1)){
const err6 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!(formats0.validate(data1))){
const err7 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err8 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.limit !== undefined){
let data2 = data.limit;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err9 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(typeof data2 == "number"){
if(data2 > 100 || isNaN(data2)){
const err10 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/maximum",keyword:"maximum",params:{comparison: "<=", limit: 100},message:"must be <= 100"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data2 < 1 || isNaN(data2)){
const err11 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
validate17.errors = vErrors;
return errors === 0;
}

export const HistoryResult = validate18;
const schema19 = {"type":"object","properties":{"items":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"turn_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"input_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"sequence":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"role":{"type":"string","enum":["system","user","assistant","tool"]},"parts":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"text":{"type":"string","pattern":"^[\\s\\S]+$"},"type":{"type":"string","enum":["text"]}},"required":["type","text"],"additionalProperties":false},{"type":"object","properties":{"reference_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"type":{"type":"string","enum":["content"]}},"required":["type","reference_id"],"additionalProperties":false}]},"minItems":1,"maxItems":128},"created_at":{"type":"string"}},"required":["id","session_id","turn_id","input_id","sequence","role","parts","created_at"],"additionalProperties":false}}},"$id":"https://whip.dev/protocol/v4/HistoryResult","$schema":"http://json-schema.org/draft-07/schema#","title":"HistoryResult","required":["items"],"additionalProperties":false};

function validate18(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/HistoryResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.items === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "items"},message:"must have required property '"+"items"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "items")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.items !== undefined){
let data0 = data.items;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/items",schemaPath:"#/properties/items/type",keyword:"type",params:{type: schema19.properties.items.type},message:"must be null,array"};
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
const err3 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.session_id === undefined){
const err4 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.turn_id === undefined){
const err5 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.input_id === undefined){
const err6 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "input_id"},message:"must have required property '"+"input_id"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.sequence === undefined){
const err7 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "sequence"},message:"must have required property '"+"sequence"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data1.role === undefined){
const err8 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "role"},message:"must have required property '"+"role"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data1.parts === undefined){
const err9 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "parts"},message:"must have required property '"+"parts"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data1.created_at === undefined){
const err10 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
for(const key1 in data1){
if(!((((((((key1 === "id") || (key1 === "session_id")) || (key1 === "turn_id")) || (key1 === "input_id")) || (key1 === "sequence")) || (key1 === "role")) || (key1 === "parts")) || (key1 === "created_at"))){
const err11 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data1.id !== undefined){
let data2 = data1.id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err12 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err13 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data1.session_id !== undefined){
let data3 = data1.session_id;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err14 = {instancePath:instancePath+"/items/" + i0+"/session_id",schemaPath:"#/properties/items/items/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err15 = {instancePath:instancePath+"/items/" + i0+"/session_id",schemaPath:"#/properties/items/items/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
if(data1.turn_id !== undefined){
let data4 = data1.turn_id;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err16 = {instancePath:instancePath+"/items/" + i0+"/turn_id",schemaPath:"#/properties/items/items/properties/turn_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err17 = {instancePath:instancePath+"/items/" + i0+"/turn_id",schemaPath:"#/properties/items/items/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data1.input_id !== undefined){
let data5 = data1.input_id;
if((data5 !== null) && (typeof data5 !== "string")){
const err18 = {instancePath:instancePath+"/items/" + i0+"/input_id",schemaPath:"#/properties/items/items/properties/input_id/type",keyword:"type",params:{type: schema19.properties.items.items.properties.input_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
if(typeof data5 === "string"){
if(!pattern0.test(data5)){
const err19 = {instancePath:instancePath+"/items/" + i0+"/input_id",schemaPath:"#/properties/items/items/properties/input_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data1.sequence !== undefined){
let data6 = data1.sequence;
if(typeof data6 === "string"){
if(!pattern11.test(data6)){
const err20 = {instancePath:instancePath+"/items/" + i0+"/sequence",schemaPath:"#/properties/items/items/properties/sequence/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(!(formats0.validate(data6))){
const err21 = {instancePath:instancePath+"/items/" + i0+"/sequence",schemaPath:"#/properties/items/items/properties/sequence/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err22 = {instancePath:instancePath+"/items/" + i0+"/sequence",schemaPath:"#/properties/items/items/properties/sequence/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data1.role !== undefined){
let data7 = data1.role;
if(typeof data7 !== "string"){
const err23 = {instancePath:instancePath+"/items/" + i0+"/role",schemaPath:"#/properties/items/items/properties/role/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(!((((data7 === "system") || (data7 === "user")) || (data7 === "assistant")) || (data7 === "tool"))){
const err24 = {instancePath:instancePath+"/items/" + i0+"/role",schemaPath:"#/properties/items/items/properties/role/enum",keyword:"enum",params:{allowedValues: schema19.properties.items.items.properties.role.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data1.parts !== undefined){
let data8 = data1.parts;
if(Array.isArray(data8)){
if(data8.length > 128){
const err25 = {instancePath:instancePath+"/items/" + i0+"/parts",schemaPath:"#/properties/items/items/properties/parts/maxItems",keyword:"maxItems",params:{limit: 128},message:"must NOT have more than 128 items"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data8.length < 1){
const err26 = {instancePath:instancePath+"/items/" + i0+"/parts",schemaPath:"#/properties/items/items/properties/parts/minItems",keyword:"minItems",params:{limit: 1},message:"must NOT have fewer than 1 items"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
const len1 = data8.length;
for(let i1=0; i1<len1; i1++){
let data9 = data8[i1];
const _errs22 = errors;
let valid6 = false;
let passing0 = null;
const _errs23 = errors;
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.type === undefined){
const err27 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data9.text === undefined){
const err28 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
for(const key2 in data9){
if(!((key2 === "text") || (key2 === "type"))){
const err29 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data9.text !== undefined){
let data10 = data9.text;
if(typeof data10 === "string"){
if(!pattern6.test(data10)){
const err30 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/text",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/properties/text/pattern",keyword:"pattern",params:{pattern: "^[\\s\\S]+$"},message:"must match pattern \""+"^[\\s\\S]+$"+"\""};
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
const err31 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/text",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data9.type !== undefined){
let data11 = data9.type;
if(typeof data11 !== "string"){
const err32 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/type",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(!(data11 === "text")){
const err33 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/type",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/properties/type/enum",keyword:"enum",params:{allowedValues: schema19.properties.items.items.properties.parts.items.oneOf[0].properties.type.enum},message:"must be equal to one of the allowed values"};
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
const err34 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/0/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
var _valid0 = _errs23 === errors;
if(_valid0){
valid6 = true;
passing0 = 0;
}
const _errs30 = errors;
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.type === undefined){
const err35 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(data9.reference_id === undefined){
const err36 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
for(const key3 in data9){
if(!((key3 === "reference_id") || (key3 === "type"))){
const err37 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data9.reference_id !== undefined){
let data12 = data9.reference_id;
if(typeof data12 === "string"){
if(!pattern0.test(data12)){
const err38 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/reference_id",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/properties/reference_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
else {
const err39 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/reference_id",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
if(data9.type !== undefined){
let data13 = data9.type;
if(typeof data13 !== "string"){
const err40 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/type",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(!(data13 === "content")){
const err41 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1+"/type",schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/properties/type/enum",keyword:"enum",params:{allowedValues: schema19.properties.items.items.properties.parts.items.oneOf[1].properties.type.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
}
else {
const err42 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf/1/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
var _valid0 = _errs30 === errors;
if(_valid0 && valid6){
valid6 = false;
passing0 = [passing0, 1];
}
else {
if(_valid0){
valid6 = true;
passing0 = 1;
}
}
if(!valid6){
const err43 = {instancePath:instancePath+"/items/" + i0+"/parts/" + i1,schemaPath:"#/properties/items/items/properties/parts/items/oneOf",keyword:"oneOf",params:{passingSchemas: passing0},message:"must match exactly one schema in oneOf"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
else {
errors = _errs22;
if(vErrors !== null){
if(_errs22){
vErrors.length = _errs22;
}
else {
vErrors = null;
}
}
}
}
}
else {
const err44 = {instancePath:instancePath+"/items/" + i0+"/parts",schemaPath:"#/properties/items/items/properties/parts/type",keyword:"type",params:{type: "array"},message:"must be array"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data1.created_at !== undefined){
if(typeof data1.created_at !== "string"){
const err45 = {instancePath:instancePath+"/items/" + i0+"/created_at",schemaPath:"#/properties/items/items/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
}
else {
const err46 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
}
else {
const err47 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
validate18.errors = vErrors;
return errors === 0;
}

export const InitializeParams = validate19;
const schema20 = {"type":"object","properties":{"major":{"type":"integer","minimum":4,"maximum":4},"expected_runtime_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"$id":"https://whip.dev/protocol/v4/InitializeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"InitializeParams","required":["major"],"additionalProperties":false};

function validate19(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/InitializeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.major === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "major"},message:"must have required property '"+"major"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!((key0 === "major") || (key0 === "expected_runtime_id"))){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.major !== undefined){
let data0 = data.major;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err2 = {instancePath:instancePath+"/major",schemaPath:"#/properties/major/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(typeof data0 == "number"){
if(data0 > 4 || isNaN(data0)){
const err3 = {instancePath:instancePath+"/major",schemaPath:"#/properties/major/maximum",keyword:"maximum",params:{comparison: "<=", limit: 4},message:"must be <= 4"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data0 < 4 || isNaN(data0)){
const err4 = {instancePath:instancePath+"/major",schemaPath:"#/properties/major/minimum",keyword:"minimum",params:{comparison: ">=", limit: 4},message:"must be >= 4"};
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
if(data.expected_runtime_id !== undefined){
let data1 = data.expected_runtime_id;
if((data1 !== null) && (typeof data1 !== "string")){
const err5 = {instancePath:instancePath+"/expected_runtime_id",schemaPath:"#/properties/expected_runtime_id/type",keyword:"type",params:{type: schema20.properties.expected_runtime_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err6 = {instancePath:instancePath+"/expected_runtime_id",schemaPath:"#/properties/expected_runtime_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
validate19.errors = vErrors;
return errors === 0;
}

export const InitializeResult = validate20;
const schema21 = {"type":"object","properties":{"major":{"type":"integer","minimum":4,"maximum":4},"minor":{"type":"integer"},"runtime_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"builtins":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}}},"$id":"https://whip.dev/protocol/v4/InitializeResult","$schema":"http://json-schema.org/draft-07/schema#","title":"InitializeResult","required":["major","minor","runtime_id","builtins"],"additionalProperties":false};

function validate20(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/InitializeResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.major === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "major"},message:"must have required property '"+"major"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.minor === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "minor"},message:"must have required property '"+"minor"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.runtime_id === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "runtime_id"},message:"must have required property '"+"runtime_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.builtins === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "builtins"},message:"must have required property '"+"builtins"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
for(const key0 in data){
if(!((((key0 === "major") || (key0 === "minor")) || (key0 === "runtime_id")) || (key0 === "builtins"))){
const err4 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.major !== undefined){
let data0 = data.major;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err5 = {instancePath:instancePath+"/major",schemaPath:"#/properties/major/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data0 == "number"){
if(data0 > 4 || isNaN(data0)){
const err6 = {instancePath:instancePath+"/major",schemaPath:"#/properties/major/maximum",keyword:"maximum",params:{comparison: "<=", limit: 4},message:"must be <= 4"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data0 < 4 || isNaN(data0)){
const err7 = {instancePath:instancePath+"/major",schemaPath:"#/properties/major/minimum",keyword:"minimum",params:{comparison: ">=", limit: 4},message:"must be >= 4"};
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
if(data.minor !== undefined){
let data1 = data.minor;
if(!((typeof data1 == "number") && (!(data1 % 1) && !isNaN(data1)))){
const err8 = {instancePath:instancePath+"/minor",schemaPath:"#/properties/minor/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.runtime_id !== undefined){
let data2 = data.runtime_id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err9 = {instancePath:instancePath+"/runtime_id",schemaPath:"#/properties/runtime_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err10 = {instancePath:instancePath+"/runtime_id",schemaPath:"#/properties/runtime_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data.builtins !== undefined){
let data3 = data.builtins;
if((data3 !== null) && (!(Array.isArray(data3)))){
const err11 = {instancePath:instancePath+"/builtins",schemaPath:"#/properties/builtins/type",keyword:"type",params:{type: schema21.properties.builtins.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(Array.isArray(data3)){
const len0 = data3.length;
for(let i0=0; i0<len0; i0++){
let data4 = data3[i0];
if(data4 && typeof data4 == "object" && !Array.isArray(data4)){
if(data4.id === undefined){
const err12 = {instancePath:instancePath+"/builtins/" + i0,schemaPath:"#/properties/builtins/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data4.revision === undefined){
const err13 = {instancePath:instancePath+"/builtins/" + i0,schemaPath:"#/properties/builtins/items/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
for(const key1 in data4){
if(!((key1 === "id") || (key1 === "revision"))){
const err14 = {instancePath:instancePath+"/builtins/" + i0,schemaPath:"#/properties/builtins/items/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data4.id !== undefined){
let data5 = data4.id;
if(typeof data5 === "string"){
if(!pattern0.test(data5)){
const err15 = {instancePath:instancePath+"/builtins/" + i0+"/id",schemaPath:"#/properties/builtins/items/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
else {
const err16 = {instancePath:instancePath+"/builtins/" + i0+"/id",schemaPath:"#/properties/builtins/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data4.revision !== undefined){
let data6 = data4.revision;
if(typeof data6 === "string"){
if(!pattern2.test(data6)){
const err17 = {instancePath:instancePath+"/builtins/" + i0+"/revision",schemaPath:"#/properties/builtins/items/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
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
const err18 = {instancePath:instancePath+"/builtins/" + i0+"/revision",schemaPath:"#/properties/builtins/items/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err19 = {instancePath:instancePath+"/builtins/" + i0,schemaPath:"#/properties/builtins/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
validate20.errors = vErrors;
return errors === 0;
}

export const Input = validate21;
const schema22 = {"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"source":{"type":"string","enum":["user","agent","schedule"]},"parts":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"text":{"type":"string","pattern":"^[\\s\\S]+$"},"type":{"type":"string","enum":["text"]}},"required":["type","text"],"additionalProperties":false},{"type":"object","properties":{"reference_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"type":{"type":"string","enum":["content"]}},"required":["type","reference_id"],"additionalProperties":false}]},"minItems":1,"maxItems":128},"state":{"type":"string","enum":["queued","claimed","cancelled"]},"turn_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"created_at":{"type":"string"}},"$id":"https://whip.dev/protocol/v4/Input","$schema":"http://json-schema.org/draft-07/schema#","title":"Input","required":["id","session_id","source","parts","state","turn_id","created_at"],"additionalProperties":false};

function validate21(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Input" */;
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
if(data.session_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.source === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.parts === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "parts"},message:"must have required property '"+"parts"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.state === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.turn_id === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.created_at === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
for(const key0 in data){
if(!(((((((key0 === "id") || (key0 === "session_id")) || (key0 === "source")) || (key0 === "parts")) || (key0 === "state")) || (key0 === "turn_id")) || (key0 === "created_at"))){
const err7 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.id !== undefined){
let data0 = data.id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err8 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err9 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.session_id !== undefined){
let data1 = data.session_id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err10 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err11 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.source !== undefined){
let data2 = data.source;
if(typeof data2 !== "string"){
const err12 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(!(((data2 === "user") || (data2 === "agent")) || (data2 === "schedule"))){
const err13 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/enum",keyword:"enum",params:{allowedValues: schema22.properties.source.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.parts !== undefined){
let data3 = data.parts;
if(Array.isArray(data3)){
if(data3.length > 128){
const err14 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/maxItems",keyword:"maxItems",params:{limit: 128},message:"must NOT have more than 128 items"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data3.length < 1){
const err15 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/minItems",keyword:"minItems",params:{limit: 1},message:"must NOT have fewer than 1 items"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
const len0 = data3.length;
for(let i0=0; i0<len0; i0++){
let data4 = data3[i0];
const _errs11 = errors;
let valid3 = false;
let passing0 = null;
const _errs12 = errors;
if(data4 && typeof data4 == "object" && !Array.isArray(data4)){
if(data4.type === undefined){
const err16 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data4.text === undefined){
const err17 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
for(const key1 in data4){
if(!((key1 === "text") || (key1 === "type"))){
const err18 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data4.text !== undefined){
let data5 = data4.text;
if(typeof data5 === "string"){
if(!pattern6.test(data5)){
const err19 = {instancePath:instancePath+"/parts/" + i0+"/text",schemaPath:"#/properties/parts/items/oneOf/0/properties/text/pattern",keyword:"pattern",params:{pattern: "^[\\s\\S]+$"},message:"must match pattern \""+"^[\\s\\S]+$"+"\""};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
else {
const err20 = {instancePath:instancePath+"/parts/" + i0+"/text",schemaPath:"#/properties/parts/items/oneOf/0/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data4.type !== undefined){
let data6 = data4.type;
if(typeof data6 !== "string"){
const err21 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/0/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(!(data6 === "text")){
const err22 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/0/properties/type/enum",keyword:"enum",params:{allowedValues: schema22.properties.parts.items.oneOf[0].properties.type.enum},message:"must be equal to one of the allowed values"};
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
const err23 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
var _valid0 = _errs12 === errors;
if(_valid0){
valid3 = true;
passing0 = 0;
}
const _errs19 = errors;
if(data4 && typeof data4 == "object" && !Array.isArray(data4)){
if(data4.type === undefined){
const err24 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data4.reference_id === undefined){
const err25 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
for(const key2 in data4){
if(!((key2 === "reference_id") || (key2 === "type"))){
const err26 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data4.reference_id !== undefined){
let data7 = data4.reference_id;
if(typeof data7 === "string"){
if(!pattern0.test(data7)){
const err27 = {instancePath:instancePath+"/parts/" + i0+"/reference_id",schemaPath:"#/properties/parts/items/oneOf/1/properties/reference_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err28 = {instancePath:instancePath+"/parts/" + i0+"/reference_id",schemaPath:"#/properties/parts/items/oneOf/1/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data4.type !== undefined){
let data8 = data4.type;
if(typeof data8 !== "string"){
const err29 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/1/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(!(data8 === "content")){
const err30 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/1/properties/type/enum",keyword:"enum",params:{allowedValues: schema22.properties.parts.items.oneOf[1].properties.type.enum},message:"must be equal to one of the allowed values"};
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
const err31 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
var _valid0 = _errs19 === errors;
if(_valid0 && valid3){
valid3 = false;
passing0 = [passing0, 1];
}
else {
if(_valid0){
valid3 = true;
passing0 = 1;
}
}
if(!valid3){
const err32 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf",keyword:"oneOf",params:{passingSchemas: passing0},message:"must match exactly one schema in oneOf"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
else {
errors = _errs11;
if(vErrors !== null){
if(_errs11){
vErrors.length = _errs11;
}
else {
vErrors = null;
}
}
}
}
}
else {
const err33 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/type",keyword:"type",params:{type: "array"},message:"must be array"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data.state !== undefined){
let data9 = data.state;
if(typeof data9 !== "string"){
const err34 = {instancePath:instancePath+"/state",schemaPath:"#/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(!(((data9 === "queued") || (data9 === "claimed")) || (data9 === "cancelled"))){
const err35 = {instancePath:instancePath+"/state",schemaPath:"#/properties/state/enum",keyword:"enum",params:{allowedValues: schema22.properties.state.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data.turn_id !== undefined){
let data10 = data.turn_id;
if((data10 !== null) && (typeof data10 !== "string")){
const err36 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/type",keyword:"type",params:{type: schema22.properties.turn_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(typeof data10 === "string"){
if(!pattern0.test(data10)){
const err37 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data.created_at !== undefined){
if(typeof data.created_at !== "string"){
const err38 = {instancePath:instancePath+"/created_at",schemaPath:"#/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err39 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
validate21.errors = vErrors;
return errors === 0;
}

export const InputParams = validate22;
const schema23 = {"type":"object","properties":{"input_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"$id":"https://whip.dev/protocol/v4/InputParams","$schema":"http://json-schema.org/draft-07/schema#","title":"InputParams","required":["input_id"],"additionalProperties":false};

function validate22(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/InputParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.input_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "input_id"},message:"must have required property '"+"input_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "input_id")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.input_id !== undefined){
let data0 = data.input_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/input_id",schemaPath:"#/properties/input_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err3 = {instancePath:instancePath+"/input_id",schemaPath:"#/properties/input_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate22.errors = vErrors;
return errors === 0;
}

export const LifecycleParams = validate23;
const schema24 = {"type":"object","properties":{"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"lifecycle":{"type":"string","enum":["active","stopped"]}},"$id":"https://whip.dev/protocol/v4/LifecycleParams","$schema":"http://json-schema.org/draft-07/schema#","title":"LifecycleParams","required":["session_id","lifecycle"],"additionalProperties":false};

function validate23(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/LifecycleParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.session_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.lifecycle === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "lifecycle"},message:"must have required property '"+"lifecycle"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
for(const key0 in data){
if(!((key0 === "session_id") || (key0 === "lifecycle"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.session_id !== undefined){
let data0 = data.session_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err4 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.lifecycle !== undefined){
let data1 = data.lifecycle;
if(typeof data1 !== "string"){
const err5 = {instancePath:instancePath+"/lifecycle",schemaPath:"#/properties/lifecycle/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!((data1 === "active") || (data1 === "stopped"))){
const err6 = {instancePath:instancePath+"/lifecycle",schemaPath:"#/properties/lifecycle/enum",keyword:"enum",params:{allowedValues: schema24.properties.lifecycle.enum},message:"must be equal to one of the allowed values"};
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
validate23.errors = vErrors;
return errors === 0;
}

export const ListSessionsParams = validate24;
const schema25 = {"type":"object","properties":{"tree_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"after":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"limit":{"type":"integer","minimum":1,"maximum":100}},"$id":"https://whip.dev/protocol/v4/ListSessionsParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ListSessionsParams","required":["tree_id","limit"],"additionalProperties":false};

function validate24(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/ListSessionsParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.tree_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "tree_id"},message:"must have required property '"+"tree_id"+"'"};
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
for(const key0 in data){
if(!(((key0 === "tree_id") || (key0 === "after")) || (key0 === "limit"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.tree_id !== undefined){
let data0 = data.tree_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err4 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.after !== undefined){
let data1 = data.after;
if((data1 !== null) && (typeof data1 !== "string")){
const err5 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/type",keyword:"type",params:{type: schema25.properties.after.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err6 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data.limit !== undefined){
let data2 = data.limit;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err7 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(typeof data2 == "number"){
if(data2 > 100 || isNaN(data2)){
const err8 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/maximum",keyword:"maximum",params:{comparison: "<=", limit: 100},message:"must be <= 100"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data2 < 1 || isNaN(data2)){
const err9 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
validate24.errors = vErrors;
return errors === 0;
}

export const ListSessionsResult = validate25;
const schema26 = {"type":"object","properties":{"items":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"tree_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"parent_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"definition":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false},"config_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"configuration":{"type":"object","properties":{"model":{"type":"object","properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":"object","properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output_schema":true},"required":["model","instructions","tools","children","hooks","output_schema"],"additionalProperties":false},"working_directory":{"type":"string"},"lifecycle":{"type":"string","enum":["active","stopped"]},"created_at":{"type":"string"}},"required":["id","tree_id","parent_id","definition","config_revision","configuration","working_directory","lifecycle","created_at"],"additionalProperties":false}}},"$id":"https://whip.dev/protocol/v4/ListSessionsResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ListSessionsResult","required":["items"],"additionalProperties":false};

function validate25(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/ListSessionsResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.items === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "items"},message:"must have required property '"+"items"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "items")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.items !== undefined){
let data0 = data.items;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/items",schemaPath:"#/properties/items/type",keyword:"type",params:{type: schema26.properties.items.type},message:"must be null,array"};
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
const err3 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.tree_id === undefined){
const err4 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "tree_id"},message:"must have required property '"+"tree_id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.parent_id === undefined){
const err5 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.definition === undefined){
const err6 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.config_revision === undefined){
const err7 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "config_revision"},message:"must have required property '"+"config_revision"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data1.configuration === undefined){
const err8 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "configuration"},message:"must have required property '"+"configuration"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data1.working_directory === undefined){
const err9 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "working_directory"},message:"must have required property '"+"working_directory"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data1.lifecycle === undefined){
const err10 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "lifecycle"},message:"must have required property '"+"lifecycle"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data1.created_at === undefined){
const err11 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
for(const key1 in data1){
if(!(func2.call(schema26.properties.items.items.properties, key1))){
const err12 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data1.id !== undefined){
let data2 = data1.id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err13 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err14 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data1.tree_id !== undefined){
let data3 = data1.tree_id;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err15 = {instancePath:instancePath+"/items/" + i0+"/tree_id",schemaPath:"#/properties/items/items/properties/tree_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
else {
const err16 = {instancePath:instancePath+"/items/" + i0+"/tree_id",schemaPath:"#/properties/items/items/properties/tree_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data1.parent_id !== undefined){
let data4 = data1.parent_id;
if((data4 !== null) && (typeof data4 !== "string")){
const err17 = {instancePath:instancePath+"/items/" + i0+"/parent_id",schemaPath:"#/properties/items/items/properties/parent_id/type",keyword:"type",params:{type: schema26.properties.items.items.properties.parent_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err18 = {instancePath:instancePath+"/items/" + i0+"/parent_id",schemaPath:"#/properties/items/items/properties/parent_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data1.definition !== undefined){
let data5 = data1.definition;
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.id === undefined){
const err19 = {instancePath:instancePath+"/items/" + i0+"/definition",schemaPath:"#/properties/items/items/properties/definition/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data5.revision === undefined){
const err20 = {instancePath:instancePath+"/items/" + i0+"/definition",schemaPath:"#/properties/items/items/properties/definition/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
for(const key2 in data5){
if(!((key2 === "id") || (key2 === "revision"))){
const err21 = {instancePath:instancePath+"/items/" + i0+"/definition",schemaPath:"#/properties/items/items/properties/definition/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data5.id !== undefined){
let data6 = data5.id;
if(typeof data6 === "string"){
if(!pattern0.test(data6)){
const err22 = {instancePath:instancePath+"/items/" + i0+"/definition/id",schemaPath:"#/properties/items/items/properties/definition/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
else {
const err23 = {instancePath:instancePath+"/items/" + i0+"/definition/id",schemaPath:"#/properties/items/items/properties/definition/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data5.revision !== undefined){
let data7 = data5.revision;
if(typeof data7 === "string"){
if(!pattern2.test(data7)){
const err24 = {instancePath:instancePath+"/items/" + i0+"/definition/revision",schemaPath:"#/properties/items/items/properties/definition/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
else {
const err25 = {instancePath:instancePath+"/items/" + i0+"/definition/revision",schemaPath:"#/properties/items/items/properties/definition/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err26 = {instancePath:instancePath+"/items/" + i0+"/definition",schemaPath:"#/properties/items/items/properties/definition/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data1.config_revision !== undefined){
let data8 = data1.config_revision;
if(typeof data8 === "string"){
if(!pattern11.test(data8)){
const err27 = {instancePath:instancePath+"/items/" + i0+"/config_revision",schemaPath:"#/properties/items/items/properties/config_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(!(formats0.validate(data8))){
const err28 = {instancePath:instancePath+"/items/" + i0+"/config_revision",schemaPath:"#/properties/items/items/properties/config_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err29 = {instancePath:instancePath+"/items/" + i0+"/config_revision",schemaPath:"#/properties/items/items/properties/config_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data1.configuration !== undefined){
let data9 = data1.configuration;
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.model === undefined){
const err30 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data9.instructions === undefined){
const err31 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/required",keyword:"required",params:{missingProperty: "instructions"},message:"must have required property '"+"instructions"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data9.tools === undefined){
const err32 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/required",keyword:"required",params:{missingProperty: "tools"},message:"must have required property '"+"tools"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(data9.children === undefined){
const err33 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/required",keyword:"required",params:{missingProperty: "children"},message:"must have required property '"+"children"+"'"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data9.hooks === undefined){
const err34 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/required",keyword:"required",params:{missingProperty: "hooks"},message:"must have required property '"+"hooks"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(data9.output_schema === undefined){
const err35 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
for(const key3 in data9){
if(!((((((key3 === "model") || (key3 === "instructions")) || (key3 === "tools")) || (key3 === "children")) || (key3 === "hooks")) || (key3 === "output_schema"))){
const err36 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
if(data9.model !== undefined){
let data10 = data9.model;
if(data10 && typeof data10 == "object" && !Array.isArray(data10)){
if(data10.provider === undefined){
const err37 = {instancePath:instancePath+"/items/" + i0+"/configuration/model",schemaPath:"#/properties/items/items/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
if(data10.name === undefined){
const err38 = {instancePath:instancePath+"/items/" + i0+"/configuration/model",schemaPath:"#/properties/items/items/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(data10.effort === undefined){
const err39 = {instancePath:instancePath+"/items/" + i0+"/configuration/model",schemaPath:"#/properties/items/items/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
for(const key4 in data10){
if(!(((key4 === "provider") || (key4 === "name")) || (key4 === "effort"))){
const err40 = {instancePath:instancePath+"/items/" + i0+"/configuration/model",schemaPath:"#/properties/items/items/properties/configuration/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data10.provider !== undefined){
let data11 = data10.provider;
if(typeof data11 === "string"){
if(!pattern0.test(data11)){
const err41 = {instancePath:instancePath+"/items/" + i0+"/configuration/model/provider",schemaPath:"#/properties/items/items/properties/configuration/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
else {
const err42 = {instancePath:instancePath+"/items/" + i0+"/configuration/model/provider",schemaPath:"#/properties/items/items/properties/configuration/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
if(data10.name !== undefined){
if(typeof data10.name !== "string"){
const err43 = {instancePath:instancePath+"/items/" + i0+"/configuration/model/name",schemaPath:"#/properties/items/items/properties/configuration/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
}
if(data10.effort !== undefined){
if(typeof data10.effort !== "string"){
const err44 = {instancePath:instancePath+"/items/" + i0+"/configuration/model/effort",schemaPath:"#/properties/items/items/properties/configuration/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
}
else {
const err45 = {instancePath:instancePath+"/items/" + i0+"/configuration/model",schemaPath:"#/properties/items/items/properties/configuration/properties/model/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
if(data9.instructions !== undefined){
let data14 = data9.instructions;
if(data14 && typeof data14 == "object" && !Array.isArray(data14)){
if(data14.text === undefined){
const err46 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
if(data14.project_files === undefined){
const err47 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
if(data14.discover_skills === undefined){
const err48 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
for(const key5 in data14){
if(!(((key5 === "text") || (key5 === "project_files")) || (key5 === "discover_skills"))){
const err49 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
if(data14.text !== undefined){
if(typeof data14.text !== "string"){
const err50 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions/text",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
if(data14.project_files !== undefined){
let data16 = data14.project_files;
if((data16 !== null) && (!(Array.isArray(data16)))){
const err51 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions/project_files",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema26.properties.items.items.properties.configuration.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
if(Array.isArray(data16)){
const len1 = data16.length;
for(let i1=0; i1<len1; i1++){
if(typeof data16[i1] !== "string"){
const err52 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions/project_files/" + i1,schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
}
}
if(data14.discover_skills !== undefined){
if(typeof data14.discover_skills !== "boolean"){
const err53 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions/discover_skills",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
}
}
else {
const err54 = {instancePath:instancePath+"/items/" + i0+"/configuration/instructions",schemaPath:"#/properties/items/items/properties/configuration/properties/instructions/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
if(data9.tools !== undefined){
let data19 = data9.tools;
if((!(data19 && typeof data19 == "object" && !Array.isArray(data19))) && (data19 !== null)){
const err55 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools",schemaPath:"#/properties/items/items/properties/configuration/properties/tools/type",keyword:"type",params:{type: schema26.properties.items.items.properties.configuration.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(data19 && typeof data19 == "object" && !Array.isArray(data19)){
for(const key6 in data19){
let data20 = data19[key6];
if(data20 && typeof data20 == "object" && !Array.isArray(data20)){
if(data20.description === undefined){
const err56 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
if(data20.input_schema === undefined){
const err57 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
if(data20.output_schema === undefined){
const err58 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
for(const key7 in data20){
if(!(((key7 === "description") || (key7 === "input_schema")) || (key7 === "output_schema"))){
const err59 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key7},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
if(data20.description !== undefined){
if(typeof data20.description !== "string"){
const err60 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/items/items/properties/configuration/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
}
else {
const err61 = {instancePath:instancePath+"/items/" + i0+"/configuration/tools/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
if(data9.children !== undefined){
let data22 = data9.children;
if((!(data22 && typeof data22 == "object" && !Array.isArray(data22))) && (data22 !== null)){
const err62 = {instancePath:instancePath+"/items/" + i0+"/configuration/children",schemaPath:"#/properties/items/items/properties/configuration/properties/children/type",keyword:"type",params:{type: schema26.properties.items.items.properties.configuration.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
if(data22 && typeof data22 == "object" && !Array.isArray(data22)){
for(const key8 in data22){
let data23 = data22[key8];
if(data23 && typeof data23 == "object" && !Array.isArray(data23)){
if(data23.id === undefined){
const err63 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
if(data23.revision === undefined){
const err64 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
for(const key9 in data23){
if(!((key9 === "id") || (key9 === "revision"))){
const err65 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key9},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
}
if(data23.id !== undefined){
let data24 = data23.id;
if(typeof data24 === "string"){
if(!pattern0.test(data24)){
const err66 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err67 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
}
if(data23.revision !== undefined){
let data25 = data23.revision;
if(typeof data25 === "string"){
if(!pattern2.test(data25)){
const err68 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
}
else {
const err69 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err70 = {instancePath:instancePath+"/items/" + i0+"/configuration/children/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
}
if(data9.hooks !== undefined){
let data26 = data9.hooks;
if((!(data26 && typeof data26 == "object" && !Array.isArray(data26))) && (data26 !== null)){
const err71 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks",schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/type",keyword:"type",params:{type: schema26.properties.items.items.properties.configuration.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
if(data26 && typeof data26 == "object" && !Array.isArray(data26)){
for(const key10 in data26){
let data27 = data26[key10];
if(data27 && typeof data27 == "object" && !Array.isArray(data27)){
if(data27.operations === undefined){
const err72 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
if(data27.optional === undefined){
const err73 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
if(data27.timeout_millis === undefined){
const err74 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
for(const key11 in data27){
if(!(((key11 === "operations") || (key11 === "optional")) || (key11 === "timeout_millis"))){
const err75 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key11},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
if(data27.operations !== undefined){
let data28 = data27.operations;
if((data28 !== null) && (!(Array.isArray(data28)))){
const err76 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema26.properties.items.items.properties.configuration.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
if(Array.isArray(data28)){
const len2 = data28.length;
for(let i2=0; i2<len2; i2++){
let data29 = data28[i2];
if(typeof data29 === "string"){
if(!pattern0.test(data29)){
const err77 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i2,schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
}
else {
const err78 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i2,schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
}
}
}
if(data27.optional !== undefined){
if(typeof data27.optional !== "boolean"){
const err79 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err79];
}
else {
vErrors.push(err79);
}
errors++;
}
}
if(data27.timeout_millis !== undefined){
let data31 = data27.timeout_millis;
if(!((typeof data31 == "number") && (!(data31 % 1) && !isNaN(data31)))){
const err80 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
if(typeof data31 == "number"){
if(data31 > 60000 || isNaN(data31)){
const err81 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
if(data31 < 1 || isNaN(data31)){
const err82 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
}
else {
const err83 = {instancePath:instancePath+"/items/" + i0+"/configuration/hooks/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/items/items/properties/configuration/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
}
else {
const err84 = {instancePath:instancePath+"/items/" + i0+"/configuration",schemaPath:"#/properties/items/items/properties/configuration/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
}
if(data1.working_directory !== undefined){
if(typeof data1.working_directory !== "string"){
const err85 = {instancePath:instancePath+"/items/" + i0+"/working_directory",schemaPath:"#/properties/items/items/properties/working_directory/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err85];
}
else {
vErrors.push(err85);
}
errors++;
}
}
if(data1.lifecycle !== undefined){
let data33 = data1.lifecycle;
if(typeof data33 !== "string"){
const err86 = {instancePath:instancePath+"/items/" + i0+"/lifecycle",schemaPath:"#/properties/items/items/properties/lifecycle/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
if(!((data33 === "active") || (data33 === "stopped"))){
const err87 = {instancePath:instancePath+"/items/" + i0+"/lifecycle",schemaPath:"#/properties/items/items/properties/lifecycle/enum",keyword:"enum",params:{allowedValues: schema26.properties.items.items.properties.lifecycle.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
}
if(data1.created_at !== undefined){
if(typeof data1.created_at !== "string"){
const err88 = {instancePath:instancePath+"/items/" + i0+"/created_at",schemaPath:"#/properties/items/items/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
}
}
else {
const err89 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
}
}
}
}
else {
const err90 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
validate25.errors = vErrors;
return errors === 0;
}

export const ModelAttemptsParams = validate26;
const schema27 = {"type":"object","properties":{"turn_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"after":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"limit":{"type":"integer","minimum":1,"maximum":100}},"$id":"https://whip.dev/protocol/v4/ModelAttemptsParams","$schema":"http://json-schema.org/draft-07/schema#","title":"ModelAttemptsParams","required":["turn_id","limit"],"additionalProperties":false};

function validate26(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/ModelAttemptsParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.turn_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
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
for(const key0 in data){
if(!(((key0 === "turn_id") || (key0 === "after")) || (key0 === "limit"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
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
let data0 = data.turn_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err4 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.after !== undefined){
let data1 = data.after;
if((data1 !== null) && (typeof data1 !== "string")){
const err5 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/type",keyword:"type",params:{type: schema27.properties.after.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err6 = {instancePath:instancePath+"/after",schemaPath:"#/properties/after/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data.limit !== undefined){
let data2 = data.limit;
if(!((typeof data2 == "number") && (!(data2 % 1) && !isNaN(data2)))){
const err7 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(typeof data2 == "number"){
if(data2 > 100 || isNaN(data2)){
const err8 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/maximum",keyword:"maximum",params:{comparison: "<=", limit: 100},message:"must be <= 100"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data2 < 1 || isNaN(data2)){
const err9 = {instancePath:instancePath+"/limit",schemaPath:"#/properties/limit/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
validate26.errors = vErrors;
return errors === 0;
}

export const ModelAttemptsResult = validate27;
const schema28 = {"type":"object","properties":{"items":{"type":["null","array"],"items":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"turn_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"logical_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"number":{"type":"integer","minimum":1,"maximum":100},"request":{"type":"object","properties":{"purpose":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"model":{"type":"object","properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"route":{"type":"string"},"adapter":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"request_digest":{"type":"string","pattern":"^[a-f0-9]{64}$"},"prices":{"type":"object","properties":{"input":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"output":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"reasoning":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"cached_input":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"cached_output":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"}},"required":["input","output","reasoning","cached_input","cached_output"],"additionalProperties":false},"max_output_tokens":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"timeout_millis":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"}},"required":["purpose","model","route","adapter","request_digest","prices","max_output_tokens","timeout_millis"],"additionalProperties":false},"state":{"type":"string","enum":["reserved","dispatched","succeeded","failed","cancelled","uncertain"]},"result":{"type":["null","object"],"properties":{"state":{"type":"string","enum":["succeeded","failed","cancelled","uncertain"]},"usage":{"type":"object","properties":{"input":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"output":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"reasoning":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"cached_input":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"cached_output":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"}},"required":["input","output","reasoning","cached_input","cached_output"],"additionalProperties":false},"reported_cost_nano_usd":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"failure":{"type":["null","string"]},"usage_note":{"type":["null","string"]}},"required":["state","usage","reported_cost_nano_usd","failure","usage_note"],"additionalProperties":false},"cost_nano_usd":{"type":["null","string"],"pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"cost_source":{"type":"string","enum":["unknown","provider","prices","not_dispatched"]},"cost_note":{"type":["null","string"]},"message_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"created_at":{"type":"string"},"dispatched_at":{"type":["null","string"]},"finished_at":{"type":["null","string"]}},"required":["id","turn_id","logical_id","number","request","state","result","cost_nano_usd","cost_source","cost_note","message_id","created_at","dispatched_at","finished_at"],"additionalProperties":false}}},"$id":"https://whip.dev/protocol/v4/ModelAttemptsResult","$schema":"http://json-schema.org/draft-07/schema#","title":"ModelAttemptsResult","required":["items"],"additionalProperties":false};

function validate27(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/ModelAttemptsResult" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.items === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "items"},message:"must have required property '"+"items"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "items")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.items !== undefined){
let data0 = data.items;
if((data0 !== null) && (!(Array.isArray(data0)))){
const err2 = {instancePath:instancePath+"/items",schemaPath:"#/properties/items/type",keyword:"type",params:{type: schema28.properties.items.type},message:"must be null,array"};
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
const err3 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data1.turn_id === undefined){
const err4 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data1.logical_id === undefined){
const err5 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "logical_id"},message:"must have required property '"+"logical_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1.number === undefined){
const err6 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "number"},message:"must have required property '"+"number"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.request === undefined){
const err7 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "request"},message:"must have required property '"+"request"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data1.state === undefined){
const err8 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
if(data1.result === undefined){
const err9 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "result"},message:"must have required property '"+"result"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data1.cost_nano_usd === undefined){
const err10 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "cost_nano_usd"},message:"must have required property '"+"cost_nano_usd"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data1.cost_source === undefined){
const err11 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "cost_source"},message:"must have required property '"+"cost_source"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data1.cost_note === undefined){
const err12 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "cost_note"},message:"must have required property '"+"cost_note"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data1.message_id === undefined){
const err13 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "message_id"},message:"must have required property '"+"message_id"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
if(data1.created_at === undefined){
const err14 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data1.dispatched_at === undefined){
const err15 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "dispatched_at"},message:"must have required property '"+"dispatched_at"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data1.finished_at === undefined){
const err16 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/required",keyword:"required",params:{missingProperty: "finished_at"},message:"must have required property '"+"finished_at"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
for(const key1 in data1){
if(!(func2.call(schema28.properties.items.items.properties, key1))){
const err17 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data1.id !== undefined){
let data2 = data1.id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err18 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
else {
const err19 = {instancePath:instancePath+"/items/" + i0+"/id",schemaPath:"#/properties/items/items/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data1.turn_id !== undefined){
let data3 = data1.turn_id;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err20 = {instancePath:instancePath+"/items/" + i0+"/turn_id",schemaPath:"#/properties/items/items/properties/turn_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
else {
const err21 = {instancePath:instancePath+"/items/" + i0+"/turn_id",schemaPath:"#/properties/items/items/properties/turn_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data1.logical_id !== undefined){
let data4 = data1.logical_id;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err22 = {instancePath:instancePath+"/items/" + i0+"/logical_id",schemaPath:"#/properties/items/items/properties/logical_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
else {
const err23 = {instancePath:instancePath+"/items/" + i0+"/logical_id",schemaPath:"#/properties/items/items/properties/logical_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data1.number !== undefined){
let data5 = data1.number;
if(!((typeof data5 == "number") && (!(data5 % 1) && !isNaN(data5)))){
const err24 = {instancePath:instancePath+"/items/" + i0+"/number",schemaPath:"#/properties/items/items/properties/number/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(typeof data5 == "number"){
if(data5 > 100 || isNaN(data5)){
const err25 = {instancePath:instancePath+"/items/" + i0+"/number",schemaPath:"#/properties/items/items/properties/number/maximum",keyword:"maximum",params:{comparison: "<=", limit: 100},message:"must be <= 100"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data5 < 1 || isNaN(data5)){
const err26 = {instancePath:instancePath+"/items/" + i0+"/number",schemaPath:"#/properties/items/items/properties/number/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
}
if(data1.request !== undefined){
let data6 = data1.request;
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.purpose === undefined){
const err27 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "purpose"},message:"must have required property '"+"purpose"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data6.model === undefined){
const err28 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data6.route === undefined){
const err29 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "route"},message:"must have required property '"+"route"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data6.adapter === undefined){
const err30 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "adapter"},message:"must have required property '"+"adapter"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data6.request_digest === undefined){
const err31 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "request_digest"},message:"must have required property '"+"request_digest"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data6.prices === undefined){
const err32 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "prices"},message:"must have required property '"+"prices"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(data6.max_output_tokens === undefined){
const err33 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "max_output_tokens"},message:"must have required property '"+"max_output_tokens"+"'"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data6.timeout_millis === undefined){
const err34 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
for(const key2 in data6){
if(!((((((((key2 === "purpose") || (key2 === "model")) || (key2 === "route")) || (key2 === "adapter")) || (key2 === "request_digest")) || (key2 === "prices")) || (key2 === "max_output_tokens")) || (key2 === "timeout_millis"))){
const err35 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data6.purpose !== undefined){
let data7 = data6.purpose;
if(typeof data7 === "string"){
if(!pattern0.test(data7)){
const err36 = {instancePath:instancePath+"/items/" + i0+"/request/purpose",schemaPath:"#/properties/items/items/properties/request/properties/purpose/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
else {
const err37 = {instancePath:instancePath+"/items/" + i0+"/request/purpose",schemaPath:"#/properties/items/items/properties/request/properties/purpose/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data6.model !== undefined){
let data8 = data6.model;
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
if(data8.provider === undefined){
const err38 = {instancePath:instancePath+"/items/" + i0+"/request/model",schemaPath:"#/properties/items/items/properties/request/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
if(data8.name === undefined){
const err39 = {instancePath:instancePath+"/items/" + i0+"/request/model",schemaPath:"#/properties/items/items/properties/request/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
if(data8.effort === undefined){
const err40 = {instancePath:instancePath+"/items/" + i0+"/request/model",schemaPath:"#/properties/items/items/properties/request/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
for(const key3 in data8){
if(!(((key3 === "provider") || (key3 === "name")) || (key3 === "effort"))){
const err41 = {instancePath:instancePath+"/items/" + i0+"/request/model",schemaPath:"#/properties/items/items/properties/request/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
if(data8.provider !== undefined){
let data9 = data8.provider;
if(typeof data9 === "string"){
if(!pattern0.test(data9)){
const err42 = {instancePath:instancePath+"/items/" + i0+"/request/model/provider",schemaPath:"#/properties/items/items/properties/request/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err43 = {instancePath:instancePath+"/items/" + i0+"/request/model/provider",schemaPath:"#/properties/items/items/properties/request/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
}
if(data8.name !== undefined){
if(typeof data8.name !== "string"){
const err44 = {instancePath:instancePath+"/items/" + i0+"/request/model/name",schemaPath:"#/properties/items/items/properties/request/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data8.effort !== undefined){
if(typeof data8.effort !== "string"){
const err45 = {instancePath:instancePath+"/items/" + i0+"/request/model/effort",schemaPath:"#/properties/items/items/properties/request/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
}
else {
const err46 = {instancePath:instancePath+"/items/" + i0+"/request/model",schemaPath:"#/properties/items/items/properties/request/properties/model/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data6.route !== undefined){
if(typeof data6.route !== "string"){
const err47 = {instancePath:instancePath+"/items/" + i0+"/request/route",schemaPath:"#/properties/items/items/properties/request/properties/route/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data6.adapter !== undefined){
let data13 = data6.adapter;
if(typeof data13 === "string"){
if(!pattern0.test(data13)){
const err48 = {instancePath:instancePath+"/items/" + i0+"/request/adapter",schemaPath:"#/properties/items/items/properties/request/properties/adapter/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
else {
const err49 = {instancePath:instancePath+"/items/" + i0+"/request/adapter",schemaPath:"#/properties/items/items/properties/request/properties/adapter/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
}
if(data6.request_digest !== undefined){
let data14 = data6.request_digest;
if(typeof data14 === "string"){
if(!pattern2.test(data14)){
const err50 = {instancePath:instancePath+"/items/" + i0+"/request/request_digest",schemaPath:"#/properties/items/items/properties/request/properties/request_digest/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
else {
const err51 = {instancePath:instancePath+"/items/" + i0+"/request/request_digest",schemaPath:"#/properties/items/items/properties/request/properties/request_digest/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
if(data6.prices !== undefined){
let data15 = data6.prices;
if(data15 && typeof data15 == "object" && !Array.isArray(data15)){
if(data15.input === undefined){
const err52 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/required",keyword:"required",params:{missingProperty: "input"},message:"must have required property '"+"input"+"'"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
if(data15.output === undefined){
const err53 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/required",keyword:"required",params:{missingProperty: "output"},message:"must have required property '"+"output"+"'"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
if(data15.reasoning === undefined){
const err54 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/required",keyword:"required",params:{missingProperty: "reasoning"},message:"must have required property '"+"reasoning"+"'"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
if(data15.cached_input === undefined){
const err55 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/required",keyword:"required",params:{missingProperty: "cached_input"},message:"must have required property '"+"cached_input"+"'"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
if(data15.cached_output === undefined){
const err56 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/required",keyword:"required",params:{missingProperty: "cached_output"},message:"must have required property '"+"cached_output"+"'"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
for(const key4 in data15){
if(!(((((key4 === "input") || (key4 === "output")) || (key4 === "reasoning")) || (key4 === "cached_input")) || (key4 === "cached_output"))){
const err57 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
}
if(data15.input !== undefined){
let data16 = data15.input;
if((data16 !== null) && (typeof data16 !== "string")){
const err58 = {instancePath:instancePath+"/items/" + i0+"/request/prices/input",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/input/type",keyword:"type",params:{type: schema28.properties.items.items.properties.request.properties.prices.properties.input.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
if(typeof data16 === "string"){
if(!pattern11.test(data16)){
const err59 = {instancePath:instancePath+"/items/" + i0+"/request/prices/input",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/input/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
if(!(formats0.validate(data16))){
const err60 = {instancePath:instancePath+"/items/" + i0+"/request/prices/input",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/input/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
}
if(data15.output !== undefined){
let data17 = data15.output;
if((data17 !== null) && (typeof data17 !== "string")){
const err61 = {instancePath:instancePath+"/items/" + i0+"/request/prices/output",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/output/type",keyword:"type",params:{type: schema28.properties.items.items.properties.request.properties.prices.properties.output.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
if(typeof data17 === "string"){
if(!pattern11.test(data17)){
const err62 = {instancePath:instancePath+"/items/" + i0+"/request/prices/output",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/output/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
if(!(formats0.validate(data17))){
const err63 = {instancePath:instancePath+"/items/" + i0+"/request/prices/output",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/output/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
if(data15.reasoning !== undefined){
let data18 = data15.reasoning;
if((data18 !== null) && (typeof data18 !== "string")){
const err64 = {instancePath:instancePath+"/items/" + i0+"/request/prices/reasoning",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/reasoning/type",keyword:"type",params:{type: schema28.properties.items.items.properties.request.properties.prices.properties.reasoning.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
if(typeof data18 === "string"){
if(!pattern11.test(data18)){
const err65 = {instancePath:instancePath+"/items/" + i0+"/request/prices/reasoning",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/reasoning/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
if(!(formats0.validate(data18))){
const err66 = {instancePath:instancePath+"/items/" + i0+"/request/prices/reasoning",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/reasoning/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
}
}
if(data15.cached_input !== undefined){
let data19 = data15.cached_input;
if((data19 !== null) && (typeof data19 !== "string")){
const err67 = {instancePath:instancePath+"/items/" + i0+"/request/prices/cached_input",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/cached_input/type",keyword:"type",params:{type: schema28.properties.items.items.properties.request.properties.prices.properties.cached_input.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
if(typeof data19 === "string"){
if(!pattern11.test(data19)){
const err68 = {instancePath:instancePath+"/items/" + i0+"/request/prices/cached_input",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/cached_input/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
if(!(formats0.validate(data19))){
const err69 = {instancePath:instancePath+"/items/" + i0+"/request/prices/cached_input",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/cached_input/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
if(data15.cached_output !== undefined){
let data20 = data15.cached_output;
if((data20 !== null) && (typeof data20 !== "string")){
const err70 = {instancePath:instancePath+"/items/" + i0+"/request/prices/cached_output",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/cached_output/type",keyword:"type",params:{type: schema28.properties.items.items.properties.request.properties.prices.properties.cached_output.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
if(typeof data20 === "string"){
if(!pattern11.test(data20)){
const err71 = {instancePath:instancePath+"/items/" + i0+"/request/prices/cached_output",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/cached_output/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
if(!(formats0.validate(data20))){
const err72 = {instancePath:instancePath+"/items/" + i0+"/request/prices/cached_output",schemaPath:"#/properties/items/items/properties/request/properties/prices/properties/cached_output/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
}
else {
const err73 = {instancePath:instancePath+"/items/" + i0+"/request/prices",schemaPath:"#/properties/items/items/properties/request/properties/prices/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
}
if(data6.max_output_tokens !== undefined){
let data21 = data6.max_output_tokens;
if(typeof data21 === "string"){
if(!pattern11.test(data21)){
const err74 = {instancePath:instancePath+"/items/" + i0+"/request/max_output_tokens",schemaPath:"#/properties/items/items/properties/request/properties/max_output_tokens/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
if(!(formats0.validate(data21))){
const err75 = {instancePath:instancePath+"/items/" + i0+"/request/max_output_tokens",schemaPath:"#/properties/items/items/properties/request/properties/max_output_tokens/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err75];
}
else {
vErrors.push(err75);
}
errors++;
}
}
else {
const err76 = {instancePath:instancePath+"/items/" + i0+"/request/max_output_tokens",schemaPath:"#/properties/items/items/properties/request/properties/max_output_tokens/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
}
if(data6.timeout_millis !== undefined){
let data22 = data6.timeout_millis;
if(typeof data22 === "string"){
if(!pattern11.test(data22)){
const err77 = {instancePath:instancePath+"/items/" + i0+"/request/timeout_millis",schemaPath:"#/properties/items/items/properties/request/properties/timeout_millis/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
if(!(formats0.validate(data22))){
const err78 = {instancePath:instancePath+"/items/" + i0+"/request/timeout_millis",schemaPath:"#/properties/items/items/properties/request/properties/timeout_millis/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
}
else {
const err79 = {instancePath:instancePath+"/items/" + i0+"/request/timeout_millis",schemaPath:"#/properties/items/items/properties/request/properties/timeout_millis/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err80 = {instancePath:instancePath+"/items/" + i0+"/request",schemaPath:"#/properties/items/items/properties/request/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
}
if(data1.state !== undefined){
let data23 = data1.state;
if(typeof data23 !== "string"){
const err81 = {instancePath:instancePath+"/items/" + i0+"/state",schemaPath:"#/properties/items/items/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
if(!((((((data23 === "reserved") || (data23 === "dispatched")) || (data23 === "succeeded")) || (data23 === "failed")) || (data23 === "cancelled")) || (data23 === "uncertain"))){
const err82 = {instancePath:instancePath+"/items/" + i0+"/state",schemaPath:"#/properties/items/items/properties/state/enum",keyword:"enum",params:{allowedValues: schema28.properties.items.items.properties.state.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
if(data1.result !== undefined){
let data24 = data1.result;
if((data24 !== null) && (!(data24 && typeof data24 == "object" && !Array.isArray(data24)))){
const err83 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
if(data24 && typeof data24 == "object" && !Array.isArray(data24)){
if(data24.state === undefined){
const err84 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
if(data24.usage === undefined){
const err85 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/required",keyword:"required",params:{missingProperty: "usage"},message:"must have required property '"+"usage"+"'"};
if(vErrors === null){
vErrors = [err85];
}
else {
vErrors.push(err85);
}
errors++;
}
if(data24.reported_cost_nano_usd === undefined){
const err86 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/required",keyword:"required",params:{missingProperty: "reported_cost_nano_usd"},message:"must have required property '"+"reported_cost_nano_usd"+"'"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
if(data24.failure === undefined){
const err87 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/required",keyword:"required",params:{missingProperty: "failure"},message:"must have required property '"+"failure"+"'"};
if(vErrors === null){
vErrors = [err87];
}
else {
vErrors.push(err87);
}
errors++;
}
if(data24.usage_note === undefined){
const err88 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/required",keyword:"required",params:{missingProperty: "usage_note"},message:"must have required property '"+"usage_note"+"'"};
if(vErrors === null){
vErrors = [err88];
}
else {
vErrors.push(err88);
}
errors++;
}
for(const key5 in data24){
if(!(((((key5 === "state") || (key5 === "usage")) || (key5 === "reported_cost_nano_usd")) || (key5 === "failure")) || (key5 === "usage_note"))){
const err89 = {instancePath:instancePath+"/items/" + i0+"/result",schemaPath:"#/properties/items/items/properties/result/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err89];
}
else {
vErrors.push(err89);
}
errors++;
}
}
if(data24.state !== undefined){
let data25 = data24.state;
if(typeof data25 !== "string"){
const err90 = {instancePath:instancePath+"/items/" + i0+"/result/state",schemaPath:"#/properties/items/items/properties/result/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err90];
}
else {
vErrors.push(err90);
}
errors++;
}
if(!((((data25 === "succeeded") || (data25 === "failed")) || (data25 === "cancelled")) || (data25 === "uncertain"))){
const err91 = {instancePath:instancePath+"/items/" + i0+"/result/state",schemaPath:"#/properties/items/items/properties/result/properties/state/enum",keyword:"enum",params:{allowedValues: schema28.properties.items.items.properties.result.properties.state.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err91];
}
else {
vErrors.push(err91);
}
errors++;
}
}
if(data24.usage !== undefined){
let data26 = data24.usage;
if(data26 && typeof data26 == "object" && !Array.isArray(data26)){
if(data26.input === undefined){
const err92 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/required",keyword:"required",params:{missingProperty: "input"},message:"must have required property '"+"input"+"'"};
if(vErrors === null){
vErrors = [err92];
}
else {
vErrors.push(err92);
}
errors++;
}
if(data26.output === undefined){
const err93 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/required",keyword:"required",params:{missingProperty: "output"},message:"must have required property '"+"output"+"'"};
if(vErrors === null){
vErrors = [err93];
}
else {
vErrors.push(err93);
}
errors++;
}
if(data26.reasoning === undefined){
const err94 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/required",keyword:"required",params:{missingProperty: "reasoning"},message:"must have required property '"+"reasoning"+"'"};
if(vErrors === null){
vErrors = [err94];
}
else {
vErrors.push(err94);
}
errors++;
}
if(data26.cached_input === undefined){
const err95 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/required",keyword:"required",params:{missingProperty: "cached_input"},message:"must have required property '"+"cached_input"+"'"};
if(vErrors === null){
vErrors = [err95];
}
else {
vErrors.push(err95);
}
errors++;
}
if(data26.cached_output === undefined){
const err96 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/required",keyword:"required",params:{missingProperty: "cached_output"},message:"must have required property '"+"cached_output"+"'"};
if(vErrors === null){
vErrors = [err96];
}
else {
vErrors.push(err96);
}
errors++;
}
for(const key6 in data26){
if(!(((((key6 === "input") || (key6 === "output")) || (key6 === "reasoning")) || (key6 === "cached_input")) || (key6 === "cached_output"))){
const err97 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key6},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err97];
}
else {
vErrors.push(err97);
}
errors++;
}
}
if(data26.input !== undefined){
let data27 = data26.input;
if((data27 !== null) && (typeof data27 !== "string")){
const err98 = {instancePath:instancePath+"/items/" + i0+"/result/usage/input",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/input/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.usage.properties.input.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err98];
}
else {
vErrors.push(err98);
}
errors++;
}
if(typeof data27 === "string"){
if(!pattern11.test(data27)){
const err99 = {instancePath:instancePath+"/items/" + i0+"/result/usage/input",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/input/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err99];
}
else {
vErrors.push(err99);
}
errors++;
}
if(!(formats0.validate(data27))){
const err100 = {instancePath:instancePath+"/items/" + i0+"/result/usage/input",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/input/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err100];
}
else {
vErrors.push(err100);
}
errors++;
}
}
}
if(data26.output !== undefined){
let data28 = data26.output;
if((data28 !== null) && (typeof data28 !== "string")){
const err101 = {instancePath:instancePath+"/items/" + i0+"/result/usage/output",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/output/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.usage.properties.output.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err101];
}
else {
vErrors.push(err101);
}
errors++;
}
if(typeof data28 === "string"){
if(!pattern11.test(data28)){
const err102 = {instancePath:instancePath+"/items/" + i0+"/result/usage/output",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/output/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err102];
}
else {
vErrors.push(err102);
}
errors++;
}
if(!(formats0.validate(data28))){
const err103 = {instancePath:instancePath+"/items/" + i0+"/result/usage/output",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/output/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
if(data26.reasoning !== undefined){
let data29 = data26.reasoning;
if((data29 !== null) && (typeof data29 !== "string")){
const err104 = {instancePath:instancePath+"/items/" + i0+"/result/usage/reasoning",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/reasoning/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.usage.properties.reasoning.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err104];
}
else {
vErrors.push(err104);
}
errors++;
}
if(typeof data29 === "string"){
if(!pattern11.test(data29)){
const err105 = {instancePath:instancePath+"/items/" + i0+"/result/usage/reasoning",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/reasoning/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err105];
}
else {
vErrors.push(err105);
}
errors++;
}
if(!(formats0.validate(data29))){
const err106 = {instancePath:instancePath+"/items/" + i0+"/result/usage/reasoning",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/reasoning/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
if(data26.cached_input !== undefined){
let data30 = data26.cached_input;
if((data30 !== null) && (typeof data30 !== "string")){
const err107 = {instancePath:instancePath+"/items/" + i0+"/result/usage/cached_input",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/cached_input/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.usage.properties.cached_input.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err107];
}
else {
vErrors.push(err107);
}
errors++;
}
if(typeof data30 === "string"){
if(!pattern11.test(data30)){
const err108 = {instancePath:instancePath+"/items/" + i0+"/result/usage/cached_input",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/cached_input/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err108];
}
else {
vErrors.push(err108);
}
errors++;
}
if(!(formats0.validate(data30))){
const err109 = {instancePath:instancePath+"/items/" + i0+"/result/usage/cached_input",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/cached_input/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
if(data26.cached_output !== undefined){
let data31 = data26.cached_output;
if((data31 !== null) && (typeof data31 !== "string")){
const err110 = {instancePath:instancePath+"/items/" + i0+"/result/usage/cached_output",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/cached_output/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.usage.properties.cached_output.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err110];
}
else {
vErrors.push(err110);
}
errors++;
}
if(typeof data31 === "string"){
if(!pattern11.test(data31)){
const err111 = {instancePath:instancePath+"/items/" + i0+"/result/usage/cached_output",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/cached_output/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err111];
}
else {
vErrors.push(err111);
}
errors++;
}
if(!(formats0.validate(data31))){
const err112 = {instancePath:instancePath+"/items/" + i0+"/result/usage/cached_output",schemaPath:"#/properties/items/items/properties/result/properties/usage/properties/cached_output/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
}
else {
const err113 = {instancePath:instancePath+"/items/" + i0+"/result/usage",schemaPath:"#/properties/items/items/properties/result/properties/usage/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err113];
}
else {
vErrors.push(err113);
}
errors++;
}
}
if(data24.reported_cost_nano_usd !== undefined){
let data32 = data24.reported_cost_nano_usd;
if((data32 !== null) && (typeof data32 !== "string")){
const err114 = {instancePath:instancePath+"/items/" + i0+"/result/reported_cost_nano_usd",schemaPath:"#/properties/items/items/properties/result/properties/reported_cost_nano_usd/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.reported_cost_nano_usd.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err114];
}
else {
vErrors.push(err114);
}
errors++;
}
if(typeof data32 === "string"){
if(!pattern11.test(data32)){
const err115 = {instancePath:instancePath+"/items/" + i0+"/result/reported_cost_nano_usd",schemaPath:"#/properties/items/items/properties/result/properties/reported_cost_nano_usd/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err115];
}
else {
vErrors.push(err115);
}
errors++;
}
if(!(formats0.validate(data32))){
const err116 = {instancePath:instancePath+"/items/" + i0+"/result/reported_cost_nano_usd",schemaPath:"#/properties/items/items/properties/result/properties/reported_cost_nano_usd/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
if(data24.failure !== undefined){
let data33 = data24.failure;
if((data33 !== null) && (typeof data33 !== "string")){
const err117 = {instancePath:instancePath+"/items/" + i0+"/result/failure",schemaPath:"#/properties/items/items/properties/result/properties/failure/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.failure.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err117];
}
else {
vErrors.push(err117);
}
errors++;
}
}
if(data24.usage_note !== undefined){
let data34 = data24.usage_note;
if((data34 !== null) && (typeof data34 !== "string")){
const err118 = {instancePath:instancePath+"/items/" + i0+"/result/usage_note",schemaPath:"#/properties/items/items/properties/result/properties/usage_note/type",keyword:"type",params:{type: schema28.properties.items.items.properties.result.properties.usage_note.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err118];
}
else {
vErrors.push(err118);
}
errors++;
}
}
}
}
if(data1.cost_nano_usd !== undefined){
let data35 = data1.cost_nano_usd;
if((data35 !== null) && (typeof data35 !== "string")){
const err119 = {instancePath:instancePath+"/items/" + i0+"/cost_nano_usd",schemaPath:"#/properties/items/items/properties/cost_nano_usd/type",keyword:"type",params:{type: schema28.properties.items.items.properties.cost_nano_usd.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err119];
}
else {
vErrors.push(err119);
}
errors++;
}
if(typeof data35 === "string"){
if(!pattern11.test(data35)){
const err120 = {instancePath:instancePath+"/items/" + i0+"/cost_nano_usd",schemaPath:"#/properties/items/items/properties/cost_nano_usd/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err120];
}
else {
vErrors.push(err120);
}
errors++;
}
if(!(formats0.validate(data35))){
const err121 = {instancePath:instancePath+"/items/" + i0+"/cost_nano_usd",schemaPath:"#/properties/items/items/properties/cost_nano_usd/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err121];
}
else {
vErrors.push(err121);
}
errors++;
}
}
}
if(data1.cost_source !== undefined){
let data36 = data1.cost_source;
if(typeof data36 !== "string"){
const err122 = {instancePath:instancePath+"/items/" + i0+"/cost_source",schemaPath:"#/properties/items/items/properties/cost_source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err122];
}
else {
vErrors.push(err122);
}
errors++;
}
if(!((((data36 === "unknown") || (data36 === "provider")) || (data36 === "prices")) || (data36 === "not_dispatched"))){
const err123 = {instancePath:instancePath+"/items/" + i0+"/cost_source",schemaPath:"#/properties/items/items/properties/cost_source/enum",keyword:"enum",params:{allowedValues: schema28.properties.items.items.properties.cost_source.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err123];
}
else {
vErrors.push(err123);
}
errors++;
}
}
if(data1.cost_note !== undefined){
let data37 = data1.cost_note;
if((data37 !== null) && (typeof data37 !== "string")){
const err124 = {instancePath:instancePath+"/items/" + i0+"/cost_note",schemaPath:"#/properties/items/items/properties/cost_note/type",keyword:"type",params:{type: schema28.properties.items.items.properties.cost_note.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err124];
}
else {
vErrors.push(err124);
}
errors++;
}
}
if(data1.message_id !== undefined){
let data38 = data1.message_id;
if((data38 !== null) && (typeof data38 !== "string")){
const err125 = {instancePath:instancePath+"/items/" + i0+"/message_id",schemaPath:"#/properties/items/items/properties/message_id/type",keyword:"type",params:{type: schema28.properties.items.items.properties.message_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err125];
}
else {
vErrors.push(err125);
}
errors++;
}
if(typeof data38 === "string"){
if(!pattern0.test(data38)){
const err126 = {instancePath:instancePath+"/items/" + i0+"/message_id",schemaPath:"#/properties/items/items/properties/message_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err126];
}
else {
vErrors.push(err126);
}
errors++;
}
}
}
if(data1.created_at !== undefined){
if(typeof data1.created_at !== "string"){
const err127 = {instancePath:instancePath+"/items/" + i0+"/created_at",schemaPath:"#/properties/items/items/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err127];
}
else {
vErrors.push(err127);
}
errors++;
}
}
if(data1.dispatched_at !== undefined){
let data40 = data1.dispatched_at;
if((data40 !== null) && (typeof data40 !== "string")){
const err128 = {instancePath:instancePath+"/items/" + i0+"/dispatched_at",schemaPath:"#/properties/items/items/properties/dispatched_at/type",keyword:"type",params:{type: schema28.properties.items.items.properties.dispatched_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err128];
}
else {
vErrors.push(err128);
}
errors++;
}
}
if(data1.finished_at !== undefined){
let data41 = data1.finished_at;
if((data41 !== null) && (typeof data41 !== "string")){
const err129 = {instancePath:instancePath+"/items/" + i0+"/finished_at",schemaPath:"#/properties/items/items/properties/finished_at/type",keyword:"type",params:{type: schema28.properties.items.items.properties.finished_at.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err129];
}
else {
vErrors.push(err129);
}
errors++;
}
}
}
else {
const err130 = {instancePath:instancePath+"/items/" + i0,schemaPath:"#/properties/items/items/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err130];
}
else {
vErrors.push(err130);
}
errors++;
}
}
}
}
}
else {
const err131 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err131];
}
else {
vErrors.push(err131);
}
errors++;
}
validate27.errors = vErrors;
return errors === 0;
}

export const RPCError = validate28;
const schema29 = {"type":"object","properties":{"code":{"type":"integer"},"message":{"type":"string"},"kind":{"type":"string","enum":["INVALID","NOT_FOUND","CONFLICT","BUSY","LIMIT","STOPPED","CLOSED","IDENTITY","METHOD","INTERNAL"]}},"$id":"https://whip.dev/protocol/v4/RPCError","$schema":"http://json-schema.org/draft-07/schema#","title":"RPCError","required":["code","message","kind"],"additionalProperties":false};

function validate28(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/RPCError" */;
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
if(data.kind === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
for(const key0 in data){
if(!(((key0 === "code") || (key0 === "message")) || (key0 === "kind"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.code !== undefined){
let data0 = data.code;
if(!((typeof data0 == "number") && (!(data0 % 1) && !isNaN(data0)))){
const err4 = {instancePath:instancePath+"/code",schemaPath:"#/properties/code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.message !== undefined){
if(typeof data.message !== "string"){
const err5 = {instancePath:instancePath+"/message",schemaPath:"#/properties/message/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
let data2 = data.kind;
if(typeof data2 !== "string"){
const err6 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!((((((((((data2 === "INVALID") || (data2 === "NOT_FOUND")) || (data2 === "CONFLICT")) || (data2 === "BUSY")) || (data2 === "LIMIT")) || (data2 === "STOPPED")) || (data2 === "CLOSED")) || (data2 === "IDENTITY")) || (data2 === "METHOD")) || (data2 === "INTERNAL"))){
const err7 = {instancePath:instancePath+"/kind",schemaPath:"#/properties/kind/enum",keyword:"enum",params:{allowedValues: schema29.properties.kind.enum},message:"must be equal to one of the allowed values"};
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
validate28.errors = vErrors;
return errors === 0;
}

export const Request = validate29;
const schema30 = {"type":"object","properties":{"jsonrpc":{"type":"string","enum":["2.0"]},"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"method":{"type":"string"},"params":true},"$id":"https://whip.dev/protocol/v4/Request","$schema":"http://json-schema.org/draft-07/schema#","title":"Request","required":["jsonrpc","id","method","params"],"additionalProperties":false};

function validate29(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Request" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.jsonrpc === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "jsonrpc"},message:"must have required property '"+"jsonrpc"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.method === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "method"},message:"must have required property '"+"method"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.params === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "params"},message:"must have required property '"+"params"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
for(const key0 in data){
if(!((((key0 === "jsonrpc") || (key0 === "id")) || (key0 === "method")) || (key0 === "params"))){
const err4 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.jsonrpc !== undefined){
let data0 = data.jsonrpc;
if(typeof data0 !== "string"){
const err5 = {instancePath:instancePath+"/jsonrpc",schemaPath:"#/properties/jsonrpc/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(!(data0 === "2.0")){
const err6 = {instancePath:instancePath+"/jsonrpc",schemaPath:"#/properties/jsonrpc/enum",keyword:"enum",params:{allowedValues: schema30.properties.jsonrpc.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.id !== undefined){
let data1 = data.id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err7 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err8 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.method !== undefined){
if(typeof data.method !== "string"){
const err9 = {instancePath:instancePath+"/method",schemaPath:"#/properties/method/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate29.errors = vErrors;
return errors === 0;
}

export const RequestIdentity = validate30;
const schema31 = {"type":"object","properties":{"client_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"request_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"$id":"https://whip.dev/protocol/v4/RequestIdentity","$schema":"http://json-schema.org/draft-07/schema#","title":"RequestIdentity","required":["client_id","request_id"],"additionalProperties":false};

function validate30(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/RequestIdentity" */;
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
if(data.request_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "request_id"},message:"must have required property '"+"request_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
for(const key0 in data){
if(!((key0 === "client_id") || (key0 === "request_id"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.client_id !== undefined){
let data0 = data.client_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/client_id",schemaPath:"#/properties/client_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data.request_id !== undefined){
let data1 = data.request_id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err5 = {instancePath:instancePath+"/request_id",schemaPath:"#/properties/request_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err6 = {instancePath:instancePath+"/request_id",schemaPath:"#/properties/request_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate30.errors = vErrors;
return errors === 0;
}

export const Response = validate31;
const schema32 = {"type":"object","properties":{"jsonrpc":{"type":"string","enum":["2.0"]},"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"result":true,"error":{"type":["null","object"],"properties":{"code":{"type":"integer"},"message":{"type":"string"},"kind":{"type":"string","enum":["INVALID","NOT_FOUND","CONFLICT","BUSY","LIMIT","STOPPED","CLOSED","IDENTITY","METHOD","INTERNAL"]}},"required":["code","message","kind"],"additionalProperties":false}},"$id":"https://whip.dev/protocol/v4/Response","$schema":"http://json-schema.org/draft-07/schema#","title":"Response","required":["jsonrpc","id"],"additionalProperties":false,"oneOf":[{"required":["result"],"not":{"required":["error"]}},{"required":["error"],"not":{"required":["result"]}}]};

function validate31(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Response" */;
let vErrors = null;
let errors = 0;
const _errs1 = errors;
let valid0 = false;
let passing0 = null;
const _errs2 = errors;
const _errs3 = errors;
const _errs4 = errors;
if(data && typeof data == "object" && !Array.isArray(data)){
let missing0;
if((data.error === undefined) && (missing0 = "error")){
const err0 = {};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
}
var valid1 = _errs4 === errors;
if(valid1){
const err1 = {instancePath,schemaPath:"#/oneOf/0/not",keyword:"not",params:{},message:"must NOT be valid"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
else {
errors = _errs3;
if(vErrors !== null){
if(_errs3){
vErrors.length = _errs3;
}
else {
vErrors = null;
}
}
}
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.result === undefined){
const err2 = {instancePath,schemaPath:"#/oneOf/0/required",keyword:"required",params:{missingProperty: "result"},message:"must have required property '"+"result"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
var _valid0 = _errs2 === errors;
if(_valid0){
valid0 = true;
passing0 = 0;
}
const _errs5 = errors;
const _errs6 = errors;
const _errs7 = errors;
if(data && typeof data == "object" && !Array.isArray(data)){
let missing1;
if((data.result === undefined) && (missing1 = "result")){
const err3 = {};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
var valid2 = _errs7 === errors;
if(valid2){
const err4 = {instancePath,schemaPath:"#/oneOf/1/not",keyword:"not",params:{},message:"must NOT be valid"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
else {
errors = _errs6;
if(vErrors !== null){
if(_errs6){
vErrors.length = _errs6;
}
else {
vErrors = null;
}
}
}
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.error === undefined){
const err5 = {instancePath,schemaPath:"#/oneOf/1/required",keyword:"required",params:{missingProperty: "error"},message:"must have required property '"+"error"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
var _valid0 = _errs5 === errors;
if(_valid0 && valid0){
valid0 = false;
passing0 = [passing0, 1];
}
else {
if(_valid0){
valid0 = true;
passing0 = 1;
}
}
if(!valid0){
const err6 = {instancePath,schemaPath:"#/oneOf",keyword:"oneOf",params:{passingSchemas: passing0},message:"must match exactly one schema in oneOf"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
else {
errors = _errs1;
if(vErrors !== null){
if(_errs1){
vErrors.length = _errs1;
}
else {
vErrors = null;
}
}
}
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.jsonrpc === undefined){
const err7 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "jsonrpc"},message:"must have required property '"+"jsonrpc"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data.id === undefined){
const err8 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
for(const key0 in data){
if(!((((key0 === "jsonrpc") || (key0 === "id")) || (key0 === "result")) || (key0 === "error"))){
const err9 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.jsonrpc !== undefined){
let data0 = data.jsonrpc;
if(typeof data0 !== "string"){
const err10 = {instancePath:instancePath+"/jsonrpc",schemaPath:"#/properties/jsonrpc/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(!(data0 === "2.0")){
const err11 = {instancePath:instancePath+"/jsonrpc",schemaPath:"#/properties/jsonrpc/enum",keyword:"enum",params:{allowedValues: schema32.properties.jsonrpc.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.id !== undefined){
let data1 = data.id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err12 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data.error !== undefined){
let data2 = data.error;
if((data2 !== null) && (!(data2 && typeof data2 == "object" && !Array.isArray(data2)))){
const err14 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/type",keyword:"type",params:{type: schema32.properties.error.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.code === undefined){
const err15 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/required",keyword:"required",params:{missingProperty: "code"},message:"must have required property '"+"code"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data2.message === undefined){
const err16 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/required",keyword:"required",params:{missingProperty: "message"},message:"must have required property '"+"message"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data2.kind === undefined){
const err17 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/required",keyword:"required",params:{missingProperty: "kind"},message:"must have required property '"+"kind"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
for(const key1 in data2){
if(!(((key1 === "code") || (key1 === "message")) || (key1 === "kind"))){
const err18 = {instancePath:instancePath+"/error",schemaPath:"#/properties/error/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data2.code !== undefined){
let data3 = data2.code;
if(!((typeof data3 == "number") && (!(data3 % 1) && !isNaN(data3)))){
const err19 = {instancePath:instancePath+"/error/code",schemaPath:"#/properties/error/properties/code/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
if(data2.message !== undefined){
if(typeof data2.message !== "string"){
const err20 = {instancePath:instancePath+"/error/message",schemaPath:"#/properties/error/properties/message/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data2.kind !== undefined){
let data5 = data2.kind;
if(typeof data5 !== "string"){
const err21 = {instancePath:instancePath+"/error/kind",schemaPath:"#/properties/error/properties/kind/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(!((((((((((data5 === "INVALID") || (data5 === "NOT_FOUND")) || (data5 === "CONFLICT")) || (data5 === "BUSY")) || (data5 === "LIMIT")) || (data5 === "STOPPED")) || (data5 === "CLOSED")) || (data5 === "IDENTITY")) || (data5 === "METHOD")) || (data5 === "INTERNAL"))){
const err22 = {instancePath:instancePath+"/error/kind",schemaPath:"#/properties/error/properties/kind/enum",keyword:"enum",params:{allowedValues: schema32.properties.error.properties.kind.enum},message:"must be equal to one of the allowed values"};
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
}
else {
const err23 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
validate31.errors = vErrors;
return errors === 0;
}

export const Session = validate32;
const schema33 = {"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"tree_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"parent_id":{"type":["null","string"],"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"definition":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false},"config_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"configuration":{"type":"object","properties":{"model":{"type":"object","properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":"object","properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output_schema":true},"required":["model","instructions","tools","children","hooks","output_schema"],"additionalProperties":false},"working_directory":{"type":"string"},"lifecycle":{"type":"string","enum":["active","stopped"]},"created_at":{"type":"string"}},"$id":"https://whip.dev/protocol/v4/Session","$schema":"http://json-schema.org/draft-07/schema#","title":"Session","required":["id","tree_id","parent_id","definition","config_revision","configuration","working_directory","lifecycle","created_at"],"additionalProperties":false};

function validate32(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Session" */;
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
if(data.tree_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "tree_id"},message:"must have required property '"+"tree_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.parent_id === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.definition === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "definition"},message:"must have required property '"+"definition"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.config_revision === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "config_revision"},message:"must have required property '"+"config_revision"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.configuration === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "configuration"},message:"must have required property '"+"configuration"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.working_directory === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "working_directory"},message:"must have required property '"+"working_directory"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data.lifecycle === undefined){
const err7 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "lifecycle"},message:"must have required property '"+"lifecycle"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
if(data.created_at === undefined){
const err8 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
for(const key0 in data){
if(!(func2.call(schema33.properties, key0))){
const err9 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.id !== undefined){
let data0 = data.id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err10 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err11 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.tree_id !== undefined){
let data1 = data.tree_id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err12 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err13 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data.parent_id !== undefined){
let data2 = data.parent_id;
if((data2 !== null) && (typeof data2 !== "string")){
const err14 = {instancePath:instancePath+"/parent_id",schemaPath:"#/properties/parent_id/type",keyword:"type",params:{type: schema33.properties.parent_id.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err15 = {instancePath:instancePath+"/parent_id",schemaPath:"#/properties/parent_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
if(data.definition !== undefined){
let data3 = data.definition;
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.id === undefined){
const err16 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data3.revision === undefined){
const err17 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
for(const key1 in data3){
if(!((key1 === "id") || (key1 === "revision"))){
const err18 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data3.id !== undefined){
let data4 = data3.id;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err19 = {instancePath:instancePath+"/definition/id",schemaPath:"#/properties/definition/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
else {
const err20 = {instancePath:instancePath+"/definition/id",schemaPath:"#/properties/definition/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data3.revision !== undefined){
let data5 = data3.revision;
if(typeof data5 === "string"){
if(!pattern2.test(data5)){
const err21 = {instancePath:instancePath+"/definition/revision",schemaPath:"#/properties/definition/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
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
const err22 = {instancePath:instancePath+"/definition/revision",schemaPath:"#/properties/definition/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err23 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data.config_revision !== undefined){
let data6 = data.config_revision;
if(typeof data6 === "string"){
if(!pattern11.test(data6)){
const err24 = {instancePath:instancePath+"/config_revision",schemaPath:"#/properties/config_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(!(formats0.validate(data6))){
const err25 = {instancePath:instancePath+"/config_revision",schemaPath:"#/properties/config_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err26 = {instancePath:instancePath+"/config_revision",schemaPath:"#/properties/config_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
if(data.configuration !== undefined){
let data7 = data.configuration;
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.model === undefined){
const err27 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/required",keyword:"required",params:{missingProperty: "model"},message:"must have required property '"+"model"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data7.instructions === undefined){
const err28 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/required",keyword:"required",params:{missingProperty: "instructions"},message:"must have required property '"+"instructions"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data7.tools === undefined){
const err29 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/required",keyword:"required",params:{missingProperty: "tools"},message:"must have required property '"+"tools"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data7.children === undefined){
const err30 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/required",keyword:"required",params:{missingProperty: "children"},message:"must have required property '"+"children"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data7.hooks === undefined){
const err31 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/required",keyword:"required",params:{missingProperty: "hooks"},message:"must have required property '"+"hooks"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
if(data7.output_schema === undefined){
const err32 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
for(const key2 in data7){
if(!((((((key2 === "model") || (key2 === "instructions")) || (key2 === "tools")) || (key2 === "children")) || (key2 === "hooks")) || (key2 === "output_schema"))){
const err33 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
}
if(data7.model !== undefined){
let data8 = data7.model;
if(data8 && typeof data8 == "object" && !Array.isArray(data8)){
if(data8.provider === undefined){
const err34 = {instancePath:instancePath+"/configuration/model",schemaPath:"#/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(data8.name === undefined){
const err35 = {instancePath:instancePath+"/configuration/model",schemaPath:"#/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(data8.effort === undefined){
const err36 = {instancePath:instancePath+"/configuration/model",schemaPath:"#/properties/configuration/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
for(const key3 in data8){
if(!(((key3 === "provider") || (key3 === "name")) || (key3 === "effort"))){
const err37 = {instancePath:instancePath+"/configuration/model",schemaPath:"#/properties/configuration/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
}
if(data8.provider !== undefined){
let data9 = data8.provider;
if(typeof data9 === "string"){
if(!pattern0.test(data9)){
const err38 = {instancePath:instancePath+"/configuration/model/provider",schemaPath:"#/properties/configuration/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
else {
const err39 = {instancePath:instancePath+"/configuration/model/provider",schemaPath:"#/properties/configuration/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
if(data8.name !== undefined){
if(typeof data8.name !== "string"){
const err40 = {instancePath:instancePath+"/configuration/model/name",schemaPath:"#/properties/configuration/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data8.effort !== undefined){
if(typeof data8.effort !== "string"){
const err41 = {instancePath:instancePath+"/configuration/model/effort",schemaPath:"#/properties/configuration/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
}
else {
const err42 = {instancePath:instancePath+"/configuration/model",schemaPath:"#/properties/configuration/properties/model/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
if(data7.instructions !== undefined){
let data12 = data7.instructions;
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
if(data12.text === undefined){
const err43 = {instancePath:instancePath+"/configuration/instructions",schemaPath:"#/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err43];
}
else {
vErrors.push(err43);
}
errors++;
}
if(data12.project_files === undefined){
const err44 = {instancePath:instancePath+"/configuration/instructions",schemaPath:"#/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
if(data12.discover_skills === undefined){
const err45 = {instancePath:instancePath+"/configuration/instructions",schemaPath:"#/properties/configuration/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
for(const key4 in data12){
if(!(((key4 === "text") || (key4 === "project_files")) || (key4 === "discover_skills"))){
const err46 = {instancePath:instancePath+"/configuration/instructions",schemaPath:"#/properties/configuration/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
}
if(data12.text !== undefined){
if(typeof data12.text !== "string"){
const err47 = {instancePath:instancePath+"/configuration/instructions/text",schemaPath:"#/properties/configuration/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
}
if(data12.project_files !== undefined){
let data14 = data12.project_files;
if((data14 !== null) && (!(Array.isArray(data14)))){
const err48 = {instancePath:instancePath+"/configuration/instructions/project_files",schemaPath:"#/properties/configuration/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema33.properties.configuration.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
if(Array.isArray(data14)){
const len0 = data14.length;
for(let i0=0; i0<len0; i0++){
if(typeof data14[i0] !== "string"){
const err49 = {instancePath:instancePath+"/configuration/instructions/project_files/" + i0,schemaPath:"#/properties/configuration/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data12.discover_skills !== undefined){
if(typeof data12.discover_skills !== "boolean"){
const err50 = {instancePath:instancePath+"/configuration/instructions/discover_skills",schemaPath:"#/properties/configuration/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
}
else {
const err51 = {instancePath:instancePath+"/configuration/instructions",schemaPath:"#/properties/configuration/properties/instructions/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
if(data7.tools !== undefined){
let data17 = data7.tools;
if((!(data17 && typeof data17 == "object" && !Array.isArray(data17))) && (data17 !== null)){
const err52 = {instancePath:instancePath+"/configuration/tools",schemaPath:"#/properties/configuration/properties/tools/type",keyword:"type",params:{type: schema33.properties.configuration.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
if(data17 && typeof data17 == "object" && !Array.isArray(data17)){
for(const key5 in data17){
let data18 = data17[key5];
if(data18 && typeof data18 == "object" && !Array.isArray(data18)){
if(data18.description === undefined){
const err53 = {instancePath:instancePath+"/configuration/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
if(data18.input_schema === undefined){
const err54 = {instancePath:instancePath+"/configuration/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
if(data18.output_schema === undefined){
const err55 = {instancePath:instancePath+"/configuration/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err55];
}
else {
vErrors.push(err55);
}
errors++;
}
for(const key6 in data18){
if(!(((key6 === "description") || (key6 === "input_schema")) || (key6 === "output_schema"))){
const err56 = {instancePath:instancePath+"/configuration/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key6},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
if(data18.description !== undefined){
if(typeof data18.description !== "string"){
const err57 = {instancePath:instancePath+"/configuration/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/configuration/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err58 = {instancePath:instancePath+"/configuration/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
}
}
}
if(data7.children !== undefined){
let data20 = data7.children;
if((!(data20 && typeof data20 == "object" && !Array.isArray(data20))) && (data20 !== null)){
const err59 = {instancePath:instancePath+"/configuration/children",schemaPath:"#/properties/configuration/properties/children/type",keyword:"type",params:{type: schema33.properties.configuration.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
if(data20 && typeof data20 == "object" && !Array.isArray(data20)){
for(const key7 in data20){
let data21 = data20[key7];
if(data21 && typeof data21 == "object" && !Array.isArray(data21)){
if(data21.id === undefined){
const err60 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
if(data21.revision === undefined){
const err61 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
for(const key8 in data21){
if(!((key8 === "id") || (key8 === "revision"))){
const err62 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key8},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
}
if(data21.id !== undefined){
let data22 = data21.id;
if(typeof data22 === "string"){
if(!pattern0.test(data22)){
const err63 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/configuration/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err63];
}
else {
vErrors.push(err63);
}
errors++;
}
}
else {
const err64 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/configuration/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data21.revision !== undefined){
let data23 = data21.revision;
if(typeof data23 === "string"){
if(!pattern2.test(data23)){
const err65 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/configuration/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err65];
}
else {
vErrors.push(err65);
}
errors++;
}
}
else {
const err66 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/configuration/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err66];
}
else {
vErrors.push(err66);
}
errors++;
}
}
}
else {
const err67 = {instancePath:instancePath+"/configuration/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err67];
}
else {
vErrors.push(err67);
}
errors++;
}
}
}
}
if(data7.hooks !== undefined){
let data24 = data7.hooks;
if((!(data24 && typeof data24 == "object" && !Array.isArray(data24))) && (data24 !== null)){
const err68 = {instancePath:instancePath+"/configuration/hooks",schemaPath:"#/properties/configuration/properties/hooks/type",keyword:"type",params:{type: schema33.properties.configuration.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err68];
}
else {
vErrors.push(err68);
}
errors++;
}
if(data24 && typeof data24 == "object" && !Array.isArray(data24)){
for(const key9 in data24){
let data25 = data24[key9];
if(data25 && typeof data25 == "object" && !Array.isArray(data25)){
if(data25.operations === undefined){
const err69 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err69];
}
else {
vErrors.push(err69);
}
errors++;
}
if(data25.optional === undefined){
const err70 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err70];
}
else {
vErrors.push(err70);
}
errors++;
}
if(data25.timeout_millis === undefined){
const err71 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err71];
}
else {
vErrors.push(err71);
}
errors++;
}
for(const key10 in data25){
if(!(((key10 === "operations") || (key10 === "optional")) || (key10 === "timeout_millis"))){
const err72 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key10},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err72];
}
else {
vErrors.push(err72);
}
errors++;
}
}
if(data25.operations !== undefined){
let data26 = data25.operations;
if((data26 !== null) && (!(Array.isArray(data26)))){
const err73 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema33.properties.configuration.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err73];
}
else {
vErrors.push(err73);
}
errors++;
}
if(Array.isArray(data26)){
const len1 = data26.length;
for(let i1=0; i1<len1; i1++){
let data27 = data26[i1];
if(typeof data27 === "string"){
if(!pattern0.test(data27)){
const err74 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err74];
}
else {
vErrors.push(err74);
}
errors++;
}
}
else {
const err75 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
}
if(data25.optional !== undefined){
if(typeof data25.optional !== "boolean"){
const err76 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err76];
}
else {
vErrors.push(err76);
}
errors++;
}
}
if(data25.timeout_millis !== undefined){
let data29 = data25.timeout_millis;
if(!((typeof data29 == "number") && (!(data29 % 1) && !isNaN(data29)))){
const err77 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err77];
}
else {
vErrors.push(err77);
}
errors++;
}
if(typeof data29 == "number"){
if(data29 > 60000 || isNaN(data29)){
const err78 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err78];
}
else {
vErrors.push(err78);
}
errors++;
}
if(data29 < 1 || isNaN(data29)){
const err79 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
}
else {
const err80 = {instancePath:instancePath+"/configuration/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/configuration/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err80];
}
else {
vErrors.push(err80);
}
errors++;
}
}
}
}
}
else {
const err81 = {instancePath:instancePath+"/configuration",schemaPath:"#/properties/configuration/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err81];
}
else {
vErrors.push(err81);
}
errors++;
}
}
if(data.working_directory !== undefined){
if(typeof data.working_directory !== "string"){
const err82 = {instancePath:instancePath+"/working_directory",schemaPath:"#/properties/working_directory/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err82];
}
else {
vErrors.push(err82);
}
errors++;
}
}
if(data.lifecycle !== undefined){
let data31 = data.lifecycle;
if(typeof data31 !== "string"){
const err83 = {instancePath:instancePath+"/lifecycle",schemaPath:"#/properties/lifecycle/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err83];
}
else {
vErrors.push(err83);
}
errors++;
}
if(!((data31 === "active") || (data31 === "stopped"))){
const err84 = {instancePath:instancePath+"/lifecycle",schemaPath:"#/properties/lifecycle/enum",keyword:"enum",params:{allowedValues: schema33.properties.lifecycle.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err84];
}
else {
vErrors.push(err84);
}
errors++;
}
}
if(data.created_at !== undefined){
if(typeof data.created_at !== "string"){
const err85 = {instancePath:instancePath+"/created_at",schemaPath:"#/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err86 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err86];
}
else {
vErrors.push(err86);
}
errors++;
}
validate32.errors = vErrors;
return errors === 0;
}

export const SessionParams = validate33;
const schema34 = {"type":"object","properties":{"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"$id":"https://whip.dev/protocol/v4/SessionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"SessionParams","required":["session_id"],"additionalProperties":false};

function validate33(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/SessionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.session_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "session_id")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.session_id !== undefined){
let data0 = data.session_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err3 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate33.errors = vErrors;
return errors === 0;
}

export const SpawnSessionParams = validate34;
const schema35 = {"type":"object","properties":{"parent_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"definition":{"type":["null","object"],"properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false},"overrides":{"type":"object","properties":{"model":{"type":["null","object"],"properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":["null","object"],"properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output":{"type":["null","object"],"properties":{"schema":true},"required":["schema"],"additionalProperties":false}},"additionalProperties":false},"working_directory":{"type":["null","string"]}},"$id":"https://whip.dev/protocol/v4/SpawnSessionParams","$schema":"http://json-schema.org/draft-07/schema#","title":"SpawnSessionParams","required":["parent_id","overrides"],"additionalProperties":false};

function validate34(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/SpawnSessionParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.parent_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "parent_id"},message:"must have required property '"+"parent_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.overrides === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "overrides"},message:"must have required property '"+"overrides"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
for(const key0 in data){
if(!((((key0 === "parent_id") || (key0 === "definition")) || (key0 === "overrides")) || (key0 === "working_directory"))){
const err2 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
}
if(data.parent_id !== undefined){
let data0 = data.parent_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err3 = {instancePath:instancePath+"/parent_id",schemaPath:"#/properties/parent_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err4 = {instancePath:instancePath+"/parent_id",schemaPath:"#/properties/parent_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.definition !== undefined){
let data1 = data.definition;
if((data1 !== null) && (!(data1 && typeof data1 == "object" && !Array.isArray(data1)))){
const err5 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/type",keyword:"type",params:{type: schema35.properties.definition.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.id === undefined){
const err6 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(data1.revision === undefined){
const err7 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
for(const key1 in data1){
if(!((key1 === "id") || (key1 === "revision"))){
const err8 = {instancePath:instancePath+"/definition",schemaPath:"#/properties/definition/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data1.id !== undefined){
let data2 = data1.id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err9 = {instancePath:instancePath+"/definition/id",schemaPath:"#/properties/definition/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err10 = {instancePath:instancePath+"/definition/id",schemaPath:"#/properties/definition/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
}
if(data1.revision !== undefined){
let data3 = data1.revision;
if(typeof data3 === "string"){
if(!pattern2.test(data3)){
const err11 = {instancePath:instancePath+"/definition/revision",schemaPath:"#/properties/definition/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
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
const err12 = {instancePath:instancePath+"/definition/revision",schemaPath:"#/properties/definition/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data.overrides !== undefined){
let data4 = data.overrides;
if(data4 && typeof data4 == "object" && !Array.isArray(data4)){
for(const key2 in data4){
if(!((((((key2 === "model") || (key2 === "instructions")) || (key2 === "tools")) || (key2 === "children")) || (key2 === "hooks")) || (key2 === "output"))){
const err13 = {instancePath:instancePath+"/overrides",schemaPath:"#/properties/overrides/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data4.model !== undefined){
let data5 = data4.model;
if((data5 !== null) && (!(data5 && typeof data5 == "object" && !Array.isArray(data5)))){
const err14 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/type",keyword:"type",params:{type: schema35.properties.overrides.properties.model.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
if(data5 && typeof data5 == "object" && !Array.isArray(data5)){
if(data5.provider === undefined){
const err15 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(data5.name === undefined){
const err16 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
if(data5.effort === undefined){
const err17 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
for(const key3 in data5){
if(!(((key3 === "provider") || (key3 === "name")) || (key3 === "effort"))){
const err18 = {instancePath:instancePath+"/overrides/model",schemaPath:"#/properties/overrides/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data5.provider !== undefined){
let data6 = data5.provider;
if(typeof data6 === "string"){
if(!pattern0.test(data6)){
const err19 = {instancePath:instancePath+"/overrides/model/provider",schemaPath:"#/properties/overrides/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
}
else {
const err20 = {instancePath:instancePath+"/overrides/model/provider",schemaPath:"#/properties/overrides/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
}
if(data5.name !== undefined){
if(typeof data5.name !== "string"){
const err21 = {instancePath:instancePath+"/overrides/model/name",schemaPath:"#/properties/overrides/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data5.effort !== undefined){
if(typeof data5.effort !== "string"){
const err22 = {instancePath:instancePath+"/overrides/model/effort",schemaPath:"#/properties/overrides/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data4.instructions !== undefined){
let data9 = data4.instructions;
if((data9 !== null) && (!(data9 && typeof data9 == "object" && !Array.isArray(data9)))){
const err23 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/type",keyword:"type",params:{type: schema35.properties.overrides.properties.instructions.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(data9 && typeof data9 == "object" && !Array.isArray(data9)){
if(data9.text === undefined){
const err24 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data9.project_files === undefined){
const err25 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(data9.discover_skills === undefined){
const err26 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
for(const key4 in data9){
if(!(((key4 === "text") || (key4 === "project_files")) || (key4 === "discover_skills"))){
const err27 = {instancePath:instancePath+"/overrides/instructions",schemaPath:"#/properties/overrides/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key4},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
}
if(data9.text !== undefined){
if(typeof data9.text !== "string"){
const err28 = {instancePath:instancePath+"/overrides/instructions/text",schemaPath:"#/properties/overrides/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
}
if(data9.project_files !== undefined){
let data11 = data9.project_files;
if((data11 !== null) && (!(Array.isArray(data11)))){
const err29 = {instancePath:instancePath+"/overrides/instructions/project_files",schemaPath:"#/properties/overrides/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema35.properties.overrides.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(Array.isArray(data11)){
const len0 = data11.length;
for(let i0=0; i0<len0; i0++){
if(typeof data11[i0] !== "string"){
const err30 = {instancePath:instancePath+"/overrides/instructions/project_files/" + i0,schemaPath:"#/properties/overrides/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
}
if(data9.discover_skills !== undefined){
if(typeof data9.discover_skills !== "boolean"){
const err31 = {instancePath:instancePath+"/overrides/instructions/discover_skills",schemaPath:"#/properties/overrides/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
if(data4.tools !== undefined){
let data14 = data4.tools;
if((!(data14 && typeof data14 == "object" && !Array.isArray(data14))) && (data14 !== null)){
const err32 = {instancePath:instancePath+"/overrides/tools",schemaPath:"#/properties/overrides/properties/tools/type",keyword:"type",params:{type: schema35.properties.overrides.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(data14 && typeof data14 == "object" && !Array.isArray(data14)){
for(const key5 in data14){
let data15 = data14[key5];
if(data15 && typeof data15 == "object" && !Array.isArray(data15)){
if(data15.description === undefined){
const err33 = {instancePath:instancePath+"/overrides/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(data15.input_schema === undefined){
const err34 = {instancePath:instancePath+"/overrides/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
if(data15.output_schema === undefined){
const err35 = {instancePath:instancePath+"/overrides/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
for(const key6 in data15){
if(!(((key6 === "description") || (key6 === "input_schema")) || (key6 === "output_schema"))){
const err36 = {instancePath:instancePath+"/overrides/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key6},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
}
if(data15.description !== undefined){
if(typeof data15.description !== "string"){
const err37 = {instancePath:instancePath+"/overrides/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/overrides/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err38 = {instancePath:instancePath+"/overrides/tools/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
}
if(data4.children !== undefined){
let data17 = data4.children;
if((!(data17 && typeof data17 == "object" && !Array.isArray(data17))) && (data17 !== null)){
const err39 = {instancePath:instancePath+"/overrides/children",schemaPath:"#/properties/overrides/properties/children/type",keyword:"type",params:{type: schema35.properties.overrides.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
if(data17 && typeof data17 == "object" && !Array.isArray(data17)){
for(const key7 in data17){
let data18 = data17[key7];
if(data18 && typeof data18 == "object" && !Array.isArray(data18)){
if(data18.id === undefined){
const err40 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
if(data18.revision === undefined){
const err41 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
for(const key8 in data18){
if(!((key8 === "id") || (key8 === "revision"))){
const err42 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key8},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
if(data18.id !== undefined){
let data19 = data18.id;
if(typeof data19 === "string"){
if(!pattern0.test(data19)){
const err43 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err44 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
}
if(data18.revision !== undefined){
let data20 = data18.revision;
if(typeof data20 === "string"){
if(!pattern2.test(data20)){
const err45 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
}
else {
const err46 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/overrides/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
else {
const err47 = {instancePath:instancePath+"/overrides/children/" + key7.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
if(data4.hooks !== undefined){
let data21 = data4.hooks;
if((!(data21 && typeof data21 == "object" && !Array.isArray(data21))) && (data21 !== null)){
const err48 = {instancePath:instancePath+"/overrides/hooks",schemaPath:"#/properties/overrides/properties/hooks/type",keyword:"type",params:{type: schema35.properties.overrides.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
if(data21 && typeof data21 == "object" && !Array.isArray(data21)){
for(const key9 in data21){
let data22 = data21[key9];
if(data22 && typeof data22 == "object" && !Array.isArray(data22)){
if(data22.operations === undefined){
const err49 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
if(data22.optional === undefined){
const err50 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
if(data22.timeout_millis === undefined){
const err51 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
for(const key10 in data22){
if(!(((key10 === "operations") || (key10 === "optional")) || (key10 === "timeout_millis"))){
const err52 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key10},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data22.operations !== undefined){
let data23 = data22.operations;
if((data23 !== null) && (!(Array.isArray(data23)))){
const err53 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema35.properties.overrides.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
if(Array.isArray(data23)){
const len1 = data23.length;
for(let i1=0; i1<len1; i1++){
let data24 = data23[i1];
if(typeof data24 === "string"){
if(!pattern0.test(data24)){
const err54 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
}
else {
const err55 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data22.optional !== undefined){
if(typeof data22.optional !== "boolean"){
const err56 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
if(data22.timeout_millis !== undefined){
let data26 = data22.timeout_millis;
if(!((typeof data26 == "number") && (!(data26 % 1) && !isNaN(data26)))){
const err57 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
if(typeof data26 == "number"){
if(data26 > 60000 || isNaN(data26)){
const err58 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
if(data26 < 1 || isNaN(data26)){
const err59 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
}
}
else {
const err60 = {instancePath:instancePath+"/overrides/hooks/" + key9.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/overrides/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
}
}
if(data4.output !== undefined){
let data27 = data4.output;
if((data27 !== null) && (!(data27 && typeof data27 == "object" && !Array.isArray(data27)))){
const err61 = {instancePath:instancePath+"/overrides/output",schemaPath:"#/properties/overrides/properties/output/type",keyword:"type",params:{type: schema35.properties.overrides.properties.output.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
if(data27 && typeof data27 == "object" && !Array.isArray(data27)){
if(data27.schema === undefined){
const err62 = {instancePath:instancePath+"/overrides/output",schemaPath:"#/properties/overrides/properties/output/required",keyword:"required",params:{missingProperty: "schema"},message:"must have required property '"+"schema"+"'"};
if(vErrors === null){
vErrors = [err62];
}
else {
vErrors.push(err62);
}
errors++;
}
for(const key11 in data27){
if(!(key11 === "schema")){
const err63 = {instancePath:instancePath+"/overrides/output",schemaPath:"#/properties/overrides/properties/output/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key11},message:"must NOT have additional properties"};
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
}
else {
const err64 = {instancePath:instancePath+"/overrides",schemaPath:"#/properties/overrides/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err64];
}
else {
vErrors.push(err64);
}
errors++;
}
}
if(data.working_directory !== undefined){
let data28 = data.working_directory;
if((data28 !== null) && (typeof data28 !== "string")){
const err65 = {instancePath:instancePath+"/working_directory",schemaPath:"#/properties/working_directory/type",keyword:"type",params:{type: schema35.properties.working_directory.type},message:"must be null,string"};
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
validate34.errors = vErrors;
return errors === 0;
}

export const SubmitParams = validate35;
const schema36 = {"type":"object","properties":{"identity":{"type":"object","properties":{"client_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"request_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"required":["client_id","request_id"],"additionalProperties":false},"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"source":{"type":"string","enum":["user","agent","schedule"]},"parts":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"text":{"type":"string","pattern":"^[\\s\\S]+$"},"type":{"type":"string","enum":["text"]}},"required":["type","text"],"additionalProperties":false},{"type":"object","properties":{"reference_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"type":{"type":"string","enum":["content"]}},"required":["type","reference_id"],"additionalProperties":false}]},"minItems":1,"maxItems":128}},"$id":"https://whip.dev/protocol/v4/SubmitParams","$schema":"http://json-schema.org/draft-07/schema#","title":"SubmitParams","required":["identity","session_id","source","parts"],"additionalProperties":false};

function validate35(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/SubmitParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.identity === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "identity"},message:"must have required property '"+"identity"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.session_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.source === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "source"},message:"must have required property '"+"source"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.parts === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "parts"},message:"must have required property '"+"parts"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
for(const key0 in data){
if(!((((key0 === "identity") || (key0 === "session_id")) || (key0 === "source")) || (key0 === "parts"))){
const err4 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
}
if(data.identity !== undefined){
let data0 = data.identity;
if(data0 && typeof data0 == "object" && !Array.isArray(data0)){
if(data0.client_id === undefined){
const err5 = {instancePath:instancePath+"/identity",schemaPath:"#/properties/identity/required",keyword:"required",params:{missingProperty: "client_id"},message:"must have required property '"+"client_id"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data0.request_id === undefined){
const err6 = {instancePath:instancePath+"/identity",schemaPath:"#/properties/identity/required",keyword:"required",params:{missingProperty: "request_id"},message:"must have required property '"+"request_id"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
for(const key1 in data0){
if(!((key1 === "client_id") || (key1 === "request_id"))){
const err7 = {instancePath:instancePath+"/identity",schemaPath:"#/properties/identity/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data0.client_id !== undefined){
let data1 = data0.client_id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err8 = {instancePath:instancePath+"/identity/client_id",schemaPath:"#/properties/identity/properties/client_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err9 = {instancePath:instancePath+"/identity/client_id",schemaPath:"#/properties/identity/properties/client_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data0.request_id !== undefined){
let data2 = data0.request_id;
if(typeof data2 === "string"){
if(!pattern0.test(data2)){
const err10 = {instancePath:instancePath+"/identity/request_id",schemaPath:"#/properties/identity/properties/request_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err11 = {instancePath:instancePath+"/identity/request_id",schemaPath:"#/properties/identity/properties/request_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err12 = {instancePath:instancePath+"/identity",schemaPath:"#/properties/identity/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data.session_id !== undefined){
let data3 = data.session_id;
if(typeof data3 === "string"){
if(!pattern0.test(data3)){
const err13 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err14 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.source !== undefined){
let data4 = data.source;
if(typeof data4 !== "string"){
const err15 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(!(((data4 === "user") || (data4 === "agent")) || (data4 === "schedule"))){
const err16 = {instancePath:instancePath+"/source",schemaPath:"#/properties/source/enum",keyword:"enum",params:{allowedValues: schema36.properties.source.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.parts !== undefined){
let data5 = data.parts;
if(Array.isArray(data5)){
if(data5.length > 128){
const err17 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/maxItems",keyword:"maxItems",params:{limit: 128},message:"must NOT have more than 128 items"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(data5.length < 1){
const err18 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/minItems",keyword:"minItems",params:{limit: 1},message:"must NOT have fewer than 1 items"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
const len0 = data5.length;
for(let i0=0; i0<len0; i0++){
let data6 = data5[i0];
const _errs16 = errors;
let valid4 = false;
let passing0 = null;
const _errs17 = errors;
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.type === undefined){
const err19 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data6.text === undefined){
const err20 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
for(const key2 in data6){
if(!((key2 === "text") || (key2 === "type"))){
const err21 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
}
if(data6.text !== undefined){
let data7 = data6.text;
if(typeof data7 === "string"){
if(!pattern6.test(data7)){
const err22 = {instancePath:instancePath+"/parts/" + i0+"/text",schemaPath:"#/properties/parts/items/oneOf/0/properties/text/pattern",keyword:"pattern",params:{pattern: "^[\\s\\S]+$"},message:"must match pattern \""+"^[\\s\\S]+$"+"\""};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
else {
const err23 = {instancePath:instancePath+"/parts/" + i0+"/text",schemaPath:"#/properties/parts/items/oneOf/0/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data6.type !== undefined){
let data8 = data6.type;
if(typeof data8 !== "string"){
const err24 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/0/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(!(data8 === "text")){
const err25 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/0/properties/type/enum",keyword:"enum",params:{allowedValues: schema36.properties.parts.items.oneOf[0].properties.type.enum},message:"must be equal to one of the allowed values"};
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
const err26 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/0/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
var _valid0 = _errs17 === errors;
if(_valid0){
valid4 = true;
passing0 = 0;
}
const _errs24 = errors;
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.type === undefined){
const err27 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "type"},message:"must have required property '"+"type"+"'"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data6.reference_id === undefined){
const err28 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/required",keyword:"required",params:{missingProperty: "reference_id"},message:"must have required property '"+"reference_id"+"'"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
for(const key3 in data6){
if(!((key3 === "reference_id") || (key3 === "type"))){
const err29 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
}
if(data6.reference_id !== undefined){
let data9 = data6.reference_id;
if(typeof data9 === "string"){
if(!pattern0.test(data9)){
const err30 = {instancePath:instancePath+"/parts/" + i0+"/reference_id",schemaPath:"#/properties/parts/items/oneOf/1/properties/reference_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err31 = {instancePath:instancePath+"/parts/" + i0+"/reference_id",schemaPath:"#/properties/parts/items/oneOf/1/properties/reference_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
}
if(data6.type !== undefined){
let data10 = data6.type;
if(typeof data10 !== "string"){
const err32 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/1/properties/type/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
if(!(data10 === "content")){
const err33 = {instancePath:instancePath+"/parts/" + i0+"/type",schemaPath:"#/properties/parts/items/oneOf/1/properties/type/enum",keyword:"enum",params:{allowedValues: schema36.properties.parts.items.oneOf[1].properties.type.enum},message:"must be equal to one of the allowed values"};
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
const err34 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf/1/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
var _valid0 = _errs24 === errors;
if(_valid0 && valid4){
valid4 = false;
passing0 = [passing0, 1];
}
else {
if(_valid0){
valid4 = true;
passing0 = 1;
}
}
if(!valid4){
const err35 = {instancePath:instancePath+"/parts/" + i0,schemaPath:"#/properties/parts/items/oneOf",keyword:"oneOf",params:{passingSchemas: passing0},message:"must match exactly one schema in oneOf"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
else {
errors = _errs16;
if(vErrors !== null){
if(_errs16){
vErrors.length = _errs16;
}
else {
vErrors = null;
}
}
}
}
}
else {
const err36 = {instancePath:instancePath+"/parts",schemaPath:"#/properties/parts/type",keyword:"type",params:{type: "array"},message:"must be array"};
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
validate35.errors = vErrors;
return errors === 0;
}

export const Tree = validate36;
const schema37 = {"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"metadata":{"type":"object","properties":{"title":{"type":["null","string"]},"archived":{"type":"boolean"},"pinned":{"type":"boolean"}},"required":["title","archived","pinned"],"additionalProperties":false},"engine":{"type":"string","enum":["starlark","quickjs"]},"policy":{"type":"object","properties":{"max_depth":{"type":"integer","minimum":0,"maximum":128},"max_sessions":{"type":"integer","minimum":1,"maximum":10000},"max_queued_inputs_per_session":{"type":"integer","minimum":1,"maximum":10000}},"required":["max_depth","max_sessions","max_queued_inputs_per_session"],"additionalProperties":false},"revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"created_at":{"type":"string"}},"$id":"https://whip.dev/protocol/v4/Tree","$schema":"http://json-schema.org/draft-07/schema#","title":"Tree","required":["id","metadata","engine","policy","revision","created_at"],"additionalProperties":false};

function validate36(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Tree" */;
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
if(data.metadata === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "metadata"},message:"must have required property '"+"metadata"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.engine === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "engine"},message:"must have required property '"+"engine"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.policy === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "policy"},message:"must have required property '"+"policy"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.revision === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.created_at === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "created_at"},message:"must have required property '"+"created_at"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
for(const key0 in data){
if(!((((((key0 === "id") || (key0 === "metadata")) || (key0 === "engine")) || (key0 === "policy")) || (key0 === "revision")) || (key0 === "created_at"))){
const err6 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
}
if(data.id !== undefined){
let data0 = data.id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err7 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err8 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.metadata !== undefined){
let data1 = data.metadata;
if(data1 && typeof data1 == "object" && !Array.isArray(data1)){
if(data1.title === undefined){
const err9 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data1.archived === undefined){
const err10 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "archived"},message:"must have required property '"+"archived"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data1.pinned === undefined){
const err11 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
for(const key1 in data1){
if(!(((key1 === "title") || (key1 === "archived")) || (key1 === "pinned"))){
const err12 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data1.title !== undefined){
let data2 = data1.title;
if((data2 !== null) && (typeof data2 !== "string")){
const err13 = {instancePath:instancePath+"/metadata/title",schemaPath:"#/properties/metadata/properties/title/type",keyword:"type",params:{type: schema37.properties.metadata.properties.title.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data1.archived !== undefined){
if(typeof data1.archived !== "boolean"){
const err14 = {instancePath:instancePath+"/metadata/archived",schemaPath:"#/properties/metadata/properties/archived/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data1.pinned !== undefined){
if(typeof data1.pinned !== "boolean"){
const err15 = {instancePath:instancePath+"/metadata/pinned",schemaPath:"#/properties/metadata/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
const err16 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.engine !== undefined){
let data5 = data.engine;
if(typeof data5 !== "string"){
const err17 = {instancePath:instancePath+"/engine",schemaPath:"#/properties/engine/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
if(!((data5 === "starlark") || (data5 === "quickjs"))){
const err18 = {instancePath:instancePath+"/engine",schemaPath:"#/properties/engine/enum",keyword:"enum",params:{allowedValues: schema37.properties.engine.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data.policy !== undefined){
let data6 = data.policy;
if(data6 && typeof data6 == "object" && !Array.isArray(data6)){
if(data6.max_depth === undefined){
const err19 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/required",keyword:"required",params:{missingProperty: "max_depth"},message:"must have required property '"+"max_depth"+"'"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data6.max_sessions === undefined){
const err20 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/required",keyword:"required",params:{missingProperty: "max_sessions"},message:"must have required property '"+"max_sessions"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data6.max_queued_inputs_per_session === undefined){
const err21 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/required",keyword:"required",params:{missingProperty: "max_queued_inputs_per_session"},message:"must have required property '"+"max_queued_inputs_per_session"+"'"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
for(const key2 in data6){
if(!(((key2 === "max_depth") || (key2 === "max_sessions")) || (key2 === "max_queued_inputs_per_session"))){
const err22 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
}
if(data6.max_depth !== undefined){
let data7 = data6.max_depth;
if(!((typeof data7 == "number") && (!(data7 % 1) && !isNaN(data7)))){
const err23 = {instancePath:instancePath+"/policy/max_depth",schemaPath:"#/properties/policy/properties/max_depth/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
if(typeof data7 == "number"){
if(data7 > 128 || isNaN(data7)){
const err24 = {instancePath:instancePath+"/policy/max_depth",schemaPath:"#/properties/policy/properties/max_depth/maximum",keyword:"maximum",params:{comparison: "<=", limit: 128},message:"must be <= 128"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
if(data7 < 0 || isNaN(data7)){
const err25 = {instancePath:instancePath+"/policy/max_depth",schemaPath:"#/properties/policy/properties/max_depth/minimum",keyword:"minimum",params:{comparison: ">=", limit: 0},message:"must be >= 0"};
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
if(data6.max_sessions !== undefined){
let data8 = data6.max_sessions;
if(!((typeof data8 == "number") && (!(data8 % 1) && !isNaN(data8)))){
const err26 = {instancePath:instancePath+"/policy/max_sessions",schemaPath:"#/properties/policy/properties/max_sessions/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
if(typeof data8 == "number"){
if(data8 > 10000 || isNaN(data8)){
const err27 = {instancePath:instancePath+"/policy/max_sessions",schemaPath:"#/properties/policy/properties/max_sessions/maximum",keyword:"maximum",params:{comparison: "<=", limit: 10000},message:"must be <= 10000"};
if(vErrors === null){
vErrors = [err27];
}
else {
vErrors.push(err27);
}
errors++;
}
if(data8 < 1 || isNaN(data8)){
const err28 = {instancePath:instancePath+"/policy/max_sessions",schemaPath:"#/properties/policy/properties/max_sessions/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
if(data6.max_queued_inputs_per_session !== undefined){
let data9 = data6.max_queued_inputs_per_session;
if(!((typeof data9 == "number") && (!(data9 % 1) && !isNaN(data9)))){
const err29 = {instancePath:instancePath+"/policy/max_queued_inputs_per_session",schemaPath:"#/properties/policy/properties/max_queued_inputs_per_session/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(typeof data9 == "number"){
if(data9 > 10000 || isNaN(data9)){
const err30 = {instancePath:instancePath+"/policy/max_queued_inputs_per_session",schemaPath:"#/properties/policy/properties/max_queued_inputs_per_session/maximum",keyword:"maximum",params:{comparison: "<=", limit: 10000},message:"must be <= 10000"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data9 < 1 || isNaN(data9)){
const err31 = {instancePath:instancePath+"/policy/max_queued_inputs_per_session",schemaPath:"#/properties/policy/properties/max_queued_inputs_per_session/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
else {
const err32 = {instancePath:instancePath+"/policy",schemaPath:"#/properties/policy/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data.revision !== undefined){
let data10 = data.revision;
if(typeof data10 === "string"){
if(!pattern11.test(data10)){
const err33 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err33];
}
else {
vErrors.push(err33);
}
errors++;
}
if(!(formats0.validate(data10))){
const err34 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
if(vErrors === null){
vErrors = [err34];
}
else {
vErrors.push(err34);
}
errors++;
}
}
else {
const err35 = {instancePath:instancePath+"/revision",schemaPath:"#/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
}
if(data.created_at !== undefined){
if(typeof data.created_at !== "string"){
const err36 = {instancePath:instancePath+"/created_at",schemaPath:"#/properties/created_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate36.errors = vErrors;
return errors === 0;
}

export const TreeParams = validate37;
const schema38 = {"type":"object","properties":{"tree_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"$id":"https://whip.dev/protocol/v4/TreeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"TreeParams","required":["tree_id"],"additionalProperties":false};

function validate37(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/TreeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.tree_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "tree_id"},message:"must have required property '"+"tree_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "tree_id")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.tree_id !== undefined){
let data0 = data.tree_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err3 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
validate37.errors = vErrors;
return errors === 0;
}

export const Turn = validate38;
const schema39 = {"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"config_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"state":{"type":"string","enum":["running","cancelling","succeeded","failed","cancelled","interrupted"]},"failure":{"type":["null","string"]},"started_at":{"type":"string"},"finished_at":{"type":["null","string"]}},"$id":"https://whip.dev/protocol/v4/Turn","$schema":"http://json-schema.org/draft-07/schema#","title":"Turn","required":["id","session_id","config_revision","state","failure","started_at","finished_at"],"additionalProperties":false};

function validate38(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/Turn" */;
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
if(data.session_id === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.config_revision === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "config_revision"},message:"must have required property '"+"config_revision"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
if(data.state === undefined){
const err3 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "state"},message:"must have required property '"+"state"+"'"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
if(data.failure === undefined){
const err4 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "failure"},message:"must have required property '"+"failure"+"'"};
if(vErrors === null){
vErrors = [err4];
}
else {
vErrors.push(err4);
}
errors++;
}
if(data.started_at === undefined){
const err5 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "started_at"},message:"must have required property '"+"started_at"+"'"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
if(data.finished_at === undefined){
const err6 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "finished_at"},message:"must have required property '"+"finished_at"+"'"};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
for(const key0 in data){
if(!(((((((key0 === "id") || (key0 === "session_id")) || (key0 === "config_revision")) || (key0 === "state")) || (key0 === "failure")) || (key0 === "started_at")) || (key0 === "finished_at"))){
const err7 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err7];
}
else {
vErrors.push(err7);
}
errors++;
}
}
if(data.id !== undefined){
let data0 = data.id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err8 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err9 = {instancePath:instancePath+"/id",schemaPath:"#/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data.session_id !== undefined){
let data1 = data.session_id;
if(typeof data1 === "string"){
if(!pattern0.test(data1)){
const err10 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err11 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
}
if(data.config_revision !== undefined){
let data2 = data.config_revision;
if(typeof data2 === "string"){
if(!pattern11.test(data2)){
const err12 = {instancePath:instancePath+"/config_revision",schemaPath:"#/properties/config_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(!(formats0.validate(data2))){
const err13 = {instancePath:instancePath+"/config_revision",schemaPath:"#/properties/config_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err14 = {instancePath:instancePath+"/config_revision",schemaPath:"#/properties/config_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data.state !== undefined){
let data3 = data.state;
if(typeof data3 !== "string"){
const err15 = {instancePath:instancePath+"/state",schemaPath:"#/properties/state/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
if(!((((((data3 === "running") || (data3 === "cancelling")) || (data3 === "succeeded")) || (data3 === "failed")) || (data3 === "cancelled")) || (data3 === "interrupted"))){
const err16 = {instancePath:instancePath+"/state",schemaPath:"#/properties/state/enum",keyword:"enum",params:{allowedValues: schema39.properties.state.enum},message:"must be equal to one of the allowed values"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data.failure !== undefined){
let data4 = data.failure;
if((data4 !== null) && (typeof data4 !== "string")){
const err17 = {instancePath:instancePath+"/failure",schemaPath:"#/properties/failure/type",keyword:"type",params:{type: schema39.properties.failure.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data.started_at !== undefined){
if(typeof data.started_at !== "string"){
const err18 = {instancePath:instancePath+"/started_at",schemaPath:"#/properties/started_at/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err18];
}
else {
vErrors.push(err18);
}
errors++;
}
}
if(data.finished_at !== undefined){
let data6 = data.finished_at;
if((data6 !== null) && (typeof data6 !== "string")){
const err19 = {instancePath:instancePath+"/finished_at",schemaPath:"#/properties/finished_at/type",keyword:"type",params:{type: schema39.properties.finished_at.type},message:"must be null,string"};
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
validate38.errors = vErrors;
return errors === 0;
}

export const TurnParams = validate39;
const schema40 = {"type":"object","properties":{"turn_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"$id":"https://whip.dev/protocol/v4/TurnParams","$schema":"http://json-schema.org/draft-07/schema#","title":"TurnParams","required":["turn_id"],"additionalProperties":false};

function validate39(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/TurnParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.turn_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "turn_id"},message:"must have required property '"+"turn_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
for(const key0 in data){
if(!(key0 === "turn_id")){
const err1 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
}
if(data.turn_id !== undefined){
let data0 = data.turn_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err2 = {instancePath:instancePath+"/turn_id",schemaPath:"#/properties/turn_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
validate39.errors = vErrors;
return errors === 0;
}

export const UpdateConfigurationParams = validate40;
const schema41 = {"type":"object","properties":{"session_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"expected_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"patch":{"type":"object","properties":{"model":{"type":["null","object"],"properties":{"provider":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"name":{"type":"string"},"effort":{"type":"string"}},"required":["provider","name","effort"],"additionalProperties":false},"instructions":{"type":["null","object"],"properties":{"text":{"type":"string"},"project_files":{"type":["null","array"],"items":{"type":"string"}},"discover_skills":{"type":"boolean"}},"required":["text","project_files","discover_skills"],"additionalProperties":false},"tools":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"description":{"type":"string"},"input_schema":true,"output_schema":true},"required":["description","input_schema","output_schema"],"additionalProperties":false}},"children":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"revision":{"type":"string","pattern":"^[a-f0-9]{64}$"}},"required":["id","revision"],"additionalProperties":false}},"hooks":{"type":["object","null"],"additionalProperties":{"type":"object","properties":{"operations":{"type":["null","array"],"items":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"}},"optional":{"type":"boolean"},"timeout_millis":{"type":"integer","minimum":1,"maximum":60000}},"required":["operations","optional","timeout_millis"],"additionalProperties":false}},"output":{"type":["null","object"],"properties":{"schema":true},"required":["schema"],"additionalProperties":false}},"additionalProperties":false}},"$id":"https://whip.dev/protocol/v4/UpdateConfigurationParams","$schema":"http://json-schema.org/draft-07/schema#","title":"UpdateConfigurationParams","required":["session_id","expected_revision","patch"],"additionalProperties":false};

function validate40(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/UpdateConfigurationParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.session_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "session_id"},message:"must have required property '"+"session_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.expected_revision === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expected_revision"},message:"must have required property '"+"expected_revision"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.patch === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "patch"},message:"must have required property '"+"patch"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
for(const key0 in data){
if(!(((key0 === "session_id") || (key0 === "expected_revision")) || (key0 === "patch"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.session_id !== undefined){
let data0 = data.session_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err4 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err5 = {instancePath:instancePath+"/session_id",schemaPath:"#/properties/session_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.expected_revision !== undefined){
let data1 = data.expected_revision;
if(typeof data1 === "string"){
if(!pattern11.test(data1)){
const err6 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!(formats0.validate(data1))){
const err7 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err8 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.patch !== undefined){
let data2 = data.patch;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
for(const key1 in data2){
if(!((((((key1 === "model") || (key1 === "instructions")) || (key1 === "tools")) || (key1 === "children")) || (key1 === "hooks")) || (key1 === "output"))){
const err9 = {instancePath:instancePath+"/patch",schemaPath:"#/properties/patch/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
}
if(data2.model !== undefined){
let data3 = data2.model;
if((data3 !== null) && (!(data3 && typeof data3 == "object" && !Array.isArray(data3)))){
const err10 = {instancePath:instancePath+"/patch/model",schemaPath:"#/properties/patch/properties/model/type",keyword:"type",params:{type: schema41.properties.patch.properties.model.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data3 && typeof data3 == "object" && !Array.isArray(data3)){
if(data3.provider === undefined){
const err11 = {instancePath:instancePath+"/patch/model",schemaPath:"#/properties/patch/properties/model/required",keyword:"required",params:{missingProperty: "provider"},message:"must have required property '"+"provider"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
if(data3.name === undefined){
const err12 = {instancePath:instancePath+"/patch/model",schemaPath:"#/properties/patch/properties/model/required",keyword:"required",params:{missingProperty: "name"},message:"must have required property '"+"name"+"'"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
if(data3.effort === undefined){
const err13 = {instancePath:instancePath+"/patch/model",schemaPath:"#/properties/patch/properties/model/required",keyword:"required",params:{missingProperty: "effort"},message:"must have required property '"+"effort"+"'"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
for(const key2 in data3){
if(!(((key2 === "provider") || (key2 === "name")) || (key2 === "effort"))){
const err14 = {instancePath:instancePath+"/patch/model",schemaPath:"#/properties/patch/properties/model/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key2},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data3.provider !== undefined){
let data4 = data3.provider;
if(typeof data4 === "string"){
if(!pattern0.test(data4)){
const err15 = {instancePath:instancePath+"/patch/model/provider",schemaPath:"#/properties/patch/properties/model/properties/provider/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err15];
}
else {
vErrors.push(err15);
}
errors++;
}
}
else {
const err16 = {instancePath:instancePath+"/patch/model/provider",schemaPath:"#/properties/patch/properties/model/properties/provider/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err16];
}
else {
vErrors.push(err16);
}
errors++;
}
}
if(data3.name !== undefined){
if(typeof data3.name !== "string"){
const err17 = {instancePath:instancePath+"/patch/model/name",schemaPath:"#/properties/patch/properties/model/properties/name/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err17];
}
else {
vErrors.push(err17);
}
errors++;
}
}
if(data3.effort !== undefined){
if(typeof data3.effort !== "string"){
const err18 = {instancePath:instancePath+"/patch/model/effort",schemaPath:"#/properties/patch/properties/model/properties/effort/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
if(data2.instructions !== undefined){
let data7 = data2.instructions;
if((data7 !== null) && (!(data7 && typeof data7 == "object" && !Array.isArray(data7)))){
const err19 = {instancePath:instancePath+"/patch/instructions",schemaPath:"#/properties/patch/properties/instructions/type",keyword:"type",params:{type: schema41.properties.patch.properties.instructions.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err19];
}
else {
vErrors.push(err19);
}
errors++;
}
if(data7 && typeof data7 == "object" && !Array.isArray(data7)){
if(data7.text === undefined){
const err20 = {instancePath:instancePath+"/patch/instructions",schemaPath:"#/properties/patch/properties/instructions/required",keyword:"required",params:{missingProperty: "text"},message:"must have required property '"+"text"+"'"};
if(vErrors === null){
vErrors = [err20];
}
else {
vErrors.push(err20);
}
errors++;
}
if(data7.project_files === undefined){
const err21 = {instancePath:instancePath+"/patch/instructions",schemaPath:"#/properties/patch/properties/instructions/required",keyword:"required",params:{missingProperty: "project_files"},message:"must have required property '"+"project_files"+"'"};
if(vErrors === null){
vErrors = [err21];
}
else {
vErrors.push(err21);
}
errors++;
}
if(data7.discover_skills === undefined){
const err22 = {instancePath:instancePath+"/patch/instructions",schemaPath:"#/properties/patch/properties/instructions/required",keyword:"required",params:{missingProperty: "discover_skills"},message:"must have required property '"+"discover_skills"+"'"};
if(vErrors === null){
vErrors = [err22];
}
else {
vErrors.push(err22);
}
errors++;
}
for(const key3 in data7){
if(!(((key3 === "text") || (key3 === "project_files")) || (key3 === "discover_skills"))){
const err23 = {instancePath:instancePath+"/patch/instructions",schemaPath:"#/properties/patch/properties/instructions/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key3},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err23];
}
else {
vErrors.push(err23);
}
errors++;
}
}
if(data7.text !== undefined){
if(typeof data7.text !== "string"){
const err24 = {instancePath:instancePath+"/patch/instructions/text",schemaPath:"#/properties/patch/properties/instructions/properties/text/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err24];
}
else {
vErrors.push(err24);
}
errors++;
}
}
if(data7.project_files !== undefined){
let data9 = data7.project_files;
if((data9 !== null) && (!(Array.isArray(data9)))){
const err25 = {instancePath:instancePath+"/patch/instructions/project_files",schemaPath:"#/properties/patch/properties/instructions/properties/project_files/type",keyword:"type",params:{type: schema41.properties.patch.properties.instructions.properties.project_files.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err25];
}
else {
vErrors.push(err25);
}
errors++;
}
if(Array.isArray(data9)){
const len0 = data9.length;
for(let i0=0; i0<len0; i0++){
if(typeof data9[i0] !== "string"){
const err26 = {instancePath:instancePath+"/patch/instructions/project_files/" + i0,schemaPath:"#/properties/patch/properties/instructions/properties/project_files/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err26];
}
else {
vErrors.push(err26);
}
errors++;
}
}
}
}
if(data7.discover_skills !== undefined){
if(typeof data7.discover_skills !== "boolean"){
const err27 = {instancePath:instancePath+"/patch/instructions/discover_skills",schemaPath:"#/properties/patch/properties/instructions/properties/discover_skills/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
if(data2.tools !== undefined){
let data12 = data2.tools;
if((!(data12 && typeof data12 == "object" && !Array.isArray(data12))) && (data12 !== null)){
const err28 = {instancePath:instancePath+"/patch/tools",schemaPath:"#/properties/patch/properties/tools/type",keyword:"type",params:{type: schema41.properties.patch.properties.tools.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err28];
}
else {
vErrors.push(err28);
}
errors++;
}
if(data12 && typeof data12 == "object" && !Array.isArray(data12)){
for(const key4 in data12){
let data13 = data12[key4];
if(data13 && typeof data13 == "object" && !Array.isArray(data13)){
if(data13.description === undefined){
const err29 = {instancePath:instancePath+"/patch/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "description"},message:"must have required property '"+"description"+"'"};
if(vErrors === null){
vErrors = [err29];
}
else {
vErrors.push(err29);
}
errors++;
}
if(data13.input_schema === undefined){
const err30 = {instancePath:instancePath+"/patch/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "input_schema"},message:"must have required property '"+"input_schema"+"'"};
if(vErrors === null){
vErrors = [err30];
}
else {
vErrors.push(err30);
}
errors++;
}
if(data13.output_schema === undefined){
const err31 = {instancePath:instancePath+"/patch/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/tools/additionalProperties/required",keyword:"required",params:{missingProperty: "output_schema"},message:"must have required property '"+"output_schema"+"'"};
if(vErrors === null){
vErrors = [err31];
}
else {
vErrors.push(err31);
}
errors++;
}
for(const key5 in data13){
if(!(((key5 === "description") || (key5 === "input_schema")) || (key5 === "output_schema"))){
const err32 = {instancePath:instancePath+"/patch/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/tools/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key5},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err32];
}
else {
vErrors.push(err32);
}
errors++;
}
}
if(data13.description !== undefined){
if(typeof data13.description !== "string"){
const err33 = {instancePath:instancePath+"/patch/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1")+"/description",schemaPath:"#/properties/patch/properties/tools/additionalProperties/properties/description/type",keyword:"type",params:{type: "string"},message:"must be string"};
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
const err34 = {instancePath:instancePath+"/patch/tools/" + key4.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/tools/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
if(data2.children !== undefined){
let data15 = data2.children;
if((!(data15 && typeof data15 == "object" && !Array.isArray(data15))) && (data15 !== null)){
const err35 = {instancePath:instancePath+"/patch/children",schemaPath:"#/properties/patch/properties/children/type",keyword:"type",params:{type: schema41.properties.patch.properties.children.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err35];
}
else {
vErrors.push(err35);
}
errors++;
}
if(data15 && typeof data15 == "object" && !Array.isArray(data15)){
for(const key6 in data15){
let data16 = data15[key6];
if(data16 && typeof data16 == "object" && !Array.isArray(data16)){
if(data16.id === undefined){
const err36 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "id"},message:"must have required property '"+"id"+"'"};
if(vErrors === null){
vErrors = [err36];
}
else {
vErrors.push(err36);
}
errors++;
}
if(data16.revision === undefined){
const err37 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/children/additionalProperties/required",keyword:"required",params:{missingProperty: "revision"},message:"must have required property '"+"revision"+"'"};
if(vErrors === null){
vErrors = [err37];
}
else {
vErrors.push(err37);
}
errors++;
}
for(const key7 in data16){
if(!((key7 === "id") || (key7 === "revision"))){
const err38 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/children/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key7},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err38];
}
else {
vErrors.push(err38);
}
errors++;
}
}
if(data16.id !== undefined){
let data17 = data16.id;
if(typeof data17 === "string"){
if(!pattern0.test(data17)){
const err39 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/patch/properties/children/additionalProperties/properties/id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err39];
}
else {
vErrors.push(err39);
}
errors++;
}
}
else {
const err40 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/id",schemaPath:"#/properties/patch/properties/children/additionalProperties/properties/id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err40];
}
else {
vErrors.push(err40);
}
errors++;
}
}
if(data16.revision !== undefined){
let data18 = data16.revision;
if(typeof data18 === "string"){
if(!pattern2.test(data18)){
const err41 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/patch/properties/children/additionalProperties/properties/revision/pattern",keyword:"pattern",params:{pattern: "^[a-f0-9]{64}$"},message:"must match pattern \""+"^[a-f0-9]{64}$"+"\""};
if(vErrors === null){
vErrors = [err41];
}
else {
vErrors.push(err41);
}
errors++;
}
}
else {
const err42 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1")+"/revision",schemaPath:"#/properties/patch/properties/children/additionalProperties/properties/revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err42];
}
else {
vErrors.push(err42);
}
errors++;
}
}
}
else {
const err43 = {instancePath:instancePath+"/patch/children/" + key6.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/children/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
}
if(data2.hooks !== undefined){
let data19 = data2.hooks;
if((!(data19 && typeof data19 == "object" && !Array.isArray(data19))) && (data19 !== null)){
const err44 = {instancePath:instancePath+"/patch/hooks",schemaPath:"#/properties/patch/properties/hooks/type",keyword:"type",params:{type: schema41.properties.patch.properties.hooks.type},message:"must be object,null"};
if(vErrors === null){
vErrors = [err44];
}
else {
vErrors.push(err44);
}
errors++;
}
if(data19 && typeof data19 == "object" && !Array.isArray(data19)){
for(const key8 in data19){
let data20 = data19[key8];
if(data20 && typeof data20 == "object" && !Array.isArray(data20)){
if(data20.operations === undefined){
const err45 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "operations"},message:"must have required property '"+"operations"+"'"};
if(vErrors === null){
vErrors = [err45];
}
else {
vErrors.push(err45);
}
errors++;
}
if(data20.optional === undefined){
const err46 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "optional"},message:"must have required property '"+"optional"+"'"};
if(vErrors === null){
vErrors = [err46];
}
else {
vErrors.push(err46);
}
errors++;
}
if(data20.timeout_millis === undefined){
const err47 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/hooks/additionalProperties/required",keyword:"required",params:{missingProperty: "timeout_millis"},message:"must have required property '"+"timeout_millis"+"'"};
if(vErrors === null){
vErrors = [err47];
}
else {
vErrors.push(err47);
}
errors++;
}
for(const key9 in data20){
if(!(((key9 === "operations") || (key9 === "optional")) || (key9 === "timeout_millis"))){
const err48 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/hooks/additionalProperties/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key9},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err48];
}
else {
vErrors.push(err48);
}
errors++;
}
}
if(data20.operations !== undefined){
let data21 = data20.operations;
if((data21 !== null) && (!(Array.isArray(data21)))){
const err49 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations",schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/operations/type",keyword:"type",params:{type: schema41.properties.patch.properties.hooks.additionalProperties.properties.operations.type},message:"must be null,array"};
if(vErrors === null){
vErrors = [err49];
}
else {
vErrors.push(err49);
}
errors++;
}
if(Array.isArray(data21)){
const len1 = data21.length;
for(let i1=0; i1<len1; i1++){
let data22 = data21[i1];
if(typeof data22 === "string"){
if(!pattern0.test(data22)){
const err50 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/operations/items/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
if(vErrors === null){
vErrors = [err50];
}
else {
vErrors.push(err50);
}
errors++;
}
}
else {
const err51 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/operations/" + i1,schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/operations/items/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err51];
}
else {
vErrors.push(err51);
}
errors++;
}
}
}
}
if(data20.optional !== undefined){
if(typeof data20.optional !== "boolean"){
const err52 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/optional",schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/optional/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err52];
}
else {
vErrors.push(err52);
}
errors++;
}
}
if(data20.timeout_millis !== undefined){
let data24 = data20.timeout_millis;
if(!((typeof data24 == "number") && (!(data24 % 1) && !isNaN(data24)))){
const err53 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/timeout_millis/type",keyword:"type",params:{type: "integer"},message:"must be integer"};
if(vErrors === null){
vErrors = [err53];
}
else {
vErrors.push(err53);
}
errors++;
}
if(typeof data24 == "number"){
if(data24 > 60000 || isNaN(data24)){
const err54 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/timeout_millis/maximum",keyword:"maximum",params:{comparison: "<=", limit: 60000},message:"must be <= 60000"};
if(vErrors === null){
vErrors = [err54];
}
else {
vErrors.push(err54);
}
errors++;
}
if(data24 < 1 || isNaN(data24)){
const err55 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1")+"/timeout_millis",schemaPath:"#/properties/patch/properties/hooks/additionalProperties/properties/timeout_millis/minimum",keyword:"minimum",params:{comparison: ">=", limit: 1},message:"must be >= 1"};
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
else {
const err56 = {instancePath:instancePath+"/patch/hooks/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),schemaPath:"#/properties/patch/properties/hooks/additionalProperties/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err56];
}
else {
vErrors.push(err56);
}
errors++;
}
}
}
}
if(data2.output !== undefined){
let data25 = data2.output;
if((data25 !== null) && (!(data25 && typeof data25 == "object" && !Array.isArray(data25)))){
const err57 = {instancePath:instancePath+"/patch/output",schemaPath:"#/properties/patch/properties/output/type",keyword:"type",params:{type: schema41.properties.patch.properties.output.type},message:"must be null,object"};
if(vErrors === null){
vErrors = [err57];
}
else {
vErrors.push(err57);
}
errors++;
}
if(data25 && typeof data25 == "object" && !Array.isArray(data25)){
if(data25.schema === undefined){
const err58 = {instancePath:instancePath+"/patch/output",schemaPath:"#/properties/patch/properties/output/required",keyword:"required",params:{missingProperty: "schema"},message:"must have required property '"+"schema"+"'"};
if(vErrors === null){
vErrors = [err58];
}
else {
vErrors.push(err58);
}
errors++;
}
for(const key10 in data25){
if(!(key10 === "schema")){
const err59 = {instancePath:instancePath+"/patch/output",schemaPath:"#/properties/patch/properties/output/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key10},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err59];
}
else {
vErrors.push(err59);
}
errors++;
}
}
}
}
}
else {
const err60 = {instancePath:instancePath+"/patch",schemaPath:"#/properties/patch/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err60];
}
else {
vErrors.push(err60);
}
errors++;
}
}
}
else {
const err61 = {instancePath,schemaPath:"#/type",keyword:"type",params:{type: "object"},message:"must be object"};
if(vErrors === null){
vErrors = [err61];
}
else {
vErrors.push(err61);
}
errors++;
}
validate40.errors = vErrors;
return errors === 0;
}

export const UpdateTreeParams = validate41;
const schema42 = {"type":"object","properties":{"tree_id":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},"expected_revision":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$","format":"counter"},"metadata":{"type":"object","properties":{"title":{"type":["null","string"]},"archived":{"type":"boolean"},"pinned":{"type":"boolean"}},"required":["title","archived","pinned"],"additionalProperties":false}},"$id":"https://whip.dev/protocol/v4/UpdateTreeParams","$schema":"http://json-schema.org/draft-07/schema#","title":"UpdateTreeParams","required":["tree_id","expected_revision","metadata"],"additionalProperties":false};

function validate41(data, {instancePath="", parentData, parentDataProperty, rootData=data}={}){
/*# sourceURL="https://whip.dev/protocol/v4/UpdateTreeParams" */;
let vErrors = null;
let errors = 0;
if(data && typeof data == "object" && !Array.isArray(data)){
if(data.tree_id === undefined){
const err0 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "tree_id"},message:"must have required property '"+"tree_id"+"'"};
if(vErrors === null){
vErrors = [err0];
}
else {
vErrors.push(err0);
}
errors++;
}
if(data.expected_revision === undefined){
const err1 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "expected_revision"},message:"must have required property '"+"expected_revision"+"'"};
if(vErrors === null){
vErrors = [err1];
}
else {
vErrors.push(err1);
}
errors++;
}
if(data.metadata === undefined){
const err2 = {instancePath,schemaPath:"#/required",keyword:"required",params:{missingProperty: "metadata"},message:"must have required property '"+"metadata"+"'"};
if(vErrors === null){
vErrors = [err2];
}
else {
vErrors.push(err2);
}
errors++;
}
for(const key0 in data){
if(!(((key0 === "tree_id") || (key0 === "expected_revision")) || (key0 === "metadata"))){
const err3 = {instancePath,schemaPath:"#/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key0},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err3];
}
else {
vErrors.push(err3);
}
errors++;
}
}
if(data.tree_id !== undefined){
let data0 = data.tree_id;
if(typeof data0 === "string"){
if(!pattern0.test(data0)){
const err4 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/pattern",keyword:"pattern",params:{pattern: "^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"},message:"must match pattern \""+"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$"+"\""};
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
const err5 = {instancePath:instancePath+"/tree_id",schemaPath:"#/properties/tree_id/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err5];
}
else {
vErrors.push(err5);
}
errors++;
}
}
if(data.expected_revision !== undefined){
let data1 = data.expected_revision;
if(typeof data1 === "string"){
if(!pattern11.test(data1)){
const err6 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/pattern",keyword:"pattern",params:{pattern: "^(0|[1-9][0-9]{0,18})$"},message:"must match pattern \""+"^(0|[1-9][0-9]{0,18})$"+"\""};
if(vErrors === null){
vErrors = [err6];
}
else {
vErrors.push(err6);
}
errors++;
}
if(!(formats0.validate(data1))){
const err7 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/format",keyword:"format",params:{format: "counter"},message:"must match format \""+"counter"+"\""};
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
const err8 = {instancePath:instancePath+"/expected_revision",schemaPath:"#/properties/expected_revision/type",keyword:"type",params:{type: "string"},message:"must be string"};
if(vErrors === null){
vErrors = [err8];
}
else {
vErrors.push(err8);
}
errors++;
}
}
if(data.metadata !== undefined){
let data2 = data.metadata;
if(data2 && typeof data2 == "object" && !Array.isArray(data2)){
if(data2.title === undefined){
const err9 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "title"},message:"must have required property '"+"title"+"'"};
if(vErrors === null){
vErrors = [err9];
}
else {
vErrors.push(err9);
}
errors++;
}
if(data2.archived === undefined){
const err10 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "archived"},message:"must have required property '"+"archived"+"'"};
if(vErrors === null){
vErrors = [err10];
}
else {
vErrors.push(err10);
}
errors++;
}
if(data2.pinned === undefined){
const err11 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/required",keyword:"required",params:{missingProperty: "pinned"},message:"must have required property '"+"pinned"+"'"};
if(vErrors === null){
vErrors = [err11];
}
else {
vErrors.push(err11);
}
errors++;
}
for(const key1 in data2){
if(!(((key1 === "title") || (key1 === "archived")) || (key1 === "pinned"))){
const err12 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/additionalProperties",keyword:"additionalProperties",params:{additionalProperty: key1},message:"must NOT have additional properties"};
if(vErrors === null){
vErrors = [err12];
}
else {
vErrors.push(err12);
}
errors++;
}
}
if(data2.title !== undefined){
let data3 = data2.title;
if((data3 !== null) && (typeof data3 !== "string")){
const err13 = {instancePath:instancePath+"/metadata/title",schemaPath:"#/properties/metadata/properties/title/type",keyword:"type",params:{type: schema42.properties.metadata.properties.title.type},message:"must be null,string"};
if(vErrors === null){
vErrors = [err13];
}
else {
vErrors.push(err13);
}
errors++;
}
}
if(data2.archived !== undefined){
if(typeof data2.archived !== "boolean"){
const err14 = {instancePath:instancePath+"/metadata/archived",schemaPath:"#/properties/metadata/properties/archived/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
if(vErrors === null){
vErrors = [err14];
}
else {
vErrors.push(err14);
}
errors++;
}
}
if(data2.pinned !== undefined){
if(typeof data2.pinned !== "boolean"){
const err15 = {instancePath:instancePath+"/metadata/pinned",schemaPath:"#/properties/metadata/properties/pinned/type",keyword:"type",params:{type: "boolean"},message:"must be boolean"};
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
const err16 = {instancePath:instancePath+"/metadata",schemaPath:"#/properties/metadata/type",keyword:"type",params:{type: "object"},message:"must be object"};
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
validate41.errors = vErrors;
return errors === 0;
}
