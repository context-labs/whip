package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestUsageCommandRequiresCodex(t *testing.T) {
	m := authTestModel(t)
	m.usageCommand()
	if out := m.transcriptText(); !strings.Contains(out, "/auth codex") {
		t.Fatalf("without a codex provider /usage should point at /auth codex:\n%s", out)
	}
}

func TestApplyCodexUsageRendersWindowsAndErrors(t *testing.T) {
	m := authTestModel(t)
	m.applyCodexUsage(codexUsageMsg{limits: llm.RateLimits{
		Plan: "plus",
		Windows: []llm.RateLimitWindow{
			{UsedPercent: 28, Window: 5 * time.Hour, ResetIn: 2*time.Hour + 10*time.Minute},
			{UsedPercent: 61, Window: 7 * 24 * time.Hour, ResetIn: 5*24*time.Hour + 19*time.Hour},
		},
	}})
	m.applyCodexUsage(codexUsageMsg{err: errors.New("boom")})
	out := m.transcriptText()
	for _, want := range []string{"plan: plus", "5h window:", "28% used", "resets in 2h10m", "weekly:", "61% used", "5d19h", "Codex usage: boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderRateLimitsEmptyAndReached(t *testing.T) {
	out := renderRateLimits(llm.RateLimits{LimitReached: true})
	if !strings.Contains(out, "limit reached") || !strings.Contains(out, "no rate-limit windows") {
		t.Fatalf("got %q", out)
	}
	if got := shortDuration(45 * time.Second); got != "1m" {
		t.Fatalf("shortDuration = %q", got)
	}
	if got := windowLabel(3 * 24 * time.Hour); got != "3d window" {
		t.Fatalf("windowLabel = %q", got)
	}
}
