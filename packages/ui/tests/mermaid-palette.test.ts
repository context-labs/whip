import assert from 'node:assert/strict';
import test from 'node:test';
import {themeCatalog} from '../src/generated/theme-catalog.ts';
import {adaptThemeForWeb, browserSurfaces, contrastRatio} from '../src/theme-contrast.ts';

test('Mermaid label and meaningful connector roles remain readable across all themes', () => {
  for (const original of themeCatalog) for (const increased of [false, true]) {
    const theme = adaptThemeForWeb(original, increased);
    const roles = browserSurfaces(theme, increased);
    for (const background of [theme.colors.background, theme.colors.element]) {
      for (const ink of [theme.colors.foreground, theme.colors.accent, roles.secondaryText]) {
        assert.ok(contrastRatio(ink, background) >= (increased ? 7 : 4.5), `${original.id}: diagram text ${ink} on ${background}`);
      }
      assert.ok(contrastRatio(roles.controlBorder, background) >= 3, `${original.id}: diagram connectors on ${background}`);
    }
  }
});
