package protocol

import (
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/config"
)

// Catalog is the wire view of the host's independently persisted provider cache.
type Catalog struct {
	FetchedAt time.Time      `json:"fetched_at"`
	BaseURL   string         `json:"base_url"`
	Models    []CatalogModel `json:"models"`
}
type CatalogModel struct {
	ID                  string   `json:"id"`
	ContextLength       int      `json:"context_length,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	ReasoningEfforts    []string `json:"reasoning_efforts,omitempty"`
	InPrice             float64  `json:"in_price,omitempty"`
	OutPrice            float64  `json:"out_price,omitempty"`
	CacheReadPrice      float64  `json:"cache_read_price,omitempty"`
	InputModalities     []string `json:"input_modalities,omitempty"`
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
			v.Models[i] = CatalogModel(m)
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
			v.Models[i] = config.ModelInfoLite(m)
		}
		r.Catalogs[name] = v
	}
	return nil
}
