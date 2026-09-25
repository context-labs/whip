import { useEffect, useState } from 'react';
import { Menu } from '@base-ui/react/menu';
import { Icon } from '../Icons';
import * as stylex from '@stylexjs/stylex';
import { useHydrated } from '../useHydrated';
import { styles } from '../Button.stylex';
import { styles as navStyles } from '../../navigation/Navigation.stylex';
import { applyTheme, readThemePreference, saveThemePreference, themeStorageKey, type ThemePreference } from './theme';

export function ThemeMenu() {
  const hydrated = useHydrated();
  const [preference, setPreference] = useState<ThemePreference>('system');
  useEffect(() => {
    setPreference(readThemePreference());
    const media = matchMedia('(prefers-color-scheme: dark)');
    const syncSystem = () => { if (readThemePreference() === 'system') applyTheme('system'); };
    const syncStorage = (event: StorageEvent) => {
      if (event.key !== themeStorageKey && event.key !== null) return;
      const next = readThemePreference(); setPreference(next); applyTheme(next);
    };
    media.addEventListener('change', syncSystem);
    window.addEventListener('storage', syncStorage);
    return () => { media.removeEventListener('change', syncSystem); window.removeEventListener('storage', syncStorage); };
  }, []);
  if (!hydrated) return null;
  return <Menu.Root><Menu.Trigger {...stylex.props(navStyles.communityLink)} className={`community-link ${stylex.props(navStyles.communityLink).className}`} aria-label={`Colour theme: ${preference}`} title="Colour theme"><Icon name="theme" /></Menu.Trigger>
    <Menu.Portal><Menu.Positioner align="end" sideOffset={8}><Menu.Popup {...stylex.props(styles.menuPopup)}>
      <Menu.RadioGroup value={preference} onValueChange={value => { const next = value as ThemePreference; setPreference(next); saveThemePreference(next); }}>
        {(['system', 'light', 'dark'] as const).map(value => <Menu.RadioItem key={value} value={value} className={state => stylex.props(styles.menuItem, state.highlighted && styles.menuItemHighlighted, state.disabled && styles.menuItemDisabled).className}><span {...stylex.props(styles.menuCheck)}><Menu.RadioItemIndicator><Icon name="check" /></Menu.RadioItemIndicator></span>{value[0].toUpperCase() + value.slice(1)}</Menu.RadioItem>)}
      </Menu.RadioGroup>
    </Menu.Popup></Menu.Positioner></Menu.Portal>
  </Menu.Root>;
}
