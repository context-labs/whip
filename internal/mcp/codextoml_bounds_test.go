package mcp

import (
	"fmt"
	"math"
	"testing"
)

func TestToIntBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input any
		want  int
		ok    bool
	}{
		{"integer minimum", int64(math.MinInt), math.MinInt, true},
		{"integer maximum", int64(math.MaxInt), math.MaxInt, true},
		{"integer 64-bit minimum", int64(math.MinInt64), math.MinInt, math.MinInt == math.MinInt64},
		{"integer 64-bit maximum", int64(math.MaxInt64), math.MaxInt, math.MaxInt == math.MaxInt64},
		{"float minimum", float64(math.MinInt), math.MinInt, true},
		{"float upper bound", -float64(math.MinInt), 0, false},
		{"float largest below int64 bound", math.Nextafter(float64(math.MaxInt64), 0), math.MaxInt - 1023, math.MaxInt == math.MaxInt64},
		{"float below minimum", math.Nextafter(float64(math.MinInt), math.Inf(-1)), 0, false},
		{"float whole", 30.0, 30, true},
		{"float fractional", 30.5, 0, false},
		{"float negative fractional", -0.5, 0, false},
		{"nan", math.NaN(), 0, false},
		{"positive infinity", math.Inf(1), 0, false},
		{"negative infinity", math.Inf(-1), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toInt(tc.input)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("toInt(%v) = %d, %v; want %d, %v", tc.input, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParseCodexRejectsInvalidTimeoutNumbers(t *testing.T) {
	for _, key := range []string{"startup_timeout_sec", "startup_timeout_ms", "tool_timeout_sec"} {
		for _, number := range []string{"9223372036854775808", "-9223372036854775809", "1e100", "NaN", "Inf", "1.5"} {
			t.Run(key+"/"+number, func(t *testing.T) {
				data := fmt.Sprintf("[mcp_servers.test]\ncommand = 'test'\n%s = %s\n", key, number)
				if _, err := ParseCodex([]byte(data)); err == nil {
					t.Fatal("out-of-range or non-integral timeout was accepted")
				}
			})
		}
	}
}
