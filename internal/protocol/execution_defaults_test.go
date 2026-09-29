package protocol

import (
	"encoding/json"
	"testing"
)

func TestExecutionDefaultsContractsRetainExactCountersAndBounds(t *testing.T) {
	base := map[string]any{"revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "engine": "quickjs", "effort": "high", "compaction_percent": 0, "goal_max_continuations": "9007199254740993", "max_attempts": 3, "preferences": ExecutionPreferences{Engine: "quickjs"}}
	for _, tc := range []struct {
		field string
		value any
		valid bool
	}{
		{"max_attempts", 1, true},
		{"max_attempts", 5, true},
		{"max_attempts", 0, false},
		{"max_attempts", 6, true},
		{"max_attempts", 9007199254740991, true},
		{"max_attempts", 9007199254740992, false},
		{"goal_max_continuations", "0", true},
		{"goal_max_continuations", "9223372036854775807", true},
		{"goal_max_continuations", "9223372036854775808", false},
		{"goal_max_continuations", 100, false},
		{"goal_max_continuations", "-1", false},
		{"compaction_percent", 100, true},
		{"compaction_percent", 101, false},
		{"compaction_percent", -1, false},
		{"engine", "lua", false},
		{"effort", "off", true},
		{"effort", "", true},
		{"effort", "custom-provider-effort", true},
	} {
		previous := base[tc.field]
		base[tc.field] = tc.value
		raw, err := json.Marshal(base)
		if err != nil || (Validate("HostExecutionDefaults", raw) == nil) != tc.valid {
			t.Fatal(tc, err, string(raw))
		}
		base[tc.field] = previous
	}
}
