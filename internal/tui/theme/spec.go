// Package theme is the single source of styling truth for the TUI: semantic
// color tokens, the surface ladder derived from the real terminal background,
// spacing, the typography conventions components compose from, and the code
// and markdown styles generated from the same tokens.
//
// A theme is data. Built-in themes are Specs in this package; user themes are
// JSON files with the same shape in the config directory's themes/ folder.
package theme

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

// Spec is a theme as written down: a name, whether it targets a dark
// background, the semantic palette, optional fixed surface fills, and an
// optional chroma style name for code. Colors are "#rrggbb" or an ANSI
// palette index "0".."255".
type Spec struct {
	Name     string        `json:"name"`
	Dark     bool          `json:"dark"`
	Palette  PaletteSpec   `json:"palette"`
	Surfaces *SurfaceSpec  `json:"surfaces,omitempty"` // omitted: derived from the background
	Syntax   *SyntaxSpec   `json:"syntax,omitempty"`   // omitted: derived from the palette
	Markdown *MarkdownSpec `json:"markdown,omitempty"` // omitted: derived from the palette
	Chroma   string        `json:"chroma,omitempty"`   // omitted: generated from the palette / syntax
	neutral  bool          // the unknown-background theme: ANSI palette colors, no fills
}

// SyntaxSpec pins the code-highlighting roles instead of deriving them.
type SyntaxSpec struct {
	Keyword     string `json:"keyword"`
	String      string `json:"string"`
	Number      string `json:"number"`
	Comment     string `json:"comment"`
	Function    string `json:"function"`
	Type        string `json:"type"`
	Operator    string `json:"operator"`
	Punctuation string `json:"punctuation"`
}

// MarkdownSpec pins the prose accents instead of deriving them.
type MarkdownSpec struct {
	Heading string `json:"heading"` // else Accent
	Strong  string `json:"strong"`  // else Warning
	Code    string `json:"code"`    // else Success
	Quote   string `json:"quote"`   // else Muted
}

// PaletteSpec names every semantic color a component may ask for.
type PaletteSpec struct {
	Text        string `json:"text"`
	Muted       string `json:"muted"`
	Faint       string `json:"faint"`
	Primary     string `json:"primary"` // selection fill, list bullets, hrefs
	Accent      string `json:"accent"`  // headings, category labels
	Success     string `json:"success"` // also inline code
	Warning     string `json:"warning"` // also strong text
	Error       string `json:"error"`
	Info        string `json:"info"`      // agent color: bars, spinner, focused borders
	Link        string `json:"link"`      // link label text
	Emphasis    string `json:"emphasis"`  // italic markdown
	OnPrimary   string `json:"onPrimary"` // text on a Primary fill
	Border      string `json:"border"`
	BorderFocus string `json:"borderFocus"`
	Bg          string `json:"bg"`      // the background the theme was designed on
	DiffAdd     string `json:"diffAdd"` // background tint behind added diff lines
	DiffDel     string `json:"diffDel"` // background tint behind removed diff lines
}

// SurfaceSpec pins the raised-layer fills instead of deriving them from the
// terminal background.
type SurfaceSpec struct {
	Panel   string `json:"panel"`   // cards, sidebar
	Element string `json:"element"` // prompt box, code blocks
	Hover   string `json:"hover"`   // hovered card, selected row
}

// Dark is the built-in dark theme (whip's opencode-derived palette).
func Dark() Spec {
	return Spec{
		Name: "dark", Dark: true,
		Palette: PaletteSpec{
			Text: "#eeeeee", Muted: "#808080", Faint: "#5a5a5a",
			Primary: "#fab283", Accent: "#9d7cd8",
			Success: "#7fd88f", Warning: "#f5a742", Error: "#e06c75", Info: "#5c9cf5",
			Link: "#56b6c2", Emphasis: "#e5c07b",
			OnPrimary: "#0a0a0a", Border: "#3a3a3a", BorderFocus: "#5c9cf5", Bg: "#1e1e1e",
			DiffAdd: "22", DiffDel: "52",
		},
	}
}

// Light is the built-in light theme.
func Light() Spec {
	return Spec{
		Name: "light", Dark: false,
		Palette: PaletteSpec{
			Text: "#1a1a1a", Muted: "#8a8a8a", Faint: "#b0b0b0",
			Primary: "#3b7dd8", Accent: "#d68c27",
			Success: "#3d9a57", Warning: "#d68c27", Error: "#c4314b", Info: "#7b5bb6",
			Link: "#318795", Emphasis: "#b0851f",
			OnPrimary: "#ffffff", Border: "#d0d0d0", BorderFocus: "#3b7dd8", Bg: "#fafafa",
			DiffAdd: "194", DiffDel: "224",
		},
	}
}

// Neutral is the theme for an unknown terminal background (mosh, a tmux
// without passthrough): only the terminal's own ANSI palette, so nothing
// assumes light or dark, and no fills at all.
func Neutral() Spec {
	return Spec{
		Name: "neutral", Dark: true, neutral: true,
		Palette: PaletteSpec{
			Text: "", Muted: "8", Faint: "8",
			Primary: "7", Accent: "5",
			Success: "2", Warning: "3", Error: "1", Info: "4",
			Link: "6", Emphasis: "3",
			OnPrimary: "0", Border: "8", BorderFocus: "4", Bg: "",
			DiffAdd: "22", DiffDel: "52",
		},
	}
}

// catalog holds the themes embedded from themes/*.json: opencode's theme
// collection converted by themes/convert_opencode.py, one spec per dark and
// light variant.
//
//go:embed themes/*.json
var catalogFS embed.FS

var (
	catalogOnce sync.Once
	catalog     []Spec
	catalogErrs []error
)

// Catalog returns the embedded themes, sorted by name. Errors are only
// possible from a broken embedded file; TestCatalogLoads guards them.
func Catalog() ([]Spec, []error) {
	catalogOnce.Do(func() {
		entries, _ := fs.ReadDir(catalogFS, "themes")
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := catalogFS.ReadFile("themes/" + e.Name())
			if err != nil {
				catalogErrs = append(catalogErrs, err)
				continue
			}
			spec, err := parseSpec(data, e.Name())
			if err != nil {
				catalogErrs = append(catalogErrs, err)
				continue
			}
			catalog = append(catalog, spec)
		}
		sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
	})
	return catalog, catalogErrs
}

// Builtins lists the themes that ship with whip, in menu order: whip's own
// light and dark, then the embedded catalog.
func Builtins() []Spec {
	specs := []Spec{Light(), Dark()}
	cat, _ := Catalog()
	return append(specs, cat...)
}

// Builtin returns a shipped theme by name.
func Builtin(name string) (Spec, bool) {
	for _, s := range Builtins() {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Neutral reports whether this is the unknown-background theme.
func (s Spec) Neutral() bool { return s.neutral }

var colorRE = regexp.MustCompile(`^(#[0-9a-fA-F]{6}|[0-9]{1,3})$`)

// Load reads every user theme under dir/themes/*.json. A theme may omit
// palette tokens; they default from the built-in of the same darkness. A
// file with an unknown key, a bad color, or an unregistered chroma style is
// reported with a fix hint and skipped, so one broken theme never hides the
// others. Themes are returned sorted by name.
func Load(dir string) ([]Spec, []error) {
	matches, _ := filepath.Glob(filepath.Join(dir, "themes", "*.json"))
	sort.Strings(matches)
	var specs []Spec
	var errs []error
	for _, path := range matches {
		spec, err := loadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		specs = append(specs, spec)
	}
	return specs, errs
}

func loadFile(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	spec, err := parseSpec(data, filepath.Base(path))
	if err != nil {
		return Spec{}, err
	}
	if _, builtin := Builtin(spec.Name); builtin {
		return Spec{}, fmt.Errorf("theme %s: %q is a built-in name; pick another", filepath.Base(path), spec.Name)
	}
	return spec, nil
}

// parseSpec decodes and validates one theme file (strict keys; the file name
// is the fallback theme name).
func parseSpec(data []byte, file string) (Spec, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return Spec{}, fmt.Errorf("theme %s: %w (allowed keys: %s)", file, err, strings.Join(allowedKeys(), ", "))
	}
	if spec.Name == "" {
		spec.Name = strings.TrimSuffix(file, ".json")
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, fmt.Errorf("theme %s: %w", file, err)
	}
	return spec, nil
}

// Validate fills missing palette tokens from the built-in of the same
// darkness and rejects malformed colors or an unknown chroma style.
func (s *Spec) Validate() error {
	base := Dark()
	if !s.Dark {
		base = Light()
	}
	pv, bv := reflect.ValueOf(&s.Palette).Elem(), reflect.ValueOf(base.Palette)
	var problems []string
	for i := 0; i < pv.NumField(); i++ {
		field := pv.Field(i)
		if field.String() == "" {
			if !s.neutral { // neutral: "" means the terminal's own default
				field.SetString(bv.Field(i).String())
			}
			continue
		}
		if !colorRE.MatchString(field.String()) {
			problems = append(problems, fmt.Sprintf("palette.%s=%q is not #rrggbb or an ANSI index 0-255", jsonName(pv.Type().Field(i)), field.String()))
		}
	}
	for _, block := range []struct {
		name string
		v    any
	}{{"surfaces", s.Surfaces}, {"syntax", s.Syntax}, {"markdown", s.Markdown}} {
		rv := reflect.ValueOf(block.v)
		if !rv.IsValid() || rv.IsNil() {
			continue
		}
		sv := rv.Elem()
		for i := 0; i < sv.NumField(); i++ {
			if v := sv.Field(i).String(); v != "" && !colorRE.MatchString(v) {
				problems = append(problems, fmt.Sprintf("%s.%s=%q is not #rrggbb or an ANSI index 0-255", block.name, jsonName(sv.Type().Field(i)), v))
			}
		}
	}
	if s.Chroma != "" {
		if _, ok := chromastyles.Registry[s.Chroma]; !ok {
			problems = append(problems, fmt.Sprintf("chroma=%q is not a registered chroma style (see https://xyproto.github.io/splash/docs/)", s.Chroma))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func jsonName(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("json"), ",")[0]
}

func allowedKeys() []string {
	keys := []string{"name", "dark", "surfaces{panel,element,hover}", "syntax{keyword,string,number,comment,function,type,operator,punctuation}", "markdown{heading,strong,code,quote}", "chroma"}
	t := reflect.TypeOf(PaletteSpec{})
	for i := 0; i < t.NumField(); i++ {
		keys = append(keys, "palette."+jsonName(t.Field(i)))
	}
	return keys
}
