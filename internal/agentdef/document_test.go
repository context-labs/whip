package agentdef

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltInsRoundTripThroughTheDocument(t *testing.T) {
	for _, id := range IDs() {
		definition, _ := Lookup(id)
		encoded, err := Encode(definition)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !reflect.DeepEqual(decoded, definition.Normalize()) {
			t.Fatalf("%s did not round trip:\n%+v\n%+v", id, decoded, definition)
		}
	}
}

// The committed fixture is what the TypeScript SDK emits for JuniorDeveloper.
// With its id set to the built-in's, it must be the same definition.
func TestTypeScriptJuniorDeveloperMatchesBuiltIn(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("testdata", "junior-developer.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.ValidateRegistration(); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "junior-developer-ts" {
		t.Fatalf("fixture id = %q", decoded.ID)
	}
	decoded.ID = "junior-developer"
	if want := JuniorDeveloper().Normalize(); !reflect.DeepEqual(decoded, want) {
		t.Fatalf("fixture differs from the built-in:\n got %+v\nwant %+v", decoded, want)
	}
}

func TestRevisionIgnoresKeyOrderAndWhitespace(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("testdata", "junior-developer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(document, &generic); err != nil {
		t.Fatal(err)
	}
	// Re-encode through a map: keys sort alphabetically and whitespace vanishes.
	reordered, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Decode(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Decode(reordered)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Revision(first)
	b, _ := Revision(second)
	if a == "" || a != b || len(a) != 64 {
		t.Fatalf("revisions %q and %q", a, b)
	}
	first.Instructions.Persona += "!"
	if c, _ := Revision(first); c == a {
		t.Fatal("changed content kept its revision")
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	if _, err := Decode([]byte(`{"id":"x","modules":["context"],"persona":"misplaced"}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestValidateRegistrationRules(t *testing.T) {
	base := func() Definition {
		d := JuniorDeveloper()
		d.ID = "helper"
		return d
	}
	if err := base().ValidateRegistration(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Definition){
		"reserved id":        func(d *Definition) { d.ID = "coding" },
		"bad id":             func(d *Definition) { d.ID = "Helper Bot" },
		"child unknown tool": func(d *Definition) { d.Children = map[string]Child{"w": {Tools: []string{"missing"}}} },
		"mcp without cap":    func(d *Definition) { d.MCP.Servers = []string{"docs"} },
		"tool bad name":      func(d *Definition) { d.Tools = []Tool{{Name: "Bad-Name", InputSchema: json.RawMessage(`{}`)}} },
		"tool schema":        func(d *Definition) { d.Tools = []Tool{{Name: "ok", InputSchema: json.RawMessage(`[]`)}} },
		"tool timeout": func(d *Definition) {
			d.Tools = []Tool{{Name: "ok", InputSchema: json.RawMessage(`{}`), TimeoutMillis: int64(MaxToolTimeout.Milliseconds()) + 1}}
		},
		"tool repeated": func(d *Definition) {
			d.Tools = []Tool{{Name: "ok", InputSchema: json.RawMessage(`{}`)}, {Name: "ok", InputSchema: json.RawMessage(`{}`)}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := base()
			mutate(&d)
			if err := d.ValidateRegistration(); err == nil {
				t.Fatalf("%s accepted", name)
			}
		})
	}
	withTools := base()
	withTools.Tools = []Tool{{Name: "lookup", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}, {Name: "other", InputSchema: json.RawMessage(`{}`)}}
	withTools.Children = map[string]Child{"w": {Tools: []string{"lookup"}}}
	if err := withTools.ValidateRegistration(); err != nil {
		t.Fatal(err)
	}
	if withTools.Tools[0].Timeout() != DefaultToolTimeout {
		t.Fatalf("default timeout = %s", withTools.Tools[0].Timeout())
	}
	child, err := withTools.Child("w", ChildOverrides{})
	if err != nil || len(child.Tools) != 1 || child.Tools[0].Name != "lookup" {
		t.Fatalf("named child tools = %+v %v", child.Tools, err)
	}
	if _, err := child.Child("", ChildOverrides{Tools: []string{"other"}}); err == nil || !strings.Contains(err.Error(), `tool "other" is not available`) {
		t.Fatalf("tool widening error = %v", err)
	}
}

// Hooks are canonical too: absent hooks encode as null, an empty hooks object
// normalizes to null, and an empty operation filter is null.
func TestHooksAreCanonicalInTheDocument(t *testing.T) {
	plain, err := Encode(JuniorDeveloper())
	if err != nil || !strings.Contains(string(plain), `"hooks":null`) {
		t.Fatalf("hooks absent encoding = %s %v", plain, err)
	}
	definition := JuniorDeveloper()
	definition.ID = "hooked"
	definition.Hooks = &Hooks{}
	if definition.Normalize().Hooks != nil {
		t.Fatal("empty hooks object survived normalization")
	}
	definition.Hooks = &Hooks{BeforeTool: &Hook{Operations: []string{}}, TurnStart: &Hook{Optional: true, TimeoutMillis: 500}}
	encoded, err := Encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"hooks":{"before_tool":{"operations":null,"optional":false,"timeout_millis":0},"before_spawn":null,"turn_start":{"operations":null,"optional":true,"timeout_millis":500}}`) {
		t.Fatalf("hooks encoding = %s", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, definition.Normalize()) {
		t.Fatalf("hooks did not round trip: %+v %v", decoded.Hooks, err)
	}
	bad, err := Decode([]byte(`{"id":"x","modules":["context"],"hooks":{"before_tool":{"operations":["shell.dance"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.ValidateRegistration(); err == nil || !strings.Contains(err.Error(), `unknown operation "shell.dance"`) {
		t.Fatalf("unknown hook operation accepted: %v", err)
	}
	first, _ := Revision(definition)
	definition.Hooks.TurnStart.Optional = false
	second, _ := Revision(definition)
	if first == second {
		t.Fatal("hook changes did not change the revision")
	}
}
