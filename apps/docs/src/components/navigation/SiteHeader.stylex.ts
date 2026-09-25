import * as stylex from '@stylexjs/stylex';
import { colors, scale } from '~/tokens.stylex';

export const styles = stylex.create({
  siteHeader: {
    height: 86,
    borderBottomWidth: 1,
    borderBottomStyle: 'solid',
    borderBottomColor: colors.divider,
    backgroundColor: colors.bg,
    display: { default: null, [scale.print]: 'none' },
  },
  siteHeaderInner: {
    display: 'flex',
    alignItems: 'center',
    gap: { default: 32, [scale.phone]: 12 },
    height: '100%',
    width: { default: `min(${scale.siteMaxWidth}, 100% - 64px)`, [scale.phone]: 'calc(100% - 40px)' },
    marginInline: 'auto',
  },
  brandLink: {
    color: colors.text,
    flexShrink: 0,
  },
  wordmark: {
    width: { default: '142.304px', [scale.phone]: 122 },
    height: { default: 28, [scale.phone]: 24 },
  },
  headerActions: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'flex-end',
    minWidth: 0,
    gap: 12,
    marginLeft: 'auto',
  },
  headerCommunity: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  headerDownload: {
    flexShrink: 0,
    // Matches the removed `.header-download` media rule. The inline padding
    // (shorthand) also wins over the `.button` longhand paddings for any
    // stylesheet order.
    width: { default: null, [scale.tiny]: 34 },
    padding: { default: null, [scale.tiny]: 8 },
  },
  headerDownloadLabel: {
    display: { default: null, [scale.tiny]: 'none' },
  },
});
