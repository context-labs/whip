package theme

import (
	"image/color"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

func TestParseColorAndHex(t *testing.T) {
	tests := []struct {
		value string
		hex   string
	}{
		{"", ""},
		{"#123456", "#123456"},
		{"1", "#800000"},
		{"16", "#000000"},
		{"255", "#eeeeee"},
	}
	for _, test := range tests {
		if got := Hex(ParseColor(test.value)); got != test.hex {
			t.Errorf("Hex(ParseColor(%q)) = %q, want %q", test.value, got, test.hex)
		}
	}
}

func TestSurfacesVariants(t *testing.T) {
	if got := Surfaces(Neutral(), color.Black); got != (SurfaceColors{}) {
		t.Fatalf("neutral surfaces = %+v", got)
	}

	pinned := Dark()
	pinned.Surfaces = &SurfaceSpec{Panel: "#111111", Element: "#222222", Hover: "#333333"}
	got := Surfaces(pinned, color.Black)
	if Hex(got.Base) != "#000000" || Hex(got.Panel) != "#111111" || Hex(got.Element) != "#222222" || Hex(got.Hover) != "#333333" {
		t.Fatalf("pinned surfaces = %+v", got)
	}

	for _, test := range []struct {
		spec Spec
		want string
	}{
		{Dark(), "#343434"},
		{Light(), "#ebebeb"},
	} {
		if got := Hex(Surfaces(test.spec, nil).Panel); got != test.want {
			t.Errorf("%s fallback panel = %s, want %s", test.spec.Name, got, test.want)
		}
	}
}

func TestResolvedSyntaxAndMarkdownColors(t *testing.T) {
	spec := Dark()
	if got := spec.SyntaxColors(); got.Keyword != spec.Palette.Primary || got.Function != spec.Palette.Accent {
		t.Fatalf("derived syntax = %+v", got)
	}
	if got := spec.MarkdownColors(); got.Heading != spec.Palette.Accent || got.Code != spec.Palette.Success {
		t.Fatalf("derived markdown = %+v", got)
	}

	spec.Syntax = &SyntaxSpec{Keyword: "#010203", String: "#040506"}
	spec.Markdown = &MarkdownSpec{Heading: "#070809"}
	if got := spec.SyntaxColors(); got.Keyword != "#010203" || got.String != "#040506" || got.Function != spec.Palette.Accent {
		t.Fatalf("overridden syntax = %+v", got)
	}
	if got := spec.MarkdownColors(); got.Heading != "#070809" || got.Code != spec.Palette.Success {
		t.Fatalf("overridden markdown = %+v", got)
	}
}

func TestCodeEntries(t *testing.T) {
	spec := Dark()
	spec.Syntax = &SyntaxSpec{Keyword: "1"}
	entries := CodeEntries(spec, ParseColor("#101112"))
	for token, want := range map[chroma.TokenType]string{
		chroma.Text:          "#eeeeee",
		chroma.Keyword:       "#800000",
		chroma.NameClass:     "#5c9cf5 bold",
		chroma.Comment:       "#5a5a5a italic",
		chroma.Error:         "#e06c75",
		chroma.Background:    "bg:#101112",
		chroma.NameFunction:  "#9d7cd8",
		chroma.LiteralString: "#7fd88f",
	} {
		if got := entries[token]; got != want {
			t.Errorf("entry %v = %q, want %q", token, got, want)
		}
	}
	if _, ok := CodeEntries(spec, nil)[chroma.Background]; ok {
		t.Fatal("nil background should not create a background entry")
	}
}
