import * as stylex from '@stylexjs/stylex';
import { colors } from '@whip/ui/tokens.stylex';

// Each panel tucks only its empty bottom padding behind the next surface.
export const composerPanels = stylex.create({
  surface: {
    borderWidth: 1, borderStyle: 'solid', borderColor: colors.border, borderBottomWidth: 0,
    borderTopLeftRadius: 20, borderTopRightRadius: 20, backgroundColor: colors.element,
    marginInline: 8, paddingTop: 4, paddingBottom: 12, marginBottom: -12, minWidth: 0,
  },
});
