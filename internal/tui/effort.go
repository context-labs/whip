package tui

import (
	"slices"

	"github.com/context-labs/whip/internal/config"
)

// effortCands completes /effort for models without advertised levels.
var effortCands = []cand{
	{"off", "No reasoning effort parameter sent"},
	{"low", "Fast, shallow reasoning"},
	{"medium", "Balanced reasoning"},
	{"high", "Deep reasoning, slower"},
}

// defaultEfforts uses the same fallback levels and order as completion.
// "off" sends no reasoning parameter.
var defaultEfforts = func() []string {
	levels := make([]string, len(effortCands))
	for i, candidate := range effortCands {
		levels[i] = candidate.Text
	}
	return levels
}()

// effortsFor returns the cycle of effort levels available for the current
// model: the provider-advertised levels if known (each prefixed by off), else
// the defaults.
func (m *model) effortsFor() []string {
	return effortsIn(m.catalogs, m.provName, m.displayModelID())
}

// effortsIn returns the effort cycle for a model id on a provider, using the
// given catalogs. Advertised levels win (prefixed by "off"); otherwise the
// provider-agnostic defaults apply.
func effortsIn(catalogs map[string]config.Catalog, provName, modelID string) []string {
	if c, ok := catalogs[provName]; ok {
		if levels := c.Efforts(modelID); len(levels) > 1 {
			return levels // advertised: ["off", "low", "medium", …]
		}
	}
	return defaultEfforts
}

// nextEffort cycles cur to the following level in levels, wrapping; an
// unknown cur resets to levels[0].
func nextEffort(levels []string, cur string) string {
	for i, e := range levels {
		if e == cur {
			return levels[(i+1)%len(levels)]
		}
	}
	return levels[0]
}

// parseEffort validates user input against levels.
func parseEffort(levels []string, s string) (string, bool) {
	if slices.Contains(levels, s) {
		return s, true
	}
	return "", false
}

// effortCandsFor builds /effort completion candidates from levels.
func effortCandsFor(levels []string) []cand {
	out := make([]cand, 0, len(levels))
	for _, e := range levels {
		out = append(out, cand{e, ""})
	}
	return out
}

// updateCatalogs replaces the cached catalogs (called when the background
// fetch completes).
func (m *model) updateCatalogs(cats map[string]config.Catalog) {
	m.catalogs = cats
	if limit := m.contextLimitFor(m.provName, m.displayModelID()); limit > 0 {
		m.clientView.contextLimit = limit
	}
}
