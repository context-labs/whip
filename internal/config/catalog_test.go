package config

import (
	"slices"
	"testing"
	"time"
)

func TestCatalogPricing(t *testing.T) {
	cat := Catalog{Models: []ModelInfoLite{
		{ID: "priced", InPrice: 1e-6, OutPrice: 5e-6, CacheReadPrice: 1e-7},
		{ID: "unpriced"},
	}}
	in, out, cr, ok := cat.Pricing("priced")
	if !ok || in != 1e-6 || out != 5e-6 || cr != 1e-7 {
		t.Fatalf("priced model: %v %v %v ok=%v", in, out, cr, ok)
	}
	if _, _, _, ok := cat.Pricing("unpriced"); ok {
		t.Fatal("model with no prices should report ok=false")
	}
	if _, _, _, ok := cat.Pricing("missing"); ok {
		t.Fatal("unknown model should report ok=false")
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
	t.Setenv("HOME", t.TempDir())
	cats := map[string]Catalog{
		"p": {Models: []ModelInfoLite{{ID: "m", InPrice: 1e-6, OutPrice: 5e-6, CacheReadPrice: 1e-7}}},
	}
	if err := SaveCatalogs(cats); err != nil {
		t.Fatal(err)
	}
	got := LoadCatalogs()
	in, out, cr, ok := got["p"].Pricing("m")
	if !ok || in != 1e-6 || out != 5e-6 || cr != 1e-7 {
		t.Fatalf("round-trip: %v %v %v ok=%v", in, out, cr, ok)
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

func TestCatalogSupportsVision(t *testing.T) {
	cat := Catalog{Models: []ModelInfoLite{
		{ID: "vision", InputModalities: []string{"text", "image"}},
		{ID: "textonly", InputModalities: []string{"text"}},
		{ID: "unadvertised"},
	}}
	cases := []struct {
		id            string
		vision, found bool
	}{
		{"vision", true, true},
		{"textonly", false, true},
		{"unadvertised", false, false}, // no modalities -> caller falls back
		{"missing", false, false},
	}
	for _, c := range cases {
		if v, f := cat.SupportsVision(c.id); v != c.vision || f != c.found {
			t.Errorf("SupportsVision(%q) = %v,%v; want %v,%v", c.id, v, f, c.vision, c.found)
		}
	}
}
