package config

import (
	"fmt"
	"slices"
)

// ResolveEffort turns the effort a session was asked for into the concrete
// value it stores: pinned when set (an explicit config or definition choice is
// honored as-is, even if the model later turns out not to support it), else
// "low" when the model advertises it, else the lowest advertised level, else
// "off" when the catalog confirms the model does not reason, else "low" as a
// best guess for a model the catalog does not know. The result is always
// "off" or a level; blank is never an effort value.
func ResolveEffort(catalogs map[string]Catalog, provider, modelID, pinned string) string {
	if pinned != "" {
		return pinned
	}
	if c, ok := catalogs[provider]; ok {
		if mi := c.Find(modelID); mi != nil {
			levels := c.Efforts(modelID) // ["off"] for a non-reasoning model
			if slices.Contains(levels, "low") {
				return "low"
			}
			for _, e := range levels {
				if e != "off" {
					return e
				}
			}
			return "off"
		}
	}
	return "low"
}

// ValidateConfiguredEffort checks an explicit effort against the configured
// route and catalog, retaining known fallback values when they are unavailable.
func ValidateConfiguredEffort(cfg *Config, model, provider, requested string) error {
	if requested == "off" {
		return nil
	}
	known := slices.Contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}, requested)
	if cfg != nil {
		resolved, _, _, apiID, err := cfg.ResolveRoute(model, provider)
		if err == nil {
			catalog := LoadCatalogs()[resolved]
			if info := catalog.Find(apiID); info != nil && len(info.ReasoningEfforts) > 0 {
				if slices.Contains(info.ReasoningEfforts, requested) {
					return nil
				}
				if known {
					return fmt.Errorf("%s does not support effort %q", model, requested)
				}
			}
		}
	}
	if !known {
		return fmt.Errorf("unknown effort level %q", requested)
	}
	return nil
}
