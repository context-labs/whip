package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/hooks"
)

// reloadHooks builds a new immutable manager and swaps it into the active
// agent. Project commands are included only after the folder trust gate; user
// plugins remain available in every directory.
func (m *model) reloadHooks(dir string, includeProject bool) {
	mgr := hooks.Load(hooks.LoadOptions{
		WorkingDir:     dir,
		IncludeProject: includeProject,
	})
	m.hookMgr = mgr
	if m.agent != nil {
		m.agent.SetHookScope(mgr, dir)
	}
	for _, warning := range mgr.Warnings() {
		config.LogEvent("hooks.load", warning)
		m.append(errStyle.Render("hooks: " + warning))
	}
}

func (m *model) hooksStatus() string {
	if m.hookMgr == nil {
		return "hooks: not loaded"
	}
	if m.hookMgr.Disabled() {
		return "hooks: disabled by WHIP_DISABLE_HOOKS=1"
	}
	project := "project hooks enabled"
	if !m.hookMgr.ProjectEnabled() {
		project = "project hooks disabled (folder is not trusted)"
	}

	entries := m.hookMgr.Entries()
	var b strings.Builder
	fmt.Fprintf(&b, "hooks: %d command(s) · %s", len(entries), project)
	for _, entry := range entries {
		fmt.Fprintf(
			&b,
			"\n%s [%s] — %s: %s",
			entry.Event,
			entry.Matcher,
			entry.Source,
			hookCommandSummary(entry.Command),
		)
	}
	if warnings := m.hookMgr.Warnings(); len(warnings) > 0 {
		fmt.Fprintf(&b, "\n%d discovery warning(s); see the messages above", len(warnings))
	}
	return b.String()
}

const maxHookCommandRunes = 160

func hookCommandSummary(command string) string {
	if utf8.RuneCountInString(command) > maxHookCommandRunes {
		runes := []rune(command)
		command = string(runes[:maxHookCommandRunes]) + "…"
	}
	return strconv.Quote(command)
}

// hookNotice keeps normal successful hooks quiet while surfacing policy
// blocks and fail-open command failures in the transcript.
func hookNotice(event hooks.Event, outcome hooks.Outcome) string {
	var details []string
	if outcome.Blocked {
		reason := strings.TrimSpace(outcome.Reason)
		if reason == "" {
			reason = "blocked"
		}
		details = append(details, reason)
	}
	if len(outcome.Failures) > 0 {
		label := "failed open: "
		if outcome.Blocked {
			label = "failure: "
		}
		details = append(details, label+strings.Join(outcome.Failures, "; "))
	}
	if len(details) == 0 {
		return ""
	}
	return fmt.Sprintf("hook %s: %s", event, strings.Join(details, " · "))
}
