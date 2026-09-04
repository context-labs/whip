package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// literalPadBaseline is how many hand-written padding literals ("  " / "   ")
// the tui package still carries outside theme/ and ui/. Padding comes from
// th.Space (Gutter, PadX, PadY, Gap) through ui components; this number only
// goes down. Lower it when you remove sites, never raise it.
const literalPadBaseline = 41

func TestLiteralPaddingOnlyShrinks(t *testing.T) {
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
		n := strings.Count(string(src), `"  "`) + strings.Count(string(src), `"   "`)
		if n > 0 {
			perFile[f] = n
			total += n
		}
	}
	if total > literalPadBaseline {
		t.Fatalf("%d literal padding strings (baseline %d): take padding from th.Space or a ui component\n%v", total, literalPadBaseline, perFile)
	}
	if total < literalPadBaseline {
		t.Logf("literal padding is down to %d: lower literalPadBaseline", total)
	}
}
