package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestChatSamplingFrozenWireAndPresence(t *testing.T) {
	for _, test := range []struct {
		name              string
		temperature, topP *float64
	}{
		{"provider defaults", nil, nil},
		{"zero", new(0.0), new(0.0)},
		{"temperature only", new(0.25), nil},
		{"top_p only", nil, new(0.75)},
		{"upper bounds", new(2.0), new(1.0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := chatRequest()
			request.Selection.Temperature, request.Selection.TopP = test.temperature, test.topP
			want := request.Selection.Clone()
			provider := chatProvider("https://example.test/v1")
			resolve := provider.Resolve
			var resolved session.ModelSelection
			provider.Resolve = func(ctx context.Context, selection session.ModelSelection) (Route, error) {
				resolved = selection
				return resolve(ctx, selection)
			}
			var body []byte
			provider.Client = &http.Client{Transport: contextLimitTransport(func(request *http.Request) (*http.Response, error) {
				var err error
				body, err = io.ReadAll(request.Body)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))}, err
			})}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if request.Selection.Temperature != nil {
				*request.Selection.Temperature = 1.5
			}
			if request.Selection.TopP != nil {
				*request.Selection.TopP = 0.5
			}
			if resolved.Temperature != nil {
				*resolved.Temperature = 1.75
			}
			if resolved.TopP != nil {
				*resolved.TopP = 0.25
			}
			provider.Resolve = nil
			for range 2 {
				if _, err := prepared.Execute(t.Context(), nil); err != nil {
					t.Fatal(err)
				}
				assertChatWireEvidence(t, prepared, want, body)
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(body, &fields); err != nil {
					t.Fatal(err)
				}
				for field, value := range map[string]*float64{"temperature": want.Temperature, "top_p": want.TopP} {
					raw, present := fields[field]
					if present != (value != nil) {
						t.Fatalf("%s presence changed: %s", field, body)
					}
					if value != nil {
						var actual float64
						if err := json.Unmarshal(raw, &actual); err != nil || actual != *value {
							t.Fatalf("%s changed: %s %v", field, raw, err)
						}
					}
				}
			}
		})
	}
}

func TestInvalidSamplingRejectsBeforeResolution(t *testing.T) {
	for _, test := range []struct {
		temperature, topP *float64
	}{
		{temperature: new(-0.01)},
		{temperature: new(2.01)},
		{temperature: new(math.NaN())},
		{temperature: new(math.Inf(1))},
		{topP: new(-0.01)},
		{topP: new(1.01)},
		{topP: new(math.NaN())},
		{topP: new(math.Inf(-1))},
	} {
		request := chatRequest()
		request.Selection.Temperature, request.Selection.TopP = test.temperature, test.topP
		provider := OpenAI{Resolve: func(context.Context, session.ModelSelection) (Route, error) {
			t.Fatal("invalid sampling reached resolution")
			return Route{}, nil
		}}
		if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid sampling accepted: %v", err)
		}
	}
}

func TestResponsesSamplingRejectsBeforeCredentialsAndHTTP(t *testing.T) {
	for _, kind := range []string{"openai-responses", "openai-codex"} {
		for _, field := range []string{"temperature", "top_p"} {
			for _, value := range []float64{0, 0.5} {
				provider, auth, request := subscriptionFixture()
				if field == "temperature" {
					request.Selection.Temperature = new(value)
				} else {
					request.Selection.TopP = new(value)
				}
				provider.Resolve = func(context.Context, session.ModelSelection) (Route, error) {
					return Route{Kind: kind, URL: "https://example.test/v1", MaxOutputTokens: 128000, TimeoutMillis: 1000, MaxAttempts: 1}, nil
				}
				provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
					t.Fatal("unsupported sampling reached HTTP")
					return nil, errors.New("unexpected HTTP request")
				})}
				_, err := provider.Prepare(t.Context(), request)
				if !errors.Is(err, session.ErrInvalid) || !strings.Contains(err.Error(), "temperature and top_p") || auth.captures != 0 || auth.refreshes != 0 {
					t.Fatalf("%s %s=%g: err=%v captures=%d", kind, field, value, err, auth.captures)
				}
			}
		}
	}
}

func TestScriptedSamplingSnapshotIsCapturedWithoutSimulation(t *testing.T) {
	request := chatRequest()
	request.Selection = session.ModelSelection{Provider: "scripted", Name: "scripted", Temperature: new(0.0), TopP: new(0.9)}
	want := request.Selection.Clone()
	prepared, err := (Scripted{}).Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	*request.Selection.Temperature, *request.Selection.TopP = 2, 1
	response, err := prepared.Execute(t.Context(), nil)
	if err != nil || !prepared.Snapshot.Model.Equal(want) || len(response.Parts) != 1 || response.Usage != (session.ModelUsage{}) {
		t.Fatalf("scripted sampling changed snapshot or invented usage: %+v %v", prepared.Snapshot, err)
	}
}
