import { Menu } from '@base-ui/react/menu';
import * as stylex from '@stylexjs/stylex';
import { CopyButton } from '../../../components/ui/CopyButton';
import { Icon } from '../../../components/ui/Icons';
import { useHydrated } from '../../../components/ui/useHydrated';
import { colors, scale } from '~/tokens.stylex';

const styles = stylex.create({
  root: {
    display: { default: 'flex', [scale.print]: 'none' },
    flexShrink: 0,
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: colors.border,
    borderRadius: 'var(--radius-md)',
  },
  copyButton: {
    width: 32,
    height: 28,
    borderWidth: 0,
    borderStyle: 'none',
    borderRadius: '5px 0 0 5px',
    backgroundColor: 'transparent',
  },
  trigger: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 31,
    height: 28,
    padding: 0,
    color: { default: colors.textMuted, ':hover': colors.text },
    backgroundColor: { default: 'transparent', ':hover': colors.surfaceHover },
    borderWidth: 0,
    borderStyle: 'none',
    borderLeftWidth: 1,
    borderLeftStyle: 'solid',
    borderLeftColor: colors.border,
    borderRadius: '0 5px 5px 0',
  },
});

export function DocPageActions({ source, filename }: { source: string; filename: string }) {
  const hydrated = useHydrated();
  if (!hydrated) return null;
  return <div {...stylex.props(styles.root)}>
    <CopyButton text={source} label="Copy page" failureMessage="Copy failed. Use the menu to download the page source." buttonStyle={styles.copyButton} />
    <Menu.Root>
      <Menu.Trigger {...stylex.props(styles.trigger)} aria-label="More page actions"><Icon name="chevron" strokeWidth={2} /></Menu.Trigger>
      <Menu.Portal><Menu.Positioner sideOffset={6} align="end"><Menu.Popup className="menu-popup">
        <Menu.Item className="menu-item" render={<a href={`data:text/plain;charset=utf-8,${encodeURIComponent(source)}`} download={filename} />}>Download page source</Menu.Item>
      </Menu.Popup></Menu.Positioner></Menu.Portal>
    </Menu.Root>
  </div>;
}
