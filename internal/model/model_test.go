package model

import (
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
	cost, err := prepared.Snapshot.Prices.Cost(response.Usage)
	if err != nil || cost == nil || *cost != 0 {
		t.Fatalf("scripted cost is not known free: %v %v", cost, err)
	}
}
