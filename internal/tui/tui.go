// Package tui is whip's interactive bubbletea session (fullscreen alt-screen).
package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/memory"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/tools/bashrun"
	"github.com/context-labs/whip/internal/update"

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
	steeredMsg    string
)

// goalFromContextMsg carries the model-formulated goal back from the
// /goal-from-context goroutine to the Update loop.
type goalFromContextMsg struct {
	goal string
	err  error
}

type compactMsg struct {
	took, kept int // messages removed / kept after compaction
	summary    string
	cutoff     int               // index in the pre-compaction history the summary replaces
	info       agent.CompactInfo // which model wrote the summary + its spend
	err        error
}

// compactStartMsg announces a compaction the moment folding begins: the
// summary call can take seconds, so the transcript shows "compacting…" while
// it runs instead of looking hung.
type compactStartMsg struct {
	took, est int // pre-compaction message count / estimated tokens
}
type turnDoneMsg struct {
	final string
	err   error
	at    int    // conversation index the turn started at (snapshot key)
	snap  string // pre-turn workspace snapshot commit ("" = not a git repo)
	clean bool   // the turn left the tree clean — snap is worthless, drop it
}
type (
	catalogsMsg    map[string]config.Catalog // background /models fetch result
	noticeMsg      string                    // dim one-liner appended to the transcript
	usageMsg       llm.Usage                 // one request's token usage
	quitArmMsg     struct{}                  // the idle ctrl+c arm window expired
	taskUpdateMsg  struct{}                  // a background subagent started/settled — redraw
	waitWakeMsg    string                    // an idle wait fired — wake as a machine turn
	orphanSteerMsg string                    // a steer orphaned at turn teardown — submit as a machine turn
	mcpStatusMsg   struct{}                  // an MCP server changed state — redraw
	thinkMsg       string                    // streamed reasoning tokens
	imageMsg       struct {                  // ctrl+v clipboard image result
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
	cfg       *config.Config
	agent     *agent.Agent
	modelName string
	provName  string
	sysPrompt string
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

	showThinking bool   // ctrl+o: render reasoning tokens
	curThink     string // in-flight partial reasoning line
	inThink      bool   // "◌ " thinking prefix printed for this reasoning segment
	menu         *menu
	picker       *picker
	mpicker      *modelPicker
	palette      *palette // ctrl+p command palette (modal dialog)
	cancel       context.CancelFunc
	prog         *tea.Program

	store     *session.Store
	sessionID string
	saved     int            // messages already persisted (index into agent.Messages)
	snapshots map[int]string // workspace snapshot ref per turn-start index (mirrors the snapshots table)

	hist     []string         // submitted inputs, for up/down recall
	pasteBuf string           // held paste text for the [Pasted ~N lines] placeholder (config collapsePaste)
	histIdx  int              // len(hist) == not navigating
	draft    string           // in-progress input saved while navigating history
	lastUp   time.Time        // last ↑ keypress; repeat detection for history rollover
	now      func() time.Time // test seam; defaults to time.Now

	turnStart  time.Time // when the in-flight turn began; zero when idle (busy line shows elapsed)
	thinkStart time.Time // opencode mode: when the current reasoning segment began (collapsed to "+ Thought: {dur}")

	queue      []string // messages typed while busy, sent after the turn ends
	queueSel   int      // selected queued message, -1 = none (not navigating)
	interrupt1 bool     // first ctrl+c pressed while busy; second cancels
	quit1      bool     // first ctrl+c pressed while idle; second quits (armed briefly)

	goal       string // active /goal; the loop continues until GOAL_MET
	goalRounds int    // continuation turns spent on the current goal
	titled     bool   // an auto-title has been attempted for this session

	pendingForkID string // busy-forked copy awaiting the turn's end to switch into ("" = none)

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
	compactModel string      // config model name for compaction summaries; "" = the built-in default
	compactProv  string
	// updateLatest is a pending newer release tag ("" when none), picked up
	// from update.Pending at startup; the notice it renders is durable, so a
	// check that lands after the report still shows next launch.
	updateLatest string
	effortX      int                       // screen column where the clickable ⚡ effort control starts
	catalogs     map[string]config.Catalog // provider model lists (capabilities)
	mcpMgr       *mcp.Manager              // MCP server connections; nil when none configured
	mcpSeen      map[string]bool           // servers whose first settle was announced
	lspMgr       *lsp.Manager              // LSP diagnostics source for write/edit tool output
	// skillScan is the skills discovery seam (skills.Scan over DefaultDirs in
	// the real model): a field so the context doctor can be tested against
	// temp-dir skills instead of whatever the test machine happens to have.
	skillScan func() []skills.Skill

	irunner *interactiveRunner // installed on tools.InteractiveBash at startup
	iactive *interactive       // in-flight interactive command; nil when idle

	perms      permRules   // saved allow-always rules
	permDialog *permDialog // open permission modal; the turn is paused on it

	tasksFocus bool      // the tasks dock owns ↑/↓/enter (never esc); typing or ↑ past the top returns to the input
	taskSel    int       // selected row in the dock (index into newest-first tasks)
	dockSkip   int       // non-task rows at the dock's top (focused hint) — click math skips them
	taskVP     *taskView // open per-task detail view; nil when on the main thread
	dockRows   int       // rendered dock height; layout() maintains it for click math

	rew    *rewindState  // open rewind picker (double-esc while idle)
	esc1   bool          // first idle esc pressed; second opens the rewind picker
	escClr bool          // first esc pressed with a draft; second clears it to history
	future []llm.Message // clipped tail kept for forward travel after a rewind

	namePrompt *namePrompt // inline text prompt (fork naming, /rename)

	// infAuth holds the in-flight inference-net device login across the
	// team → project → create prompts.
	infAuth *inferenceNetPending

	// initialPrompt (whip up <words>) is submitted as the first turn from
	// Init — late enough that m.prog exists for the turn goroutine's p.Send.
	initialPrompt string
}

// initialPromptMsg is Init's one-shot kickoff of a `whip up` first turn.
type initialPromptMsg struct{}

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

// Run starts the interactive session. It returns the id of the session that
// was active on exit ("" if nothing was said). firstRun reports the config
// file did not exist at startup (the caller checks config.Exists before
// config.Load creates it) and triggers the one-time setup wizard.
// initialPrompt (`whip up <words>`) is submitted as the first turn once the
// UI is up — after any resume replay, matching `whip run`'s order.
func Run(cfg *config.Config, modelName, provName, sysPrompt, resumeID string, cautious, firstRun bool, initialPrompt string) (string, error) {
	// One shared stdin reader for the pre-TUI prompts: a bufio.Reader reads
	// ahead, so separate readers for the trust gate and the setup wizard would
	// lose buffered answers (a pasted "y\n2\n…\n" answers both).
	stdinR := bufio.NewReader(os.Stdin)

	// Trust gate first: before whip reads a single file, ask whether this
	// folder's contents may steer the model. Persisted per absolute path in
	// ~/.whip/trusted.json (claude-code's per-project trust dialog).
	if ok, err := checkTrust(stdinR); err != nil {
		return "", err
	} else if !ok {
		return "", errors.New("folder not trusted")
	}

	// First run only: the setup wizard (provider, thinking display, MCP
	// imports) before the TUI takes the terminal. Skipped silently when stdin
	// isn't a terminal — headless launches keep the defaults.
	if firstRun {
		if err := setupWizard(cfg, stdinR); err != nil {
			return "", err
		}
	}

	ag, mn, pn, err := buildAgentWithRefresh(cfg, modelName, provName, sysPrompt)
	if err != nil {
		return "", err
	}

	ti := newInput()

	// Reasoning effort: an explicit cfg.DefaultEffort is honored as-is; "" (no
	// config / pre-feature file) resolves model-aware — "low" when the model
	// advertises it, else the lowest supported level, else off (no parameter) —
	// so a non-reasoning model never opens on an effort it can't accept.
	ag.Effort = DefaultEffortFor(config.LoadCatalogs(), pn, ag.Model, cfg.DefaultEffort)
	// Mouse capture ON by default so the wheel scrolls the transcript viewport
	// and ⚡/tool clicks work — with button-motion reporting (?1002) so a left
	// drag becomes whip's own selection (select.go): enabling click reporting
	// alone makes most terminals (Ghostty, kitty) suppress their native
	// drag-selection without sending the drag to anyone. With capture off,
	// tmux's WheelUpPane binding sees mouse_any_flag=0 and runs 'copy-mode -e',
	// scrolling tmux's own scrollback instead of the transcript. Inside tmux,
	// tmux forwards the drag to whip (mouse_any_flag is set), so whip's own
	// selection handles drag-to-copy there too. Explicit config wins.
	mouseOn := true
	if cfg.Mouse != nil {
		mouseOn = *cfg.Mouse
	}
	showThinking := true // default on; "thinking": false in config opts out
	if cfg.Thinking != nil {
		showThinking = *cfg.Thinking
	}
	sidebarHide := false // default shown (when the terminal is wide enough); "sidebar": false opts out at startup
	if cfg.Sidebar != nil {
		sidebarHide = !*cfg.Sidebar
	}
	m := &model{
		cfg: cfg, agent: ag, modelName: mn, provName: pn, sysPrompt: sysPrompt,
		input: ti, spin: spinner.New(spinner.WithSpinner(spinner.Dot)), follow: true, saved: 1, hoverIdx: -1,
		catalogs: config.LoadCatalogs(), mouseOn: mouseOn, now: time.Now, showThinking: showThinking,
		sidebarHide:  sidebarHide,
		compactModel: cfg.CompactModel, compactProv: cfg.CompactProvider,
		skillScan:     func() []skills.Skill { return skills.Scan(skills.DefaultDirs()...) },
		initialPrompt: initialPrompt,
	}
	m.applyCompactModel()
	m.applyTaskModel()
	m.agent.CompactThreshold = compactThresholdFor(cfg)
	m.wireTasks() // redraw the UI when background subagents start/settle

	// MCP: merge whip's own config with imported claude (.mcp.json) and codex
	// (~/.codex/config.toml) servers — gated by the mcpImport policy, whose
	// blocked entries stay visible in /mcp — then kick concurrent connects in
	// the background. Tool calls block on that server's first settle only, so a
	// slow/hung server never delays startup. Discovery problems (a broken
	// .mcp.json) land as a transcript note, not a startup failure.
	if wd, wdErr := os.Getwd(); wdErr == nil {
		disc := mcp.LoadMergedFiltered(wd, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
		merged, mcpErrs := disc.Merged, disc.Errs
		if len(merged) > 0 || len(disc.Blocked) > 0 || len(mcpErrs) > 0 {
			m.mcpMgr = mcp.NewManager(merged)
			m.mcpMgr.SetBlocked(disc.Blocked)
			m.mcpMgr.SetOnChange(m.mcpOnChange())
			m.mcpMgr.Start(context.Background())
			ag.SetMCPTools(m.mcpMgr.Tools())
			for src, derr := range mcpErrs {
				m.append(errStyle.Render(fmt.Sprintf("mcp: %s: %s", src, derr)))
			}
		}
		// LSP: build the diagnostics manager (built-ins merged under the
		// config's "lsp" block) and install it for write/edit tool output.
		// Servers spawn lazily on first covered file touch; a missing binary
		// is remembered as broken, so this never blocks startup.
		m.lspMgr = lsp.NewManager(lsp.FromConfigMap(cfg.LSPServers))
		tools.LSP = m.lspMgr
	}
	// computer-use: the per-app consent prompt — installed once, here, where
	// the model exists (buildAgent is package-level and has no m).
	tools.ComputerApprover = m.computerConsent
	// Permission prompts are opt-in (--cautious); without it tools run free.
	if cautious {
		m.installPermGate()
	}
	if dir, derr := config.Dir(); derr == nil {
		if st, serr := session.Open(dir + "/sessions.db"); serr == nil {
			m.store = st
			defer func() { _ = st.Close() }()
			// Seed input recall with user messages from ALL sessions (every
			// folder), so ↑ cycles global history, not just this session's.
			// UserHistory is newest-first; hist is oldest-first (up-arrow walks
			// back from the end), so reverse it into place.
			if hist, herr := st.UserHistory(500); herr == nil && len(hist) > 0 {
				for _, h := range slices.Backward(hist) {
					m.hist = append(m.hist, h)
				}
				m.histIdx = len(m.hist)
			}
		} else {
			config.LogEvent("session.open", "FAILED: "+serr.Error())
			m.append(errStyle.Render("sessions disabled: " + serr.Error()))
		}
	}
	if resumeID != "" {
		if m.store == nil {
			return "", errors.New("cannot resume: session store unavailable")
		}
		if err := m.resume(resumeID); err != nil {
			return "", err
		}
	}
	// Pick up whatever the update check recorded: a notice from an earlier
	// launch always shows; one discovered by main's background check shows
	// this launch if its 1 RTT beats startup (first-run trust prompt), else
	// next launch — the record is durable either way.
	m.updateLatest = update.Pending(Version)
	// Resolve the theme BEFORE applyUIMode/startupReport: opencode mode bakes
	// theme-resolved colors into the input styles, and startupReport's
	// unknown-background notice must reflect the final detection result.
	m.themeHow = m.applyTheme(cfg.Theme)
	if cfg.UIMode == opencodeMode {
		m.applyUIMode(opencodeMode) // set the mode BEFORE startupReport so it renders opencode-clean
	}
	m.startupReport()

	// Inline rendering (no alt-screen): the transcript lives in the normal
	// terminal scrollback, so terminal scrollback owns history. Mouse capture
	// is ON with click+wheel+button-motion (?1000/?1002): the wheel scrolls
	// the viewport (capture off → tmux eats it into copy-mode), ⚡ clicks
	// work, and a left drag paints whip's own selection and copies on release
	// (select.go) — terminals hand the drag to the app once any mouse mode is
	// on, so native selection isn't available anyway.
	//
	// We do NOT use tea.WithMouseCellMotion + an output filter: piping the
	// program output through a non-TTY makes bubbletea skip terminal-size
	// detection (ttyOutput becomes nil → no WindowSizeMsg → width/height stay
	// 0 and the whole layout collapses). Instead we keep the real TTY as the
	// output and enable click/wheel reporting directly on it.
	opts := []tea.ProgramOption{}
	if cfg.UIMode == opencodeMode {
		opts = append(opts, tea.WithAltScreen()) // opencode mode owns the whole screen
	}
	// Bottom-anchor the inline view: move the cursor to the terminal's last
	// row before bubbletea's first paint, so the view's screen position is
	// knowable (viewTop = height - viewH). Without this the view starts
	// wherever the shell prompt left the cursor, and mouse events — which are
	// ABSOLUTE screen coordinates — map a few rows off (drag-select landing
	// two lines above the pointer).
	fmt.Fprint(os.Stdout, "\x1b[9999;1H")
	if m.mouseOn {
		enableClickWheelMouse(os.Stdout)
		if cfg.UIMode == opencodeMode {
			// all-motion tracking (?1003, a superset of ?1002) so passive mouse
			// moves drive opencode's hover highlight on message cards
			fmt.Fprint(os.Stdout, "\x1b[?1003h")
		}
	}
	if m.cfgExtra == nil {
		m.cfgExtra = map[string]string{}
	}
	if dir, err := config.Dir(); err == nil { // watcher baseline: only later saves sync
		if fi, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
			m.cfgMod = fi.ModTime()
		}
	}
	p := tea.NewProgram(m, opts...)
	m.prog = p
	// install the interactive bash runner so the agent's bash tool can hand
	// sudo/ssh-style prompts to the user with a 15s inactivity timeout.
	m.irunner = newInteractiveRunner(p)
	tools.InteractiveBash = m.irunner
	go m.fetchCatalogs(false)
	go func() { p.Send(cfgSyncTick{}) }()     // start the config watcher
	go func() { p.Send(scheduleTickMsg{}) }() // start the wakeup channel
	// From here the terminal belongs to bubbletea: theme re-detections must
	// not run raw tty queries (see detectColorScheme) or they kill its input
	// reader with a spurious EOF.
	tuiRunning = true
	_, err = p.Run()
	tuiRunning = false
	// The UI has exited (quit, /quit, or a signal). We enabled click/wheel mouse
	// reporting directly on the TTY (bubbletea doesn't manage it), so release it.
	if m.mouseOn {
		disableClickWheelMouse(os.Stdout)
	}
	// Shut MCP servers down first (graceful: stdin close → SIGTERM → SIGKILL)
	// so a clean stdio server never becomes a KillAll target.
	if m.mcpMgr != nil {
		m.mcpMgr.Close()
	}
	// LSP servers get the same courtesy (shutdown/exit, then SIGKILL).
	if m.lspMgr != nil {
		m.lspMgr.Close()
		tools.LSP = nil
	}
	// Make sure no agent-spawned child process (a server the model started, a
	// watcher, a daemon) outlives whip. KillAll SIGKILLs every tracked process
	// group and waits for them.
	bashrun.KillAll()
	return m.sessionID, err
}

// startupReport prints one block naming what whip loaded — skills (with
// validation warnings, pi's [Skill conflicts] lesson: a silently truncated or
// unparseable SKILL.md is a broken skill the user never learns about) and MCP
// servers — plus degraded-mode notices. Skipped on resume (the transcript
// already carries the past).
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
	if m.mcpMgr != nil {
		sts := m.mcpMgr.Statuses()
		var parts, failed []string
		for _, st := range sts {
			switch st.Status {
			case mcp.StatusReady:
				parts = append(parts, fmt.Sprintf("%s ✓ (%d tools)", st.Name, st.Tools))
			case mcp.StatusFailed:
				parts = append(parts, st.Name+" ✗")
				failed = append(failed, st.Name+" ✗")
				warned = true
			case mcp.StatusDisabled:
				parts = append(parts, st.Name+" ○")
			default:
				parts = append(parts, st.Name+" ◌")
			}
		}
		if quiet {
			parts = failed // quiet startup: only broken servers are worth a line
		}
		if len(parts) > 0 {
			line("mcp: %s", strings.Join(parts, " · "))
		}
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

// catalogLites converts llm model records into the catalog-cache shape.
func catalogLites(infos []llm.ModelInfo) []config.ModelInfoLite {
	lites := make([]config.ModelInfoLite, len(infos))
	for i, mi := range infos {
		lites[i] = config.ModelInfoLite{
			ID:                  mi.ID,
			ContextLength:       mi.ContextLength,
			MaxCompletionTokens: mi.MaxCompletionTokens,
			ReasoningEfforts:    mi.ReasoningEfforts,
			InputModalities:     mi.InputModalities,
		}
		if mi.Pricing != nil {
			lites[i].InPrice, lites[i].OutPrice, lites[i].CacheReadPrice = mi.Pricing.Rates()
		}
	}
	return lites
}

// refreshCatalogs synchronously refreshes the cached model list of every
// configured provider that needs it (missing/stale cache, or any provider
// when force) and persists the result. Fetch or key failures skip that
// provider, keeping its stale cache; it returns the merged catalogs (never
// nil) whether or not anything was written.
func refreshCatalogs(cfg *config.Config, force bool) map[string]config.Catalog {
	cats := config.LoadCatalogs()
	dirty := false
	for name, prov := range cfg.Providers {
		if c, ok := cats[name]; ok && !force && !c.Stale() && c.BaseURL == prov.BaseURL {
			continue
		}
		key, keyErr := prov.ResolveKey()
		if keyErr != nil {
			config.LogEvent("catalog.fetch", name+" skipped: "+keyErr.Error())
			continue
		}
		if key == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		infos, err := llm.New(prov.BaseURL, key).Models(ctx)
		cancel()
		if err != nil {
			config.LogEvent("catalog.fetch", name+" failed: "+err.Error())
			continue // keep any stale cache
		}
		config.LogEvent("catalog.fetch", fmt.Sprintf("%s ok: %d models", name, len(infos)))
		cats[name] = config.Catalog{FetchedAt: time.Now(), BaseURL: prov.BaseURL, Models: catalogLites(infos)}
		dirty = true
	}
	if dirty {
		_ = config.SaveCatalogs(cats) // best-effort; the TUI still gets the fresh data
	}
	return cats
}

// fetchCatalogs refreshes each provider's cached model list in the background
// and sends the merged result to the UI. force bypasses the 24h TTL
// (/model refresh) so newly announced models appear immediately.
func (m *model) fetchCatalogs(force bool) {
	cats := refreshCatalogs(m.cfg, force)
	if m.prog != nil { // nil in tests that drive the command dispatch directly
		m.prog.Send(catalogsMsg(cats)) //nolint:uilock // background: fetchCatalogs is always `go`-launched
	}
}

// buildAgentWithRefresh retries a launch-time buildAgent once when the model
// misses: an "unknown model" may only mean the provider's cached catalog is
// stale or absent (deleted ~/.whip/models.json, or a model announced after
// the last fetch), so it force-refreshes the catalogs and retries. The happy
// path performs no network fetch; a persistent failure surfaces the ORIGINAL
// error (the refresh's failures are already logged by refreshCatalogs).
func buildAgentWithRefresh(cfg *config.Config, modelName, provName, sysPrompt string) (*agent.Agent, string, string, error) {
	ag, mn, pn, err := buildAgent(cfg, modelName, provName, sysPrompt)
	var unknown *config.UnknownModelError
	if !errors.As(err, &unknown) {
		return ag, mn, pn, err
	}
	config.LogEvent("catalog.fetch", fmt.Sprintf("startup resolve missed %q — force-refreshing catalogs", unknown.Model))
	refreshCatalogs(cfg, true)
	if ag, mn, pn, rerr := buildAgent(cfg, modelName, provName, sysPrompt); rerr == nil {
		return ag, mn, pn, nil
	}
	return nil, "", "", err
}

// ResolveWithRefresh is the same retry-on-miss wrapper for the headless
// entrypoints (whip run, whip acp), which call cfg.Resolve directly instead
// of building an agent.
func ResolveWithRefresh(cfg *config.Config, modelName, provName string) (config.Provider, config.Model, string, error) {
	prov, mdl, id, err := cfg.Resolve(modelName, provName)
	var unknown *config.UnknownModelError
	if !errors.As(err, &unknown) {
		return prov, mdl, id, err
	}
	config.LogEvent("catalog.fetch", fmt.Sprintf("startup resolve missed %q — force-refreshing catalogs", unknown.Model))
	refreshCatalogs(cfg, true)
	if prov, mdl, id, rerr := cfg.Resolve(modelName, provName); rerr == nil {
		return prov, mdl, id, nil
	}
	return config.Provider{}, config.Model{}, "", err
}

// resume replaces the conversation with a stored session.
func (m *model) resume(id string) error {
	meta, msgs, err := m.store.Load(id)
	if err != nil {
		return err
	}
	// prefer the session's model/provider; fall back to current on error.
	// The session's own effort wins; a row that pre-dates per-session effort
	// ("") inherits the current default and gets stamped on the next save.
	effort := meta.Effort
	if effort == "" {
		effort = m.agent.Effort
	}
	if ag, mn, pn, err := buildAgent(m.cfg, meta.Model, meta.Provider, m.sysPrompt); err == nil {
		m.agent, m.modelName, m.provName = ag, mn, pn
	} else {
		m.agent = agent.New(m.agent.Client, m.agent.Model, m.agent.MaxTokens, m.sysPrompt)
		m.agent.ModelName, m.agent.Provider = m.modelName, m.provName
		m.agent.ContextLimit = m.contextLimitFor(m.provName, m.agent.Model)
	}
	m.applyCompactModel()
	m.applyTaskModel()
	m.agent.CompactThreshold = compactThresholdFor(m.cfg)
	m.wireTasks()
	// Publish before restoring so the settled rows record against this session.
	m.agent.Tasks().SetSessionID(meta.ID)
	m.agent.SetSessionID(meta.ID)
	// Restore the session's background subagents into the dock. Everything
	// comes back settled: a process exit kills in-flight subagents, so a row
	// still "running" on disk means it died with the last exit.
	if tasks, terr := m.store.LoadTasks(meta.ID); terr == nil {
		for _, st := range tasks {
			status := agent.TaskStatus(st.Status)
			if status == agent.TaskRunning {
				status, st.Report = agent.TaskError, "interrupted — whip exited before this subagent finished"
			}
			m.agent.RestoreTask(agent.BackgroundTask{
				ID: st.ID, Description: st.Description, Prompt: st.Prompt,
				Status: status, Report: st.Report,
				StartedAt: st.StartedAt, EndedAt: st.EndedAt,
				Restored: true,
			})
		}
	} else {
		config.LogEvent("session.task", "load failed: "+terr.Error())
	}
	m.agent.Messages = append(m.agent.Messages, msgs...)
	m.agent.LoadTodosJSON(m.store.Todos(meta.ID))
	m.snapshots = m.store.Snapshots(meta.ID)
	// restore the cumulative token totals saved with the session; a row that
	// pre-dates the usage columns reads zero, so rebuild by summing the
	// per-message usage already stored on each assistant message. Either way
	// the next persist stamps the columns, so reconstruction happens once.
	in, cached, out := meta.UsageIn, meta.UsageCached, meta.UsageOut
	if in == 0 && out == 0 {
		for _, msg := range msgs {
			if msg.Usage != nil {
				in += msg.Usage.PromptTokens
				out += msg.Usage.CompletionTokens
				cached += msg.Usage.Cached()
			}
		}
	}
	if in > 0 || out > 0 {
		u := llm.Usage{PromptTokens: in, CompletionTokens: out}
		if cached > 0 {
			u.PromptTokensDetails = &struct {
				CachedTokens int `json:"cached_tokens"`
			}{CachedTokens: cached}
		}
		m.agent.SetUsage(u)
	}
	if slices.Contains(m.effortsFor(), effort) {
		m.agent.Effort = effort
	}
	m.sessionID = meta.ID
	m.sessTitle = meta.Title
	bashrun.SetMarkers(meta.ID, m.agent.Model)
	m.saved = len(m.agent.Messages)
	// Add this session's user messages to recall, skipping any already present
	// from the global cross-session seed (resume runs after that seed).
	seen := make(map[string]bool, len(m.hist))
	for _, h := range m.hist {
		seen[h] = true
	}
	for _, msg := range msgs {
		// Authored only: steered subagent reports and goal prompts are stored
		// as role "user" but were never typed — ↑ must not recall them.
		text := msg.TextContent()
		if msg.Role == "user" && msg.Authored && !seen[text] {
			seen[text] = true
			m.hist = append(m.hist, text)
		}
	}
	m.histIdx = len(m.hist)
	m.blocks = nil
	m.msgBlock = nil
	m.future = nil // a different session's tail isn't this session's redo
	m.goal = meta.Goal
	m.goalRounds = 0
	m.append(dimStyle.Render(fmt.Sprintf("resumed %s · %s · %s @ %s", meta.ID, meta.Title, m.modelName, m.provName)))
	interrupted := 0
	for _, msg := range msgs {
		if msg.Role == "tool" && strings.HasPrefix(msg.Content, "Error: tool call interrupted") {
			interrupted++
		}
	}
	if interrupted > 0 {
		m.append(dimStyle.Render(fmt.Sprintf("⚠ %d tool call(s) were interrupted when this session last ended; the model knows and can retry them.", interrupted)))
	}
	if m.goal != "" {
		m.append(dimStyle.Render("◎ goal restored — /goal resume to keep working on it"))
	}
	m.seedTranscript(msgs, 1)
	return nil
}

// seedTranscript re-renders stored messages into the viewport. Blocks are
// appended in one batch with a single refreshVP at the end: a resumed
// session costs one render pass, not one per message. base is the
// conversation index of msgs[0] (1 for full transcripts — the system prompt
// is never rendered); msgBlock is extended so rewind can map messages to
// their blocks.
func (m *model) seedTranscript(msgs []llm.Message, base int) {
	for i, msg := range msgs {
		bi := -1
		switch msg.Role {
		case "user":
			bi = len(m.blocks)
			m.blocks = append(m.blocks, block{kind: blockUser, text: linkifyFilePaths(msg.TextContent(), realFileExists)})
		case "assistant":
			if strings.TrimSpace(msg.TextContent()) != "" {
				bi = len(m.blocks)
				m.blocks = append(m.blocks, block{kind: blockAssistant, text: strings.TrimRight(msg.TextContent(), "\n")})
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
func (m *model) persist() {
	if m.store == nil {
		return
	}
	if m.sessionID == "" {
		if len(m.agent.Messages) <= m.saved {
			return // nothing new to say; don't create an empty session row
		}
		id, err := m.store.Create(cwd(), m.modelName, m.provName)
		if err != nil {
			config.LogEvent("session.save", "create failed: "+err.Error())
			m.append(errStyle.Render("session save failed: " + err.Error()))
			return
		}
		m.sessionID = id
		bashrun.SetMarkers(id, m.agent.Model)
		m.agent.Tasks().SetSessionID(id) // publish before Save so a settling subagent records
		m.agent.SetSessionID(id)         // scopes the per-session memory file
	}
	// Bookkeeping re-stamps every persist — even one with no new messages —
	// so a resume restores goal/effort, and the cumulative token totals that
	// survive a compaction rewrite of the messages.
	_ = m.store.SetGoal(m.sessionID, m.goal)
	_ = m.store.SetEffort(m.sessionID, m.agent.Effort)
	_ = m.store.SetTodos(m.sessionID, m.agent.TodosJSON())
	if u := m.agent.Usage(); u.PromptTokens > 0 || u.CompletionTokens > 0 {
		_ = m.store.SetUsage(m.sessionID, u.PromptTokens, u.Cached(), u.CompletionTokens)
	}
	if len(m.agent.Messages) <= m.saved {
		return
	}
	if err := m.store.Save(m.sessionID, m.saved, m.agent.Messages, m.modelName, m.provName); err != nil {
		config.LogEvent("session.save", "FAILED id="+m.sessionID+": "+err.Error())
		m.append(errStyle.Render("session save failed: " + err.Error()))
		return
	}
	m.saved = len(m.agent.Messages)
}

// setTheme switches the color scheme ("light"/"dark"/"auto") live and
// persists the pick to the global config: markdown re-renders under the new
// glamour style and every AdaptiveColor UI style follows lipgloss. A theme
// file change in ANOTHER running whip session is picked up live via
// syncThemeMsg.
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
func (m *model) setEffort(lv string) {
	m.agent.Effort = lv
	m.cfg.DefaultEffort = lv
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
	if m.store != nil && m.sessionID != "" {
		_ = m.store.SetEffort(m.sessionID, lv) // best-effort; persist() re-stamps
	}
}

// resetEffort applies a level without touching the global default.
func (m *model) resetEffort(lv string) {
	m.agent.Effort = lv
	if m.store != nil && m.sessionID != "" {
		_ = m.store.SetEffort(m.sessionID, lv)
	}
}

// setGoal updates the active goal and persists it with the session.
func (m *model) setGoal(goal string) {
	m.goal = goal
	m.goalRounds = 0
	if m.store != nil && m.sessionID != "" {
		_ = m.store.SetGoal(m.sessionID, goal)
	}
}

func buildAgent(cfg *config.Config, modelName, provName, sysPrompt string) (*agent.Agent, string, string, error) {
	prov, mdl, apiID, err := cfg.Resolve(modelName, provName)
	if err != nil {
		return nil, "", "", err
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if provName == "" {
		provName = cfg.DefaultProvider
		if provName == "" && len(mdl.Providers) > 0 {
			provName = mdl.Providers[0]
		}
	}
	key, keyErr := prov.ResolveKey()
	if keyErr != nil {
		return nil, "", "", keyErr
	}
	if key == "" {
		return nil, "", "", fmt.Errorf("no API key for provider %q (set apiKey/apiKeyEnv in ~/.whip/config.json)", provName)
	}
	// Two distinct limits:
	//   - ContextLimit: the input window (provider's context_length, else the
	//     config's context). Drives the header % and proactive compaction.
	//   - MaxTokens: the OUTPUT cap sent as max_tokens. Priority: config maxOut
	//     → provider's max_completion_tokens → config context (last resort).
	cat, hasCat := config.LoadCatalogs()[provName]
	ctxLimit := mdl.ContextWindow()
	if hasCat {
		if n := cat.ContextLength(apiID); n > 0 {
			ctxLimit = n
		}
	}
	maxOut := mdl.MaxOut
	if maxOut <= 0 && hasCat {
		maxOut = cat.MaxCompletionTokens(apiID)
	}
	if maxOut <= 0 {
		maxOut = ctxLimit // generous default; provider clamps if it's too high
	}
	client := llm.New(prov.BaseURL, key)
	client.MaxRetries = cfg.MaxRetries
	ag := agent.New(client, apiID, maxOut, sysPrompt)
	ag.ModelName, ag.Provider = modelName, provName
	ag.ContextLimit = ctxLimit
	ag.WorktreeSubagents = cfg.WorktreeSubagents != nil && *cfg.WorktreeSubagents
	if sp := mdl.SamplingParams; sp != nil {
		ag.Temperature, ag.TopP = sp.Temperature, sp.TopP
	}
	// Native browser subsystem: install the shared manager once; screenshots
	// steer back into the conversation as image parts on vision models.
	ag.BrowserDisabled = cfg.Browser.Enabled != nil && !*cfg.Browser.Enabled
	// Computer-use: per-app consent policy from config; the consent prompt is
	// installed below (the model never touches an unapproved app silently).
	ag.ComputerDisabled = cfg.Computer.Enabled != nil && !*cfg.Computer.Enabled
	if !ag.ComputerDisabled {
		defaultDeny := cfg.Computer.DefaultDeny != nil && *cfg.Computer.DefaultDeny // default: allow-all
		tools.ComputerPolicy = computer.NewPolicy(cfg.Computer.Allow, cfg.Computer.Deny, defaultDeny)
	}
	if !ag.BrowserDisabled && tools.Browser == nil {
		mode := browser.ModeLive
		switch cfg.Browser.Mode {
		case "dedicated":
			mode = browser.ModeDedicated
		case "headless":
			mode = browser.ModeHeadless
		case "extension":
			mode = browser.ModeExtension
		}
		tools.Browser = browser.NewManager(mode)
		if cfg.Browser.CDPURL != "" {
			_ = os.Setenv("WHIP_CDP_URL", cfg.Browser.CDPURL)
		}
		browser.AllowPrivateURLs = cfg.Browser.AllowPrivateURLs
	}
	if modelSupportsVision(cfg, modelName, apiID, config.LoadCatalogs(), provName) {
		tools.ScreenshotSink = func(jpegs [][]byte) {
			parts := make([]llm.ContentPart, 0, len(jpegs))
			for _, j := range jpegs {
				parts = append(parts, llm.ImagePart("jpg", j))
			}
			ag.SteerImages("browser_exec screenshots attached:", parts)
		}
	} else {
		tools.ScreenshotSink = nil
	}
	return ag, modelName, provName, nil
}

// blockKind classifies a transcript block so a resize can re-render it at
// the new width. Assistant text reflows through glamour (markdown); tool
// results hold raw output and expand/collapse; every other block — user
// input, tool calls, status lines — re-wraps plainly (its styling is baked
// in at append time; only the wrap changes).
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
			return indentLines(renderMarkdown(b.text, w), 3)
		}
		w := width - 2 // body indents under the "● " marker
		if w <= 0 {
			w = 80 // no terminal size yet: sane default
		}
		body := indentLines(renderMarkdown(b.text, w), 2)
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
	m.blocks = append(m.blocks, block{kind: kind, text: text})
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
	cmds := []tea.Cmd{textarea.Blink}
	if inTmuxEnv() {
		// live theme tracking: tmux knows the outer terminal's light/dark
		// (#{client_theme}, via the 996/2031 protocol) — poll it so an OS
		// appearance flip mid-session is picked up without a restart
		cmds = append(cmds, themePollTick())
	}
	if m.initialPrompt != "" {
		// Batch blink with the kickoff; the turn's p.Send is nil-safe in headless tests.
		cmds = append(cmds, func() tea.Msg { return initialPromptMsg{} })
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
	if len(m.queue) > 0 {
		chrome += len(m.queue) + 1
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
	if m.taskVP != nil {
		m.refreshTaskVP() // the task pane owns the free area; size it to fit
	}
	m.dockRows = 0
	if dock := m.tasksDock(); dock != "" { // lipgloss.Height("") is 1, not 0
		m.dockRows = lipgloss.Height(dock)
		// clicking computes task rows from the strip's top; a focused dock's
		// hint row isn't a task — skip it
		m.dockSkip = 0
		if m.tasksFocus {
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

// dockTop returns the screen row of the first TASK row in the dock: the dock
// renders below the input as the last dockRows rows above the blank + status
// line, but dockSkip non-task rows (the focused hint) sit on top of the task
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

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.layout()

	if vp, ok := msg.(viewProbe); ok { // tests read model state race-safely
		vp.fn(m)
		return m, nil
	}
	switch msg := msg.(type) {
	case initialPromptMsg:
		if m.initialPrompt == "" || m.busy {
			return m, nil
		}
		text := m.initialPrompt
		m.initialPrompt = "" // one-shot: no re-submit on replays
		m.hist = append(m.hist, text)
		m.histIdx = len(m.hist)
		return m.submit(text)

	case cfgSyncTick:
		return m.cfgSync()

	case cfgSyncMsg:
		m.applyCfgSync(msg)
		return m, nil

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		w := msg.Width
		if m.uiMode == opencodeMode {
			w -= opencodeLeftMargin // opencode's main column has a left margin
			if msg.Width >= sidebarMinWidth && !m.sidebarHide {
				w -= sidebarWidth + opencodeRightGap // reserve the sidebar and the gap before it
			}
		}
		resized := w != m.width // width change → re-wrap the whole transcript
		m.width, m.height = w, msg.Height
		// re-anchor the view position: after a resize (and on the first size
		// at startup) assume the view sits at the bottom — the next View()
		// computes viewTop = height - viewH from this sentinel.
		m.viewTop = 1 << 30
		m.input.SetWidth(w - 2)
		if resized {
			m.refreshVP() // every block re-renders at the new width (floored at minRenderWidth)
		}
		return m, nil

	case themePollMsg:
		if m.cfg.Theme != "" { // explicit pick: nothing to track, keep the tick alive
			return m, themePollTick()
		}
		return m, tea.Batch(pollClientTheme, themePollTick())

	case themeSyncMsg:
		if !msg.ok || m.cfg.Theme != "" {
			return m, nil
		}
		mdMu.Lock()
		same := mdKnown && mdLight == msg.light
		mdMu.Unlock()
		if same {
			return m, nil
		}
		// the outer terminal flipped (OS appearance change): follow it live
		SetLightTheme(msg.light)
		lipgloss.SetHasDarkBackground(!msg.light)
		bgCache = bgResult{light: msg.light, valid: true} // no RGB from the theme report
		if m.uiMode == opencodeMode {
			m.applyUIMode(opencodeMode) // re-bake input styles/spinner for the new scheme
		}
		m.refreshVP()
		word := "dark"
		if msg.light {
			word = "light"
		}
		m.append(dimStyle.Render("◐ theme: auto → " + word + " (terminal appearance changed)"))
		return m, nil

	case toastClearMsg:
		if msg.at.Equal(m.toastAt) { // a newer toast reset the timer: ignore the stale clear
			m.toast = ""
		}
		return m, nil

	case titleMsg:
		// only fill a title still at its auto placeholder (a /rename wins)
		if m.store != nil && m.sessionID != "" {
			if meta, _, err := m.store.Load(m.sessionID); err == nil {
				first := ""
				for _, msg := range m.agent.Messages {
					if msg.Role == "user" && msg.Authored {
						first = truncLine(strings.Join(strings.Fields(msg.TextContent()), " "), 64)
						break
					}
				}
				if meta.Title == first {
					_ = m.store.SetTitle(m.sessionID, msg.title)
					m.sessTitle = msg.title
					m.append(dimStyle.Render("◎ session titled: " + msg.title))
				}
			}
		}
		return m, nil

	case permRequest:
		m.permDialog = &permDialog{req: msg.req, reply: msg.reply}
		return m, nil

	case selScrollTick:
		// drag parked past the viewport edge: keep scrolling + extending the
		// selection until the drag ends or the viewport hits its limit
		return m, m.selEdgeScroll()

	case tea.KeyMsg:
		m.sel = nil // any keypress clears a finished selection highlight
		return m.key(msg)

	case tea.MouseMsg:
		// shift+click/drag must pass through so the terminal's native
		// selection (copy) works while mouse capture is on — consuming the
		// event here is what breaks drag-to-copy
		if msg.Shift {
			return m, nil
		}
		if m.msgActions != nil {
			// modal: any click closes the Message Actions dialog
			if msg.Action == tea.MouseActionPress {
				m.msgActions = nil
			}
			return m, nil
		}
		if handled, cmd := m.handleMouseSelect(msg); handled {
			return m, cmd
		}
		// opencode mode: passive motion (no button) drives the hover highlight
		if ocActive && msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone {
			m.updateHover(msg.X, msg.Y)
			return m, nil
		}
		// clicking the ⚡ control in the header cycles reasoning effort
		// (mouse Y is an absolute screen row; the header is the view's top
		// row — opencode mode has no header, so the branch must not fire)
		if m.uiMode != opencodeMode &&
			msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft &&
			msg.Y == m.viewTop && msg.X >= m.effortX {
			m.setEffort(nextEffort(m.effortsFor(), m.agent.Effort))
			return m, nil
		}
		if m.taskVP != nil {
			// the open task pane owns the free area: wheel scrolls it
			if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
				var cmd tea.Cmd
				m.taskVP.vp, cmd = m.taskVP.vp.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.picker == nil && m.mpicker == nil && m.palette == nil {
			// dock rows sit just above the input box: click selects/opens,
			// wheel scrolls the selection through the strip
			if top, n := m.dockTop(), len(m.dockTasks()); n > 0 && msg.Y >= top && msg.Y < top+n {
				if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
					m.tasksFocus = true
					if msg.Button == tea.MouseButtonWheelUp {
						m.taskSel = max(m.taskSel-1, 0)
					} else {
						m.taskSel = min(m.taskSel+1, n-1)
					}
					return m, nil
				}
				if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
					sel := m.taskSel
					if m.tasksFocus {
						sel = msg.Y - top
					}
					m.tasksFocus = true
					m.taskSel = min(sel, n-1)
					// re-fetch: the list can change between the hitbox check
					// above and this open (settled tasks age out)
					if tasks := m.dockTasks(); len(tasks) > 0 {
						m.openTask(tasks[min(m.taskSel, len(tasks)-1)].ID)
					}
					return m, nil
				}
			}
			// click on a collapsed tool result expands it (and vice versa).
			// Presses inside the block range were consumed by handleMouseSelect
			// (selection); this path is for anything that fell through.
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft &&
				msg.Y-m.viewTop > 1 && m.palette == nil {
				m.clickAt(msg.X, msg.Y)
				return m, nil
			}
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			m.follow = m.vp.AtBottom()
			return m, cmd
		}
		return m, nil

	case textMsg:
		m.flushThink() // reasoning always precedes the answer text
		m.current += string(msg)
		// Move complete lines into the transcript so the streaming area
		// only ever re-renders the last partial line.
		if i := strings.LastIndexByte(m.current, '\n'); i >= 0 {
			done := m.current[:i]
			m.current = m.current[i+1:]
			m.appendAssistant(done)
		}
		return m, nil

	case thinkMsg:
		if m.showThinking {
			if m.uiMode == opencodeMode {
				if m.thinkStart.IsZero() { // collapse reasoning to "+ Thought: {dur}" at flush
					m.thinkStart = m.nowFn()
				}
				m.ocThink += string(msg) // keep the text: the collapsed block expands to it
				return m, nil            // suppress the live reasoning render
			}
			m.flushCurrent() // thinking renders above the answer
			m.curThink += string(msg)
			if i := strings.LastIndexByte(m.curThink, '\n'); i >= 0 {
				done := m.curThink[:i]
				m.curThink = m.curThink[i+1:]
				m.appendThink(done)
			}
		}
		return m, nil

	case toolCallMsg:
		// a tool call still streaming from the model: show a dim queued row so
		// the user sees it before execution starts. onToolCall fires per args
		// delta with the cumulative snapshot, so update the row for this id in
		// place — appending per delta would stack one row per fragment.
		// toolStartMsg swaps it for the live running row (matched by id).
		row := dimStyle.Render("⋯ " + msg.name + m.batchSuffix(msg.name, msg.id) + " " + queuedSubject(msg.name, msg.args))
		if ocActive {
			row = ocToolPending(msg.name, msg.args)
		}
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockToolQueued && m.blocks[i].toolID == msg.id {
				m.blocks[i].text, m.blocks[i].stale = row, true
				m.refreshVP()
				return m, nil
			}
		}
		m.blocks = append(m.blocks, block{kind: blockToolQueued, text: row, toolID: msg.id, toolName: msg.name, toolArgs: msg.args})
		// A batch's first row queued before its siblings existed: renumber the
		// same-name rows now that the batch grew (1/3, 2/3, …). Queued rows are
		// transient — replaced on toolStartMsg — so rewriting their text is safe.
		for i := range m.blocks {
			if b := &m.blocks[i]; b.kind == blockToolQueued && b.toolName == msg.name && b.toolID != msg.id {
				b.text = dimStyle.Render("⋯ " + b.toolName + m.batchSuffix(b.toolName, b.toolID) + " " + queuedSubject(b.toolName, b.toolArgs))
				if ocActive {
					b.text = ocToolPending(b.toolName, b.toolArgs)
				}
				b.stale = true
			}
		}
		m.refreshVP()
		return m, nil

	case toolStartMsg:
		m.flushThink()
		m.flushCurrent()
		// batch suffix computed before the queued row is deleted: deleting it
		// first would shrink the same-name count by one and misnumber the
		// last-started call in a parallel batch.
		suffix := m.batchSuffix(msg.name, msg.id)
		// replace the queued row for this id (if the tool call streamed in)
		// rather than appending a second row for the same call.
		for i := range slices.Backward(m.blocks) {
			if m.blocks[i].kind == blockToolQueued && m.blocks[i].toolID == msg.id {
				m.blocks = slices.Delete(m.blocks, i, i+1)
				break
			}
		}
		args := msg.args
		switch msg.name {
		case "browser_exec", "computer_exec":
			// Surface the step label (the code's first # comment) as the row
			// text instead of raw JSON — the model writes it for the user.
			if label := browserStepLabel(msg.args); label != "" {
				args = label
			}
		case "subagent":
			// Surface the task's description as the subject instead of the raw
			// JSON blob — it names what the subagent is actually doing, matching
			// the completed row's "Subagent(description)" header.
			args = toolSubject("subagent", msg.args)
		}
		// a running row: icon + present-participle verb + full args (the
		// command being run is always fully visible). On toolEndMsg the same
		// block collapses in place to one line.
		row := toolStyle.Render("⚒ "+toolVerb(msg.name)+suffix+" ") + dimStyle.Render(args)
		if ocActive {
			row = ocToolPending(msg.name, msg.args)
		}
		m.blocks = append(m.blocks, block{kind: blockToolRun, text: row, toolID: msg.id, toolRunning: true, toolName: msg.name, toolArgs: msg.args})
		m.refreshVP()
		return m, nil

	case toolOutputMsg:
		// partial output for a running bash row: show the tail of what's
		// arrived so far under the verb line. toolEndMsg replaces it with
		// the final collapsed row, so no truncation bookkeeping here.
		for i := len(m.blocks) - 1; i >= 0; i-- {
			b := &m.blocks[i]
			if b.kind == blockToolRun && b.toolRunning && b.toolID == msg.id {
				if tail := lastLines(msg.text, 3); tail != "" {
					b.live = tail
					b.stale = true
					m.refreshVP()
				}
				break
			}
		}
		return m, nil

	case toolEndMsg:
		// collapse the matching running row in place: full args+result when
		// expanded, one dim line (red on failure) otherwise
		hdr := -1
		for i := len(m.blocks) - 1; i >= 0; i-- {
			b := &m.blocks[i]
			if b.kind == blockToolRun && b.toolRunning && b.toolID == msg.id {
				b.toolRunning = false
				b.toolFailed = strings.HasPrefix(msg.result, "Error:")
				b.live = ""
				// the completed row keeps the call visible ("Update(path)",
				// "Bash(cmd)") — the result renders in the blockTool below it
				b.text = toolHeaderRow(msg.name, b.toolArgs, b.toolFailed)
				b.stale = true
				hdr = i
				break
			}
		}
		// store the raw result DIRECTLY UNDER its call row (render collapses it
		// to a preview; ctrl+e / click expands). Appending at the end instead
		// orphaned results from their headers whenever a parallel batch was in
		// flight (three Subagent rows, then three detached result hints).
		result := block{kind: blockTool, text: msg.result}
		if hdr >= 0 && hdr+1 < len(m.blocks) {
			m.blocks = append(m.blocks[:hdr+1], append([]block{result}, m.blocks[hdr+1:]...)...)
			// the mid-slice insert shifted every index past hdr: renumber the
			// cached block indexes or rewind scrolls to (and Message Actions
			// copies from) the wrong block
			for i := range m.msgBlock {
				if m.msgBlock[i] > hdr {
					m.msgBlock[i]++
				}
			}
			if m.hoverIdx > hdr {
				m.hoverIdx++
			}
			if m.msgActions != nil && m.msgActions.block > hdr {
				m.msgActions.block++
			}
		} else {
			m.blocks = append(m.blocks, result)
		}
		m.follow = true
		m.refreshVP()
		return m, nil

	case meEditedMsg:
		if msg.err != nil {
			m.append(errStyle.Render("/me: editor failed: " + msg.err.Error()))
		} else if n := len(config.MeInstructions()); n > 0 {
			m.append(dimStyle.Render("✓ me.md saved — standing instructions updated (" + strconv.Itoa(n) + " chars)"))
		} else {
			m.append(dimStyle.Render("me.md saved — no standing instructions set (all comments)"))
		}
		return m, nil

	case interactiveStartMsg:
		// passthrough mode: route keystrokes into the PTY. The output pane is
		// shown by View(); a fresh toolStartMsg-style banner is appended so the
		// user sees "bash (interactive)" inline with the transcript.
		m.flushThink()
		m.flushCurrent()
		m.iactive = &interactive{keys: msg.keys}
		m.append(toolStyle.Render("⚒ bash ") + dimStyle.Render("(interactive — type to respond, 15s inactivity timeout)"))
		return m, nil

	case interactiveOutMsg:
		if m.iactive == nil {
			return m, nil
		}
		m.iactive.output += msg.chunk
		// any output means the command is producing, not waiting
		m.iactive.await = false
		return m, nil

	case interactiveAwaitMsg:
		if m.iactive == nil {
			return m, nil
		}
		m.iactive.await = true
		m.iactive.awaitcd = msg.secsLeft
		return m, nil

	case interactiveDoneMsg:
		if m.iactive != nil {
			// fold the streamed output + exit into the transcript as a normal
			// tool result so the session record matches the non-interactive path
			lines := strings.Split(strings.TrimRight(msg.output, "\n"), "\n")
			// cap the persisted preview like toolEndMsg, but keep the full text
			// available to the model (it's already in the tool result string)
			preview := lines
			if len(preview) > 5 {
				preview = preview[:5]
			}
			out := dimStyle.Render("  " + strings.Join(preview, "\n  "))
			if len(lines) > 5 {
				out += dimStyle.Render(fmt.Sprintf("\n  … +%d lines", len(lines)-5))
			}
			if msg.exit != "" {
				out += "\n" + dimStyle.Render("  ("+msg.exit+")")
			}
			m.append(out)
			m.iactive = nil
		}
		return m, nil

	case steeredMsg:
		m.flushThink()
		m.flushCurrent()
		m.append(youStyle.Render(glyphUser) + linkifyFilePaths(string(msg), realFileExists) + dimStyle.Render("  (steered)"))
		return m, nil

	case shellDoneMsg:
		// a `!` escape finished; its output lands behind any in-flight text
		m.flushThink()
		m.flushCurrent()
		m.applyShellDone(msg)
		return m, nil

	case goalFromContextMsg:
		// the formulation call finished between turns; on success set the
		// goal and kick off the goal loop exactly like /goal <text>
		m.flushThink()
		m.flushCurrent()
		switch {
		case errors.Is(msg.err, context.Canceled):
			m.busy = false
			m.cancel = nil
			m.append(dimStyle.Render("(interrupted)"))
		case msg.err != nil:
			m.busy = false
			m.cancel = nil
			m.append(errStyle.Render("goal-from-context failed: " + msg.err.Error()))
		case strings.TrimSpace(msg.goal) == "":
			m.busy = false
			m.cancel = nil
			m.append(errStyle.Render("goal-from-context: model returned an empty goal"))
		default:
			goal := strings.TrimSpace(msg.goal)
			m.setGoal(goal)
			m.append(dimStyle.Render("◎ goal set: " + goal))
			return m.submit(goal)
		}
		return m, nil

	case compactStartMsg:
		m.flushThink()
		m.flushCurrent()
		m.append(dimStyle.Render(fmt.Sprintf("◎ compacting %d msgs (est. %s) with %s…",
			msg.took, fmtTok(msg.est), m.compactModelLabel())))
		return m, nil

	case compactMsg:
		// compaction lands between turns: record it as an event and note it
		// inline. The raw message log stays on disk — Load derives the
		// compacted view from the event, so a bad summary is inspectable and
		// retryable (/compact retry). A live turn fires two compactMsgs per
		// compaction (OnCompact's counts, then OnCompacted's summary+cutoff);
		// only the one carrying the summary records/notes.
		m.flushThink()
		m.flushCurrent()
		switch {
		case msg.err != nil:
			m.append(errStyle.Render("compact failed: " + msg.err.Error()))
		case msg.summary == "":
			// counts-only path (no summary means no event was produced);
			// nothing to record
		default:
			recorded := false
			if m.store != nil && m.sessionID != "" {
				// the agent's cutoff is in compacted coordinates; store the raw
				// seq so Load never double-folds a summary
				if err := m.store.RecordCompaction(m.sessionID, m.rawCutoff(msg.cutoff), msg.summary); err != nil {
					config.LogEvent("session.compact", "record failed: "+err.Error())
				} else {
					recorded = true
				}
			}
			m.append(m.compactResultLine(msg))
			if recorded {
				m.future = nil   // compaction rewrote history; stale redo entries would resurrect it
				m.msgBlock = nil // indices no longer match; rebuilt as blocks stream in
				m.persist()      // append the new (compacted) rows; raw rows stay
			}
		}
		return m, nil

	case turnDoneMsg:
		m.flushThink()
		m.flushCurrent()
		m.busy = false
		m.cancel = nil
		m.interrupt1 = false
		if m.uiMode == opencodeMode && msg.err == nil && !m.turnStart.IsZero() {
			m.appendRaw(blockOCMeta, m.opencodeAttribution(m.nowFn().Sub(m.turnStart))) // ▣ mode · model · duration
		}
		m.turnStart = time.Time{}
		m.maybeTitle()
		// Cancellation arrives wrapped from the in-flight http request
		// ("Post ...: context canceled"), so identity comparison misses it —
		// which would strand the queue instead of draining it.
		canceled := errors.Is(msg.err, context.Canceled)
		if msg.err != nil && !canceled {
			m.append(errStyle.Render("error: " + msg.err.Error()))
		} else if canceled {
			m.append(dimStyle.Render("(interrupted — any running tool calls will be recorded as interrupted; whip can retry them next turn)"))
		}
		m.persist()
		switch {
		case msg.snap != "" && msg.clean:
			dropSnapshot(msg.snap) // the turn changed no files; nothing to roll back
		case msg.snap != "":
			if m.snapshots == nil {
				m.snapshots = map[int]string{}
			}
			m.snapshots[msg.at] = msg.snap
			if m.store != nil && m.sessionID != "" {
				_ = m.store.SetSnapshot(m.sessionID, msg.at, msg.snap)
			}
		}
		// A mid-turn /fork: the copy landed when the command ran; now that the
		// turn (and its persist) is done, move the live window onto the copy.
		// This precedes the queue drain and goal loop so any follow-up turns
		// continue inside the fork, not the abandoned original.
		if m.pendingForkID != "" {
			m.switchToForked(m.pendingForkID)
			m.pendingForkID = ""
			return m, nil
		}
		// codex-style follow-up: send queued messages one turn at a time;
		// `!` shell escapes execute locally instead of starting a turn.
		// A canceled turn also drains the queue: the empty-enter steer path
		// cancels intentionally so the queued messages go out immediately.
		for len(m.queue) > 0 && (msg.err == nil || canceled) {
			next := m.queue[0]
			if strings.HasPrefix(next, "!") {
				m.queue = m.queue[1:]
				m.queueSel = -1
				m.runShellQueued(next)
				continue
			}
			return m.drainQueueHead()
		}
		// goal loop: keep working until the model explicitly declares GOAL_MET
		if m.goal != "" && msg.err == nil {
			if goalMet(msg.final) {
				m.append(dimStyle.Render("◎ goal met after " + strconv.Itoa(m.goalRounds) + " round(s)"))
				m.setGoal("")
				return m, nil
			}
			if m.goalRounds >= m.goalMaxRounds() {
				m.append(errStyle.Render(fmt.Sprintf("◎ goal paused after %d rounds — /goal resume to continue, /goal clear to drop", m.goalRounds)))
				return m, nil
			}
			m.goalRounds++
			return m.submitGoal(goalContinuePrompt(m.goal))
		}
		return m, nil

	case catalogsMsg:
		m.updateCatalogs(msg)
		return m, nil

	case authResultMsg:
		m.applyAuthResult(msg)
		return m, nil

	case inferenceNetLoginMsg:
		m.applyInferenceNetLogin(msg)
		return m, nil

	case inferenceNetProjectsMsg:
		m.applyInferenceNetProjects(msg)
		return m, nil

	case inferenceNetProjectCreatedMsg:
		m.applyInferenceNetProjectCreated(msg)
		return m, nil

	case inferenceNetAuthMsg:
		m.applyInferenceNetAuth(msg)
		return m, nil

	case inferenceNetKeyMsg:
		m.applyInferenceNetKey(msg)
		return m, nil

	case noticeMsg:
		m.append(dimStyle.Render(string(msg)))
		return m, nil

	case usageMsg:
		// Turn already folds usage into the agent's session totals (header
		// reads those); keep the per-request figure — the status line shows
		// it as "last …" — and force a redraw mid-stream.
		m.lastResp = llm.Usage(msg)
		return m, nil

	case quitArmMsg:
		m.quit1 = false // the arm window closed; next ctrl+c starts fresh
		return m, nil

	case escArmMsg:
		m.esc1 = false   // the double-esc rewind window closed
		m.escClr = false // the double-esc draft-clear window closed
		return m, nil

	case taskUpdateMsg:
		// a background subagent started or settled; the dock shows it. An
		// open view of a settled task reloads from the stored report.
		if m.taskVP != nil {
			if t, ok := m.agent.Tasks().Get(m.taskVP.id); ok && t.Status != agent.TaskRunning && m.taskVP.live {
				m.openTask(m.taskVP.id) // reseed with the final report
			} else {
				m.refreshTaskVP()
			}
		}
		return m, nil

	case orphanSteerMsg:
		// A steer arrived after the drain snapshot at turn teardown and was
		// orphaned. The agent can't message the TUI itself (same seam as
		// waitWakeMsg) — it surfaced the steer through OnOrphanedSteer; run it
		// as a machine turn so the steer is never lost.
		if !m.busy {
			return m.submitTurn(string(msg), true)
		}
		return m, nil

	case waitWakeMsg:
		// An idle wait fired: wake as a machine-authored turn (the opencode/
		// exo wake pattern). If a turn started between the wait's TurnRunning
		// check and this message, steer into the live turn instead of
		// double-submitting — a steer parks until the next loop boundary.
		m.append(dimStyle.Render("⏲ " + firstLine(string(msg))))
		if m.busy {
			m.agent.Steer(string(msg))
			return m, nil
		}
		return m.submitTurn(string(msg), false)

	case mcpStatusMsg:
		// An MCP server changed state. Announce each server's FIRST settle in
		// the transcript (one line, once per session per server) so arrivals
		// and failures are visible without typing /mcp — later transitions
		// (auto-reconnect, toggles) stay quiet to avoid flapping noise. An open
		// MCPs palette panel rebuilds its rows so a background settle ("◌
		// connecting…" → "● N tools") shows without the user re-opening it.
		if m.mcpMgr != nil {
			if m.palette != nil {
				if pp := m.palette.top(); pp != nil && pp.kind == panelMCP {
					pp.mcps = m.buildMCPRows()
					if pp.midx >= len(pp.mcps) {
						pp.midx = len(pp.mcps) - 1
					}
				}
			}
			if m.mcpSeen == nil {
				m.mcpSeen = map[string]bool{}
			}
			for _, srv := range m.mcpMgr.Statuses() {
				if m.mcpSeen[srv.Name] || srv.Status == mcp.StatusConnecting {
					continue
				}
				m.mcpSeen[srv.Name] = true
				switch srv.Status {
				case mcp.StatusReady:
					m.append(dimStyle.Render(fmt.Sprintf("⚡ mcp: %s ready (%d tools)", srv.Name, srv.Tools)))
				case mcp.StatusFailed:
					line := fmt.Sprintf("✗ mcp: %s failed: %s", srv.Name, srv.Err)
					if srv.Source != "" {
						line += " (" + srv.Source + ")"
					}
					m.append(errStyle.Render(line + fmt.Sprintf(" (/mcp %s reconnect)", srv.Name)))
				case mcp.StatusDisabled:
					m.append(dimStyle.Render(fmt.Sprintf("○ mcp: %s disabled", srv.Name)))
				}
			}
		}
		return m, nil

	case taskEventMsg:
		// one live event from the open task's subagent stream; append it to
		// the pane's transcript (deltas coalesce into lines before append)
		tv := m.taskVP
		if tv == nil || msg.id != tv.id {
			return m, nil
		}
		if msg.kind == 4 { // follow-up turn settled; unlock the chat input
			tv.busy, tv.followCancel = false, nil
		}
		renderTaskEvent(&tv.buf, msg.kind, msg.s, msg.s2)
		m.refreshTaskVP()
		return m, nil

	case imageMsg:
		switch {
		case msg.err != nil:
			m.append(errStyle.Render("image paste failed: " + msg.err.Error()))
		case msg.path == "":
			m.append(dimStyle.Render("(no image on clipboard)"))
		default:
			m.input.InsertString("@" + msg.path + " ")
			m.refreshMenu()
		}
		return m, nil

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case scheduleTickMsg:
		return m, tea.Batch(scheduleTick(), m.fireDueSchedules())
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// interactive passthrough: forward keystrokes to the child's PTY instead
	// of editing the input box. ctrl+c ctrl+c breaks out (cancel), esc forwards
	// a single esc to the child (many prompts use esc to cancel).
	if m.iactive != nil {
		return m.iactiveKey(msg)
	}
	if m.permDialog != nil {
		m.permKey(msg)
		return m, nil
	}
	if m.msgActions != nil {
		return m.msgActionsKey(msg)
	}
	if m.palette != nil {
		return m.paletteKey(msg)
	}
	if m.rew != nil {
		return m.rewindKey(msg)
	}
	if m.picker != nil {
		return m.pickerKey(msg)
	}
	if m.mpicker != nil {
		return m.modelPickerKey(msg)
	}
	// newline keys (ctrl+j / shift+enter / alt+enter) never submit; they go
	// straight to the textarea, which splits the line via InsertNewline.
	// Note: KeyCtrlM is NOT here — it shares KeyEnter's byte (CR=13), so
	// matching it would swallow every real enter keypress. ctrl+j (LF=10),
	// alt+enter, and the shift+enter escape sequences are all distinguishable.
	if msg.Type == tea.KeyCtrlJ ||
		(msg.Type == tea.KeyEnter && msg.Alt) ||
		(msg.Type == tea.KeyRunes && msg.Alt && string(msg.Runes) == "\r") ||
		isShiftEnterSeq(msg) {
		// bubbles gates InsertNewline on MaxHeight, treating the visual cap as
		// a content limit — after a paste reaches MaxHeight lines every ctrl+j
		// would be silently swallowed. Lift the cap for this one call so the
		// newline always lands (and the textarea's own repositionView scrolls
		// the new line into view), then reapply the visual cap via SetHeight,
		// which clamps rendering only, never content.
		maxHeight := m.input.MaxHeight
		m.input.MaxHeight = 0
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
		m.input.MaxHeight = maxHeight
		m.input.SetHeight(maxHeight)
		// bubbles' InsertNewline scrolls the internal viewport to follow the
		// cursor while the box is still 1 line high (YOffset=1); the deferred
		// growInput rebuild inherits that stale offset and the first line
		// scrolls out of view. SetValue resets the scroll (Reset inside), and
		// CursorEnd keeps the caret at the end of the input.
		v := m.input.Value()
		m.input.SetValue(v)
		m.input.CursorEnd()
		m.refreshMenu()
		return m, cmd
	}
	// an open task detail view owns the keyboard until esc backs out of it
	if m.taskVP != nil {
		return m.taskViewKey(msg)
	}
	// opencode leader chords: ctrl+x arms a 2s window, the next key dispatches
	// (ctrl+x m model list, l sessions, n new, b sidebar, t theme, c compact,
	// g rewind, y copy last assistant message); esc clears the pending chord.
	// Sits BELOW the task-pane dispatch: ctrl+x there cancels the task.
	if m.uiMode == opencodeMode {
		if !m.leaderAt.IsZero() && m.nowFn().Sub(m.leaderAt) < 2*time.Second {
			m.leaderAt = time.Time{}
			if msg.String() == "esc" {
				return m, nil
			}
			if mod, cmd, ok := m.ocLeaderChord(msg.String()); ok {
				return mod, cmd
			}
			// unknown chord key: fall through as a normal keypress
		} else if msg.String() == "ctrl+x" {
			m.leaderAt = m.nowFn()
			return m, nil
		}
	}
	// Paste collapse (opt-in via config collapsePaste): a multi-line bracketed
	// paste lands as a [Pasted ~N lines] placeholder in the input instead of
	// spraying the textarea; the real text is held in pasteBuf and swapped in
	// at submit. Off by default — a paste you can't see is a paste you can't
	// trust.
	if msg.Paste {
		if path, ok := pastedImagePath(string(msg.Runes)); ok {
			// A macOS screenshot preview pastes a temporary file path. Copy it
			// off the UI thread before the preview cleans the file up.
			return m, func() tea.Msg { return pasteImageFileCmd(path) }
		}
	}
	if msg.Paste && m.cfg != nil && m.cfg.CollapsePaste != nil && *m.cfg.CollapsePaste {
		if n := strings.Count(string(msg.Runes), "\n"); n >= 2 {
			m.pasteBuf = string(msg.Runes)
			m.input.SetValue(m.input.Value() + fmt.Sprintf("[Pasted ~%d lines]", n+1))
			m.input.CursorEnd()
			m.growInput()
			return m, nil
		}
	}

	switch msg.Type {
	case tea.KeyCtrlT:
		// focus the tasks dock (or unfocus it) — the persistent strip below
		// the input listing background subagents (↓ on an empty input works too)
		if len(m.dockTasks()) == 0 {
			return m, nil
		}
		m.tasksFocus = !m.tasksFocus
		m.clampTaskSel()
		return m, nil
	case tea.KeyCtrlC:
		if m.busy && m.cancel != nil {
			// explicit interruption: first press arms, second cancels
			// ponytail: no reset timer; the flag clears on turn end
			if !m.interrupt1 {
				m.interrupt1 = true
				return m, nil
			}
			m.cancel()
			return m, nil
		}
		// idle: two presses within a short window quit, so a stray ctrl+c
		// can't nuke the session. First press arms + hints; second quits.
		if m.quit1 {
			m.quit1 = false
			return m, tea.Quit
		}
		m.quit1 = true
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return quitArmMsg{} })

	case tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		m.follow = m.vp.AtBottom()
		return m, cmd

	case tea.KeyEsc:
		// esc interrupts the agent mid-response — UNLESS there's a draft in
		// the input box: clearing the draft takes priority so esc stays
		// predictable (it always edits YOUR text first), and the agent keeps
		// running untouched.
		if m.busy && m.cancel != nil && strings.TrimSpace(m.input.Value()) == "" {
			m.cancel()
			return m, nil
		}
		// Dismissing UI takes priority and only arms the window.
		dismissed := true
		switch {
		case m.namePrompt != nil: // cancel the inline fork/rename/auth prompt
			masked := m.namePrompt.mask
			m.closeNamePrompt()
			if masked { // the draft stash must not record a key into history
				m.escClr = false
				return m, nil
			}
		case m.menu != nil:
			if m.menu.cyc { // tab cycling previewed candidates: revert the input
				m.input.SetValue(m.menu.base)
			}
			m.menu = nil
		case m.queueSel >= 0: // leave queue navigation
			m.queueSel = -1
		// NOTE: dock focus deliberately does NOT consume esc — esc stays the
		// interrupt/rewind key. Leave the dock with ↑ past its top row (it
		// sits below the input), ctrl+t, or just typing.
		default:
			dismissed = false
		}
		if !dismissed {
			// A typed draft: double-esc clears it into the input history (not
			// the chat history — it's recallable with ↑ in case it was an
			// accident). The rewind picker never arms while a draft exists.
			if strings.TrimSpace(m.input.Value()) != "" {
				if m.escClr {
					m.escClr = false
					m.hist = append(m.hist, strings.TrimSpace(m.input.Value()))
					m.histIdx = len(m.hist)
					m.input.Reset()
					m.append(dimStyle.Render("draft cleared — ↑ recalls it"))
					return m, nil
				}
				m.escClr = true
				return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return escArmMsg{} })
			}
			// No draft: a second esc within a second opens the rewind picker —
			// scroll the history, jump back (or forward again after a rewind).
			if m.esc1 {
				m.esc1 = false
				m.openRewind()
				return m, nil
			}
			m.esc1 = true
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return escArmMsg{} })
		}
		m.esc1 = false   // a dismissal consumed the press; no stale arm carries over
		m.escClr = false // same for the draft-clear arm
		return m, nil

	case tea.KeyCtrlV:
		// image on the clipboard? save it and @-mention the file; otherwise
		// let the textarea do its usual text paste
		return m, pasteImageCmd

	case tea.KeyCtrlE:
		// expand/collapse the most recent tool result block
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool {
				m.blocks[i].toggle()
				m.refreshVP()
				return m, nil
			}
		}
		return m, nil

	case tea.KeyCtrlO:
		// toggle rendering of reasoning/thinking tokens
		m.toggleThinking()
		return m, nil

	case tea.KeyCtrlK:
		// clear the conversation, exactly as if /clear ran. Intercepted here
		// because the textarea's default KeyMap claims ctrl+k for
		// delete-after-cursor (newInput disables that binding).
		return m.command("/clear")

	case tea.KeyTab:
		// completion menu: tab/shift+tab cycle the selection WITH preview —
		// each step inserts the highlighted candidate (a single match just
		// completes), enter commits, esc dismisses and reverts the input.
		if m.menu != nil {
			m.menuCycle(1)
			return m, nil
		}
		m.openMenu()
		return m, nil

	case tea.KeyDown, tea.KeyCtrlN:
		if m.menu != nil {
			m.menu.idx = (m.menu.idx + 1) % len(m.menu.cands)
			return m, nil
		}
		if m.tasksFocus {
			m.taskSel = min(m.taskSel+1, len(m.dockTasks())-1)
			return m, nil
		}
		// while busy with a queue and an empty input, ↓ moves the queue
		// selection toward newer messages (and off the end to deselect)
		if m.busy && len(m.queue) > 0 && m.input.Value() == "" {
			if m.queueSel >= 0 {
				m.queueSel++
				if m.queueSel >= len(m.queue) {
					m.queueSel = -1
				}
			}
			return m, nil
		}
		// empty input + a visible dock below it: ↓ moves focus into the
		// subagent list (↑ from its top row hands focus back)
		if m.input.Value() == "" && len(m.dockTasks()) > 0 {
			m.tasksFocus = true
			m.taskSel = 0
			return m, nil
		}
		// move within the textarea unless the cursor already sits on the
		// last (soft-wrapped) row, where ↓ falls through to history recall
		if !m.cursorOnLastLine() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		m.histNext()
		return m, nil

	case tea.KeyShiftTab:
		if m.menu != nil {
			m.menuCycle(-1)
			return m, nil
		}
		return m, nil

	case tea.KeyUp, tea.KeyCtrlP:
		if m.menu != nil {
			m.menu.idx = (m.menu.idx + len(m.menu.cands) - 1) % len(m.menu.cands)
			return m, nil
		}
		if m.tasksFocus {
			if m.taskSel == 0 { // the dock sits below the input: ↑ off its top row hands focus back
				m.tasksFocus = false
				return m, nil
			}
			m.taskSel--
			return m, nil
		}
		// while busy with a queue and an empty input, ↑ selects queued messages
		if m.busy && len(m.queue) > 0 && m.input.Value() == "" &&
			(msg.Type == tea.KeyUp || msg.Type == tea.KeyShiftTab) {
			if m.queueSel < 0 {
				m.queueSel = len(m.queue) - 1 // start at the newest
			} else if m.queueSel > 0 {
				m.queueSel--
			}
			return m, nil
		}
		// move within the textarea unless the cursor already sits on the
		// first (soft-wrapped) row, where ↑ falls through to history recall.
		// Holding ↑ auto-repeats at 30–80ms; a user who keeps holding past
		// the top is trying to reach the start of THIS message, not to
		// machine-gun through history — suppress the rollover while repeats
		// keep arriving, and only recall after a deliberate pause.
		if msg.Type == tea.KeyUp && !m.cursorOnFirstLine() {
			m.lastUp = m.nowFn()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if msg.Type == tea.KeyUp && m.nowFn().Sub(m.lastUp) < 300*time.Millisecond {
			m.lastUp = m.nowFn()
			return m, nil
		}
		m.lastUp = m.nowFn()
		if msg.Type == tea.KeyCtrlP { // command palette (opencode-style modal)
			m.openPalette()
			return m, nil
		}
		m.histPrev()
		return m, nil

	case tea.KeyDelete, tea.KeyBackspace:
		// delete the selected queued message (only when navigating the queue)
		if m.busy && m.queueSel >= 0 && m.queueSel < len(m.queue) {
			m.queue = append(m.queue[:m.queueSel], m.queue[m.queueSel+1:]...)
			if m.queueSel >= len(m.queue) {
				m.queueSel = len(m.queue) - 1
			}
			if len(m.queue) == 0 {
				m.queueSel = -1
			}
			return m, nil
		}
		// not navigating the queue: fall through to normal editing
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.refreshMenu()
		return m, cmd

	case tea.KeyEnter:
		if m.namePrompt != nil { // inline prompt (fork naming, /rename) commits
			onOK := m.namePrompt.onOK
			value := strings.TrimSpace(m.input.Value())
			m.closeNamePrompt() // restores the draft before onOK appends blocks
			onOK(value)
			return m, nil
		}
		if m.menu != nil {
			c := m.menu.cands[m.menu.idx]
			// a bare command previewed by tab cycling runs immediately, same
			// as picking it with arrows + enter (one-keystroke palette)
			if m.menu.cyc && m.menu.head == "" && execNow[c.Text] {
				m.menu = nil
				m.input.Reset()
				return m.command(c.Text)
			}
			// tab cycling already inserted the candidate: commit it; otherwise
			// insert it now (directories stay open for deeper completion)
			if m.menu.cyc {
				m.acceptPreview()
				return m, nil
			}
			// bare commands that act without further args run immediately
			if m.menu.head == "" && execNow[c.Text] {
				m.menu = nil
				m.input.Reset()
				return m.command(c.Text)
			}
			if m.accept() {
				return m, nil // completed something; next enter submits
			}
			// selection was already fully typed — fall through to submit
		}
		if m.tasksFocus { // open the selected task's detail view
			m.tasksFocus = false
			// settled tasks linger in the dock until the user sends a new
			// message, so the strip is stable between the last paint and this
			// keypress; the list can still be empty (or smaller than taskSel)
			if tasks := m.dockTasks(); len(tasks) > 0 {
				m.openTask(tasks[min(m.taskSel, len(tasks)-1)].ID)
			}
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		// a collapsed paste swaps its real text back in at submit
		if m.pasteBuf != "" {
			text = strings.Replace(text, strings.TrimSpace(fmt.Sprintf("[Pasted ~%d lines]", strings.Count(m.pasteBuf, "\n")+1)), strings.TrimSpace(m.pasteBuf), 1)
			m.pasteBuf = ""
		}
		if m.busy {
			switch {
			// settings commands don't touch the turn — run them now instead of
			// queueing them as messages for the model
			case text != "" && busyCmd(text):
				if !strings.HasPrefix(text, "/auth ") { // keys stay out of ↑-recallable history
					m.hist = append(m.hist, text)
					m.histIdx = len(m.hist)
				}
				m.input.Reset()
				m.menu = nil
				return m.command(text)
			case strings.HasPrefix(text, "!"): // shell escape runs now, not queued
				m.hist = append(m.hist, text)
				m.histIdx = len(m.hist)
				m.input.Reset()
				m.menu = nil
				m.runShell(text)
			case text != "" && m.agent.WaitingOnSubagents():
				// The turn is blocked only on subagents — steer the message in
				// as a mid-turn correction instead of queueing it behind the
				// whole turn (it isn't an interruption if the agent is just
				// waiting). Echo it like a steered background-task report.
				m.hist = append(m.hist, text)
				m.histIdx = len(m.hist)
				m.input.Reset()
				m.menu = nil
				m.agent.Steer(text)
				m.append(youStyle.Render("❯ ") + linkifyFilePaths(text, realFileExists) + dimStyle.Render("  (steered)"))
			case text != "": // codex-style: queue it (multiple allowed)
				m.queue = append(m.queue, text)
				m.hist = append(m.hist, text)
				m.histIdx = len(m.hist)
				m.input.Reset()
				m.menu = nil
			case len(m.queue) > 0: // grok-style: empty enter force-steers the queue
				// Interrupt the current generation so the queued messages
				// go out as the next turn immediately, not after the model
				// finishes whatever it's currently generating.
				if m.cancel != nil {
					m.cancel()
				}
			}
			return m, nil
		}
		if text == "" && len(m.queue) > 0 {
			// recovery: a turn that ended without draining (e.g. a wrapped
			// cancellation slipping past turnDoneMsg's check) leaves the
			// queue stranded; empty enter while idle sends the head now
			return m.drainQueueHead()
		}
		if text == "" {
			return m, nil
		}
		m.input.Reset()
		m.menu = nil
		// /auth with an inline key is kept out of input history: the key would
		// otherwise be ↑-recallable and rendered in the clear. The masked
		// prompt (bare /auth) is the recommended path.
		if !strings.HasPrefix(text, "/auth ") {
			m.hist = append(m.hist, text)
			m.histIdx = len(m.hist)
		}
		m.draft = ""
		if strings.HasPrefix(text, "/") {
			return m.command(text)
		}
		if strings.HasPrefix(text, "!") {
			m.runShell(text)
			return m, nil
		}
		return m.submit(text)
	}

	// Typing hands focus back from the dock to the input implicitly — without
	// this, enter after typing would open the selected task, not submit.
	m.tasksFocus = false
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshMenu()
	return m, cmd
}

// shiftEnterRe matches the common shift+enter encodings bubbletea doesn't map
// to a named key: CSI u (\x1b[13;2u), modifyOtherKeys (\x1b[27;2;13~), and
// kitty's shifted CR (\x1b[57441u). KeyMsg.String() renders each byte of
// unknown sequences quoted and comma-separated (digits as words), so we match
// the rendered form loosely.
var shiftEnterRe = regexp.MustCompile(
	`'\[', '1', '3', ';', '2', 'u'` + // CSI 13;2u
		`|'\[', '2', '7', ';', '2', ';', '1', '3', '~'` + // CSI 27;2;13~
		`|'\[', 'five', 'seven', 'four', 'four', 'one', 'u'`,
) // CSI 57441u

// isShiftEnterSeq reports whether msg is a shift+enter sequence bubbletea
// surfaced as an unknown/unmapped key.
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
	if u := m.agent.Usage(); u.PromptTokens > 0 || u.CompletionTokens > 0 {
		stats += fmt.Sprintf(" · %s tok", fmtTok(u.PromptTokens+u.CompletionTokens))
	}
	if m.agent.ContextLimit > 0 {
		stats += fmt.Sprintf(" · %d%%", agent.EstimateTokens(m.agent.Messages)*100/m.agent.ContextLimit)
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
	in, out, cacheRead, ok := cat.Pricing(m.agent.Model)
	if !ok {
		return 0, false
	}
	return llm.SessionCost(m.agent.Usage(), in, out, cacheRead), true
}

// compactThresholdFor converts the config's compactPct preference into the
// agent's threshold fraction. Out-of-range values clamp to [10, 90]; 0 (unset)
// means the built-in default.
func compactThresholdFor(cfg *config.Config) float64 {
	pct := cfg.CompactPct
	if pct == 0 {
		pct = config.DefaultCompactPct
	}
	return float64(min(max(pct, 10), 90)) / 100
}

// applyCompactModel points the agent's compaction summary call at the
// configured compaction model/provider. An empty m.compactModel means the
// built-in default (config.DefaultCompactModel); when it isn't in the user's
// config — or a picked entry is bad or unreachable — the override clears and
// compaction falls back to the conversation's own model.
func (m *model) applyCompactModel() {
	m.agent.CompactClient, m.agent.CompactModel = nil, ""
	cm := m.compactModel
	if cm == "" {
		cm = config.DefaultCompactModel
	}
	prov, _, apiID, err := m.cfg.Resolve(cm, m.compactProv)
	if err != nil {
		if m.compactModel != "" { // a picked model failing is worth a note; a missing default isn't
			m.append(errStyle.Render("compaction model: " + err.Error() + " — using current model"))
		}
		return
	}
	key, keyErr := prov.ResolveKey()
	if keyErr == nil && key != "" {
		m.agent.CompactClient = llm.New(prov.BaseURL, key)
		m.agent.CompactClient.MaxRetries = m.cfg.MaxRetries
		m.agent.CompactModel = apiID
	} else if m.compactModel != "" {
		if keyErr != nil {
			m.append(errStyle.Render("compaction model: " + keyErr.Error() + " — using current model"))
		} else {
			m.append(errStyle.Render("compaction model: no API key — using current model"))
		}
	}
}

// wireTasks makes the active agent's background-task registry nudge the UI on
// every start/settle. OnChange runs on the worker goroutine, so it only sends
// a message (never touches UI state directly).
func (m *model) wireTasks() {
	// Persist every start/settle to the session store so --resume can restore
	// the dock. Headless-safe (no prog needed). The session id comes in as an
	// argument — published via SetSessionID — so this worker-goroutine
	// callback never races the UI goroutine reading m.sessionID.
	st := m.store
	m.agent.Tasks().OnRecord = func(sessionID string, t *agent.BackgroundTask) {
		if st == nil || sessionID == "" {
			return // no session row yet; the settle's OnRecord will land after one exists
		}
		if err := st.SaveTask(sessionID, session.Task{
			ID: t.ID, Description: t.Description, Prompt: t.Prompt,
			Status: string(t.Status), Report: t.Report,
			StartedAt: t.StartedAt, EndedAt: t.EndedAt,
		}); err != nil {
			config.LogEvent("session.task", "save failed: "+err.Error())
		}
		// Persist the subagent's full transcript as its own attributed session
		// (id <parent>/task/<id>) once it settles — start rows carry no
		// transcript yet, so only settled tasks with a live sub have one.
		// Attribute it to the sub's own route (t.SubModel): a model-overridden
		// subagent must not be recorded under the parent's model/provider.
		if t.Status != agent.TaskRunning && t.SubMessages != nil {
			if _, err := st.SaveSubagentTranscript(sessionID, t.ID, t.SubMessages, t.SubModel, ""); err != nil {
				config.LogEvent("session.task", "transcript save failed: "+err.Error())
			}
		}
	}
	m.agent.Tasks().SetSessionID(m.sessionID)
	m.agent.SetSessionID(m.sessionID)
	m.wireWaits()
	if m.prog == nil {
		return // headless (tests)
	}
	m.agent.OnOrphanedSteer = func(text string) {
		// Detached: runs on the wait-poller goroutine; a backed-up UI queue
		// must never stall the agent (same posture as OnChange below).
		go m.prog.Send(orphanSteerMsg(text))
	}
	m.agent.Tasks().OnChange = func(*agent.BackgroundTask) {
		// Detached: OnChange runs on the subagent worker goroutine, and a
		// backed-up UI queue must never stall the agent (see sendTaskMsg).
		go m.prog.Send(taskUpdateMsg{})
	}
	// Point the MCP manager at the NEW agent — resume/model-switch replace
	// m.agent wholesale, and the OnChange closure captures the model, not a
	// specific agent, precisely so this handoff works.
	if m.mcpMgr != nil {
		m.agent.SetMCPTools(m.mcpMgr.Tools())
	}
}

// wireWaits points the active agent's wait registry at this UI's wake hook:
// an idle wait firing submits a machine-authored turn (the opencode/exo
// pattern — whip's Steer only reaches a RUNNING turn, so idle delivery needs
// the wake). Called from wireTasks so every agent swap re-installs it. The
// hook runs on the wait's poller goroutine, so it only sends a message.
func (m *model) wireWaits() {
	if m.prog == nil {
		return // headless (tests)
	}
	m.agent.Waits().OnWake = func(text string) {
		go m.prog.Send(waitWakeMsg(text)) // detached, same rule as OnChange
	}
}

// runningTasks counts background subagents still in flight (for the header badge).
func (m *model) runningTasks() int {
	n := 0
	for _, t := range m.agent.Tasks().List() {
		if t.Status == agent.TaskRunning {
			n++
		}
	}
	return n
}

// tasksView renders the background-subagent list for /tasks.
func (m *model) tasksView() string {
	tasks := m.agent.Tasks().List()
	if len(tasks) == 0 {
		return dimStyle.Render("(no background subagents)")
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render(fmt.Sprintf("background subagents (%d):", len(tasks))))
	for _, t := range tasks {
		icon := "⏳"
		switch t.Status {
		case agent.TaskDone:
			icon = "✓"
		case agent.TaskError, agent.TaskCancelled:
			icon = "✗"
		}
		line := fmt.Sprintf("  %s %s  %s", icon, t.ID, t.Description)
		if t.Restored {
			line += dimStyle.Render("  (restored)")
		}
		if t.Status == agent.TaskRunning {
			line += dimStyle.Render(fmt.Sprintf("  (%ds)", int(time.Since(t.StartedAt).Seconds())))
		}
		b.WriteString("\n" + toolStyle.Render(line))
		if t.Status != agent.TaskRunning {
			report := t.Report
			if len(report) > 200 {
				report = report[:200] + "…"
			}
			b.WriteString("\n" + dimStyle.Render("      "+strings.ReplaceAll(report, "\n", " ")))
		}
	}
	return b.String()
}

// switchModel rebuilds the agent on a new model/provider, carrying history.
// persist=false (/model-for-session) leaves the saved default untouched, so the
// next whip launch still opens on the configured model.
func (m *model) switchModel(name, prov string, persist bool) {
	ag, mn, pn, err := buildAgent(m.cfg, name, prov, m.sysPrompt)
	if err != nil {
		m.append(errStyle.Render(err.Error()))
		return
	}
	ag.Effort = m.agent.Effort
	ag.Messages = append(ag.Messages, m.agent.Messages[1:]...) // carry history
	ag.CompactClient, ag.CompactModel = m.agent.CompactClient, m.agent.CompactModel
	ag.CompactThreshold = m.agent.CompactThreshold
	m.agent, m.modelName, m.provName = ag, mn, pn
	m.applyTaskModel()
	m.wireTasks()
	if !slices.Contains(m.effortsFor(), ag.Effort) {
		m.resetEffort("") // the new model doesn't support the current level
	}
	if persist {
		m.cfg.DefaultModel, m.cfg.DefaultProvider = mn, pn // store the switch as the new default
		if err := m.cfg.Save(); err != nil {
			m.append(errStyle.Render("config save failed: " + err.Error()))
		}
		m.append(dimStyle.Render("→ " + mn + " @ " + pn))
	} else {
		m.append(dimStyle.Render("→ " + mn + " @ " + pn + " (this session only)"))
	}
}

// pickerKey handles keys while the /resume browser is open.
func (m *model) pickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.picker = nil
	case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab: // older sessions sit above
		if p.idx < len(p.metas)-1 {
			p.idx++
			p.loadPreview(m.store)
		}
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab: // newer sessions sit below
		if p.idx > 0 {
			p.idx--
			p.loadPreview(m.store)
		}
	case tea.KeyEnter:
		id := p.metas[p.idx].ID
		m.picker = nil
		if err := m.resume(id); err != nil {
			m.append(errStyle.Render(err.Error()))
		}
	}
	return m, nil
}

func (p *picker) loadPreview(store *session.Store) {
	id := p.metas[p.idx].ID
	if _, ok := p.previews[id]; !ok {
		u, a := store.LastExchange(id)
		p.previews[id] = [2]string{u, a}
	}
}

// openPicker starts the /resume browser on recent sessions.
func (m *model) openPicker() {
	if m.store == nil {
		m.append(errStyle.Render("session store unavailable"))
		return
	}
	metas, err := m.store.Recent(50)
	if err != nil {
		m.append(errStyle.Render(err.Error()))
		return
	}
	if len(metas) == 0 {
		m.append(dimStyle.Render("(no previous sessions)"))
		return
	}
	m.picker = &picker{metas: metas, previews: map[string][2]string{}}
	m.picker.loadPreview(m.store)
}

// openMenu starts tab completion: every candidate for the token's prefix is
// frozen into a cycle set and the first is previewed, so tab always inserts
// text — a single match completes outright, several cycle with preview.
func (m *model) openMenu() {
	head, cands := completions(m.input.Value(), m.modelCands(), m.providerCands(), m.skillCands(), effortCandsFor(m.effortsFor()))
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
		head, cands := completions(val, m.modelCands(), m.providerCands(), m.skillCands(), effortCandsFor(m.effortsFor()))
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
	sk := skills.Scan(skills.DefaultDirs()...)
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

// prepareTurn refreshes the system prompt's skills block (so new skills load
// without a restart) and MCP server instructions (so late-arriving servers
// teach the model how to use their tools), then expands $skill / @file
// tokens in the input. It returns the expanded text plus any image parts
// extracted from @image tags.
func (m *model) prepareTurn(text string) (string, []llm.ContentPart) {
	sk := skills.Scan(skills.DefaultDirs()...)
	sys := m.sysPrompt + skills.PromptBlock(sk)
	if m.mcpMgr != nil {
		sys += m.mcpMgr.InstructionsBlock()
	}
	sys += memory.PromptBlock(memory.Installation(), memory.Session(m.sessionID))
	m.agent.Messages[0].Content = sys
	expanded := expandMentions(expandSkills(text, sk))
	if !m.supportsVision() {
		// text-only model: leave @image tags as pointer notes (from
		// expandMentions) instead of inlining base64 the model would reject.
		return expanded, nil
	}
	parts, withNote := imageParts(text)
	return expanded + withNote, parts
}

// supportsVision reports whether the current model accepts image inputs, so
// @image tags are inlined only for models that can use them. A provider-
// advertised input_modalities entry (from /models, cached in the catalog)
// wins; otherwise the config's per-model vision flag decides (default false).
func (m *model) supportsVision() bool {
	return modelSupportsVision(m.cfg, m.modelName, m.agent.Model, m.catalogs, m.provName)
}

// modelSupportsVision is supportsVision lifted off the TUI model so
// buildAgent (which runs before the model exists) can gate the screenshot
// sink the same way.
func modelSupportsVision(cfg *config.Config, modelName, modelID string, catalogs map[string]config.Catalog, provName string) bool {
	if cat, ok := catalogs[provName]; ok {
		if vision, found := cat.SupportsVision(modelID); found {
			return vision
		}
	}
	if cfg != nil {
		if mc, ok := cfg.Models[modelName]; ok {
			return mc.Vision
		}
	}
	return false
}

// appendAssistant writes assistant text into the transcript, rendering it as
// markdown (glamour) and prefixing the first line of each segment with "● ".
// A rendered segment can reflow to a different height than the raw text, so
// the whole segment lands as one block.
// appendAssistant finalizes an in-flight assistant segment into the
// transcript as raw markdown (rendered at the current width in refreshVP).
// Consecutive segments of one message merge into a single block so the whole
// message re-renders as one markdown document on resize.
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
func (m *model) drainQueueHead() (tea.Model, tea.Cmd) {
	next := m.queue[0]
	m.queue = m.queue[1:]
	m.queueSel = -1
	m.hist = append(m.hist, next)
	m.histIdx = len(m.hist)
	return m.submit(next)
}

func (m *model) submit(text string) (tea.Model, tea.Cmd) {
	return m.submitTurn(text, true)
}

// submitGoal sends a whip-injected goal-continuation; not a typed submission,
// so it must not appear in up-arrow input history.
func (m *model) submitGoal(text string) (tea.Model, tea.Cmd) {
	return m.submitTurn(text, false)
}

func (m *model) submitTurn(text string, authored bool) (tea.Model, tea.Cmd) {
	m.busy = true
	m.turnStart = m.nowFn()
	prepared, parts := m.prepareTurn(text)
	userMsgIdx := len(m.agent.Messages) // where Turn will append this message
	// Snapshot the pre-turn workspace so a rewind past this turn restores the
	// files it is about to change. "" = not a git repo; a clean tree still
	// snapshots here (as HEAD) — turnDone drops it if the turn changed nothing.
	preSnap := snapshotWorkspace()
	// Rewind bookkeeping: if a redo stack exists, this resubmission replaces a
	// clipped message. Record the replaced text on the new message (internal,
	// stripped before the provider) before discardFuture drops the stack.
	rewoundFrom := ""
	if authored && len(m.future) > 0 {
		for _, fm := range m.future {
			if fm.Role == "user" && fm.Authored {
				rewoundFrom = oneLine(fm.Content)
				break
			}
		}
	}
	m.discardFuture() // new activity while rewound kills the redo stack
	// Settled subagents stay in the dock until the user sends a new message, so
	// they can review a finished subagent's transcript before moving on. A
	// user-typed (authored) turn sweeps them; machine turns (steered reports,
	// wake turns) don't, or a settling background task would clear its own row
	// before the user ever saw it. A task whose chat pane is open is kept (its
	// retained subagent is in use).
	if authored && m.agent != nil {
		var keep []string
		if m.taskVP != nil {
			keep = append(keep, m.taskVP.id)
		}
		m.agent.Tasks().ClearSettled(keep...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	p := m.prog
	// send is nil-safe: headless tests drive Update directly, so turn
	// callbacks drop their messages instead of panicking on a nil program
	send := func(msg tea.Msg) {
		if p != nil {
			p.Send(msg) //nolint:uilock // background: send runs on the agent Turn goroutine (go func at the caller)
		}
	}

	// Coalesce streaming deltas (~25fps) so each SSE chunk doesn't cost a
	// full Update/View cycle. Reasoning tokens get their own buffer so
	// thinking and answer text never interleave within one update; both drain
	// on the same timer.
	var mu sync.Mutex
	var pend, thinkPend string
	var timer *time.Timer
	flush := func() {
		mu.Lock()
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		text, think := pend, thinkPend
		pend, thinkPend = "", ""
		mu.Unlock()
		if think != "" {
			send(thinkMsg(think))
		}
		if text != "" {
			send(textMsg(text))
		}
	}
	schedule := func() {
		if timer == nil {
			timer = time.AfterFunc(40*time.Millisecond, flush)
		}
	}
	onText := func(d string) {
		mu.Lock()
		pend += d
		schedule()
		mu.Unlock()
	}
	onThink := func(d string) {
		mu.Lock()
		thinkPend += d
		schedule()
		mu.Unlock()
	}

	go func() {
		var compactTook, compactKept int // last OnCompact counts; read by OnCompacted
		events := agent.Events{
			OnText:  onText,
			OnThink: onThink,
			OnToolStart: func(id, n, a string) {
				flush()
				send(toolStartMsg{id, n, a})
			},
			OnToolEnd: func(id, n, r string) { send(toolEndMsg{id, n, r}) },
			// tool call still streaming from the model: show a queued row
			// before execution. Detached send like OnToolOutput — args arrive
			// in deltas and a parked p.Send must not wedge the stream.
			OnToolCall: func(id, n, a string) { go send(toolCallMsg{id, n, a}) },
			// Detached send: snapshots are lossy progress, and a parked
			// p.Send must never wedge bashrun's ticker goroutine (the ABBA
			// lesson from docs/concurrency.md — same rule as sendTaskMsg).
			OnToolOutput: func(id, soFar string) { go send(toolOutputMsg{id, soFar}) },
			OnSteer: func(s string) {
				flush()
				send(steeredMsg(s))
			},
			OnCompactStart: func(took, est int) { send(compactStartMsg{took, est}) },
			// OnCompact fires immediately before OnCompacted on the same turn
			// goroutine; stash its counts so the result note (one compactMsg)
			// carries them alongside the summary and the model/usage.
			OnCompact: func(took, kept int) { compactTook, compactKept = took, kept },
			OnCompacted: func(sum string, cutoff int, info agent.CompactInfo) {
				send(compactMsg{took: compactTook, kept: compactKept, summary: sum, cutoff: cutoff, info: info})
			},
			OnUsage: func(u llm.Usage) { send(usageMsg(u)) },
			// The decay pass rewrote n prefix messages in agent.Messages; drop
			// the saved watermark so the next persist re-saves everything
			// (from 1 — seq 0 is the system prompt, never a stored row; the
			// store INSERT OR REPLACEs rows). Direct field write: we're in the
			// turn goroutine but m.saved is only touched by the UI goroutine's
			// persist at turn end, and this lands before it.
			OnDecay: func(n int) { m.saved = 1 },
			OnRetry: func(ev llm.RetryEvent) {
				flush()
				send(noticeMsg(fmt.Sprintf("⚠ request failed (%s) — retrying in %s (attempt %d/%d)",
					ev.Err, ev.Delay.Round(time.Millisecond), ev.Attempt+1, ev.Max)))
			},
		}
		var final string
		var err error
		switch {
		case len(parts) > 0:
			final, err = m.agent.TurnWithImages(ctx, prepared, parts, events)
		case authored:
			final, err = m.agent.TurnAuthored(ctx, prepared, events)
		default:
			final, err = m.agent.Turn(ctx, prepared, events)
		}
		flush()
		// stamp rewind provenance on the submitted message (appended by turn)
		if rewoundFrom != "" && userMsgIdx < len(m.agent.Messages) {
			m.agent.Messages[userMsgIdx].RewoundFrom = rewoundFrom
		}
		send(turnDoneMsg{final: final, err: err, at: userMsgIdx, snap: preSnap, clean: workspaceClean()})
	}()
	m.appendRaw(blockUser, linkifyFilePaths(text, realFileExists))
	if authored {
		// map the message index to its block for rewind live-scroll
		for len(m.msgBlock) <= userMsgIdx {
			m.msgBlock = append(m.msgBlock, -1)
		}
		m.msgBlock[userMsgIdx] = len(m.blocks) - 1
	}
	return m, m.spin.Tick
}

// busyCmd reports whether a slash command is safe to run while a turn is in
// flight. These adjust settings or views only — they never touch
// Agent.Messages, busy, or the session — so they run immediately instead of
// being queued as messages (queued text is submitted to the model verbatim
// after the turn ends). /fork is the exception: it DOES touch the session,
// but only through a fresh copy (busyFork) — it never mutates the in-flight
// conversation, so it runs immediately too.
func busyCmd(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "/help", "/theme", "/mouse", "/effort", "/subagents", "/tasks", "/subagent", "/cd", "/pwd", "/report", "/export", "/fork":
		return true
	case "/auth": // must run now even while busy: an inline key queued as a chat message would be sent to the model
		return true
	case "/goal": // status, clear, and rounds are settings; resume/<text> submit turns
		return len(fields) == 1 || fields[1] == "clear" || fields[1] == "rounds"
	}
	return false
}

func (m *model) command(text string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(text)
	switch fields[0] {
	case "/quit", "/exit", "/q":
		return m, tea.Quit
	case "/clear":
		if m.busy {
			m.append(dimStyle.Render("(busy — /clear after this turn)"))
			return m, nil
		}
		m.agent.Messages = m.agent.Messages[:1] // keep system prompt
		m.agent.ResetUsage()                    // zero the status line's spend counters
		m.lastResp = llm.Usage{}                // the cleared history has no last response
		m.blocks = nil
		m.msgBlock = nil
		m.future = nil   // no redo across a cleared conversation
		m.setGoal("")    // clear before detaching so the old session's goal is dropped too
		m.sessionID = "" // next turn starts a fresh session
		m.agent.Tasks().SetSessionID("")
		m.agent.SetSessionID("")
		m.saved = 1
		m.append(dimStyle.Render("(conversation cleared)"))
	case "/memory":
		m.memoryCommand(fields[1:])
	case "/schedule":
		m.scheduleCommand(fields[1:])
	case "/me":
		return m, m.openMe()
	case "/compact":
		if len(fields) > 1 {
			switch fields[1] {
			case "retry":
				m.compactRetry()
				return m, nil
			case "log":
				m.compactLog()
				return m, nil
			}
			m.compactCommand(fields[1:])
			return m, nil
		}
		if m.busy {
			m.append(dimStyle.Render("(busy — /compact will land after this turn)"))
			return m, nil
		}
		m.busy = true
		took := len(m.agent.Messages)
		m.append(dimStyle.Render(fmt.Sprintf("◎ compacting %d msgs (est. %s) with %s…",
			took, fmtTok(agent.EstimateTokens(m.agent.Messages)), m.compactModelLabel())))
		p := m.prog
		ag := m.agent // capture the current conversation for the summary call
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		go func() {
			var summary string
			var cutoff int
			var info agent.CompactInfo
			err := ag.ManualCompact(ctx, agent.Events{
				OnCompacted: func(s string, c int, ci agent.CompactInfo) { summary, cutoff, info = s, c, ci },
			})
			if p != nil { // nil in headless tests; compaction still ran
				p.Send(compactMsg{took: took - len(ag.Messages), kept: len(ag.Messages), summary: summary, cutoff: cutoff, info: info, err: err})
				p.Send(turnDoneMsg{}) // clear busy state
			}
		}()
		return m, m.spin.Tick
	case "/mcp":
		return m.mcpCommand(fields)
	case "/lsp":
		return m.lspCommand(fields)
	case "/cd":
		m.cdCommand(strings.TrimSpace(strings.TrimPrefix(text, "/cd")))
		return m, nil
	case "/pwd":
		m.append(dimStyle.Render(cwd()))
		return m, nil
	case "/subagent": // user-spawned background subagent — the LLM isn't the only driver
		m.taskCommand(strings.TrimSpace(strings.TrimPrefix(text, "/subagent")))
		return m, nil
	case "/subagents", "/tasks": // /tasks kept as an alias
		if len(fields) > 1 { // /subagents <id>: jump straight into the detail view
			m.openTask(fields[1])
			return m, nil
		}
		// bare /tasks focuses the dock if it exists, else prints the list
		if len(m.dockTasks()) > 0 {
			m.tasksFocus = true
			m.clampTaskSel()
			return m, nil
		}
		m.append(m.tasksView())
		return m, nil
	case "/theme":
		if len(fields) > 1 {
			switch fields[1] {
			case "light", "dark", "auto":
				m.setTheme(fields[1])
			default:
				m.append(errStyle.Render("usage: /theme light|dark|auto"))
			}
		} else {
			m.openPaletteOn("theme") // bare: open the switcher, don't toggle blind
		}
		return m, nil
	case "/mouse":
		m.mouseOn = !m.mouseOn
		cfg := m.cfg
		b := m.mouseOn
		cfg.Mouse = &b
		if err := cfg.Save(); err != nil {
			m.append(errStyle.Render("config save failed: " + err.Error()))
		}
		m.append(dimStyle.Render("mouse capture: " + onOff(m.mouseOn) + " (on = wheel scroll + ⚡ clicks, the default, drag to select/copy; off = native drag-to-copy, but tmux captures the wheel)"))
		// We manage mouse reporting directly, so toggle the escape ourselves
		// rather than tea.EnableMouseCellMotion.
		if m.mouseOn {
			enableClickWheelMouse(os.Stdout)
			if m.uiMode == opencodeMode {
				fmt.Fprint(os.Stdout, "\x1b[?1003h") // hover needs all-motion; ?1002 alone downgraded it
			}
		} else {
			disableClickWheelMouse(os.Stdout)
		}
		return m, nil
	case "/effort":
		levels := m.effortsFor()
		if len(fields) > 1 {
			lv, ok := parseEffort(levels, fields[1])
			if !ok {
				names := make([]string, len(levels))
				for i, e := range levels {
					names[i] = effortLabel(e)
				}
				m.append(errStyle.Render("unknown effort level; " + m.agent.Model + " supports: " + strings.Join(names, ", ")))
				break
			}
			m.setEffort(lv)
			m.append(dimStyle.Render("⚡ effort: " + effortLabel(m.agent.Effort)))
		} else {
			m.openPaletteOn("reasoning effort") // bare: open the level selector
		}
	case "/goal-from-context":
		if m.busy {
			m.append(dimStyle.Render("(busy — /goal-from-context after this turn)"))
			return m, nil
		}
		window := agent.GoalFromContextDefaultWindow
		if len(fields) > 1 {
			n, err := strconv.Atoi(fields[1])
			if err != nil || n < 2 {
				m.append(errStyle.Render("usage: /goal-from-context [n] — n ≥ 2 messages of context (default " + strconv.Itoa(agent.GoalFromContextDefaultWindow) + ")"))
				return m, nil
			}
			window = n
		}
		tail, err := agent.GoalFromContextMessages(m.agent.Messages, window)
		if err != nil {
			m.append(errStyle.Render(err.Error()))
			return m, nil
		}
		// one non-streaming call on the CURRENT model (the compact-model
		// override is deliberately ignored) distills the tail into a goal
		m.busy = true
		m.append(dimStyle.Render(fmt.Sprintf("◎ formulating goal from the last %d messages…", len(tail))))
		p := m.prog
		// ag may drift from m.agent if the user /model-switches mid-formulation:
		// usage lands on the old agent, the goal submits on the new one. The
		// call itself is safe (Complete touches no Agent state, AddUsage is
		// mutex-protected) and the window is seconds — not worth a guard.
		ag := m.agent
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		prompt := agent.BuildGoalFromContextPrompt(tail)
		formulate := func() (string, error) {
			goal, usage, err := ag.Client.Complete(ctx, llm.Request{
				Model:     ag.Model,
				MaxTokens: 8192,
				Messages:  []llm.Message{{Role: "user", Content: prompt}},
			})
			ag.AddUsage(usage) // the formulation call is session spend too
			return goal, err
		}
		if p == nil {
			// headless (tests): run inline on the caller's goroutine — with
			// no program to pump messages the Update handler can't run, so
			// apply the same notes/goal here; the goal loop itself never
			// starts without a running program
			goal, err := formulate()
			m.busy = false
			m.cancel = nil
			switch {
			case err != nil && !errors.Is(err, context.Canceled):
				m.append(errStyle.Render("goal-from-context failed: " + err.Error()))
			case err == nil && strings.TrimSpace(goal) == "":
				m.append(errStyle.Render("goal-from-context: model returned an empty goal"))
			case err == nil:
				m.setGoal(strings.TrimSpace(goal))
				m.append(dimStyle.Render("◎ goal set: " + m.goal))
			}
			return m, nil
		}
		go func() {
			goal, err := formulate()
			// the msg handler owns busy/cancel: on success it submits (busy
			// belongs to the new turn), on failure it clears them directly —
			// a turnDoneMsg{} here would either cancel-proof the fresh turn
			// (success) or re-engage a paused goal's loop (failure)
			p.Send(goalFromContextMsg{goal: goal, err: err})
		}()
		return m, m.spin.Tick
	case "/computer-use", "/computer":
		m.computerUseCommand(fields[1:], text)
		return m, nil
	case "/goal":
		switch {
		case len(fields) == 1:
			if m.goal == "" {
				m.append(dimStyle.Render("no goal set — /goal <text> to set one"))
			} else {
				m.append(dimStyle.Render(fmt.Sprintf("◎ goal (round %d/%d): %s", m.goalRounds, m.goalMaxRounds(), m.goal)))
			}
		case fields[1] == "clear":
			m.setGoal("")
			m.append(dimStyle.Render("(goal cleared)"))
		case fields[1] == "rounds":
			m.goalRoundsCommand(fields[2:])
		case fields[1] == "resume":
			if m.goal == "" {
				m.append(errStyle.Render("no goal to resume — set one with /goal <text>"))
				break
			}
			m.goalRounds = 0
			m.append(dimStyle.Render("◎ resuming goal: " + m.goal))
			return m.submitGoal(goalContinuePrompt(m.goal))
		default:
			goal := strings.TrimSpace(strings.TrimPrefix(text, "/goal"))
			m.setGoal(goal)
			m.append(dimStyle.Render("◎ goal set: " + goal))
			return m.submit(goal)
		}
	case "/fork":
		// No busy guard: mid-turn /fork creates the copy right away (busyFork)
		// and defers only the switch to turn end — the whole point is cloning
		// the conversation while the model is still working.
		m.forkCommand(strings.TrimSpace(strings.TrimPrefix(text, "/fork")))
		return m, nil
	case "/rename":
		if m.busy {
			m.append(dimStyle.Render("(busy — /rename after this turn)"))
			return m, nil
		}
		m.renameCommand(strings.TrimSpace(strings.TrimPrefix(text, "/rename")))
		return m, nil
	case "/resume":
		if m.busy {
			m.append(dimStyle.Render("(busy — /resume after this turn)"))
			return m, nil
		}
		if len(fields) > 1 {
			if err := m.resume(fields[1]); err != nil {
				m.append(errStyle.Render(err.Error()))
			}
			break
		}
		m.openPicker()
	case "/context-doctor":
		m.append(m.doctorReport())
	case "/export": // read-only, so it runs mid-turn instead of being queued
		m.exportCommand(strings.TrimSpace(strings.TrimPrefix(text, "/export")))
	case "/report":
		m.append(m.reportBlock())
	case "/help":
		m.append(dimStyle.Render(helpText()))
	case "/auth":
		m.authCommand(fields[1:])
	case "/model", "/model-for-session":
		persist := fields[0] == "/model"
		if len(fields) < 2 {
			m.openModelPicker(!persist)
			break
		}
		if fields[1] == "refresh" {
			m.append(dimStyle.Render("refreshing model catalogs…"))
			go func() {
				m.fetchCatalogs(true)
				if m.prog != nil {
					m.prog.Send(noticeMsg("model catalogs refreshed — /model shows newly announced models"))
				}
			}()
			break
		}
		prov := ""
		if len(fields) > 2 {
			prov = fields[2]
		}
		name := fields[1]
		resolved, ok, alts := resolveModelFuzzy(m.cfg, name)
		if !ok {
			if len(alts) > 0 {
				m.append(errStyle.Render(fmt.Sprintf("ambiguous model %q — did you mean: %s?", name, strings.Join(alts, ", "))))
				return m, nil
			}
			m.append(errStyle.Render("unknown model " + name))
			return m, nil
		}
		m.switchModel(resolved, prov, persist)
	default:
		m.append(errStyle.Render("unknown command " + fields[0]))
	}
	return m, nil
}

// compactCommand handles "/compact <args…>": off restores the built-in
// default compaction model, "<model> [provider]" selects one (persisted). The
// model may be a config entry or a catalog-advertised id (the catalog fallback
// in Resolve routes it); anything else resolves fuzzy before giving up.
func (m *model) compactCommand(args []string) {
	if args[0] == "off" {
		m.compactModel, m.compactProv = "", ""
		m.applyCompactModel()
		m.cfg.CompactModel, m.cfg.CompactProvider = "", ""
		if err := m.cfg.Save(); err != nil {
			m.append(errStyle.Render("config save failed: " + err.Error()))
		}
		m.append(dimStyle.Render("◎ compaction model: default (" + config.DefaultCompactModel + ")"))
		return
	}
	name := args[0]
	if _, ok := m.cfg.Models[name]; !ok && !catalogAdvertises(m.cfg, name) {
		resolved, ok2, cands := resolveModelFuzzy(m.cfg, name)
		if !ok2 {
			if len(cands) > 0 {
				m.append(errStyle.Render("ambiguous model " + name + " — could be " + strings.Join(cands, ", ")))
			} else {
				m.append(errStyle.Render("unknown model " + name))
			}
			return
		}
		name = resolved
	}
	m.compactModel = name
	m.compactProv = ""
	if len(args) > 1 {
		m.compactProv = args[1]
	}
	m.applyCompactModel()
	if m.agent.CompactModel == "" { // resolve failed; don't persist a broken pick
		m.compactModel, m.compactProv = "", ""
		return
	}
	m.cfg.CompactModel, m.cfg.CompactProvider = m.compactModel, m.compactProv
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
	note := "◎ compaction model: " + m.compactModel
	if prov := resolvedProvider(m.cfg, m.compactModel, m.compactProv); prov != "" {
		note += " @ " + prov
	}
	m.append(dimStyle.Render(note))
}

// resolvedProvider reports which provider serves a picked model: the explicit
// pick when given, else the model's first configured provider, else the
// catalog's owner (for catalog-advertised picks without a config entry).
func resolvedProvider(cfg *config.Config, model, prov string) string {
	if prov != "" {
		return prov
	}
	if mdl := cfg.Models[model]; len(mdl.Providers) > 0 {
		return mdl.Providers[0]
	}
	cats := config.LoadCatalogs()
	for name := range cfg.Providers {
		if cat, ok := cats[name]; ok && cat.Find(model) != nil {
			return name
		}
	}
	return ""
}

// catalogAdvertises reports whether a configured provider's cached /models
// catalog lists the model id (making it resolvable without a config entry).
func catalogAdvertises(cfg *config.Config, name string) bool {
	cats := config.LoadCatalogs()
	for p := range cfg.Providers {
		if cat, ok := cats[p]; ok && cat.Find(name) != nil {
			return true
		}
	}
	return false
}

// compactPct returns the live threshold percent (the default when unset).
// cfg.CompactPct is the authoritative value; the agent's float is derived.
func (m *model) compactPct() int {
	pct := m.cfg.CompactPct
	if pct == 0 {
		pct = config.DefaultCompactPct
	}
	return min(max(pct, 10), 90)
}

// setCompactPct applies a compaction-threshold percent (clamped 10–90): the
// agent compacts proactively once the estimated context use crosses it.
// Persisted as the new default. Palette-driven, so no transcript note — the
// row's [NN%] badge is the feedback (same as the effort/theme steppers).
func (m *model) setCompactPct(pct int) {
	pct = min(max(pct, 10), 90)
	m.agent.CompactThreshold = float64(pct) / 100
	m.cfg.CompactPct = pct
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
}

const menuRows = 8

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
	left := fmt.Sprintf(" whip · %s @ %s · %s", m.modelName, m.provName, cwd())
	if m.goal != "" {
		left += " · ◎ " + truncLine(m.goal, 40)
	}
	if !m.follow {
		left += fmt.Sprintf(" · ↑ %d%%", int(m.vp.ScrollPercent()*100))
	}
	// session token usage, provider-reported: in (cached of it) / out, then
	// the share of the advertised context window the conversation occupies
	u := m.agent.Usage()
	if u.PromptTokens > 0 || u.CompletionTokens > 0 {
		left += fmt.Sprintf(" · ⣿ %s in", fmtTok(u.PromptTokens))
		if c := u.Cached(); c > 0 {
			left += fmt.Sprintf(" (%s cached)", fmtTok(c))
		}
		left += fmt.Sprintf(" · %s out", fmtTok(u.CompletionTokens))
	}
	if m.agent.ContextLimit > 0 {
		left += fmt.Sprintf(" · %d%% ctx", agent.EstimateTokens(m.agent.Messages)*100/m.agent.ContextLimit)
	}
	// running background subagents get a badge; /tasks lists them
	if n := m.runningTasks(); n > 0 {
		left += fmt.Sprintf(" · ⚙ %d sub", n)
	}
	// right-aligned clickable effort control; ◌ marks thinking display
	right := "⚡ " + effortLabel(m.agent.Effort) + " "
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
	if m.taskVP != nil {
		b.WriteString(m.taskViewView())
		return b.String()
	}
	// One compact hint up top — the full roster lives behind the ctrl+p palette
	// and the /help command. The bottom hint covers the busy/interactive states.
	if m.uiMode != opencodeMode { // opencode keeps the top clean; the hint lives in the prompt row
		tips := "`ctrl+p` commands"
		b.WriteString(dimStyle.Render(tips) + "\n\n")
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
	if m.busy && m.uiMode != opencodeMode { // opencode mode: the status bar carries the spinner + esc hint
		hint := " thinking… (enter queues · /theme /mouse /effort run now · esc interrupts · ctrl+c ctrl+c interrupts)"
		if m.iactive != nil {
			hint = " bash (interactive) — type to respond · ctrl+c ctrl+c to cancel"
		} else if m.interrupt1 {
			hint = " thinking… (esc or ctrl+c again to interrupt)"
		}
		b.WriteString("\n" + m.spin.View() + dimStyle.Render(m.busyStats()+hint) + "\n")
	}
	if len(m.queue) > 0 {
		nav := ""
		if m.busy && m.input.Value() == "" {
			nav = " · ↑/↓ select · del removes"
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf(" ⧗ queued (%d) — enter on empty input to steer into this turn%s", len(m.queue), nav)) + "\n")
		for i, q := range m.queue {
			// one line per queued message: truncate (never wrap) so long
			// messages don't crowd out the transcript
			line := ansi.Truncate(youStyle.Render(" "+glyphUser)+q, m.width, "…")
			if i == m.queueSel {
				line = ansi.Truncate(botStyle.Render(" → ")+q+dimStyle.Render("  (del to remove)"), m.width, "…")
			}
			b.WriteString(line + "\n")
		}
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
	// the persistent background-subagent strip sits just below the input box
	// (above the status line): ↓ on an empty input moves focus into it
	if dock := m.tasksDock(); dock != "" {
		b.WriteString("\n" + dock)
	}
	b.WriteString("\n\n" + m.statusView()) // persistent status line, with a blank line above
	return b.String()
}

// inputPlaceholder is the idle input hint; syncInputPlaceholder re-uses it
// when the busy state clears so the two sites never drift.
const inputPlaceholder = "Ask whip anything… (/ for commands, tab completes)"

// syncInputPlaceholder reflects the busy state into the input's placeholder:
// while a turn runs, typing either steers into it (when the agent is only
// waiting on subagents) or queues behind it. Called from View so it tracks
// the state every render. headless-safe.
func (m *model) syncInputPlaceholder() {
	if m.input.Value() != "" {
		return // placeholder is hidden once the user is typing
	}
	switch {
	case !m.busy:
		m.input.Placeholder = inputPlaceholder
	case m.agent != nil && m.agent.WaitingOnSubagents():
		m.input.Placeholder = "waiting on subagents — type to steer this turn"
	default:
		m.input.Placeholder = "busy — type to queue (sent when the turn ends)"
	}
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
	if e := effortLabel(m.agent.Effort); e != "off" {
		model += " (" + e + ")"
	}
	u := m.agent.Usage()
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
	dir := shortCWD()
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
func shortCWD() string {
	dir := cwd()
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
