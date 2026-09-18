# Add the RLM themes to main

Branch: `feat/themes`

## Goal

Bring the built-in theme catalog and theme picker from `origin/whip-rlm` into
the current main TUI. Users can choose a named theme with `/theme` or
`/theme <name>`, preview it in the picker, and persist the choice in the existing
`Config.Theme` field.

Keep `auto`, `light`, and `dark` backward compatible. Use the current Bubble Tea,
Lip Gloss, and Glamour v1 renderer; this is not a port of the RLM application.

## Non-goals

- RLM runtime, daemon, protocol, web UI, SDK, or generated TypeScript themes.
- Bubble Tea/Lip Gloss v2 or the RLM full-screen/cell-buffer shell.
- User-authored themes under `~/.whip/themes` in this pass.
- Layout changes or whole-screen background painting in inline mode.

## Source

Use the latest built-in definitions and JSON catalog from
`origin/whip-rlm:internal/theme`, including the Claude Code theme. Use
`origin/feat/rlm-runtime-u4-clean` only as a behavioral reference for live
picker preview and cancel/commit handling.

Do not cherry-pick the RLM TUI commits: they are tied to the v2 renderer. Port
the renderer-independent theme data and write a small adapter for main's v1
styles.

## Plan

### 1. Port the built-in theme catalog

Add `internal/theme` with only the pieces needed by the terminal:

- semantic palette roles (`text`, `muted`, `primary`, `success`, etc.);
- light, dark, neutral, and embedded named theme specs;
- strict JSON validation and deterministic builtin lookup;
- syntax/markdown color resolution and surface derivation;
- the RLM branch's `themes/*.json` catalog.

Skip custom-file discovery and browser-specific catalog APIs for now. Keep any
browser-only fields in the JSON schema only when required to load the source
files; the TUI ignores them.

Tests: every embedded theme validates, names are unique, ordering is stable,
and derived colors/surfaces match representative dark and light themes.

### 2. Adapt themes to the current TUI renderer

Add `internal/tui/theme_active.go` to resolve the active theme from:

- the existing terminal background detection for `auto`;
- an explicitly selected builtin name;
- neutral fallback when the terminal background is unknown.

Refactor current global styles in `internal/tui/tui.go`, Markdown/Chroma styling
in `internal/tui/markdown.go`, and opencode colors in
`internal/tui/opencode.go` to consume semantic roles. Keep the existing layout
and only apply background fills where the current TUI already paints a local
panel, prompt, code block, or selected row.

Theme changes must invalidate cached Markdown and transcript rendering so a
previous Chroma palette cannot leak into the new theme.

Tests: switch dark → light → named theme → dark and verify normal text,
Markdown, fenced code, spinner/input, selected rows, and opencode panels all
change without stale ANSI colors or geometry changes.

### 3. Wire selection and persistence

Generalize the existing theme paths rather than adding a second settings flow:

- `Config.Theme` stores a builtin theme ID; empty remains `auto`.
- `/theme <name>` validates, applies, and saves a theme.
- `/theme auto` clears the persisted field and resumes background detection.
- Invalid names leave the current theme/config unchanged and show the valid
  usage.
- Config watcher updates continue to apply cross-session theme changes.
- Startup with a removed/invalid name falls back safely and reports it once.

Update `internal/tui/palette.go` so the current Theme panel lists `auto`,
`light`, `dark`, then the named catalog. Moving through rows previews themes;
Escape restores the previous theme; Enter persists the selected ID. Add compact
color swatches if they fit without complicating narrow-terminal rendering.

Tests: direct command, config round trip, watcher update, picker filtering,
preview/cancel, preview/commit, and short-terminal windowing across the full
catalog.

### 4. Document and validate

- Document named built-in themes and `/theme` in `docs/features.md`.
- Check the JSON-theme roadmap item only for the shipped builtin-catalog scope,
  or rewrite it to leave custom variant files explicitly open.
- Run `gofmt`, `git diff --check`, focused `internal/theme`, `internal/tui`, and
  `internal/config` tests, then `task check`.
- Manually verify default and opencode modes under dark, light, auto/unknown,
  resize, picker preview/cancel, and process exit restoration.

## Suggested implementation order

1. Theme specs, embedded catalog, and pure tests.
2. v1 TUI adapter plus style/cache migration.
3. Commands, config, picker preview/persistence, and docs.

This can be one feature PR, but the three commits should remain separable for
review and bisection.

## Definition of done

- The themes shipped on `whip-rlm` are selectable on main.
- Existing `auto`, `light`, and `dark` behavior remains compatible.
- Theme switching updates all current TUI color surfaces without stale renders.
- Picker preview is reversible and only Enter persists a choice.
- No RLM runtime or v2 renderer code is introduced.
