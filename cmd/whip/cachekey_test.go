package main

import "testing"

func TestResolveCacheKey(t *testing.T) {
	if got := resolveCacheKey("repo/reviewer", "sess"); got != "repo/reviewer" {
		t.Fatalf("explicit key must win, got %q", got)
	}
	if got := resolveCacheKey("", "sess"); got != "sess" {
		t.Fatalf("session id is the fallback, got %q", got)
	}
	got := resolveCacheKey("", "")
	if len(got) < 5 || got[:4] != "run-" {
		t.Fatalf("no session/key must yield a per-run key, got %q", got)
	}
	if resolveCacheKey("", "") == got {
		t.Fatal("per-run keys must differ between runs")
	}
}
