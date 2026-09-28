package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitOTLPByteBounds(t *testing.T) {
	export := OTLPExport{ResourceSpans: []otlpResourceSpans{{
		Resource: otlpResource{Attributes: []otlpAttribute{stringAttr("service.name", strings.Repeat("x", 2048))}},
		ScopeSpans: []otlpScopeSpans{{
			Scope: otlpScope{Name: "whip"},
			Spans: []otlpSpan{{SpanID: "span-1", Name: "first"}},
		}},
	}}}
	marshal := func() []byte {
		t.Helper()
		data, err := json.Marshal(export)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	single := marshal()
	limit := len(single)
	for _, tooSmall := range []int{-1, 0, 100, limit - 1} {
		batches, err := SplitOTLP(single, tooSmall)
		if err == nil || len(batches) != 0 {
			t.Fatalf("limit %d accepted oversized span/envelope: %v", tooSmall, err)
		}
		if tooSmall == limit-1 && !strings.Contains(err.Error(), "span-1") {
			t.Fatalf("oversized span error must identify the span: %v", err)
		}
	}
	export.ResourceSpans[0].ScopeSpans[0].Spans = append(export.ResourceSpans[0].ScopeSpans[0].Spans,
		otlpSpan{SpanID: "span-2", Name: "other"})
	batches, err := SplitOTLP(marshal(), limit)
	if err != nil || len(batches) != 2 {
		t.Fatalf("exact single-span budget: batches=%d err=%v", len(batches), err)
	}
	for i, batch := range batches {
		if len(batch) != limit {
			t.Fatalf("batch %d: %d bytes, want exact boundary %d", i, len(batch), limit)
		}
		var decoded OTLPExport
		if err := json.Unmarshal(batch, &decoded); err != nil {
			t.Fatal(err)
		}
		spans := decoded.ResourceSpans[0].ScopeSpans[0].Spans
		if len(spans) != 1 || spans[0].SpanID != export.ResourceSpans[0].ScopeSpans[0].Spans[i].SpanID {
			t.Fatal("split changed span ordering or contents")
		}
	}
	export.ResourceSpans[0].ScopeSpans[0].Spans = []otlpSpan{}
	empty := marshal()
	batches, err = SplitOTLP(append(empty, ' '), len(empty))
	if err != nil || len(batches) != 1 || len(batches[0]) != len(empty) {
		t.Fatalf("empty export must retain its envelope: batches=%d err=%v", len(batches), err)
	}
}
