package tui

import (
	"testing"
)

func texts(cs []cand) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Text
	}
	return out
}

func TestCompletions(t *testing.T) {
	models := []cand{{"kimi-k3-fast", ""}, {"glm-5.2-fast", ""}}
	provs := []cand{{"inference", ""}}

	// slash commands
	head, cs := completions("/m", models, provs, nil, nil)
	if head != "" || len(cs) != 6 || cs[0].Text != "/mcp" || cs[1].Text != "/me" || cs[2].Text != "/memory" || cs[3].Text != "/model" || cs[4].Text != "/model-for-session" || cs[5].Text != "/mouse" {
		t.Fatalf("command completion: %q %v", head, texts(cs))
	}
	// every slash command in the switch must be completable — the "I can't
	// see /context-doctor" regression class: the command exists but the
	// completion table was never told.
	for _, cmd := range []string{"/context-doctor", "/mcp"} {
		found := false
		for _, c := range commands {
			if c.Text == cmd {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing from completion table", cmd)
		}
	}
	// /model first arg
	head, cs = completions("/model k", models, provs, nil, nil)
	if head != "/model " || len(cs) != 1 || cs[0].Text != "kimi-k3-fast" {
		t.Fatalf("model completion: %q %v", head, texts(cs))
	}
	// /model second arg
	_, cs = completions("/model kimi-k3-fast inf", models, provs, nil, nil)
	if len(cs) != 1 || cs[0].Text != "inference" {
		t.Fatalf("provider completion: %v", texts(cs))
	}
	// no match
	_, cs = completions("/nope", models, provs, nil, nil)
	if len(cs) != 0 {
		t.Fatalf("expected no candidates, got %v", texts(cs))
	}
	// slash-command args must never fall through to path completion
	for _, in := range []string{"/goal make yourself better", "/goal make yourself better ", "/resume ab"} {
		if _, cs = completions(in, models, provs, nil, nil); len(cs) != 0 {
			t.Fatalf("%q: expected no candidates, got %v", in, texts(cs))
		}
	}
}
