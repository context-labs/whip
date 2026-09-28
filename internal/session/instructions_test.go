package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func instructionManifestTest() InstructionManifest {
	return InstructionManifest{
		Bytes: 100, SHA256: strings.Repeat("ab", 32),
		Sources: []InstructionSource{{Kind: "project_file", Scope: "workspace", Path: "AGENTS.md", Bytes: 20, SHA256: strings.Repeat("cd", 32)}},
	}
}

func TestInstructionPolicyLocalUniquePaths(t *testing.T) {
	for _, paths := range [][]string{nil, {}, {"AGENTS.md", "nested/CLAUDE.md", "é.md"}} {
		if err := (&ConfigPatch{Instructions: &Instructions{ProjectFiles: paths}}).Validate(); err != nil {
			t.Fatalf("valid paths %q: %v", paths, err)
		}
	}
	for _, paths := range [][]string{
		{""},
		{" "},
		{"."},
		{".."},
		{"/AGENTS.md"},
		{"../AGENTS.md"},
		{"a/../AGENTS.md"},
		{"a/./AGENTS.md"},
		{"a//AGENTS.md"},
		{"a/"},
		{`a\AGENTS.md`},
		{"\x00"},
		{"\xff"},
		{strings.Repeat("a", 4097)},
		{"AGENTS.md", "AGENTS.md"},
		make([]string, 33),
	} {
		if err := (&ConfigPatch{Instructions: &Instructions{ProjectFiles: paths}}).Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid paths %q: %v", paths, err)
		}
	}
	for _, text := range []string{"\xff", "a\x00b", strings.Repeat("a", MaxInstructionBytes+1)} {
		if err := (Instructions{Text: text}).Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid text accepted: %v", err)
		}
	}
}

func TestInstructionManifestValidationAndClone(t *testing.T) {
	valid := instructionManifestTest()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := valid.Clone()
	clone.Sources[0].Path = "changed.md"
	if valid.Sources[0].Path != "AGENTS.md" {
		t.Fatal("clone aliases sources")
	}
	raw, err := json.Marshal(valid)
	if err != nil || !strings.Contains(string(raw), `"bytes":"100"`) || !strings.Contains(string(raw), `"bytes":"20"`) {
		t.Fatalf("byte counters are not exact decimal strings: %s %v", raw, err)
	}
	for name, mutate := range map[string]func(*InstructionManifest){
		"negative composed bytes": func(m *InstructionManifest) { m.Bytes = -1 },
		"oversized composed":      func(m *InstructionManifest) { m.Bytes = MaxInstructionBytes + 1 },
		"uppercase digest":        func(m *InstructionManifest) { m.SHA256 = strings.ToUpper(m.SHA256) },
		"short digest":            func(m *InstructionManifest) { m.SHA256 = "ab" },
		"nonhex digest":           func(m *InstructionManifest) { m.SHA256 = strings.Repeat("z", 64) },
		"source kind":             func(m *InstructionManifest) { m.Sources[0].Kind = "global" },
		"source scope":            func(m *InstructionManifest) { m.Sources[0].Scope = "host" },
		"source path":             func(m *InstructionManifest) { m.Sources[0].Path = "../AGENTS.md" },
		"source bytes":            func(m *InstructionManifest) { m.Sources[0].Bytes = -1 },
		"source oversized":        func(m *InstructionManifest) { m.Sources[0].Bytes = MaxInstructionSourceBytes + 1 },
		"source digest":           func(m *InstructionManifest) { m.Sources[0].SHA256 = "" },
		"source count":            func(m *InstructionManifest) { m.Sources = make([]InstructionSource, MaxInstructionSources+1) },
		"encoded bytes": func(m *InstructionManifest) {
			source := m.Sources[0]
			source.Path = strings.Repeat("a", 4096)
			m.Sources = nil
			for range 256 {
				m.Sources = append(m.Sources, source)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := valid.Clone()
			mutate(&value)
			if err := value.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid manifest accepted: %v", err)
			}
		})
	}
	valid.Bytes = MaxInstructionBytes
	valid.Sources[0].Bytes = MaxInstructionSourceBytes
	valid.Sources[0].Kind = "skill_metadata"
	if err := valid.Validate(); err != nil {
		t.Fatal("exact bounds rejected", err)
	}
	valid.Sources = nil
	valid.Bytes = 0
	if err := valid.Validate(); err != nil {
		t.Fatal("source-free empty composition rejected", err)
	}
}

func TestInstructionInvokedSkillSourceBounds(t *testing.T) {
	for _, kind := range []string{"project_file", "skill_metadata", "invoked_skill"} {
		t.Run(kind, func(t *testing.T) {
			value := instructionManifestTest()
			value.Sources[0].Kind = kind
			value.Sources[0].Path = ".agents/skills/review/SKILL.md"
			limit := int64(MaxInstructionSourceBytes)
			if kind == "invoked_skill" {
				limit = MaxInvokedSkillBytes
			}
			value.Sources[0].Bytes = limit
			if err := value.Validate(); err != nil {
				t.Fatalf("complete source at limit rejected: %v", err)
			}
			value.Sources[0].Bytes++
			if err := value.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("oversized source accepted: %v", err)
			}
			value.Sources[0].Bytes = -1
			if err := value.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("negative source bytes accepted: %v", err)
			}
		})
	}
}

func TestInstructionSkillRootsPolicyResolution(t *testing.T) {
	roots := []string{"personal", "team"}
	base := Configuration{Model: ModelSelection{Provider: "test", Name: "test"}, Instructions: Instructions{SkillRoots: roots}}
	resolved, err := Resolve(base, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	child, err := Resolve(resolved, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	clone := resolved.Clone()
	roots[0] = "original_mutated"
	resolved.Instructions.SkillRoots[0] = "parent_mutated"
	clone.Instructions.SkillRoots[1] = "clone_mutated"
	if !reflect.DeepEqual(child.Instructions.SkillRoots, []string{"personal", "team"}) {
		t.Fatal("copied roots alias parent or source")
	}
	for _, test := range []struct {
		name                 string
		definition, override *Instructions
		want                 []string
	}{
		{name: "definition replaces", definition: &Instructions{SkillRoots: []string{"definition"}}, want: []string{"definition"}},
		{name: "override replaces", definition: &Instructions{SkillRoots: []string{"definition"}}, override: &Instructions{SkillRoots: []string{"override"}}, want: []string{"override"}},
		{name: "clear", override: &Instructions{}, want: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(base, DefinitionDocument{Defaults: ConfigPatch{Instructions: test.definition}}, ConfigPatch{Instructions: test.override})
			if err != nil || !reflect.DeepEqual(got.Instructions.SkillRoots, test.want) {
				t.Fatalf("policy=%+v err=%v", got.Instructions, err)
			}
			if len(test.want) > 0 {
				got.Instructions.SkillRoots[0] = "changed"
				if test.definition != nil && test.definition.SkillRoots[0] == "changed" || test.override != nil && test.override.SkillRoots[0] == "changed" {
					t.Fatal("resolved selection aliases patch")
				}
			}
		})
	}
	maxRoots := make([]string, MaxSkillRoots)
	for i := range maxRoots {
		maxRoots[i] = fmt.Sprintf("root_%d", i)
	}
	if err := (Instructions{SkillRoots: maxRoots}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{""}, {"../root"}, {"bad root"}, {"same", "same"}, slices.Concat(maxRoots, []string{"extra"})} {
		if err := (ConfigPatch{Instructions: &Instructions{SkillRoots: bad}}).Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid selection accepted: %q %v", bad, err)
		}
	}
}

func TestInstructionSourceHostIdentityAndClone(t *testing.T) {
	for _, kind := range []string{"skill_metadata", "invoked_skill"} {
		source := InstructionSource{Kind: kind, Scope: "host", RootID: new("team"), Path: "review/SKILL.md", Bytes: 10, SHA256: strings.Repeat("ab", 32)}
		if err := source.Validate(); err != nil {
			t.Fatal(err)
		}
		manifest := instructionManifestTest()
		manifest.Sources = []InstructionSource{source}
		clone := manifest.Clone()
		*clone.Sources[0].RootID = "other"
		if *manifest.Sources[0].RootID != "team" {
			t.Fatal("manifest clone aliases root identity")
		}
		for name, mutate := range map[string]func(*InstructionSource){
			"missing host id": func(s *InstructionSource) { s.RootID = nil },
			"invalid id":      func(s *InstructionSource) { s.RootID = new("../team") },
			"empty id":        func(s *InstructionSource) { s.RootID = new("") },
			"workspace id":    func(s *InstructionSource) { s.Scope = "workspace" },
			"host project":    func(s *InstructionSource) { s.Kind = "project_file" },
			"unknown scope":   func(s *InstructionSource) { s.Scope = "ambient" },
		} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				bad := source
				mutate(&bad)
				if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid source accepted: %+v %v", bad, err)
				}
			})
		}
	}
	raw, err := json.Marshal(instructionManifestTest())
	if err != nil || !strings.Contains(string(raw), `"root_id":null`) {
		t.Fatalf("workspace identity must remain explicit null: %s %v", raw, err)
	}
}

func TestStandingInstructionSourceScopeAndBounds(t *testing.T) {
	valid := InstructionSource{Kind: "standing_instructions", Scope: "host", RootID: new("standing"), Path: "me.md", Bytes: MaxInstructionSourceBytes, SHA256: strings.Repeat("ab", 32)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*InstructionSource){
		"workspace":     func(s *InstructionSource) { s.Scope = "workspace"; s.RootID = nil },
		"missing root":  func(s *InstructionSource) { s.RootID = nil },
		"skill root":    func(s *InstructionSource) { s.RootID = new("team") },
		"empty root":    func(s *InstructionSource) { s.RootID = new("") },
		"nested path":   func(s *InstructionSource) { s.Path = "nested/me.md" },
		"absolute path": func(s *InstructionSource) { s.Path = "/me.md" },
		"dot":           func(s *InstructionSource) { s.Path = "." },
		"parent":        func(s *InstructionSource) { s.Path = ".." },
		"backslash":     func(s *InstructionSource) { s.Path = `nested\me.md` },
		"invalid UTF8":  func(s *InstructionSource) { s.Path = "bad\xff" },
		"nul":           func(s *InstructionSource) { s.Path = "bad\x00" },
		"oversized":     func(s *InstructionSource) { s.Bytes++ },
		"negative":      func(s *InstructionSource) { s.Bytes = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			mutate(&bad)
			if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid standing source accepted=%+v %v", bad, err)
			}
		})
	}
	valid.Path = "個人.md"
	valid.Bytes = 0
	if err := valid.Validate(); err != nil {
		t.Fatal("empty Unicode-named source rejected", err)
	}
}

func TestStandingInstructionPolicyCaptureAndClear(t *testing.T) {
	base := Configuration{Model: ModelSelection{Provider: "test", Name: "test"}, Instructions: Instructions{StandingInstructions: true}}
	captured, err := Resolve(base, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	child, err := Resolve(captured, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	clone := captured.Clone()
	clone.Instructions.StandingInstructions = false
	captured.Instructions.StandingInstructions = false
	if !child.Instructions.StandingInstructions || !base.Instructions.StandingInstructions {
		t.Fatal("standing selection aliases copied configuration")
	}
	for _, document := range []DefinitionDocument{{}, {Defaults: ConfigPatch{Instructions: &Instructions{StandingInstructions: true}}}} {
		cleared, err := Resolve(base, document, ConfigPatch{Instructions: &Instructions{}})
		if err != nil || cleared.Instructions.StandingInstructions {
			t.Fatalf("whole-field override failed to clear standing selection: %+v %v", cleared.Instructions, err)
		}
	}
	raw, err := json.Marshal(child.Instructions)
	if err != nil || !strings.Contains(string(raw), `"standing_instructions":true`) {
		t.Fatalf("standing selection missing from captured JSON: %s %v", raw, err)
	}
}
