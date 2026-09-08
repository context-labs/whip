package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestUsagePresenceAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, payload   string
		reported, valid bool
	}{
		{"zero reported", `{"prompt_tokens":0,"completion_tokens":0}`, true, true},
		{"unknown persisted", `{"reported":false,"prompt_tokens":0,"completion_tokens":0}`, false, true},
		{"negative prompt", `{"prompt_tokens":-1,"completion_tokens":0}`, true, false},
		{"negative output", `{"prompt_tokens":1,"completion_tokens":-1}`, true, false},
		{"overflow total", `{"prompt_tokens":9223372036854775807,"completion_tokens":1}`, true, false},
		{"cache exceeds prompt", `{"prompt_tokens":1,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":2}}`, true, false},
		{"reasoning exceeds output", `{"prompt_tokens":0,"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":2}}`, true, false},
		{"negative cost", `{"cost":-0.1}`, false, false},
		{"free cost", `{"cost":0}`, false, true},
		{"empty usage", `{}`, false, true},
		{"null token counts", `{"prompt_tokens":null,"completion_tokens":null}`, false, true},
		{"partial token usage", `{"prompt_tokens":10}`, false, false},
		{"partial zero usage", `{"prompt_tokens":0}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var usage Usage
			if err := json.Unmarshal([]byte(tc.payload), &usage); err != nil {
				t.Fatal(err)
			}
			if usage.Reported != tc.reported || (usage.Validate() == nil) != tc.valid {
				t.Fatalf("usage=%+v validation=%v", usage, usage.Validate())
			}
			data, err := json.Marshal(usage)
			if err != nil {
				t.Fatal(err)
			}
			var restored Usage
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.HasUsage() != usage.HasUsage() || (restored.Validate() == nil) != tc.valid {
				t.Fatalf("usage changed after persistence: %s", data)
			}
		})
	}
	for _, usage := range []Usage{{}, {Reported: true}, {PromptTokens: 3}} {
		data, err := json.Marshal(usage)
		if err != nil {
			t.Fatal(err)
		}
		var restored Usage
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.HasUsage() != usage.HasUsage() {
			t.Fatalf("usage presence changed: %s", data)
		}
	}
}

func TestPricingActualCost(t *testing.T) {
	usage := Usage{PromptTokens: 100, CompletionTokens: 20,
		PromptTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens"`
		}{80},
		CompletionTokensDetails: &struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		}{10},
	}
	priced := Pricing{Prompt: "0.000002", Completion: "0.000005", InputCacheRead: "0.0000005"}
	for _, tc := range []struct {
		name    string
		pricing Pricing
		usage   Usage
		want    int64
		known   bool
	}{
		{"cache and reasoning subsets", priced, usage, 180, true},
		{"explicit free cache", Pricing{Prompt: "0.000002", Completion: "0.000005", InputCacheRead: "0"}, usage, 140, true},
		{"missing cache full input", Pricing{Prompt: "0.000002", Completion: "0.000005"}, usage, 300, true},
		{"free model", Pricing{Prompt: "0", Completion: "0"}, usage, 0, true},
		{"absent usage", priced, Usage{}, 0, false},
		{"free model absent usage", Pricing{Prompt: "0", Completion: "0"}, Usage{}, 0, true},
		{"free model and cache absent usage", Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0"}, Usage{}, 0, true},
		{"paid cache absent usage", Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0.000001"}, Usage{}, 0, false},
		{"provider charge overrides free estimate", Pricing{Prompt: "0", Completion: "0"}, Usage{Cost: floatPointer(0.003)}, 3000, true},

		{"reported zero", priced, Usage{Reported: true}, 0, true},
		{"unknown model", Pricing{}, usage, 0, false},
		{"partial rates", Pricing{Prompt: "0.001"}, usage, 0, false},
		{"invalid catalog rate", Pricing{Prompt: "NaN", Completion: "1"}, usage, 0, false},
		{"provider cost overrides catalog", priced, Usage{PromptTokens: 100, Cost: floatPointer(0.005)}, 5000, true},
		{"provider free overrides catalog", priced, Usage{PromptTokens: 100, Cost: floatPointer(0)}, 0, true},
		{"provider cost with unknown catalog", Pricing{}, Usage{Cost: floatPointer(0.003)}, 3000, true},
		{"submicro provider cost rounds up", Pricing{}, Usage{Cost: floatPointer(0.0000001)}, 1, true},
		{"exact decimal does not overround", Pricing{}, Usage{Cost: floatPointer(0.000007)}, 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, known, err := tc.pricing.ActualCost(tc.usage)
			if err != nil || got != tc.want || known != tc.known {
				t.Fatalf("cost=(%d,%v,%v), want (%d,%v)", got, known, err, tc.want, tc.known)
			}
		})
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestPricingReservationsAndMalformedRates(t *testing.T) {
	price := Pricing{Prompt: "0.000001", Completion: "0.000003", InputCacheRead: "0.000002"}
	if got, err := price.ReserveCost(100, 10); err != nil || got != 230 {
		t.Fatalf("reservation=%d err=%v", got, err)
	}
	if _, err := (Pricing{Prompt: "1000000", Completion: "1000000"}).ReserveCost(math.MaxInt64, 1); err == nil {
		t.Fatal("overflow accepted")
	}
	if _, err := price.ReserveCost(-1, 1); err == nil {
		t.Fatal("negative reservation accepted")
	}
	for _, bad := range []string{"", "NaN", "+Inf", "-1", "1/2", "1e999999999", "1e-999999999", "words"} {
		t.Run(bad, func(t *testing.T) {
			price := Pricing{Prompt: bad, Completion: "0", InputCacheRead: bad}
			if price.Known() {
				t.Fatal("malformed price known")
			}
			if got, err := price.ReserveCost(100, 1); err != nil || got != 0 {
				t.Fatalf("reservation=%d err=%v", got, err)
			}

		})
	}
	for _, cost := range []float64{-1, math.Inf(1), math.NaN(), math.MaxFloat64} {
		if _, _, err := (Pricing{}).ActualCost(Usage{Cost: &cost}); err == nil {
			t.Fatalf("invalid provider cost accepted: %g", cost)
		}
	}
}

type attemptBudgetFunc func(context.Context, ModelAttempt) (ModelPermit, error)

func (f attemptBudgetFunc) BeginModelAttempt(ctx context.Context, attempt ModelAttempt) (ModelPermit, error) {
	return f(ctx, attempt)
}

type accountingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f accountingRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func accountingResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestModelAttemptsHaveIndependentAdmissionAndSettlement(t *testing.T) {
	noSleep(t)
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "stream"}[streaming], func(t *testing.T) {
			var attempts []ModelAttempt
			var results []ModelAttemptResult
			budget := attemptBudgetFunc(func(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
				attempts = append(attempts, attempt)
				return ModelPermit{ID: "attempt", MaxTokens: 7, Timeout: attempt.Timeout, Settle: func(result ModelAttemptResult) error { results = append(results, result); return nil }}, nil
			})
			calls := 0
			client := New("https://provider.example", "secret")
			client.HTTP.Transport = accountingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				var wire map[string]json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&wire); err != nil {
					t.Fatal(err)
				}
				if string(wire["max_tokens"]) != "7" || wire["Accounting"] != nil {
					t.Fatalf("bad request: %v", wire)
				}
				if calls == 1 {
					response := accountingResponse(503, `{"usage":{"prompt_tokens":2,"completion_tokens":0,"cost":0}}`)
					response.Status = "503 Service Unavailable"
					return response, nil
				}
				if streaming {
					return accountingResponse(200, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n"), nil
				}
				return accountingResponse(200, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`), nil
			})
			price := Pricing{Prompt: "0.000001", Completion: "0.000002"}
			req := Request{Model: "actual-model", MaxTokens: 100, Messages: []Message{{Role: "user", Content: "question"}}, Tools: []Tool{NewTool("read", "read a file", `{"type":"object"}`)}, Accounting: &CallAccounting{Budget: budget, Purpose: "turn", Provider: "router", Pricing: price}}
			var err error
			if streaming {
				_, _, err = client.Stream(context.Background(), req, nil, nil, nil)
			} else {
				_, _, err = client.Complete(context.Background(), req)
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || len(attempts) != 2 || len(results) != 2 {
				t.Fatalf("calls=%d attempts=%d settlements=%d", calls, len(attempts), len(results))
			}
			if attempts[0].LogicalID == "" || attempts[0].LogicalID != attempts[1].LogicalID || attempts[0].Number != 1 || attempts[1].Number != 2 {
				t.Fatalf("identities=%+v", attempts)
			}
			for _, attempt := range attempts {
				if attempt.Model != req.Model || attempt.Pricing != price || attempt.Provider != "router" || attempt.Purpose != "turn" || attempt.MaxTokens != 100 || attempt.InputTokens <= int64(EstimateTokens(req.Messages)) || attempt.Timeout <= 0 {
					t.Fatalf("lost request snapshot: %+v", attempt)
				}
			}
			if !results[0].Failed || !results[0].Dispatched || !results[0].Usage.Reported || results[0].Usage.Cost == nil || results[1].Failed || !results[1].Dispatched {
				t.Fatalf("bad settlements: %+v", results)
			}
		})
	}
}

func TestAccountingFailurePreservesResponseWithoutRetry(t *testing.T) {
	for _, providerFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "partial"}[providerFailed], func(t *testing.T) {
			settlements, calls := 0, 0
			budget := attemptBudgetFunc(func(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
				return ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: func(result ModelAttemptResult) error { settlements++; return errors.New("database unavailable") }}, nil
			})
			client := New("https://provider.example", "secret")
			client.HTTP.Transport = accountingRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				errorChunk := ""
				if providerFailed {
					errorChunk = `,"error":{"message":"model failed"}`
				}
				return accountingResponse(200, `{"choices":[{"message":{"content":"retained"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}`+errorChunk+`}`), nil
			})
			text, usage, err := client.Complete(context.Background(), Request{Model: "m", MaxTokens: 10, Accounting: &CallAccounting{Budget: budget}})
			if text != "retained" || !usage.Reported || !IsAccountingError(err) || IsCompletedAccountingError(err) == providerFailed || settlements != 1 || calls != 1 {
				t.Fatalf("text=%q usage=%+v err=%v settlements=%d calls=%d", text, usage, err, settlements, calls)
			}
		})
	}
}

func TestModelPermitEnforcesTimeoutAndPreDispatchFailures(t *testing.T) {
	t.Run("short elapsed permit", func(t *testing.T) {
		var result ModelAttemptResult
		budget := attemptBudgetFunc(func(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
			return ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: 10 * time.Millisecond, Settle: func(got ModelAttemptResult) error { result = got; return nil }}, nil
		})
		client := New("https://provider.example", "secret")
		client.HTTP.Transport = accountingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		_, _, err := client.Complete(context.Background(), Request{MaxTokens: 10, Accounting: &CallAccounting{Budget: budget}})
		if !errors.Is(err, context.DeadlineExceeded) || !result.Dispatched || !result.Failed || result.Elapsed <= 0 || result.Elapsed > time.Second {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
	t.Run("malformed request releases reservation", func(t *testing.T) {
		var results []ModelAttemptResult
		budget := attemptBudgetFunc(func(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
			return ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: func(result ModelAttemptResult) error { results = append(results, result); return nil }}, nil
		})
		client := New(":bad", "secret")
		_, _, err := client.Complete(context.Background(), Request{MaxTokens: 10, Accounting: &CallAccounting{Budget: budget}})
		if err == nil || len(results) != 1 || results[0].Dispatched || results[0].Elapsed != 0 || results[0].Usage.HasUsage() {
			t.Fatalf("results=%+v err=%v", results, err)
		}
	})
	t.Run("admission denial never dispatches", func(t *testing.T) {
		client := New("https://provider.example", "secret")
		client.HTTP.Transport = accountingRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unfunded call dispatched"); return nil, nil })
		budget := attemptBudgetFunc(func(context.Context, ModelAttempt) (ModelPermit, error) {
			return ModelPermit{}, errors.New("budget exhausted")
		})
		_, _, err := client.Complete(context.Background(), Request{MaxTokens: 10, Accounting: &CallAccounting{Budget: budget}})
		if !IsAccountingError(err) || IsCompletedAccountingError(err) {
			t.Fatalf("wrong error: %v", err)
		}
	})
}

func TestModelLogicalDeadlineIncludesBackoff(t *testing.T) {
	calls := 0
	client := New("https://provider.example", "secret")
	client.HTTP.Timeout = 10 * time.Millisecond
	client.HTTP.Transport = accountingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := accountingResponse(503, "busy")
		response.Status = "503 Service Unavailable"
		return response, nil
	})
	_, _, err := client.Complete(context.Background(), Request{Model: "m"})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestModelInputEstimateIncludesImagesBeforeWireStripping(t *testing.T) {
	var estimated int64
	budget := attemptBudgetFunc(func(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
		estimated = attempt.InputTokens
		return ModelPermit{}, errors.New("stop before dispatch")
	})
	image := ContentPart{Type: "image_url", W: 2800, H: 2800}
	req := Request{MaxTokens: 10, Messages: []Message{{Role: "user", Content: "look", Parts: []ContentPart{image}}}, Accounting: &CallAccounting{Budget: budget}}
	_, _, _ = New("https://provider.example", "secret").Complete(context.Background(), req)
	if estimated < int64(ImageTokens(2800, 2800)) {
		t.Fatalf("image input underestimated: %d", estimated)
	}
	if req.Messages[0].Parts[0].W != 2800 {
		t.Fatal("request mutated")
	}
}

func TestInputEstimateCannotWrapOrDoubleCountTextParts(t *testing.T) {
	if got := EstimateTokens([]Message{{Role: "user", Parts: []ContentPart{{Type: "text", Text: "12345678"}}}}); got != 6 {
		t.Fatalf("text part counted twice: %d", got)
	}
	if got := EstimateTokens([]Message{{Role: "user", Parts: []ContentPart{{Type: "image_url", W: math.MaxInt, H: math.MaxInt}}}}); got != math.MaxInt {
		t.Fatalf("image estimate wrapped: %d", got)
	}
}

func TestStreamNeverRetriesPartialOutputWithNilCallbacks(t *testing.T) {
	noSleep(t)
	for _, delta := range []string{`{"content":"partial"}`, `{"reasoning_content":"thinking"}`, `{"tool_calls":[{"index":0,"function":{"arguments":"{"}}]}`} {
		t.Run(delta, func(t *testing.T) {
			calls := 0
			client := New("https://provider.example", "secret")
			client.HTTP.Transport = accountingRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := accountingResponse(200, "")
				response.Body = io.NopCloser(io.MultiReader(strings.NewReader(`data: {"choices":[{"delta":`+delta+`}]}`+"\n\n"), accountingBrokenReader{}))
				return response, nil
			})
			_, _, err := client.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
			if err == nil || calls != 1 {
				t.Fatalf("partial output replayed: calls=%d err=%v", calls, err)
			}
		})
	}
}

type accountingBrokenReader struct{}

func (accountingBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestUsageAggregateDoesNotOverflow(t *testing.T) {
	usage := Usage{}
	usage.add(Usage{PromptTokens: math.MaxInt - 3, Reported: true})
	usage.add(Usage{PromptTokens: 2, CompletionTokens: 5, Reported: true})
	if err := usage.Validate(); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens+usage.CompletionTokens != math.MaxInt {
		t.Fatalf("aggregate=%+v", usage)
	}
	usage.add(Usage{CompletionTokens: 5, Reported: true})
	if err := usage.Validate(); err != nil {
		t.Fatal(err)
	}
}

// testCallAccounting adapts concise test hooks to the production permit API.
func testCallAccounting(begin func(context.Context, ModelAttempt) (func(ModelAttemptResult) error, error)) *CallAccounting {
	return &CallAccounting{Budget: attemptBudgetFunc(func(ctx context.Context, attempt ModelAttempt) (ModelPermit, error) {
		settle, err := begin(ctx, attempt)
		return ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: settle}, err
	})}
}

func TestUsageAggregatePreservesSettledAttempts(t *testing.T) {
	first := Usage{Reported: true, PromptTokens: 5, CompletionTokens: 3, Cost: floatPointer(0.02),
		PromptTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens"`
		}{2},
		CompletionTokensDetails: &struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		}{1},
	}
	var total Usage
	total.add(first)
	total.add(first)
	if first.Cached() != 2 || first.CompletionTokensDetails.ReasoningTokens != 1 || *first.Cost != 0.02 {
		t.Fatalf("settled attempt mutated: %+v", first)
	}
	if total.PromptTokens != 10 || total.CompletionTokens != 6 || total.Cached() != 4 || total.CompletionTokensDetails.ReasoningTokens != 2 || total.Cost == nil || *total.Cost != 0.04 {
		t.Fatalf("aggregate=%+v", total)
	}
	total.add(Usage{})
	total.add(first)
	if total.Cost != nil || total.PromptTokens != 15 || total.CompletionTokens != 9 {
		t.Fatalf("incomplete aggregate=%+v", total)
	}
}
