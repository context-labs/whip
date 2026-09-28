package instruction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

const (
	skillHeader = "---\nname: explicit\ndescription: Selected metadata.\ndisable-model-invocation: true\n---\n"
	skillPath   = ".agents/skills/explicit/SKILL.md"
)

func TestCatalogAndLoadShareWinnersAndAuditEveryMetadataSource(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, ".agents/skills/a-old/SKILL.md", "---\nname: alpha\ndescription: LOSER_VISIBLE\n---\nLOSER_BODY")
	writeFile(t, dir, ".agents/skills/m-other/SKILL.md", "---\nname: beta\ndescription: BETA_VISIBLE\n---\nBETA_BODY")
	writeFile(t, dir, ".agents/skills/z-new/SKILL.md", "---\nname: alpha\ndescription: DISABLED_WINNER\ndisable-model-invocation: true\n---\n"+strings.Repeat("\xffUNREAD_BODY", session.MaxInvokedSkillBytes))
	catalog, err := Catalog(t.Context(), root)
	if err != nil || len(catalog.Skills) != 2 || len(catalog.Sources) != 3 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	if catalog.Skills[0].Name != "alpha" || catalog.Skills[0].Source.Path != ".agents/skills/z-new/SKILL.md" || !catalog.Skills[0].Disabled || catalog.Skills[1].Name != "beta" {
		t.Fatalf("wrong winners/order: %+v", catalog.Skills)
	}
	for i, path := range []string{".agents/skills/a-old/SKILL.md", ".agents/skills/m-other/SKILL.md", ".agents/skills/z-new/SKILL.md"} {
		if catalog.Sources[i].Path != path || catalog.Sources[i].Kind != "skill_metadata" {
			t.Fatalf("loser/disabled audit lost: %+v", catalog.Sources)
		}
	}
	policy := session.Instructions{Text: "literal $unknown remains", DiscoverSkills: true}
	snapshot, err := Load(t.Context(), root, policy, []string{"beta", "unknown", "alpha", "beta", "Alpha"})
	if err != nil || len(snapshot.Selected) != 2 || !reflect.DeepEqual(snapshot.Selected, []Skill{catalog.Skills[1], catalog.Skills[0]}) || !reflect.DeepEqual(snapshot.Sources, catalog.Sources) {
		t.Fatalf("selection disagrees with catalog: %+v err=%v", snapshot, err)
	}
	if !strings.Contains(snapshot.Text, policy.Text) || !strings.Contains(snapshot.Text, "BETA_VISIBLE") {
		t.Fatal("prompt lost configured text or visible winner")
	}
	for _, absent := range []string{"LOSER_VISIBLE", "LOSER_BODY", "DISABLED_WINNER", "UNREAD_BODY", "BETA_BODY"} {
		if strings.Contains(snapshot.Text, absent) {
			t.Fatalf("prompt exposed %q", absent)
		}
	}
	policy.DiscoverSkills = false
	explicit, err := Load(t.Context(), root, policy, []string{"alpha"})
	if err != nil || explicit.Text != policy.Text || len(explicit.Selected) != 1 || !explicit.Selected[0].Disabled || !reflect.DeepEqual(explicit.Sources, catalog.Sources) {
		t.Fatalf("explicit-only discovery=%+v err=%v", explicit, err)
	}
	unknown, err := Load(t.Context(), root, policy, []string{"unknown"})
	if err != nil || unknown.Text != policy.Text || len(unknown.Selected) != 0 || len(unknown.Sources) != 3 {
		t.Fatalf("unknown invocation changed literal text: %+v err=%v", unknown, err)
	}
}

func TestInvokedNamesUsesOnlyDirectTextAndLegacyTokenRules(t *testing.T) {
	parts := []session.Part{
		{Type: "text", Text: "$one, $Two!? $one $two $three)\"' ordinary ($not-a-token) prefix$ignored\n$four: $ $... $five]"},
		{Type: "content", ReferenceID: "body", Text: "$attachment"},
		{Type: "tool_result", Result: &session.ToolResult{Output: "$result"}},
		{Type: "text", Text: "$Two\t$six;"},
	}
	before := slices.Clone(parts)
	want := []string{"one", "Two", "two", "three", "four", "five]", "six"}
	if got := InvokedNames(parts); !slices.Equal(got, want) || !reflect.DeepEqual(parts, before) {
		t.Fatalf("names=%v want=%v or parts changed", got, want)
	}
}

func TestSkillCatalogWithoutAuthorityAndCancelledReads(t *testing.T) {
	catalog, err := Catalog(t.Context(), nil)
	if err != nil || len(catalog.Skills) != 0 || len(catalog.Sources) != 0 {
		t.Fatalf("unauthorized catalog=%+v err=%v", catalog, err)
	}
	snapshot, err := Load(t.Context(), nil, session.Instructions{Text: "$explicit", DiscoverSkills: true}, []string{"explicit"})
	if err != nil || snapshot.Text != "$explicit" || len(snapshot.Selected) != 0 || len(snapshot.Sources) != 0 {
		t.Fatalf("unauthorized selection=%+v err=%v", snapshot, err)
	}
	if _, _, err := ReadSkill(t.Context(), nil, Skill{}); err == nil {
		t.Fatal("skill metadata became read authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Catalog(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled catalog=%v", err)
	}
	if _, _, err := ReadSkill(ctx, nil, Skill{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled body=%v", err)
	}
}

func TestReadSkillCapturesFreshBodyWithSelectedMetadata(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, skillPath, skillHeader+"old body")
	catalog, err := Catalog(t.Context(), root)
	if err != nil || len(catalog.Skills) != 1 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	full := skillHeader + strings.Repeat("x", session.MaxInvokedSkillBytes-len(skillHeader))
	writeFile(t, dir, skillPath, full)
	body, audit, err := ReadSkill(t.Context(), root, catalog.Skills[0])
	hash := sha256.Sum256([]byte(full))
	if err != nil || body != full || audit.Kind != "invoked_skill" || audit.Bytes != int64(len(full)) || audit.SHA256 != hex.EncodeToString(hash[:]) || audit.Path != skillPath || audit.Validate() != nil {
		t.Fatalf("fresh body bytes=%d audit=%+v err=%v", len(body), audit, err)
	}
	if catalog.Sources[0].Bytes != int64(len(skillHeader)) || catalog.Skills[0].Source != catalog.Sources[0] {
		t.Fatal("body read changed catalog metadata snapshot")
	}
	writeFile(t, dir, skillPath, full+"x")
	if body, _, err := ReadSkill(t.Context(), root, catalog.Skills[0]); err == nil || body != "" {
		t.Fatal("oversized invoked body was partially returned")
	}
}

func TestReadSkillRejectsChangedMetadataAndSelectionIdentity(t *testing.T) {
	for _, change := range []string{"name", "description", "disabled", "unrelated metadata", "selected name", "selected description", "selected disabled", "selected kind"} {
		t.Run(change, func(t *testing.T) {
			dir, root := workspace(t)
			writeFile(t, dir, skillPath, skillHeader+"original")
			catalog, err := Catalog(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			selected := catalog.Skills[0]
			header := skillHeader
			switch change {
			case "name":
				header = strings.Replace(header, "name: explicit", "name: changed", 1)
			case "description":
				header = strings.Replace(header, "Selected metadata.", "Changed metadata.", 1)
			case "disabled":
				header = strings.Replace(header, "true", "false", 1)
			case "unrelated metadata":
				header = strings.Replace(header, "description:", "license: changed\ndescription:", 1)
			case "selected name":
				selected.Name = "changed"
			case "selected description":
				selected.Description = "changed"
			case "selected disabled":
				selected.Disabled = false
			case "selected kind":
				selected.Source.Kind = "project_file"
			}
			writeFile(t, dir, skillPath, header+"new body")
			body, audit, err := ReadSkill(t.Context(), root, selected)
			if err == nil || body != "" || audit != (session.InstructionSource{}) {
				t.Fatalf("changed selection returned content: body=%q audit=%+v err=%v", body, audit, err)
			}
		})
	}
}

func TestReadSkillRejectsUnavailableOrInvalidSelectedBodies(t *testing.T) {
	for _, change := range []string{"missing", "directory", "fifo", "outside symlink", "inside changed metadata", "invalid UTF8", "NUL", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			dir, root := workspace(t)
			writeFile(t, dir, skillPath, skillHeader+"original")
			catalog, err := Catalog(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, skillPath)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			switch change {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "fifo":
				err = syscall.Mkfifo(path, 0o600)
			case "outside symlink":
				outside := t.TempDir()
				writeFile(t, outside, "SKILL.md", skillHeader+"outside secret")
				err = os.Symlink(filepath.Join(outside, "SKILL.md"), path)
			case "inside changed metadata":
				writeFile(t, dir, "changed.md", strings.Replace(skillHeader, "Selected", "Different", 1)+"new body")
				err = os.Symlink("../../../changed.md", path)
			case "invalid UTF8":
				writeFile(t, dir, skillPath, skillHeader+"\xff")
			case "NUL":
				writeFile(t, dir, skillPath, skillHeader+"\x00")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, _, err := ReadSkill(ctx, root, catalog.Skills[0])
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil || change == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("invalid body accepted: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("body open blocked before descriptor validation")
			}
		})
	}
}
