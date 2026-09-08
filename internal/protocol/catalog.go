package protocol

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// Catalog is the wire view of the host's independently persisted provider cache.
type Catalog struct {
	FetchedAt time.Time      `json:"fetched_at"`
	BaseURL   string         `json:"base_url"`
	Models    []CatalogModel `json:"models"`
}
type CatalogModel struct {
	Pricing             *llm.Pricing `json:"pricing,omitempty"`
	PricingKnown        bool         `json:"pricing_known,omitempty"`
	CacheReadPriceKnown bool         `json:"cache_read_price_known,omitempty"`
	ID                  string       `json:"id"`
	ContextLength       int          `json:"context_length,omitempty"`
	MaxCompletionTokens int          `json:"max_completion_tokens,omitempty"`
	ReasoningEfforts    []string     `json:"reasoning_efforts,omitempty"`
	InPrice             float64      `json:"in_price,omitempty"`
	OutPrice            float64      `json:"out_price,omitempty"`
	CacheReadPrice      float64      `json:"cache_read_price,omitempty"`
	InputModalities     []string     `json:"input_modalities,omitempty"`
}
type providerCatalogsWire struct {
	Models    map[string]ModelDescriptor    `json:"models"`
	Providers map[string]ProviderDescriptor `json:"providers"`
	Catalogs  map[string]Catalog            `json:"catalogs"`
	Errors    map[string]string             `json:"errors,omitempty"`
}

func (r ProviderCatalogsResult) MarshalJSON() ([]byte, error) {
	wire := providerCatalogsWire{Errors: r.Errors, Models: r.Models, Providers: r.Providers}
	if wire.Models == nil {
		wire.Models = map[string]ModelDescriptor{}
	}
	if wire.Providers == nil {
		wire.Providers = map[string]ProviderDescriptor{}
	}
	if r.Catalogs != nil {
		wire.Catalogs = make(map[string]Catalog, len(r.Catalogs))
	}
	for name, c := range r.Catalogs {
		v := Catalog{FetchedAt: c.FetchedAt, BaseURL: c.BaseURL}
		if c.Models != nil {
			v.Models = make([]CatalogModel, len(c.Models))
		}
		for i, m := range c.Models {
			model := CatalogModel{
				ID: m.ID, ContextLength: m.ContextLength, MaxCompletionTokens: m.MaxCompletionTokens,
				ReasoningEfforts: m.ReasoningEfforts, InputModalities: m.InputModalities,
			}
			pricing := m.Pricing
			model.Pricing = &pricing
			if pricing.Known() {
				model.PricingKnown = true
				model.InPrice, _ = strconv.ParseFloat(pricing.Prompt, 64)
				model.OutPrice, _ = strconv.ParseFloat(pricing.Completion, 64)
				if pricing.InputCacheRead != "" {
					model.CacheReadPriceKnown = true
					model.CacheReadPrice, _ = strconv.ParseFloat(pricing.InputCacheRead, 64)
				}
			}
			v.Models[i] = model
		}
		wire.Catalogs[name] = v
	}
	return json.Marshal(wire)
}
func (r *ProviderCatalogsResult) UnmarshalJSON(data []byte) error {
	var wire providerCatalogsWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = ProviderCatalogsResult{Errors: wire.Errors, Models: wire.Models, Providers: wire.Providers}
	if wire.Catalogs != nil {
		r.Catalogs = make(map[string]config.Catalog, len(wire.Catalogs))
	}
	for name, c := range wire.Catalogs {
		v := config.Catalog{FetchedAt: c.FetchedAt, BaseURL: c.BaseURL}
		if c.Models != nil {
			v.Models = make([]config.ModelInfoLite, len(c.Models))
		}
		for i, m := range c.Models {
			model := config.ModelInfoLite{
				ID: m.ID, ContextLength: m.ContextLength, MaxCompletionTokens: m.MaxCompletionTokens,
				ReasoningEfforts: m.ReasoningEfforts, InputModalities: m.InputModalities,
			}
			if m.Pricing != nil {
				model.Pricing = *m.Pricing
			} else if m.PricingKnown {
				// Older peers only supplied display prices. New peers preserve
				// the original decimals independently of those float views.
				model.Pricing = llm.Pricing{
					Prompt:     strconv.FormatFloat(m.InPrice, 'g', -1, 64),
					Completion: strconv.FormatFloat(m.OutPrice, 'g', -1, 64),
				}
				if m.CacheReadPriceKnown {
					model.Pricing.InputCacheRead = strconv.FormatFloat(m.CacheReadPrice, 'g', -1, 64)
				}
			}
			v.Models[i] = model
		}
		r.Catalogs[name] = v
	}
	return nil
}
