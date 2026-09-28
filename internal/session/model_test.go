package session

import (
	"math"
	"testing"
)

func TestModelCostKeepsUnknownSeparateFromFreeAndUsesExactArithmetic(t *testing.T) {
	rates := ModelPrices{Input: new(int64(2000000000)), Output: new(int64(10000000000)), Reasoning: new(int64(10000000000)), CachedInput: new(int64(500000000)), CachedOutput: new(int64(10000000000))}
	usage := ModelUsage{Input: new(int64(100)), Output: new(int64(20)), CachedInput: new(int64(40)), Reasoning: new(int64(5)), CachedOutput: new(int64(0))}
	value, err := rates.Cost(usage)
	if err != nil || value == nil || *value != 340000 {
		t.Fatalf("cost=%v err=%v", value, err)
	}
	value, err = rates.Cost(ModelUsage{Input: new(int64(0)), Output: new(int64(0))})
	if err != nil || value == nil || *value != 0 {
		t.Fatal("known zero usage with missing details did not have zero cost")
	}
	// Missing cache detail changes the price, so this is unknown, not zero.
	usage.CachedInput = nil
	value, err = rates.Cost(usage)
	if err != nil || value != nil {
		t.Fatalf("missing detail cost=%v err=%v", value, err)
	}
	rates.CachedInput = rates.Input
	value, err = rates.Cost(usage)
	if err != nil || value == nil || *value != 400000 {
		t.Fatalf("equal rates cost=%v err=%v", value, err)
	}
	zero := new(int64(0))
	free := ModelPrices{Input: zero, Output: zero, Reasoning: zero, CachedInput: zero, CachedOutput: zero}
	value, err = free.Cost(ModelUsage{})
	if err != nil || value == nil || *value != 0 {
		t.Fatalf("known free cost=%v err=%v", value, err)
	}
	value, err = (ModelPrices{}).Cost(ModelUsage{})
	if err != nil || value != nil {
		t.Fatal("unknown became free")
	}
	unit := new(int64(1))
	small := ModelPrices{Input: unit, CachedInput: unit, Output: unit, Reasoning: unit, CachedOutput: unit}
	value, err = small.Cost(ModelUsage{Input: unit, Output: unit})
	if err != nil || value == nil || *value != 1 {
		t.Fatalf("fractional cost should round once: %v %v", value, err)
	}
}

func TestModelCostRejectsInvalidUsageAndOverflow(t *testing.T) {
	maximum := new(int64(math.MaxInt64))
	rates := ModelPrices{Input: maximum, Output: maximum, Reasoning: maximum, CachedInput: maximum, CachedOutput: maximum}
	for _, usage := range []ModelUsage{
		{Input: new(int64(-1))},
		{Input: new(int64(1)), CachedInput: new(int64(2))},
		{Output: new(int64(2)), Reasoning: new(int64(2)), CachedOutput: new(int64(1))},
		{Input: maximum, Output: maximum},
	} {
		if _, err := rates.Cost(usage); err == nil {
			t.Fatalf("accepted invalid/overflow usage %+v", usage)
		}
	}
}
