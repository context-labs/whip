package tui

import (
	"strings"
	"testing"
)

func TestGoalMet(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"prefix exact", "GOAL_MET — done", true},
		{"prefix with leading space", "  GOAL_MET — done", true},
		{"token after one-line preamble (the observed loop bug)", "Verified: all green.\n\nGOAL_MET — shipped", true},
		{"token mid-paragraph", "The work is done. GOAL_MET. Summary follows.", true},
		{"aspirational mention stays false", "I am making progress toward GOAL_MET soon", false},
		{"token as substring of word", "GOAL_METRICS look good", false},
		{"no token", "still working on it", false},
		{"token only deep in a long reply", strings.Repeat("x", 500) + " GOAL_MET", false},
		{"similar but wrong token", "GOALMET — done", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := goalMet(c.in); got != c.want {
			t.Errorf("%s: goalMet(%q) = %v, want %v", c.name, c.in[:min(30, len(c.in))], got, c.want)
		}
	}
}
