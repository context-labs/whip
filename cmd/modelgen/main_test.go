package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/config/modelsdev"
	"github.com/context-labs/whip/internal/openaiauth"
)

// Fixtures use the actual policy IDs but supply no external services or keys.
func fixture(t *testing.T) []byte {
	t.Helper()
	raw := map[string]upstreamProvider{}
	for _, p := range config.ProviderPresetPolicy() {
		if p.ID == openaiauth.Provider {
			continue
		}
		id := config.ModelsDevProviderID(p.ID)
		provider := upstreamProvider{ID: id, Name: p.Provider.Name, Env: p.EnvironmentVariables, Models: map[string]upstreamModel{}}
		ids := append([]string{"test-model"}, p.SuggestedModels...)
		for _, modelID := range ids {
			model := upstreamModel{ID: modelID, Name: modelID, ToolCall: new(true), Reasoning: new(true)}
			model.Limit.Context = new(1000)
			model.Limit.Output = new(100)
			model.Modalities.Input = []string{"text"}
			model.Modalities.Output = []string{"text"}
			provider.Models[modelID] = model
		}
		raw[id] = provider
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestNormalizePreservesPresenceAndExactPrices(t *testing.T) {
	input := fixture(t)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		t.Fatal(err)
	}
	var provider map[string]any
	if err := json.Unmarshal(raw["cerebras"], &provider); err != nil {
		t.Fatal(err)
	}
	provider["models"] = map[string]any{"test-model": map[string]any{
		"id": "test-model", "name": "Test", "tool_call": false, "reasoning": false,
		"reasoning_options": []any{}, "limit": map[string]any{"context": 0},
		"modalities": map[string]any{"input": []any{}},
		"cost":       map[string]any{"input": json.Number("0"), "output": json.Number("1.4"), "cache_read": json.Number("0.0028")},
	}}
	raw["cerebras"], _ = json.Marshal(provider)
	input, _ = json.Marshal(raw)
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	snapshot, err := normalize(input, modelsdev.Snapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(snapshot, io.Discard); err != nil {
		t.Fatal(err)
	}
	model := snapshot.Providers["cerebras"].Models["test-model"]
	if model.SupportsTools == nil || *model.SupportsTools || model.Reasoning == nil || *model.Reasoning || model.ContextLength == nil || *model.ContextLength != 0 || model.MaxCompletionTokens != nil {
		t.Fatalf("presence lost: %+v", model)
	}
	if model.InputModalities == nil || len(model.InputModalities) != 0 || model.OutputModalities != nil || model.ReasoningEfforts == nil || len(model.ReasoningEfforts) != 0 {
		t.Fatalf("empty/omitted arrays collapsed: %+v", model)
	}
	if model.Pricing.Prompt != "0" || model.Pricing.Completion != "0.0000014" || model.Pricing.InputCacheRead != "0.0000000028" {
		t.Fatalf("incorrect per-token pricing: %+v", model.Pricing)
	}
	data, _ := encode(snapshot)
	roundtrip, err := modelsdev.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, roundtrip) {
		t.Fatal("snapshot roundtrip lost metadata presence")
	}
	repeated, err := normalize(input, snapshot, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	second, _ := encode(repeated)
	if !bytes.Equal(data, second) {
		t.Fatal("identical input changed output or provenance")
	}
}

func TestNormalizeDoesNotInventReasoningEfforts(t *testing.T) {
	snapshot, err := normalize(fixture(t), modelsdev.Snapshot{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if model := snapshot.Providers["cerebras"].Models["test-model"]; model.ReasoningEfforts != nil {
		t.Fatalf("reasoning true invented efforts: %v", model.ReasoningEfforts)
	}
}

func TestPerToken(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		invalid           bool
	}{
		{"absent", "", "", false}, {"free", "0", "0", false}, {"scientific", "1e-4", "0.0000000001", false},
		{"negative", "-1", "", true}, {"nondecimal", "1/2", "", true}, {"huge exponent", "1e999999999", "", true}, {"overflow precision", "1e-99", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := perToken(json.Number(test.input))
			if (err != nil) != test.invalid || got != test.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestFetchBoundsAndHTTPFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"success", http.StatusOK, `{}`, false}, {"unauthorized", http.StatusUnauthorized, `private upstream body`, true}, {"too large", http.StatusOK, strings.Repeat("x", maxInput+1), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			got, err := fetch(context.Background(), server.Client(), server.URL)
			if (err != nil) != test.wantErr {
				t.Fatalf("got %d bytes, %v", len(got), err)
			}
			if err != nil && strings.Contains(err.Error(), "private upstream body") {
				t.Fatal("included untrusted error response")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetch(ctx, http.DefaultClient, "https://models.dev/api.json"); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestRunOfflineCheckAndFailureRetention(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	if err := os.WriteFile(input, fixture(t), 0o600); err != nil {
		t.Fatal(err)
	}
	o := options{input: input, snapshot: filepath.Join(dir, "catalog.json"), environment: filepath.Join(dir, "provider-environment.ts")}
	if err := run(context.Background(), o, io.Discard); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(o.snapshot)
	envBefore, _ := os.ReadFile(o.environment)
	if err := run(context.Background(), o, io.Discard); err != nil {
		t.Fatal(err)
	}
	repeated, _ := os.ReadFile(o.snapshot)
	if !bytes.Equal(before, repeated) {
		t.Fatal("repeat changed snapshot")
	}
	check := o
	check.check = true
	check.input = ""
	if err := run(context.Background(), check, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"malformed", []byte(`{`)}, {"missing provider", []byte(`{}`)}, {"oversize", bytes.Repeat([]byte("x"), maxInput+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(input, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := run(context.Background(), o, io.Discard); err == nil {
				t.Fatal("accepted invalid input")
			}
			after, _ := os.ReadFile(o.snapshot)
			envAfter, _ := os.ReadFile(o.environment)
			if !bytes.Equal(before, after) || !bytes.Equal(envBefore, envAfter) {
				t.Fatal("failure replaced previous artifacts")
			}
		})
	}
	if err := os.WriteFile(o.environment, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), check, io.Discard); err == nil {
		t.Fatal("offline check missed artifact drift")
	}
}

func TestNormalizeRejectsMissingPolicyDefault(t *testing.T) {
	input := fixture(t)
	var raw map[string]upstreamProvider
	_ = json.Unmarshal(input, &raw)
	provider := raw["openai"]
	delete(provider.Models, "gpt-6-astra")
	raw["openai"] = provider
	input, _ = json.Marshal(raw)
	snapshot, err := normalize(input, modelsdev.Snapshot{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(snapshot, io.Discard); err == nil || !strings.Contains(err.Error(), "gpt-6-astra") {
		t.Fatalf("missing policy default accepted: %v", err)
	}
}
