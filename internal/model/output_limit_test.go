package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestRequestOutputLimitMatchesFrozenAPIWire(t *testing.T) {
	for _, adapter := range []string{"openai-chat", "openai-responses"} {
		for _, tc := range []struct {
			name          string
			limit         *int64
			ceiling, want int64
		}{
			{"nil", nil, 100, 100},
			{"narrower", new(int64(37)), 100, 37},
			{"equal", new(int64(100)), 100, 100},
			{"wider", new(int64(200)), 100, 100},
			{"maximum request", new(int64(1000000)), 100, 100},
			{"million-token model", nil, 1048576, 1048576},
			{"maximum host ceiling", nil, 1000000000, 1000000000},
			{"narrowed million-token model", new(int64(37)), 1048576, 37},
		} {
			t.Run(adapter+"/"+tc.name, func(t *testing.T) {
				request := chatRequest()
				route := Route{Kind: adapter, URL: "https://example.test/v1", Credential: "private-credential", MaxOutputTokens: tc.ceiling, TimeoutMillis: 1000, MaxAttempts: 3}
				calls := 0
				var wire []byte
				provider := OpenAI{Resolve: func(context.Context, session.ModelSelection) (Route, error) { return route, nil }, Client: &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					wire, _ = io.ReadAll(r.Body)
					body := `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`
					if adapter == "openai-responses" {
						body = responsesTerminal(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]`, "null")
					}
					return subscriptionResponse(200, body), nil
				})}}
				baseline, err := provider.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				request.OutputTokenLimit = tc.limit
				prepared, err := provider.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				if (baseline.Snapshot.RequestDigest == prepared.Snapshot.RequestDigest) != (tc.want == tc.ceiling) {
					t.Fatal("request digest does not describe the effective wire cap")
				}
				if err := prepared.Snapshot.Validate(); err != nil {
					t.Fatalf("prepared request cannot be recorded: %v", err)
				}
				route.MaxOutputTokens = 1
				if request.OutputTokenLimit != nil {
					*request.OutputTokenLimit = 1
				}
				response, err := prepared.Execute(t.Context(), nil)
				if err != nil || calls != 1 || len(response.Parts) != 1 {
					t.Fatalf("execution: %+v %v calls=%d", response, err, calls)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(wire, &fields); err != nil {
					t.Fatal(err)
				}
				field := "max_completion_tokens"
				if adapter == "openai-responses" {
					field = "max_output_tokens"
				}
				var wireLimit int64
				if err := json.Unmarshal(fields[field], &wireLimit); err != nil || wireLimit != tc.want || prepared.Snapshot.MaxOutputTokens != tc.want {
					t.Fatalf("wire cap=%d reservation=%d want=%d err=%v", wireLimit, prepared.Snapshot.MaxOutputTokens, tc.want, err)
				}
				digest := sha256.Sum256(wire)
				if prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) || strings.Contains(string(wire), "private-credential") {
					t.Fatal("request evidence was not the frozen credential-free wire")
				}
			})
		}
	}
}

func TestRequestOutputLimitRejectsInvalidBoundsBeforeSubscriptionCapture(t *testing.T) {
	for _, bound := range []int64{-1, 0, 1000001} {
		request := chatRequest()
		request.OutputTokenLimit = &bound
		for _, provider := range []interface {
			Prepare(context.Context, Request) (Prepared, error)
		}{Scripted{}, chatProvider("https://example.test/v1"), responsesProvider("https://example.test/v1")} {
			if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("bound=%d provider=%T err=%v", bound, provider, err)
			}
		}
	}
	for _, bound := range []int64{-1, 0, 127999, 1000001} {
		provider, auth, request := subscriptionFixture()
		request.OutputTokenLimit = &bound
		calls := 0
		provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected HTTP")
		})}
		if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) || auth.captures != 0 || calls != 0 {
			t.Fatalf("subscription bound=%d err=%v captures=%d calls=%d", bound, err, auth.captures, calls)
		}
	}
	for _, ceiling := range []int64{-1, 0, 1000000001} {
		request := chatRequest()
		request.OutputTokenLimit = new(int64(1))
		provider := OpenAI{Resolve: func(context.Context, session.ModelSelection) (Route, error) {
			return Route{URL: "https://example.test/v1", MaxOutputTokens: ceiling, TimeoutMillis: 1000, MaxAttempts: 1}, nil
		}}
		if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("narrowing concealed invalid route ceiling=%d: %v", ceiling, err)
		}
	}
}

func TestSubscriptionRequestOutputLimitKeepsNaturalReservationAndRefresh(t *testing.T) {
	for _, limit := range []*int64{nil, new(int64(128000)), new(int64(1000000))} {
		provider, _, request := subscriptionFixture()
		request.OutputTokenLimit = limit
		var bodies []string
		provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
			return subscriptionResponse(200, responsesTerminal(responsesOutput, responsesUsage)), nil
		})}
		request.Tools = []Tool{executeTool()}
		prepared, err := provider.Prepare(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if limit != nil {
			*limit = 1
		}
		if _, err := prepared.Execute(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		fresh, err := prepared.RefreshCredentials(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fresh.Execute(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		if len(bodies) != 2 || bodies[0] != bodies[1] || strings.Contains(bodies[0], "max_output_tokens") || prepared.Snapshot.MaxOutputTokens != 128000 || !reflect.DeepEqual(prepared.Snapshot, fresh.Snapshot) {
			t.Fatal("subscription refresh changed frozen natural-cap request")
		}
		digest := sha256.Sum256([]byte(bodies[0]))
		if prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) {
			t.Fatal("subscription digest did not match its uncapped wire")
		}
	}
}

func TestResponsesOutputLimitDoesNotChangePrivateReplayScope(t *testing.T) {
	provider := responsesProvider("https://example.test/v1")
	request := chatRequest()
	request.Tools = []Tool{executeTool()}
	provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
		return subscriptionResponse(200, responsesTerminal(responsesOutput, responsesUsage)), nil
	})}
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := prepared.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages = append(request.Messages, Message{Role: session.Assistant, Parts: response.Parts, Continuation: response.Continuation})
	request.OutputTokenLimit = new(int64(1))
	var wire string
	provider.Client.Transport = contextLimitTransport(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		wire = string(raw)
		return subscriptionResponse(200, responsesTerminal(responsesOutput, responsesUsage)), nil
	})
	narrowed, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	next, err := narrowed.Execute(t.Context(), nil)
	if err != nil || next.Continuation == nil || next.Continuation.Scope != response.Continuation.Scope || !strings.Contains(wire, "encrypted_content") || !strings.Contains(wire, "9007199254740993") {
		t.Fatalf("output bound changed same-route private replay: %v", err)
	}
}

func TestScriptedRequestOutputLimitIsFrozenReservationOnly(t *testing.T) {
	for _, tc := range []struct {
		limit *int64
		want  int64
	}{
		{nil, 4096}, {new(int64(1)), 1}, {new(int64(4096)), 4096}, {new(int64(8192)), 4096},
	} {
		request := chatRequest()
		request.Selection = session.ModelSelection{Provider: "scripted", Name: "scripted"}
		request.Purpose = "turn"
		request.OutputTokenLimit = tc.limit
		encoded, _ := json.Marshal(request)
		prepared, err := (Scripted{}).Prepare(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if tc.limit != nil {
			*tc.limit = 37
		}
		request.Messages[0].Parts[0].Text = "changed after preparation"
		response, err := prepared.Execute(t.Context(), nil)
		if err != nil || prepared.Snapshot.MaxOutputTokens != tc.want || response.Usage != (session.ModelUsage{}) || strings.Contains(response.Parts[0].Text, "changed after preparation") {
			t.Fatalf("scripted reservation/fixture behavior changed: %+v %+v %v", prepared.Snapshot, response, err)
		}
		digest := sha256.Sum256(encoded)
		if prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) {
			t.Fatal("scripted digest did not match its frozen request")
		}
	}
}
