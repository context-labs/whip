package session

import (
	"errors"
	"strings"
	"testing"
)

func TestBudgetInputsPreserveUnknownAndRejectNegativeEvidence(t *testing.T) {
	for _, limit := range []BudgetLimit{{Kind: "unknown"}, {Kind: BudgetModelTokens, Limit: new(int64(-1))}} {
		if err := limit.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid budget accepted: %+v %v", limit, err)
		}
	}
	for _, limit := range []*int64{nil, new(int64(0)), new(int64(5))} {
		if err := (BudgetLimit{Kind: BudgetModelCalls, Limit: limit}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	request := ModelRequestSnapshot{Purpose: "turn", Model: ModelSelection{Provider: "fixture", Name: "model"}, Route: "scripted://fixture", Adapter: "scripted", RequestDigest: strings.Repeat("a", 64), MaxOutputTokens: 1, TimeoutMillis: 1}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	request.InputTokenBound = new(int64(-1))
	if err := request.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative input bound accepted: %v", err)
	}
	if err := (ModelAttemptResult{State: AttemptSucceeded, ElapsedMillis: new(int64(-1))}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative elapsed time accepted: %v", err)
	}
}
