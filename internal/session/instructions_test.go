package session

import (
	"encoding/json"
	"errors"
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
