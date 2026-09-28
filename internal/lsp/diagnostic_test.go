package lsp

import (
	"strings"
	"testing"
)

func TestFormatSeverities(t *testing.T) {
	for _, tc := range []struct {
		sev  int
		want string
	}{
		{SeverityError, "ERROR [3:7] boom"},
		{SeverityWarning, "WARN [3:7] boom"},
		{SeverityInfo, "INFO [3:7] boom"},
		{SeverityHint, "HINT [3:7] boom"},
		{0, "ERROR [3:7] boom"}, // unset severity defaults to error
	} {
		if got := format(Diagnostic{Line: 3, Col: 7, Severity: tc.sev, Message: "boom"}); got != tc.want {
			t.Errorf("severity %d: %q", tc.sev, got)
		}
	}
	// Long messages are truncated with an ellipsis (rust-analyzer emits KBs).
	long := strings.Repeat("x", maxMsgLen+50)
	got := format(Diagnostic{Line: 1, Col: 1, Severity: SeverityError, Message: long})
	if !strings.HasSuffix(got, "…") || len(got) != len("ERROR [1:1] ")+maxMsgLen+len("…") {
		t.Errorf("truncation: len=%d suffix=%q", len(got), got[len(got)-4:])
	}
}

func TestBlockFiltersAndCaps(t *testing.T) {
	// Info/hint diagnostics are dropped entirely — no block at all.
	if got := block("a.go", []Diagnostic{{Severity: SeverityInfo}, {Severity: SeverityHint}}); got != "" {
		t.Errorf("info-only block: %q", got)
	}
	if got := block("a.go", nil); got != "" {
		t.Errorf("empty block: %q", got)
	}

	var many []Diagnostic
	for i := range maxPerFile + 3 {
		many = append(many, Diagnostic{Line: i + 1, Col: 1, Severity: SeverityError, Message: "e"})
	}
	many = append(many, Diagnostic{Severity: SeverityHint, Message: "ignored"})
	got := block("a.go", many)
	if !strings.HasPrefix(got, "\n\n<diagnostics file=\"a.go\">\n") || !strings.HasSuffix(got, "</diagnostics>") {
		t.Errorf("block wrapper: %q", got)
	}
	if n := strings.Count(got, "ERROR "); n != maxPerFile {
		t.Errorf("kept %d diagnostics, want %d", n, maxPerFile)
	}
	if !strings.Contains(got, "... and 3 more") || strings.Contains(got, "ignored") {
		t.Errorf("overflow line / hint filter: %q", got)
	}
}

func TestReportSiblings(t *testing.T) {
	edited := "/w/a.go"
	editedDiags := []Diagnostic{{Line: 1, Col: 1, Severity: SeverityError, Message: "undefined: foo"}}

	// No siblings: just the edited file's block.
	got := Report(edited, editedDiags, nil)
	if !strings.Contains(got, "undefined: foo") || strings.Contains(got, "other") {
		t.Errorf("no siblings: %q", got)
	}

	// Warning-only siblings and the edited file itself are not counted.
	sib := map[string][]Diagnostic{
		edited:    editedDiags,
		"/w/b.go": {{Severity: SeverityWarning, Message: "warn only"}},
	}
	if got := Report(edited, editedDiags, sib); strings.Contains(got, "cached errors") {
		t.Errorf("warning-only sibling must not be reported: %q", got)
	}

	// One erroring sibling: singular wording, block included.
	sib["/w/b.go"] = []Diagnostic{{Line: 2, Col: 3, Severity: SeverityError, Message: "b broke"}}
	got = Report(edited, editedDiags, sib)
	if !strings.Contains(got, `<diagnostics file="/w/b.go">`) || !strings.Contains(got, "errors in another file") {
		t.Errorf("single sibling: %q", got)
	}

	// More siblings than the cap: plural wording, sorted, capped.
	for _, p := range []string{"/w/c.go", "/w/d.go", "/w/e.go", "/w/f.go", "/w/g.go"} {
		sib[p] = []Diagnostic{{Line: 1, Col: 1, Severity: SeverityError, Message: "broke " + p}}
	}
	got = Report(edited, editedDiags, sib)
	if !strings.Contains(got, "cached errors in 6 other files, 5 shown") {
		t.Errorf("overflow wording: %q", got)
	}
	if n := strings.Count(got, "<diagnostics file="); n != maxSiblingFiles+1 {
		t.Errorf("blocks rendered: %d", n)
	}
	// Sorted by path: b..f shown, g dropped.
	if strings.Contains(got, "/w/g.go") {
		t.Errorf("siblings must be sorted and capped: %q", got)
	}
}

func TestSiblingErrorsSameDirOnly(t *testing.T) {
	all := map[string][]Diagnostic{
		"/w/a.go":     {{Severity: SeverityError}},
		"/w/b.go":     {{Severity: SeverityError}},
		"/w/c.go":     {{Severity: SeverityWarning}},
		"/w/sub/d.go": {{Severity: SeverityError}},
	}
	out := siblingErrors("/w/a.go", all)
	if len(out) != 1 || out["/w/b.go"] == nil {
		t.Errorf("siblingErrors = %v", out)
	}
}
