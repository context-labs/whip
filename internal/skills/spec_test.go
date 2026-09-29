package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec: agentskills.io/specification + pi's core/skills.ts enforcement.

func writeSpecSkill(t *testing.T, dir, name, frontmatter string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(frontmatter), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSpecNameValidation(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{ // dir name → expected warning fragment ("" = clean)
		"ok-name":     "",
		"BadCase":     "lowercase a-z, 0-9, hyphens",
		"under_score": "lowercase a-z, 0-9, hyphens",
		"-lead":       "must not start or end with a hyphen",
		"trail-":      "must not start or end with a hyphen",
		"double--hyp": "consecutive hyphens",
	}
	for name := range cases {
		writeSpecSkill(t, dir, name, "---\ndescription: x\n---\nbody\n")
	}
	long := strings.Repeat("a", 65)
	writeSpecSkill(t, dir, long, "---\ndescription: x\n---\n")

	sk, _ := ScanDetailed(dir)
	byName := map[string]Skill{}
	for _, s := range sk {
		byName[s.Name] = s
	}
	for name, want := range cases {
		s, ok := byName[name]
		if !ok {
			t.Errorf("%s: not scanned", name)
			continue
		}
		if want == "" && s.Warning != "" {
			t.Errorf("%s: unexpected warning %q", name, s.Warning)
		}
		if want != "" && !strings.Contains(s.Warning, want) {
			t.Errorf("%s: warning %q should contain %q", name, s.Warning, want)
		}
	}
	if !strings.Contains(byName[long].Warning, "exceeds 64") {
		t.Errorf("long name: %q", byName[long].Warning)
	}
}

func TestSpecDescriptionLimit(t *testing.T) {
	dir := t.TempDir()
	writeSpecSkill(t, dir, "fine", "---\ndescription: "+strings.Repeat("d", 1024)+"\n---\n")
	writeSpecSkill(t, dir, "over", "---\ndescription: "+strings.Repeat("d", 1025)+"\n---\n")
	sk, _ := ScanDetailed(dir)
	for _, s := range sk {
		if s.Name == "fine" && s.Warning != "" {
			t.Errorf("1024 chars is spec-legal: %q", s.Warning)
		}
		if s.Name == "over" && !strings.Contains(s.Warning, "exceeds 1024") {
			t.Errorf("1025 chars should warn: %q", s.Warning)
		}
	}
}

func TestSpecDisabledSkillMetadata(t *testing.T) {
	dir := t.TempDir()
	writeSpecSkill(t, dir, "xml-skill", "---\nname: xml-skill\ndescription: uses <angle> & \"quotes\"\n---\n")
	writeSpecSkill(t, dir, "hidden", "---\nname: hidden\ndescription: not in the catalog\ndisable-model-invocation: true\n---\n")
	sk := Scan(dir)
	var found bool
	for _, s := range sk {
		if s.Name == "hidden" && s.DisableModelInvocation {
			found = true
		}
	}
	if !found {
		t.Error("hidden skill should still scan with DisableModelInvocation=true")
	}
}
