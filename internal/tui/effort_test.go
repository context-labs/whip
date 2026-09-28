package tui

import (
	"testing"

	"github.com/context-labs/whip/internal/legacy/config"
)

func TestEffortCycleAndParse(t *testing.T) {
	got := "off"
	for _, want := range []string{"low", "medium", "high", "off", "low"} {
		got = nextEffort(defaultEfforts, got)
		if got != want {
			t.Fatalf("cycle: got %q want %q", got, want)
		}
	}
	if nextEffort(defaultEfforts, "bogus") != "off" {
		t.Fatal("unknown level should reset to off")
	}
	for _, in := range []string{"off", "low", "high"} {
		if lv, ok := parseEffort(defaultEfforts, in); !ok || lv != in {
			t.Fatalf("parse %q: %q %v", in, lv, ok)
		}
	}
	if _, ok := parseEffort(defaultEfforts, "ultra"); ok {
		t.Fatal("invalid level accepted")
	}
	if _, ok := parseEffort(defaultEfforts, ""); ok {
		t.Fatal("blank is not an effort level")
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
	if got := m.effortsFor(); len(got) != 4 || got[0] != "off" || got[3] != "max" {
		t.Fatalf("advertised levels: %v", got)
	}
	if next := nextEffort(m.effortsFor(), "high"); next != "max" {
		t.Fatalf("cycle should reach max: %q", next)
	}
	if _, ok := parseEffort(m.effortsFor(), "medium"); ok {
		t.Fatal("medium should be rejected for deepseek")
	}

	// "none" collapses into "off"
	m.clientView.modelID = "claude-opus-5"
	got := m.effortsFor()
	if got[0] != "off" || len(got) != 7 {
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
