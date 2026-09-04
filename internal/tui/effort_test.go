package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestEffortCycleAndParse(t *testing.T) {
	got := ""
	for _, want := range []string{"low", "medium", "high", "", "low"} {
		got = nextEffort(defaultEfforts, got)
		if got != want {
			t.Fatalf("cycle: got %q want %q", got, want)
		}
	}
	if nextEffort(defaultEfforts, "bogus") != "" {
		t.Fatal("unknown level should reset to off")
	}
	if effortLabel("") != "off" || effortLabel("high") != "high" {
		t.Fatal("labels")
	}
	for in, want := range map[string]string{"off": "", "low": "low", "high": "high"} {
		if lv, ok := parseEffort(defaultEfforts, in); !ok || lv != want {
			t.Fatalf("parse %q: %q %v", in, lv, ok)
		}
	}
	if _, ok := parseEffort(defaultEfforts, "ultra"); ok {
		t.Fatal("invalid level accepted")
	}
}

func TestEffortCompletion(t *testing.T) {
	_, cs := completions("/effort h", nil, nil, nil, nil)
	if len(cs) != 1 || cs[0].Text != "high" {
		t.Fatalf("effort completion: %v", texts(cs))
	}
}

func TestEffortsForAdvertisedLevels(t *testing.T) {
	m := &model{
		provName: "inference",
		agent:    &agent.Agent{Model: "deepseek-v4-flash"},
		catalogs: map[string]config.Catalog{
			"inference": {Models: []config.ModelInfoLite{
				{ID: "deepseek-v4-flash", ReasoningEfforts: []string{"low", "high", "max"}},
				{ID: "claude-opus-5", ReasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
				{ID: "gemini-3.5-flash"}, // no reasoning_efforts
			}},
		},
	}
	if got := m.effortsFor(); len(got) != 4 || got[0] != "" || got[3] != "max" {
		t.Fatalf("advertised levels: %v", got)
	}
	if next := nextEffort(m.effortsFor(), "high"); next != "max" {
		t.Fatalf("cycle should reach max: %q", next)
	}
	if _, ok := parseEffort(m.effortsFor(), "medium"); ok {
		t.Fatal("medium should be rejected for deepseek")
	}

	// "none" collapses into off ("")
	m.agent.Model = "claude-opus-5"
	got := m.effortsFor()
	if got[0] != "" || len(got) != 7 {
		t.Fatalf("claude levels: %v", got)
	}
	for _, e := range got {
		if e == "none" {
			t.Fatalf("none should map to off: %v", got)
		}
	}

	// no advertised levels → defaults
	m.agent.Model = "gemini-3.5-flash"
	if got := m.effortsFor(); len(got) != len(defaultEfforts) {
		t.Fatalf("gemini should fall back to defaults: %v", got)
	}

	// unknown provider → defaults
	m.provName = "elsewhere"
	if got := m.effortsFor(); len(got) != len(defaultEfforts) {
		t.Fatalf("missing catalog should fall back to defaults: %v", got)
	}
}

// DefaultEffortFor picks "low" when the model advertises it, the lowest
// supported level otherwise, and off ("") for non-reasoning models — so a
// startup never opens on an effort the provider would reject. An explicit
// pinned value is honored verbatim, even if unsupported.
func TestDefaultEffortForModelAware(t *testing.T) {
	cats := map[string]config.Catalog{
		"inference": {Models: []config.ModelInfoLite{
			{ID: "deepseek-v4-flash", ReasoningEfforts: []string{"low", "high", "max"}}, // no medium
			{ID: "claude-opus-5", ReasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
			{ID: "gemini-3.5-flash"}, // no reasoning_efforts
		}},
	}
	cases := []struct{ model, pinned, want string }{
		{"deepseek-v4-flash", "", "low"},          // low is supported → low
		{"claude-opus-5", "", "low"},              // low is supported → low
		{"gemini-3.5-flash", "", ""},              // non-reasoning → off (no parameter)
		{"deepseek-v4-flash", "high", "high"},     // pinned honored
		{"deepseek-v4-flash", "medium", "medium"}, // pinned honored even though unsupported
	}
	for _, c := range cases {
		if got := DefaultEffortFor(cats, "inference", c.model, c.pinned); got != c.want {
			t.Fatalf("DefaultEffortFor(%q, pinned=%q): got %q want %q", c.model, c.pinned, got, c.want)
		}
	}
	// unknown provider → default-effort cycle's first non-off (low)
	if got := DefaultEffortFor(map[string]config.Catalog{}, "elsewhere", "anything", ""); got != "low" {
		t.Fatalf("unknown provider should fall back to low, got %q", got)
	}
}

// bare /effort opens the level selector (palette panel) so the user can
// scroll ↑/↓ and pick — cycling blindly hides the choices.
func TestEffortBareOpensSelector(t *testing.T) {
	m := compactCmdModel()
	m.command("/effort")
	if m.palette == nil {
		t.Fatal("bare /effort should open the palette")
	}
	pp := m.palette.top()
	if pp == nil || pp.kind != panelEffort {
		t.Fatalf("expected the effort panel, got %+v", pp)
	}
	if len(pp.levels) != len(defaultEfforts) || pp.levels[pp.lidx] != m.agent.Effort {
		t.Fatalf("effort panel should list the model's levels on the current one: %v @%d", pp.levels, pp.lidx)
	}
	// scroll down to low and apply with enter
	tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(*model)
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.agent.Effort != "low" {
		t.Fatalf("selecting low in the selector should apply it, got %q", m.agent.Effort)
	}
	// the selector came from /effort, not ctrl+p: commit-and-close, don't
	// strand the user on a palette root they never opened
	if m.palette != nil {
		t.Fatal("enter in a directly-opened selector should close the palette")
	}
}

// A user-picked effort is both the new global default (config.json) and the
// live session's restore value (sessions.db); a reconciliation reset touches
// only the session.
func TestSetEffortPersistsGlobalAndSession(t *testing.T) {
	m := compactCmdModel()
	m.cfg.DefaultEffort = "medium"
	m.agent.Effort = "medium"
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m.store = st
	id, err := st.Create("/tmp", m.modelName, m.provName)
	if err != nil {
		t.Fatal(err)
	}
	m.sessionID = id

	m.setEffort("low") // the user picks a level
	if m.agent.Effort != "low" {
		t.Fatalf("agent effort: %q", m.agent.Effort)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.DefaultEffort != "low" {
		t.Fatalf("global default should follow the pick, got %q", reloaded.DefaultEffort)
	}
	meta, _, err := st.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Effort != "low" {
		t.Fatalf("session row should carry the pick, got %q", meta.Effort)
	}

	// a reconciliation (catalog refresh drops the level) must not rewrite the
	// user's global default, only the live session
	m.resetEffort("")
	reloaded, _ = config.Load()
	if reloaded.DefaultEffort != "low" {
		t.Fatalf("reset must not touch the global default, got %q", reloaded.DefaultEffort)
	}
	meta, _, _ = st.Load(id)
	if meta.Effort != "" {
		t.Fatalf("session row should track the reset, got %q", meta.Effort)
	}
}

// Resume restores the session's own effort; a row that pre-dates per-session
// effort ("") inherits the current default and is stamped on the next save.
func TestResumeRestoresEffort(t *testing.T) {
	m := compactCmdModel()
	m.cfg.DefaultEffort = "medium"
	m.agent.Effort = "medium"
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m.store = st

	id, err := st.Create("/tmp", m.modelName, m.provName)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q", Authored: true}}
	if err := st.Save(id, 1, msgs, m.modelName, m.provName); err != nil {
		t.Fatal(err)
	}

	// session chose high; the global default drifting to low must not matter
	if err := st.SetEffort(id, "high"); err != nil {
		t.Fatal(err)
	}
	m.agent.Effort = "low"
	if err := m.resume(id); err != nil {
		t.Fatal(err)
	}
	if m.agent.Effort != "high" {
		t.Fatalf("resume should restore the session effort, got %q", m.agent.Effort)
	}

	// a legacy row (no effort) inherits the current default…
	id2, err := st.Create("/tmp", m.modelName, m.provName)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(id2, 1, msgs, m.modelName, m.provName); err != nil {
		t.Fatal(err)
	}
	m.agent.Effort = "low"
	if err := m.resume(id2); err != nil {
		t.Fatal(err)
	}
	if m.agent.Effort != "low" {
		t.Fatalf("legacy row should inherit the current default, got %q", m.agent.Effort)
	}
	// …and the next persist stamps it so a later default change can't leak in
	m.persist()
	meta, _, _ := st.Load(id2)
	if meta.Effort != "low" {
		t.Fatalf("persist should stamp the inherited effort, got %q", meta.Effort)
	}
}

// Usage totals persist with the session: resume restores them (so the status
// line shows the real spend, not 0/0) and the next save keeps them. Legacy
// rows read zero and get stamped on the first save after resume.
func TestResumeRestoresUsage(t *testing.T) {
	m := compactCmdModel()
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m.store = st

	id, err := st.Create("/tmp", m.modelName, m.provName)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q", Authored: true}}
	if err := st.Save(id, 1, msgs, m.modelName, m.provName); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUsage(id, 12000, 8000, 1500, nil); err != nil {
		t.Fatal(err)
	}

	if err := m.resume(id); err != nil {
		t.Fatal(err)
	}
	u := m.agent.Usage()
	if u.PromptTokens != 12000 || u.Cached() != 8000 || u.CompletionTokens != 1500 {
		t.Fatalf("resume should restore usage, got in=%d cached=%d out=%d", u.PromptTokens, u.Cached(), u.CompletionTokens)
	}

	// new spend accumulates on top of the restored totals…
	m.agent.AddUsage(llm.Usage{PromptTokens: 3000, CompletionTokens: 500})
	// …and persists: the stored row is absolute, so a compaction (now a
	// recorded event, no rewrite) can't zero it
	m.persist()
	meta, _, _ := st.Load(id)
	if meta.UsageIn != 15000 || meta.UsageOut != 2000 {
		t.Fatalf("persist should store cumulative totals, got in=%d out=%d", meta.UsageIn, meta.UsageOut)
	}

	// subagent spend persists as its own ledger (never folded into usage_*)
	// and a resume restores it, so the total survives a restart intact
	m.agent.AddSubUsage("sub-m @ p", llm.Usage{PromptTokens: 5000, CompletionTokens: 40})
	m.persist()
	meta, _, _ = st.Load(id)
	if meta.UsageIn != 15000 || meta.UsageOut != 2000 {
		t.Fatalf("sub spend must not leak into the session's own usage columns: %+v", meta)
	}
	if su := meta.SubUsage["sub-m @ p"]; su.PromptTokens != 5000 || su.CompletionTokens != 40 {
		t.Fatalf("sub ledger did not persist: %+v", meta.SubUsage)
	}
	if err := m.resume(id); err != nil {
		t.Fatal(err)
	}
	if su := m.agent.SubUsage()["sub-m @ p"]; su.PromptTokens != 5000 {
		t.Fatalf("resume should restore the sub ledger, got %+v", m.agent.SubUsage())
	}
	if tot := m.agent.TotalUsage(); tot.PromptTokens != 20000 || tot.CompletionTokens != 2040 {
		t.Fatalf("restored total should be own + subs, got %+v", tot)
	}

	// legacy row (no usage columns stamped): totals are reconstructed from the
	// per-message usage stored on assistant messages, then stamped on the next
	// persist so reconstruction happens once
	id2, err := st.Create("/tmp", m.modelName, m.provName)
	if err != nil {
		t.Fatal(err)
	}
	cu := llm.Usage{
		PromptTokens: 1000, CompletionTokens: 200,
		PromptTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: 600},
	}
	legacy := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1", Authored: true},
		{Role: "assistant", Content: "a1", Usage: &cu},
		{Role: "user", Content: "q2", Authored: true},
		{Role: "assistant", Content: "a2", Usage: &llm.Usage{PromptTokens: 500, CompletionTokens: 100}},
	}
	if err := st.Save(id2, 1, legacy, m.modelName, m.provName); err != nil {
		t.Fatal(err)
	}
	if err := m.resume(id2); err != nil {
		t.Fatal(err)
	}
	u2 := m.agent.Usage()
	if u2.PromptTokens != 1500 || u2.Cached() != 600 || u2.CompletionTokens != 300 {
		t.Fatalf("legacy row should reconstruct usage from messages, got in=%d cached=%d out=%d",
			u2.PromptTokens, u2.Cached(), u2.CompletionTokens)
	}
	m.persist()
	meta2, _, _ := st.Load(id2)
	if meta2.UsageIn != 1500 || meta2.UsageCached != 600 || meta2.UsageOut != 300 {
		t.Fatalf("persist should stamp the reconstructed totals, got %+v", meta2)
	}

	// a session with no usage anywhere (pre-usage tracking) stays zero
	id3, err := st.Create("/tmp", m.modelName, m.provName)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(id3, 1, msgs, m.modelName, m.provName); err != nil {
		t.Fatal(err)
	}
	if err := m.resume(id3); err != nil {
		t.Fatal(err)
	}
	if u := m.agent.Usage(); u.PromptTokens != 0 || u.CompletionTokens != 0 {
		t.Fatalf("usage-free session should start at zero, got %+v", u)
	}
}

func TestUpdateCatalogsResetsUnsupportedEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep setEffort's cfg.Save() away from the real config
	m := &model{
		cfg:      &config.Config{},
		provName: "inference",
		agent:    &agent.Agent{Model: "deepseek-v4-flash", Effort: "medium"},
	}
	m.updateCatalogs(map[string]config.Catalog{
		"inference": {Models: []config.ModelInfoLite{
			{ID: "deepseek-v4-flash", ReasoningEfforts: []string{"low", "high", "max"}},
		}},
	})
	if m.agent.Effort != "" {
		t.Fatalf("unsupported effort should reset to off, got %q", m.agent.Effort)
	}

	// a supported effort survives the refresh
	m.agent.Effort = "high"
	m.updateCatalogs(map[string]config.Catalog{
		"inference": {Models: []config.ModelInfoLite{
			{ID: "deepseek-v4-flash", ReasoningEfforts: []string{"low", "high", "max"}},
		}},
	})
	if m.agent.Effort != "high" {
		t.Fatalf("supported effort should survive, got %q", m.agent.Effort)
	}
}
