import {restoreWorkerScope} from './mermaid-worker-bootstrap.ts';
import {renderMermaidSVG} from 'beautiful-mermaid';
import {MermaidRenderError, prepareMermaidSVG, validateMermaid, validateMermaidPalette} from './mermaid-data.ts';
import type {MermaidPalette} from './mermaid-data.ts';
import font0 from '@fontsource-variable/inter/files/inter-cyrillic-ext-wght-normal.woff2?inline';
import font1 from '@fontsource-variable/inter/files/inter-cyrillic-wght-normal.woff2?inline';
import font2 from '@fontsource-variable/inter/files/inter-greek-ext-wght-normal.woff2?inline';
import font3 from '@fontsource-variable/inter/files/inter-greek-wght-normal.woff2?inline';
import font4 from '@fontsource-variable/inter/files/inter-vietnamese-wght-normal.woff2?inline';
import font5 from '@fontsource-variable/inter/files/inter-latin-ext-wght-normal.woff2?inline';
import font6 from '@fontsource-variable/inter/files/inter-latin-wght-normal.woff2?inline';

// Prime the release's lazy FakeWorker while the import-time sentinel is present.
try {renderMermaidSVG('flowchart TD\n A --> B');} finally {restoreWorkerScope();}

const fonts = [
  [font0, 'U+0460-052F,U+1C80-1C8A,U+20B4,U+2DE0-2DFF,U+A640-A69F,U+FE2E-FE2F'],
  [font1, 'U+0301,U+0400-045F,U+0490-0491,U+04B0-04B1,U+2116'],
  [font2, 'U+1F00-1FFF'],
  [font3, 'U+0370-0377,U+037A-037F,U+0384-038A,U+038C,U+038E-03A1,U+03A3-03FF'],
  [font4, 'U+0102-0103,U+0110-0111,U+0128-0129,U+0168-0169,U+01A0-01A1,U+01AF-01B0,U+0300-0301,U+0303-0304,U+0308-0309,U+0323,U+0329,U+1EA0-1EF9,U+20AB'],
  [font5, 'U+0100-02BA,U+02BD-02C5,U+02C7-02CC,U+02CE-02D7,U+02DD-02FF,U+0304,U+0308,U+0329,U+1D00-1DBF,U+1E00-1E9F,U+1EF2-1EFF,U+2020,U+20A0-20AB,U+20AD-20C0,U+2113,U+2C60-2C7F,U+A720-A7FF'],
  [font6, 'U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD'],
];
const fontCSS = fonts.map(([url, range]) => {
  if (!url || !/^data:font\/woff2;base64,[A-Za-z0-9+/=]+$/.test(url)) throw new Error('Expected a bundled inline WOFF2 font.');
  return `@font-face{font-family:'Whip Diagram Inter';src:url(${url}) format('woff2');font-weight:100 900;font-style:normal;unicode-range:${range};}`;
}).join('');

self.onmessage = (event: MessageEvent<{id: number; source: string; palette: MermaidPalette}>) => {
  const {id, source, palette} = event.data;
  try {
    const check = validateMermaid(source);
    if (!check.ok) throw new MermaidRenderError(check.reason, check.message);
    validateMermaidPalette(palette);
    const raw = renderMermaidSVG(source, {bg: palette.background, fg: palette.foreground, line: palette.line, accent: palette.accent, muted: palette.muted, surface: palette.surface, border: palette.border, font: 'Whip Diagram Inter', interactive: false});
    self.postMessage({id, image: prepareMermaidSVG(raw, palette, fontCSS)});
  } catch (error) {
    const failure = error instanceof MermaidRenderError ? error : new MermaidRenderError('render-failed', 'The diagram could not be rendered. Showing the original source.');
    self.postMessage({id, error: {reason: failure.reason, message: failure.message}});
  }
};
self.postMessage({ready: true});
