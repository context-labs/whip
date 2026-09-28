package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountContractExactTimeAndSafeFields(t *testing.T) {
	flow := OpenAILoginFlow{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", State: "authorizing", ExpiresAt: new(AccountTimestamp("2500-02-28T12:00:00.123456789Z"))}
	raw, err := json.Marshal(flow)
	if err != nil || Validate("OpenAILoginFlow", raw) != nil {
		t.Fatal("exact expiry rejected", err, string(raw))
	}
	var decoded OpenAILoginFlow
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ExpiresAt == nil || *decoded.ExpiresAt != *flow.ExpiresAt {
		t.Fatal("exact expiry changed", err)
	}
	for _, expiry := range []string{"2024-02-29T23:59:59Z", "0001-01-01T00:00:00Z", "9999-12-31T23:59:59.000000001Z"} {
		flow.ExpiresAt = new(AccountTimestamp(expiry))
		raw, _ = json.Marshal(flow)
		if err := Validate("OpenAILoginFlow", raw); err != nil {
			t.Fatal("valid expiry rejected", expiry, err)
		}
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { delete(v, "user_code") },
		func(v map[string]any) { v["state"] = "ready" },
		func(v map[string]any) { v["user_code"] = strings.Repeat("x", 65) },
		func(v map[string]any) { v["verification_url"] = "https://untrusted.test/device" },
		func(v map[string]any) { v["state"] = "cancelled"; v["user_code"] = "stale-code" },
		func(v map[string]any) { v["device_code"] = "private" },
	} {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		mutate(value)
		encoded, _ := json.Marshal(value)
		if err := Validate("OpenAILoginFlow", encoded); err == nil {
			t.Fatal("invalid projection accepted", string(encoded))
		}
	}
	items := make([]OpenAILoginFlow, 65)
	for index := range items {
		items[index] = flow
	}
	raw, _ = json.Marshal(OpenAIFlowsResult{Items: items})
	if Validate("OpenAIFlowsResult", raw) == nil {
		t.Fatal("unbounded flow list accepted")
	}
	if Validate("EmptyParams", []byte(`{"session_id":"not-a-session-operation"}`)) == nil {
		t.Fatal("account operation accepted session payload")
	}
}

func TestInferenceAccountContractBoundsAndSecretRejection(t *testing.T) {
	flow := InferenceFlow{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", Kind: new("login"), State: "choose_team", Teams: []InferenceTeam{{ID: "team", Name: "Team", Slug: "team"}}, Projects: []InferenceProject{}}
	raw, err := json.Marshal(flow)
	if err != nil || Validate("InferenceFlow", raw) != nil {
		t.Fatal("valid flow rejected", err, string(raw))
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["access_token"] = "private" },
		func(v map[string]any) { v["device_code"] = "private" },
		func(v map[string]any) { v["user_code"] = "stale-code" },
		func(v map[string]any) { v["verification_url"] = "https://evil.test" },
		func(v map[string]any) { v["teams"] = nil },
		func(v map[string]any) { v["projects"] = make([]InferenceProject, 257) },
		func(v map[string]any) { v["failure"] = strings.Repeat("x", 513) },
	} {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		mutate(value)
		encoded, _ := json.Marshal(value)
		if Validate("InferenceFlow", encoded) == nil {
			t.Fatal("invalid flow projection accepted", string(encoded))
		}
	}
	cleanup, _ := json.Marshal(InferenceCleanupResult{Items: []InferenceCleanup{}, Failure: new(strings.Repeat("x", 513))})
	if Validate("InferenceCleanupResult", cleanup) == nil {
		t.Fatal("unbounded cleanup diagnostic accepted")
	}
	for _, pair := range []struct{ typ, raw string }{
		{"InferenceTeamParams", `{"flow_id":"AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB","team_id":""}`},
		{"InferenceProjectParams", `{"flow_id":"AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB","project_id":"bad\n"}`},
		{"InferenceCreateProjectParams", `{"flow_id":"AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB","name":"","team_id":"out-of-flow"}`},
	} {
		if Validate(pair.typ, []byte(pair.raw)) == nil {
			t.Fatal("invalid account choice accepted", pair.raw)
		}
	}
}
