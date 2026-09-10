package daemon

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRuntimeIntegerArguments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value any
		want  int64
	}{
		{"float", float64(9001), 9001},
		{"json", json.Number("9001"), 9001},
		{"wide exact integer", json.Number("9007199254740993"), 9007199254740993},
		{"maximum", json.Number("9223372036854775807"), math.MaxInt64},
		{"minimum", json.Number("-9223372036854775808"), math.MinInt64},
		{"json overflow", json.Number("9223372036854775808"), -99},
		{"float overflow", float64(0x1p63), -99},
		{"json fraction", json.Number("1.5"), -99},
		{"float fraction", 1.5, -99},
		{"nan", math.NaN(), -99},
		{"infinite", math.Inf(1), -99},
		{"string", "9001", -99},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := int64Argument(map[string]any{"offset": test.value}, "offset", -99)
			if got != test.want {
				t.Fatalf("offset=%v: got %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestRuntimeBudgetsAcceptExactIntegers(t *testing.T) {
	t.Parallel()
	budgets, err := requestedBudgets(map[string]any{"tokens": json.Number("9007199254740993")})
	if err != nil || len(budgets) != 1 || budgets[0].Limit != 9007199254740993 {
		t.Fatalf("exact budget: budgets=%+v err=%v", budgets, err)
	}
	for _, value := range []any{json.Number("-1"), json.Number("1.5"), json.Number("9223372036854775808")} {
		if _, err := requestedBudgets(map[string]any{"tokens": value}); err == nil {
			t.Errorf("accepted invalid budget %v", value)
		}
	}
}
