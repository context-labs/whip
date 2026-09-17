package theme

import (
	"testing"

	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

func TestBuiltinsResolveMarkdownAndSyntax(t *testing.T) {
	for _, spec := range Builtins() {
		resolved := Resolve(spec, nil)
		if resolved.Name != spec.Name || resolved.Text == nil || resolved.Primary == nil {
			t.Errorf("%s did not resolve semantic colors", spec.Name)
			continue
		}
		md := resolved.Markdown()
		if md.Document.Color == nil {
			t.Errorf("%s markdown has no document color", spec.Name)
		}
		if !resolved.Neutral {
			name := resolved.ChromaName()
			if resolved.ChromaStyle() == nil || chromastyles.Registry[name] == nil {
				t.Errorf("%s did not register Chroma style %q", spec.Name, name)
			}
		}
	}
}

func TestGeneratedChromaNamesSeparateSurfaceColors(t *testing.T) {
	spec := Dark()
	a := Resolve(spec, nil)
	spec.Surfaces = &SurfaceSpec{Element: "#010203", Panel: "#020304", Hover: "#030405"}
	b := Resolve(spec, nil)
	if a.ChromaName() == b.ChromaName() {
		t.Fatalf("generated Chroma name must include the code surface: %q", a.ChromaName())
	}
}
