package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every style in the tui package comes from the theme (currentTheme().On,
// th.Selected, th.MutedText, ...) or a ui component; ad-hoc lipgloss.NewStyle
// calls outside theme/ and ui/ are not allowed. This is the Phase 2 exit
// criterion of the design-system plan, kept as a test.
const styleSiteBaseline = 0

func TestNoAdHocStyles(t *testing.T) {
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
		t.Fatalf("%d ad-hoc lipgloss.NewStyle( sites: route new styles through internal/tui/theme or a ui component\n%v", total, perFile)
	}
}
