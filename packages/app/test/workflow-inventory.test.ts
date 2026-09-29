// @vitest-environment node
import { readFileSync, statSync } from 'node:fs';
import { expect, it } from 'vitest';

const root = new URL('../../../', import.meta.url);
const inventoryURL = new URL('docs/native-web-workflows.md', root);

it('classifies every native operation once with current source ownership', () => {
  const manifest = JSON.parse(readFileSync(new URL('packages/protocol/schema/manifest.json', root), 'utf8')) as {
    operations: { name: string }[];
  };
  const inventory = readFileSync(inventoryURL, 'utf8');
  const rows = [...inventory.matchAll(/^\| `([^`]+)` \| (Web|Internal|SDK-only) \| (.+) \|$/gm)];
  const names = rows.map(row => row[1]);
  expect(new Set(names).size).toBe(names.length);
  expect(names.sort()).toEqual(manifest.operations.map(operation => operation.name).sort());
  for (const [, name, classification, owner] of rows) {
    const links = [...owner!.matchAll(/\]\(\.\.\/((?:packages|apps|internal)\/[^)#]+)(?:#[^)]*)?\)/g)];
    expect(links.length, `${name} needs a concrete source owner`).toBeGreaterThan(0);
    for (const [, path] of links) {
      expect(path, `${name} must name a native owner`).not.toContain('legacy-');
      expect(statSync(new URL(path!, root)).isFile(), `${name}: ${path}`).toBe(true);
    }
    if (classification === 'Web') {
      expect(links.some(([, path]) => path!.startsWith('packages/app/src/') || path!.startsWith('apps/web/src/')),
        `${name} needs its renderer workflow owner`).toBe(true);
    }
  }
});
