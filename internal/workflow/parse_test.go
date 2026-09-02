package workflow

import (
	"strings"
	"testing"
)

const goodScript = `export const meta = {
  name: 'summarize-files',
  description: 'Summarize important project files',
  phases: [
    { title: 'Read', detail: 'Inspect files' },
    { title: 'Synthesize', detail: 'Merge findings' },
  ],
}

phase('Read')
return await agent('Read README.md and summarize.', { label: 'read:readme' })
`

func TestParseGoodScript(t *testing.T) {
	meta, body, err := Parse(goodScript)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "summarize-files" || meta.Description != "Summarize important project files" {
		t.Fatalf("meta: %+v", meta)
	}
	if len(meta.Phases) != 2 || meta.Phases[0].Title != "Read" || meta.Phases[1].Detail != "Merge findings" {
		t.Fatalf("phases: %+v", meta.Phases)
	}
	if strings.Contains(body, "export const meta") {
		t.Fatalf("meta not stripped from body: %q", body)
	}
	if !strings.Contains(body, "phase('Read')") {
		t.Fatalf("body lost its code: %q", body)
	}
}

func TestParseRejectsMissingMeta(t *testing.T) {
	_, _, err := Parse("return await agent('x')")
	if err == nil || !strings.Contains(err.Error(), "first statement") {
		t.Fatalf("expected first-statement error, got %v", err)
	}
}

func TestParseRejectsNonLiteralMeta(t *testing.T) {
	// A function call inside the meta literal — the classic non-pure-literal.
	s := "export const meta = { name: makeName(), description: 'd' }\nreturn 1"
	_, _, err := Parse(s)
	if err == nil || !strings.Contains(err.Error(), "PURE LITERAL") {
		t.Fatalf("expected pure-literal error, got %v", err)
	}
	// A spread — also non-pure.
	s = "export const meta = { ...defaults, name: 'n', description: 'd' }\nreturn 1"
	_, _, err = Parse(s)
	if err == nil || !strings.Contains(err.Error(), "PURE LITERAL") {
		t.Fatalf("expected pure-literal error for spread, got %v", err)
	}
}

func TestParseRejectsTemplateInterpolation(t *testing.T) {
	s := "export const meta = { name: `n${1+1}`, description: 'd' }\nreturn 1"
	_, _, err := Parse(s)
	if err == nil || !strings.Contains(err.Error(), "PURE LITERAL") {
		t.Fatalf("expected pure-literal error, got %v", err)
	}
}

func TestParseRejectsNondeterminism(t *testing.T) {
	for _, bad := range []string{"Date.now()", "Math.random()", "new Date()"} {
		s := "export const meta = { name: 'n', description: 'd' }\nreturn " + bad
		_, _, err := Parse(s)
		if err == nil || !strings.Contains(err.Error(), "deterministic") {
			t.Fatalf("%s: expected determinism error, got %v", bad, err)
		}
	}
}

func TestParseBraceMatchingThroughStrings(t *testing.T) {
	// A brace inside a string must not end the meta literal early.
	s := `export const meta = { name: 'a}b', description: "d{}" }
return 1`
	meta, body, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "a}b" {
		t.Fatalf("name: %q", meta.Name)
	}
	if strings.TrimSpace(body) != "return 1" {
		t.Fatalf("body: %q", body)
	}
}

func TestParseValidatesPhases(t *testing.T) {
	_, _, err := Parse(`export const meta = { name: 'n', description: 'd', phases: [{ detail: 'no title' }] }`)
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected phase title error, got %v", err)
	}
	_, _, err = Parse(`export const meta = { name: 'n', description: 'd', phases: [{ title: 'T', effort: 'ludicrous' }] }`)
	if err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatalf("expected phase effort error, got %v", err)
	}
}

func TestParseMetaModelAndEffort(t *testing.T) {
	meta, _, err := Parse(`export const meta = { name: 'n', description: 'd', model: 'gpt-x', effort: 'high' }`)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "gpt-x" || meta.Effort != "high" {
		t.Fatalf("meta: %+v", meta)
	}
}
