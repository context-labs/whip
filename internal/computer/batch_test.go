package computer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestComputerBatchCapturesWholeAppSetAndBroadScriptBeforeEffects(t *testing.T) {
	plan, err := CompileBatch(`state("TextEdit"); print(click("TextEdit",2)); chrome_tabs(); tell("Finder","tell application \"Other App\" to activate"); apps(); permissions()`)
	if err != nil {
		t.Fatal(err)
	}
	intent := plan.Intent()
	if !reflect.DeepEqual(intent.Applications, []string{"finder", "google chrome", "textedit"}) || !intent.BroadScript || !intent.Inventory || !intent.Permissions || !intent.Native {
		t.Fatal(intent)
	}
	intent.Applications[0] = "mutated"
	if plan.Intent().Applications[0] != "finder" {
		t.Fatal("intent aliases plan")
	}
	first, err := plan.Resource("generation-one")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := plan.Resource("generation-one")
	changed, _ := plan.Resource("generation-two")
	if first != again || first == changed {
		t.Fatal("scope resource does not bind generation")
	}
	other, err := CompileBatch(`tell("Finder","different script"); apps(); state("TEXTEDIT"); permissions(); chrome_tabs()`)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := other.Resource("generation-one")
	if same != first {
		t.Fatal("canonical app scope depends on order or source code")
	}
	narrow, _ := CompileBatch(`state("TextEdit")`)
	narrowResource, _ := narrow.Resource("generation-one")
	if narrowResource == first {
		t.Fatal("changed consent scope reused resource")
	}
}

func TestComputerBatchRetainsNativeAndAppleScriptVocabulary(t *testing.T) {
	programs := []string{
		`print("literal"); print(9007199254740993)`, `apps(); permissions()`, `state("A"); ax("A"); screenshot("A")`,
		`click("A",1); click("A",10,-20); type("A","text"); press("A",Return)`,
		`scroll("A",0); scroll("A",0,down); scroll("A",0,left,5)`,
		`set("A",1,"value"); select("A",1); select("A",1,"target"); menu("A",1,"AXPress")`,
		`tell("A","activate"); chrome_state(); chrome_tabs(); chrome_goto("https://example.com"); chrome_new_tab("http://127.0.0.1:3000")`,
		`chrome_activate(1,2); chrome_close(1,2); chrome_back(); chrome_reload(); chrome_js("document.title"); chrome_find("example")`,
	}
	for _, program := range programs {
		if _, err := CompileBatch(program); err != nil {
			t.Errorf("%s: %v", program, err)
		}
	}
}

func TestComputerBatchRejectsInvalidLastStepWithoutPartialPlan(t *testing.T) {
	for _, invalid := range []string{`unknown()`, `state(12)`, `state("")`, `state("A\nB")`, `state("A\u202eB")`, `click("A",1.5)`, `click("A",9007199254740993)`, `click("A",1,2,3)`, `scroll("A",1,sideways)`, `scroll("A",1,up,-1)`, `apps("A")`, `type("A")`, `menu("A",1)`, `chrome_activate(0,1)`, `chrome_close("1",2)`, `chrome_goto("file:///private")`, `chrome_new_tab("https://user:password@example.com")`} {
		plan, err := CompileBatch(`state("A"); ` + invalid)
		if err == nil || plan != nil {
			t.Errorf("invalid final step retained executable prefix: %s", invalid)
		}
	}
	if _, err := CompileBatch(`type("A",true)`); err == nil {
		t.Fatal("coerced boolean into typed text")
	}
	if _, err := CompileBatch(`tell("A", "` + strings.Repeat("x", 64<<10) + `")`); err == nil {
		t.Fatal("unbounded broad script accepted")
	}
}

func TestComputerBatchApplicationLimit(t *testing.T) {
	var program strings.Builder
	for i := range MaxBatchApplications {
		fmt.Fprintf(&program, "state(%q);", fmt.Sprintf("App %d", i))
	}
	if _, err := CompileBatch(program.String()); err != nil {
		t.Fatal(err)
	}
	program.WriteString(`state("one too many")`)
	if plan, err := CompileBatch(program.String()); err == nil || plan != nil {
		t.Fatal("unbounded app set accepted")
	}
}
