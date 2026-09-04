// Package tui is whip's interactive bubbletea session (fullscreen alt-screen).
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// UI styles use AdaptiveColor so they stay legible on both dark and light
// terminal backgrounds (detected at startup by detectColorScheme).
var (
	youStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "21", Dark: "12"}).Bold(true) // blue
	botStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "90", Dark: "13"}).Bold(true) // purple/magenta
	toolStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "11"})           // amber
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"})          // mid gray
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "9"})            // red
	growStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "10"})            // green
	// thinkingStyle renders reasoning tokens: dim and italic so they're
	// visually distinct from the answer.
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).Italic(true)
)

// Marker glyphs prefixing user and assistant turns. Package-level so the
// opencode render mode can swap them (❯→┃, ●→▣) in one place; both defaults
// and both opencode glyphs are 2 cells wide, so no layout math changes. See
// applyOpencodeMode / applyDefaultTheme in opencode.go.
var (
	glyphUser      = "❯ "
	glyphAssistant = "● "
)

// messages sent from the agent goroutine
type (
	textMsg      string
	toolStartMsg struct{ id, name, args string }
	toolEndMsg   struct{ id, name, result string }
	// toolCallMsg is a tool call still streaming from the model (args may be
	// partial): renders a dim "queued" row that toolStartMsg replaces when
	// execution begins.
	toolCallMsg   struct{ id, name, args string }
	toolOutputMsg struct{ id, text string } // partial output for a running tool row
)
type (
	noticeMsg  string    // dim one-liner appended to the transcript
	usageMsg   llm.Usage // one request's token usage
	quitArmMsg struct{}  // the idle ctrl+c arm window expired
	thinkMsg   string    // streamed reasoning tokens
	imageMsg   struct {  // ctrl+v clipboard image result
		path string // clipboard image saved to disk
		err  error
	}
)

// menu is the open completion dropdown.
type menu struct {
	head   string // input before the token being completed
	cands  []cand
	idx    int
	base   string // input when tab cycling started; esc reverts to it
	cyc    bool   // tab/shift+tab cycling with live preview
	cycled bool   // a cycle step already happened (first tab previews, not advances)
	frozen []cand // full candidate set for the cycle's prefix (nil = live filter)
}

type model struct {
	cfg                 *config.Config
	client              *Client
	clientView          clientPresentation
	clientState         ClientState
	clientErr           error
	clientCursor        int64
	clientInFlight      int
	clientTouched       bool
	clientPromptOp      string
	clientPromptCut     int
	clientTerminalID    string
	clientTurnError     string
	terminalAgentID     string
	terminalMarker      string
	historyRequested    bool
	reloadAfterCatalogs bool
	modelName           string
	provName            string
	sysPrompt           string
	// cfgExtra pins scalar settings this session explicitly changed (theme,
	// effort, …): the config watcher applies file values only for keys not
	// pinned here, so a local pick this session survives another session's
	// unrelated save while still syncing changes made elsewhere.
	cfgExtra map[string]string
	cfgMod   time.Time // last observed config.json mod time (watcher baseline)

	input  textarea.Model
	spin   spinner.Model
	vp     viewport.Model
	blocks []block // finalized transcript (raw; rendered at the current width)
	// msgBlock[i] is the block index rendering agent.Messages[i] (-1: none) —
	// rewind live-scroll uses it to jump to a message's transcript position.
	msgBlock  []int
	follow    bool // auto-scroll to bottom on new content
	width     int  // content width: full terminal width, minus the opencode sidebar when it shows
	height    int
	termWidth int // full terminal width (opencode mode places the sidebar in the reserved columns)

	busy    bool
	current string // in-flight partial assistant line
	inMsg   bool   // "● " prefix already printed for this assistant segment
	// lastResp is the token usage of the most recent API response (updated
	// per streamed request via usageMsg); the status line shows it after the
	// session spend as "last in(cached)/out tok".
	lastResp llm.Usage
	plan     []daemon.PlanItem

	showThinking bool   // ctrl+o: render reasoning tokens
	curThink     string // in-flight partial reasoning line
	inThink      bool   // "◌ " thinking prefix printed for this reasoning segment
	menu         *menu
	picker       *picker
	mpicker      *modelPicker
	palette      *palette // ctrl+p command palette (modal dialog)
	prog         *tea.Program

	sessionID string

	hist     []string         // submitted inputs, for up/down recall
	pasteBuf string           // held paste text for the [Pasted ~N lines] placeholder (config collapsePaste)
	histIdx  int              // len(hist) == not navigating
	draft    string           // in-progress input saved while navigating history
	lastUp   time.Time        // last ↑ keypress; repeat detection for history rollover
	now      func() time.Time // test seam; defaults to time.Now

	turnStart  time.Time // when the in-flight turn began; zero when idle (busy line shows elapsed)
	thinkStart time.Time // opencode mode: when the current reasoning segment began (collapsed to "+ Thought: {dur}")

	interrupt1 bool // first ctrl+c pressed while busy; second cancels
	quit1      bool // first ctrl+c pressed while idle; second quits (armed briefly)

	goal string

	mouseOn  bool       // runtime mouse-capture state (toggle with /mouse)
	sel      *selection // in-flight/last drag selection over the transcript
	selDragX int        // last drag pointer position (edge auto-scroll re-checks it)
	selDragY int
	// Input box selection tracking: View records the input's absolute screen
	// rows so drag-select can hit-test/extract/highlight it. inputBodyOff is
	// the line offset within viewBody where the input starts; inputTop is the
	// absolute screen row (viewTop + inputBodyOff), -1 when hidden.
	inputBodyOff int
	inputTop     int
	inputLines   []string    // the input box's rendered lines, ANSI-stripped
	vpLead       int         // top blank rows viewportView last dropped (selection row mapping)
	viewTop      int         // screen row of the view's first line (View tracks it; mouse Y is absolute)
	viewH        int         // height of the last rendered view
	themeHow     string      // how auto theme detection resolved (env var, OSC query, …) — captured at startup/theme change for /report; never re-queried
	uiMode       string      // "" = default whip look; "opencode" = opencode render mode (see opencode.go)
	sessTitle    string      // cached session title for the opencode sidebar (from the store; updated on title/rename)
	msgActions   *msgActions // opencode mode: the Message Actions dialog opened by clicking a message; nil = closed
	hoverIdx     int         // opencode mode: block index under the mouse (hover highlight); -1 = none
	ocThink      string      // opencode mode: reasoning text accumulated for the expandable "+ Thought" block
	toast        string      // opencode mode: top-right toast text; "" = none
	toastAt      time.Time   // when the current toast was shown (stale clears are ignored)
	leaderAt     time.Time   // opencode mode: when ctrl+x armed the leader chord; zero = not pending
	sidebarHide  bool        // opencode mode: ctrl+x b hides the sidebar
	// updateLatest is a pending newer release tag ("" when none), picked up
	// from update.Pending at startup; the notice it renders is durable, so a
	// check that lands after the report still shows next launch.
	updateLatest string
	effortX      int
	catalogs     map[string]config.Catalog
	iactive      *interactive
	permDialog   *permDialog

	agentsFocus   bool // the agents dock owns ↑/↓/enter (never esc); typing or ↑ past the top returns to the input
	agentSel      int  // selected row in the dock (index into newest-first agents)
	agentOpen     string
	agentMessages map[string][]llm.Message
	replPanel     bool                  // opencode sidebar shows the live Starlark REPL (ctrl+x r, /repl)
	repl          map[string]*replAgent // per-agent cell history for the REPL panel
	replScroll    int                   // REPL panel rows scrolled up from the newest cell (0 follows)
	replViewAgent string                // agent the REPL panel last rendered (a switch resets the scroll)
	replBodyLen   int                   // REPL body rows at the last render (keeps scrolled-up content anchored)
	replReplaying bool                  // replRebuild in progress: replayed events carry no clock
	dockSkip      int                   // non-agent rows at the dock's top (focused hint) — click math skips them
	dockRows      int                   // rendered dock height; layout() maintains it for click math

	rew    *rewindState // open rewind picker (double-esc while idle)
	esc1   bool         // first idle esc pressed; second opens the rewind picker
	escClr bool         // first esc pressed with a draft; second clears it to history

	namePrompt *namePrompt // inline text prompt (fork naming, /rename)

	// infAuth holds the in-flight inference-net device login across the
	// team → project → create prompts.
	infAuth *inferenceNetPending

	// initialPrompt (whip up <words>) is submitted as the first turn from
	// Init — late enough that m.prog exists for the turn goroutine's p.Send.
	initialPrompt string
}

// picker is the /resume session browser. metas is newest-first; the list is
// rendered oldest-at-top so newest sits at the bottom.
type picker struct {
	metas    []session.Meta
	idx      int                  // selected index into metas (0 = newest)
	previews map[string][2]string // id -> last user, last assistant
}

// newInput builds the prompt textarea with whip's keybindings and styling.
// Newlines come from ctrl+j / shift+enter / alt+enter; plain enter submits.
func newInput() textarea.Model {
	ti := textarea.New()
	ti.Placeholder = inputPlaceholder
	ti.Prompt = "┃ "
	ti.SetHeight(1)
	ti.MaxHeight = 24 // input grows with content up to this many lines
	ti.ShowLineNumbers = false
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+j", "shift+enter", "alt+enter"),
		key.WithHelp("ctrl+j", "newline"),
	)
	// ctrl+k clears the conversation (handled in (*model).key); don't let the
	// textarea's default delete-after-cursor shadow it.
	ti.KeyMap.DeleteAfterCursor = key.NewBinding()
	// The default adaptive styles misdetect the background over mosh/tmux;
	// use plain ANSI colors and no cursor-line background.
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.FocusedStyle.Placeholder = dimStyle
	ti.BlurredStyle.Placeholder = dimStyle
	ti.FocusedStyle.Prompt = botStyle
	ti.BlurredStyle.Prompt = dimStyle
	ti.Focus()
	return ti
}

// tuiRunning gates the raw tty query to BEFORE bubbletea starts; bgCache
// carries the startup answer to runtime re-detections. Both are only touched
// from the main goroutine (Run pre-tea, then Update inside the event loop).
//
// This block must stay ABOVE Run: ineffassign (the golangci-lint gate)
// decides a package-level write is effectual from the first function in file
// order that reads the var, and Run's `tuiRunning = true` precedes the read
// in detectColorScheme — declared below Run, the write is misreported as
// ineffectual.
var (
	tuiRunning bool
	bgCache    bgResult
)

func (m *model) startupReport() {
	// opencode mode keeps the startup clean (quiet): the routine roster lines
	// are suppressed, but actionable items — skill-scan warnings, failed MCP
	// servers, the update notice — must still surface, never silently drop.
	quiet := m.uiMode == opencodeMode
	if quiet && !ocThemeKnown() {
		// an unknown background means the panels render with no fill — zero
		// contrast — so say why and how to fix it instead of failing silently
		m.append(dimStyle.Render("◐ terminal background unknown — panels have no contrast; run /theme light (or dark) once to fix (mosh blocks background detection)"))
	}
	sk, problems := skills.ScanDetailed(skills.DefaultDirs()...)
	var b strings.Builder
	var warned bool

	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\n", args...)
	}
	if len(sk) > 0 && !quiet {
		line("skills: %d loaded", len(sk))
	}
	for _, s := range sk {
		if s.Warning != "" {
			line("  ⚠ %s: %s", s.Name, s.Warning)
			warned = true
		}
	}
	for _, p := range problems {
		line("  ⚠ %s: %s", p.Path, p.Err)
		warned = true
	}
	if m.updateLatest != "" {
		line("update available: %s (run: whip update)", m.updateLatest)
		warned = true
	}
	if b.Len() == 0 {
		return
	}
	out := strings.TrimRight(b.String(), "\n")
	if warned {
		m.append(errStyle.Render(out))
	} else {
		m.append(dimStyle.Render(out))
	}
}

// enableClickWheelMouse turns on mouse reporting with SGR coordinates
// (?1006): click+wheel (?1000) plus button-event motion (?1002) so a held
// left-drag reports motion events — whip turns those into its own selection
// (select.go), because enabling ?1000 alone makes most terminals hand the drag
// to the app WITHOUT starting a native selection (Ghostty, kitty), leaving
// capture-on users with no drag-to-copy at all. Writing directly to the real
// TTY keeps bubbletea's output a terminal so terminal-size detection still
// works (unlike piping output through an os.Pipe). ?1002 (not ?1003) means
// motion bytes only flow while a button is held — passive moves stay silent.
//
// ?1002 alone — NOT ?1000 as well: terminals keep ONE mouse-tracking mode, so
// writing ?1000h after ?1002h silently downgrades tracking to click-only and
// drags stop reporting motion (no highlight, no copy). ?1002 is a superset of
// ?1000 (press/release/wheel all still report).
func enableClickWheelMouse(w *os.File) {
	fmt.Fprint(w, "\x1b[?1006h\x1b[?1002h")
}

// disableClickWheelMouse releases the mouse reporting enableClickWheelMouse
// set, plus ?1000 defensively (an older whip or a downgrade may have left it).
func disableClickWheelMouse(w *os.File) {
	fmt.Fprint(w, "\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?1006l")
}

// (No applyTmuxMouseFix: inside tmux the drag IS forwarded to whip — tmux's
// factory MouseDrag1Pane binding checks mouse_any_flag, which our ?1002 sets,
// and sends every press/motion/release into the pane (verified live). whip's
// own selection (select.go) paints and copies, exactly like Claude Code. The
// old copy-mode override was what made "tmux capture kick in".)

func (m *model) seedTranscript(msgs []llm.Message, base int) {
	fileRoot := m.completionRoot()
	for i, msg := range msgs {
		bi := -1
		switch msg.Role {
		case "user":
			bi = len(m.blocks)
			m.blocks = append(m.blocks, block{kind: blockUser, text: linkifyFilePathsAt(msg.TextContent(), fileRoot), fileRoot: fileRoot})
		case "assistant":
			if strings.TrimSpace(msg.TextContent()) != "" {
				bi = len(m.blocks)
				m.blocks = append(m.blocks, block{kind: blockAssistant, text: strings.TrimRight(msg.TextContent(), "\n"), fileRoot: fileRoot})
			}
			for _, tc := range msg.ToolCalls {
				m.blocks = append(m.blocks, block{kind: blockText, text: toolHeaderRow(tc.Function.Name, tc.Function.Arguments, false)})
			}
		case "tool":
			// Synthetic results synthesized at load for interrupted calls get
			// an inline row so the user sees what the model sees; diffs from
			// write/edit results re-render so a resumed transcript still shows
			// what changed; other results stay folded under their call row.
			switch {
			case strings.HasPrefix(msg.Content, "Error: tool call interrupted"):
				m.blocks = append(m.blocks, block{kind: blockText, text: errStyle.Render("⚒ "+msg.Name+" ") + dimStyle.Render("— interrupted: session ended before a result was recorded")})
			default:
				if diff, _ := extractDiff(strings.TrimRight(msg.Content, "\n")); diff != "" {
					m.blocks = append(m.blocks, block{kind: blockTool, text: msg.Content})
				}
			}
		}
		for len(m.msgBlock) <= base+i {
			m.msgBlock = append(m.msgBlock, -1)
		}
		m.msgBlock[base+i] = bi
	}
	m.follow = true
	m.refreshVP()
}

// persist writes any unsaved messages to the session store and re-stamps the
// session's bookkeeping (goal, effort) — the effort stamp is what a resume
// restores, so it runs even when no new messages landed.
func (m *model) setTheme(theme string) {
	if theme != "light" && theme != "dark" {
		theme = "auto"
	}
	how := m.applyTheme(theme)
	m.themeHow = how // explicit picks return "" — detection source no longer applies
	if m.uiMode == opencodeMode {
		m.applyUIMode(opencodeMode) // refresh opencode styles (input box fill) for the new scheme
	}
	m.cfg.Theme = theme
	if theme == "auto" {
		m.cfg.Theme = "" // auto persists as "" (omitted = auto-detect)
	}
	if m.cfgExtra == nil {
		m.cfgExtra = map[string]string{}
	}
	if theme == "auto" {
		delete(m.cfgExtra, "theme") // explicit pick, not omission
	} else {
		m.cfgExtra["theme"] = theme
	}
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
	m.refreshVP() // re-render the transcript under the new scheme
	if theme == "auto" {
		m.append(dimStyle.Render(fmt.Sprintf("◐ theme: %s (auto: %s)", CurrentTheme(), how)))
	} else {
		m.append(dimStyle.Render("◐ theme: " + CurrentTheme()))
	}
}

// applyTheme points rendering at a scheme WITHOUT persisting: auto re-detects
// (re-reading the terminal background so switching dark→auto can't stay dark),
// explicit picks override detection directly. Called by setTheme, startup, and
// the config watcher. how (only meaningful for auto) names the detection
// source so a wrong pick is diagnosable in the transcript note.
func (m *model) applyTheme(theme string) (how string) {
	switch theme {
	case "light":
		SetLightTheme(true)
		lipgloss.SetHasDarkBackground(false)
		setSchemeOverride("light")
	case "dark":
		SetLightTheme(false)
		lipgloss.SetHasDarkBackground(true)
		setSchemeOverride("dark")
	default: // auto: don't touch m.cfg.Theme — setTheme owns persistence
		setSchemeOverride("")
		how = detectColorScheme()
	}
	return how
}

// setEffort changes the reasoning effort and stores it both ways: as the new
// global default (every future session starts here) and on the live session
// row (resuming this conversation restores it). "" = off. Callers that only
// reconcile state (model switch / catalog refresh dropping an unsupported
// level) use resetEffort instead so a quiet reconciliation never rewrites
// the user's chosen global default.
type blockKind int

const (
	blockText       blockKind = iota // already-styled line(s): re-wrap on resize
	blockAssistant                   // raw markdown: re-render through glamour
	blockTool                        // raw tool result: collapsed preview, expandable
	blockToolRun                     // a running tool call: verb line, collapses on completion
	blockToolQueued                  // a tool call still streaming from the model; replaced by blockToolRun on start
	blockUser                        // a user turn: opencode mode renders a bordered card, else the "❯ " prefix
	blockOCMeta                      // opencode meta line (▣ attribution): pre-indented, rendered verbatim (no wrap, which would trim the indent)
	blockThought                     // opencode collapsed reasoning: "+ Thought: dur" header (in live), the reasoning text behind expand (in text)
)

// toolPreviewLines is how many lines of a tool result show when collapsed.
const toolPreviewLines = 5

// minRenderWidth is the smallest width blocks render at. A transient
// degenerate WindowSizeMsg (1–4 cols from a tmux/PTY handshake) would
// otherwise collapse blockTool/blockText into a one-char-per-line strip —
// those wrap with no floor, and a cached bad render persists until a width
// *change* forces a reflow. Below this the layout is unreadable either way.
const minRenderWidth = 8

// block is one finalized transcript entry. Text holds raw markdown for
// blockAssistant, raw tool output for blockTool, and styled content
// otherwise.
type block struct {
	kind     blockKind
	text     string
	fileRoot string
	expanded bool // blockTool/blockToolRun: show the full output (click / ctrl+e toggles)
	// blockToolRun: the tool-call id this row tracks and whether it's still
	// running — on completion the row collapses in place to one line.
	toolID      string
	toolRunning bool
	toolFailed  bool
	// toolName/toolArgs are the raw call, kept so the completed row can render
	// the claude-style "Verb(subject)" header (the collapse must not lose the
	// path/command the call was about).
	toolName string
	toolArgs string
	// live is the latest partial-output snapshot for a running bash call,
	// rendered under the verb line; cleared when the tool ends.
	live string
	// y0/y1 are the block's line range in the last rendered content (set by
	// refreshVP); used to map a mouse click to the block under it.
	y0, y1 int
	// cache of the last render: valid while !stale and width matches.
	hover bool // opencode mode: pointer is over this block (user cards highlight)

	rendered string
	lines    int
	width    int
	stale    bool
}

// renderAt returns the block rendered at width, re-rendering only when the
// cache is cold (first render, width change, or text/expand mutation). This
// is what makes appends and resume cheap: unchanged blocks never re-render.
func (b *block) renderAt(width int) string {
	if !b.stale && b.width == width {
		return b.rendered
	}
	b.rendered = b.render(width)
	b.lines = lipgloss.Height(b.rendered)
	b.width, b.stale = width, false
	return b.rendered
}

// render renders the block at width (the full terminal width; assistant
// blocks get their marker + indent here so a resize re-renders everything).
func (b block) render(width int) string {
	switch b.kind {
	case blockUser:
		if ocActive {
			return opencodeUserCard(b.text, width, b.hover)
		}
		return wrap(youStyle.Render(glyphUser)+b.text, width)
	case blockOCMeta:
		return b.text // pre-indented; verbatim so wrap() can't trim the indent
	case blockThought:
		// collapsed: opencode's "+ Thought: {dur}" line; expanded (click/ctrl+e):
		// the reasoning text underneath, muted italic like whip's thinking style
		head := "   " + lipgloss.NewStyle().Foreground(ocWarnCol()).Render("+ Thought: "+b.live)
		if !b.expanded {
			return head
		}
		body := lipgloss.NewStyle().Foreground(ocMutedCol()).Italic(true).Render(strings.TrimSpace(b.text))
		return head + "\n" + wrap(body, width)
	case blockAssistant:
		if ocActive {
			// opencode assistant messages carry no bullet: the body is indented 3.
			w := width - 3
			if w <= 0 {
				w = 80
			}
			return indentLines(renderMarkdownAt(b.text, w, b.fileRoot), 3)
		}
		w := width - 2 // body indents under the "● " marker
		if w <= 0 {
			w = 80 // no terminal size yet: sane default
		}
		body := indentLines(renderMarkdownAt(b.text, w, b.fileRoot), 2)
		return botStyle.Render(glyphAssistant) + strings.TrimPrefix(body, "  ")
	case blockTool:
		// A result carrying a fenced diff (write/edit) renders claude-style:
		// "⎿ Added N lines, removed M lines" over a colored, line-numbered
		// diff — the change IS the collapsed view, not hidden behind expand.
		if diff, rest := extractDiff(strings.TrimRight(b.text, "\n")); diff != "" {
			return renderDiffResult(diff, rest, b.expanded, width)
		}
		lines := strings.Split(strings.TrimRight(b.text, "\n"), "\n")
		if ocActive {
			// opencode tucks results behind the tool row: a muted one-line hint
			// collapsed, the full body only when expanded
			return ocToolResult(lines, b.expanded, strings.HasPrefix(b.text, "Error"), b.hover, width)
		}
		lines[0] = "⎿ " + lines[0] // tie the result to its header row above
		style := dimStyle
		if strings.HasPrefix(b.text, "Error") {
			style = errStyle // failures read at a glance, like the red header
		}
		if b.expanded || len(lines) <= toolPreviewLines {
			return wrap(style.Render("  "+strings.Join(lines, "\n  ")), width)
		}
		preview := lines[:toolPreviewLines]
		out := style.Render("  " + strings.Join(preview, "\n  "))
		hint := fmt.Sprintf("\n  … +%d lines (ctrl+e or click to expand)", len(lines)-toolPreviewLines)
		return wrap(out+dimStyle.Render(hint), width)
	case blockToolRun:
		// While running, the verb line shows in full with the live output
		// tail under it. On completion the same block collapses in place to
		// its header row ("● Update(path)" — styles baked in by toolEndMsg).
		if b.toolRunning || b.expanded {
			if b.live != "" && b.toolRunning {
				return wrap(b.text, width) + "\n" + wrap(dimStyle.Render("  "+b.live), width)
			}
			return wrap(b.text, width)
		}
		return ansi.Truncate(b.text, width, "…")
	default:
		return wrap(b.text, width)
	}
}

// expand toggles a tool block and returns whether it changed.
func (b *block) toggle() bool {
	if b.kind != blockTool && b.kind != blockToolRun && b.kind != blockThought {
		return false
	}
	b.expanded = !b.expanded
	b.stale = true
	return true
}

// append adds finalized blocks to the transcript, separating blocks with a
// blank line so consecutive messages and tool calls breathe.
func (m *model) append(blocks ...string) {
	for _, s := range blocks {
		m.appendRaw(blockText, s)
	}
}

// appendAssistant appends raw assistant markdown; rendering happens in
// refreshVP at the current width.
func (m *model) appendAssistantBlock(s string) {
	m.appendRaw(blockAssistant, s)
}

func (m *model) appendRaw(kind blockKind, text string) {
	m.blocks = append(m.blocks, block{kind: kind, text: text, fileRoot: m.completionRoot()})
	m.follow = true
	m.refreshVP()
}

// refreshVP rebuilds the viewport content, bottom-anchored: short transcripts
// are padded from the top so messages grow upward from the input. Block
// renders are cached per width (renderAt), so a rebuild is an O(transcript)
// join of cached strings; the expensive glamour markdown render only happens
// for blocks that are new, mutated, or hit by a width change. This is what
// keeps resume and streaming appends near-linear.
func (m *model) refreshVP() {
	if m.width == 0 {
		return // tea hasn't started (resume path): the first WindowSizeMsg renders once at the real width
	}
	// Clamp to a sane minimum so a degenerate WindowSizeMsg (a 1–4 col width,
	// which tmux/PTY handshakes can emit transiently) never collapses blocks
	// into a one-char-per-line strip: blockTool/blockText wrap with no floor,
	// so width 1 renders one character per row. Below minRenderWidth the layout
	// is unreadable either way — render at the floor instead.
	width := max(m.width, minRenderWidth)
	var b strings.Builder
	if n := len(m.blocks); n > 0 {
		b.Grow(n*24 + 1<<20) // one big allocation up front
	}
	line := 0
	for i := range m.blocks {
		if i > 0 {
			// "\n\n" ends the previous block's last line and leaves ONE blank
			// row between blocks, so the row counter advances by 1 — adding 2
			// here would drift every later block's y0/y1 one row low per
			// separator (the click-mapping math only worked because it shared
			// the drift).
			b.WriteString("\n\n")
			line++
		}
		r := m.blocks[i].renderAt(width)
		m.blocks[i].y0 = line
		m.blocks[i].y1 = line + m.blocks[i].lines - 1
		b.WriteString(r)
		line = m.blocks[i].y1 + 1
	}
	content := b.String()
	if pad := m.contentPad(); pad > 0 {
		content = strings.Repeat("\n", pad) + content
	}
	m.vp.SetContent(content)
	if m.follow {
		m.vp.GotoBottom()
	}
}

// contentPad is the number of blank lines refreshVP prepends when the
// transcript is shorter than the viewport (click-row mapping accounts for it).
func (m *model) contentPad() int {
	if len(m.blocks) == 0 {
		return m.vp.Height
	}
	h := m.blocks[len(m.blocks)-1].y1 + 1 // content height from the last block
	return max(m.vp.Height-h, 0)
}

// viewportView renders the transcript viewport and drops the dead pad rows.
// The viewport content is `contentPad` blank lines followed by the blocks;
// SetContent bottom-anchors it, so at the bottom the view's row 0 is the
// first pad row. We paint the selection highlight in content space FIRST
// (content row r = view row r + contentPad - YOffset), then drop pad rows by
// COUNT — never by "looks blank", because a highlighted blank row reads as
// non-blank and would change the drop count mid-drag (that's what made the
// transcript jump). Dropping exactly the pad keeps screen rows stable whether
// or not a selection is active.
func (m *model) viewportView() string {
	s := sanitizeView(m.vp.View())
	if m.sel != nil {
		s = m.highlightSelection(s) // content space, pre-trim
	}
	if m.uiMode == opencodeMode {
		m.vpLead = 0
		if len(m.blocks) == 0 { // empty transcript: opencode's centered-logo home screen
			return opencodeHome(m.vp.Width, m.vp.Height)
		}
		// Full-height viewport: keep the pad so the transcript is bottom-anchored
		// (blanks above, content near the prompt) and the prompt/status sit at the
		// bottom of the screen, like opencode's session layout.
		return s
	}
	lines := strings.Split(s, "\n")
	// Drop leading pad rows by count: the content starts after the pad, but
	// the view is scrolled (YOffset) and bottom-anchored, so the number of pad
	// rows actually visible at the top is pad - YOffset (clamped).
	drop := max(min(m.contentPad()-m.vp.YOffset, len(lines)), 0)
	// Only drop rows that are actually blank — if the selection highlight
	// painted a pad row, that row is content now, stop before it.
	first := 0
	for first < drop && strings.TrimSpace(ansi.Strip(lines[first])) == "" {
		first++
	}
	m.vpLead = first // selection maps screen rows through the dropped pad
	lines = lines[first:]
	// Drop trailing dead rows (the viewport pads to its full height).
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(ansi.Strip(lines[last])) == "" {
		last--
	}
	return strings.Join(lines[:last+1], "\n")
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, waitClientUpdate(m.client)}
	if inTmuxEnv() {
		// live theme tracking: tmux knows the outer terminal's light/dark
		// (#{client_theme}, via the 996/2031 protocol) — poll it so an OS
		// appearance flip mid-session is picked up without a restart
		cmds = append(cmds, themePollTick())
	}
	return tea.Batch(cmds...)
}

// themePollMsg fires the periodic client-theme poll; themeSyncMsg carries its
// result back from the tmux subprocess.
type (
	themePollMsg struct{}
	themeSyncMsg struct {
		light, ok bool
	}
)

// themePollTick schedules the next client-theme poll.
func themePollTick() tea.Cmd {
	return tea.Tick(10*time.Second, themePollFire)
}

func themePollFire(time.Time) tea.Msg { return themePollMsg{} }

// pollClientTheme asks tmux for the outer terminal's current theme.
func pollClientTheme() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{client_theme}").Output()
	s := strings.TrimSpace(string(out))
	return themeSyncMsg{light: s == "light", ok: err == nil && (s == "light" || s == "dark")}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// fmtTok renders a token count compactly: 12.3k, 1.2M, 134 raw under 1000.
func fmtTok(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return strconv.Itoa(n)
	}
}

// fmtUsage renders cumulative or per-request usage as the status line's
// "in(cached)/out tok" shape; the cached parens appear only when reported.
func fmtUsage(u llm.Usage) string {
	if c := u.Cached(); c > 0 {
		return fmt.Sprintf("%s(%s)/%s tok", fmtTok(u.PromptTokens), fmtTok(c), fmtTok(u.CompletionTokens))
	}
	return fmt.Sprintf("%s/%s tok", fmtTok(u.PromptTokens), fmtTok(u.CompletionTokens))
}

// fmtCost renders a USD spend compactly: 4 decimals under a dollar (where the
// cents would hide the signal), 2 at or above.
func fmtCost(d float64) string {
	if d >= 1 {
		return fmt.Sprintf("$%.2f", d)
	}
	return fmt.Sprintf("$%.4f", d)
}

func cwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "?"
}

// detectColorScheme figures out whether the terminal background is light and
// calls SetLightTheme so markdown renders with a matching (high-contrast)
// glamour style. Priority:
//  1. WHIP_THEME=light|dark (explicit env override)
//  2. COLORFGBG (set by many terminals; last field is the bg color index)
//  3. an OSC 11 background query on /dev/tty with a short timeout
//  4. default: dark (the safe assumption for coding terminals)
//
// The config theme is NOT consulted here — applyTheme handles explicit picks
// before auto ever reaches detection. detectColorScheme returns a short
// human-readable note naming the source of the decision (shown by /theme auto
// so a wrong pick is diagnosable).
//
// tuiRunning and bgCache are declared above Run (see the note there).
// r/g/b hold the OSC 11 reply's actual color when hasRGB — the opencode mode
// derives its panel shades relative to the REAL background from these.
type bgResult struct {
	light, valid bool
	r, g, b      int
	hasRGB       bool
}

// inTmuxEnv reports whether whip runs inside tmux/screen, where the terminal
// can't be queried directly.
func inTmuxEnv() bool {
	return os.Getenv("TMUX") != "" ||
		strings.HasPrefix(os.Getenv("TERM"), "screen") ||
		strings.HasPrefix(os.Getenv("TERM"), "tmux")
}

func detectColorScheme() string {
	setScheme := func(light bool) {
		SetLightTheme(light)                  // glamour markdown style
		lipgloss.SetHasDarkBackground(!light) // AdaptiveColor picks
	}
	switch strings.ToLower(os.Getenv("WHIP_THEME")) {
	case "light":
		setScheme(true)
		return "WHIP_THEME"
	case "dark":
		setScheme(false)
		return "WHIP_THEME"
	}
	// While bubbletea runs, the terminal is OFF LIMITS: the raw-mode OSC 11
	// query flips the shared tty to VMIN=0/VTIME, and if bubbletea's input
	// reader issues a read in that window it gets a 0-byte result = io.EOF —
	// the reader exits SILENTLY and the session never sees input again (the
	// frozen-whip bug: /theme auto or a config-watcher sync re-ran detection
	// mid-session). Reuse the startup query's answer instead; env fallbacks
	// below are read-only and stay available.
	if tuiRunning {
		if bgCache.valid {
			setScheme(bgCache.light)
			return "terminal query (cached from startup)"
		}
		light, ok, how := fallbackScheme(inTmuxEnv(), os.Getenv("COLORFGBG"))
		if ok {
			setScheme(light)
		} else {
			SetUnknownTheme()
		}
		return how
	}
	// Query the terminal directly whenever we have one — the OSC 11 reply is
	// the terminal's REAL background, so it outranks COLORFGBG (which can be
	// stale: inherited from an outer shell/terminal with a different bg).
	// termenv's query refuses to run inside tmux/screen (its termStatusReport
	// short-circuits on TERM=screen*/tmux*) and silently assumes a dark
	// background — wrong for a tmux user on a light terminal. queryTerminal-
	// Background reaches the REAL terminal via DCS passthrough inside tmux, and
	// via a plain OSC 11 query otherwise, so use it first and keep termenv only
	// as a fallback for terminals it can query directly.
	inTmux := inTmuxEnv()
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		if r := queryTerminalBackground(tty, inTmux); r.valid {
			_ = tty.Close()
			setScheme(r.light)
			bgCache = r // keeps the reply's RGB: opencode mode derives panel shades from it
			if inTmux {
				return "terminal query (inside tmux)"
			}
			return "terminal query"
		}
		// fallback: termenv's own query — but NEVER inside tmux/screen, where
		// termenv can't reach the real terminal (its termStatusReport
		// short-circuits on TERM=screen*/tmux*) and silently ASSUMES DARK.
		// That guess rendered the dark palette on light terminals (washed-out
		// gray text on white) whenever the tmux query got no reply.
		if !inTmux {
			type result struct{ light bool }
			done := make(chan result, 1)
			go func() {
				o := termenv.NewOutput(tty)
				done <- result{light: !o.HasDarkBackground()}
			}()
			select {
			case r := <-done:
				_ = tty.Close()
				setScheme(r.light)
				bgCache = bgResult{light: r.light, valid: true}
				return "terminal query"
			case <-time.After(300 * time.Millisecond):
			}
		}
		_ = tty.Close()
	}
	light, ok, how := fallbackScheme(inTmux, os.Getenv("COLORFGBG"))
	if ok {
		setScheme(light)
	} else {
		// No reliable signal: don't force a dark guess. Neutral default keeps
		// text at the terminal's own colors instead of inverting contrast.
		SetUnknownTheme()
	}
	return how
}

// fallbackScheme decides the color scheme when no terminal query succeeded:
// COLORFGBG (set by many terminals; last field is the bg color index) when
// parseable, otherwise not-ok — the caller falls back to the neutral theme.
// Pure so the no-query-reply behavior is unit-testable: inside tmux the ONLY
// acceptable outcomes are COLORFGBG or neutral, never a dark assumption.
func fallbackScheme(inTmux bool, colorfgbg string) (light, ok bool, how string) {
	if i := strings.LastIndex(colorfgbg, ";"); i >= 0 {
		var bg int
		if _, err := fmt.Sscanf(colorfgbg[i+1:], "%d", &bg); err == nil {
			// standard palette: 0-6 dark, 7+ light (15 = white)
			return bg == 7 || bg >= 8, true, "COLORFGBG (query failed)"
		}
	}
	if inTmux {
		return false, false, "undetermined (no OSC 11 reply through tmux — outer terminal doesn't answer it (e.g. mosh), or tmux <3.4 without `allow-passthrough on`) — neutral default"
	}
	return false, false, "undetermined (query timed out) — neutral default"
}

// inputContentHeight returns the number of lines the input needs to show its
// whole value, wrapping each logical line the way the textarea does (at the
// content width, which excludes the "┃ " prompt). We must compute this from
// the value, not View(): the textarea clamps View() to its current height, so
// measuring it can never grow the box.
func (m *model) inputContentHeight() int {
	contentWidth := max(
		// minus the "┃ " prompt
		m.input.Width()-2, 1,
	)
	h := 0
	for line := range strings.SplitSeq(m.input.Value(), "\n") {
		h += max(1, (lipgloss.Width(line)+contentWidth-1)/contentWidth)
	}
	return h
}

// growInput resizes the input box to fit its content (capped at MaxHeight).
// When the box grows, the textarea's internal viewport keeps the scroll offset
// it computed for the smaller height — repositionView only ever scrolls down
// to follow the cursor, never back up — so the top lines would be clipped out
// of view. The textarea doesn't expose its viewport, so on growth we rebuild
// it at the new height (a fresh viewport starts at the top), preserving the
// content and cursor-at-end.
func (m *model) growInput() {
	if m.width <= 0 {
		return
	}
	h := max(1, min(m.inputContentHeight(), m.input.MaxHeight))
	if h == m.input.Height() {
		return
	}
	if h < m.input.Height() {
		m.input.SetHeight(h) // shrinking never clips
		return
	}
	val := m.input.Value()
	ti := newInput()
	// carry the CURRENT mode's chrome over: newInput bakes whip's defaults
	// ("┃ " prompt, whip placeholder, plain styles), which in opencode mode
	// would draw a double bar, revert the element-bg fills, and — since the
	// prompt eats 2 content cells — widen the box row past the frame
	ti.Prompt = m.input.Prompt
	ti.Placeholder = m.input.Placeholder
	ti.FocusedStyle = m.input.FocusedStyle
	ti.BlurredStyle = m.input.BlurredStyle
	ti.SetWidth(m.input.Width() + lipgloss.Width(ti.Prompt)) // Width() is content width; SetWidth takes total
	ti.SetHeight(h)
	ti.SetValue(val)
	ti.CursorEnd()
	m.input = ti
	m.input.Focus() // re-snapshot the style pointer at the COPIED struct (see applyUIMode)
}

// layout gives the viewport whatever height the chrome doesn't need,
// growing the input box with its content so the whole prompt stays visible.
func (m *model) layout() {
	m.growInput()
	// Always-on rows around the viewport: header, tips, blank below tips,
	// blank above the input, the input itself, blank above the status line,
	// and the status line. This count MUST match viewBody exactly: if chrome
	// undercounts, a full transcript renders MORE rows than the terminal has,
	// every frame scrolls the top rows off-screen, and all mouse math lands
	// that many rows above the pointer (the off-by-two drag-select bug: the
	// status line + its blank were never budgeted).
	chrome := 6 + m.input.Height()
	if m.uiMode == opencodeMode {
		// drops the header row and the tips line + its blank (-3); the prompt
		// panel adds paddingTop, a blank, the model/mode row, and the ▀ tail (+4).
		chrome++
	}
	if m.iactive != nil {
		// input box is hidden while a command has the terminal; drop its height
		// and the leading blank line View inserts before it.
		chrome -= m.input.Height()
	}
	if m.busy && m.uiMode != opencodeMode {
		chrome += 2 // blank line above the spinner + the spinner line itself (opencode mode: status bar spinner instead)
	}
	if len(m.plan) > 0 {
		chrome += len(m.plan) + 1
	}
	if details := m.agentDetails(); details != "" {
		chrome += lipgloss.Height(details) + 1
	}
	if m.current != "" {
		chrome += lipgloss.Height(m.currentView()) + 1 // + its blank separator
	}
	if m.curThink != "" {
		chrome += lipgloss.Height(m.thinkView()) + 1
	}
	if m.uiMode == opencodeMode && m.busy && !m.thinkStart.IsZero() {
		chrome += 2 // the live "+ Thinking…" line + its blank separator (must match viewBody)
	}
	if m.iactive != nil {
		chrome += lipgloss.Height(m.interactiveView()) + 1
	}
	if m.permDialog != nil {
		chrome += lipgloss.Height(m.permView()) + 1 // viewBody emits "\n"+permView(); unbudgeted it clips the alt-screen frame and shifts mouse rows
	}
	if m.menu != nil && m.uiMode != opencodeMode {
		// measure the actual render (descriptions can word-wrap). opencode mode
		// takes no rows at all: the menu overlays the frame from View()
		chrome += lipgloss.Height(m.menuView()) + 1
	}
	if m.rew != nil {
		chrome += lipgloss.Height(m.rewindView()) + 1 // + the extra blank below
	}
	if m.quit1 {
		chrome++ // "press ctrl+c again to quit"
	}
	if m.escClr || (m.esc1 && m.rew == nil && m.namePrompt == nil) {
		chrome++ // esc hint line (same conditions as viewBody)
	}
	m.dockRows = 0
	if dock := m.agentsDock(); dock != "" { // lipgloss.Height("") is 1, not 0
		m.dockRows = lipgloss.Height(dock)
		// clicking computes agent rows from the strip's top; a focused dock's
		// hint row isn't an agent — skip it
		m.dockSkip = 0
		if m.agentsFocus {
			m.dockSkip++
		}
		chrome += m.dockRows // the blank above the input is already in the base
	}
	// Floor the viewport width too: a degenerate m.width (1–4 cols) would set
	// the viewport to 1 col and re-slice the transcript into a one-char strip,
	// regardless of the render floor in refreshVP.
	w, h := max(m.width, minRenderWidth), max(m.height-chrome, 1)
	if m.vp.Width != w || m.vp.Height != h {
		m.vp.Width, m.vp.Height = w, h
		m.refreshVP()
	}
}

// dockTop returns the screen row of the first agent row in the dock: the dock
// renders below the input as the last dockRows rows above the blank + status
// line, but dockSkip non-agent rows (the focused hint) sit on top of the agent
// rows. layout() keeps both in sync with what View renders. The row is an
// absolute screen row: counted up from the view's bottom (viewTop+viewH),
// which equals the terminal bottom while the view is bottom-anchored but
// stays correct when a shrunk view floats above it.
func (m *model) dockTop() int {
	bottom := m.height
	if m.viewH > 0 {
		bottom = m.viewTop + m.viewH
	}
	return bottom - 2 - m.dockRows + m.dockSkip
}

var shiftEnterRe = regexp.MustCompile(
	`'\[', '1', '3', ';', '2', 'u'` +
		`|'\[', '2', '7', ';', '2', ';', '1', '3', '~'` +
		`|'\[', 'five', 'seven', 'four', 'four', 'one', 'u'`,
)

func isShiftEnterSeq(msg tea.KeyMsg) bool {
	s := msg.String()
	return strings.HasPrefix(s, "unknown csi sequence:") && shiftEnterRe.MatchString(s)
}

// nowFn returns the current time, honoring the test seam when set.
func (m *model) nowFn() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// busyStats renders the busy line's live counters: elapsed time since the
// turn started, session tokens so far, and the share of the advertised
// context window. Returns "" when idle (turnStart zero).
func (m *model) busyStats() string {
	if m.turnStart.IsZero() {
		return ""
	}
	d := max(m.nowFn().Sub(m.turnStart), 0)
	elapsed := d.Round(time.Second)
	stats := fmt.Sprintf(" %d:%02d", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	if u := m.displayUsage(); u.PromptTokens > 0 || u.CompletionTokens > 0 {
		stats += fmt.Sprintf(" · %s tok", fmtTok(u.PromptTokens+u.CompletionTokens))
	}
	if limit := m.displayContextLimit(); limit > 0 {
		stats += fmt.Sprintf(" · %d%%", estimateTokens(m.displayMessages())*100/limit)
	}
	return stats
}

// histPrev/histNext recall submitted inputs with the arrow keys.
func (m *model) histPrev() {
	if len(m.hist) == 0 || m.histIdx == 0 {
		return
	}
	if m.histIdx == len(m.hist) {
		m.draft = m.input.Value()
	}
	m.histIdx--
	m.input.SetValue(m.hist[m.histIdx])
}

func (m *model) histNext() {
	if m.histIdx >= len(m.hist) {
		return
	}
	m.histIdx++
	if m.histIdx == len(m.hist) {
		m.input.SetValue(m.draft)
	} else {
		m.input.SetValue(m.hist[m.histIdx])
	}
}

// cursorOnFirstLine reports whether the textarea's cursor sits on the first
// (visual) row. A single logical line that soft-wraps to several rows counts
// as several, so ↑ only rolls over to history from the topmost one.
func (m *model) cursorOnFirstLine() bool {
	if m.input.Line() != 0 {
		return false
	}
	return m.input.LineInfo().RowOffset == 0
}

// cursorOnLastLine reports whether the textarea's cursor sits on the last
// (visual) row, mirroring cursorOnFirstLine for the ↓ edge.
func (m *model) cursorOnLastLine() bool {
	if m.input.Line() != m.input.LineCount()-1 {
		return false
	}
	li := m.input.LineInfo()
	return li.RowOffset >= li.Height-1
}

// contextLimitFor returns the advertised context window for a model id on a
// provider, from the cached /models catalog (0 when unknown).
func (m *model) contextLimitFor(provName, apiID string) int {
	if cat, ok := m.catalogs[provName]; ok {
		return cat.ContextLength(apiID)
	}
	return 0
}

// sessionCost returns the session's cumulative USD spend at the current
// model's advertised rates; ok is false when the provider's catalog has no
// pricing for the model, in which case the status line hides the segment.
func (m *model) sessionCost() (float64, bool) {
	cat, ok := m.catalogs[m.provName]
	if !ok {
		return 0, false
	}
	in, out, cacheRead, ok := cat.Pricing(m.displayModelID())
	if !ok {
		return 0, false
	}
	return llm.SessionCost(m.displayUsage(), in, out, cacheRead), true
}

// compactThresholdFor converts the config's compactPct preference into the
// agent's threshold fraction. Out-of-range values clamp to [10, 90]; 0 (unset)
// means the built-in default.
func (m *model) openMenu() {
	head, cands := completionsAtRoot(m.completionRoot(), m.input.Value(), m.modelCands(), m.providerCands(), m.skillCands(), effortCandsFor(m.effortsFor()))
	if len(cands) == 0 {
		return
	}
	m.menu = &menu{head: head, cands: cands}
	m.menuCycle(0)
}

// refreshMenu keeps a live dropdown open while typing a slash command, an
// @file mention, or a $skill, re-filtering on every keystroke; otherwise
// closes it. A frozen menu (tab cycling) keeps its candidate snapshot — the
// cycle range only changes when the completed text itself is edited.
func (m *model) refreshMenu() {
	if m.menu != nil && m.menu.cyc && m.menu.frozen != nil && m.menu.idx < len(m.menu.frozen) &&
		m.input.Value() == m.menu.head+m.menu.frozen[m.menu.idx].Text {
		return // previewing a frozen candidate; nothing to re-filter
	}
	val := m.input.Value()
	token := val[strings.LastIndexAny(val, " \n")+1:]
	if strings.HasPrefix(val, "/") || strings.HasPrefix(token, "@") || strings.HasPrefix(token, "$") {
		head, cands := completionsAtRoot(m.completionRoot(), val, m.modelCands(), m.providerCands(), m.skillCands(), effortCandsFor(m.effortsFor()))
		if len(cands) > 0 {
			idx := 0
			if m.menu != nil && m.menu.idx < len(cands) && m.menu.frozen == nil {
				idx = m.menu.idx
			}
			m.menu = &menu{head: head, cands: cands, idx: idx}
			return
		}
	}
	m.menu = nil
}

// previewCand inserts the highlighted candidate as a tab-cycle preview (no
// trailing space). The frozen menu survives the input edit via refreshMenu.
func (m *model) previewCand() {
	m.input.SetValue(m.menu.head + m.menu.cands[m.menu.idx].Text)
	m.refreshMenu()
}

// acceptPreview commits a tab-cycle preview on enter: appends the trailing
// space (or stays open inside a directory) exactly like accept.
func (m *model) acceptPreview() {
	m.menu.cyc, m.menu.frozen = false, nil // committing: live filtering again
	v := m.input.Value()
	if !strings.HasSuffix(v, "/") {
		m.input.SetValue(v + " ")
	}
	m.refreshMenu()
}

// menuCycle moves the tab-cycle selection by delta, previewing the new
// candidate from the pre-cycle input. The cycle set is frozen on the first
// tab so cycling covers every candidate for the token's common prefix
// ("/m" tabs through /model and /mouse even though /model doesn't filter
// to /mouse).
func (m *model) menuCycle(delta int) {
	mu := m.menu
	if mu.frozen == nil {
		mu.cyc, mu.frozen = true, mu.cands
		mu.base = mu.head + mu.frozen[mu.idx].Text // esc reverts to here
	}
	if mu.cycled {
		mu.idx = (mu.idx + delta + len(mu.cands)) % len(mu.cands)
	} else {
		mu.cycled = true // first tab previews the current best match
	}
	m.previewCand()
}

// accept applies the selected candidate. Returns false if the input already
// equals it (nothing to complete).
func (m *model) accept() bool {
	c := m.menu.cands[m.menu.idx]
	v := m.menu.head + c.Text
	if !strings.HasSuffix(c.Text, "/") { // directories stay open for deeper completion
		v += " "
	}
	if strings.TrimRight(m.input.Value(), " ") == strings.TrimRight(v, " ") {
		m.menu = nil
		return false
	}
	m.input.SetValue(v)
	m.menu = nil
	m.refreshMenu()
	return true
}

func (m *model) modelCands() []cand {
	out := make([]cand, 0, len(m.cfg.Models))
	for name, mdl := range m.cfg.Models {
		out = append(out, cand{name, "via " + strings.Join(mdl.Providers, ", ")})
	}
	// catalog-advertised models are usable without a config entry (catalog
	// fallback in Resolve); offer them in completion too
	for _, it := range buildModelItems(m.cfg) {
		if it.fromCatalog {
			out = append(out, cand{it.model, "via " + it.provider + " (catalog)"})
		}
	}
	return out
}

func (m *model) providerCands() []cand {
	out := make([]cand, 0, len(m.cfg.Providers))
	for name, p := range m.cfg.Providers {
		out = append(out, cand{name, p.BaseURL})
	}
	return out
}

// skillCands rescans skill dirs so newly added skills appear immediately.
// ponytail: full rescan per keystroke; cache with a TTL if a huge skill tree drags
func (m *model) skillCands() []cand {
	sk := skills.Scan(skills.DirsFor(m.completionRoot())...)
	out := make([]cand, 0, len(sk))
	for _, s := range sk {
		d := s.Description
		if len(d) > 80 {
			d = d[:80] + "…"
		}
		out = append(out, cand{"$" + s.Name, d})
	}
	return out
}

func (m *model) completionRoot() string {
	if value, ok := m.runtimeAgent(m.agentOpen); ok && value.CWD != "" {
		return value.CWD
	}
	if m.clientView.workingDir != "" {
		return m.clientView.workingDir
	}
	return cwd()
}

func (m *model) appendAssistant(s string) {
	if m.inMsg && len(m.blocks) > 0 && m.blocks[len(m.blocks)-1].kind == blockAssistant {
		m.blocks[len(m.blocks)-1].text += "\n\n" + s // same message: merge
		m.blocks[len(m.blocks)-1].stale = true
		m.follow = true
		m.refreshVP()
		return
	}
	m.appendAssistantBlock(s)
	m.inMsg = true
}

// indentLines shifts rendered markdown right by n columns so the body sits
// under the transcript's "● " marker. Glamour indents every block from its
// 2-cell document margin; we subtract that margin and add n, preserving
// *relative* indentation (hanging list text, nested bullets, code blocks).
// Whitespace-only lines become truly empty so no stray dim cells render.
func indentLines(s string, n int) string {
	const docMargin = 2 // glamour styles.DarkStyleConfig Document.Margin
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(ansi.Strip(l)) == "" {
			lines[i] = ""
			continue
		}
		lead := len(l) - len(strings.TrimLeft(l, " "))
		shift := max(n+lead-docMargin, 0)
		lines[i] = strings.Repeat(" ", shift) + strings.TrimLeft(l, " ")
	}
	return strings.Join(lines, "\n")
}

// appendThink writes a reasoning line into the transcript, prefixing the
// first line of each thinking segment.
func (m *model) appendThink(s string) {
	if !m.inThink {
		s = "◌ " + s
		m.inThink = true
	}
	m.append(thinkingStyle.Render(s))
}

// flushThink moves any in-flight partial reasoning line into the transcript
// and ends the current thinking segment.
// toggleThinking flips reasoning-token display (ctrl+o / palette) and persists
// the choice to the global config, like /mouse does.
func (m *model) toggleThinking() {
	m.setThinking(!m.showThinking)
	m.append(dimStyle.Render("◌ thinking tokens: " + onOff(m.showThinking)))
}

// setThinking applies the state without the transcript note (palette ←/→
// steppers call this); it still persists.
func (m *model) setThinking(on bool) {
	m.showThinking = on
	if !on {
		m.flushThink() // drop any in-flight reasoning display
	}
	b := on
	m.cfg.Thinking = &b
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
}

func (m *model) flushThink() {
	if m.uiMode == opencodeMode {
		if !m.thinkStart.IsZero() { // collapse the reasoning segment to one line (expandable to the text)
			m.blocks = append(m.blocks, block{kind: blockThought, text: m.ocThink, live: fmtShortDur(m.nowFn().Sub(m.thinkStart))})
			m.follow = true
			m.refreshVP()
			m.thinkStart = time.Time{}
			m.ocThink = ""
		}
		m.curThink = ""
		m.inThink = false
		return
	}
	cur := strings.TrimRight(m.curThink, " \n")
	m.curThink = ""
	if cur != "" {
		m.appendThink(cur)
	}
	m.inThink = false
}

// thinkView renders the in-flight reasoning line.
func (m *model) thinkView() string {
	s := m.curThink
	if !m.inThink {
		s = "◌ " + s
	}
	return thinkingStyle.Render(wrap(s, m.width))
}

// flushCurrent moves any in-flight partial line into the transcript and ends
// the current assistant segment.
func (m *model) flushCurrent() {
	cur := strings.TrimRight(m.current, " \n")
	m.current = ""
	if cur != "" {
		m.appendAssistant(cur)
	}
	m.inMsg = false
}

// submit sends a message the human typed; it counts for input-history recall.
// drainQueueHead pops the oldest queued message and submits it as the next
// turn — the exact submission path of a typed message (system-prompt rebuild,
// history, transcript echo). Used by turnDoneMsg's queue drain and by the
// idle empty-enter recovery for a stranded queue. Callers handle `!` shell
// escapes before calling (they execute locally, not as a turn).
func (m *model) currentView() string {
	s := m.current
	if !m.inMsg {
		s = botStyle.Render(glyphAssistant) + s
	}
	return wrap(s, m.width) // streamed mid-flight: plain text; markdown renders on flush
}

// View renders the frame and tracks WHERE it sits on the screen. Mouse events
// arrive in absolute screen coordinates, so every click/drag mapping needs the
// view's top row. The inline view starts bottom-anchored (Run moves the cursor
// to the last row before the first paint); bubbletea's renderer scrolls the
// top UP when the view grows past the bottom and keeps it FIXED when the view
// shrinks — so the top row only ever decreases between resizes:
// viewTop = min(viewTop, height - viewH). A resize resets the sentinel
// (WindowSizeMsg handler) and the next render re-anchors to the bottom.
func (m *model) View() string {
	m.syncInputPlaceholder()
	v := m.viewBody()
	if m.uiMode == opencodeMode {
		// opencode's main-column left margin only — the main area stays on the
		// terminal's native background so whip keeps light/dark/auto (no forced
		// backdrop; only the panels below carry a subtle contrast shade).
		v = lipgloss.NewStyle().PaddingLeft(opencodeLeftMargin).Render(v)
	}
	if m.sidebarVisible() {
		gap := strings.Repeat(" ", opencodeRightGap) // breathing room between the panels
		if m.replPanel {
			// the REPL sits on the native background like the chat, so a hairline
			// tells the two columns apart
			rule := " " + lipgloss.NewStyle().Foreground(ocMutedCol()).Render("│")
			gap = strings.TrimSuffix(strings.Repeat(rule+"\n", lipgloss.Height(v)), "\n")
		}
		v = lipgloss.JoinHorizontal(lipgloss.Top, v, gap, m.sidebarView(lipgloss.Height(v)))
	}
	if m.uiMode == opencodeMode {
		switch { // floating dialogs over the dimmed session, opencode-style
		case m.palette != nil:
			v = m.ocOverlay(v) // Commands
		case m.msgActions != nil:
			v = m.ocOverlayRows(v, m.ocMsgActionRows())
		case m.mpicker != nil:
			v = m.ocOverlayRows(v, m.ocModelDialogRows())
		case m.picker != nil:
			v = m.ocOverlayRows(v, m.ocSessionDialogRows())
		case m.menu != nil:
			v = m.ocMenuOverlay(v) // completion popup floats above the input, no reflow
		}
		if m.toast != "" {
			v = m.ocSpliceToast(v) // top-right toast, over everything
		}
	}
	if m.height > 0 {
		m.viewH = lipgloss.Height(v)
		if m.uiMode == opencodeMode {
			m.viewTop = 0 // altscreen: the view is drawn from row 0, so mouse Y maps directly
		} else {
			m.viewTop = max(min(m.viewTop, m.height-m.viewH), 0)
		}
	}
	// Record the input box's absolute screen rows for drag-select. The input is
	// hidden during interactive bash (iactive), so there's nothing to select.
	if m.iactive != nil || m.height == 0 {
		m.inputTop = -1
		m.inputLines = nil
	} else {
		m.inputTop = m.viewTop + m.inputBodyOff
		if m.uiMode == opencodeMode {
			m.inputTop++ // the opencode prompt box opens with a padding row above the input
		}
		iv := m.input.View()
		if m.namePrompt != nil && m.namePrompt.mask {
			iv = m.namePrompt.label + " ┃ " + m.namePrompt.maskedValue(m.input.Value())
		}
		raw := strings.Split(iv, "\n")
		m.inputLines = make([]string, len(raw))
		for i, ln := range raw {
			m.inputLines[i] = strings.TrimRight(ansi.Strip(ln), " \t")
		}
	}
	return v
}

func (m *model) viewBody() string {
	var b strings.Builder
	workingDir := m.clientView.workingDir
	if workingDir == "" {
		workingDir = cwd()
	}
	left := fmt.Sprintf(" whip · %s @ %s · %s", m.modelName, m.provName, workingDir)
	if m.agentOpen != "" {
		if value, ok := m.runtimeAgent(m.agentOpen); ok {
			name := value.Name
			if name == "" {
				name = value.ID
			}
			left = fmt.Sprintf(" whip · root / %s · %s @ %s · %s", name, value.Model, value.Provider, value.CWD)
		}
	}
	if m.clientState != ClientLive {
		left += " · " + m.clientState.String()
	}
	if m.goal != "" {
		left += " · ◎ " + truncLine(m.goal, 40)
	}
	if !m.follow {
		left += fmt.Sprintf(" · ↑ %d%%", int(m.vp.ScrollPercent()*100))
	}
	// session token usage, provider-reported: in (cached of it) / out, then
	// the share of the advertised context window the conversation occupies
	u := m.displayUsage()
	if u.PromptTokens > 0 || u.CompletionTokens > 0 {
		left += fmt.Sprintf(" · ⣿ %s in", fmtTok(u.PromptTokens))
		if c := u.Cached(); c > 0 {
			left += fmt.Sprintf(" (%s cached)", fmtTok(c))
		}
		left += fmt.Sprintf(" · %s out", fmtTok(u.CompletionTokens))
	}
	if limit := m.displayContextLimit(); limit > 0 {
		left += fmt.Sprintf(" · %d%% ctx", estimateTokens(m.displayMessages())*100/limit)
	}
	if count := m.dockCount(); count > 0 {
		left += fmt.Sprintf(" · ⚙ %d agents", count)
	}
	// right-aligned clickable effort control; ◌ marks thinking display
	right := "⚡ " + effortLabel(m.displayEffort()) + " "
	if m.showThinking {
		right = "◌ on  " + right
	}
	m.effortX = max(m.width-len(right)-1, 0) // ⚡ renders 2 cells wide
	left = truncLine(left, max(m.width-len(right)-2, 0))
	pad := max(m.width-len(left)-len(right)-1, 1)
	if m.uiMode != opencodeMode { // opencode has no top header bar
		b.WriteString(dimStyle.Render(left+strings.Repeat(" ", pad)) + toolStyle.Render(right) + "\n")
	}
	if m.palette != nil && m.uiMode != opencodeMode {
		// opencode mode renders the session as usual and View() overlays the
		// Commands dialog on top of the dimmed frame (ocOverlay)
		b.WriteString(m.paletteView())
		return b.String()
	}
	if m.picker != nil && m.uiMode != opencodeMode { // opencode mode: floating Sessions dialog via View overlay
		b.WriteString(m.pickerView())
		return b.String()
	}
	if m.mpicker != nil && m.uiMode == opencodeMode {
		// floating Select-model dialog via View overlay
	} else if m.mpicker != nil {
		b.WriteString(m.modelPickerView())
		return b.String()
	}
	// One compact hint up top — the full roster lives behind the ctrl+p palette
	// and the /help command. The bottom hint covers the busy/interactive states.
	if m.uiMode != opencodeMode { // opencode keeps the top clean; the hint lives in the prompt row
		tips := "`ctrl+p` commands"
		b.WriteString(dimStyle.Render(tips) + "\n\n")
	}
	if details := m.agentDetails(); details != "" {
		b.WriteString(details + "\n\n")
	}
	b.WriteString(m.viewportView() + "\n") // selection highlight paints inside
	if m.curThink != "" {
		b.WriteString("\n" + m.thinkView() + "\n")
	}
	if m.uiMode == opencodeMode && m.busy && !m.thinkStart.IsZero() {
		// reasoning is streaming: opencode shows a transient "Thinking" line
		// where the collapsed "+ Thought: {dur}" will land on flush
		b.WriteString("\n   " + lipgloss.NewStyle().Foreground(ocWarnCol()).Render("+ Thinking…") + "\n")
	}
	if m.current != "" {
		b.WriteString("\n" + m.currentView() + "\n")
	}
	if m.iactive != nil {
		b.WriteString("\n" + m.interactiveView() + "\n")
	}
	if m.permDialog != nil {
		b.WriteString("\n" + m.permView() + "\n")
	}
	if len(m.plan) > 0 {
		b.WriteString("\n" + m.planView() + "\n")
	}
	if m.busy && m.uiMode != opencodeMode { // opencode mode: the status bar carries the spinner + esc hint
		hint := " thinking… (enter steers · safe panels run now · esc interrupts · ctrl+c ctrl+c interrupts)"
		if m.iactive != nil {
			hint = " bash (interactive) — type to respond · ctrl+c ctrl+c to cancel"
		} else if m.interrupt1 {
			hint = " thinking… (esc or ctrl+c again to interrupt)"
		}
		b.WriteString("\n" + m.spin.View() + dimStyle.Render(m.busyStats()+hint) + "\n")
	}
	b.WriteString("\n")
	if m.rew != nil {
		b.WriteString(m.rewindView() + "\n\n")
	}
	// Record where the input box starts (line offset within this viewBody) so
	// View can convert it to an absolute screen row for drag-select hit-testing.
	m.inputBodyOff = strings.Count(b.String(), "\n")
	if m.iactive == nil {
		if m.namePrompt != nil {
			b.WriteString(m.namePrompt.label + " ")
			if m.namePrompt.mask {
				// Secrets never echo: render the mask instead of the input's
				// live view (which would show the key in the clear). The "┃ "
				// prompt matches how the textarea renders its own first line.
				b.WriteString(m.highlightInput("┃ " + m.namePrompt.maskedValue(m.input.Value())))
			} else {
				b.WriteString(m.highlightInput(m.input.View()))
			}
		} else if m.uiMode == opencodeMode {
			// highlight BEFORE the box chrome is added, so the reverse-video
			// ranges land on the same raw lines inputPoint hit-tests
			b.WriteString(m.opencodePrompt(m.highlightInput(m.input.View()), m.width))
		} else {
			b.WriteString(m.highlightInput(m.input.View()))
		}
	}
	if m.quit1 {
		// first idle ctrl+c armed the quit; make the second press discoverable
		b.WriteString("\n" + errStyle.Render("press ctrl+c again to quit"))
	}
	if m.escClr {
		b.WriteString("\n" + errStyle.Render("esc again: clear the input (↑ recalls it)"))
	} else if m.esc1 && m.rew == nil && m.namePrompt == nil {
		b.WriteString("\n" + dimStyle.Render("esc again: rewind the conversation"))
	}
	if m.menu != nil && m.uiMode != opencodeMode {
		// opencode mode: View() overlays the menu ABOVE the input instead —
		// drawn on top of the frame, so nothing reflows while typing
		b.WriteString("\n" + m.menuView())
	}
	// The retained-agent strip is daemon-fed snapshot state.
	if dock := m.agentsDock(); dock != "" {
		b.WriteString("\n" + dock)
	}
	b.WriteString("\n\n" + m.statusView()) // persistent status line, with a blank line above
	return b.String()
}

// inputPlaceholder is the idle input hint; syncInputPlaceholder re-uses it
// when the busy state clears so the two sites never drift.
const inputPlaceholder = "Ask whip anything… (/ for commands, tab completes)"

// syncInputPlaceholder reflects the busy state into the input's placeholder:
// while a turn runs, typed text steers it at the next loop boundary. Called from View so it tracks
// the state every render. headless-safe.
func (m *model) syncInputPlaceholder() {
	if m.input.Value() != "" {
		return // placeholder is hidden once the user is typing
	}
	if !m.busy {
		m.input.Placeholder = inputPlaceholder
	} else {
		m.input.Placeholder = "busy — enter steers the running turn"
	}
}

func (m *model) planView() string {
	lines := make([]string, 0, len(m.plan))
	for _, item := range m.plan {
		mark := "○"
		switch item.Status {
		case "completed", "done":
			mark = "✓"
		case "in_progress", "running":
			mark = "●"
		}
		lines = append(lines, dimStyle.Render(mark+" ")+truncLine(item.Content, max(m.width-2, 1)))
	}
	return strings.Join(lines, "\n")
}

// statusView renders the always-on status line below the input: current
// directory, model (effort), provider, and session token spend. It mirrors
// the header's data but stays put while the transcript scrolls, so the four
// facts are always visible no matter where the viewport sits.
func (m *model) statusView() string {
	if m.uiMode == opencodeMode {
		return m.opencodeStatus()
	}
	model := m.modelName
	if e := effortLabel(m.displayEffort()); e != "off" {
		model += " (" + e + ")"
	}
	u := m.displayUsage()
	spend := fmtUsage(u)
	if cost, ok := m.sessionCost(); ok {
		spend += " · " + fmtCost(cost)
	}
	// The last response's own counts, so the size of the most recent API call
	// (its output especially) is readable without doing mental subtraction
	// from the session totals. Hidden until the first response arrives.
	if last := m.lastResp; last.PromptTokens > 0 || last.CompletionTokens > 0 {
		spend += " · last " + fmtUsage(last)
	}
	// The fixed segments (model/provider/spend) are the data that matters; the
	// cwd is what yields. Old code truncated the whole assembled line from the
	// right, dropping the completion-token count whenever the path was long.
	// Instead give the cwd only the space left after the fixed segments —
	// truncating it to its tail, then dropping it entirely — so the spend
	// survives whenever the fixed segments fit at all.
	right := fmt.Sprintf("   %s   %s   %s", model, m.provName, spend)
	dir := shortCWD(m.clientView.workingDir)
	if value, ok := m.runtimeAgent(m.agentOpen); ok {
		dir = shortCWD(value.CWD)
	}
	switch budget := max(m.width, 0) - len(" ") - len(right); {
	case len(dir) <= budget:
		// fits as-is
	case budget > 1:
		dir = "…" + dir[len(dir)-budget+1:] // keep the tail, the recognizable part
	default:
		dir = "" // no room for the path at all; show only the fixed segments
	}
	line := fmt.Sprintf(" %s%s", dir, right)
	return dimStyle.Render(truncLine(line, max(m.width, 0)))
}

// shortCWD renders the working directory compactly for the status line: the
// home directory collapses to ~ and only the last two path segments survive,
// so a deep path doesn't crowd out the rest of the status.
func shortCWD(dir string) string {
	if dir == "" {
		dir = cwd()
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	if len(parts) > 3 {
		return "…/" + strings.Join(parts[len(parts)-3:], "/")
	}
	return dir
}

const previewLines = 5

// pickerView renders the /resume browser: oldest at top, newest at bottom,
// the selected session expanded with previews of its last exchange.
func (m *model) pickerView() string {
	p := m.picker
	rows := []string{}
	expanded := 3 + 2*previewLines // meta + previews
	// how many collapsed rows fit alongside the expanded selection + footer
	budget := max(m.height-2-expanded-1, 2)
	lo := max(p.idx-budget/2, 0)
	hi := min(lo+budget+1, len(p.metas))

	for i := hi - 1; i >= lo; i-- { // metas is newest-first; render oldest on top
		meta := p.metas[i]
		title := meta.Title
		if title == "" {
			title = "(untitled)"
		}
		line := fmt.Sprintf("%s  %s · %s · %s @ %s", meta.ID, title, ago(meta.UpdatedAt), meta.Model, meta.Provider)
		if i != p.idx {
			rows = append(rows, wrap("    "+line, m.width))
			continue
		}
		rows = append(rows, wrap(botStyle.Render("  → ")+line, m.width))
		prev := p.previews[meta.ID]
		rows = append(rows, previewBlock(youStyle.Render(glyphUser), prev[0], m.width)...)
		rows = append(rows, previewBlock(botStyle.Render(glyphAssistant), prev[1], m.width)...)
	}
	rows = append(rows, dimStyle.Render(fmt.Sprintf("  (%d/%d) ↑ older · ↓ newer · enter resume · esc cancel", p.idx+1, len(p.metas))))
	// pad so the footer stays at the bottom of the screen
	for len(rows) < m.height-1 {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}

// previewBlock renders up to previewLines lines of a message under a prefix.
// previewBlock renders up to previewLines *rendered* lines of a message
// under a prefix, wrapping each source line at the given width (no
// truncation — long lines wrap instead of ending in "…").
func previewBlock(prefix, text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	w := max(width-8, 8)
	var lines []string
	for i, l := range strings.Split(text, "\n") {
		wrapped := strings.Split(ansi.Hardwrap(l, w, true), "\n")
		for j, wl := range wrapped {
			if i == 0 && j == 0 {
				lines = append(lines, "      "+prefix+wl)
			} else {
				lines = append(lines, "        "+wl)
			}
		}
	}
	if len(lines) > previewLines {
		lines = append(lines[:previewLines],
			dimStyle.Render(fmt.Sprintf("        … +%d lines (full text after resume)", len(lines)-previewLines)))
	}
	return lines
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (m *model) menuView() string {
	// window of menuRows candidates around the selection
	start := 0
	if m.menu.idx >= menuRows {
		start = m.menu.idx - menuRows + 1
	}
	end := min(start+menuRows, len(m.menu.cands))

	nameW := 0
	for _, c := range m.menu.cands[start:end] {
		nameW = max(nameW, len(c.Text))
	}
	if m.uiMode == opencodeMode {
		// opencode's autocomplete popup: a panel with the selected row in the
		// primary fill. Long descriptions word-wrap onto a second line (capped
		// at two — the menu stays scannable) instead of being chopped.
		bg := ocPanelBg()
		text := lipgloss.NewStyle().Foreground(ocTextCol()).Background(bg)
		muted := lipgloss.NewStyle().Foreground(ocMutedCol()).Background(bg)
		sel := lipgloss.NewStyle().Foreground(ocSelFg()).Background(ocSelBg())
		descW := max(m.width-nameW-6, 8)
		indent := strings.Repeat(" ", nameW+4)
		var rows []string
		for i := start; i < end; i++ {
			c := m.menu.cands[i]
			name := fmt.Sprintf("%-*s", nameW, c.Text)
			descLines := strings.Split(wrap(c.Desc, descW), "\n")
			if len(descLines) > 2 {
				descLines = descLines[:2]
				descLines[1] = truncLine(descLines[1], descW-2) + " …"
			}
			for j, d := range descLines {
				switch {
				case i == m.menu.idx && j == 0:
					rows = append(rows, ocPadTo(sel.Render("  "+name+"  "+d), m.width, ocSelBg()))
				case i == m.menu.idx:
					rows = append(rows, ocPadTo(sel.Render("  "+indent+d), m.width, ocSelBg()))
				case j == 0:
					rows = append(rows, ocPadTo(text.Render("  "+name)+muted.Render("  "+d), m.width, bg))
				default:
					rows = append(rows, ocPadTo(muted.Render("  "+indent+d), m.width, bg))
				}
			}
		}
		rows = append(rows, ocPadTo(muted.Render(fmt.Sprintf("  %d/%d", m.menu.idx+1, len(m.menu.cands))), m.width, bg))
		return strings.Join(rows, "\n")
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		c := m.menu.cands[i]
		line := fmt.Sprintf("%-*s  ", nameW, c.Text)
		var row string
		if i == m.menu.idx {
			row = botStyle.Render("→ "+line) + dimStyle.Render(c.Desc)
		} else {
			row = "  " + line + dimStyle.Render(c.Desc)
		}
		// clamp to the content width: an untruncated description widens the
		// whole frame (in opencode mode it shoved the sidebar off-screen)
		b.WriteString(ansi.Truncate(row, max(m.width, 8), "…"))
		b.WriteByte('\n')
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("  (%d/%d)", m.menu.idx+1, len(m.menu.cands))))
	return b.String()
}

func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Wrap(s, width, " ") // word-aware: break at spaces, not mid-token
}

func truncLine(s string, width int) string {
	if width > 0 && len(s) > width {
		return s[:width-1] + "…"
	}
	return s
}
