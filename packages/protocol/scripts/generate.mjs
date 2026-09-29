import { readFile, readdir, mkdir, writeFile } from 'node:fs/promises';
import { compile } from 'json-schema-to-typescript';
import Ajv from 'ajv';
import standaloneCode from 'ajv/dist/standalone/index.js';
import equalRuntime from 'ajv/dist/runtime/equal.js';
import { _ } from 'ajv/dist/compile/codegen/index.js';

const check = process.argv.includes('--check');
const manifest = JSON.parse(await readFile('schema/manifest.json', 'utf8'));
const schemas = {};
for (const file of (await readdir('schema')).sort()) {
  if (file === 'manifest.json' || file === 'fixtures.json') continue;
  schemas[file.slice(0, -5)] = JSON.parse(await readFile('schema/' + file, 'utf8'));
}
let declarations = '// Generated from Go DTOs. Run npm run generate.\n';
for (const [name, schema] of Object.entries(schemas)) {
  // Nested bounded collections otherwise expand into thousands of unioned
  // tuples. Go and standalone validators retain every exact collection bound.
  declarations += await compile(schema, name, { bannerComment: '', maxItems: 4 });
}
declarations += '\nexport interface ContractTypes {\n';
for (const name of Object.keys(schemas)) declarations += '  ' + name + ': ' + name + ';\n';
declarations += '}\nexport interface Operations {\n';
for (const op of manifest.operations) {
  declarations += '  ' + JSON.stringify(op.name) + ': { params: ' + op.params + '; result: ' + op.result + ' };\n';
}
declarations += [
  '}',
  'export declare const manifest: { major: 4; minor: number; operations: readonly { name: keyof Operations; params: keyof ContractTypes; result: keyof ContractTypes }[] };',
  'export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown): value is ContractTypes[T];',
  'export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown): asserts value is ContractTypes[T];',
  '',
].join('\n');

// Embed browser-native format checks; consumers never compile code. Account
// timestamps retain nanoseconds as strings; Date is used only for calendar validity.
function accountTime(value) {
  if (!/^(?!0000)[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:[.][0-9]{0,8}[1-9])?Z$/.test(value)) return false;
  const time = Date.parse(value);
  return Number.isFinite(time) && new Date(time).toISOString().slice(0, 19) === value.slice(0, 19);
}
const formats = _`{counter: {type: 'string', validate: value =>
  /^(0|[1-9][0-9]{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n}, 'account-time': {type: 'string', validate: ${_([accountTime.toString()])}}}`;
const ajv = new Ajv({ strict: false, allErrors: true, code: { source: true, esm: true, lines: true, formats } });
ajv.addFormat('counter', { type: 'string', validate: value => /^(0|[1-9][0-9]{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n });
ajv.addFormat('account-time', { type: 'string', validate: accountTime });
const exports = {};
for (const [name, schema] of Object.entries(schemas)) {
  ajv.addSchema(schema);
  exports[name] = schema.$id;
}
const runtime = [
  '// Generated from Go DTOs. Run npm run generate.',
  "import * as validators from './validators.js';",
  'export const manifest = ' + JSON.stringify(manifest, null, 2) + ';',
  'export function validate(type, value) {',
  "  if (!Object.hasOwn(validators, type)) throw new TypeError('Unknown contract type: ' + type);",
  '  return validators[type](value);',
  '}',
  'export function assertValid(type, value) {',
  "  if (!validate(type, value)) throw new TypeError('Invalid ' + type + ': ' + JSON.stringify(validators[type].errors));",
  '}',
  '',
].join('\n');
// Ajv emits a CommonJS runtime reference for string-length bounds even in ESM
// standalone mode. Embed this browser-native helper so clients stay dependency
// free and correctly count surrogate pairs. Reject any new unresolved helper.
function unicodeLength(value) {
  let count = 0;
  for (const _ of value) count++;
  return count;
}
// Bounded tool-image arrays use structural uniqueness. Embed the exact pinned
// Ajv equality implementation; do not add a browser dependency or dynamic code.
const validators = standaloneCode(ajv, exports)
  .replaceAll('require("ajv/dist/runtime/ucs2length").default', unicodeLength.toString())
  .replaceAll('require("ajv/dist/runtime/equal").default', equalRuntime.default.toString());
if (/\brequire\s*\(/.test(validators)) throw new Error('Generated validator contains an unresolved runtime dependency');
const files = { 'index.d.ts': declarations, 'index.js': runtime, 'validators.js': validators };
if (!check) await mkdir('generated', { recursive: true });
for (const [name, expected] of Object.entries(files)) {
  const path = 'generated/' + name;
  if (check) {
    if (await readFile(path, 'utf8') !== expected) throw new Error('Generated contract drift: ' + path);
  } else await writeFile(path, expected);
}
for (const name of await readdir('generated')) {
  if (!Object.hasOwn(files, name)) throw new Error('Stale generated contract: ' + name);
}
