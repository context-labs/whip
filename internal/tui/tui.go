// Package tui is whip's interactive bubbletea session (fullscreen alt-screen).
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
	"github.com/context-labs/whip/internal/tui/ui"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// UI styles stay legible on both dark and light terminal backgrounds. Lip
// Gloss v2 has no global background state, so refreshBaseStyles rebuilds
// them from whip's own scheme detection whenever it changes.
var youStyle, botStyle, toolStyle, dimStyle, errStyle, thinkingStyle lipgloss.Style

func init() { refreshBaseStyles() }

// refreshBaseStyles picks the light or dark variant of every package-level
// style for the current scheme (see SetLightTheme / SetUnknownTheme).
func refreshBaseStyles() {
	rebuildTheme()
	th := currentTheme()
	youStyle = th.On(th.Info, nil).Bold(true)
	botStyle = th.On(th.Accent, nil).Bold(true)
	toolStyle = th.On(th.Warning, nil)
	dimStyle = th.On(th.Muted, nil)
	errStyle = th.On(th.Error, nil)
	thinkingStyle = th.On(th.Muted, nil).Italic(true)
	diffAddStyle = th.On(nil, th.DiffAdd)
	diffDelStyle = th.On(nil, th.DiffDel)
}

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
	vp     transcriptView
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
	// rows so drag-select can hit-test/extract/highlight it.
	inputLines  []string         // the input box's rendered lines, ANSI-stripped
	scr         *uv.ScreenBuffer // the frame buffer View draws into (reused across frames)
	themeHow    string           // how auto theme detection resolved (env var, OSC query, …) — captured at startup/theme change for /report; never re-queried
	sessTitle   string           // cached session title for the opencode sidebar (from the store; updated on title/rename)
	msgActions  *msgActions      // opencode mode: the Message Actions dialog opened by clicking a message; nil = closed
	ocThink     string           // opencode mode: reasoning text accumulated for the expandable "+ Thought" block
	toast       string           // opencode mode: top-right toast text; "" = none
	toastAt     time.Time        // when the current toast was shown (stale clears are ignored)
	leaderAt    time.Time        // opencode mode: when ctrl+x armed the leader chord; zero = not pending
	sidebarHide bool             // opencode mode: ctrl+x b hides the sidebar
	// updateLatest is a pending newer release tag ("" when none), picked up
	// from update.Pending at startup; the notice it renders is durable, so a
	// check that lands after the report still shows next launch.
	updateLatest string
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
	ti.Prompt = "" // the prompt box (opencodePrompt) draws the ┃ bar per line
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
	ti.SetStyles(currentTheme().Textarea) // applyOpencodeStyles re-applies on every theme change
	ti.SetVirtualCursor(false)            // the terminal draws the cursor where View places it
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
	quiet := true // the full-screen UI keeps the startup report to the essentials
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
	if !knownThemeName(theme) {
		theme = "auto"
	}
	how := m.applyTheme(theme)
	m.themeHow = how        // explicit picks return "" — detection source no longer applies
	m.applyOpencodeStyles() // refresh the input box fill for the new scheme
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
		setSchemeOverride("light")
	case "dark":
		SetLightTheme(false)
		setSchemeOverride("dark")
	case "", "auto": // don't touch m.cfg.Theme — setTheme owns persistence
		setSchemeOverride("")
		how = detectColorScheme()
	default: // a user theme: its darkness picks the scheme, its name pins the palette
		spec := userThemeSpec(theme)
		if spec == nil {
			loadUserThemes()
			spec = userThemeSpec(theme)
		}
		if spec == nil {
			setSchemeOverride("")
			return detectColorScheme()
		}
		SetLightTheme(!spec.Dark)
		setSchemeOverride(theme)
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
	// cache of the last render: valid while !stale, width matches and the
	// theme generation is unchanged (a runtime /theme repaints every block).
	rendered string
	rows     []string // rendered split into rows (what the transcript window reads)
	gen      int
	lines    int
	width    int
	stale    bool
}

// renderAt returns the block rendered at width, re-rendering only when the
// cache is cold (first render, width change, or text/expand mutation). This
// is what makes appends and resume cheap: unchanged blocks never re-render.
func (b *block) renderAt(width int) string {
	gen := themeGeneration()
	if !b.stale && b.width == width && b.gen == gen {
		return b.rendered
	}
	b.rendered = b.render(width)
	b.rows = strings.Split(b.rendered, "\n")
	b.lines = len(b.rows)
	b.width, b.gen, b.stale = width, gen, false
	return b.rendered
}

// render renders the block at width (the full terminal width; assistant
// blocks get their marker + indent here so a resize re-renders everything).
func (b block) render(width int) string {
	switch b.kind {
	case blockUser:
		return opencodeUserCard(b.text, width)
	case blockOCMeta:
		return b.text // pre-indented; verbatim so wrap() can't trim the indent
	case blockThought:
		// collapsed: opencode's "+ Thought: {dur}" line; expanded (click/ctrl+e):
		// the reasoning text underneath, muted italic like whip's thinking style
		th := currentTheme()
		head := "   " + th.On(th.Warning, nil).Render("+ Thought: "+b.live)
		if !b.expanded {
			return head
		}
		body := th.On(th.Muted, nil).Italic(true).Render(strings.TrimSpace(b.text))
		return head + "\n" + wrap(body, width)
	case blockAssistant:
		// assistant messages carry no bullet: the body is indented 3
		w := width - 3
		if w <= 0 {
			w = 80 // no terminal size yet: sane default
		}
		return indentLines(renderMarkdownAt(b.text, w, b.fileRoot), 3)
	case blockTool:
		// A result carrying a fenced diff (write/edit) renders claude-style:
		// "⎿ Added N lines, removed M lines" over a colored, line-numbered
		// diff — the change IS the collapsed view, not hidden behind expand.
		if diff, rest := extractDiff(strings.TrimRight(b.text, "\n")); diff != "" {
			return renderDiffResult(diff, rest, b.expanded, width)
		}
		lines := strings.Split(strings.TrimRight(b.text, "\n"), "\n")
		// results tuck behind the tool row: a muted one-line hint collapsed,
		// the full body only when expanded
		return ocToolResult(lines, b.expanded, strings.HasPrefix(b.text, "Error"), width)
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
	line := 0
	for i := range m.blocks {
		if i > 0 {
			line++ // one blank separator row between blocks
		}
		m.blocks[i].renderAt(width) // warms the cache; unchanged blocks cost nothing
		m.blocks[i].y0 = line
		m.blocks[i].y1 = line + m.blocks[i].lines - 1
		line = m.blocks[i].y1 + 1
	}
	m.vp.rows = m.contentRow
	m.vp.setTotal(m.contentPad() + line) // blank pad rows (bottom-anchoring) then the content
	if m.follow {
		m.vp.GotoBottom()
	}
}

// contentPad is the number of blank lines refreshVP prepends when the
// transcript is shorter than the viewport (click-row mapping accounts for it).
func (m *model) contentPad() int {
	if len(m.blocks) == 0 {
		return m.vp.Height()
	}
	h := m.blocks[len(m.blocks)-1].y1 + 1 // content height from the last block
	return max(m.vp.Height()-h, 0)
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
	s := sanitizeView(m.vp.View()) // the drag selection is painted onto the cells by View
	if len(m.blocks) == 0 {        // empty transcript: the centered-logo home screen
		return opencodeHome(m.vp.Width(), m.vp.Height())
	}
	// Full-height viewport: keep the pad so the transcript is bottom-anchored
	// (blanks above, content near the prompt) and the prompt/status sit at the
	// bottom of the screen.
	return s
}

func (m *model) Init() tea.Cmd {
	// Bubble Tea's own OSC 11 query is a second scheme signal: whip's pre-run
	// query stays authoritative (it handles tmux passthrough), and the reply
	// only matters when that query came back unknown (applyDetectedBackground).
	cmds := []tea.Cmd{waitClientUpdate(m.client), tea.RequestBackgroundColor}
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

// applyDetectedBackground consumes Bubble Tea's background-color reply. It
// resolves the scheme only when whip's own pre-run query could not (mosh, an
// old tmux without passthrough) and no theme is pinned in the config; a known
// scheme is never overridden, so the two signals cannot flip-flop.
func (m *model) applyDetectedBackground(msg tea.BackgroundColorMsg) {
	if ocThemeKnown() || msg.Color == nil || (m.cfg != nil && m.cfg.Theme != "") {
		return
	}
	r, g, b, _ := msg.Color.RGBA()
	light := !msg.IsDark()
	bgCache = bgResult{light: light, valid: true, hasRGB: true, r: int(r >> 8), g: int(g >> 8), b: int(b >> 8)}
	SetLightTheme(light)
	m.themeHow = "terminal reply after startup"
	m.applyOpencodeStyles()
	m.refreshVP()
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
		SetLightTheme(light) // glamour markdown style + the package-level styles
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
	ti.SetStyles(m.input.Styles())
	ti.SetWidth(m.input.Width() + lipgloss.Width(ti.Prompt)) // Width() is content width; SetWidth takes total
	ti.SetHeight(h)
	ti.SetValue(val)
	ti.CursorEnd()
	m.input = ti
	m.input.Focus() // re-snapshot the style pointer at the COPIED struct (see applyOpencodeStyles)
}

// layout gives the viewport whatever height the other regions don't need,
// growing the input box with its content so the whole prompt stays visible.
// The same measurements place the region rectangles (layoutFrame), so a
// full transcript can never overflow the terminal or shift the mouse rows.
func (m *model) layout() {
	m.growInput()
	// Floor the viewport width too: a degenerate m.width (1–4 cols) would set
	// the viewport to 1 col and re-slice the transcript into a one-char strip,
	// regardless of the render floor in refreshVP.
	w, h := max(m.width, minRenderWidth), max(m.height-m.measure().fixed(), 1)
	if m.vp.Width() != w || m.vp.Height() != h {
		m.vp.SetWidth(w)
		m.vp.SetHeight(h)
		m.refreshVP()
	}
}

// nowFn returns the current time, honoring the test seam when set.
func (m *model) nowFn() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
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
	if !m.thinkStart.IsZero() { // collapse the reasoning segment to one line (expandable to the text)
		m.blocks = append(m.blocks, block{kind: blockThought, text: m.ocThink, live: fmtShortDur(m.nowFn().Sub(m.thinkStart))})
		m.follow = true
		m.refreshVP()
		m.thinkStart = time.Time{}
		m.ocThink = ""
	}
	m.curThink = ""
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
// view's top row, which is always row 0 in the alternate screen.
// recordInputRows notes the input box's absolute screen rows for drag-select.
// The input is hidden during interactive bash (iactive), so there's nothing
// to select.
func (m *model) recordInputRows() {
	if m.iactive != nil || m.height == 0 {
		m.inputLines = nil
	} else {
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
}

func (m *model) viewBody() string {
	var b strings.Builder
	if details := m.agentDetails(); details != "" {
		b.WriteString(details + "\n\n")
	}
	b.WriteString(m.viewportView() + "\n") // selection highlight paints inside
	if m.curThink != "" {
		b.WriteString("\n" + m.thinkView() + "\n")
	}
	if m.busy && !m.thinkStart.IsZero() {
		// reasoning is streaming: a transient "Thinking" line
		// where the collapsed "+ Thought: {dur}" will land on flush
		b.WriteString("\n   " + thinkingLabel() + "\n")
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
	b.WriteString("\n")
	if m.rew != nil {
		b.WriteString(m.rewindView() + "\n\n")
	}
	if m.iactive == nil {
		if m.namePrompt != nil {
			b.WriteString(m.namePrompt.label + " ")
			if m.namePrompt.mask {
				// Secrets never echo: render the mask instead of the input's
				// live view (which would show the key in the clear). The "┃ "
				// prompt matches how the textarea renders its own first line.
				b.WriteString("┃ " + m.namePrompt.maskedValue(m.input.Value()))
			} else {
				b.WriteString(m.input.View())
			}
		} else {
			b.WriteString(m.opencodePrompt(m.input.View(), m.width))
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
func (m *model) statusView() string { return m.opencodeStatus() }

const previewLines = 5

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
	{
		// the autocomplete popup: a panel with the selected row in the primary
		// fill. Long descriptions word-wrap onto a second line (capped at two
		// — the menu stays scannable) instead of being chopped.
		th := currentTheme()
		bg := th.Surface.Panel
		text := th.On(th.Text, bg)
		muted := th.On(th.Muted, bg)
		sel := th.Selected
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
					rows = append(rows, ui.PadRow(sel.Render("  "+name+"  "+d), m.width, th.Primary))
				case i == m.menu.idx:
					rows = append(rows, ui.PadRow(sel.Render("  "+indent+d), m.width, th.Primary))
				case j == 0:
					rows = append(rows, ui.PadRow(text.Render("  "+name)+muted.Render("  "+d), m.width, bg))
				default:
					rows = append(rows, ui.PadRow(muted.Render("  "+indent+d), m.width, bg))
				}
			}
		}
		rows = append(rows, ui.PadRow(muted.Render(fmt.Sprintf("  %d/%d", m.menu.idx+1, len(m.menu.cands))), m.width, bg))
		return strings.Join(rows, "\n")
	}
}

// thinkingLabel is the transient "+ Thinking…" line while reasoning streams.
func thinkingLabel() string {
	th := currentTheme()
	return th.On(th.Warning, nil).Render("+ Thinking…")
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
