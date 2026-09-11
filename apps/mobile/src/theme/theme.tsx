import { createContext, useContext, useEffect, useMemo, useState, type PropsWithChildren } from 'react';
import { AccessibilityInfo, Platform, useColorScheme } from 'react-native';
import { adaptThemeForWeb, readableColor, themeCatalog, type ThemeDefinition } from '@whip/ui/theme-data';
import { defaultAppearance, type Appearance } from './preferences';
import { fonts } from './fonts';
export { defaultAppearance, type Appearance } from './preferences';
export { themeCatalog };
export function nativeTheme(source: ThemeDefinition, increased = false) {
  const theme = adaptThemeForWeb(source, increased);
  return { ...theme, colors: { ...theme.colors, onPrimary: readableColor(theme.colors.onPrimary, [theme.colors.primary], increased ? 7 : 4.5) } };
}
const ThemeContext = createContext(nativeTheme(themeCatalog.find(t => t.id === 'claude-code')!));
const DisplayContext = createContext({ ...defaultAppearance, reducedMotion: false, regular: fonts.regular, semibold: fonts.semibold, mono: fonts.mono });
const PreviewContext = createContext<(id?: string) => void>(() => {});
export function NativeTheme({ appearance, themes = [], children }: PropsWithChildren<{ appearance: Appearance; themes?: readonly ThemeDefinition[] }>) {
  const system = useColorScheme(); const [reduce, setReduce] = useState(false); const [contrast, setContrast] = useState(false); const [preview, setPreview] = useState<string>();
  useEffect(() => {
    let live = true, motionEvent = false, contrastEvent = false;
    void AccessibilityInfo.isReduceMotionEnabled().then(value => { if (live && !motionEvent) setReduce(value); }).catch(() => {});
    const motion = AccessibilityInfo.addEventListener('reduceMotionChanged', value => { motionEvent = true; setReduce(value); });
    const high = AccessibilityInfo.addEventListener('highTextContrastChanged', value => { contrastEvent = true; setContrast(value); });
    if (Platform.OS === 'android') void AccessibilityInfo.isHighTextContrastEnabled().then(value => { if (live && !contrastEvent) setContrast(value); }).catch(() => {});
    return () => { live = false; motion.remove(); high.remove(); };
  }, []);
  const preferences = { ...defaultAppearance, ...appearance };
  const dark = preferences.mode === 'system' ? system !== 'light' : preferences.mode === 'dark';
  const id = preview ?? (dark ? preferences.dark : preferences.light);
  const increased = preferences.contrast === 'increased' || preferences.contrast === 'system' && contrast;
  const theme = useMemo(() => nativeTheme([...themeCatalog, ...themes].find(t => t.id === id) ?? themeCatalog.find(t => t.id === (dark ? 'claude-code' : 'github-light'))!, increased), [id, dark, themes, increased]);
  const display = { ...preferences, reducedMotion: preferences.motion === 'reduce' || reduce, regular: preferences.font === 'system' ? undefined : fonts.regular, semibold: preferences.font === 'system' ? undefined : fonts.semibold, mono: preferences.codeFont === 'system' ? (Platform.OS === 'ios' ? 'Menlo' : 'monospace') : fonts.mono };
  return <ThemeContext.Provider value={theme}><DisplayContext.Provider value={display}><PreviewContext.Provider value={setPreview}>{children}</PreviewContext.Provider></DisplayContext.Provider></ThemeContext.Provider>;
}
export const useTheme = () => useContext(ThemeContext);
export const useDisplay = () => useContext(DisplayContext);
export const useThemePreview = () => useContext(PreviewContext);
