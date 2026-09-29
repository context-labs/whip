package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeCompletionUsesHostOwnerAndRestoresOriginalDraft(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.clientDirectory = t.TempDir()
	if err := os.WriteFile(filepath.Join(m.clientDirectory, "local-only.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.owner.WorkingDirectory, "host-only.txt"), []byte("not completion metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("inspect\n@host")
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if command == nil {
		t.Fatal("host completion was not scheduled")
	}
	result := command().(nativeCompletionResult)
	m.Update(result)
	if result.err != nil || m.input.Value() != "inspect\n@host-only.txt" || len(m.completion.candidates) != 1 {
		t.Fatal(result, m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.input.Value() != "inspect\n@host" || m.completion != nil {
		t.Fatal("Escape failed to restore original draft")
	}
	page, err := m.handle.History(t.Context(), protocol.HistoryPageParams{Direction: "forward", Limit: 10})
	if err != nil || len(page.Messages) != 0 {
		t.Fatal("completion admitted a prompt", page, err)
	}
	grants := nativeMenuRPC[protocol.GrantsResult](t, m.connection, "grants.list", protocol.GrantsParams{SessionID: m.owner.ID, Limit: 10})
	if len(grants.Items) != 0 {
		t.Fatal("completion minted authority", grants)
	}
	other := nativeNavigationRoot(t, m, "completion-other", "Other")
	if err := os.WriteFile(filepath.Join(other.WorkingDirectory, "other-only.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.attachSession(other); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("@other")
	m.Update(m.completeInput(true)())
	if m.input.Value() != "@other-only.txt" {
		t.Fatal("completion read previous owner", m.input.Value())
	}
}

func TestNativeSkillCompletionRequiresExistingAuthority(t *testing.T) {
	m, _ := nativeUIFixture(t)
	directory := filepath.Join(m.owner.WorkingDirectory, ".agents", "skills", "review")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: review\ndescription: Review fixture changes\n---\nFixture body must not enter completion.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("$rev")
	m.Update(m.completeInput(true)())
	if m.completion == nil || len(m.completion.candidates) != 0 || m.input.Value() != "$rev" {
		t.Fatal("ungranted skill metadata exposed", m.status, m.completion)
	}
	nativeMenuRPC[protocol.Grant](t, m.connection, "grants.create", protocol.CreateGrantParams{ID: "completion-read", SessionID: m.owner.ID, Capability: "files.read", Resource: m.owner.WorkingDirectory})
	m.Update(m.completeInput(true)())
	if m.completion == nil || len(m.completion.candidates) != 1 || m.input.Value() != "$review" {
		t.Fatal("authorized skill absent", m.status, m.completion)
	}
	if strings.Contains(m.completionView(), "Fixture body") {
		t.Fatal("completion loaded a skill body")
	}
	nativeMenuRPC[protocol.Grant](t, m.connection, "grants.revoke", protocol.GrantParams{GrantID: "completion-read", SessionID: new(m.owner.ID)})
	m.input.SetValue("$rev")
	m.Update(m.completeInput(true)())
	if len(m.completion.candidates) != 0 {
		t.Fatal("revoked skill completion cache remained")
	}
}

func TestNativeCompletionDropsStaleResponsesAndJoinsCancellation(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.input.SetValue("@pending")
	command := m.completeInput(false)
	old := m.completion
	m.input.SetValue("new draft")
	m.Update(nativeCompletionResult{request: old, candidates: []protocol.WorkspaceCompletionCandidate{{Text: "@wrong-owner"}}})
	if m.input.Value() != "new draft" || m.completion != nil {
		t.Fatal("late completion replaced edited draft")
	}
	value := command().(nativeCompletionResult)
	if !errors.Is(value.err, context.Canceled) {
		t.Fatal("superseded delayed request still ran", value.err)
	}
	m.input.SetValue("@again")
	command = m.completeInput(false)
	m.work.close()
	if value := command().(nativeCompletionResult); value.err == nil {
		t.Fatal("completion survived terminal close")
	}
}

func TestNativeCommandCompletionAndCandidateBounds(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.input.SetValue("/mode")
	m.completeInput(false)
	if m.completion == nil || len(m.completion.candidates) != 2 {
		t.Fatal(m.completion)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/model" {
		t.Fatal("first Tab skipped first result", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/model-for-session" {
		t.Fatal(m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.completion != nil || m.menu != nil || m.input.Value() != "/model-for-session" {
		t.Fatal("completion selection executed command")
	}
	m.input.SetValue("/")
	m.completeInput(false)
	for _, size := range [][2]int{{8, 4}, {80, 24}, {150, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		rows := strings.Split(m.View().Content, "\n")
		if len(rows) > size[1] {
			t.Fatal("completion overflowed terminal height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > size[0] {
				t.Fatal("completion overflowed terminal width")
			}
		}
	}
	m.input.SetValue("@value")
	m.completeInput(true)
	request := m.completion
	m.Update(nativeCompletionResult{request: request, candidates: []protocol.WorkspaceCompletionCandidate{{Text: "@value\tchanged"}}})
	if m.completion != nil || m.input.Value() != "@value" {
		t.Fatal("unsafe candidate changed draft")
	}
}
