package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInputIdentityProjectionIsNullableAndBounded(t *testing.T) {
	for _, value := range []any{
		InputPageResult{Items: []InputSummary{{ID: "input", SessionID: "owner", Ordinal: 9007199254740993, Source: "agent", Kind: "prompt", State: "queued", CreatedAt: "2026-09-28T00:00:00Z"}}},
		InputPageResult{Items: []InputSummary{{Identity: &RequestIdentity{ClientID: ID(strings.Repeat("c", 128)), RequestID: ID(strings.Repeat("r", 128))}, ID: "input", SessionID: "owner", Ordinal: 9007199254740993, Source: "user", Kind: "prompt", State: "queued", CreatedAt: "2026-09-28T00:00:00Z"}}},
	} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("InputPageResult", raw); err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		item := fields["items"].([]any)[0].(map[string]any)
		if item["ordinal"] != "9007199254740993" {
			t.Fatal("input ordinal lost precision", item)
		}
		item["identity"] = map[string]any{"client_id": strings.Repeat("c", 129), "request_id": "request"}
		invalid, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("InputPageResult", invalid); err == nil {
			t.Fatal("unbounded receipt identity accepted")
		}
	}
}
