package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
)

func writeCompletionSkill(t *testing.T, root, directory, name, description, extra string) {
	t.Helper()
	writeDaemonPromptFile(t, filepath.Join(root, ".agents", "skills", directory, "SKILL.md"),
		"---\nname: "+name+"\ndescription: "+description+"\n"+extra+"---\nSECRET_BODY_NOT_METADATA")
}

func TestHostSkillsCompleteReadOnlyAcrossTransports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	fixture := newV2Fixture(t, &fakeRunner{})
	parent := canonicalPromptDirectory(t, t.TempDir())
	cwd := filepath.Join(parent, "project")
	writeCompletionSkill(t, parent, "ancestor", "ancestor", "not initial context", "")
	writeCompletionSkill(t, cwd, "prefix-one", "prefix-one", "project", "disable-model-invocation: true\n")
	writeCompletionSkill(t, cwd, "prefix-two", "prefix-two", "project two", "")
	writeCompletionSkill(t, os.Getenv("HOME"), "prefix-one", "prefix-one", "user winner", "")
	before, err := fixture.store.RecentContext(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	authority, _, err := fixture.store.LoadAgentAuthority(t.Context(), fixture.rootID, fixture.rootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"unix", "websocket"} {
		client := fixture.dial(transport, "skills-"+transport)
		if !slices.Contains(client.InitializeResult().Capabilities, "host_skill_completion") || !slices.Contains(client.InitializeResult().Capabilities, "workspace_completion") {
			t.Fatal("missing independent capabilities")
		}
		for _, mode := range []string{"", "prompt", "automatic"} {
			params := protocol.HostSkillCompletionParams{CWD: cwd, PermissionMode: mode, Prefix: "prefix", Limit: 1024}
			var result CompletionResult
			if err := client.Call(t.Context(), "host.skills.complete", params, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Candidates) != 2 || result.Candidates[0].Text != "$prefix-one" {
				t.Fatalf("result=%+v", result)
			}
			options := agentdef.Coding().PromptOptions(rlm.PromptOptions{WorkingDirectory: cwd, ProjectRoots: []string{cwd}})
			roster, err := rlm.LoadPromptSkills(options)
			if err != nil {
				t.Fatal(err)
			}
			winner := ""
			for _, skill := range roster {
				if skill.Name == "prefix-one" {
					winner = skill.Description
				}
			}
			if result.Candidates[0].Description != winner {
				t.Fatalf("wrong invocation winner: %+v expected %s", result, winner)
			}
			params.Prefix, params.Limit = "", 1
			if err := client.Call(t.Context(), "host.skills.complete", params, &result); err != nil || len(result.Candidates) != 1 || !result.Truncated {
				t.Fatalf("limit: %+v %v", result, err)
			}
			for _, prefix := range []string{"Prefix", "fix", "ancestor"} {
				params.Prefix, params.Limit = prefix, 8
				if err := client.Call(t.Context(), "host.skills.complete", params, &result); err != nil || len(result.Candidates) != 0 {
					t.Fatalf("prefix %s: %+v %v", prefix, result, err)
				}
			}
		}
	}
	after, err := fixture.store.RecentContext(t.Context(), 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("preview changed sessions: %v", err)
	}
	afterAuthority, _, err := fixture.store.LoadAgentAuthority(t.Context(), fixture.rootID, fixture.rootID)
	if err != nil || authority != afterAuthority {
		t.Fatalf("preview changed authority: %v", err)
	}
	fixture.server.daemon.mu.Lock()
	defer fixture.server.daemon.mu.Unlock()
	if len(fixture.server.daemon.roots) != 0 {
		t.Fatal("preview opened a runtime")
	}
}

func TestHostSkillsCompleteValidationAndDefinition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	fixture := newV2Fixture(t, &fakeRunner{})
	cwd := t.TempDir()
	writeCompletionSkill(t, cwd, "local", "local", "local metadata", "")
	file := filepath.Join(cwd, "file")
	writeDaemonPromptFile(t, file, "not a directory")
	for _, p := range []protocol.HostSkillCompletionParams{
		{Limit: 8},
		{CWD: file, Limit: 8},
		{CWD: filepath.Join(cwd, "missing"), Limit: 8},
		{CWD: cwd},
		{CWD: cwd, Limit: 1025},
		{CWD: cwd, Limit: 8, PermissionMode: "invalid"},
		{CWD: cwd, Limit: 8, Definition: "not-registered"},
		{CWD: cwd, Limit: 8, Prefix: strings.Repeat("x", 4097)},
	} {
		if _, err := fixture.server.completeHostSkills(t.Context(), p); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.server.completeHostSkills(ctx, protocol.HostSkillCompletionParams{CWD: cwd, Limit: 8}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	definition := agentdef.Coding()
	definition.ID = "no-skills"
	definition.Instructions.SkillDiscovery = false
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RegisterDefinition(t.Context(), definition.ID, "v1", raw, "test"); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: cwd, Definition: definition.ID, Limit: 8})
	if err != nil || len(result.Candidates) != 0 {
		t.Fatalf("disabled discovery: %+v %v", result, err)
	}
}

func TestHostSkillsCompleteSymlinkPreviewPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	fixture := newV2Fixture(t, &fakeRunner{})
	cwd, outside := t.TempDir(), t.TempDir()
	writeCompletionSkill(t, outside, "external", "external", "outside", "")
	if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, ".agents", "skills"), filepath.Join(cwd, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"prompt", "automatic"} {
		result, err := fixture.server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: cwd, PermissionMode: mode, Limit: 1024})
		want := 0
		if mode == "automatic" {
			want = 1
		}
		if err != nil || len(result.Candidates) != want {
			t.Fatalf("%s: %+v %v", mode, result, err)
		}
	}
}

func TestWorkspaceSkillCompletionUsesChildScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	cwd := canonicalPromptDirectory(t, t.TempDir())
	allowed := filepath.Join(cwd, "allowed")
	writeCompletionSkill(t, cwd, "parent", "parent", "parent", "")
	writeCompletionSkill(t, allowed, "child", "child", "child", "disable-model-invocation: true\n")
	root := storeBackedInputSession(t, cwd)
	root.root.authority = root.authority
	childID := "restricted-completion"
	if _, err := root.root.store.AdmitAgent(t.Context(), session.AgentAdmission{
		RootID: root.id, ParentAgentID: root.id, ChildAgentID: childID,
		Capabilities: []session.CapabilityDelegation{{ID: "restricted-files", Issuer: root.authority.Files, AgentID: childID, Operations: []string{"read"}, Scopes: []string{allowed}}},
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{daemon: &Daemon{store: root.root.store}}
	agent, err := root.root.store.LoadAgent(t.Context(), root.id, childID)
	if err != nil {
		t.Fatal(err)
	}
	agent.CWD = allowed
	options, err := server.completionPromptOptions(t.Context(), agent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := completePromptSkills(t.Context(), options, "", 1024)
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Text != "$child" {
		t.Fatalf("child scope: %+v %v", result, err)
	}
	if _, err := server.completeWorkspace(t.Context(), CompletionParams{RootID: "wrong-root", AgentID: childID, Kind: "skill", Limit: 8}); err == nil {
		t.Fatal("cross-root completion accepted")
	}
}

func TestHostSkillsCompleteDoesNotWriteStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	counts := func() []int {
		t.Helper()
		var result []int
		for _, table := range []string{"sessions", "agents", "capabilities", "commands", "events"} {
			var n int
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			result = append(result, n)
		}
		return result
	}
	before := counts()
	server := &Server{daemon: &Daemon{store: store}}
	cwd := t.TempDir()
	writeCompletionSkill(t, cwd, "sample", "sample", "metadata", "")
	writeCompletionSkill(t, os.Getenv("HOME"), "global", "global", "global metadata", "")
	for _, mode := range []string{"prompt", "automatic"} {
		for _, params := range []protocol.HostSkillCompletionParams{
			{CWD: cwd, PermissionMode: mode, Limit: 1024},
			{Scope: "global", PermissionMode: mode, Limit: 1024},
		} {
			if _, err := server.completeHostSkills(t.Context(), params); err != nil {
				t.Fatal(err)
			}
		}
	}
	if after := counts(); !reflect.DeepEqual(before, after) {
		t.Fatalf("preview wrote database: %v -> %v", before, after)
	}
}

func TestWorkspaceSkillsCompletePinnedNamedChildDefinition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	definition := agentdef.Coding()
	definition.ID = "completion-definition"
	disabled := definition.Instructions
	disabled.SkillDiscovery = false
	definition.Children = map[string]agentdef.Child{"quiet": {Instructions: &disabled}}
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterDefinition(t.Context(), definition.ID, "v1", raw, "test"); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeCompletionSkill(t, cwd, "visible", "visible", "metadata", "")
	if _, err := store.AdmitCommand(t.Context(), session.CommandAdmission{ClientID: "test", CommandID: "create", Scope: session.CommandScopeDaemon, RequestDigest: "create"}); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateSessionForCommandWithDefinition(t.Context(), "test", "create", session.SessionKindAgent, cwd, "model", "provider", "prompt", "starlark", definition.ID, "v1")
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(record.Outcome.Inline, &created); err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureRootAuthority(t.Context(), created.RootID, rootGrants(definition, true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: created.RootID, ParentAgentID: created.RootID, ChildAgentID: "quiet-child", Definition: "quiet", Capabilities: []session.CapabilityDelegation{{ID: "child-files", Issuer: authority.Files, AgentID: "quiet-child", Operations: []string{"read"}, InheritScope: true}}}); err != nil {
		t.Fatal(err)
	}
	// A later revision must not alter this root or its named child's instructions.
	definition.Instructions.SkillDiscovery = false
	definition.Children = nil
	raw, err = json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterDefinition(t.Context(), definition.ID, "v2", raw, "test"); err != nil {
		t.Fatal(err)
	}
	server := &Server{daemon: &Daemon{store: store}}
	for _, test := range []struct {
		id   string
		want int
	}{{created.RootID, 1}, {"quiet-child", 0}} {
		result, err := server.completeWorkspace(t.Context(), CompletionParams{RootID: created.RootID, AgentID: test.id, Kind: "skill", Limit: 8})
		if err != nil || len(result.Candidates) != test.want {
			t.Fatalf("%s: %+v %v", test.id, result, err)
		}
	}
}

func TestHostSkillsCompleteBoundsMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	server := &Server{daemon: &Daemon{store: store}}
	cwd := t.TempDir()
	writeCompletionSkill(t, cwd, "too-large", "too-large", strings.Repeat("x", 65<<10), "")
	if _, err := server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: cwd, Limit: 8}); err == nil || !strings.Contains(err.Error(), "frontmatter exceeds") {
		t.Fatalf("oversized metadata accepted: %v", err)
	}
}
