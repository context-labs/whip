package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// The you-web skill (installed under .agents/skills) must be discoverable and
// spec-clean: no warning means the name/description frontmatter validated.
func TestYouWebSkillScans(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills")); err != nil {
		t.Skip("no .agents/skills in this checkout")
	}
	sk := Scan(filepath.Join(root, ".agents", "skills"))
	for _, s := range sk {
		if s.Name == "you-web" {
			if s.Description == "" {
				t.Fatal("empty description")
			}
			if s.Warning != "" {
				t.Fatalf("spec warning: %s", s.Warning)
			}
			return
		}
	}
	t.Fatal("you-web not scanned from .agents/skills")
}
