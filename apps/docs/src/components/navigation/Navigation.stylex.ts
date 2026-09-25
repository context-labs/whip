import * as stylex from '@stylexjs/stylex';
import { colors, scale } from '~/tokens.stylex';

// Parent-hover -> child color, replacing `.pager-card:hover .label`.
export const pagerVars = stylex.defineVars({
  labelColor: colors.textMuted,
});

export const styles = stylex.create({
  topNavLink: {
    fontSize: 12,
    lineHeight: '16px',
    color: { default: colors.textSecondary, ':hover': colors.text },
    textDecorationLine: 'none',
  },
  // `[aria-current]` is not expressible in StyleX; applied conditionally.
  topNavLinkActive: {
    color: { default: colors.text, ':hover': colors.text },
    fontWeight: 500,
  },
  communityLink: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 32,
    height: 32,
    borderWidth: 0,
    borderStyle: 'none',
    borderRadius: 'var(--radius-md)',
    backgroundColor: { default: 'transparent', ':hover': colors.surfaceHover },
    color: { default: colors.textSecondary, ':hover': colors.text },
  },
  sidebarItem: {
    display: 'block',
    fontSize: 13,
    lineHeight: '22px',
    paddingTop: 4,
    paddingBottom: 4,
    paddingLeft: 8,
    paddingRight: 8,
    marginTop: 2,
    color: { default: colors.textSecondary, ':hover': colors.text },
    borderRadius: 'var(--radius-md)',
    textDecorationLine: 'none',
    backgroundColor: { default: null, ':hover': colors.surfacePanel },
  },
  sidebarItemActive: {
    backgroundColor: { default: colors.surfaceControl, ':hover': colors.surfaceControl },
    color: { default: colors.text, ':hover': colors.text },
  },
  pagerCard: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    minHeight: 76,
    padding: { default: 16, [scale.phone]: 12 },
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: { default: colors.border, ':hover': colors.borderControl },
    borderRadius: 'var(--radius-md)',
    backgroundColor: { default: colors.surfacePanel, ':hover': colors.surfaceElement },
    textDecorationLine: 'none',
    [pagerVars.labelColor]: { default: colors.textMuted, ':hover': colors.textSecondary },
  },
  pagerNext: {
    justifyContent: 'flex-end',
    textAlign: 'right',
  },
  pagerLabel: {
    display: 'block',
    marginBottom: 4,
    color: pagerVars.labelColor,
  },
  pagerIcon: {
    width: 15,
    height: 15,
    color: colors.textSecondary,
  },
  pagerTitle: {
    fontSize: 13,
    lineHeight: '20px',
    fontWeight: 500,
    color: colors.text,
  },
});
