package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/hooks"
)

func writeProjectHook(t *testing.T, project, command string) {
	t.Helper()
	path := filepath.Join(project, ".agents", "plugins", "policy", "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"hooks":{"UserPromptSubmit":[{"hooks":[{"command":` + strconv.Quote(command) + `}]}]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReloadHooksWiresProjectPolicyAndStatus(t *testing.T) {
	project := t.TempDir()
	writeProjectHook(t, project, `printf 'repository policy' >&2; exit 2`)
	m := shellModel()
	m.agent = agent.New(nil, "m", 100, "sys")
	m.reloadHooks(project, true)

	if m.hookMgr.Count() != 1 {
		t.Fatalf("loaded commands = %d, warnings = %v", m.hookMgr.Count(), m.hookMgr.Warnings())
	}
	if status := m.hooksStatus(); !strings.Contains(status, "1 command") ||
		!strings.Contains(status, "project hooks enabled") ||
		!strings.Contains(status, "repository policy") {
		t.Fatalf("status = %q", status)
	}
	_, err := m.agent.Turn(context.Background(), "ship", agent.Events{})
	if err == nil || !strings.Contains(err.Error(), "repository policy") {
		t.Fatalf("wired project policy error = %v", err)
	}
}

func TestHookCommandSummaryIsSingleLineAndBounded(t *testing.T) {
	command := "printf 'first line'\n" + strings.Repeat("界", maxHookCommandRunes)
	got := hookCommandSummary(command)
	if strings.Contains(got, "\n") {
		t.Fatalf("summary contains a newline: %q", got)
	}
	if !strings.Contains(got, `\n`) || !strings.HasSuffix(got, "…\"") {
		t.Fatalf("long command was not visibly truncated: %q", got)
	}
}

func TestCDReloadsOnlyTrustedProjectHooks(t *testing.T) {
	start := t.TempDir()
	t.Chdir(start)
	untrusted := t.TempDir()
	trusted := t.TempDir()
	writeProjectHook(t, untrusted, "exit 0")
	writeProjectHook(t, trusted, "exit 0")
	trustedScope, err := filepath.EvalSymlinks(trusted)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Trust(trustedScope); err != nil {
		t.Fatal(err)
	}

	m := shellModel()
	m.cdCommand(untrusted)
	if m.hookMgr.ProjectEnabled() || m.hookMgr.Count() != 0 {
		t.Fatalf("untrusted /cd loaded project commands: count=%d", m.hookMgr.Count())
	}
	m.cdCommand(trusted)
	if !m.hookMgr.ProjectEnabled() || m.hookMgr.Count() != 1 {
		t.Fatalf("trusted /cd did not reload project commands: count=%d warnings=%v", m.hookMgr.Count(), m.hookMgr.Warnings())
	}
}

func TestHookNoticeReportsOnlyActionableOutcomes(t *testing.T) {
	if got := hookNotice(hooks.PreToolUse, hooks.Outcome{Ran: 1}); got != "" {
		t.Fatalf("successful hook should stay quiet: %q", got)
	}
	got := hookNotice(hooks.PreToolUse, hooks.Outcome{
		Blocked:  true,
		Reason:   "denied",
		Failures: []string{"timeout"},
	})
	if !strings.Contains(got, "denied") || !strings.Contains(got, "failure: timeout") || strings.Contains(got, "failed open") {
		t.Fatalf("actionable notice = %q", got)
	}
	got = hookNotice(hooks.PostToolUse, hooks.Outcome{Failures: []string{"timeout"}})
	if !strings.Contains(got, "failed open: timeout") {
		t.Fatalf("fail-open notice = %q", got)
	}
}
