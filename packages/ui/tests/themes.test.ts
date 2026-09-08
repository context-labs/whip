import {test} from 'node:test';
import assert from 'node:assert/strict';
import {themeCatalog} from '../src/generated/theme-catalog.ts';
import {adaptThemeForWeb, contrastRatio} from '../src/theme-contrast.ts';
import {validateTheme} from '../src/theme-data.ts';

test('every shipped theme remains exact and derives readable control/code foregrounds', () => {
  for (const source of themeCatalog) {
    const before = JSON.stringify(source);
    const shown = adaptThemeForWeb(validateTheme(source));
    assert.equal(JSON.stringify(source), before, source.id);
    for (const background of ['background', 'panel', 'element', 'hover'] as const) {
      assert.ok(contrastRatio(shown.colors.foreground, shown.colors[background]) >= 4.5, `${source.id} foreground on ${background}`);
    }
    for (const color of Object.values(shown.syntax)) assert.ok(contrastRatio(color, shown.code.background) >= 4.5, `${source.id} syntax`);
    assert.equal(shown.id, source.id);
  }
});
test('custom Chroma colors retain their selected background and complete attributes', () => {
  const custom = structuredClone(themeCatalog[0]!);
  const source = {...custom, id: 'runtime:custom', code: {foreground: '#eeeeee', background: '#101010', tokens: {Keyword: {color: '#ff99aa', background: '#101010', bold: true, italic: true, underline: true}}}};
  const shown = adaptThemeForWeb(validateTheme(source));
  assert.equal(shown.code.background, '#101010');
  assert.deepEqual(shown.code.tokens.Keyword, source.code.tokens.Keyword);
});
test('resolved color validation rejects raw CSS, malformed token attributes and oversized data', () => {
  const source = structuredClone(themeCatalog[0]!);
  assert.throws(() => validateTheme({...source, colors: {...source.colors, background: 'url(https://invalid.example)'}}), /Invalid resolved theme color/);
  assert.throws(() => validateTheme({...source, code: {...source.code, tokens: {Keyword: {color: '#123456', background: '#ffffff', bold: 'true', italic: false, underline: false}}}}), /Invalid syntax token attributes/);
  assert.throws(() => validateTheme({...source, extra: 'x'.repeat(262145)}), /256 KiB/);
  assert.throws(() => validateTheme({...source, colors: {}}), /Invalid resolved theme color/);
});
test('validated custom data cannot be mutated by a retained caller reference', () => {
  const source = structuredClone(themeCatalog[0]!);
  const resolved = validateTheme(source);
  assert.notEqual(resolved, source);
  assert.notEqual(resolved.colors, source.colors);
});

test('Claude Code preserves Paper surfaces through browser adaptation', () => {
  const source = themeCatalog.find(theme => theme.id === 'claude-code')!;
  assert.ok(source);
  assert.equal(source.name, 'Claude Code');
  assert.equal(source.dark, true);
  const shown = adaptThemeForWeb(validateTheme(source));
  assert.deepEqual(shown.web, {navigation: '#111110', quietBorder: '#1c1c1b', codeBackground: '#1b1b19', inlineCodeBackground: '#2b2726'});
  assert.equal(shown.code.background, '#1b1b19');
  assert.ok(Object.values(shown.code.tokens).every(token => token.background === '#1b1b19'));
  assert.ok(contrastRatio(shown.markdown.code, shown.web.inlineCodeBackground!) >= 4.5);
  for (const [role, expected] of Object.entries({background: '#141414', foreground: '#c2c0b8', muted: '#aaa99f', panel: '#1b1b19', element: '#222221', hover: '#343434', border: '#343430', primary: '#c87555'})) {
    assert.equal(shown.colors[role as keyof typeof shown.colors], expected, role);
  }
});

test('optional browser surfaces are validated and incompatible navigation stays readable', () => {
  const source = themeCatalog.find(theme => theme.id === 'dark')!;
  for (const web of [null, [], {navigation: 'red'}, {quietBorder: 'url(x)'}, {arbitrary: '#111111'}]) {
    assert.throws(() => validateTheme({...source, web}), /web theme/);
  }
  const custom = validateTheme({...source, web: {navigation: '#ffffff'}});
  const shown = adaptThemeForWeb(custom);
  assert.equal(custom.web?.navigation, '#ffffff');
  assert.equal(shown.web?.navigation, shown.colors.background);
  assert.ok(contrastRatio(shown.colors.foreground, shown.web!.navigation!) >= 4.5);
});
