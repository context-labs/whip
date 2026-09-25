import * as stylex from '@stylexjs/stylex';
import { colors, scale } from '~/tokens.stylex';

// Site shell styles that live outside any feature component: the root footer
// and the 404 page's main landmark. Migrated from the removed rules in
// styles/layout.css.
export const styles = stylex.create({
  siteFooter: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 24,
    width: { default: `min(${scale.siteMaxWidth}, 100% - 64px)`, [scale.phone]: 'calc(100% - 40px)' },
    marginInline: 'auto',
    paddingTop: 24,
    paddingBottom: 24,
    borderTopWidth: 1,
    borderTopStyle: 'solid',
    borderTopColor: colors.border,
    color: colors.textSecondary,
    fontSize: 12,
  },
  footerActions: {
    display: 'flex',
    alignItems: 'center',
    gap: 16,
  },
  siteMain: {
    width: { default: `min(${scale.siteMaxWidth}, 100% - 64px)`, [scale.phone]: 'calc(100% - 40px)' },
    marginInline: 'auto',
    paddingTop: { default: 56, [scale.phone]: 40 },
    paddingBottom: { default: 96, [scale.phone]: 64 },
  },
});
