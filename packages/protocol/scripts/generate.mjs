import { readFile, readdir, mkdir, writeFile } from 'node:fs/promises';
import { compile } from 'json-schema-to-typescript';
import Ajv from 'ajv';
import standaloneCode from 'ajv/dist/standalone/index.js';
import { _ } from 'ajv/dist/compile/codegen/index.js';

const check = process.argv.includes('--check');
const schemas = {};
const filenames = (await readdir('schema')).filter(name => !['manifest.json', 'fixtures.json', 'signing-fixture.json'].includes(name)).sort();
for (const filename of filenames) {
  schemas[filename.slice(0, -5)] = JSON.parse(await readFile(`schema/${filename}`, 'utf8'));
}
const manifest = JSON.parse(await readFile('schema/manifest.json', 'utf8'));
const operations = surface => manifest.operations.filter(operation => operation.surface === surface);
let declarations = '// Generated from Go wire types. Run npm run generate.\n';
for (const [name, schema] of Object.entries(schemas)) {
  declarations += await compile(schema, name, { bannerComment: '', unreachableDefinitions: true });
  declarations += '\n';
}
declarations += 'export interface ContractTypes {\n';
for (const name of Object.keys(schemas)) declarations += `  ${name}: ${name};\n`;
declarations += '}\nexport interface EventPayloadTypes {\n';
for (const [kind, name] of Object.entries(manifest.event_payloads)) declarations += `  ${JSON.stringify(kind)}: ${name} | ContentEventPayload;\n`;
declarations += '}\n';
for (const [surface, name] of [['rpc', 'RpcMethods'], ['runtime', 'RuntimeOperations']]) {
  declarations += `export interface ${name} {\n`;
  for (const operation of operations(surface)) {
    declarations += `  ${JSON.stringify(operation.name)}: { params: ${operation.params_type}; result: ${operation.result_type}; execution: ${JSON.stringify(operation.execution)}; permission: ${JSON.stringify(operation.permission)}; sensitive: ${operation.sensitive ?? false} };\n`;
  }
  declarations += '}\n';
}
declarations += `
export type RpcMethod = keyof RpcMethods;
export type RuntimeOperation = keyof RuntimeOperations;
export type RuntimeOperationOf<E extends RuntimeOperations[RuntimeOperation]['execution']> = {
  [K in RuntimeOperation]: RuntimeOperations[K]['execution'] extends E ? K : never
}[RuntimeOperation];
export type QueryOperation = RuntimeOperationOf<'query'>;
export type CommandOperation = RuntimeOperationOf<'command'>;
export type EphemeralOperation = RuntimeOperationOf<'ephemeral'>;
export interface OperationMetadata {
  readonly name: string;
  readonly surface: 'rpc' | 'runtime';
  readonly execution: 'query' | 'command' | 'ephemeral' | 'subscription' | 'lifecycle';
  readonly permission: string;
  readonly sensitive: boolean;
  readonly params_type: keyof ContractTypes;
  readonly result_type: keyof ContractTypes;
}
export declare const rpcOperations: Readonly<{ [K in RpcMethod]: OperationMetadata & { readonly name: K; readonly execution: RpcMethods[K]['execution']; readonly permission: RpcMethods[K]['permission']; readonly sensitive: RpcMethods[K]['sensitive'] } }>;
export declare const runtimeOperations: Readonly<{ [K in RuntimeOperation]: OperationMetadata & { readonly name: K; readonly execution: RuntimeOperations[K]['execution']; readonly permission: RuntimeOperations[K]['permission']; readonly sensitive: RuntimeOperations[K]['sensitive'] } }>;
export type RootEventEnvelope = Omit<EventNotification['event'], 'kind' | 'payload'>;
export type TypedRootEvent = { [K in keyof EventPayloadTypes]: RootEventEnvelope & { unknown?: false; kind: K; payload: EventPayloadTypes[K] } }[keyof EventPayloadTypes];
export type UnknownRootEvent = RootEventEnvelope & { unknown: true; kind: string; payload: unknown };
export type RootEvent = TypedRootEvent | UnknownRootEvent;
export type TypedEventNotification = { event: RootEvent };
export type ValidationMode = 'request' | 'response';
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown, mode?: ValidationMode): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown, mode?: ValidationMode): asserts value is ContractTypes[T];
export declare const manifest: {
  major: number; minor: number;
  operations: readonly (Omit<OperationMetadata, 'sensitive'> & { readonly sensitive?: boolean })[];
  events: Readonly<Record<string, keyof ContractTypes>>;
  event_payloads: Readonly<Record<string, keyof ContractTypes>>;
};
`;

// This exact expression is embedded into the generated module. No format helper,
// Ajv runtime compiler, or dynamic function construction is needed by consumers.
const int64Code = _`{int64: {type: 'string', validate: value => {
  if (!/^-?(0|[1-9][0-9]*)$/.test(value) || value.length > 20) return false;
  const number = BigInt(value);
  return number >= -9223372036854775808n && number <= 9223372036854775807n;
}}}`;
const int64 = value => /^-?(0|[1-9][0-9]*)$/.test(value) && value.length <= 20 && BigInt(value) >= -9223372036854775808n && BigInt(value) <= 9223372036854775807n;
function allowAdditions(value) {
  if (Array.isArray(value)) return value.map(allowAdditions);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key,
    key === 'additionalProperties' && child === false ? true : allowAdditions(child),
  ]));
}
function validators(mode) {
  const ajv = new Ajv({ allErrors: true, strict: false, validateFormats: true,
    code: { source: true, esm: true, lines: true, formats: int64Code },
  });
  ajv.addFormat('int64', { type: 'string', validate: int64 });
  const exports = {};
  for (const [name, schema] of Object.entries(schemas)) {
    ajv.addSchema(mode === 'response' ? allowAdditions(schema) : schema);
    exports[name] = schema.$id;
  }
  return '// Generated standalone validators. Run npm run generate.\n' + standaloneCode(ajv, exports);
}
const runtime = `// Generated from Go wire types. Run npm run generate.
import * as requests from './request-validators.js';
import * as responses from './response-validators.js';
export const manifest = ${JSON.stringify(manifest, null, 2)};
function operationLookup(surface) {
  return Object.freeze(Object.fromEntries(manifest.operations.filter(operation => operation.surface === surface).map(operation =>
    [operation.name, Object.freeze({ ...operation, sensitive: operation.sensitive ?? false })],
  )));
}
export const rpcOperations = operationLookup('rpc');
export const runtimeOperations = operationLookup('runtime');
function validatorFor(type, mode) {
  if (mode !== 'request' && mode !== 'response') throw new TypeError('Unknown WHIP validation mode: ' + mode);
  const validators = mode === 'response' ? responses : requests;
  if (!Object.hasOwn(validators, type)) throw new TypeError('Unknown WHIP contract type: ' + type);
  return validators[type];
}
export function validate(type, value, mode = 'request') {
  return validatorFor(type, mode)(value);
}
export function assertValid(type, value, mode = 'request') {
  const validator = validatorFor(type, mode);
  if (!validator(value)) {
    const details = validator.errors.map(error => (error.instancePath || '/') + ' ' + error.message).join('; ');
    throw new TypeError('Invalid WHIP ' + type + ': ' + details);
  }
}
`;
const files = {
  'index.d.ts': declarations,
  'index.js': runtime,
  'request-validators.js': validators('request'),
  'response-validators.js': validators('response'),
};
if (!check) await mkdir('generated', { recursive: true });
for (const [name, expected] of Object.entries(files)) {
  if (check) {
    const actual = await readFile(`generated/${name}`, 'utf8');
    if (actual !== expected) throw new Error(`Generated contract drift: generated/${name}`);
  } else await writeFile(`generated/${name}`, expected);
}
