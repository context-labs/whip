import { themeCatalog, contrastRatio } from '@whip/ui/theme-data';
import { appearanceRecord, defaultAppearance } from './preferences';
import { nativeTheme } from './theme';
test('every generated theme has readable native foregrounds and a valid selectable record', () => {
  for (const source of themeCatalog) {
    const key = source.dark ? 'dark' : 'light';
    expect(appearanceRecord({ ...defaultAppearance, [key]: source.id }).appearance[key]).toBe(source.id);
    const theme = nativeTheme(source);
    for (const surface of ['background', 'panel', 'element'] as const) expect(contrastRatio(theme.colors.foreground, theme.colors[surface])).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(theme.colors.onPrimary, theme.colors.primary)).toBeGreaterThanOrEqual(4.49);
  }
});
test('custom themes validate identity, appearance and budgets', () => {
  const custom = { ...themeCatalog[0], id: 'import:test', name: 'My palette' };
  const key = custom.dark ? 'dark' : 'light';
  expect(appearanceRecord({ ...defaultAppearance, [key]: custom.id }, [custom]).themes).toHaveLength(1);
  expect(() => appearanceRecord(defaultAppearance, [themeCatalog[0]])).toThrow(/built-in/);
  expect(() => appearanceRecord(defaultAppearance, [custom, custom])).toThrow(/unique/);
  expect(() => appearanceRecord({ ...defaultAppearance, light: 'missing' })).toThrow(/available/);
  expect(() => appearanceRecord({ ...defaultAppearance, textScale: Infinity })).toThrow(/size/);
});
