import * as stylex from '@stylexjs/stylex';
import { colors, typography, scale } from '~/tokens.stylex';

export const styles = stylex.create({
  tableScroll: {
    overflow: 'auto',
    borderWidth: '1px',
    borderStyle: 'solid',
    borderColor: colors.border,
    borderRadius: scale.radiusMd,
    maxWidth: '100%',
    ':focus-visible': { outlineOffset: '3px' },
  },
  table: {
    borderCollapse: 'separate',
    borderSpacing: 0,
    width: '100%',
    fontFamily: typography.sans,
    fontSize: '12px',
    lineHeight: '20px',
  },
  tableTh: {
    fontFamily: typography.sans,
    fontWeight: 600,
    fontSize: '11px',
    lineHeight: '27px',
    textTransform: 'uppercase',
    letterSpacing: 'var(--tracking-label, 0.08em)',
    paddingTop: '6px',
    paddingBottom: '6px',
    paddingLeft: '12px',
    paddingRight: '12px',
    textAlign: 'left',
    backgroundColor: colors.surfaceElement,
    color: colors.textSecondary,
  },
  tableTd: {
    padding: '12px',
    verticalAlign: 'top',
    borderTopWidth: '1px',
    borderTopStyle: 'solid',
    borderTopColor: colors.border,
  },
  // `.table-scroll :is(th, td) + :is(th, td) { border-left }`
  tableCellSibling: {
    borderLeftWidth: '1px',
    borderLeftStyle: 'solid',
    borderLeftColor: colors.border,
  },
  tableCaption: {
    textAlign: 'left',
    padding: '12px',
    color: colors.text,
  },
});
