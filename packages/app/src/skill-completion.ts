/** UTF-16 offsets deliberately match the native textarea selection API. */
export interface SkillTrigger { start: number; end: number; caret: number; prefix: string }

export function skillTrigger(text: string, start: number, end = start): SkillTrigger | null {
  if (start !== end || start < 0 || start > text.length) return null;
  let from = start;
  while (from > 0 && !/\s/.test(text[from - 1]!)) from--;
  if (text[from] !== '/' || start <= from) return null;
  let to = start;
  while (to < text.length && !/\s/.test(text[to]!)) to++;
  // Host names may be non-spec Unicode tokens; only exclude path/code delimiters.
  if (!/^\/[^/\s`]*$/u.test(text.slice(from, to))) return null;
  // ponytail: a bounded delimiter scan, not a Markdown parser. Matching backtick
  // runs cover inline code/fences; tilde fences are recognized at line starts.
  let ticks = 0;
  let tildeFence = false;
  const before = text.slice(0, from);
  for (const line of before.split('\n')) {
    if (/^ {0,3}~{3,}/.test(line) && ticks === 0) { tildeFence = !tildeFence; continue; }
    if (tildeFence) continue;
    for (const match of line.matchAll(/(`+)|\\./g)) {
      if (!match[1]) continue;
      const length = match[1].length;
      if (!ticks) ticks = length;
      else if (ticks === length || (ticks >= 3 && length >= ticks)) ticks = 0;
    }
  }
  if (ticks || tildeFence) return null;
  return { start: from, end: to, caret: start, prefix: text.slice(from + 1, start) };
}

export function insertSkill(text: string, trigger: SkillTrigger, reference: string): { text: string; caret: number } | null {
  // Invocation consumes a whitespace-delimited reference, not an ASCII identifier.
  if (!/^\$\S+$/u.test(reference)) return null;
  const current = skillTrigger(text, trigger.caret);
  if (!current || current.start !== trigger.start || current.end !== trigger.end || current.prefix !== trigger.prefix) return null;
  const suffix = text.slice(trigger.end);
  const insert = reference + (suffix && /^\s/.test(suffix) ? '' : ' ');
  const next = text.slice(0, trigger.start) + insert + suffix;
  if (next.length > 256 * 1024) return null;
  // Consume one existing separator too, so continued typing cannot extend $name.
  // A preserved newline places the caret at the beginning of the following line.
  return { text: next, caret: trigger.start + insert.length + (suffix ? 1 : 0) };
}
