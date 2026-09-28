package skills

import (
	"strings"
	"testing"
)

func TestPromptMetadataPreservesKeysAfterBlocksWithoutReadingBody(t *testing.T) {
	for _, header := range []string{">-", "|+", ">2-", "|-2"} {
		data := []byte("---\ndescription: " + header + "\n  first line\n  second line\nname: preserved\ndisable-model-invocation: true\n---\n")
		skill, err := ParsePromptMetadata(".agents/skills/fallback/SKILL.md", data)
		if err != nil || skill.Name != "preserved" || !skill.DisableModelInvocation || !strings.Contains(skill.Description, "second line") {
			t.Fatalf("header=%s metadata=%+v err=%v", header, skill, err)
		}
		if PromptBlock([]Skill{skill}) != "" {
			t.Fatal("disabled skill became visible after block scalar")
		}
	}
	data := []byte("---\nname: okay\ndescription: >-\n  instruction metadata\n---\nname: body-must-not-override\n")
	skill, err := parseMetadata("relative/SKILL.md", strings.NewReader(string(data)))
	if err != nil || skill.Name != "okay" {
		t.Fatalf("frontmatter close after block scalar was lost: %+v %v", skill, err)
	}
	if _, err := ParsePromptMetadata("relative/SKILL.md", data); err == nil {
		t.Fatal("bounded pure parser accepted body bytes")
	}
}

func TestPromptMetadataRejectsMalformedKnownFields(t *testing.T) {
	for _, body := range []string{
		"description: [one, two]", "name: {nested: yes}", "description:",
		"disable-model-invocation: perhaps", "disable-model-invocation: 'true'",
		"description: \"unclosed", "description: 'one' 'two'", "description: \"bad\\q\"",
		"name: first\nname: second", "description: |++\n  bad", "description: >0\n  bad",
		"description: \"bad\\u0000\"", "description: \xff", "name: BAD", "not a mapping",
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := ParsePromptMetadata(".agents/skills/fallback/SKILL.md", []byte("---\n"+body+"\n---\n")); err == nil {
				t.Fatal("malformed known metadata accepted")
			}
		})
	}
	for _, data := range []string{"", "name: absent", "---\nname: unclosed\n", "---\n#" + strings.Repeat("x", 64<<10) + "\n---\n"} {
		if _, err := ParsePromptMetadata(".agents/skills/fallback/SKILL.md", []byte(data)); err == nil {
			t.Fatal("malformed or oversized frontmatter accepted")
		}
	}
}

func TestPromptMetadataQuotedScalarsAndNestedUnrelatedFields(t *testing.T) {
	data := []byte("---\nname: 'valid'\ndescription: \"line one\\nline two\"\nmetadata:\n  name: never-shadow\n  tags: [one, two]\n---\n")
	skill, err := ParsePromptMetadata(".agents/skills/fallback/SKILL.md", data)
	if err != nil || skill.Name != "valid" || skill.Description != "line one\nline two" {
		t.Fatalf("quoted metadata=%+v err=%v", skill, err)
	}
	skill, err = ParsePromptMetadata(".agents/skills/fallback/SKILL.md", []byte("---\ndescription: 'it''s quoted'\n---\n"))
	if err != nil || skill.Name != "fallback" || skill.Description != "it's quoted" {
		t.Fatalf("fallback metadata=%+v err=%v", skill, err)
	}
	skill, err = ParsePromptMetadata(".agents/skills/fallback/SKILL.md", []byte("---\ndescription: |-\n  before\n  ---\n  after\ndisable-model-invocation: true\n---\n"))
	if err != nil || skill.Description != "before\n---\nafter" || !skill.DisableModelInvocation {
		t.Fatalf("indented delimiter became frontmatter close: %+v err=%v", skill, err)
	}
}
