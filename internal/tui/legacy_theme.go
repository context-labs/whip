package tui

import (
	"github.com/context-labs/whip/internal/legacy/config"
	"github.com/context-labs/whip/internal/tui/theme"
)

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
