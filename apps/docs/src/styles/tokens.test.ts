import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
// Vitest stubs CSS imports; read the authored tokens, not transformed CSS.
const css = readFileSync(resolve('src/styles/tokens.css'), 'utf8');
const syntax = readFileSync(resolve('src/styles/syntax.css'), 'utf8');
function luminance(hex: string) {
  const rgb = hex.slice(1).match(/../g)!.map(value => parseInt(value, 16) / 255).map(value => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4);
  return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
}
function contrast(a: string, b: string) { const x = luminance(a); const y = luminance(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); }
function hex(name: string) { return css.match(new RegExp(`--${name}: (#[0-9a-fA-F]{6});`))![1]; }
describe('Transcript links and inline code', () => {
  for (const mode of ['', 'light-']) {
    for (const surface of ['bg', 'surface-panel', 'surface-element']) {
      it(`${mode || 'dark-'}link meets 4.5:1 on ${surface}`, () => {
        expect(contrast(hex(`${mode}color-link`), hex(`${mode}color-${surface}`))).toBeGreaterThanOrEqual(4.5);
      });
    }
    it(`${mode || 'dark-'}inline code meets 4.5:1 on its element background`, () => {
      expect(contrast(hex(`${mode}color-code`), hex(`${mode}color-surface-element`))).toBeGreaterThanOrEqual(4.5);
    });
  }
});
describe('Paper Carbonfox syntax tokens', () => {
  for (const mode of ['', 'light-']) for (const role of ['keyword', 'string', 'number', 'comment', 'function', 'type', 'operator', 'punctuation']) {
    it(`${mode || 'dark-'}${role} meets unrounded 4.5:1 contrast`, () => {
      expect(contrast(hex(`${mode}color-syntax-${role}`), hex(`${mode}color-surface-panel`))).toBeGreaterThanOrEqual(4.5);
    });
  }
  it('resets nested token roles, never paints all descendants as a parent role', () => {
    expect(syntax).toContain('pre .token { color: var(--color-text); }');
    expect(syntax).not.toMatch(/\.token[^{}]*\s\*/);
    expect(syntax).toContain('.number, .boolean');
  });
});
