package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// grok.go implements whip's "grok" UI mode: a full-screen render mode that
// reproduces Grok Build's TUI (the `grok` CLI; grok-build/crates/codegen/
// xai-grok-pager) pixel-for-pixel where the terminal allows: the rounded
// prompt box with the model line inside its bottom border, the braille
// wordmark on the home screen, ◆ tool rows, "Thought for Xs" reasoning
// blocks, the braille-spinner turn status with a ⇣ token count, and the
// GrokNight/GrokDay palettes resolved against whip's own light/dark theme
// detection (mirroring how opencode.go anchors its colors to mdLight/mdKnown
// so auto/unknown terminals never render inverted panels).
//
// Enabled with config UIMode == "grok" or via the command palette
// (Display → UI mode).

// grokMode is the config/UIMode value that selects this render mode.
const grokMode = "grok"

// gkActive mirrors m.uiMode == grokMode at package scope so block.render (a
// method on block, not model) can branch on the render mode. Set by
// applyUIMode.
var gkActive bool

// gkPick resolves a grok palette color against whip's OWN detected theme
// (mdLight/mdKnown), exactly like opencode's ocPick: dark value on dark
// terminals, light value on light ones, and a terminal-palette-safe neutral
// (ANSI 0-15, or no fill) when the background is unknown — so nothing assumes
// light or dark on a terminal whose background we couldn't detect.
func gkPick(dark, light, neutral string) lipgloss.TerminalColor {
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

// gkPadTo pads content to width with spaces EXPLICITLY styled with the given
// background — lipgloss's Style.Width padding lands after nested segments'
// closing resets without re-opening the background (see ocPadTo).
func gkPadTo(content string, width int, bg lipgloss.TerminalColor) string {
	return ocPadTo(content, width, bg)
}

// gkOnBg lays a pre-styled line onto a background, re-opening the bg after
// every embedded full reset (see ocOnBg).
func gkOnBg(ln string, bg lipgloss.TerminalColor) string {
	return ocOnBg(ln, bg)
}

// gkThemeKnown reports whether whip resolved the terminal background (see
// ocThemeKnown).
func gkThemeKnown() bool { return ocThemeKnown() }

// GrokNight / GrokDay palette roles (xai-grok-pager-render/src/theme/
// groknight.rs, grokday.rs). Each function returns the theme-resolved color;
// the third gkPick argument is the unknown-terminal fallback (an ANSI 0-15
// index or "" for the terminal default).

func gkTextCol() lipgloss.TerminalColor    { return gkPick("#e1e1e1", "#262626", "") }  // text_primary
func gkTextSecCol() lipgloss.TerminalColor { return gkPick("#c8c8c8", "#444444", "7") } // text_secondary
func gkMutedCol() lipgloss.TerminalColor   { return gkPick("#6c6c6c", "#767676", "8") } // gray
func gkGrayDimCol() lipgloss.TerminalColor { return gkPick("#585858", "#a5a5a5", "8") } // gray_dim
func gkGrayBrtCol() lipgloss.TerminalColor { return gkPick("#787878", "#626262", "8") } // gray_bright

func gkUserCol() lipgloss.TerminalColor      { return gkPick("#c8c8c8", "#444444", "7") } // accent_user
func gkAssistantCol() lipgloss.TerminalColor { return gkPick("#bb9af7", "#7d4bc6", "5") } // accent_assistant (magenta)
func gkThinkingCol() lipgloss.TerminalColor  { return gkPick("#bb9af7", "#7d4bc6", "5") } // accent_thinking
func gkToolCol() lipgloss.TerminalColor      { return gkPick("#787878", "#626262", "8") } // accent_tool
func gkSystemCol() lipgloss.TerminalColor    { return gkPick("#7aa2f7", "#2f64d2", "4") } // accent_system (blue)
func gkErrorCol() lipgloss.TerminalColor     { return gkPick("#f7768e", "#cd3048", "1") } // accent_error (red)
func gkSuccessCol() lipgloss.TerminalColor   { return gkPick("#9ece6a", "#378e23", "2") } // accent_success (green)
func gkCommandCol() lipgloss.TerminalColor   { return gkPick("#e0af68", "#a27612", "3") } // command (yellow)
func gkPathCol() lipgloss.TerminalColor      { return gkPick("#ff9e64", "#c3691e", "3") } // path (orange)
func gkRunningCol() lipgloss.TerminalColor   { return gkPick("#7dcfff", "#0082aa", "6") } // running (cyan)
func gkWarnCol() lipgloss.TerminalColor      { return gkPick("#e0af68", "#a27612", "3") } // warning (yellow)
func gkFuzzyCol() lipgloss.TerminalColor     { return gkPick("#7aa2f7", "#2f64d2", "4") } // fuzzy_accent (blue)
func gkModelCol() lipgloss.TerminalColor     { return gkPick("#1abc9c", "#0a8e70", "6") } // accent_model (teal)
func gkPlanCol() lipgloss.TerminalColor      { return gkPick("#ffdb8d", "#a8780a", "3") } // accent_plan (golden)
func gkLinkCol() lipgloss.TerminalColor      { return gkPick("#7aa6da", "#2f64d2", "4") } // link_fg

// Prompt chrome: the rounded border is a dimmer gray when idle and a brighter
// gray when focused (prompt_border / prompt_border_active).
func gkPromptBorder() lipgloss.TerminalColor       { return gkPick("#323237", "#c8c8cd", "8") } // prompt_border
func gkPromptBorderActive() lipgloss.TerminalColor { return gkPick("#505058", "#a5a5af", "8") } // prompt_border_active

// Panel/band backgrounds. Grok's bands sit a few steps off the terminal
// background; like opencode's ocPanelBg we derive them RELATIVE to the real
// background when the OSC 11 reply captured its RGB (ocBgShift), so the bands
// read as raised layers on ANY terminal, and fall back to GrokNight/GrokDay's
// literal values otherwise. When the background is unknown there is no fill.
func gkBandBg() lipgloss.TerminalColor { // user-message band, dialog panels (bg_light)
	if c, ok := ocBgShift(16); ok {
		return c
	}
	return gkPick("#242424", "#dedede", "")
}

func gkHoverBg() lipgloss.TerminalColor { // hover band (bg_hover)
	if c, ok := ocBgShift(24); ok {
		return c
	}
	return gkPick("#2c2c2c", "#d0d0d0", "")
}

// gkSelBg is the selected-row fill in pickers and menus (bg_visual).
func gkSelBg() lipgloss.TerminalColor { return gkPick("#363636", "#c6c6c6", "7") }

// gkSelBar is the ▏ selection indicator color (selection border is a subtle
// gray; the bar glyph itself is rendered in the accent color for legibility).
func gkSelBar() lipgloss.TerminalColor { return gkPick("#3c3c41", "#b9b9be", "8") }

// Layout constants for grok's session screen. The content column carries a
// 2-cell horizontal margin on each side (H_MARGIN in views/welcome/mod.rs),
// the prompt box and bands span the content column edge-to-edge.
const (
	grokMargin = 2 // H_MARGIN: the left/right margin of grok's content column
)

// The grok braille wordmark (assets/logo/logo07.txt — the full 7-row art,
// shown when the terminal is tall enough, and logo05.txt — the 5-row small
// art). On grok the logo sits centered on the home screen; a sheen animation
// sweeps it (we render the static resting color, theme.gray).
var (
	gkLogoFull = []string{
		"⠀⠀⠀⠀⠀⠀⣀⣀⡀⠀⠀⠀⢀⠄",
		"⠀⠀⠀⣠⣾⠿⠛⠛⠛⠛⢀⡴⠁⠀",
		"⠀⠀⣼⡟⠁⠀⠀⠀⢀⡴⠻⣿⡀⠀",
		"⠀⠀⣿⡇⠀⠀⠀⠔⠁⠀⠀⣿⡇⠀",
		"⠀⠀⢹⣷⠀⠀⠀⠀⠀⢀⣴⡿⠀⠀",
		"⠀⢀⠞⠁⠠⢶⣶⣶⣶⠿⠋⠀⠀⠀",
		"⠐⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀",
	}
	gkLogoSmall = []string{
		"⠀⠀⠀⣀⣤⣤⣀⠀⠀⡠",
		"⠀⢀⡾⠋⠁⠀⢁⢴⡎⠀",
		"⠀⢸⡇⠀⠀⠐⠁⢀⣿⠀",
		"⠀⢈⠗⢀⣀⣀⣠⡾⠃⠀",
		"⠐⠁⠀⠈⠉⠉⠉⠀⠀⠀",
	}
)

// grokLogo renders the braille wordmark in the resting gray (Grok sweeps a
// sheen across it; the resting glyph color is theme.gray). Small art when the
// home area is short, matching pick_logo's height tiers (full ≥ 26 rows, small
// ≥ 22 — but those are terminal heights; our home area is already the content
// area, so we pick on the area itself).
func grokLogo(height int) string {
	art := gkLogoFull
	if height < 12 { // tight home area: the small art
		art = gkLogoSmall
	}
	st := lipgloss.NewStyle().Foreground(gkMutedCol())
	var b strings.Builder
	for i, ln := range art {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(st.Render(ln))
	}
	return b.String()
}

// grokHome renders the empty-state "home" screen: the braille wordmark
// centered in the transcript area, like Grok Build's welcome screen before
// any messages (logo centered, the rounded prompt below it — which viewBody
// appends separately).
func grokHome(width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, grokLogo(height))
}

// grokPrompt wraps the textarea in Grok Build's prompt chrome: a rounded box
// (╭─╮ top, │ sides, ╰─╯ bottom) with the model info line INSIDE the bottom
// border — "╰─ grok-code-fast · effort ──╯" style. The top border carries the
// session title, right-aligned. Border color is the dimmer prompt_border when
// idle and prompt_border_active when the box is focused (whip's input is
// effectively always focused in the session view, so we use the active color).
//
// inner is m.input.View() (the textarea renders its own "┃ " prompt; we strip
// it and supply grok's "❯ " prefix ourselves on the first text row).
func (m *model) grokPrompt(inner string, width int) string {
	if width < 6 {
		return inner
	}
	border := lipgloss.NewStyle().Foreground(gkPromptBorderActive())
	muted := lipgloss.NewStyle().Foreground(gkMutedCol())
	grayDim := lipgloss.NewStyle().Foreground(gkGrayDimCol())
	// The info-line caption (model name) blends toward text_secondary at ~0.6
	// over the box bg — on the terminal default bg that is simply a slightly
	// brighter-than-gray tone.
	caption := lipgloss.NewStyle().Foreground(gkTextSecCol())

	innerW := width - 2 // the two │ side borders

	var b strings.Builder

	// Top divider: ╭──────────╮ with the session title inlined right-aligned
	// (2 cells before ╮), caption-styled.
	top := border.Render("╭" + strings.Repeat("─", innerW) + "╮")
	if title := strings.TrimSpace(m.sessTitle); title != "" && width >= 12 {
		label := " " + title + " "
		if w := lipgloss.Width(label); w <= innerW-2 {
			pad := innerW - 2 - w // ends 2 cells before ╮
			top = border.Render("╭"+strings.Repeat("─", pad)) + caption.Render(label) + border.Render("──╮")
		}
	}
	b.WriteString(top + "\n")

	// Text rows: │ ❯ content …│. The textarea renders its own prompt ("┃ " by
	// default); in grok mode applyUIMode sets the prompt to "❯ " already, so
	// the inner view arrives grok-ready — we only frame it with the side
	// borders. The textarea pads its lines to its width with plain spaces;
	// trim and re-pad so the fill lands inside the right border.
	for ln := range strings.SplitSeq(inner, "\n") {
		ln = strings.TrimRight(ln, " ")
		if lipgloss.Width(ln) > innerW-2 {
			ln = ansi.Truncate(ln, innerW-2, "")
		}
		pad := innerW - 2 - lipgloss.Width(ln)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(border.Render("│") + " " + ln + strings.Repeat(" ", pad) + " " + border.Render("│") + "\n")
	}

	// Bottom divider: ╰────────╯ with the model info line inside: a leading
	// pad space, model name (caption style), " · " separators (gray_dim), and
	// flags (gray). whip's analog of grok's flags is the reasoning effort.
	model := m.modelName
	if model == "" {
		model = "grok"
	}
	info := caption.Render(model)
	if eff := m.gkModeLabel(); eff != "" {
		info += grayDim.Render(" · ") + muted.Render(eff)
	}
	infoW := lipgloss.Width(info)
	// Place the info line starting 1 cell after ╰; the fill runs around it.
	if infoW <= innerW-3 {
		lead := 1 // one ─ between ╰ and the info line
		trail := innerW - lead - infoW
		if trail < 1 {
			trail = 1
		}
		b.WriteString(border.Render("╰"+strings.Repeat("─", lead)) + info + border.Render(strings.Repeat("─", trail)+"╯"))
	} else {
		b.WriteString(border.Render("╰" + strings.Repeat("─", innerW) + "╯"))
	}
	return b.String()
}

// grokUserCard renders a user turn as Grok Build renders it: the text rows on
// a full-width background band (bg_light), prefixed by "❯ " in accent_user
// with the body in text_primary. Hover lifts the band to bg_hover.
func grokUserCard(text string, width int, hover bool) string {
	if width < 4 {
		return text
	}
	bg := gkBandBg()
	if hover {
		bg = gkHoverBg()
	}
	prefix := lipgloss.NewStyle().Foreground(gkUserCol()).Background(bg).Render("❯ ")
	txt := lipgloss.NewStyle().Foreground(gkTextCol()).Background(bg)
	lines := strings.Split(wrap(text, width-2), "\n")
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		var content string
		if i == 0 {
			content = prefix + txt.Render(ln)
		} else {
			content = strings.Repeat(" ", 2) + txt.Render(ln)
		}
		b.WriteString(gkPadTo(content, width, bg)) // the band spans the full content width
	}
	return b.String()
}

// gkBraille is Grok Build's busy spinner: the rotating braille frames
// (⠋⠙⠹⠸⠼⠴⠦⠧) at ~7.5fps (turn_status SPINNER_DIVISOR = 4 ticks
// at ~30fps ≈ 133ms per frame).
var gkBraille = spinner.Spinner{
	Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"},
	FPS:    133 * time.Millisecond,
}

// grokStatus renders Grok Build's bottom status row. Idle: the working
// directory on the left (gray) and the context usage "8.5K / 1.0M" plus model
// on the right. Busy: the braille spinner and activity label on the left with
// the phase timer, and the turn timer + ⇣ token count + [stop] on the right —
// the turn-status row that sits between the scrollback and the prompt in
// grok, folded into whip's single status line.
func (m *model) grokStatus() string {
	muted := lipgloss.NewStyle().Foreground(gkMutedCol())
	grayDim := lipgloss.NewStyle().Foreground(gkGrayDimCol())
	txt := lipgloss.NewStyle().Foreground(gkTextCol())
	w := max(m.width, 0)

	if m.busy {
		// Turn status: "⠋ Thinking… 1m15s ⇣12k [stop]" — spinner + activity label
		// (thinking accent) on the left, the turn timer + token count + stop
		// button on the right. The timer shows ONCE, right-aligned.
		spin := m.spin.View()
		left := spin + lipgloss.NewStyle().Foreground(gkThinkingCol()).Render("Thinking…")
		// Right side: turn timer + token count + stop button.
		var right string
		if !m.turnStart.IsZero() {
			right = muted.Render(fmtGrokDur(m.nowFn().Sub(m.turnStart)))
		}
		if u := m.agent.Usage(); u.PromptTokens+u.CompletionTokens > 0 {
			if right != "" {
				right += muted.Render(" ")
			}
			right += muted.Render("⇣" + gkFmtTokens(u.PromptTokens+u.CompletionTokens))
		}
		stop := lipgloss.NewStyle().Foreground(gkErrorCol()).Render("[stop]")
		if right != "" {
			right += " " + stop
		} else {
			right = stop
		}
		rightW := lipgloss.Width(ansi.Strip(right))
		pad := max(w-lipgloss.Width(ansi.Strip(left))-rightW, 1)
		line := left + strings.Repeat(" ", pad) + right
		if w > 0 {
			line = ansi.Truncate(line, w, "")
		}
		return line
	}

	// Idle: cwd left (gray), context usage + model right.
	left := grayDim.Render(shortCWD())
	rightR := ""
	if u := m.agent.Usage(); u.PromptTokens+u.CompletionTokens > 0 {
		rightR = muted.Render(gkContextUsage(m)) + "  "
	}
	rightR += txt.Render(m.modelName)
	if m.provName != "" {
		rightR += muted.Render(" · " + m.provName)
	}
	rightW := lipgloss.Width(ansi.Strip(rightR))
	lw := lipgloss.Width(ansi.Strip(left))
	if lw+rightW+2 > w {
		// Truncate the cwd to its tail, keeping the right side intact.
		keep := max(w-rightW-2, 0)
		left = grayDim.Render(truncLine(ansi.Strip(left), keep))
		lw = lipgloss.Width(ansi.Strip(left))
	}
	pad := max(w-lw-rightW, 1)
	return left + strings.Repeat(" ", pad) + rightR
}

// gkContextUsage renders the context bar's default form: "8.5K / 1.0M" —
// actual tokens over the context limit, colored by usage percentage (the
// color blends text_primary → accent_user → warning → accent_error as the
// window fills; we step to the nearest breakpoint color).
func gkContextUsage(m *model) string {
	u := m.agent.Usage()
	used := u.PromptTokens + u.CompletionTokens
	limit := m.agent.ContextLimit
	if limit <= 0 {
		return gkFmtTokens(used)
	}
	pct := used * 100 / limit
	color := gkTextCol()
	switch {
	case pct >= 95:
		color = gkErrorCol()
	case pct >= 75:
		color = gkWarnCol()
	case pct >= 50:
		color = gkUserCol()
	}
	return lipgloss.NewStyle().Foreground(color).Render(gkFmtTokens(used) + " / " + gkFmtTokens(limit))
}

// gkFmtTokens formats a token count the way Grok's context bar does:
// raw under 1K, "1.2K" under 10K, "12K" under 1M, "1.2M" under 10M, "12M" above.
func gkFmtTokens(n int) string {
	switch {
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000.0)
	case n < 1_000_000:
		return fmt.Sprintf("%dK", n/1_000)
	case n < 10_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
	default:
		return fmt.Sprintf("%dM", n/1_000_000)
	}
}

// fmtGrokDur formats a duration Grok-style: "0.2s" under a minute, else
// "1m20s".
func fmtGrokDur(d time.Duration) string {
	secs := d.Seconds()
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs) / 60
	return fmt.Sprintf("%dm%02.0fs", mins, secs-float64(mins*60))
}

// gkModeLabel is whip's analog of grok's prompt flags: the reasoning effort,
// lowercased (grok renders its flags in muted gray).
func (m *model) gkModeLabel() string {
	eff := m.agent.Effort
	if eff == "" || eff == "off" {
		return ""
	}
	return eff
}

// gkToolBullet is the ◆ (diamond_filled) that opens every tool row.
const gkToolBullet = "◆"

// gkToolLabel maps a tool to Grok Build's verb + subject header forms:
// "Read path", "Search pattern", "Edit path", "List dir", "$ command".
func gkToolLabel(name string) (label, sep string) {
	switch name {
	case "bash":
		return "$ ", "" // shell style: "$ command"
	case "read":
		return "Read", " "
	case "edit", "write":
		return "Edit", " "
	case "grep", "glob":
		return "Search", " "
	case "webfetch":
		return "Fetch", " "
	case "websearch":
		return "Search", " "
	case "subagent", "subagent_steer":
		return "Task", " "
	default:
		return toolHeaderName(name), " "
	}
}

// grokToolRow renders a tool call Grok-style: a ◆ bullet (accent_tool gray,
// or red on failure), then the verb and subject. Collapsed tools mute fully.
func grokToolRow(name, args string, failed bool) string {
	label, sep := gkToolLabel(name)
	subject := toolSubject(name, args)
	if failed {
		e := lipgloss.NewStyle().Foreground(gkErrorCol())
		return e.Render(gkToolBullet + " " + label + sep + subject)
	}
	bullet := lipgloss.NewStyle().Foreground(gkToolCol()).Render(gkToolBullet)
	verb := lipgloss.NewStyle().Foreground(gkTextSecCol()).Render(label)
	sub := lipgloss.NewStyle().Foreground(gkMutedCol()).Render(sep + subject)
	if name == "bash" {
		// "$ command": the "$ " prompt is dim, the command is text_secondary.
		return bullet + " " + lipgloss.NewStyle().Foreground(gkGrayDimCol()).Render(label) +
			lipgloss.NewStyle().Foreground(gkTextSecCol()).Render(subject)
	}
	return bullet + " " + verb + sub
}

// grokToolPending renders a queued/running tool call: the ◆ bullet and the
// header, muted (grok renders in-flight rows in the running accent; whip's
// pending state is closer to grok's collapsed-muted look).
func grokToolPending(name, args string) string {
	muted := lipgloss.NewStyle().Foreground(gkMutedCol())
	label, sep := gkToolLabel(name)
	return muted.Render(gkToolBullet+" "+label+sep) +
		lipgloss.NewStyle().Foreground(gkGrayDimCol()).Render(toolSubject(name, args))
}

// grokToolResult renders a tool result: collapsed, a muted "↳ N lines" hint;
// expanded, the full body indented under the header. Errors go red.
func grokToolResult(lines []string, expanded, isErr, hover bool, width int) string {
	style := lipgloss.NewStyle().Foreground(gkMutedCol())
	if hover {
		style = lipgloss.NewStyle().Foreground(gkTextSecCol())
	}
	if isErr {
		style = lipgloss.NewStyle().Foreground(gkErrorCol())
	}
	if !expanded && len(lines) > 2 {
		return style.Render(fmt.Sprintf("  ↳ %d lines · ctrl+e or click expands", len(lines)))
	}
	return wrap(style.Render("  ↳ "+strings.Join(lines, "\n    ")), width)
}

// gkDimLine dims a backdrop line (SGR 2), re-applying after embedded resets —
// the same treatment as ocDimLine, reused for grok's modal overlays.
func gkDimLine(s string) string { return ocDimLine(s) }

// grokBoxKit bundles the styles and row builders every floating grok dialog
// shares: a panel on the band background with rounded borders, a bold header,
// and name-left / hint-right rows; the selected row carries the ▏ bar and
// bg_visual fill.
type grokBoxKit struct {
	w                              int
	bg                             lipgloss.TerminalColor
	pnl, text, head, muted, accent lipgloss.Style
	border                         lipgloss.Style
	blank                          string
}

func (m *model) newGrokBox() grokBoxKit {
	bg := gkBandBg()
	w := min(64, max(m.width-2, 20))
	text := lipgloss.NewStyle().Foreground(gkTextCol()).Background(bg)
	return grokBoxKit{
		w: w, bg: bg,
		pnl:    lipgloss.NewStyle().Background(bg),
		text:   text,
		head:   text.Bold(true),
		muted:  lipgloss.NewStyle().Foreground(gkMutedCol()).Background(bg),
		accent: lipgloss.NewStyle().Foreground(gkSystemCol()).Background(bg).Bold(true),
		border: lipgloss.NewStyle().Foreground(gkPromptBorder()).Background(bg),
		blank:  gkPadTo("", w, bg),
	}
}

// lr assembles left+right onto one padded row: left at col 2, right at the edge.
func (k grokBoxKit) lr(left, right string) string {
	gap := max(k.w-2-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return gkPadTo(k.pnl.Render("  ")+left+k.pnl.Render(strings.Repeat(" ", gap))+right, k.w, k.bg)
}

// selRow renders a selected row: a ▏ selection bar at col 0 and the bg_visual
// fill (Grok's picker highlight — no primary-color inversion).
func (k grokBoxKit) selRow(content string) string {
	selBg := gkSelBg()
	bar := lipgloss.NewStyle().Foreground(gkSystemCol()).Background(selBg).Render("▏")
	body := lipgloss.NewStyle().Foreground(gkTextCol()).Background(selBg).Render(" " + content)
	return gkPadTo(bar+body, k.w, selBg)
}

// gkDialogRows renders the command palette as grok's command dropdown: a
// Search line, accent category headers, and name-left / hint-right rows.
func (m *model) gkDialogRows() []string {
	p := m.palette
	k := m.newGrokBox()
	w, bg := k.w, k.bg
	pnl, text, head, muted, accent := k.pnl, k.text, k.head, k.muted, k.accent
	lr := k.lr
	blank := k.blank

	if pp := p.top(); pp != nil {
		rows := []string{blank, lr(head.Render("Commands › "+pp.title), muted.Render("esc")), blank}
		for ln := range strings.SplitSeq(strings.TrimRight(m.panelView(pp), "\n"), "\n") {
			rows = append(rows, gkPadTo(pnl.Render("  ")+gkOnBg(ln, bg), w, bg))
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
			rows = append(rows, k.selRow(it.title+strings.Repeat(" ", max(w-2-len(it.title)-lipgloss.Width(hint)-2, 1))+hint))
		} else {
			rows = append(rows, lr(text.Render(it.title), muted.Render(hint)))
		}
	}
	if len(p.items) == 0 {
		rows = append(rows, lr(muted.Render("No results found"), ""))
	}
	return append(rows, blank)
}

// gkMsgActionRows renders the Message Actions dialog.
func (m *model) gkMsgActionRows() []string {
	a := m.msgActions
	k := m.newGrokBox()
	rows := []string{k.blank, k.lr(k.head.Render("Message Actions"), k.muted.Render("esc")), k.blank}
	if a.filter == "" {
		rows = append(rows, k.lr(k.muted.Render("Search"), ""))
	} else {
		rows = append(rows, k.lr(k.text.Render(a.filter), ""))
	}
	rows = append(rows, k.blank)
	items := a.items()
	for i, it := range items {
		if i == a.sel {
			rows = append(rows, k.selRow(it.name+" "+it.desc))
		} else {
			rows = append(rows, k.lr(k.text.Render(it.name)+k.muted.Render(" "+it.desc), ""))
		}
	}
	if len(items) == 0 {
		rows = append(rows, k.lr(k.muted.Render("No results found"), ""))
	}
	return append(rows, k.blank)
}

// gkModelDialogRows renders the model picker.
func (m *model) gkModelDialogRows() []string {
	p := m.mpicker
	k := m.newGrokBox()
	rows := []string{k.blank, k.lr(k.head.Render("Select model"), k.muted.Render("esc")), k.blank}
	if p.filter.query == "" {
		rows = append(rows, k.lr(k.muted.Render("Search"), ""))
	} else {
		rows = append(rows, k.lr(k.text.Render(p.filter.query), ""))
	}
	rows = append(rows, k.blank)

	items := p.view()
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
			cur = "● "
		}
		if i == p.idx {
			rows = append(rows, k.selRow(cur+it.model))
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

// gkSessionDialogRows renders the resume picker.
func (m *model) gkSessionDialogRows() []string {
	p := m.picker
	k := m.newGrokBox()
	rows := []string{k.blank, k.lr(k.head.Render("Sessions"), k.muted.Render("esc")), k.blank}
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
			rows = append(rows, k.selRow(title))
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

// gkSpliceToast paints the toast box into the frame's top-right corner (grok's
// toasts sit top-right on a panel band with a leading accent bar).
func (m *model) gkSpliceToast(v string) string {
	bg := gkBandBg()
	pnl := lipgloss.NewStyle().Background(bg)
	bar := lipgloss.NewStyle().Foreground(gkSuccessCol()).Background(bg).Render("┃")
	txt := lipgloss.NewStyle().Foreground(gkTextCol()).Background(bg)
	inner := truncLine(m.toast, max(min(56, m.termWidth-10), 8))
	w := lipgloss.Width(inner) + 6
	mid := bar + pnl.Render("  ") + txt.Render(inner) + pnl.Render("  ") + bar
	pad := gkPadTo(bar, w-1, bg) + bar
	return ocSpliceAt(v, []string{pad, mid, pad}, max(m.termWidth-w-2, 0), 2)
}

// grokAttribution renders the per-response footer Grok-style: a muted line
// under the assistant body with the model and the turn duration.
// "  model · 2.4s" — grok has no whip-style mode chip; the teal model color
// (accent_model) is grok's model-name tint.
func (m *model) grokAttribution(d time.Duration) string {
	model := lipgloss.NewStyle().Foreground(gkModelCol())
	muted := lipgloss.NewStyle().Foreground(gkMutedCol())
	return model.Render(m.modelName) + muted.Render(" · "+fmtGrokDur(d))
}

// gkRecalcWidth recomputes the content width from the terminal width (mirrors
// the WindowSizeMsg math) — needed when something toggles chrome at runtime.
func (m *model) gkRecalcWidth() {
	if m.uiMode != grokMode || m.termWidth == 0 {
		return
	}
	w := m.termWidth - 2*grokMargin
	if w != m.width {
		m.width = w
		m.input.SetWidth(w - 2)
		m.refreshVP()
	}
}

// gkOverlay draws a dialog OVER the live session: the whole frame keeps
// rendering behind the modal, dimmed, with the dialog rows spliced in
// centered (upper third) — same overlay mechanics as ocOverlay.
func (m *model) gkOverlay(v string) string {
	return m.ocOverlayRows(v, m.gkDialogRows())
}

// gkMenuOverlay draws the completion popup on top of the frame, anchored just
// above the input box (grok's slash dropdown position) — the frame beneath
// never reflows while typing.
func (m *model) gkMenuOverlay(v string) string {
	rows := strings.Split(m.menuView(), "\n")
	if len(rows) > m.inputBodyOff {
		rows = rows[len(rows)-m.inputBodyOff:]
	}
	return ocSpliceAt(v, rows, grokMargin, m.inputBodyOff-len(rows))
}

// applyGrokInputStyles bakes grok's prompt styling into the textarea: the
// "❯ " prefix in accent_user, the "Build anything" placeholder, and plain
// (no-fill) text styles — grok's prompt is a bordered box on the terminal
// background, not a filled panel.
func (m *model) applyGrokInputStyles() {
	m.input.Prompt = "❯ "
	m.input.Placeholder = "Build anything"
	user := lipgloss.NewStyle().Foreground(gkUserCol())
	plain := lipgloss.NewStyle()
	m.input.FocusedStyle.Text = plain
	m.input.FocusedStyle.CursorLine = plain
	m.input.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(gkMutedCol())
	m.input.FocusedStyle.Prompt = user
	m.input.BlurredStyle.Text = plain
	m.input.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(gkMutedCol())
	m.input.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(gkMutedCol())
}

// gkLeaderChord is intentionally minimal: grok has no leader chords (that's
// an opencode-ism); the stub exists so the key dispatch can branch cleanly.
func (m *model) gkLeaderChord(k string) (tea.Model, tea.Cmd, bool) {
	return m, nil, false
}

// gkSidebarVisible reports whether a grok sidebar shows: grok has no sidebar,
// so this is always false (kept for symmetry with sidebarVisible).
func (m *model) gkSidebarVisible() bool { return false }
