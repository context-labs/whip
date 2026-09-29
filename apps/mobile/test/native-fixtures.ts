import { assertValid, type ContractTypes } from '@whip/protocol';
import fixtures from '../../../packages/protocol/schema/fixtures.json';
/** Real native contract examples, checked before a UI test changes its scope. */
export function nativeFixture<T extends keyof ContractTypes>(type: T): ContractTypes[T] {
  const value: unknown = structuredClone(fixtures.find(item => item.type === type && item.valid)?.value);
  assertValid(type, value);
  return value;
}
