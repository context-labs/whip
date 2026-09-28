package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func writeRuntimeSkill(t *testing.T, workspace, directory, metadata, body string) string {
	t.Helper()
	path := filepath.Join(workspace, ".agents", "skills", directory, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeInstructionFile(t, path, metadata+body)
	return path
}

func TestSkillsInspectCurrentWinnersWithoutClaimingWork(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	policy := session.Instructions{Text: "literal", DiscoverSkills: false}
	owner, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	writeRuntimeSkill(t, owner.WorkingDirectory, "a", "---\nname: duplicate\ndescription: old\n---\n", "OLD")
	winner := writeRuntimeSkill(t, owner.WorkingDirectory, "z", "---\nname: duplicate\ndescription: winner\ndisable-model-invocation: true\n---\n", "BODY")
	writeRuntimeSkill(t, owner.WorkingDirectory, "next", "---\nname: next\ndescription: Next\n---\n", "BODY")
	queued := submitSkillInput(t, r, owner.ID, "queued-skill", "$duplicate")
	values, next, err := r.Skills(t.Context(), owner.ID, "", "", 1)
	if err != nil || len(values) != 0 || next != nil {
		t.Fatalf("ungranted catalog=%+v %v %v", values, next, err)
	}
	grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory})
	if err != nil {
		t.Fatal(err)
	}
	values, next, err = r.Skills(t.Context(), owner.ID, "", "", 1)
	if err != nil || len(values) != 1 || values[0].Name != "duplicate" || values[0].Description != "winner" || !values[0].Disabled || next == nil || *next != "duplicate" {
		t.Fatalf("winner page=%+v %v %v", values, next, err)
	}
	if values[0].Source.Path != ".agents/skills/z/SKILL.md" {
		t.Fatal("catalog picked wrong source")
	}
	values, next, err = r.Skills(t.Context(), owner.ID, "", *next, 1)
	if err != nil || len(values) != 1 || values[0].Name != "next" || next != nil {
		t.Fatalf("last page=%+v %v %v", values, next, err)
	}
	for _, prefix := range []string{"du", "Du"} {
		values, _, err = r.Skills(t.Context(), owner.ID, prefix, "", 100)
		want := 1
		if prefix == "Du" {
			want = 0
		}
		if err != nil || len(values) != want {
			t.Fatalf("prefix %s=%+v %v", prefix, values, err)
		}
	}
	observed, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "queued-skill"})
	if err != nil || observed.Turn != nil || !reflect.DeepEqual(observed, queued) {
		t.Fatalf("inspection changed admission: %+v %v", observed, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 0 {
		t.Fatalf("inspection created history: %+v %v", history, err)
	}
	if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	values, _, err = r.Skills(t.Context(), owner.ID, "du", "", 100)
	if err != nil || len(values) != 1 {
		t.Fatalf("stopped inspection=%+v %v", values, err)
	}
	if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	writeInstructionFile(t, winner, string([]byte{0xff}))
	values, _, err = r.Skills(t.Context(), owner.ID, "", "", 100)
	if err != nil || len(values) != 0 {
		t.Fatalf("revoked inspection probed files: %+v %v", values, err)
	}
	for _, limit := range []int{0, 101} {
		if _, _, err := r.Skills(t.Context(), owner.ID, "", "", limit); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("limit %d accepted: %v", limit, err)
		}
	}
	if _, _, err := r.Skills(t.Context(), "missing", "", "", 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing owner: %v", err)
	}
}

func TestInvokedSkillsCaptureOnlyCurrentCanonicalInput(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	policy := session.Instructions{Text: "literal", DiscoverSkills: false}
	owner, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	metadata := "---\nname: hidden\ndescription: Hidden\ndisable-model-invocation: true\n---\n"
	body := "CURRENT_BODY" + strings.Repeat("x", 70<<10)
	path := writeRuntimeSkill(t, owner.WorkingDirectory, "hidden", metadata, body)
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory}); err != nil {
		t.Fatal(err)
	}
	literal := "Please use $hidden, $hidden and $unknown."
	submitSkillInput(t, r, owner.ID, "literal", literal)
	claimed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	text, err := r.Instructions(t.Context(), claimed.Turn, claimed.Configuration.Instructions)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, "CURRENT_BODY") != 1 || strings.Contains(text, "<available_skills>") {
		t.Fatal("invocation was duplicated or automatic discovery was enabled")
	}
	manifest, err := r.InstructionManifest(t.Context(), claimed.Turn.ID)
	if err != nil || manifest == nil || len(manifest.Sources) != 2 || manifest.Sources[1].Kind != "invoked_skill" || manifest.Sources[1].Bytes != int64(len(metadata+body)) {
		t.Fatalf("body audit=%+v %v", manifest, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 1 || history[0].Parts[0].Text != literal {
		t.Fatalf("canonical input rewritten: %+v %v", history, err)
	}
	if _, err := r.store.Finish(t.Context(), claimed.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	writeInstructionFile(t, path, metadata+string([]byte{0xff}))
	submitSkillInput(t, r, owner.ID, "next", "next without reference")
	next, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	text, err = r.Instructions(t.Context(), next.Turn, next.Configuration.Instructions)
	if err != nil || strings.Contains(text, "CURRENT_BODY") {
		t.Fatalf("historical reference was expanded: %v", err)
	}
	nextManifest, err := r.InstructionManifest(t.Context(), next.Turn.ID)
	if err != nil || nextManifest == nil || len(nextManifest.Sources) != 0 {
		t.Fatalf("sourcefree next turn=%+v %v", nextManifest, err)
	}
	if _, err := r.store.Finish(t.Context(), next.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	submitSkillInput(t, r, owner.ID, "bad", "bad $hidden")
	bad, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Instructions(t.Context(), bad.Turn, bad.Configuration.Instructions); err == nil {
		t.Fatal("invalid selected body accepted")
	}
	if manifest, err := r.InstructionManifest(t.Context(), bad.Turn.ID); err != nil || manifest != nil {
		t.Fatalf("invalid body published audit: %+v %v", manifest, err)
	}
}

func TestInvokedSkillRechecksAuthorityAfterDiscovery(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	writeRuntimeSkill(t, owner.WorkingDirectory, "skill", "---\nname: skill\ndescription: Skill\n---\n", "BODY")
	grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory})
	if err != nil {
		t.Fatal(err)
	}
	submitSkillInput(t, r, owner.ID, "skill", "$skill")
	claimed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(owner.WorkingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	captured, err := instruction.Load(t.Context(), []instruction.Root{{FS: root}}, session.Instructions{}, []string{"skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.invokedInstructions(t.Context(), claimed.Turn.ID, []instruction.Root{{FS: root}}, &captured); err == nil {
		t.Fatal("catalog identity authorized a revoked body read")
	}
}

func submitSkillInput(t *testing.T, r *Runtime, owner session.SessionID, key, text string) store.Admission {
	t.Helper()
	value, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, store.Submission{SessionID: owner, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
