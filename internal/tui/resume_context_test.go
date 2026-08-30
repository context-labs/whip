package tui

import "testing"

func TestRerootSystemPrompt(t *testing.T) {
	prompt := "before\n  Working directory: /old/root\n  Platform: darwin\nafter"
	want := "before\n  Working directory: /new/root\n  Platform: darwin\nafter"
	if got := rerootSystemPrompt(prompt, "/new/root"); got != want {
		t.Fatalf("rerooted prompt = %q, want %q", got, want)
	}
	if got := rerootSystemPrompt("custom prompt", "/new/root"); got != "custom prompt" {
		t.Fatalf("custom prompt changed: %q", got)
	}
}
