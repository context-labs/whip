package tui

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/lsp"
)

// opencode.go implements whip's "opencode" UI mode: an opt-in *structural*
// layout inspired by opencode's TUI (github.com/sst/opencode) — full-screen,
// a right-hand sidebar, and the block-glyph wordmark. It deliberately keeps
// whip's own theming (light/dark/auto) and colors; only the layout/structure
// changes. Enabled with config UIMode == "opencode" or via the command palette
// (Display → UI mode).

// opencodeMode is the config/UIMode value that selects this render mode.
const opencodeMode = "opencode"

// whipPlaceholder is whip's default prompt placeholder, restored when leaving
// opencode mode. Keep in sync with newInput.
const whipPlaceholder = "Ask whip anything… (/ for commands, tab completes)"

// ocActive mirrors m.uiMode == opencodeMode at package scope so block.render (a
// method on block, not model) can branch on the render mode. Set by applyUIMode.
var ocActive bool

// opencode's own theme palette (packages/tui/src/theme/assets/opencode.json),
// resolved against whip's OWN detected theme (mdLight/mdKnown) rather than
// lipgloss.AdaptiveColor — AdaptiveColor reads lipgloss's separate background
// detection, which desyncs from whip's in the auto/unknown case (SetUnknownTheme
// leaves lipgloss at its dark default), rendering dark panels on a light
// terminal. When the background is unknown, each role falls back to a
// terminal-palette-safe value (ANSI 0-15, or no fill) so nothing assumes
// light or dark — mirroring the markdown neutralStyle.
func ocPick(dark, light, neutral string) lipgloss.TerminalColor {
	mdMu.Lock()
	l, known := mdLight, mdKnown
	mdMu.Unlock()
	switch {
	case !known:
		if neutral == "" {
			return lipgloss.NoColor{} // transparent: no light/dark assumption
		}
		return lipgloss.Color(neutral)
	case l:
		return lipgloss.Color(light)
	default:
		return lipgloss.Color(dark)
	}
}

// ocPadTo pads content to width with spaces EXPLICITLY styled with the panel
// background. lipgloss's Style.Width padding lands after the nested segments'
// closing resets without re-opening the background, so padded panel rows
// rendered their tail on the terminal default — a text-width chip instead of a
// full-width panel.
func ocPadTo(content string, width int, bg lipgloss.TerminalColor) string {
	if pad := width - lipgloss.Width(content); pad > 0 {
		content += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
	}
	return content
}

// ocOnBg lays a pre-styled line ONTO the box background: the line's inner
// styles close with full resets, which drop back to the terminal-default
// background and punch bright chips through the panel. Re-open the box bg at
// the start and after every reset.
func ocOnBg(ln string, bg lipgloss.TerminalColor) string {
	seq := bgSeqOf(bg)
	if seq == "" || ln == "" {
		return ln
	}
	return seq + strings.ReplaceAll(ln, "\x1b[0m", "\x1b[0m"+seq) + "\x1b[0m"
}

// bgSeqOf extracts the raw SGR sequence that opens the given background
// ("" when the color is a no-op, e.g. NoColor on an unknown theme).
func bgSeqOf(bg lipgloss.TerminalColor) string {
	r := lipgloss.NewStyle().Background(bg).Render("x")
	i := strings.IndexByte(r, 'x')
	if i <= 0 {
		return ""
	}
	return r[:i]
}

// ocThemeKnown reports whether whip resolved the terminal background — glyph
// art that depends on a bg-matched color (the prompt's ▀ shadow) must skip
// rendering when it's unknown, or it draws in the default fg (a black bar on a
// light terminal).
func ocThemeKnown() bool {
	mdMu.Lock()
	defer mdMu.Unlock()
	return mdKnown
}

// ocBgShift derives a panel shade RELATIVE to the terminal's real background
// when the OSC 11 query captured its RGB: lightened on a dark background,
// darkened on a light one — opencode's own bg→panel→element stepping, but
// anchored to the actual bg so panels read as raised layers on ANY terminal
// (fixed constants assume opencode's near-black; on a #262a2e terminal they
// rendered as sunken holes).
func ocBgShift(delta int) (lipgloss.TerminalColor, bool) {
	if !bgCache.valid || !bgCache.hasRGB || !ocThemeKnown() {
		return nil, false
	}
	if rgbIsLight(bgCache.r, bgCache.g, bgCache.b) {
		delta = -2 * delta // darken, doubled: small deltas near white are invisible
	}
	c := func(v int) int { return min(max(v+delta, 0), 255) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c(bgCache.r), c(bgCache.g), c(bgCache.b))), true
}

// Light fills use deeper steps from opencode's own light ramp (step4/step5)
// rather than its literal panel values (#fafafa/#f5f5f5): a 2% delta from
// white is invisible in a terminal, which read as zero panel contrast.
// The dark constants below are the no-RGB fallback (explicit /theme, or mosh —
// which can't answer OSC 11, so the real bg is unknowable). They sit HIGHER
// than opencode's literal #141414/#1e1e1e: those assume a near-black terminal
// and render as sunken holes on the common #202830-ish dark schemes; #343434/
// #404040 read as raised panels across the whole dark range. When the real
// bg RGB was captured, ocBgShift supersedes these with exact relative shades.
func ocPanelBg() lipgloss.TerminalColor { // cards, sidebar (no fill if unknown)
	if c, ok := ocBgShift(10); ok {
		return c
	}
	return ocPick("#343434", "#ebebeb", "")
}

func ocElementBg() lipgloss.TerminalColor { // prompt box
	if c, ok := ocBgShift(20); ok {
		return c
	}
	return ocPick("#404040", "#e1e1e1", "")
}
func ocAgentCol() lipgloss.TerminalColor   { return ocPick("#5c9cf5", "#7b5bb6", "4") } // bars, ▣
func ocTextCol() lipgloss.TerminalColor    { return ocPick("#eeeeee", "#1a1a1a", "") }  // text (default fg if unknown)
func ocMutedCol() lipgloss.TerminalColor   { return ocPick("#808080", "#8a8a8a", "8") } // muted
func ocWarnCol() lipgloss.TerminalColor    { return ocPick("#f5a742", "#d68c27", "3") } // "+ Thought"
func ocSuccessCol() lipgloss.TerminalColor { return ocPick("#7fd88f", "#3d9a57", "2") } // footer bullet
func ocAccentCol() lipgloss.TerminalColor  { return ocPick("#9d7cd8", "#d68c27", "5") } // palette category headers
func ocSelBg() lipgloss.TerminalColor      { return ocPick("#fab283", "#3b7dd8", "7") } // selected row fill (primary)
func ocSelFg() lipgloss.TerminalColor      { return ocPick("#0a0a0a", "#ffffff", "0") } // selected row text

// sidebarWidth is the fixed width of the opencode-mode right sidebar, matching
// opencode (routes/session/sidebar.tsx). The sidebar shows only when the
// terminal is at least sidebarMinWidth columns wide, so a narrow terminal
// falls back to the single-column layout.
const (
	sidebarWidth    = 42
	sidebarMinWidth = 120
	// opencodeLeftMargin is the left padding on opencode's main column
	// (routes/session paddingLeft=2), applied to the whole main body.
	opencodeLeftMargin = 2
	// opencodeRightGap separates the main column from the sidebar (opencode's
	// main-column paddingRight=2) so the panels don't touch.
	opencodeRightGap = 2
)

// The "whip" block-glyph wordmark, drawn in the same ▀▄█ pixel font as
// opencode's logo: "wh" muted, "ip" bold foreground (mirroring opencode's
// two-tone open|code mark), themed via whip's light/dark handling.
var (
	ocLogoWh = []string{
		"      ▄   ",
		"█ ▄ █ █▀▀█",
		"█ █ █ █  █",
		"▀▀▀▀▀ ▀  ▀",
	}
	ocLogoIp = []string{
		"▄     ",
		"█ █▀▀█",
		"█ █  █",
		"▀ █▀▀▀",
	}
)

// opencodeLogo renders the wordmark: muted "wh", bold "ip", joined with a
// single-column gap per line (opencode's two-tone logo treatment).
func opencodeLogo() string {
	left := lipgloss.NewStyle().Foreground(ocMutedCol())
	right := lipgloss.NewStyle().Foreground(ocTextCol()).Bold(true)
	var b strings.Builder
	for i := range ocLogoWh {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(left.Render(ocLogoWh[i]))
		b.WriteByte(' ')
		b.WriteString(right.Render(ocLogoIp[i]))
	}
	return b.String()
}

// opencodeHome renders the empty-state "home" screen: the wordmark logo
// centered in the given area, like opencode's home route before any messages.
func opencodeHome(width, height int) string {
	logo := opencodeLogo()
	block := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, logo)
	return block
}

// sidebarVisible reports whether the opencode-mode sidebar should render: the
// mode is on and the terminal is wide enough to spare sidebarWidth columns.
func (m *model) sidebarVisible() bool {
	return m.uiMode == opencodeMode && m.termWidth >= sidebarMinWidth && !m.sidebarHide
}

// sidebarView renders the opencode right sidebar: session title, a Context
// block (tokens / % of window / spend), LSP status, and a footer. Height is
// the number of rows to fill so the sidebar spans the body. All styling uses
// whip's theme styles, so it honors light/dark/auto.
func (m *model) sidebarView(height int) string {
	// Every style carries the panel background so text doesn't punch holes in the
	// filled panel column; opencode's exact text/muted colors for readability.
	head := lipgloss.NewStyle().Bold(true).Foreground(ocTextCol()).Background(ocPanelBg())
	dim := lipgloss.NewStyle().Foreground(ocMutedCol()).Background(ocPanelBg())

	title := strings.TrimSpace(m.sessTitle)
	if title == "" {
		title = filepath.Base(cwd()) // untitled session: fall back to the working dir
	}

	var b strings.Builder
	b.WriteString(head.Render(truncLine(title, sidebarWidth-4)) + "\n\n")

	// Context: tokens used, share of the window, spend.
	b.WriteString(head.Render("Context") + "\n")
	u := m.agent.Usage()
	b.WriteString(dim.Render(fmtTok(u.PromptTokens+u.CompletionTokens)+" tokens") + "\n")
	if m.agent.ContextLimit > 0 {
		pct := agent.EstimateTokens(m.agent.Messages) * 100 / m.agent.ContextLimit
		b.WriteString(dim.Render(fmt.Sprintf("%d%% used", pct)) + "\n")
	}
	if cost, ok := m.sessionCost(); ok {
		b.WriteString(dim.Render(fmt.Sprintf("$%.2f spent", cost)) + "\n\n")
	} else {
		b.WriteString(dim.Render("$0.00 spent") + "\n\n")
	}

	// LSP status.
	b.WriteString(head.Render("LSP") + "\n")
	b.WriteString(dim.Render(m.lspSummary()) + "\n")

	// Top content (title + Context + LSP), clipped if the sidebar is very short.
	top := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	bullet := lipgloss.NewStyle().Foreground(ocSuccessCol()).Background(ocPanelBg())
	footer := bullet.Render("• ") + head.Render("whip") + dim.Render(" "+Version)

	rows := make([]string, 0, height)
	if height <= 0 {
		rows = append(top, footer)
	} else {
		if len(top) > height-1 { // keep the last row for the footer
			top = top[:max(height-1, 0)]
		}
		rows = append(rows, top...)
		for len(rows) < height-1 {
			rows = append(rows, "")
		}
		rows = append(rows, footer) // pinned to the bottom row
	}
	// opencode's sidebar is set apart by a panel background (no border). Pad each
	// row manually with bg-styled spaces (ocPadTo) so the WHOLE column carries
	// the panel shade — style.Width padding drops the bg after nested resets.
	bg := ocPanelBg()
	pad2 := lipgloss.NewStyle().Background(bg).Render("  ")
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ocPadTo(pad2+r, sidebarWidth, bg)
	}
	return strings.Join(out, "\n")
}

// lspSummary is a one-line LSP status for the sidebar: a connected count, or a
// disabled note when no LSP manager is configured.
func (m *model) lspSummary() string {
	if m.lspMgr == nil {
		return "LSPs are disabled"
	}
	return lspSummaryLine(m.lspMgr.Statuses())
}

// lspSummaryLine is the pure formatter behind lspSummary (extracted so its
// branches are testable without a live LSP server).
func lspSummaryLine(servers []lsp.Status) string {
	if len(servers) == 0 {
		return "no servers"
	}
	connected := 0
	for _, s := range servers {
		if s.State == "connected" {
			connected++
		}
	}
	return fmt.Sprintf("%d/%d connected", connected, len(servers))
}

// opencodePrompt wraps the textarea in opencode's prompt chrome: a ┃ left bar,
// the input, a model/mode row beneath, and a ╹ tail with a ▀ underline. Themed
// with whip's styles (no forced colors). width is the content width. inner is
// m.input.View() (already includes the textarea's own "┃ " prompt, so we strip
// it and supply the bar ourselves for the full-height box).
func (m *model) opencodePrompt(inner string, width int) string {
	if width < 6 {
		return inner
	}
	ebg := ocElementBg()
	elem := lipgloss.NewStyle().Background(ebg)
	bar := lipgloss.NewStyle().Foreground(ocAgentCol()).Background(ebg).Render("┃")
	// truncate BEFORE padding: a full-width input line (bar + 2-space gutter +
	// content) exceeds width, wraps in the terminal, and grows the alt-screen
	// frame a row past layout()'s budget — skewing every mouse-Y hit-test
	row := func(content string) string { return ocPadTo(ansi.Truncate(content, width, ""), width, ebg) }
	var b strings.Builder
	b.WriteString(row(bar) + "\n") // paddingTop (bar continues down the whole box)
	for ln := range strings.SplitSeq(inner, "\n") {
		// The textarea pads lines to its width with PLAIN spaces (its internal
		// viewport) — a default-background tail that would punch a white stripe
		// through the box. Trim it and let ocPadTo re-pad with the box bg.
		ln = strings.TrimRight(ln, " ")
		b.WriteString(row(bar+elem.Render("  "+ln)) + "\n")
	}
	b.WriteString(row(bar) + "\n") // padding below the input, above the meta row
	// model/mode row: mode in the agent color, model in text, provider muted.
	agent := lipgloss.NewStyle().Foreground(ocAgentCol()).Background(ocElementBg())
	txt := lipgloss.NewStyle().Foreground(ocTextCol()).Background(ocElementBg())
	muted := lipgloss.NewStyle().Foreground(ocMutedCol()).Background(ocElementBg())
	meta := agent.Render(m.ocModeLabel()) + muted.Render(" · ") + txt.Render(m.modelName) + muted.Render("  "+m.provName)
	b.WriteString(row(bar+elem.Render("  ")+meta) + "\n")
	// Soft bottom edge: a ╹ tail then a ▀ line the SAME color as the box fill, so
	// it reads as the box's rounded bottom rather than a bright bar. When the
	// terminal background is unknown there is no box fill to match — skip the ▀
	// glyphs (they'd render in the default fg: a solid black bar on a light
	// terminal) and keep just the bar tail so the row count stays stable.
	b.WriteString(lipgloss.NewStyle().Foreground(ocAgentCol()).Render("╹"))
	if ocThemeKnown() {
		shadow := lipgloss.NewStyle().Foreground(ocElementBg())
		b.WriteString(shadow.Render(strings.Repeat("▀", max(width-1, 0))))
	}
	return b.String()
}

// opencodeUserCard renders a user turn as opencode's bordered card: a ┃ left
// bar (accent color) with one blank padding row above and below the text.
// Themed with whip's styles (no forced background), so the bar + padding give
// the card impression while honoring light/dark/auto.
func opencodeUserCard(text string, width int, hover bool) string {
	if width < 4 {
		return text
	}
	bg := ocPanelBg()
	if hover {
		bg = ocElementBg() // opencode's hover state: the card lifts to the element shade
	}
	bar := lipgloss.NewStyle().Foreground(ocAgentCol()).Background(bg).Render("┃")
	txt := lipgloss.NewStyle().Foreground(ocTextCol()).Background(bg)
	lines := strings.Split(wrap(text, width-3), "\n")
	rows := append([]string{""}, lines...) // blank padding row above
	rows = append(rows, "")                // blank padding row below
	var b strings.Builder
	for i, ln := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		content := bar // opencode draws the left bar on every card row, padding rows included
		if ln != "" {
			content = bar + txt.Render("  "+ln) // two spaces after the bar
		}
		b.WriteString(ocPadTo(content, width, bg)) // fill the row to width with the panel bg
	}
	return b.String()
}

// ocKnightRider is opencode's generation spinner: a block bar sweeping back
// and forth (■ active over ⬝ inactive, 40ms frames).
var ocKnightRider = spinner.Spinner{
	Frames: func() []string {
		const cells, bar = 8, 3
		var f []string
		for p := 0; p <= cells-bar; p++ { // forward sweep
			f = append(f, strings.Repeat("⬝", p)+strings.Repeat("■", bar)+strings.Repeat("⬝", cells-bar-p))
		}
		for p := cells - bar - 1; p > 0; p-- { // and back
			f = append(f, strings.Repeat("⬝", p)+strings.Repeat("■", bar)+strings.Repeat("⬝", cells-bar-p))
		}
		return f
	}(),
	FPS: 80 * time.Millisecond, // half opencode's 40ms — full speed read as frantic
}

// opencodeStatus renders opencode's session footer: the working directory on
// the left (replaced by the knight-rider spinner + "esc interrupt" while the
// model responds), and "{tokens} ({pct%})  ctrl+p commands" on the right.
func (m *model) opencodeStatus() string {
	muted := lipgloss.NewStyle().Foreground(ocMutedCol())
	txt := lipgloss.NewStyle().Foreground(ocTextCol())
	// right side: "{tokens} ({pct})  " muted, then "ctrl+p" in text, " commands" muted.
	rightRaw := ""
	if u := m.agent.Usage(); u.PromptTokens+u.CompletionTokens > 0 {
		rightRaw = strings.ToUpper(fmtTok(u.PromptTokens + u.CompletionTokens)) // opencode uses uppercase (15.8K)
		if m.agent.ContextLimit > 0 {
			rightRaw += fmt.Sprintf(" (%d%%)", agent.EstimateTokens(m.agent.Messages)*100/m.agent.ContextLimit)
		}
		rightRaw += "  "
	}
	rightRaw += "ctrl+p commands"
	right := muted.Render(strings.TrimSuffix(rightRaw, "ctrl+p commands")) + txt.Render("ctrl+p") + muted.Render(" commands")
	w := max(m.width, 0)
	rightW := lipgloss.Width(rightRaw)
	var leftR string
	if m.busy {
		// generating: the spinner sweeps where the cwd usually sits (opencode's
		// bottom-bar treatment), with the interrupt hint beside it
		hint := " interrupt"
		if m.interrupt1 {
			hint = " again to interrupt"
		}
		leftR = " " + m.spin.View() + "  " + txt.Render("esc") + muted.Render(hint)
	} else {
		left := cwd()
		if lipgloss.Width(left)+rightW+2 > w { // no room: truncate the cwd, keep the right side
			left = truncLine(left, max(w-rightW-2, 0))
		}
		leftR = muted.Render(" " + left)
	}
	pad := max(w-lipgloss.Width(leftR)-rightW, 1)
	line := leftR + muted.Render(strings.Repeat(" ", pad)) + right
	if w > 0 {
		// the busy side (spinner + esc hint) has no width-aware trim of its
		// own: clamp the row or it wraps the alt-screen frame on a narrow
		// terminal and shifts all mouse math
		line = ansi.Truncate(line, w, "")
	}
	return line
}

// opencodePaletteView renders the ctrl+p command list as opencode's Commands
// ocBoxKit bundles the styles and row builders every floating opencode dialog
// shares (Commands, Message Actions): a fixed-width panel with lr rows.
type ocBoxKit struct {
	w                              int
	bg                             lipgloss.TerminalColor
	pnl, text, head, muted, accent lipgloss.Style
	blank                          string
}

func (m *model) newOcBox() ocBoxKit {
	bg := ocPanelBg()
	w := min(64, max(m.width-2, 20))
	text := lipgloss.NewStyle().Foreground(ocTextCol()).Background(bg)
	return ocBoxKit{
		w: w, bg: bg,
		pnl:    lipgloss.NewStyle().Background(bg),
		text:   text,
		head:   text.Bold(true),
		muted:  lipgloss.NewStyle().Foreground(ocMutedCol()).Background(bg),
		accent: lipgloss.NewStyle().Foreground(ocAccentCol()).Background(bg).Bold(true),
		blank:  ocPadTo("", w, bg),
	}
}

// lr assembles left+right onto one padded row: left at col 2, right at the edge.
func (k ocBoxKit) lr(left, right string) string {
	gap := max(k.w-2-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return ocPadTo(k.pnl.Render("  ")+left+k.pnl.Render(strings.Repeat(" ", gap))+right, k.w, k.bg)
}

// dialog rows: a panel on the panel background with a bold "Commands" header +
// right-aligned esc, a Search line, accent category headers, and name-left /
// hint-right rows; the selected row is a full-width primary fill. Each row is
// exactly w cells wide — ocOverlay splices them over the dimmed session.
func (m *model) ocDialogRows() []string {
	p := m.palette
	k := m.newOcBox()
	w, bg := k.w, k.bg
	pnl, text, head, muted, accent := k.pnl, k.text, k.head, k.muted, k.accent
	lr := k.lr
	blank := k.blank

	// a sub-panel (theme picker, model list, …) renders inside the same box
	if pp := p.top(); pp != nil {
		rows := []string{blank, lr(head.Render("Commands › "+pp.title), muted.Render("esc")), blank}
		for ln := range strings.SplitSeq(strings.TrimRight(m.panelView(pp), "\n"), "\n") {
			rows = append(rows, ocPadTo(pnl.Render("  ")+ocOnBg(ln, bg), w, bg))
		}
		return append(rows, blank)
	}

	rows := []string{blank, lr(head.Render("Commands"), muted.Render("esc")), blank}
	if p.filter == "" {
		rows = append(rows, lr(muted.Render("Search"), ""))
	} else {
		rows = append(rows, lr(text.Render(p.filter), ""))
	}
	rows = append(rows, blank)

	sel := lipgloss.NewStyle().Foreground(ocSelFg()).Background(ocSelBg())
	lastCat := ""
	for i, it := range p.items {
		if it.category != lastCat {
			if lastCat != "" {
				rows = append(rows, blank)
			}
			rows = append(rows, lr(accent.Render(it.category), ""))
			lastCat = it.category
		}
		hint := ""
		if it.dynHint != nil {
			hint = truncLine(it.dynHint(m), max(w-4-len(it.title)-2, 0))
		}
		if i == p.idx {
			// full-width primary fill, opencode's selected-row treatment
			row := sel.Render("  "+it.title) + sel.Render(strings.Repeat(" ", max(w-2-len(it.title)-lipgloss.Width(hint)-2, 1))) + sel.Render(hint+"  ")
			rows = append(rows, ocPadTo(row, w, ocSelBg()))
		} else {
			rows = append(rows, lr(text.Render(it.title), muted.Render(hint)))
		}
	}
	if len(p.items) == 0 {
		rows = append(rows, lr(muted.Render("No results found"), ""))
	}
	return append(rows, blank)
}

// ocToolIcon maps a tool to opencode's inline-tool icon glyphs.
func ocToolIcon(name string) string {
	switch name {
	case "bash":
		return "$"
	case "read", "edit", "write":
		return "←"
	case "grep", "glob":
		return "✱"
	case "webfetch":
		return "%"
	case "websearch", "browser_exec", "computer_exec":
		return "◈"
	case "subagent", "subagent_steer", "skill":
		return "→"
	default:
		return "⚙"
	}
}

// ocToolLabel is the display name + separator for a tool row: subagents read
// as opencode tasks ("Task — {description}"), everything else keeps the tool
// name and a plain space ("Bash git status").
func ocToolLabel(name string) (label, sep string) {
	if name == "subagent" {
		return "Task", " — "
	}
	return toolHeaderName(name), " "
}

// ocToolRow renders a completed tool call opencode-style: indent 3, an icon,
// the tool name in text color, and the subject muted. Failed calls go red.
func ocToolRow(name, args string, failed bool) string {
	icon, subject := ocToolIcon(name), toolSubject(name, args)
	label, sep := ocToolLabel(name)
	if failed {
		e := lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
		return "   " + e.Render(icon+" "+label+sep+subject)
	}
	txt := lipgloss.NewStyle().Foreground(ocTextCol())
	muted := lipgloss.NewStyle().Foreground(ocMutedCol())
	return "   " + muted.Render(icon) + " " + txt.Render(label) + muted.Render(sep+subject)
}

// ocToolPending renders a queued/running tool call: opencode's "~ " prefix,
// all muted.
func ocToolPending(name, args string) string {
	muted := lipgloss.NewStyle().Foreground(ocMutedCol())
	label, sep := ocToolLabel(name)
	return "   " + muted.Render("~ "+label+sep+toolSubject(name, args))
}

// ocToolResult renders a tool result block: collapsed to a single muted "↳ N
// lines" hint (opencode tucks results away behind the tool row), the full body
// indented when expanded. Errors keep the error color; hover brightens the
// hint (opencode's clickable-row hover).
func ocToolResult(lines []string, expanded, isErr, hover bool, width int) string {
	style := lipgloss.NewStyle().Foreground(ocMutedCol())
	if hover {
		style = lipgloss.NewStyle().Foreground(ocTextCol())
	}
	if isErr {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
	}
	// short results (a launch confirmation, a one-line answer) read inline —
	// a "↳ 1 line · expand" hint for one line is pure friction
	if !expanded && len(lines) > 2 {
		return "   " + style.Render(fmt.Sprintf("↳ %d lines · ctrl+e or click expands", len(lines)))
	}
	return wrap(style.Render("   ↳ "+strings.Join(lines, "\n     ")), width)
}

// ocDimLine renders a backdrop line faint (SGR 2), re-applying the faint after
// every full reset inside the line so embedded styles can't undo the dim.
func ocDimLine(s string) string {
	if s == "" {
		return s
	}
	return "\x1b[2m" + strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m\x1b[2m") + "\x1b[0m"
}

// msgActions is the state of the opencode-style Message Actions dialog opened
// by clicking a message: the clicked block, the selected action, and a filter.
type msgActions struct {
	block  int // index into m.blocks
	sel    int
	filter string
}

// msgAction is one row of the Message Actions dialog.
type msgAction struct {
	name, desc string
	run        func(*model, int) tea.Cmd
}

var msgActionList = []msgAction{
	{"Revert", "undo messages and file changes", func(m *model, _ int) tea.Cmd { m.openRewind(); return nil }},
	{"Copy", "message text to clipboard", func(m *model, blk int) tea.Cmd {
		if blk >= 0 && blk < len(m.blocks) {
			copyText(ansi.Strip(m.blocks[blk].text))
			return m.showToast("Message copied to clipboard!")
		}
		return nil
	}},
	{"Fork", "create a new session", func(m *model, _ int) tea.Cmd { m.forkCommand(""); return nil }},
}

// msgActionItems returns the actions matching the dialog's filter.
func (a *msgActions) items() []msgAction {
	if a.filter == "" {
		return msgActionList
	}
	var out []msgAction
	for _, it := range msgActionList {
		if strings.Contains(strings.ToLower(it.name+" "+it.desc), strings.ToLower(a.filter)) {
			out = append(out, it)
		}
	}
	return out
}

// ocMsgActionRows renders the Message Actions dialog box rows.
func (m *model) ocMsgActionRows() []string {
	a := m.msgActions
	k := m.newOcBox()
	rows := []string{k.blank, k.lr(k.head.Render("Message Actions"), k.muted.Render("esc")), k.blank}
	if a.filter == "" {
		rows = append(rows, k.lr(k.muted.Render("Search"), ""))
	} else {
		rows = append(rows, k.lr(k.text.Render(a.filter), ""))
	}
	rows = append(rows, k.blank)
	items := a.items()
	sel := lipgloss.NewStyle().Foreground(ocSelFg()).Background(ocSelBg())
	for i, it := range items {
		if i == a.sel {
			rows = append(rows, ocPadTo(sel.Render("  "+it.name+" "+it.desc), k.w, ocSelBg()))
		} else {
			rows = append(rows, k.lr(k.text.Render(it.name)+k.muted.Render(" "+it.desc), ""))
		}
	}
	if len(items) == 0 {
		rows = append(rows, k.lr(k.muted.Render("No results found"), ""))
	}
	return append(rows, k.blank)
}

// ocWindow returns the [lo,hi) slice bounds showing up to budget rows
// centered on idx.
func ocWindow(n, idx, budget int) (int, int) {
	if budget >= n {
		return 0, n
	}
	lo := max(idx-budget/2, 0)
	hi := min(lo+budget, n)
	return max(hi-budget, 0), hi
}

// ocModelDialogRows renders the model picker as opencode's "Select model"
// dialog: Search line, provider names as accent group headers, the selected
// row a primary fill, catalog-only routes marked (new).
func (m *model) ocModelDialogRows() []string {
	p := m.mpicker
	k := m.newOcBox()
	rows := []string{k.blank, k.lr(k.head.Render("Select model"), k.muted.Render("esc")), k.blank}
	if p.filter.query == "" {
		rows = append(rows, k.lr(k.muted.Render("Search"), ""))
	} else {
		rows = append(rows, k.lr(k.text.Render(p.filter.query), ""))
	}
	rows = append(rows, k.blank)

	items := p.view()
	sel := lipgloss.NewStyle().Foreground(ocSelFg()).Background(ocSelBg())
	lo, hi := ocWindow(len(items), p.idx, max(m.height-14, 4))
	lastProv := ""
	for i := lo; i < hi; i++ {
		it := items[i]
		if it.provider != lastProv {
			if lastProv != "" {
				rows = append(rows, k.blank)
			}
			rows = append(rows, k.lr(k.accent.Render(it.provider), ""))
			lastProv = it.provider
		}
		mark := ""
		if it.fromCatalog {
			mark = "(new)"
		}
		cur := "  "
		if it.model == m.modelName && it.provider == m.provName {
			cur = "● " // opencode's current-model gutter
		}
		if i == p.idx {
			rows = append(rows, ocPadTo(sel.Render("  "+cur+it.model), k.w, ocSelBg()))
		} else {
			rows = append(rows, k.lr(k.text.Render(cur+it.model), k.muted.Render(mark)))
		}
	}
	if len(items) == 0 {
		rows = append(rows, k.lr(k.muted.Render("No results found"), ""))
	}
	rows = append(rows, k.blank,
		k.lr(k.text.Render("enter")+k.muted.Render(" select")+k.pnl.Render("  ")+k.text.Render("type")+k.muted.Render(" to filter"), ""))
	return append(rows, k.blank)
}

// ocSessionDialogRows renders the resume picker as opencode's "Sessions"
// dialog: date group headers in accent, title left / age right, selected row
// a primary fill.
func (m *model) ocSessionDialogRows() []string {
	p := m.picker
	k := m.newOcBox()
	rows := []string{k.blank, k.lr(k.head.Render("Sessions"), k.muted.Render("esc")), k.blank}
	sel := lipgloss.NewStyle().Foreground(ocSelFg()).Background(ocSelBg())
	lo, hi := ocWindow(len(p.metas), p.idx, max(m.height-12, 4))
	lastDay := ""
	for i := lo; i < hi; i++ {
		meta := p.metas[i]
		day := meta.UpdatedAt.Format("Mon Jan 2 2006")
		if day == m.nowFn().Format("Mon Jan 2 2006") {
			day = "Today"
		}
		if day != lastDay {
			if lastDay != "" {
				rows = append(rows, k.blank)
			}
			rows = append(rows, k.lr(k.accent.Render(day), ""))
			lastDay = day
		}
		title := meta.Title
		if title == "" {
			title = "(untitled)"
		}
		title = truncLine(title, k.w-16)
		if i == p.idx {
			rows = append(rows, ocPadTo(sel.Render("  "+title), k.w, ocSelBg()))
		} else {
			rows = append(rows, k.lr(k.text.Render(title), k.muted.Render(ago(meta.UpdatedAt))))
		}
	}
	if len(p.metas) == 0 {
		rows = append(rows, k.lr(k.muted.Render("No sessions"), ""))
	}
	rows = append(rows, k.blank,
		k.lr(k.text.Render("enter")+k.muted.Render(" resume")+k.pnl.Render("  ")+k.text.Render("↑/↓")+k.muted.Render(" select"), ""))
	return append(rows, k.blank)
}

// toastClearMsg expires the toast set by showToast.
type toastClearMsg struct{ at time.Time }

// showToast displays opencode's top-right toast for 5s (a new toast replaces
// the current one and resets the timer).
func (m *model) showToast(msg string) tea.Cmd {
	m.toast = msg
	at := m.nowFn()
	m.toastAt = at
	return tea.Tick(5*time.Second, toastClear(at))
}

// toastClear builds the tick payload carrying the toast's timestamp, so a
// stale timer can't clear a newer toast.
func toastClear(at time.Time) func(time.Time) tea.Msg {
	return func(time.Time) tea.Msg { return toastClearMsg{at: at} }
}

// ocSpliceToast paints the toast box into the frame's top-right corner
// (opencode: top 2, right 2, panel bg, success-colored side bars).
func (m *model) ocSpliceToast(v string) string {
	bg := ocPanelBg()
	pnl := lipgloss.NewStyle().Background(bg)
	bar := lipgloss.NewStyle().Foreground(ocSuccessCol()).Background(bg).Render("┃")
	txt := lipgloss.NewStyle().Foreground(ocTextCol()).Background(bg)
	inner := truncLine(m.toast, max(min(56, m.termWidth-10), 8))
	w := lipgloss.Width(inner) + 6
	mid := bar + pnl.Render("  ") + txt.Render(inner) + pnl.Render("  ") + bar
	pad := ocPadTo(bar, w-1, bg) + bar // side bars on the padding rows too
	return ocSpliceAt(v, []string{pad, mid, pad}, max(m.termWidth-w-2, 0), 2)
}

// ocLeaderChord dispatches an opencode leader chord (ctrl+x then a key,
// within 2s). Returns handled=false for unknown keys.
func (m *model) ocLeaderChord(k string) (tea.Model, tea.Cmd, bool) {
	switch k {
	case "m": // model list
		m.openModelPicker(false)
	case "l": // session list
		return mcCmd(m.command("/resume"))
	case "n": // new session
		return mcCmd(m.command("/clear"))
	case "b": // sidebar toggle
		m.sidebarHide = !m.sidebarHide
		m.ocRecalcWidth()
	case "t": // theme list
		m.openPaletteOn("theme")
	case "c": // compact
		return mcCmd(m.command("/compact"))
	case "g": // jump back through messages (whip's rewind picker)
		m.openRewind()
	case "y": // copy last assistant message
		for _, b := range slices.Backward(m.blocks) {
			if b.kind == blockAssistant {
				copyText(ansi.Strip(b.text))
				return m, m.showToast("Message copied to clipboard!"), true
			}
		}
		return m, m.showToast("No assistant messages found"), true
	default:
		return m, nil, false
	}
	return m, nil, true
}

// mcCmd adapts a (model, cmd) pair to the chord-dispatch triple.
func mcCmd(mod tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd, bool) { return mod, cmd, true }

// ocRecalcWidth recomputes the content width from the terminal width (mirrors
// the WindowSizeMsg math) — needed when the sidebar is toggled at runtime.
func (m *model) ocRecalcWidth() {
	if m.uiMode != opencodeMode || m.termWidth == 0 {
		return
	}
	w := m.termWidth - opencodeLeftMargin
	if m.termWidth >= sidebarMinWidth && !m.sidebarHide {
		w -= sidebarWidth + opencodeRightGap
	}
	if w != m.width {
		m.width = w
		m.input.SetWidth(w - 2)
		m.refreshVP()
	}
}

// msgActionsKey handles keys while the Message Actions dialog is open.
func (m *model) msgActionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.msgActions
	items := a.items()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.msgActions = nil
	case "enter":
		if a.sel < len(items) {
			m.msgActions = nil
			return m, items[a.sel].run(m, a.block)
		}
	case "up":
		if a.sel > 0 {
			a.sel--
		}
	case "down":
		if a.sel < len(items)-1 {
			a.sel++
		}
	case "backspace":
		if a.filter != "" {
			a.filter = a.filter[:len(a.filter)-1]
			a.sel = 0
		}
	default:
		if msg.Type == tea.KeyRunes {
			a.filter += string(msg.Runes)
			a.sel = 0
		}
	}
	return m, nil
}

// vpTopRows is the number of chrome rows above the transcript viewport —
// mouse row math must match viewBody exactly (opencode mode drops the header
// and tips lines).
func (m *model) vpTopRows() int {
	if m.uiMode == opencodeMode {
		return 0
	}
	return 3
}

// vpXOff is the columns the main body is shifted right (opencode's left margin).
func (m *model) vpXOff() int {
	if m.uiMode == opencodeMode {
		return opencodeLeftMargin
	}
	return 0
}

// updateHover tracks the message block under the pointer (opencode's hover
// effect on user cards) and re-renders when it changes.
func (m *model) updateHover(x, y int) {
	row := y - m.viewTop - m.vpTopRows() - m.contentPad() + m.vp.YOffset + m.vpLead
	idx := -1
	if x >= m.vpXOff() && x < m.vpXOff()+m.width {
		for i := range m.blocks {
			k := m.blocks[i].kind
			clickable := k == blockUser || k == blockTool || k == blockThought // rows with a click affordance highlight on hover
			if clickable && row >= m.blocks[i].y0 && row <= m.blocks[i].y1 {
				idx = i
				break
			}
		}
	}
	if idx == m.hoverIdx {
		return
	}
	if m.hoverIdx >= 0 && m.hoverIdx < len(m.blocks) {
		m.blocks[m.hoverIdx].hover, m.blocks[m.hoverIdx].stale = false, true
	}
	if idx >= 0 {
		m.blocks[idx].hover, m.blocks[idx].stale = true, true
	}
	m.hoverIdx = idx
	m.refreshVP()
}

// ocOverlay draws the Commands dialog OVER the live session, opencode-style:
// the whole frame keeps rendering behind the modal, dimmed, with the dialog
// rows spliced in centered (upper third). The dialog is clipped to the screen
// height so the frame never scrolls (which would shift the sidebar).
func (m *model) ocOverlay(v string) string {
	return m.ocOverlayRows(v, m.ocDialogRows()) // rows never empty: dialog chrome is unconditional
}

// ocSpliceAt paints rows onto the frame starting at column x, row y — the
// ANSI-aware overlay primitive behind the dialogs, the toast, and the
// completion popup. Rows outside the frame are skipped.
func ocSpliceAt(v string, rows []string, x, y int) string {
	lines := strings.Split(v, "\n")
	for i, r := range rows {
		li := y + i
		if li < 0 || li >= len(lines) {
			continue
		}
		l := lines[li]
		left := ansi.Truncate(l, x, "")
		if pad := x - lipgloss.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(l, x+lipgloss.Width(r), "")
		lines[li] = left + "\x1b[0m" + r + "\x1b[0m" + right
	}
	return strings.Join(lines, "\n")
}

// ocOverlayRows dims the frame and splices the given dialog rows in centered.
func (m *model) ocOverlayRows(v string, rows []string) string {
	lines := strings.Split(v, "\n")
	for i := range lines {
		lines[i] = ocDimLine(lines[i])
	}
	w := lipgloss.Width(rows[0])
	x := max((max(m.termWidth, w)-w)/2, 0)
	if len(rows) > len(lines) {
		rows = rows[:len(lines)] // ponytail: bottom-clip; scroll-follow selection if lists outgrow screens
	}
	y := max((len(lines)-len(rows))/3, 0)
	return ocSpliceAt(strings.Join(lines, "\n"), rows, x, y)
}

// ocMenuOverlay draws the completion popup ON TOP of the frame, bottom-anchored
// to the row just above the input box (opencode's autocomplete position) — the
// frame beneath never reflows while typing.
func (m *model) ocMenuOverlay(v string) string {
	rows := strings.Split(m.menuView(), "\n")
	if len(rows) > m.inputBodyOff { // clip the top if there's no room above the box
		rows = rows[len(rows)-m.inputBodyOff:]
	}
	return ocSpliceAt(v, rows, opencodeLeftMargin, m.inputBodyOff-len(rows))
}

// opencodeAttribution renders opencode's per-response attribution line:
// "▣  {mode} · {model} · {duration}", indented 3 to sit under the assistant body.
func (m *model) opencodeAttribution(d time.Duration) string {
	agent := lipgloss.NewStyle().Foreground(ocAgentCol())
	txt := lipgloss.NewStyle().Foreground(ocTextCol())
	muted := lipgloss.NewStyle().Foreground(ocMutedCol())
	return "   " + agent.Render("▣") + txt.Render("  "+m.ocModeLabel()) + // 3-space indent under the assistant column
		muted.Render(" · "+m.modelName+" · "+fmtShortDur(d))
}

// fmtShortDur formats a duration the way opencode does: "173ms" under a second,
// otherwise "2.4s".
func fmtShortDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ocModeLabel is the left segment of the prompt meta row. whip has no named
// agents like opencode's "Build"; its closest analog is the reasoning effort.
func (m *model) ocModeLabel() string {
	eff := m.agent.Effort
	if eff == "" {
		eff = "off"
	}
	return strings.ToUpper(eff[:1]) + eff[1:]
}

// applyUIMode points the live render state at the given UI mode. opencode mode
// is purely structural — it does not touch whip's theme, colors, glyphs, or
// spinner — so this only records the flag.
func (m *model) applyUIMode(mode string) {
	invalidateMDRenderer() // opencode markdown style differs; rebuild on mode change
	if mode == opencodeMode {
		m.uiMode = opencodeMode
		ocActive = true
		m.spin = spinner.New(spinner.WithSpinner(ocKnightRider))
		m.spin.Style = lipgloss.NewStyle().Foreground(ocAgentCol())
		m.input.Prompt = "" // opencodePrompt supplies the ┃ bar per line
		// opencode's placeholder carries a random example (picked once, not cycled)
		examples := []string{"Fix a TODO in the codebase", "What is the tech stack of this project?", "Fix broken tests"}
		m.input.Placeholder = fmt.Sprintf("Ask anything… %q", examples[rand.IntN(len(examples))]) //nolint:gosec // G404: cosmetic placeholder pick, not security-sensitive
		// Fill the textarea with the element background so the input box reads as
		// a filled panel (opencode's prompt box).
		elem := lipgloss.NewStyle().Background(ocElementBg())
		m.input.FocusedStyle.Text = elem
		m.input.FocusedStyle.CursorLine = elem
		m.input.FocusedStyle.Placeholder = dimStyle.Background(ocElementBg())
		m.input.BlurredStyle.Text = elem
		m.input.BlurredStyle.Placeholder = dimStyle.Background(ocElementBg())
		if tuiRunning && m.mouseOn {
			// a runtime toggle INTO opencode mode must arm all-motion tracking
			// itself — Run's ?1003h only covers sessions that start here
			fmt.Fprint(os.Stdout, "\x1b[?1003h")
		}
	} else {
		m.uiMode = ""
		ocActive = false
		m.spin = spinner.New(spinner.WithSpinner(spinner.Dot))
		m.input.Prompt = "┃ "
		m.input.Placeholder = whipPlaceholder
		m.input.FocusedStyle.Text = lipgloss.NewStyle()
		m.input.FocusedStyle.CursorLine = lipgloss.NewStyle()
		m.input.FocusedStyle.Placeholder = dimStyle
		m.input.BlurredStyle.Text = lipgloss.NewStyle()
		m.input.BlurredStyle.Placeholder = dimStyle
		if tuiRunning && m.mouseOn {
			fmt.Fprint(os.Stdout, "\x1b[?1003l") // drop all-motion tracking with the mode
		}
		// clear any stuck hover highlight left behind by the mode switch
		if m.hoverIdx >= 0 && m.hoverIdx < len(m.blocks) {
			m.blocks[m.hoverIdx].hover, m.blocks[m.hoverIdx].stale = false, true
		}
		m.hoverIdx = -1
	}
	// The textarea reads styles through a pointer snapshotted at Focus() time
	// (style = &m.FocusedStyle). The struct has been copied since newInput's
	// Focus(), so the writes above land in a field View() never reads.
	// Re-focus to re-snapshot the pointer at the CURRENT struct.
	m.input.Focus()
}

// setUIMode switches render mode live, persists the choice, and redraws. It
// returns the bubbletea command that enters/exits the alternate screen so the
// full-screen state tracks the mode.
func (m *model) setUIMode(mode string) tea.Cmd {
	if mode != opencodeMode {
		mode = ""
	}
	// a mid-stream toggle must not strand reasoning in the old mode's fields:
	// flush under the OLD uiMode (each branch drains its own accumulator),
	// then clear the opencode fields a zero thinkStart would leave behind
	m.flushThink()
	m.thinkStart, m.ocThink = time.Time{}, ""
	m.applyUIMode(mode)
	// leaving the alt screen restores a bottom-anchored inline view, but only
	// WindowSizeMsg resets the re-anchor sentinel — without it viewTop stays
	// at opencode's pinned 0 and every mouse row maps above the pointer until
	// the next resize. View() recomputes viewTop from this sentinel.
	m.viewTop = 1 << 30
	m.cfg.UIMode = mode
	if m.cfgExtra == nil {
		m.cfgExtra = map[string]string{}
	}
	if mode == "" {
		delete(m.cfgExtra, "uiMode")
	} else {
		m.cfgExtra["uiMode"] = mode
	}
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
	m.refreshVP()
	m.append(dimStyle.Render("◐ ui mode: " + uiModeLabel(mode)))
	if mode == opencodeMode {
		return tea.EnterAltScreen
	}
	return tea.ExitAltScreen
}

// uiModeLabel is the display name for a UI mode value.
func uiModeLabel(mode string) string {
	if mode == opencodeMode {
		return "opencode"
	}
	return "default"
}
