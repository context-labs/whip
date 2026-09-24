package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/protocol"
)

func TestHostGlobalSkillsCompleteAcrossTransports(t *testing.T) {
	home, appHome, project := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(buildinfo.Env("HOME"), appHome)
	t.Chdir(project)
	writeDaemonPromptFile(t, filepath.Join(project, ".agents", "skills", "tripwire", "SKILL.md"), "not frontmatter")
	writeDaemonPromptFile(t, filepath.Join(appHome, "skills", "duplicate", "SKILL.md"),
		"---\nname: duplicate\ndescription: app\n---\nSECRET_BODY_NOT_METADATA")
	writeCompletionSkill(t, home, "duplicate", "duplicate", "user winner", "disable-model-invocation: true\n")
	writeCompletionSkill(t, home, "unique", "unique", "user only", "")
	fixture := newV2Fixture(t, &fakeRunner{})
	before, err := fixture.store.RecentContext(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"unix", "websocket"} {
		client := fixture.dial(transport, "global-skills-"+transport)
		for _, mode := range []string{"", "prompt", "automatic"} {
			params := protocol.HostSkillCompletionParams{Scope: "global", PermissionMode: mode, Limit: 1024}
			var result CompletionResult
			if err := client.Call(t.Context(), "host.skills.complete", params, &result); err != nil {
				t.Fatal(err)
			}
			want := []protocol.CompletionCandidate{{Text: "$duplicate", Description: "user winner"}, {Text: "$unique", Description: "user only"}}
			if !reflect.DeepEqual(result.Candidates, want) || result.Truncated {
				t.Fatalf("global result: %+v", result)
			}
			params.Prefix, params.Limit = "uni", 32
			if err := client.Call(t.Context(), "host.skills.complete", params, &result); err != nil ||
				len(result.Candidates) != 1 || result.Candidates[0].Text != "$unique" {
				t.Fatalf("global prefix: %+v, %v", result, err)
			}
			params.Prefix, params.Limit = "", 1
			if err := client.Call(t.Context(), "host.skills.complete", params, &result); err != nil ||
				len(result.Candidates) != 1 || !result.Truncated {
				t.Fatalf("global truncation: %+v, %v", result, err)
			}
		}
	}
	after, err := fixture.store.RecentContext(t.Context(), 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("global preview changed sessions: %v", err)
	}
	fixture.server.daemon.mu.Lock()
	defer fixture.server.daemon.mu.Unlock()
	if len(fixture.server.daemon.roots) != 0 {
		t.Fatal("global preview opened a runtime")
	}
}

func TestHostGlobalSkillsDefinitionAndValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(buildinfo.Env("HOME"), t.TempDir())
	writeCompletionSkill(t, home, "global", "global", "global metadata", "")
	fixture := newV2Fixture(t, &fakeRunner{})
	for _, discovery := range []bool{false, true} {
		definition := agentdef.Coding()
		definition.ID = "no-read"
		definition.Capabilities = nil
		definition.Instructions.SkillDiscovery = discovery
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		revision := "disabled"
		if discovery {
			revision = "enabled"
		}
		if _, err := fixture.store.RegisterDefinition(t.Context(), definition.ID, revision, raw, "test"); err != nil {
			t.Fatal(err)
		}
		wantCount := 0
		if discovery {
			wantCount = 1
		}
		for _, mode := range []string{"prompt", "automatic"} {
			result, err := fixture.server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{
				Scope: "global", Definition: definition.ID, PermissionMode: mode, Limit: 1024,
			})
			if err != nil || len(result.Candidates) != wantCount {
				t.Fatalf("discovery=%v mode=%s: %+v, %v", discovery, mode, result, err)
			}
		}
	}
	for _, params := range []protocol.HostSkillCompletionParams{
		{Limit: 32},
		{Scope: "global", CWD: home, Limit: 32},
		{Scope: "global", CWD: " ", Limit: 32},
		{Scope: "project", CWD: home, Limit: 32},
		{Scope: "unknown", Limit: 32},
		{Scope: "global", Limit: 0},
		{Scope: "global", Limit: 1025},
		{Scope: "global", Limit: 32, Definition: "missing"},
		{Scope: "global", Limit: 32, PermissionMode: "invalid"},
		{Scope: "global", Limit: 32, Prefix: strings.Repeat("x", 4097)},
	} {
		if _, err := fixture.server.completeHostSkills(t.Context(), params); err == nil {
			t.Fatalf("accepted %+v", params)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.server.completeHostSkills(ctx, protocol.HostSkillCompletionParams{Scope: "global", Limit: 32}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	// Selecting a folder retains the existing project-plus-global preview.
	project := t.TempDir()
	writeCompletionSkill(t, project, "local", "local", "project metadata", "")
	result, err := fixture.server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: project, Limit: 32})
	if err != nil || len(result.Candidates) != 2 {
		t.Fatalf("selected project: %+v, %v", result, err)
	}
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: project, Limit: 32}); err == nil {
		t.Fatal("missing selected project fell back to global")
	}
}
