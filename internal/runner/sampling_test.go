package runner

import (
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestContextPressureComparesSamplingValuesAndOwnsSnapshot(t *testing.T) {
	selection := session.ModelSelection{Provider: "p", Name: "model", Temperature: new(0.0), TopP: new(0.75)}
	prepared := model.Prepared{Snapshot: session.ModelRequestSnapshot{Model: selection, Route: "route", Adapter: "adapter"}, ContextWindowTokens: new(int64(100000))}
	var pressure contextPressure
	pressure.sync(prepared, 0)
	pressure.observe(new(int64(60000)), 100)
	prepared.Snapshot.Model = selection.Clone()
	pressure.sync(prepared, 0)
	if got := pressure.occupancy(125); got != 60025 {
		t.Fatalf("equal sampling at different pointer addresses lost usage: %d", got)
	}
	*prepared.Snapshot.Model.TopP = 0.5
	pressure.sync(prepared, 0)
	if got := pressure.occupancy(125); got != 125 {
		t.Fatalf("mutating a later snapshot rewrote prior sampling identity: %d", got)
	}
	pressure.observe(new(int64(60000)), 100)
	prepared.Snapshot.Model.Temperature = nil
	pressure.sync(prepared, 0)
	if got := pressure.occupancy(125); got != 125 {
		t.Fatalf("explicit zero became indistinguishable from provider default: %d", got)
	}
}
