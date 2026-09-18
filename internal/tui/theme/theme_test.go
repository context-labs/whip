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

func TestResolvedSemanticTextStyles(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
	}{
		{name: "dark", spec: Dark()},
		{name: "light", spec: Light()},
		{name: "neutral", spec: Neutral()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved := Resolve(test.spec, nil)
			if got, want := resolved.Body.GetForeground(), resolved.Terminal(resolved.Text); got != want {
				t.Errorf("body foreground = %v, want %v", got, want)
			}
			if got, want := resolved.MutedText.GetForeground(), resolved.Terminal(resolved.Muted); got != want {
				t.Errorf("muted foreground = %v, want %v", got, want)
			}
			if got, want := resolved.Selected.GetForeground(), resolved.Terminal(resolved.OnPrimary); got != want {
				t.Errorf("selected foreground = %v, want %v", got, want)
			}
			if got, want := resolved.Selected.GetBackground(), resolved.Terminal(resolved.Primary); got != want {
				t.Errorf("selected background = %v, want %v", got, want)
			}
		})
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
