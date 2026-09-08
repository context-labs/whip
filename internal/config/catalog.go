package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

// catalogTTL is how long a provider's fetched model list stays fresh.
const catalogTTL = 24 * time.Hour

// Catalog is the cached model list of one provider.
type Catalog struct {
	FetchedAt time.Time       `json:"fetchedAt"`
	BaseURL   string          `json:"baseUrl"`
	Models    []ModelInfoLite `json:"models"`
}

// ModelInfoLite is the subset of the provider's /models entry whip uses.
type ModelInfoLite struct {
	PricingKnown        bool     `json:"pricingKnown,omitempty"`
	CacheReadPriceKnown bool     `json:"cacheReadPriceKnown,omitempty"`
	ID                  string   `json:"id"`
	ContextLength       int      `json:"contextLength,omitempty"`       // model's context window (input), 0 if unadvertised
	MaxCompletionTokens int      `json:"maxCompletionTokens,omitempty"` // provider's output cap, 0 if unadvertised
	ReasoningEfforts    []string `json:"reasoningEfforts,omitempty"`
	InPrice             float64  `json:"inPrice,omitempty"`         // USD per prompt token; PricingKnown distinguishes free from missing
	OutPrice            float64  `json:"outPrice,omitempty"`        // USD per completion token; see PricingKnown
	CacheReadPrice      float64  `json:"cacheReadPrice,omitempty"`  // USD per cached prompt token; see CacheReadPriceKnown
	InputModalities     []string `json:"inputModalities,omitempty"` // provider-advertised input types (["text","image"])
}

// SupportsVision reports whether the catalog advertises image input for a model
// id. The bool is tri-state: found==false means the catalog has no entry or the
// entry doesn't advertise modalities, so the caller falls back to config.
func (c Catalog) SupportsVision(id string) (vision, found bool) {
	for _, mi := range c.Models {
		if mi.ID == id {
			if len(mi.InputModalities) == 0 {
				return false, false
			}
			if slices.Contains(mi.InputModalities, "image") {
				return true, true
			}
			return false, true
		}
	}
	return false, false
}

// ContextLength reports the advertised context window for a model id
// (0 when the catalog has no entry for it — callers must fall back).
func (c Catalog) ContextLength(id string) int {
	for _, mi := range c.Models {
		if mi.ID == id {
			return mi.ContextLength
		}
	}
	return 0
}

// MaxCompletionTokens reports the advertised output-token cap for a model id
// (0 when unknown — callers must fall back to the configured context).
func (c Catalog) MaxCompletionTokens(id string) int {
	for _, mi := range c.Models {
		if mi.ID == id {
			return mi.MaxCompletionTokens
		}
	}
	return 0
}

// Pricing reports the advertised per-token USD rates for a model id; ok is
// false when the catalog has no entry for it or the entry has no prices, in
// which case callers should hide cost rather than show $0.
func (c Catalog) Pricing(id string) (in, out, cacheRead float64, ok bool) {
	prices := c.TokenPrices(id)
	return prices.Input, prices.Output, prices.CacheRate(), prices.Known
}

// TokenPrices returns a snapshot; an explicit free rate stays distinct from
// absent pricing. Older positive cached prices remain usable until refresh.
func (c Catalog) TokenPrices(id string) llm.TokenPrices {
	if mi := c.Find(id); mi != nil {
		prices := llm.TokenPrices{Input: mi.InPrice, Output: mi.OutPrice, CacheRead: mi.CacheReadPrice,
			Known:          mi.PricingKnown || mi.InPrice > 0 && mi.OutPrice > 0,
			CacheReadKnown: mi.CacheReadPriceKnown || mi.CacheReadPrice > 0}
		if prices.Validate() == nil {
			return prices
		}
	}
	return llm.TokenPrices{}
}

// ModelLimits resolves the same context and response ceilings for every route.
func (c Catalog) ModelLimits(id string, model Model) (contextLimit, maxOutput int) {
	contextLimit = model.ContextWindow()
	if value := c.ContextLength(id); value > 0 {
		contextLimit = value
	}
	maxOutput = model.MaxOut
	if maxOutput <= 0 {
		maxOutput = c.MaxCompletionTokens(id)
	}
	if maxOutput <= 0 {
		maxOutput = contextLimit
	}
	return contextLimit, maxOutput
}

// Find returns the catalog entry for a model id (nil when unadvertised).
func (c Catalog) Find(id string) *ModelInfoLite {
	for i := range c.Models {
		if c.Models[i].ID == id {
			return &c.Models[i]
		}
	}
	return nil
}

// Efforts returns the reasoning-effort levels available for a model id, in
// provider order and prefixed by "" (off): ["", "low", "medium", "high", …].
// "" is the only entry (i.e. the model doesn't reason) when the catalog has no
// entry for the model or the entry advertises no efforts. A "none" effort is
// collapsed into the leading off ("").
func (c Catalog) Efforts(id string) []string {
	mi := c.Find(id)
	if mi == nil || len(mi.ReasoningEfforts) == 0 {
		return []string{""}
	}
	out := []string{""}
	for _, e := range mi.ReasoningEfforts {
		if e != "none" { // "none" is our off ("")
			out = append(out, e)
		}
	}
	return out
}

func catalogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

// LoadCatalogs reads ~/.whip/models.json. A missing or unreadable file is
// not an error and yields an empty (non-nil) map, so callers can always write
// into the result.
var catalogMu sync.Mutex

func LoadCatalogs() map[string]Catalog {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	return loadCatalogsUnlocked()
}

func loadCatalogsUnlocked() map[string]Catalog {
	cats := map[string]Catalog{}
	p, err := catalogPath()
	if err != nil {
		return cats
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cats
	}
	if json.Unmarshal(data, &cats) != nil || cats == nil {
		return map[string]Catalog{}
	}
	return cats
}

// SaveCatalogs writes ~/.whip/models.json.
func SaveCatalogs(cats map[string]Catalog) error {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	return saveCatalogsUnlocked(cats)
}

func saveCatalogsUnlocked(cats map[string]Catalog) error {
	p, err := catalogPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cats, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// Stale reports whether the cached catalog should be refetched.
func (c Catalog) Stale() bool { return time.Since(c.FetchedAt) > catalogTTL }

// UpdateCatalog merges one host-fetched catalog without replacing concurrently
// refreshed providers.
func UpdateCatalog(provider string, catalog Catalog) error {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	catalogs := loadCatalogsUnlocked()
	catalogs[provider] = catalog
	return saveCatalogsUnlocked(catalogs)
}
