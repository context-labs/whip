package helperprogram

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRetainedBrowserAndComputerHelperSyntax(t *testing.T) {
	calls, err := Parse(`# TextEdit's page; user's data is not code
 goto("https://example.com"); waitLoad()
 print(js("(()=>{const a=1;return a})()"))
 fill('#q', "paper, towels")
 press(Enter)
 print(state('TextEdit'))
 upload("#file", ["/tmp/a", "/tmp/b"])
 waitFor(".ok", true)`)
	if err != nil || len(calls) != 8 {
		t.Fatal(calls, err)
	}
	if calls[0].Name != "goto" || calls[0].Arguments[0] != "https://example.com" || calls[3].Arguments[0] != "#q" || calls[4].Arguments[0] != "Enter" {
		t.Fatal(calls)
	}
	nested := calls[2].Arguments[0].(Call)
	if nested.Name != "js" || nested.Arguments[0] != "(()=>{const a=1;return a})()" {
		t.Fatal(nested)
	}
	if calls[5].Arguments[0].(Call).Arguments[0] != "TextEdit" {
		t.Fatal(calls[5])
	}
	if len(calls[6].Arguments[1].([]any)) != 2 || calls[7].Arguments[1] != true {
		t.Fatal(calls)
	}
}

func TestHelperLiteralsPreserveExactNumbersStringsAndNestedJSON(t *testing.T) {
	calls, err := Parse(`click(9007199254740993, -0, 20.5); print('TextEdit\'s "notes"\n😀'); value({"nested":[null,9007199254740993]})`)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"9007199254740993", "-0", "20.5"} {
		if calls[0].Arguments[index].(json.Number).String() != want {
			t.Fatal(calls)
		}
	}
	if calls[1].Arguments[0] != "TextEdit's \"notes\"\n😀" {
		t.Fatal(calls[1])
	}
	nested := calls[2].Arguments[0].(map[string]any)["nested"].([]any)
	if nested[0] != nil || nested[1].(json.Number).String() != "9007199254740993" {
		t.Fatal(nested)
	}
}

func TestHelperProgramBoundsAndInvalidSuffixesFailBeforeExecution(t *testing.T) {
	for _, code := range []string{
		"# only a comment", "", strings.Repeat(" ", MaxBytes+1), "a(\"\xff\")", "a(\"\x00\")",
		`print()`, `print(1,2)`, `not.a.helper()`, `a("unterminated)`, `a(1))`, `a(1) trailing`,
		`a([1,2})`, `a(1 2)`, `a(1,)`, `a(,1)`, `a(-1oops)`, `a("quoted" trailing)`,
		strings.Repeat("a();", MaxCalls+1), `a(` + strings.Repeat("1,", MaxArguments) + `1)`,
		strings.Repeat("print(", MaxPrintDepth+2) + `"x"` + strings.Repeat(")", MaxPrintDepth+2),
	} {
		if _, err := Parse(code); err == nil {
			t.Errorf("accepted invalid helper program %q", code[:min(len(code), 100)])
		}
	}
	calls, err := Parse(strings.Repeat("a();", MaxCalls))
	if err != nil || len(calls) != MaxCalls {
		t.Fatal("rejected exact call bound", err)
	}
	_, err = Parse(`unknown("do-not-echo-private-argument", broken value)`)
	if err == nil || strings.Contains(err.Error(), "do-not-echo") {
		t.Fatal("diagnostic exposed literal", err)
	}
}
