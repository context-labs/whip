import { constants } from 'node:fs';
import { glob, open, realpath } from 'node:fs/promises';
import { homedir } from 'node:os';
import path from 'node:path';
import type { SSHProfile, SSHProfileList } from '@whip/app/platform';

const MAX_FILES = 64;
const MAX_BYTES = 1 << 20;
const MAX_PROFILES = 256;
const aliasPattern = /^[a-zA-Z0-9\[][a-zA-Z0-9_.:\[\]-]*$/;

/** Tokenize config values without interpreting shell commands or expansions. */
function words(line: string): string[] {
  const result: string[] = [];
  let word = '', quote = '', escaped = false;
  for (const char of line) {
    if (escaped) { word += char; escaped = false; }
    else if (char === '\\') escaped = true;
    else if (quote) { if (char === quote) quote = ''; else word += char; }
    else if (char === '"' || char === "'") quote = char;
    else if (char === '#') break;
    else if (/\s/.test(char)) { if (word) { result.push(word); word = ''; } }
    else word += char;
  }
  if (quote || escaped) throw new Error('An SSH configuration entry has an unfinished quote or escape.');
  if (word) result.push(word);
  return result;
}

/** Static hints only: OpenSSH still resolves the selected alias at connection time.
 * Never use ssh -G here: Match exec can execute local commands during evaluation.
 */
export async function listSSHProfiles(home = homedir()): Promise<SSHProfileList> {
  const profiles = new Map<string, SSHProfile>();
  const active = new Set<string>();
  const base = path.join(home, '.ssh');
  let bytes = 0, files = 0, includes = 0, truncated = false;
  let block: SSHProfile[] = [], conditional = false;
  const visit = async (filename: string, depth: number): Promise<void> => {
    if (depth > 8 || files >= MAX_FILES) { truncated = true; return; }
    let canonical: string;
    try { canonical = await realpath(filename); }
    catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return; throw error; }
    if (active.has(canonical)) throw new Error('SSH configuration includes a cycle. Fix the Include entries or enter a host manually.');
    const file = await open(canonical, constants.O_RDONLY | constants.O_NONBLOCK);
    let source: string;
    try {
      if (!(await file.stat()).isFile()) throw new Error('An SSH configuration path is not a regular file.');
      // Read one extra byte to detect oversized files without allocating their contents.
      const buffer = Buffer.alloc(MAX_BYTES - bytes + 1);
      let length = 0;
      while (length < buffer.length) {
        const read = await file.read(buffer, length, buffer.length - length, length);
        if (!read.bytesRead) break;
        length += read.bytesRead;
      }
      bytes += length;
      if (bytes > MAX_BYTES) throw new Error('SSH configuration exceeds the 1 MiB discovery limit. Enter a host manually.');
      source = buffer.subarray(0, length).toString('utf8');
    } finally { await file.close(); }
    files++; active.add(canonical);
    try {
      for (const line of source.split(/\r?\n/)) {
        const directive = /^\s*([^\s=#]+)\s*(?:=\s*)?(.*)$/.exec(line);
        if (!directive || directive[1].startsWith('#')) continue;
        const key = directive[1].toLowerCase();
        // Do not parse or expose arbitrary SSH options, private-key contents or commands.
        if (!['host', 'match', 'include', 'hostname', 'user', 'port'].includes(key)) continue;
        if (key === 'match') { conditional = true; block = []; continue; }
        const values = words(directive[2]);
        if (key === 'include' && !conditional) {
          for (const value of values) {
            if (++includes > MAX_FILES || files >= MAX_FILES) { truncated = true; break; }
            const pattern = value.startsWith('~/') ? path.join(home, value.slice(2)) : path.resolve(base, value);
            // Recursive directory scans are not needed to show a useful bounded profile list.
            if (pattern.includes('**')) { truncated = true; continue; }
            const matches: string[] = [];
            for await (const match of glob(pattern)) {
              matches.push(match);
              if (matches.length >= MAX_FILES) { truncated = true; break; }
            }
            for (const match of matches.sort()) await visit(match, depth + 1);
          }
        } else if (key === 'host') {
          conditional = false;
          block = [];
          for (const alias of values) {
            if (alias.length > 255 || !aliasPattern.test(alias) || (alias.includes('[') && !/^\[[0-9a-f:]+\]$/i.test(alias))) continue;
            const id = alias.toLowerCase();
            let profile = profiles.get(id);
            if (!profile) {
              if (profiles.size >= MAX_PROFILES) { truncated = true; continue; }
              profile = { alias }; profiles.set(id, profile);
            }
            block.push(profile);
          }
        } else if (!conditional && values.length === 1) {
          const value = values[0];
          if (value.length > 255 || /[\u0000-\u001f\u007f%$]/.test(value)) continue;
          for (const profile of block) {
            if (key === 'hostname') profile.hostname ??= value;
            if (key === 'user') profile.user ??= value;
            if (key === 'port' && /^\d+$/.test(value) && Number(value) > 0 && Number(value) <= 65535) profile.port ??= Number(value);
          }
        }
      }
    } finally { active.delete(canonical); }
  };
  await visit(path.join(base, 'config'), 0);
  return { profiles: [...profiles.values()], truncated };
}
