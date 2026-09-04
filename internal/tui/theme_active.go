package tui

import (
	"image/color"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/tui/theme"
)

// The active theme is process-global like the scheme it follows (mdLight /
// mdKnown / mdScheme): everything that paints reads currentTheme(), and
// rebuildTheme() re-resolves it whenever the scheme, the detected background
// RGB, the color profile, or the pinned theme name changes. themeGen bumps on
// every rebuild so caches keyed on the look (glamour renderer, block renders)
// know to drop.
var (
	themeMu      sync.Mutex
	activeTheme  = theme.Resolve(theme.Neutral(), nil, colorprofile.TrueColor)
	themeGen     int
	themeProfile = colorprofile.TrueColor
	userThemes   []theme.Spec
)

func currentTheme() *theme.Theme {
	themeMu.Lock()
	defer themeMu.Unlock()
	return activeTheme
}

func themeGeneration() int {
	themeMu.Lock()
	defer themeMu.Unlock()
	return themeGen
}

// rebuildTheme resolves the spec the scheme state selects: a pinned user
// theme by name, else the built-in for the detected scheme, else neutral
// when the background is unknown. Must not be called with mdMu held.
func rebuildTheme() {
	mdMu.Lock()
	light, known, pick := mdLight, mdKnown, mdScheme
	mdMu.Unlock()
	var bg color.Color
	if known && bgCache.valid && bgCache.hasRGB {
		bg = color.RGBA{R: uint8(bgCache.r), G: uint8(bgCache.g), B: uint8(bgCache.b), A: 0xff}
	}
	spec := theme.Neutral()
	switch {
	case userThemeSpec(pick) != nil:
		spec = *userThemeSpec(pick)
	case !known:
		bg = nil
	case light:
		spec = theme.Light()
	default:
		spec = theme.Dark()
	}
	themeMu.Lock()
	activeTheme = theme.Resolve(spec, bg, themeProfile)
	themeGen++
	themeMu.Unlock()
}

// setThemeProfile records the terminal's color depth (Bubble Tea's
// ColorProfileMsg) and re-resolves: under 16 colors the surface fills vanish.
func setThemeProfile(p colorprofile.Profile) {
	themeMu.Lock()
	changed := themeProfile != p
	themeProfile = p
	themeMu.Unlock()
	if changed {
		refreshBaseStyles()
	}
}

// loadUserThemes (re)reads <config dir>/themes/*.json. Broken files are
// returned as errors and skipped so one typo never hides the other themes.
func loadUserThemes() []error {
	dir, err := config.Dir()
	if err != nil {
		return []error{err}
	}
	specs, errs := theme.Load(dir)
	themeMu.Lock()
	userThemes = specs
	themeMu.Unlock()
	return errs
}

func userThemeSpec(name string) *theme.Spec {
	if name == "" {
		return nil
	}
	themeMu.Lock()
	defer themeMu.Unlock()
	for i := range userThemes {
		if userThemes[i].Name == name {
			return &userThemes[i]
		}
	}
	return nil
}

// themeNames lists what /theme accepts: auto, the built-ins, then user themes.
func themeNames() []string {
	names := []string{"auto"}
	for _, s := range theme.Builtins() {
		names = append(names, s.Name)
	}
	themeMu.Lock()
	for _, s := range userThemes {
		names = append(names, s.Name)
	}
	themeMu.Unlock()
	return names
}

func knownThemeName(name string) bool {
	for _, n := range themeNames() {
		if n == name {
			return true
		}
	}
	return false
}

// orNo turns a nil token (terminal default) into lipgloss's explicit no-op
// color for call sites that hand the value straight to a Style.
func orNo(c color.Color) color.Color {
	if c == nil {
		return lipgloss.NoColor{}
	}
	return c
}
