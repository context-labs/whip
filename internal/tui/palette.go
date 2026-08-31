package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
)

// paletteItem is one row in the ctrl+p command palette. It mirrors opencode's
// DialogSelectOption: title + description + category header + a dimmed hint
// (the keybind or slash form — the palette teaches the shortcuts).
//
// Items are interactive: every row either toggles a live setting in place
// (enter, or ←/→ for reversible ones) or opens a sub-panel inside the palette
// where the change is explored and applied without leaving ctrl+p. Nothing
// closes the palette just to make a change — esc backs out instead.
type paletteItem struct {
	title    string // display name, e.g. "Model"
	category string // "Agent", "Session", "Display", "App"

	// dynDesc/dynHint render live state, so the palette always shows the
	// current value instead of a static description.
	dynDesc func(m *model) string
	dynHint func(m *model) string

	suggested bool // pinned into a "Suggested" group when the filter is empty

	// action rows: enter runs it (palette stays open)
	run func(m *model) (tea.Model, tea.Cmd)

	// sub-panel rows: enter/→ drills in (push), esc pops back
	panel func(m *model) *ppanel

	// reversible rows: ←/→ step the value backward/forward without a panel
	stepBack func(m *model)
	stepFwd  func(m *model)
}

// panelKind enumerates the palette's sub-panels.
type panelKind int

const (
	panelSubagent panelKind = iota
	panelModel
	panelEffort
	panelGoal
	panelCompact
	panelTheme
	panelBrowser
	panelMCP
)

// mcpRow is one row in the MCPs sub-panel: a source-toggle header (claude/
// codex imports) or one configured server.
type mcpRow struct {
	name     string // server name, or "claude"/"codex" for source rows
	source   bool   // source-toggle row
	on       bool   // current toggle state
	detail   string // status ("ready · 4 tools", "disabled", "blocked by mcpImport config")
	filtered bool   // source has only/exclude name filters (config-file only)
	disabled bool   // row can't toggle (a policy-blocked server)
}

// ppanel is a palette sub-panel: the interactive editor behind a row. Key
// handling switches on kind; the slice fields hold whatever that kind lists
// (models, effort levels, …).
type ppanel struct {
	kind  panelKind
	title string

	items []modelItem // panelModel: flattened model@provider routes
	idx   int

	levels []string // panelEffort: available levels ("" = off)
	lidx   int

	prepare string // panelGoal: text submitted when the editor closes

	list []string // panelCompact/panelSubagent: "default (…)" + every known model (config then catalog "(new)"); panelTheme: {"auto","light","dark"}
	midx int      // panelCompact/panelSubagent/panelTheme/panelBrowser/panelMCP: selection

	filter modelFilter // panelCompact/panelSubagent: type-to-filter over list

	note string // panelCompact/panelSubagent: dim footer note (stale catalog hint)

	mcps []mcpRow // panelMCP: the two source toggles then one row per server

	err string // inline error from a failed apply (bad compact model, …)

	// direct marks a panel a slash command opened straight into (bare /effort,
	// /theme): enter applies and closes the whole palette instead of popping
	// back to the root list, since the user never asked for ctrl+p.
	direct bool
}

// palette is the ctrl+p command palette: a modal full-screen dialog with its
// own filter line (opencode's DialogSelect). Typing fuzzy-filters, ↑/↓ moves,
// enter applies or drills in, ←/→ steps reversible settings, esc pops a level.
type palette struct {
	items  []paletteItem // filtered
	all    []paletteItem // unfiltered
	idx    int
	filter string
	stack  []*ppanel
}

// Hint/keybind constants for the palette-only rows that don't dispatch
// through the command switch. /help renders from these too, so a keybind or
// description lives in exactly one place.
const (
	palHintRewind   = "esc esc"
	palDescRewind   = "rewind the conversation"
	palHintThinking = "ctrl+o"
	palHintQuit     = "ctrl+c ctrl+c"
)

// slashHint looks a command's one-liner up in the registry so the palette
// and /help can never disagree about what a command does.
func slashHint(m *model, name string) string {
	if e := registryFind(name); e != nil {
		return e.Hint
	}
	return name
}

func (m *model) paletteItems() []paletteItem {
	return []paletteItem{
		{
			title: "Model", category: "Agent", suggested: true,
			// first suggestion: ctrl+p → enter opens the model panel directly
			dynDesc: func(m *model) string { return m.modelName + " @ " + m.provName },
			dynHint: func(m *model) string { return "/model · tab" },
			panel: func(m *model) *ppanel {
				items := buildModelItems(m.cfg)
				if len(items) == 0 {
					return nil
				}
				pp := &ppanel{kind: panelModel, title: "Model", items: items}
				for i, it := range items { // start on the active route
					if it.model == m.modelName && it.provider == m.provName {
						pp.idx = i
						break
					}
				}
				return pp
			},
		},
		{
			title: "Reasoning effort", category: "Agent",
			dynDesc: func(m *model) string {
				return "thinking level for " + m.agent.Model
			},
			dynHint: func(m *model) string { return "/effort " + slashHint(m, "/effort") },
			panel: func(m *model) *ppanel {
				levels := m.effortsFor()
				pp := &ppanel{kind: panelEffort, title: "Reasoning effort", levels: levels}
				for i, e := range levels {
					if e == m.agent.Effort {
						pp.lidx = i
						break
					}
				}
				return pp
			},
			stepBack: func(m *model) { m.setEffort(prevEffort(m.effortsFor(), m.agent.Effort)) },
			stepFwd:  func(m *model) { m.setEffort(nextEffort(m.effortsFor(), m.agent.Effort)) },
		},
		{
			title: "Resume session", category: "Session", suggested: true,
			dynDesc: func(m *model) string { return slashHint(m, "/resume") },
			dynHint: func(m *model) string { return "/resume" },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.palette = nil
				m.openPicker()
				return m, nil
			},
		},
		{
			title: "Rewind conversation", category: "Session", suggested: true,
			dynDesc: func(m *model) string {
				if len(m.future) > 0 {
					return "rewound — browse to go back further or forward again"
				}
				return "jump back (or forward) to any earlier message"
			},
			dynHint: func(m *model) string { return palHintRewind },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.palette = nil
				if m.busy {
					return m, nil
				}
				m.openRewind()
				return m, nil
			},
		},
		{
			title: "Fork session", category: "Session",
			dynDesc: func(m *model) string { return slashHint(m, "/fork") },
			dynHint: func(m *model) string { return "/fork" },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.palette = nil
				m.forkCommand("") // works mid-turn: copies now, switches at turn end
				return m, nil
			},
		},
		{
			title: "Rename session", category: "Session",
			dynDesc: func(m *model) string {
				if m.sessionID == "" || m.store == nil {
					return "retitle this session"
				}
				if meta, _, err := m.store.Load(m.sessionID); err == nil && meta.Title != "" {
					return meta.Title
				}
				return "retitle this session"
			},
			dynHint: func(m *model) string { return "/rename " + slashHint(m, "/rename") },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.palette = nil
				if !m.busy {
					m.renameCommand("")
				}
				return m, nil
			},
		},
		{
			title: "New session", category: "Session",
			dynDesc: func(m *model) string { return slashHint(m, "/clear") },
			dynHint: func(m *model) string { return "/clear" },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.palette = nil
				return m.command("/clear")
			},
		},
		{
			title: "Compact session", category: "Session", suggested: true,
			dynDesc: func(m *model) string { return slashHint(m, "/compact") },
			dynHint: func(m *model) string { return "/compact" },
			run:     func(m *model) (tea.Model, tea.Cmd) { return m.command("/compact") },
		},
		{
			title: "Context doctor", category: "Session",
			dynDesc: func(m *model) string { return slashHint(m, "/context-doctor") },
			dynHint: func(m *model) string { return "/context-doctor" },
			run:     func(m *model) (tea.Model, tea.Cmd) { return m.command("/context-doctor") },
		},
		{
			title: "Bug report", category: "Session",
			dynDesc: func(m *model) string { return slashHint(m, "/report") },
			dynHint: func(m *model) string { return "/report" },
			run:     func(m *model) (tea.Model, tea.Cmd) { return m.command("/report") },
		},
		{
			title: "MCPs", category: "Session",
			dynDesc: func(m *model) string { return slashHint(m, "/mcp") + "; toggle claude/codex imports" }, // live count: [n/n ready] badge
			dynHint: func(m *model) string { return "/mcp" },
			panel: func(m *model) *ppanel {
				rows := m.buildMCPRows()
				if len(rows) == 0 {
					return nil
				}
				return &ppanel{kind: panelMCP, title: "MCPs", mcps: rows}
			},
		},
		{
			title: "Compaction model", category: "Session",
			dynDesc: func(m *model) string {
				if m.compactModel == "" {
					return "default (" + config.DefaultCompactModel + ")"
				}
				return m.compactModel
			},
			dynHint: func(m *model) string { return "/compact <model>" },
			panel: func(m *model) *ppanel {
				names := modelNamesFor(m.cfg)
				pp := &ppanel{
					kind:  panelCompact,
					title: "Compaction model",
					list:  append([]string{"default (" + config.DefaultCompactModel + ")"}, names...),
				}
				for i, name := range pp.list {
					if name == m.compactModel {
						pp.midx = i
						break
					}
				}
				if st := staleCatalogs(m.cfg, config.LoadCatalogs()); len(st) > 0 {
					pp.note = "catalog stale for " + strings.Join(st, ", ") + " — /model refresh pulls newly announced models"
				}
				return pp
			},
		},
		{
			title: "Compaction level", category: "Session",
			dynDesc: func(m *model) string {
				return "auto-compact at this share of the context window"
			},
			dynHint:  func(m *model) string { return "←/→" },
			stepBack: func(m *model) { m.setCompactPct(m.compactPct() - 10) },
			stepFwd:  func(m *model) { m.setCompactPct(m.compactPct() + 10) },
		},
		{
			title: "Goal", category: "Session",
			dynDesc: func(m *model) string {
				if m.goal == "" {
					return fmt.Sprintf("keep working until the goal is met (max %d rounds)", m.goalMaxRounds())
				}
				return truncLine(m.goal, 40)
			},
			dynHint: func(m *model) string { return "/goal " + slashHint(m, "/goal") },
			panel: func(m *model) *ppanel {
				pp := &ppanel{kind: panelGoal, title: "Goal", prepare: m.goal}
				return pp
			},
		},
		// After "Goal": the "goal" filter fuzzy-matches this row's haystack too
		// ("SubAgent model Session" contains g→o→a→l), and first match wins —
		// Goal is the exact hit, so it must sit earlier.
		{
			title: "Subagent model", category: "Session",
			dynDesc: func(m *model) string {
				if m.cfg.TaskModel == "" {
					return "default (" + config.DefaultTaskModel + ")"
				}
				return m.cfg.TaskModel
			},
			dynHint: func(m *model) string { return "config taskModel" },
			panel: func(m *model) *ppanel {
				names := modelNamesFor(m.cfg)
				pp := &ppanel{
					kind:  panelSubagent,
					title: "Subagent model",
					list:  append([]string{"default (" + config.DefaultTaskModel + ")"}, names...),
				}
				for i, name := range pp.list {
					if name == m.cfg.TaskModel {
						pp.midx = i
						break
					}
				}
				if st := staleCatalogs(m.cfg, config.LoadCatalogs()); len(st) > 0 {
					pp.note = "catalog stale for " + strings.Join(st, ", ") + " — /model refresh pulls newly announced models"
				}
				return pp
			},
		},
		{
			title: "Thinking tokens", category: "Display",
			dynDesc: func(m *model) string { return "show or hide model reasoning" },
			dynHint: func(m *model) string { return palHintThinking },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.toggleThinking()
				return m, nil
			},
			stepBack: func(m *model) { m.setThinking(false) },
			stepFwd:  func(m *model) { m.setThinking(true) },
		},
		{
			title: "Theme", category: "Display",
			dynDesc: func(m *model) string { return "current: " + CurrentTheme() },
			dynHint: func(m *model) string { return "/theme " + slashHint(m, "/theme") },
			panel: func(m *model) *ppanel {
				list := []string{"auto", "light", "dark"}
				cur := m.cfg.Theme
				if cur == "" {
					cur = "auto"
				}
				pp := &ppanel{kind: panelTheme, title: "Theme", list: list}
				for i, t := range list {
					if t == cur {
						pp.midx = i
						break
					}
				}
				return pp
			},
			stepBack: func(m *model) { m.setTheme("light") },
			stepFwd:  func(m *model) { m.setTheme("dark") },
		},
		{
			title: "Browser driver", category: "Display",
			dynDesc: func(m *model) string {
				return "current: " + browser.Driver + " — which automation engine drives Chrome"
			},
			dynHint: func(m *model) string { return "WHIP_BROWSER_DRIVER" },
			panel: func(m *model) *ppanel {
				list := browser.Drivers
				pp := &ppanel{kind: panelBrowser, title: "Browser driver", list: list}
				for i, d := range list {
					if d == browser.Driver {
						pp.midx = i
						break
					}
				}
				return pp
			},
			stepBack: func(m *model) { m.switchBrowserDriver(browser.DriverRod) },
			stepFwd:  func(m *model) { m.switchBrowserDriver(browser.DriverChromedp) },
		},
		{
			title: "Mouse capture", category: "Display",
			dynDesc: func(m *model) string { return slashHint(m, "/mouse") },
			dynHint: func(m *model) string { return "/mouse" },
			run: func(m *model) (tea.Model, tea.Cmd) {
				return m.command("/mouse")
			},
			stepBack: func(m *model) { m.setMouse(false) },
			stepFwd:  func(m *model) { m.setMouse(true) },
		},
		{
			title: "Help", category: "App",
			dynDesc: func(m *model) string { return slashHint(m, "/help") },
			dynHint: func(m *model) string { return "/help" },
			run: func(m *model) (tea.Model, tea.Cmd) {
				m.palette = nil
				return m.command("/help")
			},
		},
		{
			title: "Quit", category: "App",
			dynDesc: func(m *model) string { return "exit whip" },
			dynHint: func(m *model) string { return "/quit · " + palHintQuit },
			run:     func(m *model) (tea.Model, tea.Cmd) { return m, tea.Quit },
		},
	}
}

// setMouse applies a mouse-capture state (the palette's reversible steppers
// need to set an explicit value; /mouse toggles).
func (m *model) setMouse(on bool) {
	if m.mouseOn == on {
		return
	}
	m.command("/mouse")
}

func (m *model) openPalette() {
	all := m.paletteItems()
	m.palette = &palette{all: all}
	m.palette.applyFilter(m)
}

// openPaletteOn opens the palette and drills straight into the named row's
// sub-panel (used by bare slash commands like /theme that should land on a
// switcher, not toggle blindly). The invocation counts as being inside the
// panel — not the palette — so enter applies AND closes; esc pops back to
// the root list.
func (m *model) openPaletteOn(title string) {
	m.openPalette()
	for i, it := range m.palette.items {
		if strings.EqualFold(it.title, title) && it.panel != nil {
			m.palette.idx = i
			pp := it.panel(m)
			pp.direct = true
			m.palette.stack = append(m.palette.stack, pp)
			return
		}
	}
}

// paletteFilterMatch is a cheap fuzzy match: all query runes must appear in
// order across title+category (case-insensitive). Good enough for ~10 rows
// without pulling in fuzzysort.
func paletteFilterMatch(query, hay string) bool {
	if query == "" {
		return true
	}
	hay = strings.ToLower(hay)
	for _, r := range strings.ToLower(query) {
		i := strings.IndexRune(hay, r)
		if i < 0 {
			return false
		}
		hay = hay[i+1:]
	}
	return true
}

// itemHaystack is the text a filter query matches against: title, category,
// and the slash name (not the hint's usage text — "new sess" shouldn't match
// /goal's "resume" in its hint).
func itemHaystack(m *model, it paletteItem) string {
	s := it.title + " " + it.category
	if it.dynHint != nil {
		if f := strings.Fields(it.dynHint(m)); len(f) > 0 && strings.HasPrefix(f[0], "/") {
			s += " " + f[0]
		}
	}
	return s
}

// applyFilter recomputes the visible rows. With an empty filter,
// suggested entries pin into a "Suggested" category on top (opencode), then
// everything else grouped by category.
func (p *palette) applyFilter(m *model) {
	q := p.filter
	var items []paletteItem
	for _, it := range p.all {
		if paletteFilterMatch(q, itemHaystack(m, it)) {
			items = append(items, it)
		}
	}
	// stable category grouping (first-seen order)
	seen := map[string]bool{}
	var cats []string
	for _, it := range items {
		if !seen[it.category] {
			seen[it.category] = true
			cats = append(cats, it.category)
		}
	}
	var grouped []paletteItem
	for _, c := range cats {
		for _, it := range items {
			if it.category == c {
				grouped = append(grouped, it)
			}
		}
	}
	if q == "" {
		var sugg []paletteItem
		for _, it := range grouped {
			if it.suggested {
				sugg = append(sugg, it)
			}
		}
		if len(sugg) > 0 {
			for i := range sugg {
				sugg[i].category = "Suggested"
			}
			grouped = append(sugg, grouped...)
		}
	}
	p.items = grouped
	if p.idx >= len(p.items) {
		p.idx = max(len(p.items)-1, 0)
	}
}

// selected returns the highlighted row (nil when the filter matched nothing).
func (p *palette) selected() *paletteItem {
	if len(p.items) == 0 {
		return nil
	}
	return &p.items[p.idx]
}

// top returns the active sub-panel (nil = the root command list).
func (p *palette) top() *ppanel {
	if len(p.stack) == 0 {
		return nil
	}
	return p.stack[len(p.stack)-1]
}

// move moves the root-list selection by delta, wrapping at both ends.
func (p *palette) move(delta int) {
	n := len(p.items)
	if n == 0 {
		return
	}
	p.idx = (p.idx + delta + n) % n
}

// paletteKey handles input while the palette is open: esc pops one level
// (sub-panel → root list → closed), typing edits the filter or the active
// sub-panel's editor.
func (m *model) paletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.palette
	if pp := p.top(); pp != nil {
		return m.panelKey(msg, pp)
	}
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.palette = nil
	case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
		p.move(-1)
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
		p.move(1)
	case tea.KeyLeft:
		if it := p.selected(); it != nil && it.stepBack != nil {
			it.stepBack(m)
		}
	case tea.KeyRight:
		it := p.selected()
		if it == nil {
			break
		}
		if it.stepFwd != nil {
			it.stepFwd(m)
		} else if it.panel != nil {
			m.pushPanel(it)
		}
	case tea.KeyEnter:
		it := p.selected()
		if it == nil {
			return m, nil
		}
		switch {
		case it.panel != nil:
			m.pushPanel(it)
		case it.run != nil:
			return it.run(m)
		}
	case tea.KeyBackspace, tea.KeyDelete:
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.applyFilter(m)
		}
	case tea.KeyRunes:
		p.filter += string(msg.Runes)
		p.idx = 0
		p.applyFilter(m)
	}
	return m, nil
}

// pushPanel drills into an item's sub-panel. Items whose setting can't be
// listed (no models configured) fail in place with a transcript note.
func (m *model) pushPanel(it *paletteItem) {
	pp := it.panel(m)
	if pp == nil {
		m.append(errStyle.Render(it.title + ": nothing to choose from (check ~/.whip/config.json)"))
		return
	}
	m.palette.stack = append(m.palette.stack, pp)
}

// panelKey routes keys inside a sub-panel: esc applies-and-pops (goal) or
// just pops, ↑/↓ moves, enter applies.
func (m *model) panelKey(msg tea.KeyMsg, pp *ppanel) (tea.Model, tea.Cmd) {
	p := m.palette
	pop := func() {
		p.stack = p.stack[:len(p.stack)-1]
		// a slash command opened this panel directly (bare /effort, /theme):
		// commit-and-close, never land on the root list the user didn't open
		if pp.direct && len(p.stack) == 0 {
			m.palette = nil
		}
	}

	switch pp.kind {
	case panelModel:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			pop()
		case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
			pp.idx = (pp.idx - 1 + len(pp.items)) % len(pp.items)
			m.previewModel(pp.items[pp.idx])
		case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
			pp.idx = (pp.idx + 1) % len(pp.items)
			m.previewModel(pp.items[pp.idx])
		case tea.KeyEnter:
			it := pp.items[pp.idx]
			m.switchModel(it.model, it.provider, true)
			pop()
		}

	case panelEffort:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			pop()
		case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
			pp.lidx = (pp.lidx - 1 + len(pp.levels)) % len(pp.levels)
		case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
			pp.lidx = (pp.lidx + 1) % len(pp.levels)
		case tea.KeyLeft, tea.KeyRight, tea.KeyEnter:
			// ←/→ and enter all apply the highlighted level: selecting is the
			// point of the panel, so any confirm key is a commitment
			m.setEffort(pp.levels[pp.lidx])
			if msg.Type == tea.KeyEnter {
				pop()
			}
		}

	case panelSubagent, panelCompact:
		// compact and subagent share the model-list interaction: type-to-filter,
		// ↑/↓ move over the filtered rows, ←/→/enter apply the highlighted one.
		view := pp.filter.view(len(pp.list))
		apply := func() {
			if len(view) == 0 || pp.midx >= len(view) {
				return
			}
			row := view[pp.midx]
			name := strings.TrimSuffix(pp.list[row], dimNew)
			pp.err = ""
			if row == 0 { // the "default (…)" row
				if pp.kind == panelCompact {
					m.compactCommand([]string{"off"})
				} else {
					m.subagentModelCommand([]string{"off"})
				}
				return
			}
			if pp.kind == panelCompact {
				m.compactCommand([]string{name})
				if m.compactModel != name {
					pp.err = "couldn't resolve " + name + " — kept previous"
				}
			} else {
				m.subagentModelCommand([]string{name})
				if m.cfg.TaskModel != name {
					pp.err = "couldn't resolve " + name + " — kept previous"
				}
			}
		}
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			pop()
		case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
			if len(view) > 0 {
				pp.midx = (pp.midx - 1 + len(view)) % len(view)
			}
		case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
			if len(view) > 0 {
				pp.midx = (pp.midx + 1) % len(view)
			}
		case tea.KeyLeft, tea.KeyRight:
			apply()
		case tea.KeyEnter:
			apply()
			if pp.err == "" {
				pop()
			}
		case tea.KeyBackspace, tea.KeyDelete:
			if pp.filter.backspace() {
				pp.filter.applyModelList(pp.list)
				pp.midx = 0
			}
		case tea.KeyRunes, tea.KeySpace:
			if pp.filter.typeRunes(msg.Runes) {
				pp.filter.applyModelList(pp.list)
				pp.midx = 0
			}
		}

	case panelTheme:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			pop()
		case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
			pp.midx = (pp.midx - 1 + len(pp.list)) % len(pp.list)
		case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
			pp.midx = (pp.midx + 1) % len(pp.list)
		case tea.KeyLeft, tea.KeyRight, tea.KeyEnter:
			m.setTheme(pp.list[pp.midx]) // applies live; re-renders the transcript
			if msg.Type == tea.KeyEnter {
				pop()
			}
		}

	case panelBrowser:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			pop()
		case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
			pp.midx = (pp.midx - 1 + len(pp.list)) % len(pp.list)
		case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
			pp.midx = (pp.midx + 1) % len(pp.list)
		case tea.KeyLeft, tea.KeyRight, tea.KeyEnter:
			m.switchBrowserDriver(pp.list[pp.midx])
			if msg.Type == tea.KeyEnter {
				pop()
			}
		}

	case panelMCP:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			pop()
		case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
			pp.midx = (pp.midx - 1 + len(pp.mcps)) % len(pp.mcps)
		case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
			pp.midx = (pp.midx + 1) % len(pp.mcps)
		case tea.KeyLeft, tea.KeyRight, tea.KeyEnter:
			row := &pp.mcps[pp.midx]
			if row.disabled {
				return m, nil // policy-blocked server: the note is the action
			}
			if row.source {
				m.mcpSetImport(row.name, !row.on)
			} else {
				m.mcpSetEnabled(row.name, !row.on)
			}
			// Rebuild in place so the checkbox flips visibly without leaving
			// the panel (mcpSetEnabled/mcpSetImport appended the transcript note).
			pp.mcps = m.buildMCPRows()
			if pp.midx >= len(pp.mcps) {
				pp.midx = len(pp.mcps) - 1
			}
		}

	case panelGoal:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.commitGoal(pp) // esc applies too — the editor is the goal
			pop()
		case tea.KeyEnter:
			m.commitGoal(pp)
			pop()
		case tea.KeyBackspace, tea.KeyDelete:
			if len(pp.prepare) > 0 {
				pp.prepare = pp.prepare[:len(pp.prepare)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			pp.prepare += string(msg.Runes)
		}
	}
	return m, nil
}

// previewModel switches live as the model panel browses, without persisting:
// the pick becomes the default only on enter (switchModel).
func (m *model) previewModel(it modelItem) {
	if it.model == m.modelName && it.provider == m.provName {
		return
	}
	ag, mn, pn, err := buildAgent(m.cfg, it.model, it.provider, m.sysPrompt)
	if err != nil {
		return // unresolved routes stay visible but unselectable-feeling
	}
	ag.Effort = m.agent.Effort
	ag.Messages = append(ag.Messages, m.agent.Messages[1:]...) // carry history
	ag.CompactClient, ag.CompactModel = m.agent.CompactClient, m.agent.CompactModel
	ag.CompactThreshold = m.agent.CompactThreshold
	m.agent, m.modelName, m.provName = ag, mn, pn
	m.applyTaskModel()
	if !slices.Contains(m.effortsFor(), ag.Effort) {
		m.setEffort("") // the previewed model doesn't support the current level
	}
}

// commitGoal applies the goal panel's text: set, clear (empty), or resume.
// Resuming an unchanged goal continues with the check prompt; a fresh or
// edited goal starts at round 0 (mirrors /goal resume vs /goal <text>).
func (m *model) commitGoal(pp *ppanel) {
	goal := strings.TrimSpace(pp.prepare)
	if goal == m.goal {
		if goal != "" && !m.busy {
			m.goalRounds = 0
			m.append(dimStyle.Render("◎ resuming goal: " + goal))
			m.submitGoal(goalContinuePrompt(goal))
		}
		return
	}
	m.setGoal(goal)
	if goal == "" {
		m.append(dimStyle.Render("(goal cleared)"))
		return
	}
	m.append(dimStyle.Render("◎ goal set: " + goal))
	if !m.busy {
		m.submit(goal)
	}
}

// prevEffort mirrors nextEffort in reverse for ← stepping.
func prevEffort(levels []string, cur string) string {
	for i, e := range levels {
		if e == cur {
			return levels[(i-1+len(levels))%len(levels)]
		}
	}
	return levels[0]
}

// paletteView renders the modal dialog: a title bar, the filter line, and
// category-grouped rows with dimmed hints. A sub-panel replaces the list.
func (m *model) paletteView() string {
	p := m.palette
	var b strings.Builder
	title := " Commands"
	if pp := p.top(); pp != nil {
		title = " Commands › " + pp.title
	}
	b.WriteString(botStyle.Render(title))
	if p.top() == nil && p.filter != "" {
		b.WriteString(dimStyle.Render("  — type to filter"))
	}
	b.WriteString("\n\n")

	if pp := p.top(); pp != nil {
		b.WriteString(m.panelView(pp))
		return b.String()
	}

	b.WriteString(" " + youStyle.Render("❯ ") + p.filter + dimStyle.Render("█"))
	b.WriteString("\n\n")

	lastCat := ""
	hintW := 0
	for _, it := range p.items {
		if it.dynHint != nil {
			hintW = max(hintW, len(it.dynHint(m)))
		}
	}
	for i, it := range p.items {
		if it.category != lastCat {
			if lastCat != "" {
				b.WriteString("\n")
			}
			b.WriteString(dimStyle.Render("  " + it.category))
			b.WriteString("\n")
			lastCat = it.category
		}
		hint := ""
		if it.dynHint != nil {
			hint = dimStyle.Render(fmt.Sprintf("%*s", hintW, it.dynHint(m)))
		}
		line := " " + it.title
		if it.dynDesc != nil {
			line += dimStyle.Render("  — " + it.dynDesc(m))
		}
		state := paletteState(m, it)
		if i == p.idx {
			b.WriteString(botStyle.Render("→") + line + state + "  " + hint)
		} else {
			b.WriteString(" " + line + state + "  " + hint)
		}
		b.WriteString("\n")
	}
	if len(p.items) == 0 {
		b.WriteString(dimStyle.Render("  (no matches)"))
		b.WriteString("\n")
	}
	b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  (%d/%d) ↑/↓ select · enter open/apply · ←/→ change · esc close",
		min(p.idx+1, len(p.items)), len(p.items))))
	return b.String()
}

// paletteState renders a row's live value (toggle state, effort level, …).
func paletteState(m *model, it paletteItem) string {
	switch it.title {
	case "Reasoning effort":
		return dimStyle.Render("  [" + effortLabel(m.agent.Effort) + "]")
	case "Thinking tokens":
		return dimStyle.Render("  [" + onOff(m.showThinking) + "]")
	case "Mouse capture":
		return dimStyle.Render("  [" + onOff(m.mouseOn) + "]")
	case "Goal":
		if m.goal != "" {
			return dimStyle.Render("  [on]")
		}
	case "Compaction level":
		return dimStyle.Render(fmt.Sprintf("  [%d%%]", m.compactPct()))
	case "MCPs":
		if m.mcpMgr == nil {
			return ""
		}
		ready, total := 0, 0
		for _, st := range m.mcpMgr.Statuses() {
			total++
			if st.Status == mcp.StatusReady {
				ready++
			}
		}
		return dimStyle.Render(fmt.Sprintf("  [%d/%d ready]", ready, total))
	}
	return ""
}

// panelView renders the active sub-panel.
func (m *model) panelView(pp *ppanel) string {
	var b strings.Builder
	switch pp.kind {
	case panelModel:
		lastModel := ""
		for i, it := range pp.items {
			if it.model != lastModel {
				heading := " " + it.model
				if it.fromCatalog {
					heading = dimStyle.Render(heading + dimNew)
				}
				b.WriteString(heading + "\n")
				lastModel = it.model
			}
			cur := ""
			if it.model == m.modelName && it.provider == m.provName {
				cur = dimStyle.Render("  (current)")
			}
			line := fmt.Sprintf("%-12s  ", it.provider) + dimStyle.Render(it.url)
			if it.fromCatalog {
				line = dimStyle.Render(line)
			}
			if i == pp.idx {
				b.WriteString(botStyle.Render("   → "+line) + cur + "\n")
			} else {
				b.WriteString("     " + line + cur + "\n")
			}
		}
		b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  (%d/%d) ↑/↓ preview · enter switch · esc back", pp.idx+1, len(pp.items))))

	case panelEffort:
		for i, e := range pp.levels {
			cur := ""
			if e == m.agent.Effort {
				cur = dimStyle.Render("  (current)")
			}
			if i == pp.lidx {
				b.WriteString(botStyle.Render(" → "+effortLabel(e)) + cur + "\n")
			} else {
				b.WriteString("   " + effortLabel(e) + cur + "\n")
			}
		}
		b.WriteString("\n" + dimStyle.Render("  ↑/↓ select · enter/←/→ apply · esc back"))

	case panelSubagent, panelCompact:
		// compact and subagent share the model-list render: a "/" query line,
		// the filtered rows, then err/note/footer.
		b.WriteString("  " + botStyle.Render("/") + pp.filter.query + dimStyle.Render("▏") + "\n")
		view := pp.filter.view(len(pp.list))
		for i, row := range view {
			name := pp.list[row]
			cur := ""
			current := m.compactModel
			if pp.kind == panelSubagent {
				current = m.cfg.TaskModel
			}
			if (row == 0 && current == "") || (row > 0 && name == current) {
				cur = dimStyle.Render("  (current)")
			}
			line := name + cur
			if strings.HasSuffix(name, dimNew) {
				line = dimStyle.Render(line)
			}
			if i == pp.midx {
				b.WriteString(botStyle.Render(" → "+line) + "\n")
			} else {
				b.WriteString("   " + line + "\n")
			}
		}
		if len(view) == 0 {
			b.WriteString(dimStyle.Render("  no models match "+strconv.Quote(pp.filter.query)) + "\n")
		}
		if pp.err != "" {
			b.WriteString(errStyle.Render("  "+pp.err) + "\n")
		}
		if pp.note != "" {
			b.WriteString(dimStyle.Render("  "+pp.note) + "\n")
		}
		b.WriteString("\n" + dimStyle.Render("  type to filter · ↑/↓ select · enter/←/→ apply · esc back"))

	case panelTheme:
		cur := m.cfg.Theme
		if cur == "" {
			cur = "auto"
		}
		for i, name := range pp.list {
			mark := ""
			if name == cur {
				mark = dimStyle.Render("  (current)")
			}
			if i == pp.midx {
				b.WriteString(botStyle.Render(" → "+name) + mark + "\n")
			} else {
				b.WriteString("   " + name + mark + "\n")
			}
		}
		b.WriteString("\n" + dimStyle.Render("  ↑/↓ select · enter/←/→ apply · esc back"))

	case panelBrowser:
		for i, name := range pp.list {
			mark := ""
			if name == browser.Driver {
				mark = dimStyle.Render("  (current)")
			}
			if i == pp.midx {
				b.WriteString(botStyle.Render(" → "+name) + mark + "\n")
			} else {
				b.WriteString("   " + name + mark + "\n")
			}
		}
		b.WriteString("\n" + dimStyle.Render("  ↑/↓ select · enter/←/→ apply · esc back"))

	case panelGoal:
		b.WriteString(" " + youStyle.Render("❯ ") + pp.prepare + dimStyle.Render("█"))
		b.WriteString("\n\n" + dimStyle.Render(fmt.Sprintf("  type the goal · empty clears · enter/esc apply · max %d rounds (/goal rounds)", m.goalMaxRounds())))

	case panelMCP:
		for i, row := range pp.mcps {
			box := "[x]"
			if !row.on {
				box = "[ ]"
			}
			label := row.name
			if row.source {
				label = map[string]string{"claude": "Import Claude MCPs", "codex": "Import Codex MCPs"}[row.name]
			}
			line := fmt.Sprintf("%s %-22s %s", box, label, dimStyle.Render(row.detail))
			if row.filtered {
				line += dimStyle.Render("  (name filters set — edit config)")
			}
			if row.disabled {
				line = dimStyle.Render(line)
			}
			if i == pp.midx {
				b.WriteString(botStyle.Render(" → "+line) + "\n")
			} else {
				b.WriteString("   " + line + "\n")
			}
		}
		b.WriteString("\n" + dimStyle.Render("  ↑/↓ select · enter/←/→ toggle · esc back · /mcp for reconnect"))
	}
	b.WriteString("\n")
	return b.String()
}
