package instruction

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func skillDocument(name, description string, disabled bool) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\ndisable-model-invocation: %t\n---\n", name, description, disabled)
}

func TestNamedRootsSharePrecedenceAndRetainEverySource(t *testing.T) {
	firstDir, first := workspace(t)
	secondDir, second := workspace(t)
	workDir, work := workspace(t)
	writeFile(t, firstDir, "a/SKILL.md", skillDocument("same", "FIRST", false)+"FIRST_BODY")
	writeFile(t, secondDir, "a/SKILL.md", skillDocument("same", "SECOND_OLD", false))
	writeFile(t, secondDir, "z/SKILL.md", skillDocument("same", "SECOND_NEW", true))
	writeFile(t, secondDir, "other/SKILL.md", skillDocument("host", "HOST_VISIBLE", false))
	writeFile(t, workDir, ".agents/skills/a/SKILL.md", skillDocument("same", "WORKSPACE_WINS", true))
	writeFile(t, workDir, ".agents/skills/b/SKILL.md", skillDocument("workspace", "WORKSPACE_VISIBLE", false))
	roots := []Root{{FS: work}, {ID: "first", FS: first}, {ID: "second", FS: second}}
	before := append([]Root(nil), roots...)
	catalog, err := Catalog(t.Context(), roots)
	if err != nil || len(catalog.Skills) != 3 || len(catalog.Sources) != 6 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	if !reflect.DeepEqual(roots, before) {
		t.Fatal("catalog reordered caller roots")
	}
	winner := catalog.Skills[1]
	if winner.Name != "same" || winner.Description != "WORKSPACE_WINS" || !winner.Disabled || winner.Source.Scope != "workspace" || winner.Source.RootID != nil {
		t.Fatalf("workspace did not override disabled host winner: %+v", winner)
	}
	for i, id := range []string{"first", "second", "second", "second", "", ""} {
		source := catalog.Sources[i]
		if err := source.Validate(); err != nil {
			t.Fatal(err)
		}
		if id == "" {
			if source.Scope != "workspace" || source.RootID != nil {
				t.Fatalf("workspace attribution=%+v", source)
			}
		} else if source.Scope != "host" || source.RootID == nil || *source.RootID != id {
			t.Fatalf("host attribution=%+v", source)
		}
	}
	policy := session.Instructions{DiscoverSkills: true, SkillRoots: []string{"first", "second"}}
	snapshot, err := Load(t.Context(), roots, policy, []string{"same", "host", "same"})
	if err != nil || len(snapshot.Selected) != 2 || snapshot.Selected[0].Source.Scope != "workspace" || !snapshot.Selected[0].Disabled || !reflect.DeepEqual(snapshot.Sources, catalog.Sources) {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	for _, want := range []string{"skills.read", "<root_id>null</root_id>", "<root_id>" + html.EscapeString(`"second"`) + "</root_id>", "HOST_VISIBLE", "WORKSPACE_VISIBLE"} {
		if !strings.Contains(snapshot.Text, want) {
			t.Fatalf("prompt missing %q: %s", want, snapshot.Text)
		}
	}
	for _, absent := range []string{firstDir, secondDir, workDir, "FIRST", "SECOND", "WORKSPACE_WINS", "files.read"} {
		if strings.Contains(snapshot.Text, absent) {
			t.Fatalf("prompt leaked %q", absent)
		}
	}
	withoutWorkspace, err := Catalog(t.Context(), roots[1:])
	if err != nil || withoutWorkspace.Skills[1].Description != "SECOND_NEW" || !withoutWorkspace.Skills[1].Disabled {
		t.Fatalf("last root/path winner=%+v err=%v", withoutWorkspace, err)
	}
	reversed, err := Catalog(t.Context(), []Root{roots[2], roots[1]})
	if err != nil || reversed.Skills[1].Description != "FIRST" {
		t.Fatalf("host policy order ignored: %+v err=%v", reversed, err)
	}
}

func TestNamedRootReadPreservesAttributionAndHash(t *testing.T) {
	dir, root := workspace(t)
	header := skillDocument("same", "<read & learn>", false)
	writeFile(t, dir, "same/SKILL.md", header+"BODY")
	// Host project rules are not workspace project rules.
	writeFile(t, dir, "AGENTS.md", "HOST_RULES_MUST_NOT_BE_READ")
	roots := []Root{{ID: "null", FS: root}}
	snapshot, err := Load(t.Context(), roots, session.Instructions{DiscoverSkills: true, ProjectFiles: []string{"AGENTS.md"}}, []string{"same"})
	if err != nil || len(snapshot.Sources) != 1 || len(snapshot.Selected) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if strings.Contains(snapshot.Text, "HOST_RULES") || strings.Contains(snapshot.Text, "<read & learn>") || !strings.Contains(snapshot.Text, html.EscapeString(`"null"`)) {
		t.Fatalf("incorrect catalog rendering: %s", snapshot.Text)
	}
	selected := snapshot.Selected[0]
	// Caller copies may have distinct RootID pointer identity.
	selected.Source.RootID = new("null")
	body := header + "FRESH_BODY π"
	writeFile(t, dir, "same/SKILL.md", body)
	text, evidence, err := ReadSkill(t.Context(), root, selected)
	hash := sha256.Sum256([]byte(body))
	metadataHash := sha256.Sum256([]byte(header))
	if err != nil || text != body || evidence.Kind != "invoked_skill" || evidence.Scope != "host" || evidence.RootID == nil || *evidence.RootID != "null" || evidence.Path != "same/SKILL.md" || evidence.Bytes != int64(len(body)) || evidence.SHA256 != hex.EncodeToString(hash[:]) || selected.Source.SHA256 != hex.EncodeToString(metadataHash[:]) {
		t.Fatalf("body evidence=%+v err=%v", evidence, err)
	}
	roots[0].ID = "changed"
	if *evidence.RootID != "null" || *selected.Source.RootID != "null" {
		t.Fatal("root input mutation changed captured attribution")
	}
	writeFile(t, dir, "same/SKILL.md", skillDocument("same", "changed metadata", false)+"BODY")
	if _, _, err := ReadSkill(t.Context(), root, selected); err == nil {
		t.Fatal("changed host metadata accepted after selection")
	}
}

func TestInstructionRootValidationIsBoundedAndSourceFree(t *testing.T) {
	_, root := workspace(t)
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	var tooMany []Root
	for i := range session.MaxSkillRoots + 1 {
		tooMany = append(tooMany, Root{ID: fmt.Sprintf("root%d", i), FS: root})
	}
	for name, roots := range map[string][]Root{
		"duplicate host":      {{ID: "same", FS: root}, {ID: "same", FS: root}},
		"duplicate workspace": {{FS: root}, {FS: root}},
		"invalid ID":          {{ID: "../outside", FS: root}},
		"nil directory":       {{ID: "named"}},
		"too many named":      tooMany,
		"too many total":      append(append([]Root(nil), tooMany...), Root{FS: root}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Catalog(t.Context(), roots); err == nil || errors.Is(err, os.ErrClosed) {
				t.Fatalf("invalid roots were accepted or accessed: %v", err)
			}
		})
	}
	allowed := append(append([]Root(nil), tooMany[:session.MaxSkillRoots]...), Root{FS: root})
	snapshot, err := Load(t.Context(), allowed, session.Instructions{Text: "static only"}, nil)
	if err != nil || snapshot.Text != "static only" || len(snapshot.Sources) != 0 {
		t.Fatalf("static load touched closed roots: %+v %v", snapshot, err)
	}
}

func TestNamedCatalogBoundsApplyAcrossRootsBeforeDeduplication(t *testing.T) {
	for _, kind := range []string{"metadata", "directory entries"} {
		t.Run(kind, func(t *testing.T) {
			dir, root := workspace(t)
			count := maxEntries/2 + 1
			if kind == "metadata" {
				count = maxSkills/2 + 1
			}
			for i := range count {
				path := fmt.Sprintf("entry-%04d", i)
				if kind == "metadata" {
					// Every file is a disabled duplicate, but still consumes audit space.
					writeFile(t, dir, path+"/SKILL.md", skillDocument("duplicate", "hidden", true))
				} else {
					writeFile(t, dir, path, "not a skill directory")
				}
			}
			if _, err := Catalog(t.Context(), []Root{{ID: "one", FS: root}}); err != nil {
				t.Fatalf("single root should fit: %v", err)
			}
			if _, err := Catalog(t.Context(), []Root{{ID: "one", FS: root}, {ID: "two", FS: root}}); err == nil {
				t.Fatal("combined root bound was not enforced")
			}
		})
	}
}

func TestNamedRootSkillReadsStayConfined(t *testing.T) {
	for _, kind := range []string{"outside", "dangling", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir, root := workspace(t)
			path := filepath.Join(dir, "skill", "SKILL.md")
			writeFile(t, dir, "skill/SKILL.md", skillDocument("skill", "safe", false))
			catalog, err := Catalog(t.Context(), []Root{{ID: "shared", FS: root}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "outside":
				outside := t.TempDir()
				writeFile(t, outside, "SKILL.md", skillDocument("skill", "safe", false)+"PRIVATE")
				err = os.Symlink(filepath.Join(outside, "SKILL.md"), path)
			case "dangling":
				err = os.Symlink("missing", path)
			case "fifo":
				err = syscall.Mkfifo(path, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := ReadSkill(t.Context(), root, catalog.Skills[0]); err == nil || strings.Contains(err.Error(), dir) {
				t.Fatalf("unsafe body read or host path leak: %v", err)
			}
			if _, err := Catalog(t.Context(), []Root{{ID: "shared", FS: root}}); err == nil || strings.Contains(err.Error(), dir) {
				t.Fatalf("unsafe catalog read or host path leak: %v", err)
			}
		})
	}
}

func TestDescriptorReadErrorsDoNotExposeHostPaths(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "skill/SKILL.md", skillDocument("skill", "safe", false))
	file, err := root.Open("skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readComplete(file, info, session.MaxInvokedSkillBytes); !errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), dir) {
		t.Fatalf("underlying error lost or host path exposed: %v", err)
	}
}
