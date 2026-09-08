package theme_test

import (
	"encoding/json"
	"image/color"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/colorprofile"
	"github.com/context-labs/whip/internal/theme"
	terminal "github.com/context-labs/whip/internal/tui/theme"
)

func TestAllShippedThemesMatchTerminal(t *testing.T) {
	catalog, err := theme.BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	specs := terminal.Builtins()
	if len(catalog) != len(specs) {
		t.Fatalf("browser %d themes; terminal %d", len(catalog), len(specs))
	}
	// Guard the shipped inventory without encoding it into application logic.
	if len(catalog) != 66 {
		t.Fatalf("shipped inventory changed (%d); review theme parity fixtures", len(catalog))
	}
	seen := map[string]bool{}
	for i, w := range catalog {
		t.Run(w.ID, func(t *testing.T) {
			if seen[w.ID] {
				t.Fatalf("duplicate theme %s", w.ID)
			}
			seen[w.ID] = true
			spec := specs[i]
			tui := terminal.Resolve(spec, nil, colorprofile.TrueColor)
			if w.ID != tui.Name || w.Dark != tui.Dark {
				t.Fatalf("identity mismatch: %+v / %+v", w, tui)
			}
			pairs := []struct {
				web      string
				terminal color.Color
			}{
				{w.Colors.Foreground, tui.Text}, {w.Colors.Background, tui.Bg}, {w.Colors.Primary, tui.Primary},
				{w.Colors.Accent, tui.Accent}, {w.Colors.Muted, tui.Muted}, {w.Colors.Faint, tui.Faint},
				{w.Colors.OnPrimary, tui.OnPrimary}, {w.Colors.Success, tui.Success}, {w.Colors.Warning, tui.Warning},
				{w.Colors.Error, tui.Error}, {w.Colors.Info, tui.Info}, {w.Colors.Link, tui.Link},
				{w.Colors.Emphasis, tui.Emphasis}, {w.Colors.Border, tui.Border}, {w.Colors.BorderFocus, tui.BorderFocus},
				{w.Colors.DiffAdd, tui.DiffAdd}, {w.Colors.DiffDel, tui.DiffDel},
				{w.Colors.Panel, tui.Surface.Panel}, {w.Colors.Element, tui.Surface.Element}, {w.Colors.Hover, tui.Surface.Hover},
			}
			for _, pair := range pairs {
				if pair.web != theme.Hex(pair.terminal) {
					t.Errorf("browser %s != terminal %s", pair.web, theme.Hex(pair.terminal))
				}
			}
			syntax := tui.Syntax()
			if w.Syntax.Keyword != theme.Hex(syntax.Keyword) || w.Syntax.String != theme.Hex(syntax.String) ||
				w.Syntax.Number != theme.Hex(syntax.Number) || w.Syntax.Comment != theme.Hex(syntax.Comment) ||
				w.Syntax.Function != theme.Hex(syntax.Func) || w.Syntax.Type != theme.Hex(syntax.Type) ||
				w.Syntax.Operator != theme.Hex(syntax.Op) || w.Syntax.Punctuation != theme.Hex(syntax.Punct) {
				t.Errorf("syntax differs: %+v", w.Syntax)
			}
			md := tui.Markdown()
			if w.Markdown.Heading != *md.Heading.Color || w.Markdown.Strong != *md.Strong.Color ||
				w.Markdown.Code != *md.Code.Color || w.Markdown.Quote != *md.BlockQuote.Color {
				t.Errorf("markdown differs: %+v", w.Markdown)
			}
			assertColors(t, w)
		})
	}
}

func TestCustomResolution(t *testing.T) {
	t.Run("display label and optional browser surfaces", func(t *testing.T) {
		spec := theme.Spec{Name: "ink", DisplayName: "Readable Ink", Dark: true,
			Web: &theme.WebSpec{Navigation: "16", QuietBorder: "#ABCDEF",
				CodeBackground: "17", InlineCodeBackground: "#FEDCBA"}}
		got, err := theme.ResolveSpec(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "ink" || got.Name != "Readable Ink" ||
			got.Web.Navigation != "#000000" || got.Web.QuietBorder != "#abcdef" ||
			got.Web.CodeBackground != "#00005f" || got.Web.InlineCodeBackground != "#fedcba" {
			t.Fatalf("identity or normalized surfaces lost: %+v", got)
		}
		if spec.Web.Navigation != "16" || spec.Web.QuietBorder != "#ABCDEF" {
			t.Fatal("resolver mutated source surfaces")
		}
		got, err = theme.ResolveJSON([]byte(`{"name":"partial","web":{"navigation":"16"}}`))
		if err != nil || got.Web.Navigation != "#000000" || got.Web.QuietBorder != "" {
			t.Fatalf("partial override: %+v, %v", got, err)
		}
	})
	t.Run("defaults and ansi", func(t *testing.T) {
		got, err := theme.ResolveJSON([]byte(`{"name":"ocean","dark":true,"palette":{"primary":"21","diffAdd":"255"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if got.Colors.Primary != "#0000ff" || got.Colors.DiffAdd != "#eeeeee" || got.Colors.Foreground != "#eeeeee" {
			t.Fatalf("ANSI normalization / same-darkness defaults: %+v", got.Colors)
		}
		if got.Syntax.Keyword != got.Colors.Primary || got.Markdown.Heading != got.Colors.Accent {
			t.Fatal("derived roles did not use palette")
		}
		assertColors(t, got)
	})
	t.Run("partial pinned surfaces", func(t *testing.T) {
		got, err := theme.ResolveJSON([]byte(`{"name":"paper","dark":false,"surfaces":{"panel":"#EEEEEE"},"markdown":{"heading":"#123456"},"syntax":{"comment":"#654321"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if got.Colors.Panel != "#eeeeee" || got.Colors.Element != got.Colors.Background || got.Colors.Hover != got.Colors.Background {
			t.Fatalf("unspecified pinned surfaces should inherit canvas: %+v", got.Colors)
		}
		if got.Markdown.Heading != "#123456" || got.Syntax.Comment != "#654321" {
			t.Fatal("overrides missing")
		}
		assertColors(t, got)
	})
	t.Run("chroma override precedence and attributes", func(t *testing.T) {
		got, err := theme.ResolveJSON([]byte(`{"name":"custom-dracula","dark":true,"chroma":"dracula","syntax":{"keyword":"#123456","comment":"#abcdef"}}`))
		if err != nil {
			t.Fatal(err)
		}
		style := chromastyles.Registry["dracula"]
		want := style.Get(chroma.Keyword).Colour.String()
		if got.Syntax.Keyword != want || got.Syntax.Keyword == "#123456" {
			t.Fatalf("Chroma did not win: %+v", got.Syntax)
		}
		for _, tt := range style.Types() {
			want, entry := style.Get(tt), got.Code.Tokens[tt.String()]
			if want.Colour.IsSet() && entry.Color != want.Colour.String() {
				t.Errorf("%s color %s != %s", tt, entry.Color, want.Colour)
			}
			if entry.Bold != (want.Bold == chroma.Yes) || entry.Italic != (want.Italic == chroma.Yes) || entry.Underline != (want.Underline == chroma.Yes) {
				t.Errorf("%s attributes mismatch: %+v / %+v", tt, entry, want)
			}
		}
		assertColors(t, got)
	})
}

func TestRejectInvalidCustomThemes(t *testing.T) {
	cases := map[string]string{
		"null":                    `null`,
		"unknown property":        `{"dark":true,"css":"*{display:none}"}`,
		"css injection":           `{"palette":{"primary":"red; color: black"}}`,
		"ansi above bound":        `{"palette":{"primary":"256"}}`,
		"ansi negative":           `{"palette":{"primary":"-1"}}`,
		"ansi syntax above bound": `{"syntax":{"number":"999"}}`,
		"unknown chroma":          `{"chroma":"missing-style"}`,
		"built-in name":           `{"name":"dark"}`,
		"auto reserved":           `{"name":"auto"}`,
		"neutral reserved":        `{"name":"neutral"}`,
		"trailing json":           `{} {}`,
		"trailing garbage":        `{} nonsense`,
		"invalid type":            `{"dark":"true"}`,
		"oversized":               strings.Repeat(" ", theme.MaxJSONBytes) + "{}",
		"oversized name":          `{"name":"` + strings.Repeat("a", 129) + `"}`,
		"name control":            `{"name":"oops\n"}`,
		"display name control":    `{"displayName":"oops\n"}`,
		"oversized display name":  `{"displayName":"` + strings.Repeat("a", 121) + `"}`,
		"invalid web surface":     `{"web":{"navigation":"url(https://example.com)"}}`,
		"unknown web surface":     `{"web":{"custom":"#123456"}}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := theme.ResolveJSON([]byte(data)); err == nil {
				t.Fatalf("accepted %s", data[:min(256, len(data))])
			}
		})
	}
}

func TestResolveDoesNotRegisterStyles(t *testing.T) {
	before := len(chromastyles.Registry)
	if _, err := theme.ResolveJSON([]byte(`{"name":"unregistered-browser-theme","dark":true}`)); err != nil {
		t.Fatal(err)
	}
	if len(chromastyles.Registry) != before {
		t.Fatal("browser resolution mutated the global terminal style registry")
	}
}

func assertColors(t *testing.T, value theme.Resolved) {
	t.Helper()
	colorRE := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	for _, block := range []any{value.Colors, value.Syntax, value.Markdown} {
		v := reflect.ValueOf(block)
		for i := range v.NumField() {
			if !colorRE.MatchString(v.Field(i).String()) {
				t.Errorf("%s not portable: %q", v.Type().Field(i).Name, v.Field(i).String())
			}
		}
	}
	for name, s := range value.Code.Tokens {
		if !colorRE.MatchString(s.Color) || !colorRE.MatchString(s.Background) {
			t.Errorf("%s code colors not portable: %+v", name, s)
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "onPrimary") {
		t.Fatal("host wire must use snake_case")
	}
}
