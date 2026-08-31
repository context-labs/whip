package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
)

func TestPaletteOpensAndClosesOnEsc(t *testing.T) {
	m := compactCmdModel()
	tm, _ := m.key(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = tm.(*model)
	if m.palette == nil {
		t.Fatal("ctrl+p should open the palette")
	}
	// esc pops the dialog (opencode: esc pops one level)
	tm, _ = m.key(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(*model)
	if m.palette != nil {
		t.Fatal("esc should close the palette")
	}
}

func TestPaletteSuggestedGroupOnTop(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	if m.palette.items[0].category != "Suggested" {
		t.Fatalf("empty filter should pin a Suggested group, got %q", m.palette.items[0].category)
	}
	titles := map[string]bool{}
	for _, it := range m.palette.items {
		titles[it.title] = true
	}
	for _, want := range []string{"Model", "Resume session", "Compact session", "Goal", "Help", "Quit"} {
		if !titles[want] {
			t.Errorf("palette missing %q", want)
		}
	}
}

func TestPaletteFilter(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	for _, r := range "new sess" {
		tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	if len(m.palette.items) != 1 || m.palette.items[0].title != "New session" {
		t.Fatalf("filter 'new sess': %+v", m.palette.items)
	}
	if m.palette.items[0].category != "Session" {
		t.Fatalf("filtering drops the Suggested group, got %q", m.palette.items[0].category)
	}
	// backspace restores the full list
	for range 8 {
		tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyBackspace})
		m = tm.(*model)
	}
	if len(m.palette.items) < 10 {
		t.Fatalf("backspace should restore all items, got %d", len(m.palette.items))
	}
}

func TestPaletteNavigationWraps(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	n := len(m.palette.items)
	// up from the top wraps to the bottom
	tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyUp})
	m = tm.(*model)
	if m.palette.idx != n-1 {
		t.Fatalf("up from 0 should wrap to %d, got %d", n-1, m.palette.idx)
	}
	// down from the bottom wraps to the top
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(*model)
	if m.palette.idx != 0 {
		t.Fatalf("down should wrap to 0, got %d", m.palette.idx)
	}
}

func TestPaletteEnterRunsCommand(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	for _, r := range "quit" {
		tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	_, cmd := m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Quit should return tea.Quit")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Fatalf("expected tea.QuitMsg, got %v", msg)
	}
}

func TestPaletteViewRendersCategories(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	m.width = 100
	v := m.paletteView()
	for _, want := range []string{"Commands", "Suggested", "Agent", "Session", "Display", "App", "esc close"} {
		if !strings.Contains(v, want) {
			t.Errorf("palette view missing %q:\n%s", want, v)
		}
	}
}

// The palette must not swallow the agent's interrupt keys while a turn runs:
// it routes through key() only as a modal, and ctrl+c closes it like esc.
func TestPaletteCtrlCClosesNotQuits(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	tm, _ := m.key(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(*model)
	if m.palette != nil {
		t.Fatal("ctrl+c should close the palette, not quit the app")
	}
}

// Reversible rows change the setting in place with ←/→ while the palette
// stays open — the core of the interactive palette.
func TestPaletteArrowsStepEffortInPlace(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	var tm tea.Model
	for _, r := range "effort" {
		tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	if m.palette.items[m.palette.idx].title != "Reasoning effort" {
		t.Fatalf("filter 'effort' should select Reasoning effort")
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRight})
	m = tm.(*model)
	if m.palette == nil {
		t.Fatal("→ must keep the palette open")
	}
	if m.agent.Effort != "low" {
		t.Fatalf("→ should step off → low, got %q", m.agent.Effort)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyLeft})
	m = tm.(*model)
	if m.agent.Effort != "" {
		t.Fatalf("← should step back to off, got %q", m.agent.Effort)
	}
}

// Toggles apply in place too: enter flips thinking tokens, palette open.
func TestPaletteToggleThinkingInPlace(t *testing.T) {
	m := compactCmdModel()
	m.showThinking = true // matches the Run() default
	m.openPalette()
	var tm tea.Model
	for _, r := range "thinking" {
		tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.palette == nil {
		t.Fatal("enter on a toggle must keep the palette open")
	}
	if m.showThinking {
		t.Fatal("enter should have toggled thinking tokens off")
	}
	// the toggle persists to the global config (reload proves the round-trip)
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.Thinking == nil || *reloaded.Thinking {
		t.Fatalf("expected thinking: false saved to config, got %v", reloaded.Thinking)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if !m.showThinking {
		t.Fatal("a second enter should toggle thinking tokens back on")
	}
	reloaded, err = config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.Thinking == nil || !*reloaded.Thinking {
		t.Fatalf("expected thinking: true saved to config, got %v", reloaded.Thinking)
	}
}

// Sub-panels drill in and esc pops back one level to the root list.
func TestPalettePanelPushPop(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	var tm tea.Model
	for _, r := range "effort" {
		tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	pp := m.palette.top()
	if pp == nil || pp.kind != panelEffort {
		t.Fatal("enter should push the effort panel")
	}
	if pp.levels[pp.lidx] != m.agent.Effort {
		t.Fatalf("panel should start on the current level, got %q", pp.levels[pp.lidx])
	}
	// filter input is paused inside a panel: typing runes does nothing
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = tm.(*model)
	if m.palette.filter != "effort" {
		t.Fatalf("panel should not edit the root filter, got %q", m.palette.filter)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(*model)
	if m.palette == nil || m.palette.top() != nil {
		t.Fatal("esc should pop back to the root list, not close")
	}
}

// The effort panel lists the model's levels and applies the highlighted one.
func TestPaletteEffortPanelApplies(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	var tm tea.Model
	for _, r := range "effort" {
		tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter}) // push panel
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown}) // off → low
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown}) // low → medium
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.agent.Effort != "medium" {
		t.Fatalf("enter should apply the highlighted level, got %q", m.agent.Effort)
	}
	if m.palette.top() != nil {
		t.Fatal("enter should pop the panel after applying")
	}
}

// The model panel previews routes live while browsing — the header follows
// the selection before anything is committed.
func TestPaletteModelPanelPreviewsLive(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	var tm tea.Model
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown}) // Suggested: Model → Resume…
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyUp}) // back onto Model
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter}) // push model panel
	m = tm.(*model)
	pp := m.palette.top()
	if pp == nil || pp.kind != panelModel {
		t.Fatal("enter should push the model panel")
	}
	if len(pp.items) != 3 { // kimi + glm + the compaction default route
		t.Fatalf("expected 3 routes, got %d", len(pp.items))
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(*model)
	if m.modelName != config.DefaultCompactModel { // routes sort alphabetically: deepseek is next after kimi
		t.Fatalf("browsing should live-preview the switch, got %q", m.modelName)
	}
	if m.cfg.DefaultModel != "kimi-k3-fast" {
		t.Fatal("preview must not persist the default before enter")
	}
	// esc cancels the browse but the preview stays (switch back manually)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(*model)
	if m.palette.top() != nil {
		t.Fatal("esc should pop the model panel")
	}
}

// The goal panel edits the goal inline; enter applies and starts working.
func TestPaletteGoalPanelSetsGoal(t *testing.T) {
	m := compactCmdModel()
	// commitGoal submits the first turn; give the goroutine a program to send
	// to (offline — messages just drain into its queue)
	m.prog = tea.NewProgram(m, tea.WithoutRenderer())
	defer m.prog.Kill()
	m.openPalette()
	var tm tea.Model
	for _, r := range "goal" {
		tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter}) // push goal panel
	m = tm.(*model)
	for _, r := range "ship it" {
		tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.goal != "ship it" {
		t.Fatalf("enter should set the goal, got %q", m.goal)
	}
	if !m.busy {
		t.Fatal("setting a goal should start the first turn")
	}
	if m.palette.top() != nil {
		t.Fatal("enter should pop the goal panel")
	}
}

// The compaction-model panel applies on ←/→ without closing.
func TestPaletteCompactPanelAppliesInPlace(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	var tm tea.Model
	for m.palette.items[m.palette.idx].title != "Compaction model" {
		tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
		m = tm.(*model)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter}) // push compact panel
	m = tm.(*model)
	pp := m.palette.top()
	if pp == nil || pp.kind != panelCompact {
		t.Fatal("enter should push the compaction panel")
	}
	if pp.midx != 0 { // no override configured → the default row selected
		t.Fatalf("should start on the default row, got %d", pp.midx)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyRight}) // apply in place
	m = tm.(*model)
	if m.compactModel == "" {
		t.Fatal("→ should apply the highlighted model")
	}
	if m.palette.top() == nil {
		t.Fatal("→ must keep the panel open")
	}
}

// The panel's first row restores the built-in default (""), not "current
// model": picking a model then selecting the default row resets the override.
func TestPaletteCompactPanelDefaultRowRestores(t *testing.T) {
	m := compactCmdModel()
	m.compactCommand([]string{"glm-5.2-fast"}) // pick an override first
	m.openPaletteOn("Compaction model")
	pp := m.palette.top()
	if pp == nil || pp.kind != panelCompact {
		t.Fatal("openPaletteOn should land in the compaction panel")
	}
	if !strings.Contains(pp.list[0], "default (") {
		t.Fatalf("first row should read default (…), got %q", pp.list[0])
	}
	for pp.midx != 0 { // navigate to the default row
		tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyUp})
		m = tm.(*model)
	}
	tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.compactModel != "" || m.agent.CompactModel != config.DefaultCompactModel {
		t.Fatalf("the default row should restore the built-in default: %q / %q", m.compactModel, m.agent.CompactModel)
	}
	// enter popped the panel — and since it was opened directly (not drilled
	// into from the root list), the whole palette closed with it
	if m.palette != nil && m.palette.top() != nil {
		t.Fatal("enter should pop the panel")
	}
}

// The compaction panel lists catalog-advertised models alongside the
// configured ones (marked (new)), and selecting one applies it.
func TestPaletteCompactPanelListsCatalogModels(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {FetchedAt: time.Now(), Models: []config.ModelInfoLite{{ID: "deepseek-v4-pro", ContextLength: 1048576}}},
	}); err != nil {
		t.Fatal(err)
	}
	m := compactCmdModel()
	m.openPaletteOn("Compaction model")
	pp := m.palette.top()
	if pp == nil || pp.kind != panelCompact {
		t.Fatal("openPaletteOn should land in the compaction panel")
	}
	found := -1
	for i, name := range pp.list {
		if strings.HasPrefix(name, "deepseek-v4-pro") {
			found = i
			if !strings.HasSuffix(name, dimNew) {
				t.Fatalf("the catalog row should carry the (new) marker, got %q", name)
			}
		}
	}
	if found < 0 {
		t.Fatalf("catalog models should be listed, got %v", pp.list)
	}
	for pp.midx != found { // walk onto the catalog row
		tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
		m = tm.(*model)
	}
	tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.compactModel != "deepseek-v4-pro" || m.agent.CompactModel != "deepseek-v4-pro" {
		t.Fatalf("enter should pick the catalog model, got %q / %q", m.compactModel, m.agent.CompactModel)
	}
	if _, ok := m.cfg.Models["deepseek-v4-pro"]; ok {
		t.Error("picking a catalog model must not write it into cfg.Models")
	}
}

// The Compaction level row steps the threshold ±10% in place and shows it.
func TestPaletteCompactionLevelSteps(t *testing.T) {
	m := compactCmdModel()
	m.agent.CompactThreshold = compactThresholdFor(m.cfg) // default 50%
	m.openPalette()
	var it *paletteItem
	for i := range m.palette.items {
		if m.palette.items[i].title == "Compaction level" {
			it = &m.palette.items[i]
			break
		}
	}
	if it == nil {
		t.Fatal("palette should have a Compaction level row")
	}
	if it.stepFwd == nil || it.stepBack == nil {
		t.Fatal("Compaction level should be ←/→ steppable")
	}
	it.stepFwd(m)
	if m.agent.CompactThreshold != 0.6 {
		t.Fatalf("→ should step to 60%%, got %v", m.agent.CompactThreshold)
	}
	it.stepBack(m)
	it.stepBack(m)
	if m.agent.CompactThreshold != 0.4 {
		t.Fatalf("← ← should step to 40%%, got %v", m.agent.CompactThreshold)
	}
	if state := paletteState(m, *it); !strings.Contains(state, "40%") {
		t.Fatalf("the row badge should show the live level, got %q", state)
	}
}
