package session

import (
	"reflect"
	"testing"
)

func TestBuiltinSkillDefaultsPreserveImmutableCustomPolicies(t *testing.T) {
	base := Configuration{Instructions: Instructions{SkillRoots: []string{"personal"}}}
	for _, document := range Builtins() {
		canonical, _, _, err := CanonicalDefinition(document)
		if err != nil {
			t.Fatal(err)
		}
		inherited := DefinitionInstructions(base.Instructions, canonical)
		if !reflect.DeepEqual(inherited.SkillRoots, base.Instructions.SkillRoots) || inherited.Text != document.Defaults.Instructions.Text || inherited.DiscoverSkills != document.Defaults.Instructions.DiscoverSkills {
			t.Fatal(document.ID, inherited)
		}
		inherited.SkillRoots[0] = "changed"
		if base.Instructions.SkillRoots[0] != "personal" {
			t.Fatal("aliased defaults")
		}
		custom := document
		custom.Name += " edited"
		if roots := DefinitionInstructions(base.Instructions, custom).SkillRoots; len(roots) != 0 {
			t.Fatal("custom policy widened", roots)
		}
		resolved, err := Resolve(base, document, ConfigPatch{Instructions: &Instructions{SkillRoots: []string{}, Text: "explicit"}})
		if err != nil || len(resolved.Instructions.SkillRoots) != 0 || resolved.Instructions.Text != "explicit" {
			t.Fatal(resolved, err)
		}
	}
}
