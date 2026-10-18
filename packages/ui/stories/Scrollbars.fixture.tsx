import * as stylex from '@stylexjs/stylex';
import { Button, ScrollArea, useTheme } from '../src';
import { colors, surface } from '../src/tokens.stylex';
import { scrollbarStyles } from '../src/scrollbar.stylex';

const styles = stylex.create({
  page: { padding: 24, backgroundColor: colors.background, color: colors.foreground },
  panels: { display: 'flex', flexWrap: 'wrap', gap: 24, marginTop: 16 },
  viewport: { width: 280, height: 240, overflow: 'auto' },
  navigation: { backgroundColor: surface.navigation },
  content: { width: 480, padding: 12 },
  row: { margin: 0, paddingBlock: 10, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
});

export function ScrollbarsExample() {
  const { theme, setTheme } = useTheme();
  const rows = <div {...stylex.props(styles.content)}>{Array.from({ length: 30 }, (_, index) =>
    <p key={index} {...stylex.props(styles.row)}>Session {index + 1} — Inspect queue lifecycle</p>)}</div>;
  return <main {...stylex.props(styles.page)}>
    <h1>Native scrollbars</h1>
    <Button onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>Switch theme</Button>
    <div {...stylex.props(styles.panels)}>
      <ScrollArea aria-label="Output" xstyle={styles.viewport}>{rows}</ScrollArea>
      <div role="region" aria-label="Navigation" tabIndex={0} {...stylex.props(styles.viewport, styles.navigation, scrollbarStyles.native)}>{rows}</div>
    </div>
  </main>;
}
