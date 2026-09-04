package tui

import (
	"fmt"
	"github.com/context-labs/whip/internal/tui/ui"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/llm"
)

// opencode.go implements whip's "opencode" UI mode: an opt-in *structural*
// layout inspired by opencode's TUI (github.com/sst/opencode) — full-screen,
// a right-hand sidebar, and the block-glyph wordmark. It deliberately keeps
// whip's own theming (light/dark/auto) and colors; only the layout/structure
// changes. It is the only UI since 2026-09 (the inline mode was removed).
// (Display → UI mode).

// opencode's own theme palette (packages/tui/src/theme/assets/opencode.json),
// resolved against whip's OWN detected theme (mdLight/mdKnown) rather than
// lipgloss.AdaptiveColor — AdaptiveColor reads lipgloss's separate background
// detection, which desyncs from whip's in the auto/unknown case (SetUnknownTheme
// leaves lipgloss at its dark default), rendering dark panels on a light
// terminal. When the background is unknown, each role falls back to a
// terminal-palette-safe value (ANSI 0-15, or no fill) so nothing assumes
// light or dark — mirroring the markdown neutralStyle.
// ocThemeKnown reports whether whip resolved the terminal background — glyph
// art that depends on a bg-matched color (the prompt's ▀ shadow) must skip
// rendering when it's unknown, or it draws in the default fg (a black bar on a
// light terminal).
func ocThemeKnown() bool {
	mdMu.Lock()
	defer mdMu.Unlock()
	return mdKnown
}

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
	// opencodeRightMargin keeps text off the terminal edge when there is no
	// sidebar; the transcript scrollbar draws in it.
	opencodeRightMargin = 1
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
	th := currentTheme()
	left := th.On(th.Muted, nil)
	right := th.On(th.Text, nil).Bold(true)
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
	return m.termWidth >= sidebarMinWidth && !m.sidebarHide
}

// sidebarView renders the opencode right sidebar: session title, a Context
// block (tokens / % of window / spend), LSP status, and a footer. Height is
// the number of rows to fill so the sidebar spans the body. All styling uses
// whip's theme styles, so it honors light/dark/auto.
func (m *model) sidebarView(height int) string {
	if m.replPanel {
		return m.replPanelView(height)
	}
	// Every style carries the panel background so text doesn't punch holes in
	// the filled panel column.
	th := currentTheme()
	bg := th.Surface.Panel
	head := th.On(th.Text, bg).Bold(true)
	dim := th.On(th.Muted, bg)

	title := strings.TrimSpace(m.sessTitle)
	if value, ok := m.runtimeAgent(m.agentOpen); ok {
		title = value.Name
	}
	if title == "" {
		title = filepath.Base(m.completionRoot()) // untitled session: fall back to the working dir
	}

	var b strings.Builder
	b.WriteString(head.Render(truncLine(title, sidebarWidth-4)) + "\n\n")

	// Context: tokens used, share of the window, spend.
	b.WriteString(head.Render("Context") + "\n")
	u := m.displayUsage()
	b.WriteString(dim.Render(fmtTok(u.PromptTokens+u.CompletionTokens)+" tokens") + "\n")
	if limit := m.displayContextLimit(); limit > 0 {
		pct := estimateTokens(m.displayMessages()) * 100 / limit
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

	// Agent tree (opencode mode has no dock under the input).
	if agents := m.agentTreeRows(sidebarWidth-3, newReplStyles(bg), false); len(agents) > 0 {
		b.WriteString("\n" + strings.Join(agents, "\n") + "\n")
	}

	// Top content (title + Context + LSP), clipped if the sidebar is very short.
	top := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	bullet := th.On(th.Success, bg)
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
	// the sidebar is set apart by a panel background (no border); every row is
	// padded to the column width so the WHOLE column carries the shade
	pad2 := th.On(nil, bg).Render("  ")
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ui.PadRow(pad2+r, sidebarWidth, bg)
	}
	return strings.Join(out, "\n")
}

// lspSummary is a one-line LSP status for the sidebar: a connected count, or a
// disabled note when no LSP manager is configured.
func (m *model) lspSummary() string {
	return "managed by daemon"
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
	th := currentTheme()
	ebg := th.Surface.Element
	elem := th.On(nil, ebg)
	bar := th.On(th.Info, ebg).Render("┃")
	// truncate BEFORE padding: a full-width input line (bar + 2-space gutter +
	// content) exceeds width, wraps in the terminal, and grows the alt-screen
	// frame a row past layout()'s budget — skewing every mouse-Y hit-test
	row := func(content string) string { return ui.PadRow(ansi.Truncate(content, width, ""), width, ebg) }
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
	agent := th.On(th.Info, ebg)
	txt := th.On(th.Text, ebg)
	muted := th.On(th.Muted, ebg)
	meta := agent.Render(m.ocModeLabel()) + muted.Render(" · ") + txt.Render(m.modelName) + muted.Render("  "+m.provName)
	b.WriteString(row(bar+elem.Render("  ")+meta) + "\n")
	// Soft bottom edge: a ╹ tail then a ▀ line the SAME color as the box fill, so
	// it reads as the box's rounded bottom rather than a bright bar. When the
	// terminal background is unknown there is no box fill to match — skip the ▀
	// glyphs (they'd render in the default fg: a solid black bar on a light
	// terminal) and keep just the bar tail so the row count stays stable.
	b.WriteString(th.On(th.Info, nil).Render("╹"))
	if ebg != nil {
		shadow := th.On(ebg, nil)
		b.WriteString(shadow.Render(strings.Repeat("▀", max(width-1, 0))))
	}
	return b.String()
}

// opencodeUserCard renders a user turn as opencode's bordered card: a ┃ left
// bar (accent color) with one blank padding row above and below the text.
// Themed with whip's styles (no forced background), so the bar + padding give
// the card impression while honoring light/dark/auto.
func opencodeUserCard(text string, width int) string {
	if width < 4 {
		return text
	}
	th := currentTheme()
	bg := th.Surface.Panel
	bar := th.On(th.Info, bg).Render("┃")
	txt := th.On(th.Text, bg)
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
		b.WriteString(ui.PadRow(content, width, bg)) // fill the row to width with the panel bg
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
	th := currentTheme()
	muted := th.On(th.Muted, nil)
	txt := th.On(th.Text, nil)
	// right side: "{tokens} ({pct})  " muted, then "ctrl+p" in text, " commands" muted.
	rightRaw := ""
	if u := m.displayUsage(); u.PromptTokens+u.CompletionTokens > 0 {
		rightRaw = strings.ToUpper(fmtTok(u.PromptTokens + u.CompletionTokens)) // opencode uses uppercase (15.8K)
		if limit := m.displayContextLimit(); limit > 0 {
			rightRaw += fmt.Sprintf(" (%d%%)", estimateTokens(m.displayMessages())*100/limit)
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
		left := m.completionRoot()
		if lipgloss.Width(left)+rightW+2 > w { // no room: truncate the cwd, keep the right side
			left = truncLine(left, max(w-rightW-2, 0))
		}
		leftR = muted.Render(" " + left)
	}
	// the busy side (spinner + esc hint) has no width-aware trim of its own:
	// the bar clamps the row or it wraps the alt-screen frame on a narrow
	// terminal and shifts all mouse math
	return ui.StatusBar{Left: leftR, Right: right, Width: w}.Render(th)
}

func estimateTokens(messages []llm.Message) int {
	bytes := 0
	for _, message := range messages {
		bytes += len(message.TextContent())
		for _, call := range message.ToolCalls {
			bytes += len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	return (bytes + 3) / 4
}

// ocDialogRows renders the command palette as a ui.List: bold "Commands" +
// esc, a Search row, accent category headers, name-left / hint-right rows
// with the selection as a full-width primary fill.
func (m *model) ocDialogRows() []string {
	p := m.palette
	var groups []ui.ListGroup
	for _, it := range p.items {
		hint := ""
		if it.dynHint != nil {
			hint = it.dynHint(m)
		}
		if n := len(groups); n == 0 || groups[n-1].Title != it.category {
			groups = append(groups, ui.ListGroup{Title: it.category})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, ui.ListItem{Left: it.title, Right: hint})
	}
	return ui.List{Title: "Commands", Hint: "esc", Search: true, Query: p.filter, Groups: groups, Sel: p.idx,
		Empty: "No results found", Width: m.dialogWidth(), Window: m.dialogWindow(12)}.Render(currentTheme())
}

// dialogWidth is the floating dialogs' panel width.
func (m *model) dialogWidth() int { return min(64, max(m.width-2, 20)) }

// dialogWindow is how many list rows a dialog shows, leaving chrome rows of
// the terminal for its header, footer and margins; 0 (no window) before the
// terminal size is known.
func (m *model) dialogWindow(chrome int) int {
	if m.height <= 0 {
		return 0
	}
	return max(m.height-chrome, 4)
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
	case "skill":
		return "→"
	default:
		return "⚙"
	}
}

// ocToolLabel is the display name + separator for a tool row.
func ocToolLabel(name string) (label, sep string) { return toolHeaderName(name), " " }

// ocToolRow renders a completed tool call opencode-style: indent 3, an icon,
// the tool name in text color, and the subject muted. Failed calls go red.
func ocToolRow(name, args string, failed bool) string {
	icon, subject := ocToolIcon(name), toolSubject(name, args)
	label, sep := ocToolLabel(name)
	th := currentTheme()
	if failed {
		return "   " + th.On(th.Error, nil).Render(icon+" "+label+sep+subject)
	}
	txt := th.On(th.Text, nil)
	muted := th.On(th.Muted, nil)
	return "   " + muted.Render(icon) + " " + txt.Render(label) + muted.Render(sep+subject)
}

// ocToolPending renders a queued/running tool call: opencode's "~ " prefix,
// all muted.
func ocToolPending(name, args string) string {
	th := currentTheme()
	muted := th.On(th.Muted, nil)
	label, sep := ocToolLabel(name)
	return "   " + muted.Render("~ "+label+sep+toolSubject(name, args))
}

// ocToolResult renders a tool result block: collapsed to a single muted "↳ N
// lines" hint (opencode tucks results away behind the tool row), the full body
// indented when expanded. Errors keep the error color.
func ocToolResult(lines []string, expanded, isErr bool, width int) string {
	th := currentTheme()
	style := th.On(th.Muted, nil)
	if isErr {
		style = th.On(th.Error, nil)
	}
	// short results (a launch confirmation, a one-line answer) read inline —
	// a "↳ 1 line · expand" hint for one line is pure friction
	if !expanded && len(lines) > 2 {
		return "   " + style.Render(fmt.Sprintf("↳ %d lines · ctrl+e or click expands", len(lines)))
	}
	return wrap(style.Render("   ↳ "+strings.Join(lines, "\n     ")), width)
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
	{"Fork", "create a new session", func(m *model, _ int) tea.Cmd { _, cmd := m.thinCommand("/fork"); return cmd }},
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
	var items []ui.ListItem
	for _, it := range a.items() {
		items = append(items, ui.ListItem{Left: it.name, Right: it.desc})
	}
	return ui.List{Title: "Message Actions", Hint: "esc", Search: true, Query: a.filter, Groups: []ui.ListGroup{{Items: items}},
		Sel: a.sel, Empty: "No results found", Width: m.dialogWidth()}.Render(currentTheme())
}

// ocModelDialogRows renders the model picker as opencode's "Select model"
// dialog: Search line, provider names as accent group headers, the selected
// row a primary fill, catalog-only routes marked (new).
func (m *model) ocModelDialogRows() []string {
	p := m.mpicker
	var groups []ui.ListGroup
	for _, it := range p.view() {
		mark := ""
		if it.fromCatalog {
			mark = "(new)"
		}
		cur := "  "
		if it.model == m.modelName && it.provider == m.provName {
			cur = "● " // the current-model gutter
		}
		if n := len(groups); n == 0 || groups[n-1].Title != it.provider {
			groups = append(groups, ui.ListGroup{Title: it.provider})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, ui.ListItem{Left: cur + it.model, Right: mark})
	}
	return ui.List{Title: "Select model", Hint: "esc", Search: true, Query: p.filter.query, Groups: groups, Sel: p.idx,
		Empty: "No results found", Footer: []string{"enter", "select", "type", "to filter"},
		Width: m.dialogWidth(), Window: m.dialogWindow(14)}.Render(currentTheme())
}

// ocSessionDialogRows renders the resume picker as opencode's "Sessions"
// dialog: date group headers in accent, title left / age right, selected row
// a primary fill.
func (m *model) ocSessionDialogRows() []string {
	p := m.picker
	var groups []ui.ListGroup
	today := m.nowFn().Format("Mon Jan 2 2006")
	for _, meta := range p.metas {
		day := meta.UpdatedAt.Format("Mon Jan 2 2006")
		if day == today {
			day = "Today"
		}
		title := meta.Title
		if title == "" {
			title = "(untitled)"
		}
		title = ansi.Truncate(title, max(m.dialogWidth()-16, 4), "…")
		if n := len(groups); n == 0 || groups[n-1].Title != day {
			groups = append(groups, ui.ListGroup{Title: day})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, ui.ListItem{Left: title, Right: ago(meta.UpdatedAt)})
	}
	return ui.List{Title: "Sessions", Hint: "esc", Groups: groups, Sel: p.idx, Empty: "No sessions",
		Footer: []string{"enter", "resume", "↑/↓", "select"}, Width: m.dialogWidth(), Window: m.dialogWindow(12)}.Render(currentTheme())
}

// toastClearMsg expires the toast set by showToast.
type toastClearMsg struct{ at time.Time }

// showToast displays opencode's top-right toast for 5s (a new toast replaces
// the current one and resets the timer).
func (m *model) showToast(msg string) tea.Cmd { return m.toastOf(ui.Success, msg) }

// toastError reports a failed local command in the toast instead of writing
// into the transcript, which is the conversation.
func (m *model) toastError(msg string) tea.Cmd { return m.toastOf(ui.Error, msg) }

func (m *model) toastOf(kind ui.Kind, msg string) tea.Cmd {
	m.toast, m.toastKind = msg, kind
	at := m.nowFn()
	m.toastAt = at
	return tea.Tick(5*time.Second, toastClear(at))
}

// toastClear builds the tick payload carrying the toast's timestamp, so a
// stale timer can't clear a newer toast.
func toastClear(at time.Time) func(time.Time) tea.Msg {
	return func(time.Time) tea.Msg { return toastClearMsg{at: at} }
}

// toastRows renders the toast box (panel bg, success-colored side bars); View
// places it top-right.
func (m *model) toastRows() []string {
	w := min(lipgloss.Width(m.toast)+5, max(m.termWidth-10, 12))
	return []string{ui.Toast{Text: m.toast, Kind: m.toastKind, Width: w}.Render(currentTheme())}
}

// ocLeaderChord dispatches an opencode leader chord (ctrl+x then a key,
// within 2s). Returns handled=false for unknown keys.
func (m *model) ocLeaderChord(k string) (tea.Model, tea.Cmd, bool) {
	switch k {
	case "m": // model list
		m.openModelPicker(false)
	case "l": // session list
		return mcCmd(m.thinCommand("/resume"))
	case "n": // new session
		return mcCmd(m.thinCommand("/clear"))
	case "b": // sidebar toggle
		m.sidebarHide = !m.sidebarHide
		m.recalcWidth()
	case "r": // REPL panel in the sidebar
		m.replPanel = !m.replPanel
		m.recalcWidth()
	case "t": // theme list
		m.openThinThemePalette()
	case "c": // compact
		return mcCmd(m.thinCommand("/compact"))
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

// recalcWidth derives the main-column width from the terminal width and the
// sidebar state (the one place this math lives: WindowSizeMsg and the
// runtime sidebar/REPL toggles both call it).
func (m *model) recalcWidth() {
	if m.termWidth == 0 {
		return
	}
	w := m.termWidth - opencodeLeftMargin
	if m.termWidth >= sidebarMinWidth && !m.sidebarHide {
		w -= m.panelWidth() + opencodeRightGap
	} else {
		w -= opencodeRightMargin
	}
	if w != m.width {
		m.width = w
		m.input.SetWidth(w - 3) // the prompt box gutter "┃  " takes three cells
		m.refreshVP()
	}
}

// msgActionsKey handles keys while the Message Actions dialog is open.
func (m *model) msgActionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		if msg.Text != "" {
			a.filter += msg.Text
			a.sel = 0
		}
	}
	return m, nil
}

// opencodeAttribution renders opencode's per-response attribution line:
// "▣  {mode} · {model} · {duration}", indented 3 to sit under the assistant body.
func (m *model) opencodeAttribution(d time.Duration) string {
	th := currentTheme()
	agent := th.On(th.Info, nil)
	txt := th.On(th.Text, nil)
	muted := th.On(th.Muted, nil)
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
	eff := m.displayEffort()
	if eff == "" {
		eff = "off"
	}
	return strings.ToUpper(eff[:1]) + eff[1:]
}

// applyOpencodeStyles installs the full-screen UI's input chrome and spinner.
// It runs at startup and again whenever the color scheme changes, because the
// input box fill is derived from the detected terminal background.
func (m *model) applyOpencodeStyles() {
	invalidateMDRenderer() // the markdown style follows the scheme; rebuild
	th := currentTheme()
	m.spin.Spinner = ocKnightRider // keep the model (and its tick ID): a new one would orphan the running loop
	m.spin.Style = th.Spinner
	// Fill the textarea with the element background so the input box reads as
	// a filled panel (opencode's prompt box).
	st := m.input.Styles()
	st.Focused.Text = th.Textarea.Focused.Text
	st.Focused.CursorLine = th.Textarea.Focused.CursorLine
	st.Focused.Placeholder = th.Textarea.Focused.Placeholder
	st.Blurred.Text = th.Textarea.Blurred.Text
	st.Blurred.Placeholder = th.Textarea.Blurred.Placeholder
	st.Cursor = th.Textarea.Cursor
	m.input.SetStyles(st)
	m.input.Focus()
}
