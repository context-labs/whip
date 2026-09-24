# Component-board and syntax-highlighting research

Research date: 2026-09-24. Planning only; no component implementation or Paper edits.
This supplements [the implementation plan](README.md).

## Design authority and inspected sources

The user explicitly requested implementation of the supplied design board and
multicolour syntax highlighting. Do not interpret that as permission to invent
another visual direction or to carry over the old monochrome-code restriction.

- [Supplied Design Tokens — Dark board](https://app.paper.design/file/01M3A92K3HM4T5QF90SJFKQR0A/p-1-0/1O0-0)
- [Companion Design Tokens — Light board](https://app.paper.design/file/01M3A92K3HM4T5QF90SJFKQR0A/p-1-0/21O-0)
- [Dark reference page](https://app.paper.design/file/01M3A92K3HM4T5QF90SJFKQR0A/p-1-0/Y3-0)
- [Light reference page](https://app.paper.design/file/01M3A92K3HM4T5QF90SJFKQR0A/p-1-0/269-0)

Paper file: WHIPCODE DOCS, Page 1. Token content hash at inspection: `36d994cb`.
Nine artboards include the two component boards, two reference pages, palette
boards and a written guide. No mobile-specific artboard was listed.

Inspected via Paper MCP: file metadata, full CSS token export, Dark component
hierarchy, Light component hierarchy, Dark board screenshot, representative
computed styles for button states/copy controls/callouts/code panels/inline
code/table/pager/TOC, code-block and body-typography JSX, and reference-page grid
and header styles. Screenshot checks establish visual context; numerical values
below come from token/computed-style/JSX output, not image estimation.

The Dark board is 1440px wide. Its large title, numbered sections and annotated
swatches are design-system documentation, not the production docs-page layout.
Implement that board as a Storybook reference composition, and implement the
reusable components it demonstrates. Reference-page copy still mentions Fast
Inference; use it only as test content, not as whip product requirements.

## Verified foundation

| Role | Dark | Light |
| --- | --- | --- |
| Page background | `#161616` | `#FFFFFF` |
| Panel / code background | `#1A1A1A` | `#F4F4F4` |
| Raised element | `#1E1E1E` | `#F4F4F4` |
| Control | `#262626` | `#E8E8E8` |
| Hover | `#2C2C2C` | `#E0E0E0` |
| Border | `#303030` | `#DCDCDC` |
| Control border | `#3A3A3A` | `#C6C6C6` |
| Prose / heading | `#F2F4F8` | `#161616` |
| Secondary chrome | `#A9AFBC` | `#525252` |
| Muted labels | `#7D848F` | `#6F6F6F` |
| Focus | `#33B1FF` | `#0043CE` |

- System sans for prose/UI; system mono for code. Translate Paper's symbolic
  family names to the explicit platform stacks in the guide. No web fonts.
- H1 32/40, H2 22/28, H3 16/24, lead 15/24, body 13/22, small/code 12/20,
  label 11/16. Weights 400/500/600; label tracking 0.08em.
- Spacing tokens: 4, 8, 12, 16, 20, 24, 32, 64px.
- Containers/controls use 6px corners; inline code uses 4px. Borders and subtle
  surface changes supply depth, not shadows/gradients.
- Keep prose full-strength and navigation quiet. Active nav is weight/surface,
  not a blue pill; blue in syntax is a separate explicit user-approved role.

## Board-to-library checklist

Use one implementation with token swaps for both themes. Preserve semantic HTML,
not Paper's generic frame markup. Board sample widths belong to demonstration
containers unless the component's role requires a fixed dimension.

| Component family | Board variants / implementation contract |
| --- | --- |
| Primary button | Default, hover, focus, disabled. 32px minimum height, 8px vertical/16px horizontal padding, 1px control border, 6px radius. Board-measured total height is 34px with a 16px line and borders: do not force total height to 32px. |
| Icon/copy button | Default/hover, 28px square, 14px icon, accessible name and tooltip. Add keyboard focus, copied and failure states. |
| Split button | Action plus separate menu trigger; shared 6px outer boundary and separator. The board shows copy-page styling: implement the primitive/stories, but do not invent raw-page export or AI actions before page requirements are agreed. |
| Sidebar item | Group label, default, hover, active. 13/22 text, 4px/8px inset, full-width active neutral surface, 6px radius, no accent bar. |
| Top-nav link | Default, hover, active; 12/16, active weight/colour only. Use real links and aria-current where applicable. |
| TOC link | Default/active on 1px rail, 11/18 text, active 2px neutral marker. Anchor targets must match generated headings. |
| Community link | Official monochrome GitHub/Discord-style marks, approximately 16px within 32px targets, hover surface and accessible service name. Do not copy destination URLs blindly. |
| Callout | Info, tip, warning, alert. Panel background, 6px radius, 16px vertical/20px horizontal padding, 8px title/body gap; colour on 1px border only, full-strength title/body, no decorative icon. Static documentation callouts should not announce as live alerts. |
| Code block | Single and tabbed. 40px header, 20px code-body padding, 12/20 mono, 1px border, 6px panel; neutral labels and underlined active tab. Add multicolour token spans inside this unchanged shell. |
| Inline code | Neutral mono 11/18, panel chip, 4px radius, 2px/4px padding, 1px border. Not syntax-highlighted by default. |
| Table | Semantic table with header/cells, one rounded outer boundary, raised header, single internal separators, horizontal overflow on narrow screens. |
| Pager | Default/hover; previous and next compositions, 16px padding, 76px minimum height, label + page name + directional icon, 6px radius. |

Supporting behavior absent from the static board but needed in a real site:
Base UI menus/tooltips/tabs/mobile dialog, theme selection, focus/keyboard state,
copy feedback, long-label wrapping and responsive layout. Keep these additions
visually derived from the board; do not confuse them with supplied specifications.

## Discrepancies to resolve explicitly

1. **Monochrome samples:** both the old guide and code samples are superseded by
   the user's multicolour requirement. Preserve the panel/type/spacing/chrome;
   replace only token colouring and add syntax-role swatches/stories.
2. **Blue restricted to focus:** scope that rule to UI chrome. Dedicated syntax
   function colours may be blue; do not use `--color-accent` as a syntax shortcut.
3. **Code tab line height:** the exported Dark code-tab label uses 39px inside a
   40px header; the guide specifies 27px. Follow the supplied board's header
   geometry, centring the label semantically, and reconcile the guide.
4. **Copy-control radius:** standalone icon-button styles specify 6px, while the
   embedded code-block copy export lacks a radius. Use the canonical rounded
   icon-button primitive; record that normalization in visual review.
5. **Header/grid:** the reference header explicitly measures 86px, despite the
   general 4px-grid rule. Preserve as a documented exception rather than silently
   rounding. The exported grid names 228px, up-to-728px, 150–188px columns and
   32px gaps inside a max-1240px wrapper; measured maxima can exceed the wrapper.
   Keep sidebar/TOC intent and allow the article to shrink in the browser. Verify
   no horizontal page overflow instead of copying fixed frame measurements.
6. **Responsive layouts:** not supplied by the board. Propose/test 390px mobile,
   768px tablet and reference desktop widths; set breakpoints from content fit.
   Use a mobile navigation drawer and collapse/reposition TOC deliberately.
7. **Source parity:** update the worktree guide's precedence, syntax restrictions,
   colour roles, code/tab specs and checklist during component implementation.
   The Paper board itself remains unmodified in this research task. If updating
   its specimens later, do so as a separately reviewed design edit.

These do not block the framework scaffold. Bring genuine design ambiguities to
component review; avoid making the user adjudicate implementation-only details.

## Syntax-highlighting options researched

| Option | Fit | Decision |
| --- | --- | --- |
| Fast-web's Highlight.js | Existing reference uses core + Bash/Python/TypeScript/custom curl, called by a React renderer that injects highlighted HTML. Adaptable to build time, but adds another engine to whip. | Do not copy its runtime rendering/custom curl grammar. |
| Shiki + `@shikijs/rehype` | First-class rehype/HAST integration, VS Code grammars, custom/dual themes. Can be entirely build-time; dual themes use per-token CSS variables. | Viable alternative if future grammar fidelity requires it, not necessary for this board and language set. |
| Refractor (Prism AST) | Already used by whip, emits HAST token nodes/class names, explicit grammar imports, custom CSS roles. Naturally fits MDX/rehype and needs no browser runtime. | **Recommended.** Use existing dependency with a minimal build-time adapter. |
| `rehype-prism-plus` | Refractor-based integration with line wrapping, numbering, highlights and diff features. Can take a custom Refractor via its generator. | No need for its extra features now; a small adapter is sufficient. Reconsider instead of growing bespoke line/diff tooling later. |

References:

- [MDX syntax highlighting](https://mdxjs.com/guides/syntax-highlighting/):
  supports compile-time rehype plugins and emits highlighted JSX; CSS is still
  needed. Runtime rendering is optional, not required.
- [Refractor API](https://github.com/wooorm/refractor): core entry point has no
  pre-registered languages; `highlight` returns HAST with `token` classes.
- [rehype-prism-plus](https://github.com/timlrx/rehype-prism-plus): optional wrapper
  and feature comparison, not a dependency recommendation for phase 1.
- [Shiki rehype](https://shiki.style/packages/rehype) and
  [dual themes](https://shiki.style/guide/dual-themes): verified alternative.
- Local fast-web: `src/features/docs/components/docs-highlight.ts:1-33` and
  `DocsHighlightedCode.tsx:1-21`.
- Local whip: `packages/ui/src/code-highlight.ts:1-17,21-48` and
  `packages/ui/package.json:49`. Use as prior art, not a private-module import.

### Minimal compilation path

Fenced MDX code -> rehype pre/code nodes -> explicit Refractor grammar -> HAST
span nodes -> MDX's normal JSX compilation -> prerendered HTML + static CSS.

The adapter retains the language and original source for code-block controls.
Only the snippet's compiled markup/raw copy text reaches its page module; the
engine, grammars and full-site source corpus do not. The same Vite MDX compiler
configuration should power Storybook fixtures to exercise real output.

Start with Bash (`sh`, `shell`, `curl`), JavaScript (`js`), TypeScript (`ts`),
Python (`py`), Go (`golang`), JSON and YAML (`yml`). Curl is a shell command, not
its own required grammar. Python grammar is an explicitly approximate display
for Starlark/`bzl`; do not claim language validation. Unknown labels render plain
text and produce an author warning; no auto-detection. Add actual JSX/TSX/JSONC
support when corresponding examples appear.

Implement token-to-role mappings for the classes emitted by these grammars,
including nested token precedence: keyword, string, number/boolean, comment,
function, type/builtin, operator and punctuation. Property/variable/parameter
text can remain ordinary foreground when no semantic role is appropriate;
unmapped tokens must remain readable. Do not write custom regex tokenizers.

### Syntax palette and measured contrast

These proposed docs syntax roles come from `syntax` in whip's Carbonfox theme
JSON files (lines 28–36), not from newly invented swatch colours. Store local CSS
roles with a source comment; no runtime dependency on Go theme files or shared UI.
The Paper export does not yet have dedicated syntax roles beyond emphasis/code.

Calculated using WCAG relative luminance against the board's solid panel colours
(`#1A1A1A` / `#F4F4F4`), rounded to two decimals:

| Syntax role | Dark | Contrast | Light | Contrast |
| --- | --- | --- | --- | --- |
| keyword | `#BE95FF` | 7.40 | `#6929C4` | 7.03 |
| string | `#25BE6A` | 7.17 | `#198038` | 4.56 |
| number / boolean | `#3DDBD9` | 10.22 | `#007D79` | 4.54 |
| comment | `#7D848F` | 4.62 | `#6F6F6F` | 4.57 |
| function | `#8CB6FF` | 8.50 | `#0043CE` | 7.09 |
| type | `#08BDBA` | 7.46 | `#007D79` | 4.54 |
| operator | `#A9AFBC` | 7.91 | `#525252` | 7.10 |
| punctuation | `#A9AFBC` | 7.91 | `#161616` | 16.45 |

Use `--color-syntax-<role>` / `--light-color-syntax-<role>`; switch active roles
with the same theme selector as the site. Several pairs only narrowly exceed
4.5:1: do not reduce opacity or change the code surface without rechecking.
Tests should enforce contrast with unrounded values. No multicolour typography
is needed in prose, callout titles, navigation or inline-code chips.

## Verification and review gates

Research proof: ran a read-only Node 24 check against the installed Refractor in
the original checkout. Bash/curl, TypeScript, Python, Go, JSON and YAML each
produced multiple token classes and reconstructed the exact original text,
including line breaks and `<script>&` text. This was not a browser/security test
or full MDX integration test; those remain implementation acceptance gates.

Phase 1:
- A fixture's built HTML already contains token spans with multicolour CSS.
- Refractor/grammars are absent from browser chunks; no runtime highlighting.
- Unknown language/code with angle brackets remains text; source preserved.

Phase 2:
- Board Reference story matches the foundation/component order at 1440px.
- Each board variant exists in both themes; interactive states are functional.
- Component-level screenshot comparisons plus approved browser baselines.
- Code and tabs use the real MDX compilation path, including nested fences.
- Copy includes exactly the active snippet, not syntax markup or tab labels;
  preserve whitespace, Unicode, shell escaping and trailing newline policy.
- Highlight colours switch without parsing, flash or hydration mismatch.
- No-JS mode can read code; keyboard focus/tabs/copy and horizontal scrolling
  work without page overflow. Code must remain understandable without colour.
- Contrast assertions cover all emitted semantic roles in both modes.

The actual scaffold, MDX adapter, component stories and browser tests are not
implemented yet. The sequencing remains scaffold -> component library -> pages.
