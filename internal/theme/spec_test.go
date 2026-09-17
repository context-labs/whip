package theme

import "testing"

func TestBuiltinsValidateAndHaveUniqueNames(t *testing.T) {
	seen := map[string]bool{}
	builtins := Builtins()
	if len(builtins) < 60 {
		t.Fatalf("expected the embedded catalog, got %d themes", len(builtins))
	}
	for _, spec := range builtins {
		if err := spec.Validate(); err != nil {
			t.Errorf("%s: %v", spec.Name, err)
		}
		if seen[spec.Name] {
			t.Errorf("duplicate built-in theme %q", spec.Name)
		}
		seen[spec.Name] = true
		if got, ok := Builtin(spec.Name); !ok || got.Name != spec.Name {
			t.Errorf("Builtin(%q) = %q, %v", spec.Name, got.Name, ok)
		}
	}
}

func TestSurfacesDeriveInThemeDirection(t *testing.T) {
	for _, spec := range []Spec{Dark(), Light()} {
		base := ParseColor(spec.Palette.Bg)
		s := Surfaces(spec, base)
		if s.Panel == nil || s.Element == nil || s.Hover == nil {
			t.Fatalf("%s surfaces are incomplete: %+v", spec.Name, s)
		}
		if spec.Dark && !IsDark(s.Panel) {
			t.Fatalf("dark panel became light: %s", Hex(s.Panel))
		}
		if !spec.Dark && IsDark(s.Panel) {
			t.Fatalf("light panel became dark: %s", Hex(s.Panel))
		}
	}
}
