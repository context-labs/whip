# Mermaid image rendering: integration traps

Observed while adding chat diagrams with `beautiful-mermaid@1.1.3`,
`elkjs@0.11.1`, Vite 8, and a strict production CSP. Current architecture and
limits live in [frontend.md](../frontend.md#mermaid-diagrams); this records why
some otherwise surprising integration choices exist.

## Validate the actual worker, not just SVG output

A Node render followed by a browser Blob-image paint passed while the real
Vite-built worker failed. ELK's bundled FakeWorker export detection depends on
`document`/`self`; Beautiful Mermaid's construction-time workaround runs too late
for nested module evaluation and can fail to restore worker globals on errors.
The narrow worker bootstrap supplies descriptors for a fixed two-node warmup and
restores them in `finally`. Keep it version-specific and rerun real workers when
upgrading; never apply this workaround on the main window.

Installed source packages are another boundary. Workspace development passed,
but the same worker inside npm-installed `@whip/ui` imported raw ELK CommonJS and
failed during module loading. Explicit `optimizeDeps.include: ['beautiful-mermaid']`
fixes dependency discovery without eager runtime loading. The packed consumer
probe now renders a real diagram in both production and development.

## Parse success does not establish diagram fidelity

This renderer intentionally accepts a subset of Mermaid, and its parsers can
silently ignore statements instead of throwing. Concrete examples included late
node labels, activation syntax, reserved-prefix sequence participant names,
pre-message notes, and ER comments rendered only as inaccessible inner SVG
tooltips. The small app-owned acceptance policy rejects unreviewed constructs;
regression fixtures assert actual visible output, not merely a truthy SVG.
Do not extend the policy into a second Mermaid parser or claim family-wide
compatibility from one happy path.

## Image isolation changes fonts, accessibility, and export

An SVG image does not inherit page fonts or CSS variables, and its internal DOM
is not available to the host's accessibility or event systems. Embed bundled
fonts, resolve theme colors, retain original source, and do not present tooltip-
only information as visible content. No raw SVG download/open action is included:
a document context has different security properties from an image context.

Axe sees the host image, not every diagram label. Contrast assertions must cover
actual theme roles and upstream private text styles: class visibility symbols
used a fixed 25% foreground blend despite a correct muted palette. Map such
text-bearing styles explicitly and test all catalog themes.

## Distinguish screenshot machinery from product CSP violations

Playwright's WebKit `inPagePrepareForScreenshots` unconditionally inserts a
`<style>body {}</style>` to synchronize animations. Under `style-src 'self'` this
logs a console CSP error only around screenshot calls, even with caret hiding
disabled. It is not a reason to relax application CSP. Exact canvas background
pixels and Inter/system font comparisons, plus normal render/interaction network
and violation checks, distinguish this harness behavior from a broken image.
The browser runner records this limitation rather than concealing it.

## Keep read position and input targets stable

Diagrams finish asynchronously and may resize a virtual row above the reader.
Use the existing measured reading anchors, not a new scroll-to-bottom effect.
Keep explicit source views independent by structural fence identity, even for
identical source. A changing Copy label can also shrink a right-aligned toolbar
between pointer-down and pointer-up; reserve its footprint rather than patching
click timing.
