import type { Preview } from '@storybook/react-vite';
import { ThemeProvider, UIProvider } from '../src';
import '../src/reset.css';
import '../src/fonts.css';
const preview: Preview = {
  // The standalone runner owns its awaited Axe scans. Normal Storybook visits
  // retain automatic scans and the interactive Accessibility panel.
  initialGlobals: {a11y: {manual: new URLSearchParams(location.search).get('whip_a11y') === 'manual'}},
  decorators: [Story => <ThemeProvider initialTheme={new URLSearchParams(location.search).get('theme') ?? undefined} storage={{getItem: key => localStorage.getItem(key), setItem: (key, value) => localStorage.setItem(key, value)}}><UIProvider><Story/></UIProvider></ThemeProvider>],
  parameters: {layout: 'padded', a11y: {test: 'error'}, controls: {expanded: true}},
};
export default preview;
