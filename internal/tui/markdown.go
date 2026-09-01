package tui

import (
	"regexp"
	"strings"
	"sync"

	chromaStyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

// renderMarkdown renders assistant message text as rich terminal markdown
// (glamour): headings, bold/italic, lists, fenced code blocks, tables.
// Falls back to the raw input when parsing fails — a degraded transcript is
// never worth a broken one.
//
// The style is picked by mdStyle from the background detected at startup
// (never WithEnvironmentConfig: an OSC background query mid-session can hang
// over mosh/tmux — see detectColorScheme).
func renderMarkdown(s string, width int) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	width = max(width, 8) // glamour treats width<=0 as its ~80-col default
	out, err := mdRenderer(width).Render(s)
	if err != nil {
		return s
	}
	rendered := stripLinePadding(strings.Trim(out, "\n"))
	linked := hyperlinkGlamourLinks(rendered, realFileExists)
	linked = linkifyRenderedFilePaths(linked, realFileExists)
	return wrapWideLines(linked, width)
}

// wrapWideLines hard-wraps any rendered line still wider than width.
// Glamour never breaks code-fence or table content, so a long line overflows
// the terminal; ansi.Hardwrap is cell- and escape-aware (styles stay intact).
func wrapWideLines(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > width {
			lines[i] = ansi.Hardwrap(l, width, true) // ANSI-aware, breaks mid-word
		}
	}
	return strings.Join(lines, "\n")
}

// padStripRE matches glamour's right-padding at end of line: runs of (SGR
// sequence [empty params allowed — bare \x1b[m], spaces), optionally closed
// by a final SGR reset. The reset is kept (captured group) so a line's
// styling never bleeds into the next block.
var padStripRE = regexp.MustCompile(`(?:\x1b\[[0-9;]*m[ \t]*)+(\x1b\[[0-9;]*m)?$`)

// stripLinePadding removes glamour's right-padding: it pads every line to
// the full render width with individually styled spaces, which bloats the
// transcript 10-20x and breaks terminal select/copy. Lines whose visible
// content is empty (blank separators) become truly empty — no styled blank
// rows. Leading indentation and styled content are untouched.
//
// Every surviving styled line is made SELF-TERMINATING (ends in \x1b[0m):
// glamour often places a line's closing reset inside the padding we just
// stripped (heading lines especially), and a line left un-reset bleeds its
// color into every following line until the next SGR — a blue heading painted
// a whole table blue. cleanLine owns both steps so sanitizeView (the per-frame
// pass) applies the same rule via selfTerminate.
func stripLinePadding(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = padStripRE.ReplaceAllString(l, "$1")
		if ansi.StringWidth(l) == 0 || strings.TrimSpace(ansi.Strip(l)) == "" {
			l = "" // blank separator line: drop any leftover styling entirely
		} else {
			l = selfTerminate(l)
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// selfTerminate closes any styled line with a full reset so its style can
// never bleed into the line below.
func selfTerminate(l string) string {
	if strings.Contains(l, "\x1b[") && !strings.HasSuffix(l, "\x1b[0m") {
		l += "\x1b[0m"
	}
	return l
}

var (
	mdMu          sync.Mutex
	mdAtWidth     int
	mdAtLight     bool // theme the cached renderer was built for
	mdAtKnown     bool // whether the cached renderer was built with a known bg
	mdRendererC   *glamour.TermRenderer
	mdRendererErr bool   // style init failed once: don't retry per message
	mdLight       bool   // light terminal background detected (set at startup)
	mdKnown       bool   // background was actually determined; false = no good signal
	mdScheme      string // explicit scheme ("light"/"dark"); "" = follow detection
)

// applyLight/applyDark/applyUnknown drop the cached renderer so the next
// render rebuilds with the matching style. They do NOT touch the detected
// terminal background (mdLight/mdKnown) — that belongs to detectColorScheme.
// Splitting the two is what lets an explicit /theme pick override detection
// without corrupting it (auto must still resolve from the real background).
// SetLightTheme records the terminal's background and drops the cached
// renderer so the next render builds with the matching style. Called from
// Run once the background is known (OSC query result or heuristic).
func SetLightTheme(light bool) {
	mdMu.Lock()
	mdLight, mdKnown = light, true
	mdRendererC, mdAtWidth = nil, 0
	mdMu.Unlock()
}

// SetUnknownTheme records that the terminal background could NOT be determined
// (auto mode with no reliable signal: tmux without passthrough, mosh, a
// terminal that ignores OSC 11). Markdown then renders in neutralStyle — full
// markdown structure, but only terminal-palette ANSI colors — so nothing is
// inverted by a wrong dark/light assumption.
func SetUnknownTheme() {
	mdMu.Lock()
	mdKnown = false
	mdRendererC, mdAtWidth = nil, 0
	mdMu.Unlock()
}

// setSchemeOverride records an explicit scheme pick ("light"/"dark", "" = back
// to detection) for CurrentTheme reporting.
func setSchemeOverride(s string) {
	mdMu.Lock()
	mdScheme = s
	mdMu.Unlock()
}

// CurrentTheme reports the active scheme ("light"/"dark"/"auto") for the UI.
// An explicit pick wins; otherwise it follows detection, where "auto" means
// the background wasn't determined and markdown is rendering in the neutral
// default style.
func CurrentTheme() string {
	mdMu.Lock()
	defer mdMu.Unlock()
	if mdScheme != "" {
		return mdScheme
	}
	if !mdKnown {
		return "auto"
	}
	if mdLight {
		return "light"
	}
	return "dark"
}

// unregisterChromaStyle drops glamour's global chroma style ("charm").
// Glamour registers it once per process, guarded by "if not present" — so
// the FIRST theme to render a code block wins forever and a later theme
// switch keeps the wrong syntax colors (a light render poisons every later
// dark render with color 235). Deleting the entry on theme change lets the
// next render register the right palette.
func unregisterChromaStyle() {
	delete(chromaStyles.Registry, "charm")
}

// invalidateMDRenderer drops the cached markdown renderer so the next render
// rebuilds it — used when the UI mode toggles (opencode markdown style differs).
func invalidateMDRenderer() {
	mdMu.Lock()
	mdRendererC, mdAtWidth = nil, 0
	mdMu.Unlock()
}

// mdStyle picks the glamour style for the detected background. The light
// variant gets a higher-contrast inline-code treatment: stock Light uses
// salmon (203) on near-white (254), which is nearly unreadable — dark red on
// a light-gray chip instead. When the background is unknown (mdKnown false —
// auto mode with no reliable signal), it uses neutralStyle so nothing assumes
// a dark or light background.
//
// Tables: stock Dark/Light ship an empty StyleTable, leaving separator
// choice to lipgloss defaults. Pin the separators explicitly (column pipes +
// box-drawing joints on the header rule) so a lipgloss default change can't
// silently unformat tables, and drop the per-cell margin to one space —
// glamour's default cell padding wastes ~4 columns per cell, which is the
// difference between a readable table and wrapped mush at narrow widths.
func mdStyle() glamouransi.StyleConfig {
	if ocActive && mdKnown { // unknown bg → fall through to neutralStyle (no light/dark assumption)
		return opencodeMDStyle(mdLight)
	}
	var st glamouransi.StyleConfig
	switch {
	case !mdKnown:
		st = neutralStyle()
	case mdLight:
		st = styles.LightStyleConfig
		st.Code.Color = new("124")           // dark red
		st.Code.BackgroundColor = new("255") // lightest gray chip
	default:
		st = styles.DarkStyleConfig
	}
	st.Table.ColumnSeparator = new("│")
	st.Table.CenterSeparator = new("┼")
	st.Table.RowSeparator = new("─")
	zero := uint(0)
	st.Table.Margin = &zero
	return st
}

// opencodeMDStyle renders assistant markdown in opencode's palette (both
// theme variants), so the body text and inline styles match opencode
// pixel-for-pixel. Document.Margin is left at glamour's default 2 because the
// assistant indent math (indentLines) accounts for it.
func opencodeMDStyle(light bool) glamouransi.StyleConfig {
	pick := func(dark, lt string) *string {
		s := dark
		if light {
			s = lt
		}
		return &s
	}
	st := styles.DarkStyleConfig
	if light {
		st = styles.LightStyleConfig
	}
	st.Document.Color = pick("#eeeeee", "#1a1a1a") // markdownText (no background: the main area stays terminal-native)
	st.Heading.Color = pick("#9d7cd8", "#d68c27")  // markdownHeading (accent)
	st.H1.Color = pick("#9d7cd8", "#d68c27")
	st.H1.BackgroundColor = nil
	st.Code.Color = pick("#7fd88f", "#3d9a57") // markdownCode (green)
	st.Code.BackgroundColor = pick("#1e1e1e", "#f5f5f5")
	st.Link.Color = pick("#fab283", "#3b7dd8")     // markdownLink
	st.LinkText.Color = pick("#56b6c2", "#318795") // markdownLinkText (cyan)
	st.Strong.Color = pick("#f5a742", "#d68c27")   // markdownStrong (orange)
	st.Emph.Color = pick("#e5c07b", "#b0851f")     // markdownEmph (yellow)
	st.Item.Color = pick("#fab283", "#3b7dd8")     // markdownListItem
	st.Table.ColumnSeparator = new("│")
	st.Table.CenterSeparator = new("┼")
	st.Table.RowSeparator = new("─")
	zero := uint(0)
	st.Table.Margin = &zero
	return st
}

// neutralStyle is the unknown-background style: auto mode with no reliable
// signal — e.g. mosh+tmux, where the OSC 11 query is structurally unanswerable
// (mosh's terminal emulator doesn't implement it, so neither tmux nor the
// passthrough copy ever gets a reply). The old fallback here was glamour's
// ASCII style, which reads as broken: literal ## headings, kept ** markers,
// raw table pipes, zero color.
//
// This keeps the dark style's STRUCTURE (styled headings, italic/bold, • items,
// box-drawing tables) but drops or remaps every color that assumes a dark
// background to a basic ANSI color (0–15) — those come from the terminal's own
// palette, so they stay legible on any background. Code blocks render without
// syntax highlighting: chroma's fixed hex palettes need a known background.
func neutralStyle() glamouransi.StyleConfig {
	st := styles.DarkStyleConfig
	st.Document.Color = nil // terminal default foreground
	st.Heading.Color = new("4")
	st.H1.Color, st.H1.BackgroundColor = nil, nil // no color chip
	st.H1.Prefix, st.H1.Suffix = "# ", ""
	st.H6.Color = nil
	st.HorizontalRule.Color = new("8")
	st.Link.Color = new("4")
	st.LinkText.Color = new("6")
	st.Image.Color = new("4")
	st.ImageText.Color = new("8")
	st.Code.Color = new("1") // inline code: ANSI red, no chip
	st.Code.BackgroundColor = nil
	st.CodeBlock.Color = nil
	st.CodeBlock.Chroma = nil
	return st
}

// mdRenderer returns a cached renderer per width (glamour builds a
// style-traversed renderer per Render call otherwise).
func mdRenderer(width int) *glamour.TermRenderer {
	mdMu.Lock()
	defer mdMu.Unlock()
	if mdRendererErr {
		return nil
	}
	// Glamour registers its chroma style ("charm") in a process-global
	// registry, first-registration-wins — so a render under one theme leaves
	// that theme's syntax colors in place for every later render under the
	// other theme. The registry entry is keyed by name, not theme: drop it
	// whenever the cached renderer's theme isn't the current one, and also
	// when the entry's origin is unknown (first call after a theme flip).
	if mdRendererC != nil && mdAtWidth == width && mdAtLight == mdLight && mdAtKnown == mdKnown {
		return mdRendererC
	}
	unregisterChromaStyle()
	st := mdStyle()
	margin := uint(2)
	st.Document.Margin = &margin
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(st),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(), // streamed text keeps its line breaks verbatim
	)
	if err != nil { // style is built-in; only reachable on a broken build
		mdRendererErr = true
		return nil
	}
	mdRendererC, mdAtWidth, mdAtLight, mdAtKnown = r, width, mdLight, mdKnown
	return r
}

// bareSGR is the empty SGR escape (\x1b[m) lipgloss' Width().Render appends
// before its right-padding; some terminals render the empty parameter list
// inconsistently, and the styled pad shows up as visual smear. Normalize it
// to a proper reset.
var bareSGR = strings.NewReplacer("\x1b[m", "\x1b[0m")

// sanitizeView cleans one rendered screen: bare SGR escapes become real
// resets, trailing style+space tails (lipgloss/viewport padding) are trimmed
// from each line, and every styled line is re-closed with a reset — the trim
// can eat a line's own closing reset (glamour/lipgloss put padding after it),
// and an un-reset line bleeds its style into the rest of the frame. Unlike
// stripLinePadding this never blanks whitespace-only lines: a frame can carry
// intentionally styled blank rows (e.g. the drag-selection highlight).
func sanitizeView(s string) string {
	s = bareSGR.Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if !ocActive {
			// opencode mode: styled trailing spaces ARE the panel fills (user
			// cards) — stripping them collapses a full-width panel to a chip.
			// Markdown got its own padding stripped at render time either way.
			l = padStripRE.ReplaceAllString(l, "$1")
		}
		lines[i] = selfTerminate(l)
	}
	return strings.Join(lines, "\n")
}
