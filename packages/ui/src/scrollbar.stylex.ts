import * as stylex from '@stylexjs/stylex';
import { colors } from './tokens.stylex';

const desktop = '@media (pointer: fine) and (forced-colors: none)';

// Style the native viewport, never replace it: virtualizers and scroll intent
// handlers keep ownership. Transparent tracks/insets blend into any surface.
export const scrollbarStyles = stylex.create({
  native: {
    // Non-auto standard properties override WebKit pseudo-elements in Chromium.
    // Use them only as the fallback; touch and forced colors stay platform-native.
    scrollbarWidth: {
      default: null,
      '@supports not selector(::-webkit-scrollbar)': { default: null, [desktop]: 'thin' },
    },
    scrollbarColor: {
      default: null,
      '@supports not selector(::-webkit-scrollbar)': {
        default: null,
        [desktop]: `color-mix(in srgb, ${colors.foreground} 25%, transparent) transparent`,
      },
    },
    '::-webkit-scrollbar': {
      width: { default: null, [desktop]: 9 },
      height: { default: null, [desktop]: 9 },
    },
    '::-webkit-scrollbar-track': {
      backgroundColor: { default: null, [desktop]: 'transparent' },
    },
    '::-webkit-scrollbar-corner': {
      backgroundColor: { default: null, [desktop]: 'transparent' },
    },
    '::-webkit-scrollbar-thumb': {
      backgroundColor: { default: null, [desktop]: `color-mix(in srgb, ${colors.foreground} 25%, transparent)` },
      backgroundClip: { default: null, [desktop]: 'padding-box' },
      borderWidth: { default: null, [desktop]: 2 },
      borderStyle: { default: null, [desktop]: 'solid' },
      borderColor: { default: null, [desktop]: 'transparent' },
      borderRadius: { default: null, [desktop]: 999 },
    },
    '::-webkit-scrollbar-thumb:hover': {
      backgroundColor: { default: null, [desktop]: `color-mix(in srgb, ${colors.foreground} 40%, transparent)` },
    },
  },
});
