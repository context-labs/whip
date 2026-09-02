package tui

import (
	"slices"

	"github.com/context-labs/whip/internal/config"
)

// defaultEfforts are the fallback levels when the provider doesn't advertise
// supported reasoning efforts; "" means off (parameter omitted from requests).
var defaultEfforts = []string{"", "low", "medium", "high"}

// effortCands completes /effort for models without advertised levels.
var effortCands = []cand{
	{"off", "No reasoning effort parameter sent"},
	{"low", "Fast, shallow reasoning"},
	{"medium", "Balanced reasoning"},
	{"high", "Deep reasoning, slower"},
}

// effortsFor returns the cycle of effort levels available for the current
// model: the provider-advertised levels if known (each prefixed by off), else
// the defaults.
func (m *model) effortsFor() []string {
	return effortsIn(m.catalogs, m.provName, m.displayModelID())
}

// effortsIn returns the effort cycle for a model id on a provider, using the
// given catalogs. Advertised levels win (prefixed by off ""); otherwise the
// provider-agnostic defaults apply.
func effortsIn(catalogs map[string]config.Catalog, provName, modelID string) []string {
	if c, ok := catalogs[provName]; ok {
		if levels := c.Efforts(modelID); len(levels) > 1 {
			return levels // advertised: ["", "low", "medium", …]
		}
	}
	return defaultEfforts
}

// DefaultEffortFor resolves the effort a new session should open on when the
// user hasn't pinned one (cfg.DefaultEffort == ""): "low" when the model
// advertises it (the intended default), else the lowest advertised level, else
// "" (off) when the catalog confirms the model doesn't reason, else "low" as a
// best guess when the model is entirely unknown (no catalog entry). pinned,
// when non-empty, is returned verbatim — an explicit config choice is honored
// as-is, even if the model later turns out not to support it (updateCatalogs
// resets the live session).
func DefaultEffortFor(catalogs map[string]config.Catalog, provName, modelID, pinned string) string {
	if pinned != "" {
		return pinned
	}
	// When the catalog has the entry, trust its advertised levels: prefer "low",
	// else the lowest level, else off (the model doesn't reason). When the entry
	// is missing entirely (unknown model/provider), fall back to "low".
	if c, ok := catalogs[provName]; ok {
		if mi := c.Find(modelID); mi != nil {
			levels := c.Efforts(modelID) // [""] for a non-reasoning model
			if slices.Contains(levels, "low") {
				return "low"
			}
			for _, e := range levels {
				if e != "" {
					return e
				}
			}
			return "" // catalog confirms: no reasoning
		}
	}
	return "low" // unknown model — best-guess default for a reasoning ecosystem
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

// effortLabel renders a level for display ("" shows as off).
func effortLabel(e string) string {
	if e == "" {
		return "off"
	}
	return e
}

// parseEffort validates user input against levels ("off" maps to "").
func parseEffort(levels []string, s string) (string, bool) {
	if s == "off" {
		return "", true
	}
	for _, e := range levels[1:] {
		if s == e {
			return e, true
		}
	}
	return "", false
}

// effortCandsFor builds /effort completion candidates from levels.
func effortCandsFor(levels []string) []cand {
	out := make([]cand, 0, len(levels))
	for _, e := range levels {
		out = append(out, cand{effortLabel(e), ""})
	}
	return out
}

// updateCatalogs replaces the cached catalogs (called when the background
// fetch completes).
func (m *model) updateCatalogs(cats map[string]config.Catalog) {
	m.catalogs = cats
	if n := m.contextLimitFor(m.provName, m.agent.Model); n != m.agent.ContextLimit {
		m.agent.ContextLimit = n // /models is the source of truth
	}
	if !slices.Contains(m.effortsFor(), m.agent.Effort) {
		m.resetEffort("")
		m.append(dimStyle.Render("⚡ effort reset to off: not supported by " + m.agent.Model))
	}
}
