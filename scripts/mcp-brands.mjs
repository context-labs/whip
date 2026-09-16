#!/usr/bin/env node
// Builds packages/app/src/assets/mcp-brands.json: one small mark per domain in
// mcp-brands.txt, fetched from integrations.sh and stored as a data: URI so the
// import screen draws the common MCP vendors without any network. Run it by
// hand when the list changes:
//
//   node scripts/mcp-brands.mjs
//
// A domain the service does not know is reported and left out; the screen
// falls back to DuckDuckGo at run time, then to a monogram.
import { readFile, writeFile } from 'node:fs/promises';

const list = new URL('../packages/app/src/assets/mcp-brands.txt', import.meta.url);
const out = new URL('../packages/app/src/assets/mcp-brands.json', import.meta.url);
const maxBytes = 12_000;
const types = /^image\/(png|jpeg|webp|svg\+xml)$/;

const domains = (await readFile(list, 'utf8')).split('\n').map(line => line.trim()).filter(line => line && !line.startsWith('#'));
const icons = {};
const missing = [];
for (let i = 0; i < domains.length; i += 8) {
  await Promise.all(domains.slice(i, i + 8).map(async domain => {
    try {
      const response = await fetch(`https://integrations.sh/logo/${domain}`, { headers: { accept: 'image/*' } });
      const type = response.headers.get('content-type')?.split(';')[0]?.trim() ?? '';
      const bytes = Buffer.from(await response.arrayBuffer());
      if (!response.ok || !types.test(type) || bytes.length === 0 || bytes.length > maxBytes) { missing.push(`${domain} (${response.status} ${type} ${bytes.length}B)`); return; }
      icons[domain] = `data:${type};base64,${bytes.toString('base64')}`;
    } catch (error) { missing.push(`${domain} (${error instanceof Error ? error.message : String(error)})`); }
  }));
}
const sorted = Object.fromEntries(Object.entries(icons).sort(([a], [b]) => a.localeCompare(b)));
await writeFile(out, `${JSON.stringify(sorted)}\n`);
console.log(`wrote ${Object.keys(sorted).length} marks to ${out.pathname}`);
if (missing.length) console.warn(`no mark for ${missing.length}:\n  ${missing.join('\n  ')}`);
