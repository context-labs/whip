import { createContext, useContext, useMemo, type PropsWithChildren } from 'react';
import { useColorScheme } from 'react-native';
import { adaptThemeForWeb, themeCatalog, type ThemeDefinition } from '@whip/ui/theme-data';

export type Appearance = { mode: 'system' | 'light' | 'dark'; light: string; dark: string };
export const defaultAppearance: Appearance = { mode: 'system', light: 'github-light', dark: 'claude-code' };
const fallback = (dark: boolean) => themeCatalog.find(t => t.dark === dark)!;
const ThemeContext = createContext<ThemeDefinition>(adaptThemeForWeb(fallback(true)));
export function NativeTheme({ appearance, children }: PropsWithChildren<{ appearance: Appearance }>) {
  const system = useColorScheme();
  const dark = appearance.mode === 'system' ? system !== 'light' : appearance.mode === 'dark';
  const id = dark ? appearance.dark : appearance.light;
  const theme = useMemo(() => adaptThemeForWeb(themeCatalog.find(t => t.id === id && t.dark === dark) ?? fallback(dark)), [id, dark]);
  return <ThemeContext.Provider value={theme}>{children}</ThemeContext.Provider>;
}
export const useTheme = () => useContext(ThemeContext);
export { themeCatalog };
