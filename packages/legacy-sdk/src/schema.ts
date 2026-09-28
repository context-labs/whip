import { object } from './util.js';

/**
 * Standard Schema and Standard JSON Schema, mirrored as types so the SDK adds
 * no dependency on any schema library. Zod 4.2+, ArkType 2.1.28+, and Valibot
 * (through @valibot/to-json-schema) implement both. https://standardschema.dev
 */
export interface StandardIssue {
  readonly message: string;
  readonly path?: ReadonlyArray<PropertyKey | { readonly key: PropertyKey }> | undefined;
}
export type StandardResult<Output> =
  | { readonly value: Output; readonly issues?: undefined }
  | { readonly issues: ReadonlyArray<StandardIssue> };
export interface StandardTypes<Input, Output> { readonly input: Input; readonly output: Output }
export interface StandardSchemaProps<Input = unknown, Output = Input> {
  readonly version: 1;
  readonly vendor: string;
  readonly validate: (value: unknown) => StandardResult<Output> | Promise<StandardResult<Output>>;
  readonly types?: StandardTypes<Input, Output> | undefined;
}
export type JsonSchemaTarget = 'draft-2020-12' | 'draft-07' | 'openapi-3.0' | (string & {});
export interface JsonSchemaOptions { readonly target: JsonSchemaTarget; readonly libraryOptions?: Record<string, unknown> | undefined }
export interface JsonSchemaConverter {
  readonly input: (options: JsonSchemaOptions) => Record<string, unknown>;
  readonly output: (options: JsonSchemaOptions) => Record<string, unknown>;
}
export interface StandardJSONSchemaProps<Input = unknown, Output = Input> {
  readonly version: 1;
  readonly vendor: string;
  readonly types?: StandardTypes<Input, Output> | undefined;
  readonly jsonSchema: JsonSchemaConverter;
}
export interface StandardSchemaV1<Input = unknown, Output = Input> { readonly '~standard': StandardSchemaProps<Input, Output> }
export interface StandardJSONSchemaV1<Input = unknown, Output = Input> { readonly '~standard': StandardJSONSchemaProps<Input, Output> }
/** A schema that both validates and serializes: what tool() accepts for typed inputs and outputs. */
export interface StandardSchemaWithJSON<Input = unknown, Output = Input> {
  readonly '~standard': StandardSchemaProps<Input, Output> & StandardJSONSchemaProps<Input, Output>;
}

/** Raw JSON Schema, sent as written and typed unknown. */
export type JsonSchema = Record<string, unknown>;
/** Anything tool() and defineAgent() accept as a schema. */
export type Schema = StandardSchemaWithJSON | JsonSchema;

type Typed = { readonly '~standard': { readonly types?: StandardTypes<unknown, unknown> | undefined } };
/** The library's input type for a Standard Schema; unknown for raw JSON Schema. */
export type InferInput<S> = S extends Typed ? NonNullable<S['~standard']['types']>['input'] : unknown;
/** The library's output type for a Standard Schema; unknown for raw JSON Schema. */
export type InferOutput<S> = S extends Typed ? NonNullable<S['~standard']['types']>['output'] : unknown;

/**
 * The draft derived schemas target. It is the spec's first-class target and
 * the daemon's validator accepts it beside draft-07, which hand-written raw
 * schemas may still use.
 */
export const JSON_SCHEMA_TARGET: JsonSchemaTarget = 'draft-2020-12';

export function isStandardSchema(schema: unknown): schema is StandardSchemaWithJSON {
  if (!object(schema)) return false;
  const props = (schema as { '~standard'?: unknown })['~standard'];
  return object(props) && typeof props.validate === 'function' && object(props.jsonSchema) && typeof props.jsonSchema.input === 'function';
}

/**
 * Derive the wire JSON Schema for a tool input or output. Raw JSON Schema
 * passes through compacted (undefined members dropped). A library that cannot
 * produce the target is a definition-time error: the document's bytes are its
 * revision, so there is no silent fallback to another draft.
 */
export function toJsonSchema(schema: Schema, io: 'input' | 'output', subject: string): JsonSchema {
  if (isStandardSchema(schema)) {
    let derived: unknown;
    try { derived = schema['~standard'].jsonSchema[io]({ target: JSON_SCHEMA_TARGET }); }
    catch (error) { throw new TypeError(`${subject}: ${schema['~standard'].vendor} schema cannot produce ${JSON_SCHEMA_TARGET} JSON Schema`, { cause: error }); }
    if (!object(derived)) throw new TypeError(`${subject}: ${schema['~standard'].vendor} schema produced no JSON Schema object`);
    return JSON.parse(JSON.stringify(derived)) as JsonSchema;
  }
  if (!object(schema)) throw new TypeError(`${subject}: schema must be a Standard JSON Schema or a JSON Schema object`);
  return JSON.parse(JSON.stringify(schema)) as JsonSchema;
}

/** Whether a JSON Schema describes an object, which keyword arguments require. A schema without a type is left to the daemon. */
export function describesObject(schema: JsonSchema): boolean {
  const type = schema.type;
  if (type === undefined) return true;
  return type === 'object' || (Array.isArray(type) && type.includes('object'));
}

/** Run a Standard Schema's own validation. Raw JSON Schema has none and passes the value through. */
export async function validateWith(schema: Schema | undefined, value: unknown): Promise<StandardResult<unknown>> {
  if (!isStandardSchema(schema)) return { value };
  return schema['~standard'].validate(value);
}

/** One bounded line for an error message. */
export function formatIssues(issues: ReadonlyArray<StandardIssue>, limit = 5): string {
  const lines = issues.slice(0, limit).map(issue => {
    const path = (issue.path ?? []).map(segment => String(typeof segment === 'object' ? segment.key : segment)).join('.');
    return path ? `${path}: ${issue.message}` : issue.message;
  });
  if (issues.length > limit) lines.push(`and ${issues.length - limit} more`);
  return lines.join('; ');
}
