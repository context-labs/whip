package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func idleProvider(kind string) (OpenAI, Request) {
	if kind == "openai-codex" {
		p, _, request := subscriptionFixture()
		return p, request
	}
	p := chatProvider("https://provider.example")
	p.Resolve = func(context.Context, session.ModelSelection) (Route, error) {
		return Route{Kind: kind, URL: "https://provider.example", Credential: "private-credential", MaxOutputTokens: 4096, TimeoutMillis: 600000, MaxAttempts: 3}, nil
	}
	return p, chatRequest()
}

func assertIdleFailure(t *testing.T, response Response, err error) {
	t.Helper()
	failure, ok := errors.AsType[*CallError](err)
	if !ok || !failure.Uncertain || failure.Retryable || failure.AuthRejected || failure.ContextLimit || !strings.Contains(failure.Message, "stalled") || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private") {
		t.Fatalf("unexpected idle outcome: %v", err)
	}
	if len(response.Parts) != 0 || response.Continuation != nil {
		t.Fatal("idle stream exposed executable output")
	}
}

func TestIdlePolicyDefaultsAndFrozenRefresh(t *testing.T) {
	for _, kind := range []string{"openai-chat", "openai-responses", "openai-codex"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				provider, request := idleProvider(kind)
				calls := 0
				provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					<-r.Context().Done()
					return nil, errors.New("private transport diagnostic")
				})}
				prepared, err := provider.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				provider.IdleTimeout = time.Hour
				if kind == "openai-codex" {
					prepared, err = prepared.RefreshCredentials(t.Context())
					if err != nil {
						t.Fatal(err)
					}
				}
				started := time.Now()
				response, err := prepared.Execute(t.Context(), nil)
				assertIdleFailure(t, response, err)
				want := 5 * time.Minute
				if kind == "openai-chat" {
					want = 2 * time.Minute
				}
				if time.Since(started) != want || calls != 1 || response.Usage.Input != nil || response.ReportedCostNanoUSD != nil {
					t.Fatalf("elapsed=%v calls=%d accounting=%+v", time.Since(started), calls, response)
				}
				provider.IdleTimeout = -1
				if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("negative idle policy: %v", err)
				}
			})
		})
	}
}

func idleTerminal(kind string) string {
	if kind == "openai-chat" {
		return streamText("done") + streamFinish("stop") + streamUsage() + streamEvent("[DONE]")
	}
	return streamEvent(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":20,"output_tokens":5,"cost":0.25}}}`)
}

func TestIdleActiveStreamsAndCallerBoundaries(t *testing.T) {
	for _, kind := range []string{"openai-chat", "openai-responses", "openai-codex"} {
		for _, outcome := range []string{"complete", "cancel", "ceiling"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					provider, request := idleProvider(kind)
					provider.IdleTimeout = time.Second
					var cancel context.CancelFunc
					ctx := t.Context()
					if outcome == "ceiling" {
						ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
					} else {
						ctx, cancel = context.WithCancel(ctx)
					}
					defer cancel()
					provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
						reader, writer := io.Pipe()
						go func() {
							defer writer.Close()
							for range 8 {
								select {
								case <-r.Context().Done():
									_ = writer.CloseWithError(r.Context().Err())
									return
								case <-time.After(400 * time.Millisecond):
								}
								if _, err := io.WriteString(writer, ": keepalive\n\n"); err != nil {
									return
								}
							}
							_, _ = io.WriteString(writer, idleTerminal(kind))
						}()
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
					})}
					prepared, err := provider.Prepare(ctx, request)
					if err != nil {
						t.Fatal(err)
					}
					if outcome == "cancel" {
						go func() { time.Sleep(2 * time.Second); cancel() }()
					}
					started := time.Now()
					response, err := prepared.Execute(ctx, nil)
					switch outcome {
					case "complete":
						if err != nil || len(response.Parts) != 1 || response.Parts[0].Text != "done" || time.Since(started) <= provider.IdleTimeout {
							t.Fatalf("active stream: %+v %v elapsed=%v", response, err, time.Since(started))
						}
					case "cancel", "ceiling":
						want := context.Canceled
						if outcome == "ceiling" {
							want = context.DeadlineExceeded
						}
						if !errors.Is(err, want) || strings.Contains(err.Error(), "stalled") || len(response.Parts) > 0 {
							t.Fatalf("caller boundary: %+v %v", response, err)
						}
					}
				})
			})
		}
	}
}

func TestIdleHTTPHeadersAndPartialBody(t *testing.T) {
	for _, kind := range []string{"openai-chat", "openai-responses", "openai-codex"} {
		for _, phase := range []string{"headers", "body", "json"} {
			t.Run(kind+"/"+phase, func(t *testing.T) {
				var calls atomic.Int32
				exited := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(exited)
					_, _ = io.Copy(io.Discard, r.Body)
					calls.Add(1)
					if phase != "headers" {
						if phase == "json" {
							w.Header().Set("Content-Type", "application/json")
							_, _ = io.WriteString(w, `{"private":"incomplete`)
						} else {
							w.Header().Set("Content-Type", "text/event-stream")
							if kind == "openai-chat" {
								_, _ = io.WriteString(w, streamUsage()+streamText("preview"))
							} else {
								_, _ = io.WriteString(w, streamEvent(`{"type":"response.output_text.delta","delta":"preview"}`))
							}
						}
						_ = http.NewResponseController(w).Flush()
					}
					<-r.Context().Done()
				}))
				defer server.Close()
				provider, request := idleProvider(kind)
				provider.IdleTimeout = 100 * time.Millisecond
				// Only this test transport redirects the fixed subscription URL to
				// loopback; production endpoint/redirect policy remains unchanged.
				target, _ := url.Parse(server.URL)
				provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
					local := r.Clone(r.Context())
					local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
					return server.Client().Transport.RoundTrip(local)
				})}
				prepared, err := provider.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				provider.IdleTimeout = time.Hour // Prepared owns the old policy.
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				var preview strings.Builder
				response, err := prepared.Execute(ctx, func(chunk Chunk) { preview.WriteString(chunk.Text) })
				assertIdleFailure(t, response, err)
				if calls.Load() != 1 {
					t.Fatalf("stall replayed HTTP: %d", calls.Load())
				}
				if phase == "body" && preview.String() != "preview" {
					t.Fatalf("lost provisional output: %q", preview.String())
				}
				if phase == "body" && kind == "openai-chat" {
					if response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
						t.Fatalf("lost independently known accounting: %+v", response)
					}
				} else if response.Usage.Input != nil || response.ReportedCostNanoUSD != nil {
					t.Fatal("inferred accounting without provider evidence")
				}
				select {
				case <-exited:
				case <-ctx.Done():
					t.Fatal("HTTP request was not cancelled")
				}
			})
		}
	}
}

func TestIdleCompletedResponseWinsAndWatchdogJoins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider, request := idleProvider("openai-responses")
		provider.IdleTimeout = time.Second
		provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(idleTerminal("openai-responses")))}, nil
		})}
		prepared, err := provider.Prepare(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		// A synchronous observer may take time after the full terminal response
		// has been read. Its timing must not turn known success into uncertainty.
		response, err := prepared.Execute(t.Context(), func(Chunk) { time.Sleep(2 * time.Second) })
		if err != nil || len(response.Parts) != 1 || response.Continuation == nil || response.Usage.Input == nil {
			t.Fatalf("completed response lost to idle race: %+v %v", response, err)
		}
		ctx, watchdog := watchIdle(t.Context(), time.Hour)
		watchdog.finish(ctx, &response, &err)
		select {
		case <-watchdog.done:
		default:
			t.Fatal("watchdog not joined")
		}
	})
}
