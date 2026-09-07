import { readFileSync } from 'node:fs';
import { expect, it } from 'vitest';

it('classifies every registered operation in the web workflow inventory', () => {
  const manifest = JSON.parse(readFileSync('packages/protocol/schema/manifest.json', 'utf8')) as {
    operations: { surface: string; name: string }[];
  };
  const inventory = readFileSync('.ai-docs/plans/web-app/WORKFLOW-INVENTORY.md', 'utf8');
  const rows = [
    ...inventory.matchAll(
      /^\| `(rpc|runtime):([^`]+)` \| (Web|Internal|SDK-only|Deferred) \| .+ \|$/gm,
    ),
  ];
  const names = rows.map((row) => `${row[1]}:${row[2]}`);
  expect(new Set(names).size).toBe(names.length);
  expect(names.sort()).toEqual(
    manifest.operations.map((operation) => `${operation.surface}:${operation.name}`).sort(),
  );
});
