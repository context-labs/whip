// npm owns the dependency graph and CycloneDX format. Add the modules linked
// into the Go executable and collect upstream notice files from local packages.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { lstat, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
const command = async (name, args) => (await exec(name, args, { cwd: repositoryRoot, maxBuffer: 32 << 20, timeout: 120_000 })).stdout;
export async function dependencyNotices(binary, directory, version) {
  const args = ['sbom', '--sbom-format', 'cyclonedx', ...['desktop', 'web', 'app', 'ui', 'sdk'].flatMap(name => ['--workspace', `@whip/${name}`])];
  const production = JSON.parse(await command('npm', [...args, '--omit=dev']));
  const full = JSON.parse(await command('npm', args));
  const selected = new Set(production.components.map(component => component['bom-ref']));
  // These ship in the app but npm marks them dev because they are hoisted from
  // workspace peer/build declarations. Follow their actual transitive graph.
  const dependencies = new Map(full.dependencies.map(value => [value.ref, value.dependsOn || []]));
  const visit = ref => { for (const dependency of dependencies.get(ref) || []) if (!selected.has(dependency)) { selected.add(dependency); visit(dependency); } };
  for (const component of full.components) if (['react', 'react-dom', 'electron'].includes(component.name)) {
    selected.add(component['bom-ref']);
    // Electron's npm dependencies download/unpack its binary; they do not ship.
    if (component.name !== 'electron') visit(component['bom-ref']);
  }
  const components = full.components.filter(component => selected.has(component['bom-ref']));
  for (const name of ['react', 'react-dom', 'electron']) assert(components.some(component => component.name === name), `Missing shipped dependency: ${name}`);
  const notices = ['Whip third-party notices\n\nIncludes runtime dependency graphs and their supporting packages; bundling may remove unused code. Electron/Chromium notices are also included separately.'];
  const lock = JSON.parse(await readFile(path.join(repositoryRoot, 'package-lock.json'), 'utf8'));
  const missing = [];
  async function collect(label, root, component) {
    const names = (await readdir(root)).filter(name => /^(license|copying|notice|copyright|ofl)(\.|$|-)/i.test(name)).sort();
    if (!names.length) {
      assert(component?.licenses?.length, `Missing upstream license declaration: ${label}`);
      missing.push(label);
      notices.push(`\n${label}\nThe publisher supplied this license declaration without a standalone license file:\n${JSON.stringify(component.licenses)}\n${JSON.stringify(component.externalReferences || [])}\n`);
      return;
    }
    for (const name of names) {
      const file = path.join(root, name); const stat = await lstat(file);
      if (!stat.isFile()) continue;
      assert(stat.size <= 2 << 20, `Notice exceeds its limit: ${label}`);
      notices.push(`\n${'='.repeat(72)}\n${label} — ${name}\n${'='.repeat(72)}\n\n${await readFile(file, 'utf8')}`);
    }
  }
  for (const component of components) {
    if (component.properties?.some(property => property.name === 'cdx:npm:package:private' && property.value === 'true')) continue;
    const relative = Object.keys(lock.packages).find(name => name.endsWith(`node_modules/${component.name}`) && lock.packages[name].version === component.version);
    assert(relative, `Cannot locate installed npm package: ${component['bom-ref']}`);
    await collect(component['bom-ref'], path.join(repositoryRoot, relative), component);
  }
  const modules = (await command('go', ['version', '-m', binary])).split('\n').filter(line => line.startsWith('\tdep\t')).map(line => {
    const [, , name, version, sum] = line.split('\t'); return { name, version, sum };
  });
  for (const module of modules) {
    const metadata = JSON.parse(await command('go', ['list', '-m', '-json', module.name]));
    assert.equal(metadata.Version, module.version, `Linked Go dependency differs: ${module.name}`);
    assert(metadata.Dir && !metadata.Replace, `Unverifiable Go dependency: ${module.name}`);
    await collect(`${module.name}@${module.version}`, metadata.Dir);
    components.push({ type: 'library', name: module.name, version: module.version,
      'bom-ref': `pkg:golang/${module.name}@${module.version}`, purl: `pkg:golang/${module.name}@${module.version}`,
      properties: [{ name: 'go:module:sum', value: module.sum }] });
  }
  await collect('OpenCode provider logos (MIT)', path.join(repositoryRoot, 'packages/app/src/settings/assets'));
  // The toolchain's runtime and standard library are linked too.
  await collect('Go runtime and standard library', (await command('go', ['env', 'GOROOT'])).trim());
  full.metadata.properties = [{ name: 'whip:license-declarations-without-files', value: missing.join(', ') }];
  full.components = components;
  const root = full.metadata.component['bom-ref'];
  full.dependencies = full.dependencies.filter(item => selected.has(item.ref)).map(item => ({ ...item,
    dependsOn: (item.dependsOn || []).filter(ref => selected.has(ref)) }));
  full.dependencies.push({ ref: root, dependsOn: components.map(component => component['bom-ref']) });
  full.metadata.component.name = 'Whip'; full.metadata.component.version = version;
  full.metadata.component.description = 'Whip desktop runtime dependency inventory, including transitive support packages and linked Go modules';
  await writeFile(path.join(directory, 'sbom.cdx.json'), JSON.stringify(full, null, 2) + '\n');
  const text = notices.join('\n'); assert(Buffer.byteLength(text) <= 32 << 20, 'Combined notices exceed the package limit');
  await writeFile(path.join(directory, 'THIRD_PARTY_NOTICES.txt'), text + '\n');
}
