/** Normalize only the declaration input. The wire schema and validators retain
 * their original constraints. json-schema-to-typescript otherwise drops sibling
 * object properties on nested oneOf/anyOf schemas. */
export function declarationSchema(source) {
  const schema = structuredClone(source);
  function visit(value, root = false) {
    if (!value || typeof value !== 'object') return value;
    for (const key of ['properties', 'patternProperties', '$defs', 'definitions', 'dependentSchemas']) {
      if (value[key]) value[key] = Object.fromEntries(Object.entries(value[key]).map(([name, child]) => [name, visit(child)]));
    }
    for (const key of ['items', 'additionalProperties', 'not', 'if', 'then', 'else', 'contains', 'propertyNames']) {
      if (value[key] !== undefined) value[key] = Array.isArray(value[key]) ? value[key].map(child => visit(child)) : visit(value[key]);
    }
    for (const key of ['allOf', 'anyOf', 'oneOf', 'prefixItems']) {
      if (value[key]) value[key] = value[key].map(child => visit(child));
    }
    // if/then/else clauses refine existing DTO fields at validation time. The
    // declaration compiler cannot express them and invents an open index map,
    // which destroys discriminated-union narrowing in otherwise closed DTOs.
    if (value.allOf) {
      value.allOf = value.allOf.filter(child => !child || typeof child !== 'object' || Object.keys(child).some(key => !['if', 'then', 'else'].includes(key)));
      if (!value.allOf.length) delete value.allOf;
    }
    // JSON's non-null values include primitives. The compiler incorrectly treats
    // this exact constraint as an object map; TS {} is the non-null value type.
    if (Object.keys(value).length === 1 && value.not && Object.keys(value.not).length === 1 && value.not.type === 'null') return { tsType: '{}' };
    const choices = value.oneOf ? 'oneOf' : value.anyOf ? 'anyOf' : undefined;
    if (choices && value.properties && !root) {
      const branches = value[choices]; delete value[choices];
      return { allOf: [value, { [choices]: branches }] };
    }
    return value;
  }
  return visit(schema, true);
}
