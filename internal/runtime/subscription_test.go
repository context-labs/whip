package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type subscriptionTransport func(*http.Request) (*http.Response, error)

func (f subscriptionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func subscriptionProvider(auth *openaiauth.Manager, transport http.RoundTripper) model.OpenAI {
	return model.OpenAI{Auth: auth, Client: &http.Client{Transport: transport}, Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
		return model.Route{Kind: "openai-codex", MaxAttempts: 2, TimeoutMillis: 5000, ContextWindowTokens: new(int64(400000))}, nil
	}}
}

func subscriptionCredentials() openaiauth.Credentials {
	return openaiauth.Credentials{AccessToken: "private-access", RefreshToken: "private-refresh", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour)}
}

func TestSubscriptionBothEnginesKeepPrivateReplayAndREPLAcrossRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			auth := openaiauth.New(t.Context(), directory)
			t.Cleanup(auth.Close)
			credentials := subscriptionCredentials()
			if err := auth.Install(t.Context(), auth.Generation(), credentials); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var bodies []string
			var expectedAccount atomic.Value
			expectedAccount.Store("account")
			transport := subscriptionTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != openaiauth.BaseURL+"/responses" || request.Header.Get("Chatgpt-Account-Id") != expectedAccount.Load().(string) {
					return nil, errors.New("incorrect subscription route/account")
				}
				raw, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				mu.Lock()
				bodies = append(bodies, string(raw))
				mu.Unlock()
				var wire struct {
					Input []struct {
						Type, Role, Output string
						Content            []struct{ Text string }
					}
				}
				if err := json.Unmarshal(raw, &wire); err != nil {
					return nil, err
				}
				if len(wire.Input) == 0 {
					return nil, errors.New("missing model input")
				}
				var output string
				if wire.Input[len(wire.Input)-1].Type == "function_call_output" {
					output = `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]`
				} else {
					prompt := ""
					for _, input := range wire.Input {
						if input.Role == "user" && len(input.Content) > 0 {
							prompt = input.Content[0].Text
						}
					}
					code := "x += 1\nprint(x)"
					if engine == session.QuickJS {
						code = "x += 1; console.log(x)"
					}
					if prompt == "first" {
						code = "x = 41\nprint(x)"
						if engine == session.QuickJS {
							code = "var x = 41; console.log(x)"
						}
					}
					args, _ := json.Marshal(map[string]string{"code": code})
					quoted, _ := json.Marshal(string(args))
					callID, _ := json.Marshal("call_" + prompt)
					output = `[{"type":"reasoning","encrypted_content":"opaque-subscription","future":9007199254740993},{"type":"function_call","id":"provider-item","call_id":` + string(callID) + `,"name":"execute","arguments":` + string(quoted) + `}]`
				}
				body := `data: {"type":"response.completed","response":{"status":"completed","output":` + output + `,"usage":{"input_tokens":20,"output_tokens":5}}}` + "\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			open := func(manager *openaiauth.Manager) *Runtime {
				r, err := Open(t.Context(), directory, subscriptionProvider(manager, transport), engineOptions(t))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := r.Close(); err != nil {
						t.Error(err)
					}
				})
				return r
			}
			r := open(auth)
			owner := createEngineSession(t, r, engine)
			if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "subscription", Name: "gpt-6-astra"}}); err != nil {
				t.Fatal(err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			checkTurn := func(r *Runtime, key, want string, replay bool) {
				t.Helper()
				mu.Lock()
				before := len(bodies)
				mu.Unlock()
				submitTest(t, r, owner.ID, key)
				admission := waitTestWithin(t, r, key, terminal, 30*time.Second)
				if admission.Turn.State != session.Succeeded {
					t.Fatalf("subscription turn=%s failure=%v", admission.Turn.State, admission.Turn.Failure)
				}
				attempts, err := r.ModelAttempts(t.Context(), admission.Turn.ID, "", 100)
				if err != nil || len(attempts) != 2 {
					t.Fatalf("attempts=%d err=%v", len(attempts), err)
				}
				for _, attempt := range attempts {
					if attempt.State != session.AttemptSucceeded || attempt.Request.Adapter != "openai-codex" || attempt.Request.MaxOutputTokens != 128000 || attempt.CostNanoUSD != nil {
						t.Fatal("subscription bypassed common truthful accounting")
					}
				}
				mu.Lock()
				requests := append([]string(nil), bodies[before:]...)
				mu.Unlock()
				if len(requests) != 2 || strings.Contains(requests[0], "opaque-subscription") != replay || !strings.Contains(requests[1], "opaque-subscription") || !strings.Contains(requests[1], "9007199254740993") {
					t.Fatal("private replay did not respect account/restart scope")
				}
				cell, err := r.store.LatestCell(t.Context(), owner.ID)
				if err != nil || cell == nil || cell.State != session.CellSucceeded || cell.Checkpoint == nil {
					t.Fatalf("cell=%v err=%v", cell, err)
				}
				history, err := r.History(t.Context(), owner.ID, 0, 100)
				if err != nil {
					t.Fatal(err)
				}
				var result struct{ Result struct{ Output string } }
				if err := json.Unmarshal([]byte(history[len(history)-2].Parts[0].Result.Output), &result); err != nil || result.Result.Output != want {
					t.Fatalf("checkpoint output=%q want=%q err=%v", result.Result.Output, want, err)
				}
				public, _ := json.Marshal(struct {
					History  []session.Message
					Attempts []session.ModelAttempt
				}{history, attempts})
				for _, private := range []string{"opaque-subscription", "private-access", "private-refresh", "encrypted_content"} {
					if strings.Contains(string(public), private) {
						t.Fatal("private provider state leaked into public projections")
					}
				}
			}
			checkTurn(r, "first", "41\n", false)
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			auth.Close()
			reopenedAuth := openaiauth.New(t.Context(), directory)
			t.Cleanup(reopenedAuth.Close)
			captured, err := reopenedAuth.Capture(t.Context())
			if err != nil || captured.Credentials.AccountID != "account" {
				t.Fatalf("saved host credential did not reopen: %v", err)
			}
			r = open(reopenedAuth)
			// A lost acknowledgement retry remains the original completed input;
			// the next request proves restart did not replay that execution.
			retry := submitTest(t, r, owner.ID, "first")
			if retry.Turn == nil || retry.Turn.State != session.Succeeded {
				t.Fatal("restart retry lost receipt")
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			checkTurn(r, "restart", "42\n", true)
			credentials.AccountID = "other-account"
			credentials.AccessToken = "replacement-access"
			if err := reopenedAuth.Install(t.Context(), reopenedAuth.Generation(), credentials); err != nil {
				t.Fatal(err)
			}
			expectedAccount.Store("other-account")
			checkTurn(r, "replacement", "43\n", false)
		})
	}
}

type subscriptionPreparedBarrier struct {
	provider model.OpenAI
	ready    chan struct{}
	release  chan struct{}
}

func (p subscriptionPreparedBarrier) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := p.provider.Prepare(ctx, request)
	if err != nil {
		return model.Prepared{}, err
	}
	close(p.ready)
	select {
	case <-p.release:
		return prepared, nil
	case <-ctx.Done():
		return model.Prepared{}, ctx.Err()
	}
}

func TestSubscriptionRuntimeRechecksCapturedAccountBeforeHTTP(t *testing.T) {
	for _, change := range []string{"logout", "same account", "different account", "cancel"} {
		t.Run(change, func(t *testing.T) {
			directory := t.TempDir()
			auth := openaiauth.New(t.Context(), directory)
			t.Cleanup(auth.Close)
			credentials := subscriptionCredentials()
			if err := auth.Install(t.Context(), auth.Generation(), credentials); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			barrier := subscriptionPreparedBarrier{provider: subscriptionProvider(auth, subscriptionTransport(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("unexpected HTTP") })), ready: make(chan struct{}), release: make(chan struct{})}
			r := openTest(t, directory, barrier)
			owner := createTest(t, r)
			if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "subscription", Name: "gpt-6-astra"}}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "blocked")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-barrier.ready:
			case <-time.After(5 * time.Second):
				t.Fatal("preparation did not reach barrier")
			}
			switch change {
			case "cancel":
				admission := waitTest(t, r, "blocked", func(a store.Admission) bool { return a.Turn != nil })
				if _, err := r.CancelTurn(t.Context(), admission.Turn.ID); err != nil {
					t.Fatal(err)
				}
			case "logout":
				if err := auth.Logout(); err != nil {
					t.Fatal(err)
				}
			default:
				if change == "different account" {
					credentials.AccountID = "other"
				}
				if err := auth.Install(t.Context(), auth.Generation(), credentials); err != nil {
					t.Fatal(err)
				}
			}
			close(barrier.release)
			finished := waitTest(t, r, "blocked", terminal)
			attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 0 {
				t.Fatal("stale/cancelled preparation contacted provider")
			}
			if change == "cancel" {
				if finished.Turn.State != session.Cancelled || len(attempts) != 0 {
					t.Fatal("cancelled preparation admitted work")
				}
			} else if finished.Turn.State != session.Failed || len(attempts) != 1 || attempts[0].State != session.AttemptCancelled || attempts[0].DispatchedAt != nil {
				t.Fatalf("invalid account outcome: %s attempts=%d", finished.Turn.State, len(attempts))
			}
		})
	}
}
