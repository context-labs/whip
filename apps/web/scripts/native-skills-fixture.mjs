import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { deadline, startFixture } from './native-fixture.mjs';

/** Explicit disposable native publication + builtin selection. Publication and
 * human draft previews grant no session permission. Existing sessions still need
 * a separately requested, exact standing grant before reading those sources. */
export async function startSkillsFixture() {
  const fixture = await startFixture({ lifetimeMs: 600000 });
  try {
    const cwd = join(fixture.directory, 'project');
    const skills = ['accept-alpha', 'accept-alpine', 'accept-beta', ...Array.from({ length: 96 }, (_, index) => `accept-long-${String(index).padStart(2, '0')}`), 'accept-zebra'];
    const globalSkills = skills.map(skill => skill.replace('accept-', 'global-'));
    const roots = { whip: join(fixture.directory, 'home/.whipcode/skills'), agents: join(fixture.directory, 'home/.agents/skills') };
    for (const name of [...skills, ...globalSkills]) {
      const directory = join(name === 'global-beta' ? roots.agents : name.startsWith('global-') ? roots.whip : join(cwd, '.agents/skills'), name);
      await mkdir(directory, { recursive: true });
      await writeFile(join(directory, 'SKILL.md'), `---\nname: ${name}\ndescription: Synthetic ${name} acceptance skill\n---\nSynthetic fixture instructions; do not access any external service.\n`);
    }
    const client = await fixture.connect(`skills-${randomUUID()}`);
    let published = await client.hosts.skillRoots(deadline());
    for (const [id, path] of Object.entries(roots)) published = await client.hosts.publishSkillRoot(published.revision, id, path, deadline());
    await client.hosts.setDefaultSkillRoots(published.revision, Object.keys(roots), deadline());
    const ref = client.builtins.find(ref => ref.id === 'coding'); assert(ref);
    const definition = { ref };
    const configured = await client.listProviders(deadline()), route = configured.routes.find(route => route.id === 'provider'); assert(route);
    const declaration = { kind: route.kind, base_url: route.base_url, credential: { source: 'none', environment: '', file: '', command: null }, models: route.models };
    const cleared = await client.setProviderDefaults({ revision: configured.revision, defaults: { selection: null, settings: null } }, deadline());
    await client.removeProvider({ revision: cleared.revision, provider: 'provider', replacement: null }, deadline());
    const inventory = await client.listProviders(deadline()); assert.equal(inventory.defaults, null); assert.deepEqual(inventory.routes, []);
    return { fixture, client, definition, cwd, skills, globalSkills, projectCatalog: [...skills, ...globalSkills].sort(),
      async configureProvider() {
        const current = await client.listProviders(deadline());
        const created = await client.createProvider({ revision: current.revision, provider: 'provider', keep_credential: false, key: null, declaration }, deadline());
        await client.setProviderDefaults({ revision: created.revision, defaults: { selection: configured.defaults, settings: null } }, deadline());
        await client.refreshProviderCatalog('provider', deadline());
      },
      async grant(sessionID) {
        const session = await client.session(sessionID).get(deadline()); assert.equal(session.working_directory, cwd); assert.equal(session.parent_id, null);
        for (const [capability, resource] of [['files.read', cwd], ['skills.read', 'whip'], ['skills.read', 'agents']])
          await client.call('grants.create', { id: randomUUID(), session_id: sessionID, capability, resource }, deadline());
      },
    };
  } catch (error) { await fixture.close(); throw error; }
}
