# Reviewed component screenshot reference

These ten PNGs are real Playwright screenshot comparison baselines, not generated
documentation illustrations. `npm run test:visual -w @whip/ui` fails on differences
or missing baselines. `test:visual:update` is the only update command; every
changed image requires review. Run from an installed repository after
`npm run build:storybook -w @whip/ui`.

## Reference environment

Reviewed on 2026-09-06:

| Setting | Reference |
| --- | --- |
| Operating system | macOS 26.3.1, ARM64, Apple M4 Max |
| Node | 24.14.1 |
| Playwright | `@playwright/test` 1.63.0, pinned by root lockfile |
| Browser | bundled Chromium 153.0.8010.12, headless |
| Fonts | self-hosted Inter Variable and JetBrains Mono Variable 5.3.0 |
| Desktop viewport | 1080 × 960 CSS pixels |
| Narrow viewport | 390 × 844 CSS pixels; full-page capture |
| Scale | device pixel ratio 1; screenshot CSS scale |
| Locale / timezone | en-US / UTC |
| Motion | reduced motion; screenshot animations disabled; caret hidden |
| Comparison | Playwright perceptual threshold 0.1; zero differing pixels allowed |

The runner rejects non-macOS platforms with an actionable error rather than
silently passing. The macOS check prevents comparing Linux rendering to these
images; it does not assert all macOS versions/architectures render identically.
Use this reference environment for the local release gate. A new browser or OS
reference requires a reviewed baseline update, or a separately reviewed baseline
set. There is no automated visual CI claim for other reference environments.
Portable browser interactions, Axe and CSP tests remain independent gates.

## Fixture coverage and review

Each fixture has `-light.png` and `-dark.png` variants:

| Fixture | Rendered state |
| --- | --- |
| content | Active Activity tab, expanded tool output, progress, badges, reconnecting, loading, empty and error states |
| forms | Field descriptions/errors, multiline input, checkbox/switch/radio, select/combobox, number control and disabled input |
| overlay | Open dialog with nested open menu rendered through Base UI portals |
| narrow | Actions, forms and content at 390 pixels; all content captured vertically |
| runtime | Pending and resolved permission notices, interrupted turn, reconnecting, root/child agent states, highlighted Starlark and loading state |

All ten images were opened and visually inspected. Review verified portal theme
inheritance, readable control/state labels, wrapped narrow controls, code colors
and absence of clipping or horizontal overflow. Visual inspection found radio
descriptions concatenating with labels; those now use the existing block
description style, and all four affected form/narrow images were regenerated and
reviewed. These are deterministic component states, not claims about a physical
phone or assistive technology.

`test:visual:proof` enlarges the actual runtime fixture's Agent activity heading.
The captured deliberate difference is required to fail `toHaveScreenshot` and
produce a nonempty diff PNG, while the baseline SHA-256 remains unchanged. Proof
artifacts are ignored under `packages/ui/ui-test-results/visual-diff-proof*`;
subsequent passing comparisons do not erase them. Baselines themselves are
checked in. The first proof produced 97,039 differing pixels and a height change
from 960 to 983 pixels; all ten normal comparisons passed again afterward.

Playwright documents platform-dependent baselines and explicit update behavior in
[Visual comparisons](https://playwright.dev/docs/test-snapshots) and screenshot
stability/options in [toHaveScreenshot](https://playwright.dev/docs/api/class-pageassertions#page-assertions-to-have-screenshot-1).
