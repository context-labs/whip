// Package theme adapts the shared theme catalog to terminal rendering.
package theme

import shared "github.com/context-labs/whip/internal/theme"

type (
	Spec         = shared.Spec
	PaletteSpec  = shared.PaletteSpec
	SurfaceSpec  = shared.SurfaceSpec
	SyntaxSpec   = shared.SyntaxSpec
	MarkdownSpec = shared.MarkdownSpec
)

func Dark() Spec                       { return shared.Dark() }
func Light() Spec                      { return shared.Light() }
func Neutral() Spec                    { return shared.Neutral() }
func Catalog() ([]Spec, []error)       { return shared.Embedded() }
func Builtins() []Spec                 { return shared.Builtins() }
func Builtin(name string) (Spec, bool) { return shared.Builtin(name) }
