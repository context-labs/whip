package instruction

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestProjectRulesCatalogAndInvocationShareOneChain(t *testing.T) {
	dir, fs := workspace(t)
	hostDir, host := workspace(t)
	writeFile(t, hostDir, "same/SKILL.md", skillDocument("same", "HOST", false))
	for _, prefix := range []string{".", "app", "app/nested"} {
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			writeFile(t, dir, filepath.Join(prefix, name), prefix+"/"+name)
		}
		writeFile(t, dir, filepath.Join(prefix, ".agents/skills/same/SKILL.md"), skillDocument("same", prefix, prefix == "app/nested")+"SELECTED_BODY")
	}
	writeFile(t, dir, "sibling/AGENTS.md", strings.Repeat("x", session.MaxInstructionSourceBytes+1))
	writeFile(t, dir, "sibling/.agents/skills/broken/SKILL.md", "broken")
	project := Root{ID: "shared", FS: fs, ProjectDirectories: []string{".", "app", "app/nested"}}
	roots := []Root{project, {ID: "shared", FS: host}}
	policy := session.Instructions{ProjectRoot: new("shared"), ProjectFiles: []string{"CLAUDE.md", "AGENTS.md"}, DiscoverSkills: true}
	got, err := Load(t.Context(), roots, policy, []string{"same"})
	if err != nil || len(got.Sources) != 10 || len(got.Selected) != 1 {
		t.Fatalf("snapshot=%+v err=%v", got, err)
	}
	previous := -1
	for _, source := range got.Sources[:6] {
		if source.Scope != "project" || source.RootID == nil || *source.RootID != "shared" {
			t.Fatalf("wrong project source: %+v", source)
		}
		position := strings.Index(got.Text, strconv.Quote("project:shared/"+source.Path))
		if position <= previous {
			t.Fatal("rules did not preserve directory and configured filename precedence")
		}
		previous = position
	}
	selected := got.Selected[0]
	if !selected.Disabled || selected.Description != "app/nested" || selected.Source.Path != "app/nested/.agents/skills/same/SKILL.md" || strings.Contains(got.Text, "SELECTED_BODY") {
		t.Fatalf("wrong winner or automatic body expansion: %+v", selected)
	}
	text, source, err := ReadSkill(t.Context(), project.FS, selected)
	if err != nil || !strings.Contains(text, "SELECTED_BODY") || source.Scope != "project" || source.Kind != "invoked_skill" {
		t.Fatalf("selected project read=%q %+v %v", text, source, err)
	}
	if strings.Contains(got.Text, dir) || strings.Contains(got.Text, hostDir) {
		t.Fatal("host path escaped into the prompt")
	}
}

func TestProjectReaderBoundsAndConfinesChain(t *testing.T) {
	dir, fs := workspace(t)
	for _, paths := range [][]string{{"app"}, {".", "a/b"}, {".", ".."}, {".", "a", "a"}, {".", "a\\b"}, {".", strings.Repeat("a", 4097)}} {
		if _, err := Catalog(t.Context(), []Root{{ID: "project", FS: fs, ProjectDirectories: paths}}); err == nil {
			t.Fatalf("invalid chain accepted: %q", paths)
		}
	}
	chain := []string{"."}
	for range MaxProjectDirectories {
		chain = append(chain, filepath.ToSlash(filepath.Join(chain[len(chain)-1], "a")))
	}
	if _, err := Catalog(t.Context(), []Root{{ID: "project", FS: fs, ProjectDirectories: chain}}); err == nil {
		t.Fatal("unbounded chain accepted")
	}
	out := filepath.Join(t.TempDir(), "rule")
	if err := os.WriteFile(out, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	root := Root{ID: "project", FS: fs, ProjectDirectories: []string{"."}}
	if _, err := Load(t.Context(), []Root{root}, session.Instructions{ProjectFiles: []string{"AGENTS.md"}}, nil); err == nil || strings.Contains(err.Error(), out) {
		t.Fatalf("escaping source accepted or host path leaked: %v", err)
	}
	if _, err := Catalog(t.Context(), []Root{root, {FS: fs}}); err == nil {
		t.Fatal("project chain and separate cwd scan would duplicate sources")
	}
	before := append([]string(nil), root.ProjectDirectories...)
	if _, err := Catalog(t.Context(), []Root{root}); err != nil || !reflect.DeepEqual(before, root.ProjectDirectories) {
		t.Fatal("catalog changed caller chain", err)
	}
}

func TestProjectReaderBoundsRulesAndCatalogTogether(t *testing.T) {
	dir, root := workspace(t)
	chain := []string{"."}
	for range 35 {
		chain = append(chain, filepath.Join(chain[len(chain)-1], "a"))
	}
	files := make([]string, 32)
	for i := range files {
		files[i] = "RULE_" + strconv.Itoa(i) + ".md"
	}
	lastRule := filepath.Join(chain[len(chain)-1], files[len(files)-1])
	for _, directory := range chain {
		for _, name := range files {
			path := filepath.Join(directory, name)
			if path != lastRule {
				writeFile(t, dir, path, "rule")
			}
		}
	}
	writeFile(t, dir, ".agents/skills/bounded/SKILL.md", skillDocument("bounded", "bounded metadata", false))
	roots := []Root{{ID: "project", FS: root, ProjectDirectories: chain}}
	for _, boundary := range []string{"exact", "overflow"} {
		if boundary == "overflow" {
			writeFile(t, dir, lastRule, "one rule too many")
		}
		for _, discover := range []bool{false, true} {
			t.Run(boundary+"/discover="+strconv.FormatBool(discover), func(t *testing.T) {
				policy := session.Instructions{ProjectRoot: new("project"), ProjectFiles: files, DiscoverSkills: discover}
				var invoked []string
				if !discover {
					invoked = []string{"bounded"}
				}
				got, err := Load(t.Context(), roots, policy, invoked)
				if boundary == "overflow" {
					if err == nil || !strings.Contains(err.Error(), "instruction source manifest exceeds bounds") || !reflect.DeepEqual(got, Snapshot{}) {
						t.Fatalf("over-bound snapshot returned: sources=%d err=%v", len(got.Sources), err)
					}
					return
				}
				if err != nil || len(got.Sources) != session.MaxInstructionSources || got.Sources[len(got.Sources)-1].Kind != "skill_metadata" {
					t.Fatalf("exact-bound snapshot rejected: sources=%d err=%v", len(got.Sources), err)
				}
			})
		}
	}
}
