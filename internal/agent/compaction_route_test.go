package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
)

func compactionRouteAgent(t *testing.T, handler http.HandlerFunc) *Agent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handler(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := llm.New(server.URL, "test")
	client.MaxRetries = 1
	a := newTestAgent(client, "conversation", 1024, "system")
	a.Provider = "conversation-provider"
	a.ContextLimit = 16000
	a.Pricing = llm.Pricing{Prompt: "0.000002", Completion: "0.000005"}
	a.CompactClient, a.CompactModel, a.CompactProvider = client, "custom", "custom-provider"
	a.CompactPricing = llm.Pricing{Prompt: "0.000007", Completion: "0.000011"}
	a.CompactMaxTokens = 512
	for range 6 {
		a.Messages = append(a.Messages, llm.Message{Role: "user", Content: "work"},
			llm.Message{Role: "assistant", Content: strings.Repeat("completed work ", 600)})
	}
	return a
}

func TestCompactionFallbackAccountsBothRoutesAndKeepsConfiguration(t *testing.T) {
	var models []string
	a := compactionRouteAgent(t, func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		models = append(models, request.Model)
		if request.Model == "custom" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "{\"error\":{\"message\":\"unavailable\"},\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":0}}")
			return
		}
		fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"retained summary\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3}}")
	})
	var attempts []llm.ModelAttempt
	var results []llm.ModelAttemptResult
	a.SetModelCallBudget(attemptBudgetFunc(func(_ context.Context, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
		attempts = append(attempts, attempt)
		return llm.ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: func(result llm.ModelAttemptResult) error {
			results = append(results, result)
			return nil
		}}, nil
	}))
	var notices []string
	var info CompactInfo
	err := a.ManualCompact(t.Context(), Events{
		OnCompactFallback: func(reason string) { notices = append(notices, reason) },
		OnCompacted:       func(_ string, _ int, value CompactInfo) { info = value },
	})
	if err != nil || !reflect.DeepEqual(models, []string{"custom", "conversation"}) {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if len(attempts) != 2 || len(results) != 2 || !results[0].Failed || results[1].Failed {
		t.Fatalf("attempts=%+v results=%+v", attempts, results)
	}
	for i, provider := range []string{"custom-provider", "conversation-provider"} {
		if attempts[i].Purpose != "compaction" || attempts[i].Provider != provider || !results[i].Dispatched {
			t.Fatalf("attempt=%+v result=%+v", attempts[i], results[i])
		}
	}
	if attempts[0].Pricing != a.CompactPricing || attempts[1].Pricing != a.Pricing ||
		attempts[0].MaxTokens != 512 || attempts[1].MaxTokens != 1024 {
		t.Fatalf("wrong route prices/limits: %+v", attempts)
	}
	if info.Model != a.Model || info.Provider != a.Provider || len(notices) != 1 || notices[0] != info.Fallback ||
		info.Usage.PromptTokens != 17 || info.Usage.CompletionTokens != 3 || a.Usage().PromptTokens != 17 {
		t.Fatalf("info=%+v notices=%v usage=%+v", info, notices, a.Usage())
	}
	if a.CompactClient != a.Client || a.CompactModel != "custom" || a.CompactProvider != "custom-provider" || a.CompactFallback != "" {
		t.Fatal("runtime fallback mutated the configured route")
	}
}

func TestCompactionFallbackNeverReplaysUnsafeFailures(t *testing.T) {
	for _, name := range []string{"budget", "settlement", "completed", "cancelled", "partial", "malformed", "network", "timeout", "request-timeout", "server-error", "bad-gateway", "generated-usage", "same-route", "fallback-budget"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			a := compactionRouteAgent(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch name {
				case "completed":
					fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"completed summary\"}}]}")
				case "partial":
					fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"partial summary\"}}],\"error\":{\"message\":\"incomplete\"}}")
				case "malformed":
					fmt.Fprint(w, "{\"choices\":[")
				case "generated-usage":
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, "{\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":1}}")
				case "timeout":
					http.Error(w, "timeout", http.StatusGatewayTimeout)
				case "request-timeout":
					http.Error(w, "timeout", http.StatusRequestTimeout)
				case "server-error":
					http.Error(w, "processing failed", http.StatusInternalServerError)
				case "bad-gateway":
					http.Error(w, "upstream response lost", http.StatusBadGateway)
				default:
					http.Error(w, "rejected", http.StatusBadRequest)
				}
			})
			if name == "same-route" {
				a.CompactModel = a.Model
			}
			if name == "network" {
				a.CompactClient = llm.New("https://custom.test", "test")
				a.CompactClient.MaxRetries = 1
				a.CompactClient.HTTP = &http.Client{Transport: compactionTransport(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return nil, errors.New("connection lost after send")
				})}
			}
			a.SetModelCallBudget(attemptBudgetFunc(func(_ context.Context, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
				if name == "budget" || name == "fallback-budget" && attempt.Model == a.Model {
					return llm.ModelPermit{}, errors.New("budget exhausted")
				}
				return llm.ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: func(llm.ModelAttemptResult) error {
					if name == "settlement" || name == "completed" {
						return errors.New("accounting unavailable")
					}
					if name == "cancelled" {
						cancel()
					}
					return nil
				}}, nil
			}))
			before := a.MessagesSnapshot()
			var journaled string
			err := a.ManualCompact(ctx, Events{OnCompacted: func(summary string, _ int, _ CompactInfo) { journaled = summary }})
			wantCalls := int32(1)
			if name == "budget" {
				wantCalls = 0
			}
			if err == nil || calls.Load() != wantCalls {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
			if name == "completed" {
				if !llm.IsCompletedAccountingError(err) || journaled != "completed summary" || !strings.Contains(a.Messages[1].Content, journaled) {
					t.Fatalf("completed response lost: journaled=%q err=%v", journaled, err)
				}
			} else if !reflect.DeepEqual(before, a.MessagesSnapshot()) {
				t.Fatal("failed compaction changed history")
			}
		})
	}
}

type compactionTransport func(*http.Request) (*http.Response, error)

func (f compactionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCompactionFallbackDoesNotHideEarlierUnknownCompletion(t *testing.T) {
	var custom, conversation int
	a := compactionRouteAgent(t, func(w http.ResponseWriter, _ *http.Request) {
		conversation++
		fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"summary\"}}]}")
	})
	a.CompactClient = llm.New("https://custom.test", "test")
	a.CompactClient.MaxRetries = 2
	a.CompactClient.HTTP = &http.Client{Transport: compactionTransport(func(*http.Request) (*http.Response, error) {
		custom++
		if custom == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("rejected"))}, nil
	})}
	_, _, _, err := a.CompactNow(t.Context())
	if err == nil || custom != 2 || conversation != 0 {
		t.Fatalf("custom=%d conversation=%d err=%v", custom, conversation, err)
	}
}

func TestCompactionFallbackPreflightsPreparedSummaryRequest(t *testing.T) {
	for _, limit := range []int{100, 8192} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			var requests []llm.Request
			a := compactionRouteAgent(t, func(w http.ResponseWriter, r *http.Request) {
				var request llm.Request
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests = append(requests, request)
				fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"summary\"}}]}")
			})
			a.CompactContextLimit = limit
			a.Messages = []llm.Message{{Role: "system", Content: "system"}, {Role: "user", Content: "work"}}
			a.Messages = toolPairs(a.Messages, 10, 100000, "t")
			if EstimateTokens(a.Messages) < limit {
				t.Fatal("fixture must exceed dedicated context before transcript preparation")
			}
			_, _, info, err := a.CompactNow(t.Context())
			if err != nil || len(requests) != 1 {
				t.Fatalf("requests=%d err=%v", len(requests), err)
			}
			request := requests[0]
			if limit == 100 {
				if request.Model != a.Model || info.Fallback == "" || request.MaxTokens != a.MaxTokens {
					t.Fatalf("preflight fallback: request=%+v info=%+v", request, info)
				}
			} else if request.Model != a.CompactModel || info.Fallback != "" || request.MaxTokens != a.CompactMaxTokens ||
				llm.EstimateTokens(request.Messages)+request.MaxTokens > limit {
				t.Fatalf("prepared transcript fits: model=%s max=%d input=%d info=%+v", request.Model, request.MaxTokens, llm.EstimateTokens(request.Messages), info)
			}
		})
	}
}

func TestCompactionFallbackUsesSubscriptionNaturalOutputForPreflight(t *testing.T) {
	for _, fits := range []bool{false, true} {
		t.Run(strconv.FormatBool(fits), func(t *testing.T) {
			var conversation, subscription int
			a := compactionRouteAgent(t, func(w http.ResponseWriter, _ *http.Request) {
				conversation++
				fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"summary\"}}]}")
			})
			auth := openaiauth.New(t.Context(), t.TempDir())
			defer auth.Close()
			if fits {
				if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{
					AccessToken: "fixture", RefreshToken: "fixture", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour),
				}); err != nil {
					t.Fatal(err)
				}
			}
			a.CompactClient = llm.NewSubscription(auth)
			a.CompactModel = "gpt-6-astra"
			a.CompactMaxTokens = 128000
			a.CompactContextLimit = 64000
			if fits {
				a.CompactContextLimit = 256000
			}
			a.CompactClient.HTTP = &http.Client{Transport: compactionTransport(func(r *http.Request) (*http.Response, error) {
				subscription++
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request["max_output_tokens"] != nil || request["max_tokens"] != nil {
					t.Error("subscription request included an unsupported explicit output cap")
				}
				body := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"summary\"}]}]}}\n\n"
				return &http.Response{Status: "200 OK", StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var reservation int
			a.SetModelCallBudget(attemptBudgetFunc(func(_ context.Context, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
				reservation = attempt.MaxTokens
				return llm.ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: func(llm.ModelAttemptResult) error { return nil }}, nil
			}))
			_, _, info, err := a.CompactNow(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if fits {
				if subscription != 1 || conversation != 0 || reservation != 128000 || info.Fallback != "" {
					t.Fatalf("subscription=%d conversation=%d reservation=%d info=%+v", subscription, conversation, reservation, info)
				}
			} else if subscription != 0 || conversation != 1 || !strings.Contains(info.Fallback, "cannot fit") {
				t.Fatalf("preflight skipped natural ceiling: subscription=%d conversation=%d info=%+v", subscription, conversation, info)
			}
		})
	}
}

func TestCompactionFallbackDoesNotHideRegeneratedPartialOutput(t *testing.T) {
	var conversation, custom int
	a := compactionRouteAgent(t, func(w http.ResponseWriter, _ *http.Request) {
		conversation++
		fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"summary\"}}]}")
	})
	a.CompactClient = llm.New("https://api.openai.com/v1", "fixture")
	a.CompactModel = "gpt-6-astra"
	a.CompactClient.MaxRetries, a.CompactClient.Regenerations = 2, 1
	a.CompactClient.HTTP = &http.Client{Transport: compactionTransport(func(*http.Request) (*http.Response, error) {
		custom++
		if custom == 1 {
			body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Partial\"}\n\n"
			return &http.Response{Status: "200 OK", StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		return &http.Response{Status: "400 Bad Request", StatusCode: http.StatusBadRequest, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("rejected"))}, nil
	})}
	_, _, _, err := a.CompactNow(t.Context())
	if err == nil || custom != 2 || conversation != 0 {
		t.Fatalf("custom=%d conversation=%d err=%v", custom, conversation, err)
	}
}

func TestCompactionFallbackHasOneBoundedConversationAttemptGroup(t *testing.T) {
	var custom, conversation int
	a := compactionRouteAgent(t, func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Model == "custom" {
			custom++
		} else {
			conversation++
		}
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})
	a.Client.MaxRetries = 2
	_, _, info, err := a.CompactNow(t.Context())
	if err == nil || custom != 2 || conversation != 2 || info.Fallback == "" {
		t.Fatalf("custom=%d conversation=%d info=%+v err=%v", custom, conversation, info, err)
	}
}

func TestCompactionFallbackRejectsSyntheticSSEHTTPStatus(t *testing.T) {
	var conversation, custom int
	a := compactionRouteAgent(t, func(w http.ResponseWriter, _ *http.Request) {
		conversation++
		fmt.Fprint(w, "{\"choices\":[{\"message\":{\"content\":\"summary\"}}]}")
	})
	a.CompactClient = llm.New("https://api.openai.com/v1", "fixture")
	a.CompactModel = "gpt-6-astra"
	a.CompactClient.MaxRetries = 1
	a.CompactClient.HTTP = &http.Client{Transport: compactionTransport(func(*http.Request) (*http.Response, error) {
		custom++
		body := "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"Thinking\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"invalid_request\"}}}\n\n"
		return &http.Response{Status: "200 OK", StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	_, _, _, err := a.CompactNow(t.Context())
	if err == nil || custom != 1 || conversation != 0 {
		t.Fatalf("custom=%d conversation=%d err=%v", custom, conversation, err)
	}
}
