package tools

import (
	"testing"
)

func TestParseBrowserProgram(t *testing.T) {
	prog, err := parseHelperProgram(`# Searching example for widgets
goto("https://example.com"); waitLoad()
print(js("document.title"))
fill('#q', "paper, towels")
press(Enter)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(prog) != 5 {
		t.Fatalf("want 5 statements, got %d: %+v", len(prog), prog)
	}
	if prog[0].name != "goto" || prog[0].args[0] != "https://example.com" {
		t.Errorf("stmt0: %+v", prog[0])
	}
	// semicolon inside a js string must not split
	jsStmt := prog[2].args[0].(helperStmt)
	if jsStmt.name != "js" || jsStmt.args[0] != "document.title" {
		t.Errorf("nested js: %+v", jsStmt)
	}
	if prog[3].name != "fill" || prog[3].args[0] != "#q" || prog[3].args[1] != "paper, towels" {
		t.Errorf("fill args: %+v", prog[3].args)
	}
	if prog[4].name != "press" || prog[4].args[0] != "Enter" {
		t.Errorf("bare-word key arg: %+v", prog[4].args)
	}
}

func TestParseSemicolonsInsideStrings(t *testing.T) {
	prog, err := parseHelperProgram(`js("(()=>{const a=1;return a})()"); click(10, 20.5)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(prog) != 2 {
		t.Fatalf("want 2, got %d: %+v", len(prog), prog)
	}
	if prog[1].args[0] != 10.0 || prog[1].args[1] != 20.5 {
		t.Errorf("numeric args: %+v", prog[1].args)
	}
}

func TestParseCommentsAndBlanksOnly(t *testing.T) {
	if _, err := parseHelperProgram("# just a label\n// nothing else"); err == nil {
		t.Fatal("comment-only program must fail")
	}
}

func TestParseMalformed(t *testing.T) {
	if _, err := parseHelperProgram(`goto("https://x" `); err == nil {
		t.Fatal("unbalanced call must fail")
	}
	if _, err := parseHelperProgram(`print()`); err == nil {
		t.Fatal("empty print must fail")
	}
}

func TestParseArraysAndBools(t *testing.T) {
	prog, err := parseHelperProgram(`upload("#f", ["/tmp/a.png", "/tmp/b.png"]); waitFor(".ok", true)`)
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := prog[0].args[1].([]any)
	if !ok || len(arr) != 2 || arr[0] != "/tmp/a.png" {
		t.Errorf("array arg: %+v", prog[0].args[1])
	}
	if prog[1].args[1] != true {
		t.Errorf("bool arg: %+v", prog[1].args[1])
	}
}

// The tool must refuse to run without an installed manager instead of
// panicking the loop.
func TestBrowserExecNoManager(t *testing.T) {
	out := Execute(t.Context(), []Tool{BrowserExec(NewServices())}, "browser_exec", []byte(`{"code":"info()"}`))
	if out == "" || out[:5] != "Error" {
		t.Fatalf("want error string, got %q", out)
	}
}

func TestHelperStmtString(t *testing.T) {
	prog, err := parseHelperProgram(`goto("https://example.com")`)
	if err != nil {
		t.Fatal(err)
	}
	if got := prog[0].String(); got != `goto("https://example.com")` {
		t.Errorf("String must return the raw statement text: %q", got)
	}
}
