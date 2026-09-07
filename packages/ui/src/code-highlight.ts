import {refractor} from 'refractor/core';
import python from 'refractor/python';
import javascript from 'refractor/javascript';
import typescript from 'refractor/typescript';
import go from 'refractor/go';
import json from 'refractor/json';
import bash from 'refractor/bash';
import type {Element, RootContent} from 'hast';

for (const grammar of [python, javascript, typescript, go, json, bash]) refractor.register(grammar);
export interface CodeToken {text: string; kind: string; offset: number}
export interface HighlightedCode {tokens: readonly CodeToken[]; unavailable?: string}
const aliases: Record<string, string> = {py: 'python', starlark: 'python', bzl: 'python', js: 'javascript', jsx: 'javascript', ts: 'typescript', tsx: 'typescript', golang: 'go', sh: 'bash', shell: 'bash', zsh: 'bash', jsonc: 'json'};
export function normalizeLanguage(language?: string): string | undefined {
  const name = language?.toLowerCase().trim().replace(/^language-/, '');
  if (!name || ['text', 'txt', 'plaintext', 'plain', 'none'].includes(name)) return;
  return aliases[name] ?? name;
}
/** This module is lazy-loaded by CodeBlock. No grammar is evaluated until a
 * bounded code body actually needs highlighting; unsupported input remains text. */
export function highlightCode(code: string, language?: string): HighlightedCode {
  const name = normalizeLanguage(language);
  const plain = {tokens: [{text: code, kind: 'plain', offset: 0}]};
  if (!name) return plain;
  if (!['python', 'javascript', 'typescript', 'go', 'json', 'bash'].includes(name)) return {...plain, unavailable: `Highlighting is unavailable for ${language}. Showing plain text.`};
  if (code.length > 16384) return {...plain, unavailable: 'Highlighting is unavailable for this large body. Showing plain text.'};
  try {
    const tree = refractor.highlight(code, name);
    const tokens: CodeToken[] = []; let offset = 0; let nodeCount = 0;
    const visit = (nodes: readonly RootContent[], inherited = 'plain', depth = 0) => {
      if (depth > 64) throw new Error('Syntax nesting limit');
      for (const node of nodes) {
        if (++nodeCount > 4096) throw new Error('Syntax node limit');
        if (node.type === 'text') {
          const previous = tokens.at(-1);
          if (previous?.kind === inherited) previous.text += node.value;
          else tokens.push({text: node.value, kind: inherited, offset});
          offset += node.value.length;
        } else if (node.type === 'element') {
          const classes = (node as Element).properties.className;
          const kind = Array.isArray(classes) ? classes.filter(item => typeof item === 'string' && item !== 'token').at(0) : undefined;
          visit(node.children, typeof kind === 'string' ? kind : inherited, depth + 1);
        }
      }
    };
    visit(tree.children);
    return {tokens};
  } catch {return {...plain, unavailable: 'Syntax highlighting could not complete within its limits. Showing plain text.'};}
}
