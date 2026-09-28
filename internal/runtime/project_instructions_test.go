package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func projectSession(t *testing.T, r *Runtime, cwd string, engine session.Engine) session.Session {
	t.Helper()
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, owner, err := r.CreateTree(t.Context(), store.CreateTree{
		Engine: engine, Definition: refs[0], WorkingDirectory: cwd,
		Overrides: session.ConfigPatch{
			ReportMode: new(session.ReportMessage), Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"},
			Instructions: &session.Instructions{ProjectRoot: new("repo"), ProjectFiles: []string{"CLAUDE.md", "AGENTS.md"}, DiscoverSkills: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func TestProjectInstructionMembershipAliasesAndBoundaries(t *testing.T) {
	boundary := t.TempDir()
	cwd := filepath.Join(boundary, "app", "nested")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "cwd")
	boundaryAlias := filepath.Join(t.TempDir(), "project")
	for name, target := range map[string]string{alias: cwd, boundaryAlias: boundary} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, paths := range [][2]string{{boundary, cwd}, {boundaryAlias, alias}, {boundary, boundary}} {
		project, err := openProjectInstructions(t.Context(), "repo", paths[0], paths[1])
		if err != nil || project == nil || project.Scope() != "project" {
			t.Fatalf("membership: %+v %v", project, err)
		}
		want := []string{".", "app", "app/nested"}
		if paths[1] == boundary {
			want = []string{"."}
		}
		if !reflect.DeepEqual(project.ProjectDirectories, want) {
			t.Fatalf("chain=%v want=%v", project.ProjectDirectories, want)
		}
		if err := project.FS.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if project, err := openProjectInstructions(t.Context(), "repo", boundary, t.TempDir()); err != nil || project != nil {
		t.Fatal("unrelated boundary admitted", err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), alias); err != nil {
		t.Fatal(err)
	}
	if project, err := openProjectInstructions(t.Context(), "repo", boundaryAlias, alias); err != nil || project != nil {
		t.Fatal("retargeted cwd alias reused prior membership", err)
	}
	deep := boundary
	for range instruction.MaxProjectDirectories {
		deep = filepath.Join(deep, "a")
	}
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := openProjectInstructions(t.Context(), "repo", boundary, deep); err == nil || strings.Contains(err.Error(), boundary) {
		t.Fatal("deep chain unbounded or host path leaked", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := openProjectInstructions(ctx, "repo", "unavailable", "unavailable"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled open probed filesystem", err)
	}
}

func TestProjectInstructionsDeniedAndUnrelatedUseOnlyIndependentWorkspace(t *testing.T) {
	for _, mode := range []string{"denied missing boundary", "authorized unrelated", "project alone"} {
		t.Run(mode, func(t *testing.T) {
			r := openTest(t, t.TempDir(), model.Scripted{})
			cwd := t.TempDir()
			boundary := t.TempDir()
			switch mode {
			case "project alone":
				cwd = filepath.Join(boundary, "child")
				if err := os.Mkdir(cwd, 0o700); err != nil {
					t.Fatal(err)
				}
			case "denied missing boundary":
				boundary = filepath.Join(boundary, "missing")
			}
			r.host.ProjectRoots = map[string]string{"repo": boundary}
			owner := projectSession(t, r, cwd, session.Starlark)
			writeInstructionFile(t, filepath.Join(cwd, "AGENTS.md"), "LOCAL_RULE")
			writeRuntimeSkill(t, cwd, "local", "---\nname: local\ndescription: local\n---\n", "body")
			if mode != "denied missing boundary" {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "project", SessionID: owner.ID, Capability: "instructions.read", Resource: "project:repo"}); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "project alone" {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "workspace", SessionID: owner.ID, Capability: "files.read", Resource: cwd}); err != nil {
					t.Fatal(err)
				}
			}
			catalog, _, err := r.Skills(t.Context(), owner.ID, "", "", 100)
			if err != nil || len(catalog) != 1 {
				t.Fatalf("catalog=%+v err=%v", catalog, err)
			}
			want := "workspace"
			if mode == "project alone" {
				want = "project"
			}
			if catalog[0].Source.Scope != want {
				t.Fatalf("scope=%s want=%s", catalog[0].Source.Scope, want)
			}
			submitTest(t, r, owner.ID, "capture")
			claim, err := r.store.Claim(t.Context(), owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			text, err := r.Instructions(t.Context(), claim.Turn, claim.Configuration.Instructions)
			if err != nil || !strings.Contains(text, "LOCAL_RULE") {
				t.Fatal("independent source capture failed", err)
			}
			audit, err := r.InstructionManifest(t.Context(), claim.Turn.ID)
			if err != nil || len(audit.Sources) != 2 || audit.Sources[0].Scope != want {
				t.Fatalf("duplicate/mis-scoped sources: %+v %v", audit, err)
			}
		})
	}
}

func TestBothEnginesProjectInstructionsRefreshDelegateAndReadSkills(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			requests := make(chan model.Request, 20)
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				requests <- request
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.User && strings.Contains(last.Parts[0].Text, " read") {
					code := `p=skills.read(scope="project",root_id="repo",name="ancestor",offset="0",length=65536); print(p["source"]["scope"],p["source"]["path"])`
					if engine == session.QuickJS {
						code = `const p=await skills.read({scope:"project",root_id:"repo",name:"ancestor",offset:"0",length:65536}); console.log(p.source.scope,p.source.path);`
					}
					raw, _ := json.Marshal(map[string]string{"code": code})
					return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "read", Name: "execute", Arguments: raw}}}}, nil
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			})
			boundary, directory := t.TempDir(), t.TempDir()
			cwd := filepath.Join(boundary, "app")
			if err := os.Mkdir(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(cwd, alias); err != nil {
				t.Fatal(err)
			}
			writeInstructionFile(t, filepath.Join(boundary, "AGENTS.md"), "ANCESTOR_BEFORE")
			writeInstructionFile(t, filepath.Join(cwd, "AGENTS.md"), "LOCAL_RULE")
			writeRuntimeSkill(t, boundary, "ancestor", "---\nname: ancestor\ndescription: inherited\ndisable-model-invocation: true\n---\n", "EXPLICIT_ANCESTOR_BODY")
			host, err := config.Initialize(directory)
			if err != nil {
				t.Fatal(err)
			}
			host.ProjectRoots = map[string]string{"repo": boundary}
			if err := config.Save(directory, host); err != nil {
				t.Fatal(err)
			}
			r := openEngineTest(t, directory, provider)
			owner := projectSession(t, r, alias, engine)
			grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "project", SessionID: owner.ID, Capability: "instructions.read", Resource: "project:repo"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "project-read"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "$ancestor read"}}}); err != nil {
				t.Fatal(err)
			}
			first := nextInstructionRequest(t, requests)
			if !strings.Contains(first.Instructions, "ANCESTOR_BEFORE") || !strings.Contains(first.Instructions, "LOCAL_RULE") || !strings.Contains(first.Instructions, "EXPLICIT_ANCESTOR_BODY") {
				t.Fatal("project grant alone did not capture complete ancestor instructions")
			}
			done := waitTestWithin(t, r, "project-read", terminal, 30*time.Second)
			if done.Turn.State != session.Succeeded {
				t.Fatalf("project skill read failed: %+v", done.Turn)
			}
			second := nextInstructionRequest(t, requests)
			result := second.Messages[len(second.Messages)-1].Parts[0].Result
			if result == nil || result.IsError || !strings.Contains(result.Output, "project .agents/skills/ancestor/SKILL.md") || second.Instructions != first.Instructions {
				t.Fatalf("guest read or frozen instructions failed: %+v", result)
			}
			audit, err := r.InstructionManifest(t.Context(), first.TurnID)
			if err != nil || len(audit.Sources) != 4 {
				t.Fatalf("audit=%+v err=%v", audit, err)
			}
			for _, source := range audit.Sources {
				if source.Scope != "project" || source.RootID == nil || *source.RootID != "repo" || filepath.IsAbs(source.Path) {
					t.Fatal("project source identity lost", source)
				}
			}
			writeInstructionFile(t, filepath.Join(boundary, "AGENTS.md"), "ANCESTOR_AFTER")
			for _, restricted := range []bool{true, false} {
				key := "delegated"
				var grants []session.GrantID
				if restricted {
					key, grants = "restricted", []session.GrantID{}
				}
				child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, store.ChildRequest{ParentID: owner.ID, GrantIDs: grants, Parts: []session.Part{{Type: "text", Text: key}}})
				if err != nil {
					t.Fatal(err)
				}
				request := nextInstructionRequest(t, requests)
				if strings.Contains(request.Instructions, "ANCESTOR_AFTER") == restricted || request.SessionID != child.Session.ID {
					t.Fatal("child authority did not control inherited sources")
				}
				if got := waitTest(t, r, key, terminal); got.Turn.State != session.Succeeded {
					t.Fatalf("child failed: %+v", got.Turn)
				}
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			writeInstructionFile(t, filepath.Join(boundary, "AGENTS.md"), "ANCESTOR_RESTARTED")
			r = openEngineTest(t, directory, provider)
			retained, err := r.InstructionManifest(t.Context(), first.TurnID)
			if err != nil || !reflect.DeepEqual(retained, audit) {
				t.Fatal("restart changed capture", err)
			}
			submitTest(t, r, owner.ID, "restart")
			if request := nextInstructionRequest(t, requests); !strings.Contains(request.Instructions, "ANCESTOR_RESTARTED") {
				t.Fatal("restart reused stale project files")
			}
			waitTest(t, r, "restart", terminal)
			if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
				t.Fatal(err)
			}
			writeInstructionFile(t, filepath.Join(boundary, "AGENTS.md"), "\xff")
			submitTest(t, r, owner.ID, "revoked")
			if request := nextInstructionRequest(t, requests); strings.Contains(request.Instructions, "ANCESTOR_") {
				t.Fatal("revoked project sources were read")
			}
			waitTest(t, r, "revoked", terminal)
		})
	}
}

func TestProjectSkillReadCannotReuseForeignScopeOrUnselectedBoundary(t *testing.T) {
	r, owner, cell, _ := skillReadFixture(t)
	r.host.ProjectRoots = map[string]string{"shared": owner.WorkingDirectory}
	for _, args := range []map[string]any{
		{"scope": "project", "root_id": "shared", "name": "exact"},
		{"scope": "workspace", "root_id": "shared", "name": "exact"},
		{"scope": "project", "name": "exact"},
		{"scope": "other", "root_id": "shared", "name": "exact"},
	} {
		if _, err := r.prepareSkillRead(t.Context(), owner, tool.Invocation{SessionID: owner.ID, CellID: cell.ID, Module: "skills", Name: "read", Arguments: args}); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid locator accepted: %v %v", args, err)
		}
	}
}
