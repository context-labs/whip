package tui

import (
	"image/color"
	"slices"
	"sort"
	"sync"

	uitheme "github.com/context-labs/whip/internal/tui/theme"
)

var (
	themeMu     sync.Mutex
	activeTheme = uitheme.Resolve(uitheme.Neutral(), nil)
	themeGen    int
)

func currentTheme() *uitheme.Theme {
	themeMu.Lock()
	defer themeMu.Unlock()
	return activeTheme
}

func themeGeneration() int {
	themeMu.Lock()
	defer themeMu.Unlock()
	return themeGen
}

// rebuildTheme resolves the selected catalog entry against the detected
// terminal background. It must not be called while mdMu is held.
func rebuildTheme() {
	mdMu.Lock()
	light, known, pick := mdLight, mdKnown, mdScheme
	mdMu.Unlock()

	var bg color.Color
	if known && bgCache.valid && bgCache.hasRGB {
		bg = color.RGBA{R: byte(bgCache.r), G: byte(bgCache.g), B: byte(bgCache.b), A: 0xff} //nolint:gosec // OSC RGB components are parsed in the 0..255 range.
	}
	spec := uitheme.Neutral()
	switch {
	case pick != "":
		if selected, ok := uitheme.Builtin(pick); ok {
			spec = selected
		} else if light {
			spec = uitheme.Light()
		} else {
			spec = uitheme.Dark()
		}
	case !known:
		bg = nil
	case light:
		spec = uitheme.Light()
	default:
		spec = uitheme.Dark()
	}

	themeMu.Lock()
	activeTheme = uitheme.Resolve(spec, bg)
	themeGen++
	themeMu.Unlock()
}

func themeNames() []string {
	dark, light := []string{}, []string{}
	for _, spec := range uitheme.Builtins() {
		if spec.Dark {
			dark = append(dark, spec.Name)
		} else {
			light = append(light, spec.Name)
		}
	}
	sort.Strings(dark)
	sort.Strings(light)
	return append(append([]string{"auto"}, dark...), light...)
}

func themeLabel(name string) string {
	if name == "auto" {
		return "◐  Auto"
	}
	if spec, ok := uitheme.Builtin(name); ok {
		if spec.Dark {
			return "☾  " + spec.Label()
		}
		return "☀  " + spec.Label()
	}
	return name
}

func knownThemeName(name string) bool {
	return slices.Contains(themeNames(), name)
}
