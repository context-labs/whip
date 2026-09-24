package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writePromptSkill(t *testing.T, root, name, text string) string {
	t.Helper()
	path := filepath.Join(root, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPromptCatalogRetainsScanOrderingAndMetadata(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writePromptSkill(t, first, "z", "---\nname: shared\ndescription: from first root\n---\nbody")
	writePromptSkill(t, first, "a", "---\ndescription: fallback name\ndisable-model-invocation: true\n---\nbody")
	writePromptSkill(t, second, "a", "---\nname: shared\ndescription: from second root\n---\nbody")
	want := Scan(first, second)
	got, err := LoadPromptCatalog(filepath.Join(t.TempDir(), "missing"), first, second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strict loader changed catalog precedence or metadata\ngot: %#v\nwant: %#v", got, want)
	}
}

func TestLoadPromptCatalogBoundsFrontmatterWithoutLoadingBody(t *testing.T) {
	dir := t.TempDir()
	writePromptSkill(t, dir, "huge-body", "---\nname: huge-body\ndescription: complete header\n---\n"+strings.Repeat("body", 2*maxPromptMetadataBytes))
	got, err := LoadPromptCatalog(dir)
	if err != nil || len(got) != 1 || got[0].Description != "complete header" {
		t.Fatalf("large skill bodies must not alter bounded metadata: %#v, %v", got, err)
	}
	writePromptSkill(t, dir, "huge-header", "---\ndescription: "+strings.Repeat("x", maxPromptMetadataBytes)+"\n---\n")
	if got, err := LoadPromptCatalog(dir); err == nil || got != nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized frontmatter must fail explicitly: %#v, %v", got, err)
	}
}

func TestLoadPromptCatalogRejectsBrokenApplicableSources(t *testing.T) {
	for _, failure := range []string{"no-frontmatter", "unclosed", "directory", "bad-directory", "invalid-utf8", "broken-symlink"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			switch failure {
			case "no-frontmatter":
				writePromptSkill(t, dir, "broken", "not a skill header")
			case "unclosed":
				writePromptSkill(t, dir, "broken", "---\nname: broken\n")
			case "directory":
				if err := os.MkdirAll(filepath.Join(dir, "broken", "SKILL.md"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "bad-directory":
				dir = writePromptSkill(t, dir, "broken", "---\nname: broken\n---\n")
			case "invalid-utf8":
				writePromptSkill(t, dir, "broken", "---\nname: broken\ndescription: \xff\n---\n")
			case "broken-symlink":
				if err := os.Mkdir(filepath.Join(dir, "broken"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken", "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := LoadPromptCatalog(dir); err == nil || got != nil {
				t.Fatalf("broken source silently omitted: %#v, %v", got, err)
			}
		})
	}
}
