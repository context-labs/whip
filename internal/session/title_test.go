package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAutomaticTitleSourceAndCandidateBoundaries(t *testing.T) {
	parts := []Part{{Type: "text", Text: " \n  日本語\t"}, {Type: "content", ReferenceID: "private-attachment"}, {Type: "text", Text: strings.Repeat("猫", 310)}}
	source := TitleSource(parts)
	if !strings.HasPrefix(source, "日本語 ") || utf8.RuneCountInString(source) != 300 || utf8.RuneCountInString(TitleFallback(source)) != 64 || strings.Contains(source, "attachment") {
		t.Fatal("source/fallback did not preserve authored Unicode bounds")
	}
	if TitleSource([]Part{{Type: "content", ReferenceID: "attachment"}}) != "" {
		t.Fatal("attachment initialized a title")
	}
	for _, value := range []string{"", " title", "title ", "one\ntwo", "one\rtwo", "one\ttwo", "one\x00two", "one\u2028two", "one\u2029two", "bad\xff", strings.Repeat("猫", 81)} {
		if ValidateAutomaticTitle(value) == nil {
			t.Fatal("invalid generated title accepted")
		}
	}
	if err := ValidateAutomaticTitle(strings.Repeat("猫", 80)); err != nil {
		t.Fatal("exact Unicode title bound rejected", err)
	}
}

func TestAutomaticTitlePolicyPreservesExplicitFalseAndInheritance(t *testing.T) {
	base := Configuration{AutomaticTitle: true, Model: ModelSelection{Provider: "fixture", Name: "model"}}
	for _, enabled := range []bool{true, false} {
		definition := DefinitionDocument{ID: "fixture", Name: "Fixture", Defaults: ConfigPatch{AutomaticTitle: new(enabled)}}
		canonical, raw, _, err := CanonicalDefinition(definition)
		if err != nil || !strings.Contains(string(raw), `"automatic_title":`) || canonical.Defaults.AutomaticTitle == nil || *canonical.Defaults.AutomaticTitle != enabled {
			t.Fatal("policy was omitted from immutable definition", err)
		}
		resolved, err := Resolve(base, canonical, ConfigPatch{})
		if err != nil || resolved.AutomaticTitle != enabled {
			t.Fatal("definition policy was not copied", err)
		}
		resolved, err = Resolve(resolved, DefinitionDocument{}, ConfigPatch{AutomaticTitle: new(!enabled)})
		if err != nil || resolved.AutomaticTitle == enabled {
			t.Fatal("explicit replacement was not applied", err)
		}
	}
}
