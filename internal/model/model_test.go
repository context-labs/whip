package model

import (
	"context"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestScriptedAccountingBoundDoesNotInventUsage(t *testing.T) {
	request := chatRequest()
	request.Selection = session.ModelSelection{Provider: "scripted", Name: "scripted"}
	prepared, err := (Scripted{}).Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := prepared.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Snapshot.InputTokenBound == nil || *prepared.Snapshot.InputTokenBound != 0 || response.Usage != (session.ModelUsage{}) {
		t.Fatalf("scripted bound invented usage: %+v %+v", prepared.Snapshot, response)
	}
	if prepared.ContextWindowTokens != nil {
		t.Fatalf("scripted reservation bound invented a context window: %d", *prepared.ContextWindowTokens)
	}
	cost, err := prepared.Snapshot.Prices.Cost(response.Usage)
	if err != nil || cost == nil || *cost != 0 {
		t.Fatalf("scripted cost is not known free: %v %v", cost, err)
	}
}

func TestProviderSnapshotsKeepRequestPurpose(t *testing.T) {
	for _, purpose := range []string{"", "compaction"} {
		request := chatRequest()
		request.Purpose = purpose
		want := purpose
		if want == "" {
			want = "turn"
		}
		for _, provider := range []interface {
			Prepare(context.Context, Request) (Prepared, error)
		}{Scripted{}, chatProvider("https://example.test/v1")} {
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil || prepared.Snapshot.Purpose != want {
				t.Fatalf("purpose=%q snapshot=%+v err=%v", purpose, prepared.Snapshot, err)
			}
		}
	}
}
