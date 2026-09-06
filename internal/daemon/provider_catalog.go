package daemon

import (
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func cacheProviderModels(provider, baseURL string, models []llm.ModelInfo) {
	entries := make([]config.ModelInfoLite, len(models))
	for i, model := range models {
		entries[i] = config.ModelInfoLite{ID: model.ID, ContextLength: model.ContextLength,
			MaxCompletionTokens: model.MaxCompletionTokens, ReasoningEfforts: model.ReasoningEfforts, InputModalities: model.InputModalities}
		if model.Pricing != nil {
			entries[i].InPrice, entries[i].OutPrice, entries[i].CacheReadPrice = model.Pricing.Rates()
		}
	}
	// Catalogs are a cache; the runtime can refresh a failed write on next use.
	_ = config.UpdateCatalog(provider, config.Catalog{FetchedAt: time.Now(), BaseURL: baseURL, Models: entries})
}
