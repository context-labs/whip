package tui

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
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
		provName:   "inference",
		clientView: clientPresentation{modelID: "deepseek-v4-flash"},
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
	m.clientView.modelID = "claude-opus-5"
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
	m.clientView.modelID = "gemini-3.5-flash"
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
