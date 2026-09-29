package config

import (
	"slices"

	"github.com/context-labs/whip/internal/modelcatalog"
)

// ModelsDevProviderID maps Whip's stable IDs to the upstream catalog.
func ModelsDevProviderID(id string) string { return modelcatalog.ProviderID(id) }

func augmentProviderPresets(presets []ProviderPreset) []ProviderPreset {
	for i := range presets {
		metadata, ok := modelcatalog.Metadata(ModelsDevProviderID(presets[i].ID))
		if !ok {
			continue
		}
		// Local spelling, endpoint, credentials and recommendations are policy.
		if presets[i].Provider.Name == "" {
			presets[i].Provider.Name = metadata.Name
		}
		for _, name := range metadata.Env {
			if !slices.Contains(presets[i].EnvironmentVariables, name) {
				presets[i].EnvironmentVariables = append(presets[i].EnvironmentVariables, name)
			}
		}
	}
	return presets
}
