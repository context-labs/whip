import { readFile, readdir, mkdir, writeFile } from 'node:fs/promises';
import { compile } from 'json-schema-to-typescript';

const check = process.argv.includes('--check');
const schemas = {};
const filenames = (await readdir('schema')).filter(name => !['manifest.json', 'fixtures.json', 'signing-fixture.json'].includes(name)).sort();
for (const filename of filenames) {
  schemas[filename.slice(0, -5)] = JSON.parse(await readFile(`schema/${filename}`, 'utf8'));
}
const manifest = JSON.parse(await readFile('schema/manifest.json', 'utf8'));
let declarations = '// Generated from Go wire types. Run npm run generate.\n';
for (const [name, schema] of Object.entries(schemas)) {
  declarations += await compile(schema, name, { bannerComment: '', unreachableDefinitions: true });
  declarations += '\n';
}
declarations += 'export interface ContractTypes {\n';
for (const name of Object.keys(schemas)) declarations += `  ${name}: ${name};\n`;
declarations += '}\nexport interface EventPayloadTypes {\n';
for (const [kind, name] of Object.entries(manifest.event_payloads)) declarations += `  ${JSON.stringify(kind)}: ${name} | ContentEventPayload;\n`;
declarations += `}
export type TypedRootEvent = { [K in keyof EventPayloadTypes]: { kind: K; payload: EventPayloadTypes[K] } }[keyof EventPayloadTypes];
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown): asserts value is ContractTypes[T];
export declare const manifest: {
  major: number; minor: number;
  operations: readonly { name: string; surface: string; execution: string; permission: string; sensitive?: boolean; params_type: keyof ContractTypes; result_type: keyof ContractTypes }[];
  events: Readonly<Record<string, keyof ContractTypes>>;
 event_payloads: Readonly<Record<string, keyof ContractTypes>>;
};
`;
const runtime = `// Generated from Go wire types. Run npm run generate.
import Ajv from 'ajv';
import schemas from './schemas.json' with { type: 'json' };
export const manifest = ${JSON.stringify(manifest, null, 2)};
const ajv = new Ajv({ allErrors: true, strict: false, validateFormats: true });
ajv.addFormat('int64', { type: 'string', validate: value => {
  if (!/^-?(0|[1-9][0-9]*)$/.test(value) || value.length > 20) return false;
  const number = BigInt(value);
  return number >= -9223372036854775808n && number <= 9223372036854775807n;
}});
const validators = new Map();
export function validate(type, value) {
  if (!Object.hasOwn(schemas, type)) throw new TypeError('Unknown WHIP contract type: ' + type);
  let validator = validators.get(type);
  if (!validator) { validator = ajv.compile(schemas[type]); validators.set(type, validator); }
  return validator(value);
}
export function assertValid(type, value) {
  if (!validate(type, value)) throw new TypeError('Invalid WHIP ' + type + ': ' + ajv.errorsText(validators.get(type).errors));
}
`;
const files = {
  'index.d.ts': declarations,
  'index.js': runtime,
  'schemas.json': JSON.stringify(schemas, null, 2) + '\n',
};
if (!check) await mkdir('generated', { recursive: true });
for (const [name, expected] of Object.entries(files)) {
  if (check) {
    const actual = await readFile(`generated/${name}`, 'utf8');
    if (actual !== expected) throw new Error(`Generated contract drift: generated/${name}`);
  } else await writeFile(`generated/${name}`, expected);
}
