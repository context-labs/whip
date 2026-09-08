package daemon

import (
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func cacheProviderModels(provider, baseURL string, models []llm.ModelInfo) {
	// Catalogs are a cache; the runtime can refresh a failed write on next use.
	_ = config.UpdateCatalog(provider, config.Catalog{FetchedAt: time.Now(), BaseURL: baseURL, Models: modelInfoLites(models)})
}
