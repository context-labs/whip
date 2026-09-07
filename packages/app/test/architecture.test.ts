import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { expect, it } from 'vitest';

function files(path: string): string[] {
  return readdirSync(path, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? files(join(path, entry.name))
      : /\.[cm]?[jt]sx?$/.test(entry.name)
        ? [join(path, entry.name)]
        : [],
  );
}
it('keeps the UI domain-free and the shared application browser-compatible', () => {
  for (const file of files('packages/ui/src')) {
    const imports = readFileSync(file, 'utf8').matchAll(
      /(?:from\s*|import\s*\()\s*['"]([^'"]+)['"]/g,
    );
    for (const [, name] of imports)
      expect(name, file).not.toMatch(
        /^(@whip\/(app|sdk|protocol)|node:|react-router|@tanstack\/react-query)/,
      );
  }
  for (const file of files('packages/app/src')) {
    const source = readFileSync(file, 'utf8');
    expect(source, file).not.toMatch(
      /from\s*['"](?:node:|@base-ui\/|@whip\/sdk\/node)/,
    );
    expect(source, file).not.toMatch(
      /new\s+(?:WebSocket|Worker)\s*\(|\bfetch\s*\(/,
    );
  }
});
