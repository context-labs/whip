package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/codexauth"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// codexUsageMsg carries the /usage fetch back to the UI goroutine.
type codexUsageMsg struct {
	limits llm.RateLimits
	err    error
}

// usageCommand implements /usage: the Codex subscription's rate-limit windows.
// Pay-per-token providers (OpenRouter, inference-net) have no quota to show,
// so the command only applies when a Codex provider is configured.
func (m *model) usageCommand() {
	if !m.codexConfigured() {
		m.append(dimStyle.Render("/usage shows ChatGPT Codex subscription limits — run /auth codex first (API-key providers bill per token and have no quota)"))
		return
	}
	if m.prog == nil {
		return
	}
	m.append(dimStyle.Render("fetching Codex usage…"))
	p := m.prog
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		limits, err := llm.NewCodex(config.CodexBaseURL, &codexauth.Source{}).RateLimits(ctx)
		p.Send(codexUsageMsg{limits: limits, err: err})
	}()
}

func (m *model) codexConfigured() bool {
	if m.cfg == nil {
		return false
	}
	for _, p := range m.cfg.Providers {
		if p.API == "openai-codex-responses" {
			return true
		}
	}
	return false
}

func (m *model) applyCodexUsage(msg codexUsageMsg) {
	if msg.err != nil {
		m.append(errStyle.Render("Codex usage: " + msg.err.Error()))
		return
	}
	m.append(renderRateLimits(msg.limits))
}

// renderRateLimits formats the windows as one line each, e.g.
//
//	Codex usage (plan: plus)
//	  5h window:   28% used · resets in 2h10m
//	  weekly:      61% used · resets in 5d19h
func renderRateLimits(r llm.RateLimits) string {
	var sb strings.Builder
	sb.WriteString("Codex usage")
	if r.Plan != "" {
		sb.WriteString(" (plan: " + r.Plan + ")")
	}
	if r.LimitReached {
		sb.WriteString(" — limit reached")
	}
	if len(r.Windows) == 0 {
		sb.WriteString("\n  no rate-limit windows reported")
	}
	for _, w := range r.Windows {
		fmt.Fprintf(&sb, "\n  %-10s %3d%% used · resets in %s", windowLabel(w.Window)+":", w.UsedPercent, shortDuration(w.ResetIn))
	}
	return sb.String()
}

func windowLabel(d time.Duration) string {
	switch {
	case d >= 6*24*time.Hour:
		return "weekly"
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd window", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dh window", int(d.Hours()))
	}
}

func shortDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
