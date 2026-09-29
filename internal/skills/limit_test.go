package skills

import (
	"testing"
)

// Every skill that ships with the repo must be Agent Skills spec-clean:
// valid name, description ≤1024, parseable frontmatter. This test is the
// ratchet — the startup report warns on violations, and this fails CI.
func TestRepoSkillsSpecClean(t *testing.T) {
	sk, problems := ScanDetailed("../../.agents/skills")
	for _, p := range problems {
		t.Errorf("unparseable skill: %s: %s", p.Path, p.Err)
	}
	for _, s := range sk {
		if s.Warning != "" {
			t.Errorf("%s: %s", s.Name, s.Warning)
		}
	}
}
