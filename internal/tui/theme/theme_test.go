package theme

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/colorprofile"
)

func TestBuiltinsResolveAndDeriveSurfaces(t *testing.T) {
	dark := Resolve(Dark(), color.RGBA{R: 0x26, G: 0x2a, B: 0x2e, A: 0xff}, colorprofile.TrueColor)
	if !dark.Dark || dark.Surface.Panel == nil || dark.Surface.Element == nil || dark.Surface.Hover == nil {
		t.Fatalf("dark surfaces not derived: %+v", dark.Surface)
	}
	if hexOf(dark.Surface.Panel) <= hexOf(dark.Surface.Base) { // lighter than the background
		t.Fatalf("dark panel %s should be lighter than bg %s", hexOf(dark.Surface.Panel), hexOf(dark.Surface.Base))
	}
	light := Resolve(Light(), color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, colorprofile.TrueColor)
	if light.Dark || hexOf(light.Surface.Panel) >= "#ffffff" {
		t.Fatalf("light panel should be darker than white: %s", hexOf(light.Surface.Panel))
	}
	// no terminal background: the surfaces step from the theme's own Bg in
	// small increments (a whole-number "percent" would clamp to white/black)
	fb := Resolve(Dark(), nil, colorprofile.TrueColor)
	if got := [3]string{hexOf(fb.Surface.Panel), hexOf(fb.Surface.Element), hexOf(fb.Surface.Hover)}; got != [3]string{"#282828", "#323232", "#3c3c3c"} || hexOf(fb.Surface.Base) != hexOf(fb.Bg) {
		t.Fatalf("dark surfaces from bg %s = %v", hexOf(fb.Bg), got)
	}
	fl := Resolve(Light(), nil, colorprofile.TrueColor)
	if got := [3]string{hexOf(fl.Surface.Panel), hexOf(fl.Surface.Element), hexOf(fl.Surface.Hover)}; got != [3]string{"#ebebeb", "#dcdcdc", "#cdcdcd"} {
		t.Fatalf("light surfaces from bg %s = %v", hexOf(fl.Bg), got)
	}
	// 16 colors: no fills at all, borders carry the layering
	if ansi := Resolve(Dark(), color.RGBA{A: 0xff}, colorprofile.ANSI); ansi.Surface.Panel != nil || ansi.Surface.Element != nil {
		t.Fatalf("ANSI-16 must drop surfaces: %+v", ansi.Surface)
	}
	if neutral := Resolve(Neutral(), nil, colorprofile.TrueColor); !neutral.Neutral || neutral.Surface.Panel != nil || neutral.Text != nil {
		t.Fatalf("neutral theme must have no fills and the default foreground: %+v", neutral.Surface)
	}
}

func TestMarkdownAndChromaComeFromTheSameTokens(t *testing.T) {
	th := Resolve(Dark(), color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}, colorprofile.TrueColor)
	md := th.Markdown()
	if md.CodeBlock.Theme != th.ChromaName() || md.CodeBlock.Chroma != nil {
		t.Fatalf("markdown code blocks must use the registered chroma style: %+v", md.CodeBlock)
	}
	if _, ok := chromastyles.Registry[th.ChromaName()]; !ok {
		t.Fatalf("chroma style %q not registered", th.ChromaName())
	}
	if *md.Document.Color != "#eeeeee" || *md.Code.Color != "#7fd88f" || md.Code.BackgroundColor == nil {
		t.Fatalf("markdown colors not from the palette: doc=%v code=%v chip=%v", *md.Document.Color, *md.Code.Color, md.Code.BackgroundColor)
	}
	neutral := Resolve(Neutral(), nil, colorprofile.TrueColor).Markdown()
	if neutral.Document.Color != nil || neutral.Code.BackgroundColor != nil || neutral.CodeBlock.Theme != "" || *neutral.Heading.Color != "5" {
		t.Fatalf("neutral markdown must use terminal colors only: %+v", neutral)
	}
}

func TestLoadUserThemes(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(themes, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ocean.json", `{"name":"ocean","dark":true,"palette":{"primary":"#00aaff","text":"#e0e0e0"},"chroma":"dracula"}`)
	write("bad-key.json", `{"name":"bad","dark":true,"palette":{"primary":"#00aaff"},"radius":2}`)
	write("bad-color.json", `{"name":"badcolor","dark":false,"palette":{"primary":"blue"}}`)
	write("taken.json", `{"name":"dark","dark":true}`)
	write("pinned.json", `{"dark":false,"surfaces":{"panel":"#eeeeee","element":"#e0e0e0","hover":"#d0d0d0"}}`)

	specs, errs := Load(dir)
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "ocean,pinned" {
		t.Fatalf("loaded themes = %v (errors %v)", names, errs)
	}
	if len(errs) != 3 {
		t.Fatalf("expected 3 rejected themes, got %v", errs)
	}
	joined := ""
	var joinedSb92 strings.Builder
	for _, err := range errs {
		joinedSb92.WriteString(err.Error() + "\n")
	}
	joined += joinedSb92.String()
	for _, want := range []string{`unknown field "radius"`, "allowed keys:", `palette.primary="blue"`, "built-in name"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("error text missing %q:\n%s", want, joined)
		}
	}
	// missing tokens default from the built-in of the same darkness
	ocean := specs[0]
	if ocean.Palette.Muted != Dark().Palette.Muted || ocean.Palette.Primary != "#00aaff" {
		t.Fatalf("defaults not applied: %+v", ocean.Palette)
	}
	th := Resolve(ocean, nil, colorprofile.TrueColor)
	if th.ChromaName() != "dracula" || hexOf(th.Primary) != "#00aaff" {
		t.Fatalf("user theme not honored: chroma=%s primary=%s", th.ChromaName(), hexOf(th.Primary))
	}
	pinned := Resolve(specs[1], color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, colorprofile.TrueColor)
	if hexOf(pinned.Surface.Panel) != "#eeeeee" {
		t.Fatalf("pinned surfaces must win over derivation: %s", hexOf(pinned.Surface.Panel))
	}
}

// Every embedded catalog theme parses, validates, resolves with a background
// and pinned surfaces, and pins its syntax and markdown colours.
func TestCatalogLoads(t *testing.T) {
	specs, errs := Catalog()
	if len(errs) > 0 {
		t.Fatalf("catalog errors: %v", errs)
	}
	if len(specs) < 60 {
		t.Fatalf("expected the opencode catalog (dark and light variants), got %d themes", len(specs))
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if seen[s.Name] {
			t.Fatalf("duplicate theme %q", s.Name)
		}
		seen[s.Name] = true
		if strings.HasSuffix(s.Name, "-light") == s.Dark {
			t.Fatalf("%s: dark=%v does not match its name", s.Name, s.Dark)
		}
		th := Resolve(s, nil, colorprofile.TrueColor)
		if th.Bg == nil || th.Surface.Panel == nil || th.Surface.Element == nil || th.Text == nil || th.Primary == nil {
			t.Fatalf("%s: missing background, surfaces or core tokens: %+v", s.Name, th.Palette)
		}
		if th.Markdown().Document.Color == nil {
			t.Fatalf("%s: markdown has no text colour", s.Name)
		}
	}
	tn, ok := Builtin("tokyonight")
	if !ok {
		t.Fatal("tokyonight missing from the built-ins")
	}
	th := Resolve(tn, nil, colorprofile.TrueColor)
	if hexOf(th.Syntax().Keyword) != "#c099ff" || *th.Markdown().Heading.Color != "#c099ff" || hexOf(th.Bg) != "#1a1b26" {
		t.Fatalf("tokyonight pins: keyword=%s heading=%s bg=%s", hexOf(th.Syntax().Keyword), *th.Markdown().Heading.Color, hexOf(th.Bg))
	}
	if l, ok := Builtin("tokyonight-light"); !ok || l.Dark {
		t.Fatalf("tokyonight-light: %+v", l)
	}
}
