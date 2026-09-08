package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamPreservesPartialResponseAndErrorUsage(t *testing.T) {
	srv := sseServer(t,
		`data: {"choices":[{"delta":{"content":"retained answer"}}]}`,
		`data: {"error":{"message":"provider failed"},"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
	)
	defer srv.Close()
	msg, usage, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err == nil || msg.Content != "retained answer" || usage.PromptTokens != 12 || usage.CompletionTokens != 3 {
		t.Fatalf("partial response lost: message=%+v usage=%+v err=%v", msg, usage, err)
	}
}

func TestStreamUnterminatedResponseCannotExecuteTools(t *testing.T) {
	srv := sseServer(t, `data: {"choices":[{"delta":{"content":"partial","tool_calls":[{"index":0,"id":"call","function":{"name":"write","arguments":"{}"}}]}}]}`)
	defer srv.Close()
	msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err == nil || msg.Content != "partial" || len(msg.ToolCalls) != 0 {
		t.Fatalf("unterminated response accepted: message=%+v err=%v", msg, err)
	}
}

func TestCompletePreservesUsageWithMissingChoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
	}))
	defer srv.Close()
	client := New(srv.URL, "test-key")
	client.MaxRetries = 1
	_, usage, err := client.Complete(context.Background(), Request{Model: "m"})
	if err == nil || usage.PromptTokens != 12 || usage.CompletionTokens != 3 {
		t.Fatalf("error usage lost: usage=%+v err=%v", usage, err)
	}
}

func TestMalformedUsageDoesNotEraseResponseContent(t *testing.T) {
	for _, test := range []struct{ name, tokens string }{{"overflow", "999999999999999999999999"}, {"wrong type", `"invalid"`}} {
		t.Run(test.name, func(t *testing.T) {
			for _, streaming := range []bool{false, true} {
				t.Run(map[bool]string{false: "complete", true: "stream"}[streaming], func(t *testing.T) {
					usageJSON := `{"prompt_tokens":` + test.tokens + `,"completion_tokens":1,"cost":0.02}`
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						if streaming {
							_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"retained\"},\"finish_reason\":\"stop\"}],\"usage\":%s}\n\n", usageJSON)
							return
						}
						_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"retained"}}],"usage":%s}`, usageJSON)
					}))
					defer srv.Close()
					client := New(srv.URL, "test-key")
					var content string
					var usage Usage
					var err error
					if streaming {
						var message Message
						message, usage, err = client.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
						content = message.Content
					} else {
						content, usage, err = client.Complete(context.Background(), Request{Model: "m"})
					}
					if err == nil || content != "retained" || usage.HasUsage() || usage.Cost == nil || *usage.Cost != 0.02 {
						t.Fatalf("content=%q usage=%+v err=%v", content, usage, err)
					}
				})
			}
		})
	}
}

func TestUsageFieldsFailIndependentlyAcrossTransports(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		reported      bool
		cost          *float64
	}{
		{name: "malformed tokens", payload: `{"prompt_tokens":"bad","completion_tokens":1,"cost":0.02}`, cost: floatPointer(0.02)},
		{name: "invalid token relationship", payload: `{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":3},"cost":0}`, cost: floatPointer(0)},
		{name: "malformed cost", payload: `{"prompt_tokens":2,"completion_tokens":1,"cost":"bad"}`, reported: true},
		{name: "negative cost", payload: `{"prompt_tokens":2,"completion_tokens":1,"cost":-1}`, reported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []string{"complete", "stream", "http error"} {
				t.Run(transport, func(t *testing.T) {
					requests := 0
					client := New("https://provider.example", "secret")
					client.HTTP.Transport = accountingRoundTripFunc(func(*http.Request) (*http.Response, error) {
						requests++
						if transport == "http error" {
							response := accountingResponse(http.StatusBadGateway, `{"usage":`+tc.payload+`}`)
							response.Status = "502 Bad Gateway"
							return response, nil
						}
						if transport == "stream" {
							return accountingResponse(200, `data: {"choices":[{"delta":{"content":"retained"},"finish_reason":"stop"}],"usage":`+tc.payload+"}\n\n"), nil
						}
						return accountingResponse(200, `{"choices":[{"message":{"content":"retained"}}],"usage":`+tc.payload+`}`), nil
					})
					var settled ModelAttemptResult
					req := Request{Accounting: testCallAccounting(func(context.Context, ModelAttempt) (func(ModelAttemptResult) error, error) {
						return func(result ModelAttemptResult) error { settled = result; return nil }, nil
					})}
					var err error
					var text string
					if transport == "stream" {
						var message Message
						message, _, err = client.Stream(t.Context(), req, nil, nil, nil)
						text = message.Content
					} else {
						text, _, err = client.Complete(t.Context(), req)
					}
					if err == nil || requests != 1 || !settled.Dispatched || !settled.Failed {
						t.Fatalf("requests=%d settled=%+v err=%v", requests, settled, err)
					}
					if transport != "http error" && text != "retained" {
						t.Fatalf("response lost: %q", text)
					}
					if got := settled.Usage; got.HasUsage() != tc.reported || (got.Cost == nil) != (tc.cost == nil) || got.Cost != nil && *got.Cost != *tc.cost {
						t.Fatalf("usage=%+v", got)
					}
				})
			}
		})
	}
}

func TestProviderCannotSupplyInternalUsagePresence(t *testing.T) {
	usage, err := decodeUsage([]byte(`{"reported":true,"cost":0}`))
	if err != nil || usage.HasUsage() || usage.Cost == nil || *usage.Cost != 0 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}
