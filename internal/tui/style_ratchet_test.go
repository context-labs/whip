package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// styleSiteBaseline is how many ad-hoc lipgloss.NewStyle( calls the tui
// package still has outside theme/ and ui/. New UI code must take its styles
// from the theme (currentTheme().On, th.Heading, ...) or a ui component, so
// this number only goes down: lower it when you remove sites, never raise it.
const styleSiteBaseline = 54

func TestAdHocStylesOnlyShrink(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	perFile := map[string]int{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(src), "lipgloss.NewStyle("); n > 0 {
			perFile[f] = n
			total += n
		}
	}
	if total > styleSiteBaseline {
		t.Fatalf("%d ad-hoc lipgloss.NewStyle( sites (baseline %d): route new styles through internal/tui/theme or a ui component\n%v", total, styleSiteBaseline, perFile)
	}
	if total < styleSiteBaseline {
		t.Logf("ad-hoc style sites are down to %d: lower styleSiteBaseline", total)
	}
}
