# Claude Code theme

Branch: `whip-rlm`

## Goal

Add a selectable **Claude Code** dark theme using the user's Paper desktop
reference. Preserve WHIP's layout, typography, device-local selection, and
accessibility adaptation. No dependencies or default-theme change.

## Research

Inspected the running Claude desktop Code workspace on 2026-09-07. Exact design
values come from [Paper's conversation and sidebar](https://app.paper.design/file/01M1YJSY1KZZMV9R11WBK1WBAN/1-0/2-0):

| Role | Paper value |
| --- | --- |
| Conversation canvas | `#141414` |
| Sidebar | `#111110` |
| Sidebar divider | `#1c1c1b` |
| Body text | `#c2c0b8` |
| Secondary navigation text | `#aaa99f` |
| Composer / user message / hover row | `#222221` |
| Selected session | `#343434` |
| Panel / code surface | `#1b1b19` |
| Control border | `#343430` |
| Claude mark | `#c87555` |
| Inline code | `#cf7569` |

Read the installed app's CSS at
`/Applications/Claude.app/Contents/Resources/ion-dist/assets/v1/c6a992d55-CWwY3mut.css`
for roles missing from Paper: blue `#6da7ec`, green `#91d68b`, yellow `#fab219`,
red `#ec7e7e`, violet `#b796ff`, and dark success/danger fills `#11260f`/`#3c0e0e`.
The bundled CDS palette and Paper are not identical revisions; Paper is the
authority for the supplied screens. Syntax uses these semantic colors; the
reference conversation does not specify a complete syntax-highlighting theme.

## Design

- Add `internal/theme/themes/claude-code.json` to the existing shared catalog.
- Optional `displayName` keeps the stable `claude-code` ID while showing
  **Claude Code** in the browser picker.
- Optional `web` color overrides pin `navigation`, `quietBorder`, `codeBackground`, and `inlineCodeBackground` in the source
  catalog. Existing themes retain their exact derived behavior. Validate and
  normalize these like all other colors; no theme-ID conditionals or arbitrary CSS.
- Generate UI and protocol artifacts using their existing generators.
- Preserve `adaptThemeForWeb`: source values stay exact, but insufficient text
  contrast still receives WHIP's normal accessible foreground adjustment.

## Tasks and verification

- [x] Read architecture and reference colors.
- [x] Add palette and minimal catalog support.
- [x] Verify resolution, validation, generated output and browser persistence.
- [x] Compare rendered sidebar, conversation and appearance picker.
- [x] Run `task check`, UI checks and all-theme browser contrast coverage.
- [x] Review for correctness and unnecessary complexity.
- [x] Update `docs/features.md`, `docs/frontend.md`, and current theme inventory.

The user's request authorizes implementation. No persisted session state or
concurrency changes are involved.

## Validation record

- Go theme resolver, generator and terminal-theme parity tests passed; 66 themes.
- UI type check and all 11 unit tests passed.
- Production app packaging and Storybook build passed.
- Component browser suite: 66 palettes and 10 interaction scenarios passed.
- Workspace tabs: Chromium/Firefox interactions and all 66 Axe checks passed.
- Full app: `node apps/web/scripts/claude-code-theme.mjs` passed in Chromium and
  Firefox, covering picker/keyboard selection, persistence, exact computed Paper
  colors, hover/selection, search contrast, mobile and theme-switch reset/CSP.
  Screenshots: `/tmp/whip-claude-code-theme-results/`.
- Adversarial review caught the code-block fill mismatch; fixed by pinning code
  and inline-chip fills. Re-review found no remaining actionable issues.
- Updated only the terminal palette golden affected by the added catalog row;
  its full-width trailing spaces are intentional.
- Initial full gates encountered unrelated provider-catalog/daemon timing
  failures. The catalog test inherited `INFERENCE_API_KEY` and called the live
  provider. Its isolated rerun with that variable unset passed. Final full gate
  uses `env -u INFERENCE_API_KEY GOFLAGS=-p=2 task check`.
- Final isolated `task check` passed in full (exit 0), including Go, generated
  protocol drift, SDK, app types/build/tests, UI types/unit tests and asset checks.
