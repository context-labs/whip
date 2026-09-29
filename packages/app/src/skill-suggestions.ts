import type { Client, DefinitionRef, HostSkillsResult } from '@whip/sdk';

export type SkillScope = { sessionId: string } | { cwd: string; definition: DefinitionRef | null };

/** Bounded metadata discovery for an exact native session or a draft host scope. */
export async function readSkillSuggestions(client: Client, scope: SkillScope, prefix: string, limit: 32 | 1024, signal: AbortSignal): Promise<HostSkillsResult> {
  if (!('sessionId' in scope)) return client.completeHostSkills({ scope: scope.cwd.trim() ? 'project' : 'global', cwd: scope.cwd.trim(), definition: scope.definition, prefix, limit }, { signal });
  const candidates: HostSkillsResult['candidates'] = [];
  const seen = new Set<string>();
  const cursors = new Set<string>();
  let after: string | undefined, scanned = 0, retainedBytes = 0;
  for (let pageIndex = 0; pageIndex < Math.ceil(limit / 100) && scanned < limit; pageIndex++) {
    signal.throwIfAborted();
    const page = await client.call('skills.list', { session_id: scope.sessionId, prefix, ...(after ? { after } : {}), limit: Math.min(100, limit - scanned) }, { signal });
    if (page.items.length > Math.min(100, limit - scanned)) throw new Error('Skill page exceeded its requested limit');
    for (const item of page.items) {
      scanned++;
      if (seen.has(item.name)) throw new Error('Skill page repeated metadata');
      seen.add(item.name);
      if (!item.name.startsWith(prefix)) throw new Error('Skill metadata does not match the requested prefix');
      if (item.disabled) continue;
      const candidate = { text: `$${item.name}`, description: item.description };
      retainedBytes += new TextEncoder().encode(JSON.stringify(candidate)).byteLength;
      if (retainedBytes > 1 << 20) return { candidates, truncated: true };
      candidates.push(candidate);
    }
    if (page.next_after === null) return { candidates, truncated: false };
    if (!page.items.length || cursors.has(page.next_after) || page.next_after !== page.items.at(-1)!.name) throw new Error('Skill page did not advance its exact cursor');
    cursors.add(page.next_after);
    after = page.next_after;
  }
  return { candidates, truncated: true };
}
