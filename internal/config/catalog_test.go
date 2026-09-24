package config

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestCatalogModelPricingPreservesPresence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pricing llm.Pricing
	}{
		{name: "unknown"},
		{name: "free", pricing: llm.Pricing{Prompt: "0", Completion: "0"}},
		{name: "free cache", pricing: llm.Pricing{Prompt: "0.000002", Completion: "0.000005", InputCacheRead: "0"}},
		{name: "absent cache", pricing: llm.Pricing{Prompt: "0.000002", Completion: "0.000005"}},
		{name: "missing output", pricing: llm.Pricing{Prompt: "0.000002"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			catalog := Catalog{Models: []ModelInfoLite{
				{ID: "selected", Pricing: test.pricing},
				{ID: "other", Pricing: llm.Pricing{Prompt: "1", Completion: "2"}},
			}}
			got := catalog.ModelPricing("selected")
			if got != test.pricing {
				t.Fatalf("pricing = %+v, want %+v", got, test.pricing)
			}
			catalog.Models[0].Pricing.Prompt = "9"
			if got != test.pricing {
				t.Fatal("an existing pricing snapshot changed after a catalog update")
			}
			if missing := catalog.ModelPricing("missing"); missing != (llm.Pricing{}) {
				t.Fatalf("missing model pricing = %+v", missing)
			}
		})
	}
}

func TestCatalogEffortsNormalizesOffAndMissingModels(t *testing.T) {
	catalog := Catalog{Models: []ModelInfoLite{
		{ID: "reasoning", ReasoningEfforts: []string{"none", "low", "high"}},
		{ID: "plain"},
	}}
	if got := catalog.Efforts("reasoning"); !slices.Equal(got, []string{"", "low", "high"}) {
		t.Fatalf("reasoning efforts = %v", got)
	}
	for _, model := range []string{"plain", "missing"} {
		if got := catalog.Efforts(model); !slices.Equal(got, []string{""}) {
			t.Errorf("%s efforts = %v", model, got)
		}
	}
}

func TestCatalogPricingRoundTrip(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	free := llm.Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0"}
	paid := llm.Pricing{Prompt: "0.000001", Completion: "0.000005", InputCacheRead: "0.0000001"}
	cats := map[string]Catalog{
		"p": {Models: []ModelInfoLite{
			{ID: "m", Pricing: paid},
			{ID: "free", Pricing: free},
			{ID: "unknown"},
		}},
	}
	if err := SaveCatalogs(cats); err != nil {
		t.Fatal(err)
	}
	got := LoadCatalogs()
	if pricing := got["p"].ModelPricing("m"); pricing != paid {
		t.Fatalf("paid rates lost on round-trip: %+v", pricing)
	}
	if pricing := got["p"].ModelPricing("free"); pricing != free {
		t.Fatalf("free rates lost on round-trip: %+v", pricing)
	}
	if pricing := got["p"].ModelPricing("unknown"); pricing != (llm.Pricing{}) {
		t.Fatalf("unknown rates became known on round-trip: %+v", pricing)
	}
}

func TestCatalogStale(t *testing.T) {
	if (Catalog{FetchedAt: time.Now()}).Stale() {
		t.Fatal("just-fetched catalog must be fresh")
	}
	if !(Catalog{FetchedAt: time.Now().Add(-25 * time.Hour)}).Stale() {
		t.Fatal("day-old catalog must be stale")
	}
	if !(Catalog{}).Stale() {
		t.Fatal("zero-value catalog must be stale")
	}
}

func TestCatalogDiscoveryVersionSurvivesRestart(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	catalog := Catalog{FetchedAt: time.Now(), Models: []ModelInfoLite{{ID: "future-model"}}}
	if !catalog.NeedsDiscovery() {
		t.Fatal("legacy allowlist cache should refresh before TTL expiry")
	}
	if err := UpdateCatalog("cerebras", catalog); err != nil {
		t.Fatal(err)
	}
	loaded := LoadCatalogs()["cerebras"]
	if loaded.NeedsDiscovery() || loaded.Models[0].ID != "future-model" {
		t.Fatalf("current cache did not survive disk round trip: %+v", loaded)
	}
	loaded.FetchedAt = time.Now().Add(-25 * time.Hour)
	if !loaded.NeedsDiscovery() {
		t.Fatal("current version bypassed TTL expiry")
	}
}

func TestCatalogSupportsVision(t *testing.T) {
	cat := Catalog{Models: []ModelInfoLite{
		{ID: "vision", InputModalities: []string{"text", "image"}},
		{ID: "textonly", InputModalities: []string{"text"}},
		{ID: "unadvertised"},
		{ID: "empty", InputModalities: []string{}},
	}}
	cases := []struct {
		id            string
		vision, found bool
	}{
		{"vision", true, true},
		{"textonly", false, true},
		{"empty", false, true},
		{"unadvertised", false, false}, // no modalities -> caller falls back
		{"missing", false, false},
	}
	for _, c := range cases {
		if v, f := cat.SupportsVision(c.id); v != c.vision || f != c.found {
			t.Errorf("SupportsVision(%q) = %v,%v; want %v,%v", c.id, v, f, c.vision, c.found)
		}
	}
}

func TestCatalogModelLimitsAndFreePrices(t *testing.T) {
	catalog := Catalog{Models: []ModelInfoLite{{ID: "model", ContextLength: 1048576, MaxCompletionTokens: 1048576, Pricing: llm.Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0"}}}}
	for _, tc := range []struct {
		name  string
		model Model
		want  int
	}{
		{name: "advertised", model: Model{Context: 4096}, want: 1048576},
		{name: "explicit", model: Model{Context: 4096, MaxOut: 8192}, want: 8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contextLimit, output := catalog.ModelLimits("model", tc.model)
			if contextLimit != 1048576 || output != tc.want {
				t.Fatalf("context=%d output=%d", contextLimit, output)
			}
		})
	}
	if _, output := (Catalog{}).ModelLimits("missing", Model{Context: 32768}); output != 32768 {
		t.Fatalf("fallback output=%d", output)
	}
	if prices := catalog.ModelPricing("model"); !prices.Known() || prices.InputCacheRead != "0" {
		t.Fatalf("free: %+v", prices)
	}
}

func TestCatalogRoundTripPreservesEmptyCapabilities(t *testing.T) {
	catalog := Catalog{Models: []ModelInfoLite{
		{ID: "empty", ReasoningEfforts: []string{}, InputModalities: []string{}},
		{ID: "unknown"},
	}}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var got Catalog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Models[0].ReasoningEfforts == nil || got.Models[0].InputModalities == nil || got.Models[1].ReasoningEfforts != nil || got.Models[1].InputModalities != nil {
		t.Fatalf("empty and unknown capabilities collapsed: %s", data)
	}
}
