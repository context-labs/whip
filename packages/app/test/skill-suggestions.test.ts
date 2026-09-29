import { expect, it } from 'vitest';
import { readSkillSuggestions } from '../src/skill-suggestions';
import { providerFixture } from './provider-fixture';

const definition = { id: 'coding', revision: 'a'.repeat(64) };
const skill = (name: string, disabled = false) => ({ name, description: `Description ${name}`, disabled, source: { kind: 'skill_metadata', scope: 'project', root_id: 'project', path: `/project/${name}/SKILL.md`, bytes: '20', sha256: 'a'.repeat(64) } });

it('uses explicit global/project host scopes and exact immutable definition references', async () => {
  const f = await providerFixture();
  f.data.handlers['host.skills.complete'] = () => ({ candidates: [{ text: '$native', description: 'Metadata' }], truncated: false });
  const signal = new AbortController().signal;
  await readSkillSuggestions(f.client, { cwd: '', definition: null }, '', 1024, signal);
  await readSkillSuggestions(f.client, { cwd: '/project', definition }, 'na', 32, signal);
  expect(f.calls.filter(call => call.method === 'host.skills.complete').map(call => call.params)).toEqual([
    { scope: 'global', cwd: '', definition: null, prefix: '', limit: 1024 },
    { scope: 'project', cwd: '/project', definition, prefix: 'na', limit: 32 },
  ]);
  expect(f.calls.every(call => ['initialize', 'host.skills.complete'].includes(call.method))).toBe(true);
});

it('reads at most 1024 metadata records in eleven exact selected-session pages and excludes disabled skills', async () => {
  const f = await providerFixture();
  const all = Array.from({ length: 1200 }, (_, i) => skill(`skill${String(i).padStart(4, '0')}`, i === 5));
  f.data.handlers['skills.list'] = request => {
    if (request.method !== 'skills.list') throw new Error('Wrong method');
    const start = request.params.after ? all.findIndex(item => item.name === request.params.after) + 1 : 0;
    const items = all.slice(start, start + request.params.limit);
    return { items, next_after: start + items.length < all.length ? items.at(-1)!.name : null };
  };
  const result = await readSkillSuggestions(f.client, { sessionId: 'child' }, '', 1024, new AbortController().signal);
  expect(result.truncated).toBe(true); expect(result.candidates).toHaveLength(1023);
  expect(result.candidates.some(item => item.text === '$skill0005')).toBe(false);
  const reads = f.calls.filter(call => call.method === 'skills.list');
  expect(reads).toHaveLength(11);
  expect(reads.every(call => call.params.session_id === 'child' && call.params.limit <= 100)).toBe(true);
  expect(reads.at(-1)!.params).toEqual({ session_id: 'child', prefix: '', after: 'skill0999', limit: 24 });
  expect(f.calls.every(call => ['initialize', 'skills.list'].includes(call.method))).toBe(true);
});

it('bounds requests even when each metadata page has a short continuation', async () => {
  const f = await providerFixture(); let index = 0;
  f.data.handlers['skills.list'] = () => { const item = skill(`s${++index}`); return { items: [item], next_after: item.name }; };
  const result = await readSkillSuggestions(f.client, { sessionId: 'child' }, '', 1024, new AbortController().signal);
  expect(f.count('skills.list')).toBe(11); expect(result.candidates).toHaveLength(11); expect(result.truncated).toBe(true);
});

it('uses one bounded prefix fallback request and preserves truthful empty metadata', async () => {
  const f = await providerFixture();
  f.data.handlers['skills.list'] = () => ({ items: [], next_after: null });
  expect(await readSkillSuggestions(f.client, { sessionId: 'child' }, 'missing', 32, new AbortController().signal)).toEqual({ candidates: [], truncated: false });
  expect(f.calls.at(-1)?.params).toEqual({ session_id: 'child', prefix: 'missing', limit: 32 });
});

it.each(['wrong cursor', 'foreign prefix', 'repeated metadata'])('rejects %s rather than retaining ambiguous skill pages', async failure => {
  const f = await providerFixture(); let index = 0;
  f.data.handlers['skills.list'] = () => {
    index++;
    if (failure === 'foreign prefix') return { items: [skill('other')], next_after: null };
    if (failure === 'wrong cursor') return { items: [skill('skill')], next_after: 'not-the-last-name' };
    return { items: [skill('skill')], next_after: index === 1 ? 'skill' : null };
  };
  await expect(readSkillSuggestions(f.client, { sessionId: 'child' }, 's', 1024, new AbortController().signal)).rejects.toThrow(/Skill/);
  expect(f.count('skills.list')).toBeLessThanOrEqual(2);
});

it('stops discovery at an aborted scope without starting another page', async () => {
  const f = await providerFixture(); const controller = new AbortController();
  f.data.handlers['skills.list'] = () => { controller.abort(); return { items: [skill('skill')], next_after: 'skill' }; };
  await expect(readSkillSuggestions(f.client, { sessionId: 'child' }, '', 1024, controller.signal)).rejects.toThrow();
  expect(f.count('skills.list')).toBe(1);
});
