package config

import "testing"

// ResolveEffort picks "low" when the model advertises it, the lowest supported
// level otherwise, and "off" for non-reasoning models, so a session never
// opens on an effort the provider would reject. An explicit pinned value is
// honored verbatim, even if unsupported. Blank is never returned.
func TestResolveEffortModelAware(t *testing.T) {
	cats := map[string]Catalog{
		"inference": {Models: []ModelInfoLite{
			{ID: "deepseek-v4-flash", ReasoningEfforts: []string{"low", "high", "max"}}, // no medium
			{ID: "claude-opus-5", ReasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
			{ID: "gemini-3.5-flash"}, // no reasoning_efforts
		}},
	}
	cases := []struct{ model, pinned, want string }{
		{"deepseek-v4-flash", "", "low"},          // low is supported → low
		{"claude-opus-5", "", "low"},              // low is supported → low
		{"gemini-3.5-flash", "", "off"},           // non-reasoning → off
		{"deepseek-v4-flash", "high", "high"},     // pinned honored
		{"deepseek-v4-flash", "medium", "medium"}, // pinned honored even though unsupported
		{"gemini-3.5-flash", "off", "off"},        // explicit off stays off
	}
	for _, c := range cases {
		if got := ResolveEffort(cats, "inference", c.model, c.pinned); got != c.want {
			t.Fatalf("ResolveEffort(%q, pinned=%q): got %q want %q", c.model, c.pinned, got, c.want)
		}
	}
	if got := ResolveEffort(map[string]Catalog{}, "elsewhere", "anything", ""); got != "low" {
		t.Fatalf("unknown provider should fall back to low, got %q", got)
	}
}
