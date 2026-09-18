package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCustomThemes(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTheme := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(themes, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeTheme("b.json", `{"name":"custom-b","dark":true,"palette":{"text":"#abcdef"}}`)
	writeTheme("a.json", `{"name":"custom-a","dark":false,"palette":{}}`)
	writeTheme("bad.json", `{"name":"bad","unknown":true}`)

	specs, errs := Load(dir)
	if len(specs) != 2 || specs[0].Name != "custom-a" || specs[1].Name != "custom-b" {
		t.Fatalf("Load specs = %+v", specs)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "unknown field") {
		t.Fatalf("Load errors = %v", errs)
	}
	if specs[0].Palette.Text != Light().Palette.Text {
		t.Fatalf("missing light palette values were not filled: %+v", specs[0].Palette)
	}
	if specs[1].Palette.Text != "#abcdef" || specs[1].Palette.Muted != Dark().Palette.Muted {
		t.Fatalf("dark palette overrides/defaults = %+v", specs[1].Palette)
	}
}

func TestLoadReportsUnreadableTheme(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(themes, "broken.json")); err != nil {
		t.Fatal(err)
	}
	if specs, errs := Load(dir); len(specs) != 0 || len(errs) != 1 {
		t.Fatalf("Load unreadable file = specs %+v, errors %v", specs, errs)
	}
}

func TestLoadRejectsBuiltinName(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "dark.json"), []byte(`{"name":"dark","dark":true,"palette":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if specs, errs := Load(dir); len(specs) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "built-in name") {
		t.Fatalf("Load builtin collision = specs %+v, errors %v", specs, errs)
	}
}

func TestParseSpecErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"not-object", `[]`, "expected a JSON object"},
		{"invalid-json", `{`, "unexpected EOF"},
		{"extra-object", `{} {}`, "exactly one JSON object"},
		{"bad-color", `{"palette":{"text":"red"}}`, "not #rrggbb"},
		{"bad-chroma", `{"chroma":"missing-style","palette":{}}`, "not a registered chroma style"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseSpec([]byte(test.data), "fallback.json")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseSpec error = %v, want substring %q", err, test.want)
			}
		})
	}

	spec, err := parseSpec([]byte(`{"palette":{}}`), "fallback.json")
	if err != nil || spec.Name != "fallback" {
		t.Fatalf("fallback name: spec=%+v err=%v", spec, err)
	}
}

func TestValidateReportsInvalidFields(t *testing.T) {
	control := "bad\nname"
	spec := Spec{
		Name: control, DisplayName: control, Dark: true, Chroma: "missing-style",
		Palette:  PaletteSpec{Text: "256"},
		Surfaces: &SurfaceSpec{Panel: "bad"},
		Syntax:   &SyntaxSpec{Keyword: "-1"},
		Markdown: &MarkdownSpec{Heading: "#xyzxyz"},
		Web:      &WebSpec{Navigation: "999"},
	}
	err := spec.Validate()
	if err == nil {
		t.Fatal("Validate unexpectedly succeeded")
	}
	for _, want := range []string{"name must", "displayName must", "palette.text", "surfaces.panel", "syntax.keyword", "markdown.heading", "web.navigation", "chroma="} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate error %q missing %q", err, want)
		}
	}
}

func TestHelpers(t *testing.T) {
	if got := (Spec{Name: "plain"}).Label(); got != "plain" {
		t.Fatalf("fallback label = %q", got)
	}
	if _, ok := Builtin("missing"); ok {
		t.Fatal("missing builtin found")
	}
	for _, test := range []struct {
		value string
		want  bool
	}{{"#abcdef", true}, {"0", true}, {"255", true}, {"256", false}, {"-1", false}, {"red", false}} {
		if got := validColor(test.value); got != test.want {
			t.Errorf("validColor(%q) = %v, want %v", test.value, got, test.want)
		}
	}
	keys := strings.Join(allowedKeys(), ",")
	if !strings.Contains(keys, "palette.text") || !strings.Contains(keys, "displayName") {
		t.Fatalf("allowed keys = %s", keys)
	}
}
