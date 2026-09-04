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
	// no background RGB: the built-in fallback fills
	if fb := Resolve(Dark(), nil, colorprofile.TrueColor); hexOf(fb.Surface.Panel) != "#343434" {
		t.Fatalf("fallback panel = %s", hexOf(fb.Surface.Panel))
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
	for _, err := range errs {
		joined += err.Error() + "\n"
	}
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
