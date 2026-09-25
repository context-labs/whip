# whipcode docs - Brand & Design Guide

The single source of truth for designing in the whipcode docs style. Everything here is backed
by design tokens in this Paper file (WHIPCODE DOCS).

- Token boards: "Design Tokens - Dark", "Design Tokens - Light"
- Reference pages: "Fast Inference - Dark", "Fast Inference - Light"
- Theme: carbonfox (dark) / carbonfox-light (light), from whip `internal/theme/themes/`

The user-approved Paper token/component boards are the visual authority. This guide
records their implementation in `apps/docs`; explicit user instructions take precedence.
The approved multicolour syntax requirement supersedes the board's monochrome code
samples. Paper remains unchanged; do not edit designs as a side effect of code work.
See `.ai-docs/plans/docs-site/design-research.md` for measured values and exceptions.

---

## 1. Principles

1. **Calm and technical.** A reading surface for engineers. Nothing shouts.
2. **Content first, chrome quiet.** Prose is full-strength text colour; navigation and meta
   are dimmer so the content is always the brightest thing on the page.
3. **Neutral by default, colour means something.** Greys carry the layout. Colour is only
   used for status (tip, warning, alert), code, and focus. Never for decoration.
4. **Borders, not shadows.** Depth comes from 1px hairlines and small surface steps.
   No drop shadows, no glows, no gradients.
5. **One corner.** Every container and control uses the same 6px radius.
6. **One grid.** Every size, gap and padding is a multiple of 4px.
7. **Remove before you add.** No icon, border, or colour unless removing it loses meaning.

## 2. Colour

Every colour has a dark token (`--color-*`) and a light twin (`--light-color-*`) with the same
role. Design with roles, never raw hex.

### 2.1 Surfaces

| Token                   | Dark    | Light   | Use                                 |
|-------------------------|---------|---------|-------------------------------------|
| --color-bg              | #161616 | #FFFFFF | Page background, header, sidebar    |
| --color-surface-panel   | #1A1A1A | #F4F4F4 | Callouts, code blocks, cards, chips |
| --color-surface-element | #1E1E1E | #F4F4F4 | Table header, raised rows           |
| --color-surface-control | #262626 | #E8E8E8 | Buttons, active sidebar pill        |
| --color-surface-hover   | #2C2C2C | #E0E0E0 | Hover fill for controls             |

### 2.2 Lines

| Token                  | Dark        | Light       | Use                                |
|------------------------|-------------|-------------|------------------------------------|
| --color-border         | #303030     | #DCDCDC     | Default 1px hairline everywhere    |
| --color-border-control | #3A3A3A     | #C6C6C6     | Button edges, hover borders        |
| --color-divider        | #F2F4F8 12% | #161616 10% | Header bottom edge, faint dividers |

### 2.3 Text

| Token                  | Dark    | Light   | Use                                      |
|------------------------|---------|---------|------------------------------------------|
| --color-text           | #F2F4F8 | #161616 | Headings, ALL body copy, active states   |
| --color-text-secondary | #A9AFBC | #525252 | Navigation, tabs, icons     |
| --color-text-muted     | #7D848F | #6F6F6F | Labels, inactive tabs/TOC, default icons |

### 2.4 Accent & status

| Token            | Dark    | Light   | Use                                         |
|------------------|---------|---------|---------------------------------------------|
| --color-accent   | #33B1FF | #0043CE | Focus rings only                            |
| --color-link     | #33B1FF | #0072C3 | Underlined content links                    |
| --color-code     | #25BE6A | #198038 | Inline code; fences use syntax roles (9.2) |
| --color-success  | #25BE6A | #198038 | Tip / success                               |
| --color-warning  | #F1C21B | #8E6A00 | Warning                                     |
| --color-error    | #EE5396 | #9F1853 | Alert / error                               |
| --color-emphasis | #BE95FF | #6929C4 | Syntax keyword source colour (see 9.2)     |

### 2.5 Callout borders

| Token                          | Dark        | Light       |
|--------------------------------|-------------|-------------|
| --color-callout-info-border    | #303030     | #DCDCDC     |
| --color-callout-tip-border     | #25BE6A 35% | #198038 35% |
| --color-callout-warning-border | #F1C21B 35% | #8E6A00 35% |
| --color-callout-alert-border   | #EE5396 40% | #9F1853 40% |

### 2.6 Colour rules

- Prose (paragraphs, list items, callout bodies, table cells) uses `--color-text`,
  except content links (`--color-link`) and inline code (`--color-code`).
  Never set body copy in secondary or muted.
- Headings are also `--color-text`. Hierarchy comes from size and weight, not colour.
- `--color-text-secondary` is for chrome: nav links, icons, active tab labels.
- `--color-text-muted` is for labels and inactive states only. Never for anything the reader
  must read to complete a task.
- `--color-accent` remains the keyboard-focus role. Prose links use the separate
  `--color-link` role (Carbonfox blue), underlined with a 3px offset in every state.
  Navigation, buttons, callout titles and decorative markers remain neutral.
  Code function syntax uses `--color-syntax-function`, never the focus token.
- Status colours appear only as callout borders or status text. Never as large fills, and
  never on callout titles.
- Light mode `--light-color-warning` is #8E6A00 (darkened carbonfox yellow). Carbonfox-light's
  own warning (#007D79 teal) is not used because it does not read as a warning; raw yellow
  #F1C21B is not used because it is 1.7:1 on white.

## 3. Contrast

All text pairings meet WCAG AA (4.5:1). Measured values:

| Pairing                   | Dark   | Light  |
|---------------------------|--------|--------|
| text on bg                | 16.4:1 | 18.1:1 |
| text on surface-panel     | 15.8:1 | 16.5:1 |
| text-secondary on bg      | 8.2:1  | 7.8:1  |
| text-muted on bg          | 4.8:1  | 5.0:1  |
| code on surface-panel     | 7.2:1  | 4.6:1  |
| accent on bg (focus ring) | 7.7:1  | 7.8:1  |
| warning on bg             | -      | 5.0:1  |

Rules:
- Muted text is the floor. Nothing lighter than `--color-text-muted` may carry words.
- Do not put muted text on `--color-surface-control` or darker/lighter steps without
  re-checking contrast.
- Light-mode code green (#198038) is just above AA on panel. Do not shrink code below 12px.

## 4. Typography

### 4.1 Families

| Token       | Value             | Stack                                                   |
|-------------|-------------------|---------------------------------------------------------|
| --font-sans | System Sans-Serif | system-ui, sans-serif (San Francisco on macOS)          |
| --font-mono | System Monospace  | ui-monospace, SF Mono, Menlo, Consolas, Liberation Mono |

- Sans is used for everything: headings, prose, UI, labels, tables.
- Mono is used ONLY for code blocks, inline code, and token names in documentation.
- No web fonts. No display fonts. No italics for emphasis in UI.

### 4.2 Type scale

| Style         | Size | Line | Weight  | Tracking | Colour         | Tokens                         |
|---------------|------|------|---------|----------|----------------|--------------------------------|
| H1 Page title | 32   | 40   | 500     | -0.02em  | text           | --text-h1 / --leading-h1       |
| H2 Section    | 22   | 28   | 500     | -0.01em  | text           | --text-h2 / --leading-h2       |
| H3 Step       | 16   | 24   | 600     | 0        | text           | --text-h3 / --leading-h3       |
| Lead          | 15   | 24   | 400     | 0        | text           | --text-lead / --leading-lead   |
| Body          | 13   | 22   | 400     | 0        | text           | --text-body / --leading-body   |
| Callout title | 13   | 22   | 600     | 0        | text           | --text-body / --leading-body   |
| Nav item      | 13   | 22   | 400     | 0        | text-secondary | --text-body / --leading-body   |
| Small         | 12   | 20   | 400     | 0        | text           | --text-small / --leading-small |
| Top nav / btn | 12   | 16   | 400/500 | 0        | varies         | --text-small / --leading-label |
| Label         | 11   | 16   | 500     | 0.08em   | text-muted     | --text-label / --leading-label |
| TOC item      | 11   | 18   | 400     | 0        | text-muted     | --text-label                   |
| Code          | 12   | 20   | 400     | 0        | code or text   | --font-mono / --text-small     |
| Inline code   | 11   | 18   | 400     | 0        | text-secondary | --font-mono / --text-label     |

All sizes and line heights are in px. Line heights are fixed px values on the 4px grid
(exceptions: 18px for TOC/inline code, 27px table headers, 39px code-tab labels
inside the 40px header; see 9).

### 4.3 Type rules

- Weights: 400 regular, 500 medium, 600 semibold. Never use 700+ or below 400.
- One H1 per page. H2 opens every page section. H3 is for steps and subsections only.
- Hierarchy = size + weight. Never make a heading a different colour from body.
- Sentence case for all headings, buttons, nav and titles ("Send your first request").
- UPPERCASE only for the Label style (section group labels, "On this page", "Next",
  code-block language labels, table headers). Always with 0.08em tracking.
- Numbered steps put the number in the H3 text: "1. Create your account".
- Max prose width is the content column (728px). Do not set prose wider.
- No coloured text in prose. Emphasis is weight 600, sparingly.

## 5. Spacing & layout

### 5.1 Spacing scale (4px base)

| Token      | Value | Typical use                                    |
|------------|-------|------------------------------------------------|
| --space-1  | 4px   | Icon gaps, chip padding, nav item vertical pad |
| --space-2  | 8px   | Title to text, nav item inset, split-btn pad   |
| --space-3  | 12px  | Table cells, code-header left pad, tab pad     |
| --space-4  | 16px  | Paragraph spacing, callout vertical pad        |
| --space-5  | 20px  | Callout horizontal pad, code body pad          |
| --space-6  | 24px  | Space above blocks (callout, code, table)      |
| --space-8  | 32px  | Nav group gap, step gap, column gutter         |
| --space-16 | 64px  | Space above each page section                  |

Rule: every margin, padding, gap, width and height is a multiple of 4. Padding is symmetric
unless content requires otherwise (documented per component).

### 5.2 Page grid

- Shared header, body and footer wrapper: width min(1280px, 100% - 64px),
  centred, using --site-max-width: 1280px. Below 760px, retain 20px side gutters.
- Three columns: sidebar 228px | content minmax(0, 1fr) | TOC 188px.
  The reading column fills the remaining width so the TOC reaches the wrapper edge.
- Column gap: 32px. Wrapper padding: 56px top, 96px bottom.
- Header spans full width above the grid (see 8.1).

### 5.3 Vertical rhythm (content column)

| Between                                 | Space                                     |
|-----------------------------------------|-------------------------------------------|
| Page title row -> lead                  | 16px                                      |
| Lead -> page header divider             | 32px (padding) + 1px border               |
| Paragraph -> paragraph                  | 16px                                      |
| Paragraph -> block (callout/code/table) | 24px above the block                      |
| Block -> following content              | 32px                                      |
| Previous content -> section             | 64px, then 1px border, then 32px, then H2 |
| H2 -> first content                     | 16px                                      |
| H3 step title -> step text              | 0 (line height carries it)                |
| Step -> next step                       | 32px                                      |
| Content -> pager footer                 | 64px, 1px border, 24px                    |

## 6. Radius, borders & depth

### 6.1 Radius

| Token         | Value | Use                                                                  |
|---------------|-------|----------------------------------------------------------------------|
| --radius-md   | 6px   | CANONICAL. Buttons, icon buttons, split button, sidebar pill,        |
|               |       | callouts, code blocks, tables, pager cards - every container/control |
| --radius-sm   | 4px   | Inline code chips only                                               |
| --radius-none | 0px   | Reserved: full-bleed dividers only. No component uses it.            |

Rules:
- Never mix radii inside one component. Children of a rounded container are clipped
  (overflow: clip), not individually rounded.
- Tables: one rounded 1px outer border; internal cell lines are single 1px borders
  (top on rows, left on the second column). Never border every cell.

### 6.2 Borders

- All borders are 1px solid. The only 2px line is the active TOC marker.
- Default colour `--color-border`; controls use `--color-border-control`.
- No coloured left bars, no 3px accents, no dashed borders in production UI.

### 6.3 Depth

- Borders only. No box-shadow anywhere (including offset "hard" shadows).
- Elevation is expressed by one surface step: bg -> panel -> element -> control -> hover.
- No gradients, blurs, glows or transparency effects.

## 7. Components - controls

The guide defines components, not copy. Button labels are chosen per page using the rules
below; never treat an example label as part of the spec.

### 7.1 Button

| State    | Fill                    | Border                 | Label and icon           |
|----------|-------------------------|------------------------|--------------------------|
| Default  | --color-surface-control | --color-border-control | --color-text, 12/16, 500 |
| Hover    | --color-surface-hover   | --color-border-control | --color-text             |
| Focus    | --color-surface-control | --color-accent         | --color-text             |
| Disabled | --color-surface-panel   | --color-border         | --color-text-muted       |

- Min height 32px. Padding 8px 16px. Radius 6px. Content centred.
- Never an accent fill. Never white/inverted.
- At most one primary button per view: the single most important action on the page
  (in docs, the header action).

Labels:
- Sentence case, verb first, 1-3 words. Describe the action, not the destination
  ("Download", not "Go to downloads").
- No trailing punctuation, no emoji, no product-name-only labels.

Icons in buttons:
- Optional. Add one only when it makes the action faster to recognise (download,
  external link, add, copy). If removing the icon loses no meaning, remove it.
- Leading position (left of the label) for the action's icon. Trailing position only
  for direction or disclosure: a chevron for a menu, an arrow for navigation, an
  external-link glyph for leaving the site.
- One icon per button. Never both leading and trailing.
- Size 14px. Gap between icon and label 8px (--space-2).
- The icon always uses the label colour and changes with it in every state (hover,
  focus, disabled). Never muted, accent or status inside a labelled button.
- A primary button always has a text label. It is never icon-only.

### 7.2 Icon-only button

- Use only for frequent, universally recognised actions next to the thing they act on
  (copy, close, more). Anything else gets a text label.
- 28x28px square, radius 6px, fill --color-bg, border --color-border.
- 14px icon, --color-text-muted, centred, no padding.
- Hover: fill --color-surface-hover, border --color-border-control, icon --color-text.
- Must carry an accessible name (aria-label and tooltip) stating the action.

### 7.3 Split button

- A primary action segment plus a menu segment for related actions.
- Container: 1px --color-border, radius 6px, clipped.
- Action segment: icon-only (32x24px, icon centred) or icon + label per 7.1.
- Menu segment: trailing chevron, padding 4px 8px, 1px left divider --color-border.
- In the page header it sits on the same row as the H1, right-aligned, vertically centred.

## 8. Components - navigation

### 8.1 Header

- Height 86px. Inner wrapper matches the body's 1280px maximum and side gutters
  (5.2); no additional inset padding. Gap 32px. Fill --color-bg.
- Bottom edge: 1px --color-divider.
- Left: whipcode wordmark (SVG, 28px tall, fill --color-text).
- Match Paper node `Y8-0`: no text navigation, site menu or theme control in the header.
- Right: Discord then GitHub icon links in a 4px-gap group, then a 12px gap to
  the Download button. No divider between the icon links and Download.
- Download has a leading 14px/2px-stroke icon, 8px icon/label gap, and 34px rendered
  height. It links to `/docs/download`; release links belong in that page.
- Discord uses the user-provided invite `https://discord.gg/K2deYSXNu`.
- Keep Download visible on mobile; below 370px use the named icon-only control
  to avoid crowding. Docs navigation remains in the docs shell, not the header.
- Theme selection lives in the footer; system preference and persistence remain.

### 8.2 Top nav link

| State   | Colour                 | Weight |
|---------|------------------------|--------|
| Default | --color-text-secondary | 400    |
| Hover   | --color-text           | 400    |
| Active  | --color-text           | 500    |

- 12/16. No underline, no dot, no pill, no accent. Active = colour + weight only.

### 8.3 Sidebar

- Column 228px. Right border 1px --color-border running full page height.
- 16px between items and the right border.
- Groups: stacked, 32px apart. Group label (Label style) then items, 8px between.
- Group label inset: 8px left (aligns with item text).
- Items: 13/22, padding 4px 8px, 2px gap between items, full column width.

| State   | Fill                    | Text                        | Radius |
|---------|-------------------------|-----------------------------|--------|
| Default | none                    | --color-text-secondary, 400 | -      |
| Hover   | --color-surface-panel   | --color-text, 400           | 6px    |
| Active  | --color-surface-control | --color-text, 400           | 6px    |

- No left bar, no accent, no chevrons, no arrows, no row dividers.

### 8.4 On this page (TOC)

- Title "On this page" in Label style, 12px above the list.
- List sits on a 1px --color-border left rail.
- Items: 11/18, padding 8px 12px.
- Default: --color-text-muted, 400.
- Active: --color-text, 500, with a 2px --color-text bar on the rail (overlaps the rail).
- No accent colour, no pill.

### 8.5 Pager card (Next / Previous)

- Panel fill, 1px --color-border, radius 6px, padding 16px, min height 76px.
- Right-aligned stack: "Next" (Label) over page name (13/20, 500, --color-text),
  then a 15px arrow icon (--color-text-secondary), gap 12px.
- Hover: fill --color-surface-element, border --color-border-control. Lift the
  label to --color-text-secondary on hover: muted on the raised Dark surface is
  below 4.5:1. This accessibility normalization is intentional.
- Sits in a 2-column grid under a 1px top border, 64px below content.

### 8.6 Community links

Small icon links to external community spaces (GitHub, Discord, and similar). These are
guidelines; adjust when a layout needs it, but keep the spirit: quiet, recognisable, and
clearly secondary to the primary button.

- Usually sit in the header, between the nav links and the primary button. A thin
  1px x 16px divider (--color-border) before the primary button helps separate
  "community" from "action", but drop it if the header already feels busy.
- Use each service's official mark, not a redrawn line icon. Brand marks are the one
  place filled icons are fine. Around 16px reads well next to 12px nav text.
- Give each mark a comfortable hit area (about 32x32px, radius 6px) and keep them
  close together (around 4px) so they read as one group.
- Default colour matches the nav links (--color-text-secondary), so they sit with the
  nav rather than competing with the primary button. Avoid the services' own brand
  colours (Discord blurple, etc.); they pull focus in a neutral header.
- On hover, lift the mark to --color-text and add a soft --color-surface-hover fill,
  like other icon-only controls.
- Keep it to a few links. If you need more than three or four, move them to a footer
  or a "Community" page instead.
- Always give each link an accessible name ("GitHub", "Discord") since there is no
  visible label.

## 9. Components - content

### 9.1 Callouts

| Type    | Border token                   | When                                           |
|---------|--------------------------------|------------------------------------------------|
| Info    | --color-callout-info-border    | Default. Context, prerequisites, neutral notes |
| Tip     | --color-callout-tip-border     | Optional shortcut or best practice             |
| Warning | --color-callout-warning-border | Something easy to get wrong or lose            |
| Alert   | --color-callout-alert-border   | Destructive / irreversible. Use sparingly      |

Anatomy (all types identical except the border colour):
- Fill --color-surface-panel. 1px border (type token) on all four sides. Radius 6px.
- Padding 16px 20px. Gap 8px between title and body.
- Title: 13/22, 600, --color-text. Body: 13/22, 400, --color-text.
- No icon. No left bar. No coloured title. No coloured fill.
- Max one callout per step. Never stack two callouts back to back.

### 9.2 Code block

- Fill --color-surface-panel, 1px --color-border, radius 6px, clipped.
- Header: min height 40px, padding 0 8px 0 12px, 1px bottom border, space-between.
  - Single language: Label-style language name ("BASH") on the left.
  - Tabbed: retain the 12px left header inset shown in page nodes `12G-0` and
    `13A-0`; tabs add their own 12px label padding. Gap to copy control: 16px.
  - Icon-only copy action on the right (7.2). Normalize embedded copy controls to
    the canonical 6px icon-button radius (the isolated board export omitted it).
- Body: padding 20px, mono 12/20, white-space pre, tab size 2.
- Fenced code uses build-time Refractor tokenization. No runtime parser or grammar
  download. Unknown languages remain escaped plain text; original source is retained
  for copying, including Unicode, whitespace, shell escapes and trailing newline.
- Syntax uses dedicated Carbonfox roles, not the UI focus token. Unmapped tokens
  remain full-strength text. Nested tokens reset parent colour before their own role.

| Syntax role | Dark | Light |
| --- | --- | --- |
| keyword | #BE95FF | #6929C4 |
| string | #25BE6A | #198038 |
| number / boolean | #3DDBD9 | #007D79 |
| comment | #7D848F | #6F6F6F |
| function | #8CB6FF | #0043CE |
| type / builtin | #08BDBA | #007D79 |
| operator | #A9AFBC | #525252 |
| punctuation | #A9AFBC | #161616 |

`--color-syntax-<role>` and `--light-color-syntax-<role>` switch with the site theme.
All pairings meet unrounded 4.5:1 on the panel; do not reduce opacity. The colour
source is `internal/theme/themes/carbonfox.json` / `carbonfox-light.json`, copied
into app-owned CSS rather than imported from product UI.

### 9.3 Code tabs

- Label style (11px, 500, 0.08em, uppercase), line height 27px, vertically centred
  within the 40px header, padding 0 12px. This follows page node `12J-0`; the
  component-board sample's 39px line height is not the page typography.
- The horizontal tab scroller extends over the header border so it does not clip
  the active underline. The 20px body inset remains identical in all variants.
- Default: --color-text-muted. Active: --color-text-secondary with a 1px
  --color-text-secondary underline sitting on the header border.

### 9.4 Inline code

- Match whip's transcript (`packages/app/src/timeline.tsx`): mono at 0.85em,
  inherited line height, green `--color-code` text. The relative size also applies
  within tables and callouts; do not add vertical padding that expands line boxes.
- Fill `--color-inline-code-background` (the element surface: #1E1E1E dark,
  #F4F4F4 light), no border, radius 4px, padding 0 4px.
- Allow long identifiers to wrap; clone the background/padding across wrapped lines.
- Use for status codes, header names, env vars, file names, identifiers. Fenced
  code blocks retain their existing padding, typography and multicolour syntax.

### 9.4.1 Content links

- Blue `--color-link`: #33B1FF dark / #0072C3 light, matching Carbonfox's link role.
- Underline stays visible at rest, hover and focus; underline offset 3px. Keyboard
  focus retains the normal focus ring. Linked inline code remains green, with
  the link underline marking it as interactive.
- Applied to prose links in paragraphs, lists, tables and callouts; `.text-link`
  provides the same library treatment outside prose. Nav, cards, pager links,
  social icons and buttons do not inherit it.
- The user's transcript reference supersedes the original board's neutral links
  and bordered grey inline-code chips. Both themes must meet 4.5:1 contrast.

### 9.5 Table

- One 1px --color-border outer border, radius 6px, clipped.
- Header row: fill --color-surface-element, Label style, 600, line height 27px,
  padding 6px 12px.
- Body cells: padding 12px, Small (12/20), --color-text.
- Row separators: 1px top border. Column separator: 1px left border on column 2.
- First column may hold an inline-code chip or plain text.

## 10. Page anatomy

1. Header (8.1)
2. Grid: sidebar | content | TOC (5.2)
3. Page header: H1 + split button on one row -> 16px -> lead -> 32px -> 1px divider.
   No eyebrow, breadcrumb, "Updated" date or status dot above the H1.
4. Intro paragraph(s), optional Info callout.
5. Sections: 64px -> 1px divider -> 32px -> H2 -> content.
6. Steps inside a section: H3 "N. Title" -> body -> optional callout/code -> 32px.
   No step rail, no circled numbers, no connecting lines.
7. Closing sentence -> pager footer (8.5).

Both modes are built from the same structure; only tokens change.

The full-article Storybook specimen preserves Paper `Y3-0` with whipcode-specific
copy. Public V1 pages use the same title, copy/download controls, TOC and
sequential pagination. Quickstart is the installation-first docs entry and uses
exactly the same typography and spacing as every other public article, with no
page-specific layout class or CSS overrides. The shared title divider has 32px
bottom padding; section dividers have 64px top margin and 32px top padding.
The first H2 has no extra rule because the title already provides one; do not
add standalone rules between sections.
Download uses a bordered platform/action row and existing code tabs. Most other
pages remain heading-only outlines. The preserved specimen:
- Intro then the Before you start callout; five numbered setup steps; terminal
  examples; workspace scope; troubleshooting table; next-page CLI card.
- H1 is Getting started; the existing left-nav label remains Get started.
- Its TOC lists H2 sections only. Step H3 anchors remain available to direct links.
- Title-row copy control copies the original MDX; the chevron opens Download page
  source. The source stays in a page-local chunk, not global navigation metadata.
- Page action hit targets are 28px high (30px outer), a deliberate accessibility
  increase from Paper's 24px controls; width, 6px corner and neutral border match.
- Paper article spacing: 36px after the header divider, 24px before callout/code,
  12px from step heading to body, 32px between steps, 64px before sections,
  32px section padding and 24px above the final pager.
- Hide the script-only page actions without JavaScript; all text and tabbed
  examples remain readable. The header and 1280px shared wrapper are unchanged.

## 11. Iconography

- Single line icon set (Lucide style): 24x24 viewBox, 2px stroke, round caps and joins,
  no fill.
- Render sizes: 14px in all controls, 15px for the pager arrow.
- Colour depends on context:
  - Standalone icons (icon-only buttons, split-button action, pager arrow):
    --color-text-muted default, --color-text on hover.
  - Icons inside a labelled button: same colour as the label, in every state (7.1).
  - Never accent or status colours.
- Brand marks (GitHub, Discord, etc.) are the exception to the line-icon style: use
  the official filled mark, roughly 16px, in the surrounding text colour (see 8.6).
- Icons clarify actions (copy, chevron, arrow). No decorative icons, no icons in callouts,
  headings or nav items. No emoji.

## 12. Voice & content

- Direct, second person, present tense: "Create an API key", "Set the key in your shell".
- Headings are tasks or nouns, sentence case, no trailing punctuation.
- One idea per paragraph; paragraphs of 1–3 sentences.
- Put identifiers in inline code; put commands in code blocks.
- Callout titles state the point in 2–5 words ("Store it now").

## 13. Light & dark mode

- Every colour token has a light twin: `--color-X` <-> `--light-color-X`.
- Type, spacing, radius and layout tokens are shared by both modes.
- Icons and the wordmark must switch stroke/fill with the mode (--color-text family).
- Build a component once, then swap tokens. Never hand-pick a light hex.

## 14. Accessibility

- AA contrast for all text (see 3). Controls retain the board's 1px focus-colour
  border; keyboard focus also gets a 2px offset outline for clear visibility.
- Base UI handles menu, tooltip, tabs and mobile-dialog keyboard/focus behaviour.
  Arrow keys activate code tabs; copy always uses the active original snippet.
  Failure tells the reader to select/copy manually; success is a polite status.
- Without JavaScript all tabbed snippets render with labels; copy/theme controls
  are omitted and mobile navigation uses native details. System colour preference
  works through CSS; script-enhanced mode stores light/dark/system locally.
- Docs navigation collapses below 760px; TOC moves above the article below 1100px.
  Validate 390px, 768px and desktop widths. No horizontal page overflow.
- Minimum text size 11px. Minimum hit target 28x28px.
- Active sidebar items retain the inactive font weight; a neutral background and
  brighter text show selection, with `aria-current` for assistive technology.
  Active TOC items also show a bar; callout types differ by title text.

## 15. Don'ts

- No drop shadows, glows, gradients, or offset "hard" shadows.
- No blue for navigation active states, buttons, callout titles or markers;
  content links use their dedicated blue role.
- No coloured left bars on callouts, no callout icons, no coloured callout titles.
- No white/inverted primary buttons.
- No grey body copy.
- No mixed radii; no square containers.
- No values off the 4px grid except documented type line heights, 1px borders,
  the measured 86px header, and 34px rendered button height (16px line + 8px
  vertical padding on each side + borders).
- No step rails or eyebrows above the H1.
- No icon-only buttons without an accessible name; no icon that repeats the label.
- No specific button copy in specs or tokens; labels follow 7.1.
- No fonts other than system sans and system mono.

## 16. Checklist before shipping a design

- [ ] Every colour is a token (no raw hex).
- [ ] Prose and headings use --color-text; nav/meta use secondary/muted.
- [ ] All spacing is on the 4px grid and matches the rhythm table (5.3).
- [ ] Every container and control has a 6px radius; inline code 4px.
- [ ] No shadows; depth from borders and surface steps only.
- [ ] Focus, prose links and syntax use their own semantic colour roles.
- [ ] Inline code is green on a borderless subtle surface, with no vertical padding.
- [ ] Real MDX fences retain token spans and exact source; no runtime highlighter.
- [ ] Active-tab copy, copy failure, no-JS snippets and keyboard behaviour pass.
- [ ] Syntax contrast is asserted without rounding in both colour modes.
- [ ] Board Reference stories include every variant and dark/light modes.
- [ ] Button icons are 14px, leading, one per button, and match the label colour.
- [ ] Callouts use the right type border and white titles.
- [ ] Both dark and light versions exist and pass contrast.
