package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSuggestTool(t *testing.T) {
	cands := []string{"bash", "read", "edit", "mcp__docs__greet", "mcp__docs__fail", "mcp__github__create_issue"}
	tests := []struct {
		name string
		want []string
	}{
		{"mcp__docs__gret", []string{"mcp__docs__greet"}},                // 1 edit
		{"mcp__doc__greet", []string{"mcp__docs__greet"}},                // 1 edit
		{"mcp__docs__greet2", []string{"mcp__docs__greet"}},              // 1 edit
		{"mcp__docs__", []string{"mcp__docs__greet", "mcp__docs__fail"}}, // prefix
		{"mcp__github__create_iss", []string{"mcp__github__create_issue"}},
		{"completely_unrelated_xyz", nil}, // nothing close
		{"bsh", []string{"bash"}},
	}
	for _, tt := range tests {
		got := SuggestTool(tt.name, cands)
		if len(got) != len(tt.want) {
			t.Errorf("SuggestTool(%q) = %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("SuggestTool(%q) = %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestSuggestToolCapsAtTwo(t *testing.T) {
	cands := []string{"mcp__s__aaa", "mcp__s__aab", "mcp__s__aac"}
	if got := SuggestTool("mcp__s__aa", cands); len(got) > 2 {
		t.Errorf("got %v, want at most 2", got)
	}
}

func TestLevenshteinCap(t *testing.T) {
	if d := levenshtein("abc", "abc", 2); d != 0 {
		t.Errorf("identical = %d", d)
	}
	if d := levenshtein("abc", "abd", 2); d != 1 {
		t.Errorf("1 edit = %d", d)
	}
	if d := levenshtein("short", "a-much-longer-string", 3); d <= 3 {
		t.Errorf("should exceed cap, got %d", d)
	}
	if d := levenshtein("", "abcdef", 2); d <= 2 {
		t.Errorf("length gap beyond cap = %d", d)
	}
}

func TestExecuteSuggestsOnUnknownTool(t *testing.T) {
	out := ExecuteWithSuggester(context.Background(), nil, "mcp__doc__greet", nil, func(name string) []string { return []string{"mcp__docs__greet"} })
	if !strings.Contains(out, `unknown tool "mcp__doc__greet"`) || !strings.Contains(out, "did you mean mcp__docs__greet") {
		t.Errorf("got %q", out)
	}
	// No suggester → plain error (zero-config path unchanged).
	out = Execute(context.Background(), nil, "nope", nil)
	if strings.Contains(out, "did you mean") {
		t.Errorf("no-suggester path should be plain, got %q", out)
	}
}
