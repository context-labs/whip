// Package tui is whip's interactive bubbletea session (fullscreen alt-screen).
package tui

import (
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
	"github.com/context-labs/whip/internal/codexauth"
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
	// thinkingStyle renders reasoning tokens: dim and italic so they're
	// visually distinct from the answer.
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).Italic(true)
)

// messages sent from the agent goroutine
type (
	textMsg       string
	toolStartMsg  struct{ id, name, args string }
	toolEndMsg    struct{ id, name, result string }
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
	cutoff     int // index in the pre-compaction history the summary replaces
	err        error
}
type turnDoneMsg struct {
	final string
	err   error
	at    int    // conversation index the turn started at (snapshot key)
	snap  string // pre-turn workspace snapshot commit ("" = not a git repo)
	clean bool   // the turn left the tree clean — snap is worthless, drop it
}
type (
	catalogsMsg   map[string]config.Catalog // background /models fetch result
	noticeMsg     string                    // dim one-liner appended to the transcript
	usageMsg      llm.Usage                 // one request's token usage
	quitArmMsg    struct{}                  // the idle ctrl+c arm window expired
	taskUpdateMsg struct{}                  // a background subagent started/settled — redraw
	mcpStatusMsg  struct{}                  // an MCP server changed state — redraw
	thinkMsg      string                    // streamed reasoning tokens
	imageMsg      struct {                  // ctrl+v clipboard image result
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
	msgBlock []int
	follow   bool // auto-scroll to bottom on new content
	width    int
	height   int

	busy    bool
	current string // in-flight partial assistant line
	inMsg   bool   // "● " prefix already printed for this assistant segment

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

	turnStart time.Time // when the in-flight turn began; zero when idle (busy line shows elapsed)

	queue      []string // messages typed while busy, sent after the turn ends
	queueSel   int      // selected queued message, -1 = none (not navigating)
	interrupt1 bool     // first ctrl+c pressed while busy; second cancels
	quit1      bool     // first ctrl+c pressed while idle; second quits (armed briefly)

	goal       string // active /goal; the loop continues until GOAL_MET
	goalRounds int    // continuation turns spent on the current goal
	titled     bool   // an auto-title has been attempted for this session

	mouseOn      bool       // runtime mouse-capture state (toggle with /mouse)
	sel          *selection // in-flight/last drag selection over the transcript
	selDragX     int        // last drag pointer position (edge auto-scroll re-checks it)
	selDragY     int
	vpLead       int    // top blank rows viewportView last dropped (selection row mapping)
	viewTop      int    // screen row of the view's first line (View tracks it; mouse Y is absolute)
	viewH        int    // height of the last rendered view
	themeHow     string // how auto theme detection resolved (env var, OSC query, …) — captured at startup/theme change for /report; never re-queried
	compactModel string // config model name for compaction summaries; "" = the built-in default
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

	tasksFocus bool      // the tasks dock owns ↑/↓/enter/esc instead of the input
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
	ti.Placeholder = "Ask whip anything… (/ for commands, tab completes)"
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
// was active on exit ("" if nothing was said).
func Run(cfg *config.Config, modelName, provName, sysPrompt, resumeID string, cautious bool) (string, error) {
	// Trust gate first: before whip reads a single file, ask whether this
	// folder's contents may steer the model. Persisted per absolute path in
	// ~/.whip/trusted.json (claude-code's per-project trust dialog).
	if ok, err := checkTrust(); err != nil {
		return "", err
	} else if !ok {
		return "", errors.New("folder not trusted")
	}

	ag, mn, pn, err := buildAgent(cfg, modelName, provName, sysPrompt)
	if err != nil {
		return "", err
	}

	ti := newInput()

	// default on: "" (config never set / pre-feature file) means medium, not
	// off; an explicit "off" in the file is honored
	ag.Effort = cfg.DefaultEffort
	if ag.Effort == "" {
		ag.Effort = "medium"
	}
	// Mouse capture ON by default so the wheel scrolls the transcript viewport
	// and ⚡/tool clicks work — with button-motion reporting (?1002) so a left
	// drag becomes whip's own selection (select.go): enabling click reporting
	// alone makes most terminals (Ghostty, kitty) suppress their native
	// drag-selection without sending the drag to anyone. With capture off,
	// tmux's WheelUpPane binding sees mouse_any_flag=0 and runs 'copy-mode -e',
	// scrolling tmux's own scrollback instead of the transcript. In tmux the
	// drag never reaches whip, so applyTmuxMouseFix routes MouseDrag1Pane to
	// copy-mode for drag-to-copy there. Explicit config wins.
	mouseOn := true
	if cfg.Mouse != nil {
		mouseOn = *cfg.Mouse
	}
	showThinking := true // default on; "thinking": false in config opts out
	if cfg.Thinking != nil {
		showThinking = *cfg.Thinking
	}
	m := &model{
		cfg: cfg, agent: ag, modelName: mn, provName: pn, sysPrompt: sysPrompt,
		input: ti, spin: spinner.New(spinner.WithSpinner(spinner.Dot)), follow: true, saved: 1,
		catalogs: config.LoadCatalogs(), mouseOn: mouseOn, now: time.Now, showThinking: showThinking,
		compactModel: cfg.CompactModel, compactProv: cfg.CompactProvider,
		skillScan: func() []skills.Skill { return skills.Scan(skills.DefaultDirs()...) },
	}
	m.applyCompactModel()
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
			// MCP connects settle in the background; push each new tool set
			// into the CURRENT agent (mutex-guarded on the agent side) so
			// servers that connect after turn 1 show up without a restart.
			// The closure reads m.agent at call time: resume/model-switch
			// replace the agent, and wireTasks re-points the manager at it.
			m.mcpMgr.SetOnChange(func() {
				m.agent.SetMCPTools(m.mcpMgr.Tools())
				if m.prog != nil { // nil in headless tests
					m.prog.Send(mcpStatusMsg{})
				}
			})
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
	// Bottom-anchor the inline view: move the cursor to the terminal's last
	// row before bubbletea's first paint, so the view's screen position is
	// knowable (viewTop = height - viewH). Without this the view starts
	// wherever the shell prompt left the cursor, and mouse events — which are
	// ABSOLUTE screen coordinates — map a few rows off (drag-select landing
	// two lines above the pointer).
	fmt.Fprint(os.Stdout, "\x1b[9999;1H")
	if m.mouseOn {
		enableClickWheelMouse(os.Stdout)
		applyTmuxMouseFix()
	}
	// pick the glamour style that matches the pick/detection resolution;
	// keep how detection resolved so /report can name the source
	m.themeHow = m.applyTheme(cfg.Theme)
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
	sk, problems := skills.ScanDetailed(skills.DefaultDirs()...)
	var b strings.Builder
	var warned bool

	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\n", args...)
	}
	if len(sk) > 0 {
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
		var parts []string
		for _, st := range sts {
			switch st.Status {
			case mcp.StatusReady:
				parts = append(parts, fmt.Sprintf("%s ✓ (%d tools)", st.Name, st.Tools))
			case mcp.StatusFailed:
				parts = append(parts, st.Name+" ✗")
				warned = true
			case mcp.StatusDisabled:
				parts = append(parts, st.Name+" ○")
			default:
				parts = append(parts, st.Name+" ◌")
			}
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
	fmt.Fprint(w, "\x1b[?1002l\x1b[?1000l\x1b[?1006l")
}

// applyTmuxMouseFix makes plain drag-to-copy work inside tmux while whip
// captures the mouse for wheel/clicks. tmux's default MouseDrag1Pane binding
// checks mouse_any_flag (set by our ?1000/?1002) and forwards the drag to the
// app — but tmux itself never forwards drag bytes from a terminal, so whip's
// own selection (select.go) can't see them. Rebinding it to copy-mode -M
// (only when the pane isn't already in a mode and isn't full mouse-tracking)
// makes the drag open tmux copy-mode selection instead, restoring drag-to-copy.
// Wheel still reaches whip: WheelUpPane stays bound to send -M. No-op outside
// tmux or if tmux isn't available.
func applyTmuxMouseFix() {
	if os.Getenv("TMUX") == "" {
		return
	}
	// Only override when the pane can still use copy-mode: not in alt-screen,
	// not already in a mode, and not full/all mouse tracking (in which case the
	// app genuinely wants the drag). Then select via copy-mode -M.
	_ = exec.CommandContext(context.Background(), "tmux", "bind-key", "-T", "root", "MouseDrag1Pane", "if-shell", "-F",
		"#{||:#{alternate_on},#{pane_in_mode},#{mouse_all_flag}}", "send-keys -M", "copy-mode -M").Run()
}

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

// fetchCatalogs refreshes each provider's cached model list in the background
// and sends the merged result to the UI. force bypasses the 24h TTL
// (/model refresh) so newly announced models appear immediately.
func (m *model) fetchCatalogs(force bool) {
	cats := config.LoadCatalogs()
	if cats == nil { // defensive; LoadCatalogs already returns non-nil
		cats = map[string]config.Catalog{}
	}
	dirty := false
	for name, prov := range m.cfg.Providers {
		if c, ok := cats[name]; ok && !force && !c.Stale() && c.BaseURL == prov.BaseURL {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var infos []llm.ModelInfo
		var err error
		if prov.API == "openai-codex-responses" {
			if strings.TrimRight(prov.BaseURL, "/") != config.CodexBaseURL {
				cancel()
				config.LogEvent("catalog.fetch", name+" skipped: Codex credentials are only sent to "+config.CodexBaseURL)
				continue
			}
			infos, err = llm.NewCodex(prov.BaseURL, &codexauth.Source{}).Models(ctx)
		} else {
			key, keyErr := prov.ResolveKey()
			if keyErr != nil {
				cancel()
				config.LogEvent("catalog.fetch", name+" skipped: "+keyErr.Error())
				continue
			}
			if key == "" {
				cancel()
				continue
			}
			infos, err = llm.New(prov.BaseURL, key).Models(ctx)
		}
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
	if m.prog != nil { // nil in tests that drive the command dispatch directly
		m.prog.Send(catalogsMsg(cats))
	}
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
			m.blocks = append(m.blocks, block{kind: blockText, text: youStyle.Render("❯ ") + linkifyFilePaths(msg.TextContent(), realFileExists)})
		case "assistant":
			if strings.TrimSpace(msg.TextContent()) != "" {
				bi = len(m.blocks)
				m.blocks = append(m.blocks, block{kind: blockAssistant, text: strings.TrimRight(msg.TextContent(), "\n")})
			}
			for _, tc := range msg.ToolCalls {
				m.blocks = append(m.blocks, block{kind: blockText, text: toolStyle.Render("⚒ "+tc.Function.Name+" ") + dimStyle.Render(tc.Function.Arguments)})
			}
		case "tool":
			// Synthetic results synthesized at load for interrupted calls get
			// an inline row so the user sees what the model sees; real tool
			// results stay folded under their assistant block.
			if strings.HasPrefix(msg.Content, "Error: tool call interrupted") {
				m.blocks = append(m.blocks, block{kind: blockText, text: errStyle.Render("⚒ "+msg.Name+" ") + dimStyle.Render("— interrupted: session ended before a result was recorded")})
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
	client, err := clientForProvider(prov, provName, cfg.MaxRetries)
	if err != nil {
		return nil, "", "", err
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
	ag := agent.New(client, apiID, maxOut, sysPrompt)
	ag.ModelName, ag.Provider = modelName, provName
	ag.ContextLimit = ctxLimit
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
			var parts []llm.ContentPart
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
	blockText      blockKind = iota // already-styled line(s): re-wrap on resize
	blockAssistant                  // raw markdown: re-render through glamour
	blockTool                       // raw tool result: collapsed preview, expandable
	blockToolRun                    // a running tool call: verb line, collapses on completion
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
	// live is the latest partial-output snapshot for a running bash call,
	// rendered under the verb line; cleared when the tool ends.
	live string
	// y0/y1 are the block's line range in the last rendered content (set by
	// refreshVP); used to map a mouse click to the block under it.
	y0, y1 int
	// cache of the last render: valid while !stale and width matches.
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
	case blockAssistant:
		w := width - 2 // body indents under the "● " marker
		if w <= 0 {
			w = 80 // no terminal size yet: sane default
		}
		body := indentLines(renderMarkdown(b.text, w), 2)
		return botStyle.Render("● ") + strings.TrimPrefix(body, "  ")
	case blockTool:
		lines := strings.Split(strings.TrimRight(b.text, "\n"), "\n")
		if b.expanded || len(lines) <= toolPreviewLines {
			return wrap(dimStyle.Render("  "+strings.Join(lines, "\n  ")), width)
		}
		preview := lines[:toolPreviewLines]
		// An edit-style result carries a fenced diff at the tail; surface its
		// -/+ lines in the collapsed preview so the change shows without
		// expanding.
		if strings.HasSuffix(lines[len(lines)-1], "```") {
			var diffs []string
			for _, l := range lines {
				if strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+") {
					diffs = append(diffs, l)
				}
			}
			if len(diffs) > 0 {
				preview = append(preview, diffs...)
			}
		}
		out := dimStyle.Render("  " + strings.Join(preview, "\n  "))
		hint := fmt.Sprintf("\n  … +%d lines (ctrl+e or click to expand)", len(lines)-toolPreviewLines)
		return wrap(out+dimStyle.Render(hint), width)
	case blockToolRun:
		// While running, the verb line shows in full with the live output
		// tail under it. On completion the same block collapses in place to
		// one line (red on failure); ctrl+e expands.
		if b.toolRunning || b.expanded {
			if b.live != "" && b.toolRunning {
				return wrap(b.text, width) + "\n" + wrap(dimStyle.Render("  "+b.live), width)
			}
			return wrap(b.text, width)
		}
		line := ansi.Truncate(b.text, width, "…")
		if b.toolFailed {
			return wrap(errStyle.Render(line), width)
		}
		return wrap(dimStyle.Render(line), width)
	default:
		return wrap(b.text, width)
	}
}

// expand toggles a tool block and returns whether it changed.
func (b *block) toggle() bool {
	if b.kind != blockTool && b.kind != blockToolRun {
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
	return textarea.Blink
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
type bgResult struct{ light, valid bool }

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
		if light, ok := queryTerminalBackground(tty, inTmux); ok {
			_ = tty.Close()
			setScheme(light)
			bgCache = bgResult{light: light, valid: true}
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
	ti.SetWidth(m.input.Width() + 2) // Width() is content width; SetWidth takes total
	ti.SetHeight(h)
	ti.SetValue(val)
	ti.CursorEnd()
	m.input = ti
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
	if m.iactive != nil {
		// input box is hidden while a command has the terminal; drop its height
		// and the leading blank line View inserts before it.
		chrome -= m.input.Height()
	}
	if m.busy {
		chrome += 2 // blank line above the spinner + the spinner line itself
	}
	if m.current != "" {
		chrome += lipgloss.Height(m.currentView()) + 1 // + its blank separator
	}
	if m.curThink != "" {
		chrome += lipgloss.Height(m.thinkView()) + 1
	}
	if m.iactive != nil {
		chrome += lipgloss.Height(m.interactiveView()) + 1
	}
	if m.menu != nil {
		chrome += min(len(m.menu.cands), menuRows) + 1
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
// renders as the last dockRows rows above the input box and bottom pad, but
// dockSkip non-task rows (the focused hint) sit on top of the task rows.
// layout() keeps both in sync with what View renders. The row is an absolute
// screen row: counted up from the view's bottom (viewTop+viewH), which equals
// the terminal bottom while the view is bottom-anchored but stays correct
// when a shrunk view floats above it.
func (m *model) dockTop() int {
	bottom := m.height
	if m.viewH > 0 {
		bottom = m.viewTop + m.viewH
	}
	return bottom - 2 - m.input.Height() - m.dockRows + m.dockSkip
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.layout()

	if vp, ok := msg.(viewProbe); ok { // tests read model state race-safely
		vp.fn(m)
		return m, nil
	}
	switch msg := msg.(type) {
	case cfgSyncTick:
		return m.cfgSync()

	case cfgSyncMsg:
		m.applyCfgSync(msg)
		return m, nil

	case tea.WindowSizeMsg:
		resized := msg.Width != m.width // width change → re-wrap the whole transcript
		m.width, m.height = msg.Width, msg.Height
		// re-anchor the view position: after a resize (and on the first size
		// at startup) assume the view sits at the bottom — the next View()
		// computes viewTop = height - viewH from this sentinel.
		m.viewTop = 1 << 30
		m.input.SetWidth(msg.Width - 2)
		if resized {
			m.refreshVP() // every block re-renders at the new width (floored at minRenderWidth)
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
		if handled, cmd := m.handleMouseSelect(msg); handled {
			return m, cmd
		}
		// clicking the ⚡ control in the header cycles reasoning effort
		// (mouse Y is an absolute screen row; the header is the view's top row)
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft &&
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
			m.flushCurrent() // thinking renders above the answer
			m.curThink += string(msg)
			if i := strings.LastIndexByte(m.curThink, '\n'); i >= 0 {
				done := m.curThink[:i]
				m.curThink = m.curThink[i+1:]
				m.appendThink(done)
			}
		}
		return m, nil

	case toolStartMsg:
		m.flushThink()
		m.flushCurrent()
		args := msg.args
		if msg.name == "browser_exec" || msg.name == "computer_exec" {
			// Surface the step label (the code's first # comment) as the row
			// text instead of raw JSON — the model writes it for the user.
			if label := browserStepLabel(msg.args); label != "" {
				args = label
			}
		}
		// a running row: icon + present-participle verb + full args (the
		// command being run is always fully visible). On toolEndMsg the same
		// block collapses in place to one line.
		row := toolStyle.Render("⚒ "+toolVerb(msg.name)+" ") + dimStyle.Render(args)
		m.blocks = append(m.blocks, block{kind: blockToolRun, text: row, toolID: msg.id, toolRunning: true})
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
		// store the raw result; render collapses to a preview (ctrl+e /
		// click expands) and re-wraps on resize
		m.appendRaw(blockTool, msg.result)
		// collapse the matching running row in place: full args+result when
		// expanded, one dim line (red on failure) otherwise
		for i := len(m.blocks) - 1; i >= 0; i-- {
			b := &m.blocks[i]
			if b.kind == blockToolRun && b.toolRunning && b.toolID == msg.id {
				b.toolRunning = false
				b.toolFailed = strings.HasPrefix(msg.result, "Error:")
				b.live = ""
				b.text = toolStyle.Render("⚒ "+msg.name+" ") + dimStyle.Render(firstLine(msg.result))
				b.stale = true
				break
			}
		}
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
		m.append(youStyle.Render("❯ ") + linkifyFilePaths(string(msg), realFileExists) + dimStyle.Render("  (steered)"))
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
			if m.store != nil && m.sessionID != "" {
				// the agent's cutoff is in compacted coordinates; store the raw
				// seq so Load never double-folds a summary
				if err := m.store.RecordCompaction(m.sessionID, m.rawCutoff(msg.cutoff), msg.summary); err != nil {
					config.LogEvent("session.compact", "record failed: "+err.Error())
				}
			}
			m.append(dimStyle.Render(fmt.Sprintf("◎ compacted — summarized %d msgs, %d kept · raw history preserved", msg.took, msg.kept)))
			m.future = nil   // compaction rewrote history; stale redo entries would resurrect it
			m.msgBlock = nil // indices no longer match; rebuilt as blocks stream in
			m.persist()      // append the new (compacted) rows; raw rows stay
		}
		return m, nil

	case turnDoneMsg:
		m.flushThink()
		m.flushCurrent()
		m.busy = false
		m.cancel = nil
		m.interrupt1 = false
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

	case codexLoginResultMsg:
		m.applyCodexLoginResult(msg)
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
		// reads those); this message just forces a redraw mid-stream.
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

	case mcpStatusMsg:
		// An MCP server changed state. Announce each server's FIRST settle in
		// the transcript (one line, once per session per server) so arrivals
		// and failures are visible without typing /mcp — later transitions
		// (auto-reconnect, toggles) stay quiet to avoid flapping noise.
		if m.mcpMgr != nil {
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
		switch msg.kind {
		case 0: // text delta
			tv.buf.WriteString(msg.s)
		case 1: // tool start
			fmt.Fprintf(&tv.buf, "\n%s %s %s\n", toolStyle.Render("⚒"), msg.s, dimStyle.Render(msg.s2))
		case 2: // tool end
			preview := strings.Split(strings.TrimRight(msg.s2, "\n"), "\n")
			if len(preview) > 4 {
				preview = append(preview[:4], fmt.Sprintf("… +%d lines", len(msg.s2)-4))
			}
			fmt.Fprintf(&tv.buf, "%s\n", dimStyle.Render("  "+strings.Join(preview, "\n  ")))
		}
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
	// Paste collapse (opt-in via config collapsePaste): a multi-line bracketed
	// paste lands as a [Pasted ~N lines] placeholder in the input instead of
	// spraying the textarea; the real text is held in pasteBuf and swapped in
	// at submit. Off by default — a paste you can't see is a paste you can't
	// trust.
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
		// focus the tasks dock (or unfocus it) — the persistent strip above
		// the input listing background subagents
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
		case m.tasksFocus: // leave dock navigation, back to the main thread
			m.tasksFocus = false
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
			m.taskSel = max(m.taskSel-1, 0)
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
			// dockTasks is time-dependent (settled tasks age out after
			// dockSettledGrace), so the strip can go empty — or shrink below
			// taskSel — between the last paint and this keypress
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
	if m.cfg != nil && m.cfg.Providers[m.provName].API == "openai-codex-responses" {
		return 0, false
	}
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

	// A compaction model without an explicit provider follows its model route,
	// not the session's DefaultProvider. In particular, the built-in DeepSeek
	// summary model is available through inference.net, not a Codex subscription.
	compactProv := m.compactProv
	if compactProv == "" {
		if mdl, ok := m.cfg.Models[cm]; ok && len(mdl.Providers) > 0 {
			compactProv = mdl.Providers[0]
		}
	}
	prov, _, apiID, err := m.cfg.Resolve(cm, compactProv)
	if err != nil {
		if m.compactModel != "" { // a picked model failing is worth a note; a missing default isn't
			m.append(errStyle.Render("compaction model: " + err.Error() + " — using current model"))
		}
		return
	}
	client, err := clientForProvider(prov, compactProv, m.cfg.MaxRetries)
	if err == nil {
		m.agent.CompactClient = client
		m.agent.CompactModel = apiID
	} else if m.compactModel != "" {
		m.append(errStyle.Render("compaction model: " + err.Error() + " — using current model"))
	}
}

func clientForProvider(prov config.Provider, name string, maxRetries int) (llm.Client, error) {
	switch prov.API {
	case "", "openai-completions":
		key, err := prov.ResolveKey()
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("no API key for provider %q (set apiKey/apiKeyEnv in ~/.whip/config.json)", name)
		}
		client := llm.New(prov.BaseURL, key)
		client.MaxRetries = maxRetries
		return client, nil
	case "openai-codex-responses":
		if prov.Auth != "codex" {
			return nil, fmt.Errorf("codex provider %q requires auth:\"codex\"", name)
		}
		if strings.TrimRight(prov.BaseURL, "/") != "https://chatgpt.com/backend-api" {
			return nil, fmt.Errorf("codex provider %q must use https://chatgpt.com/backend-api", name)
		}
		source := &codexauth.Source{}
		if err := source.Available(); err != nil {
			return nil, err
		}
		return llm.NewCodex(prov.BaseURL, source), nil
	default:
		return nil, fmt.Errorf("unsupported API %q for provider %q", prov.API, name)
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
	}
	m.agent.Tasks().SetSessionID(m.sessionID)
	m.agent.SetSessionID(m.sessionID)
	if m.prog == nil {
		return // headless (tests)
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
func (m *model) switchModel(name, prov string) {
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
	m.wireTasks()
	if !slices.Contains(m.effortsFor(), ag.Effort) {
		m.resetEffort("") // the new model doesn't support the current level
	}
	m.cfg.DefaultModel, m.cfg.DefaultProvider = mn, pn // store the switch as the new default
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
	m.append(dimStyle.Render("→ " + mn + " @ " + pn))
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
	token := val[strings.LastIndexByte(val, ' ')+1:]
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
	// settled subagents already reported into the transcript; clear them off
	// the dock strip so a new turn starts with only what's still running
	if m.agent != nil {
		m.agent.Tasks().ClearSettled()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	p := m.prog
	// send is nil-safe: headless tests drive Update directly, so turn
	// callbacks drop their messages instead of panicking on a nil program
	send := func(msg tea.Msg) {
		if p != nil {
			p.Send(msg)
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
		events := agent.Events{
			OnText:  onText,
			OnThink: onThink,
			OnToolStart: func(id, n, a string) {
				flush()
				send(toolStartMsg{id, n, a})
			},
			OnToolEnd: func(id, n, r string) { send(toolEndMsg{id, n, r}) },
			// Detached send: snapshots are lossy progress, and a parked
			// p.Send must never wedge bashrun's ticker goroutine (the ABBA
			// lesson from docs/concurrency.md — same rule as sendTaskMsg).
			OnToolOutput: func(id, soFar string) { go send(toolOutputMsg{id, soFar}) },
			OnSteer: func(s string) {
				flush()
				send(steeredMsg(s))
			},
			OnCompacted: func(sum string, cutoff int) { send(compactMsg{summary: sum, cutoff: cutoff}) },
			OnUsage:     func(u llm.Usage) { send(usageMsg(u)) },
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
	m.append(youStyle.Render("❯ ") + linkifyFilePaths(text, realFileExists))
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
// after the turn ends).
func busyCmd(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "/help", "/theme", "/mouse", "/effort", "/tasks", "/cd", "/pwd", "/report":
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
		m.append(dimStyle.Render("◎ compacting…"))
		p := m.prog
		ag := m.agent // capture the current conversation for the summary call
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		go func() {
			took := len(ag.Messages)
			var summary string
			var cutoff int
			err := ag.ManualCompact(ctx, agent.Events{
				OnCompacted: func(s string, c int) { summary, cutoff = s, c },
			})
			if p != nil { // nil in headless tests; compaction still ran
				p.Send(compactMsg{took: took - len(ag.Messages), kept: len(ag.Messages), summary: summary, cutoff: cutoff, err: err})
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
	case "/tasks":
		if len(fields) > 1 { // /tasks <id>: jump straight into the detail view
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
		// We manage click/wheel reporting directly (no motion ?1002), so toggle
		// the escape ourselves rather than tea.EnableMouseCellMotion (which would
		// turn motion back on and break native drag-to-copy).
		if m.mouseOn {
			enableClickWheelMouse(os.Stdout)
			applyTmuxMouseFix()
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
		if m.busy {
			m.append(dimStyle.Render("(busy — /fork after this turn)"))
			return m, nil
		}
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
	case "/report":
		m.append(m.reportBlock())
	case "/help":
		m.append(dimStyle.Render(helpText()))
	case "/auth":
		m.authCommand(fields[1:])
	case "/model":
		if len(fields) < 2 {
			m.openModelPicker()
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
		m.switchModel(resolved, prov)
	default:
		m.append(errStyle.Render("unknown command " + fields[0]))
	}
	return m, nil
}

// compactCommand handles "/compact <args…>": off restores the built-in
// default compaction model, "<model> [provider]" selects one (persisted).
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
	if _, ok := m.cfg.Models[args[0]]; !ok {
		m.append(errStyle.Render("unknown model " + args[0]))
		return
	}
	m.compactModel = args[0]
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
	prov := m.compactProv
	if prov == "" {
		if mdl := m.cfg.Models[m.compactModel]; len(mdl.Providers) > 0 {
			prov = mdl.Providers[0]
		}
	}
	m.append(dimStyle.Render("◎ compaction model: " + m.compactModel + " @ " + prov))
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
		s = botStyle.Render("● ") + s
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
	v := m.viewBody()
	if m.height > 0 {
		m.viewH = lipgloss.Height(v)
		m.viewTop = max(min(m.viewTop, m.height-m.viewH), 0)
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
	b.WriteString(dimStyle.Render(left+strings.Repeat(" ", pad)) + toolStyle.Render(right) + "\n")
	if m.palette != nil {
		b.WriteString(m.paletteView())
		return b.String()
	}
	if m.picker != nil {
		b.WriteString(m.pickerView())
		return b.String()
	}
	if m.mpicker != nil {
		b.WriteString(m.modelPickerView())
		return b.String()
	}
	if m.taskVP != nil {
		b.WriteString(m.taskViewView())
		return b.String()
	}
	// One compact hint up top — the full roster lives behind the ctrl+p palette
	// and the /help command. The bottom hint covers the busy/interactive states.
	tips := "`ctrl+p` commands"
	b.WriteString(dimStyle.Render(tips) + "\n\n")
	b.WriteString(m.viewportView() + "\n") // selection highlight paints inside
	if m.curThink != "" {
		b.WriteString("\n" + m.thinkView() + "\n")
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
	if m.busy {
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
			line := ansi.Truncate(youStyle.Render(" ❯ ")+q, m.width, "…")
			if i == m.queueSel {
				line = ansi.Truncate(botStyle.Render(" → ")+q+dimStyle.Render("  (del to remove)"), m.width, "…")
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n")
	// the persistent background-subagent strip sits just above the input box
	if dock := m.tasksDock(); dock != "" {
		b.WriteString(dock + "\n")
	}
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
			b.WriteString(m.input.View())
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
	if m.menu != nil {
		b.WriteString("\n" + m.menuView())
	}
	b.WriteString("\n\n" + m.statusView()) // persistent status line, with a blank line above
	return b.String()
}

// statusView renders the always-on status line below the input: current
// directory, model (effort), provider, and session token spend. It mirrors
// the header's data but stays put while the transcript scrolls, so the four
// facts are always visible no matter where the viewport sits.
func (m *model) statusView() string {
	model := m.modelName
	if e := effortLabel(m.agent.Effort); e != "off" {
		model += " (" + e + ")"
	}
	u := m.agent.Usage()
	spend := fmt.Sprintf("%s/%s tok", fmtTok(u.PromptTokens), fmtTok(u.CompletionTokens))
	if c := u.Cached(); c > 0 {
		spend = fmt.Sprintf("%s(%s)/%s tok", fmtTok(u.PromptTokens), fmtTok(c), fmtTok(u.CompletionTokens))
	}
	if cost, ok := m.sessionCost(); ok {
		spend += " · " + fmtCost(cost)
	}
	line := fmt.Sprintf(" %s   %s   %s   %s", shortCWD(), model, m.provName, spend)
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
		rows = append(rows, previewBlock(youStyle.Render("❯ "), prev[0], m.width)...)
		rows = append(rows, previewBlock(botStyle.Render("● "), prev[1], m.width)...)
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
	var b strings.Builder
	for i := start; i < end; i++ {
		c := m.menu.cands[i]
		line := fmt.Sprintf("%-*s  ", nameW, c.Text)
		if i == m.menu.idx {
			b.WriteString(botStyle.Render("→ "+line) + dimStyle.Render(c.Desc))
		} else {
			b.WriteString("  " + line + dimStyle.Render(c.Desc))
		}
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
