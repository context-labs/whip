package runtime

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestAutomaticInstructionSourcesRequirePublishedSelectedScopeAndChildAuthority(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	setRuntimeMode(t, r, owner.ID, "automatic", 1, session.PermissionAutomatic)
	hostRoot := t.TempDir()
	r.host.SkillRoots = map[string]string{"selected": hostRoot, "unselected": filepath.Join(t.TempDir(), "absent")}
	r.host.ProjectRoots = map[string]string{"selected": owner.WorkingDirectory, "unselected": filepath.Join(t.TempDir(), "absent")}
	r.host.StandingInstructionsFile = filepath.Join(t.TempDir(), "standing.md")
	if err := config.Save(r.directory, r.host); err != nil {
		t.Fatal(err)
	}
	writeInstructionFile(t, filepath.Join(owner.WorkingDirectory, "AGENTS.md"), "PROJECT_RULE")
	writeInstructionFile(t, r.host.StandingInstructionsFile, "STANDING_RULE")
	writeRuntimeSkill(t, owner.WorkingDirectory, "local", "---\nname: local\ndescription: local skill\n---\n", "LOCAL_BODY")
	writeHostSkill(t, hostRoot, "shared", "---\nname: shared\ndescription: shared skill\n---\nSHARED_BODY")
	policy := session.Instructions{ProjectRoot: new("selected"), ProjectFiles: []string{"AGENTS.md"}, SkillRoots: []string{"selected"}, DiscoverSkills: true, StandingInstructions: true}
	owner, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "default"}, store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "$local $shared"}}})
	if err != nil {
		t.Fatal(err)
	}
	restricted, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "restricted"}, store.ChildRequest{ParentID: owner.ID, GrantIDs: []session.GrantID{}, Parts: []session.Part{{Type: "text", Text: "$local $shared"}}})
	if err != nil {
		t.Fatal(err)
	}
	changedCWD, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "other-cwd"}, store.ChildRequest{ParentID: owner.ID, WorkingDirectory: t.TempDir(), Parts: []session.Part{{Type: "text", Text: "$shared"}}})
	if err != nil {
		t.Fatal(err)
	}
	submitSkillInput(t, r, owner.ID, "root", "$local $shared")
	for _, target := range []struct {
		id      session.SessionID
		allowed bool
	}{{owner.ID, true}, {child.Session.ID, true}, {restricted.Session.ID, false}, {changedCWD.Session.ID, false}} {
		claimed, err := r.store.Claim(t.Context(), target.id)
		if err != nil {
			t.Fatal(err)
		}
		text, err := r.Instructions(t.Context(), claimed.Turn, claimed.Configuration.Instructions)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range []string{"PROJECT_RULE", "STANDING_RULE", "LOCAL_BODY", "SHARED_BODY"} {
			if strings.Contains(text, source) != target.allowed {
				t.Fatalf("session %s source %s allowed=%v", target.id, source, target.allowed)
			}
		}
		manifest, err := r.InstructionManifest(t.Context(), claimed.Turn.ID)
		wantSources := 0
		if target.allowed {
			wantSources = 6
		}
		if err != nil || manifest == nil || len(manifest.Sources) != wantSources {
			t.Fatalf("instruction manifest=%+v err=%v", manifest, err)
		}
		catalog, _, err := r.Skills(t.Context(), target.id, "", "", 100)
		wantSkills := 0
		if target.allowed {
			wantSkills = 2
		}
		if err != nil || len(catalog) != wantSkills {
			t.Fatalf("skill inspection disagrees with capture: %+v %v", catalog, err)
		}
	}
	// Full Access does not introduce any synthetic standing grants.
	grants, err := r.Grants(t.Context(), owner.ID, "", 100)
	if err != nil || len(grants) != 0 {
		t.Fatalf("automatic reads manufactured grants: %+v %v", grants, err)
	}
}

func TestInvokedSkillRechecksAutomaticAuthorityAfterDiscovery(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	setRuntimeMode(t, r, owner.ID, "automatic", 1, session.PermissionAutomatic)
	writeRuntimeSkill(t, owner.WorkingDirectory, "skill", "---\nname: skill\ndescription: Skill\n---\n", "BODY")
	submitSkillInput(t, r, owner.ID, "skill", "$skill")
	claimed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, authority, err := r.store.SessionInstructions(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	roots, closeRoots, err := r.instructionRoots(t.Context(), owner.WorkingDirectory, owner.Config.Instructions, authority, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	captured, err := instruction.Load(t.Context(), roots, session.Instructions{}, []string{"skill"})
	if err != nil || len(captured.Selected) != 1 {
		t.Fatalf("skill discovery=%+v %v", captured, err)
	}
	setRuntimeMode(t, r, owner.ID, "ask", 2, session.PermissionPrompt)
	if _, err := r.invokedInstructions(t.Context(), claimed.Turn.ID, roots, &captured); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("mode change after discovery still read skill body: %v", err)
	}
	if len(captured.Sources) != 1 || captured.Sources[0].Kind != "skill_metadata" {
		t.Fatalf("denied body added audit source: %+v", captured.Sources)
	}
}
