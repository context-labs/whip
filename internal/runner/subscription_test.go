package runner

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type subscriptionTransport func(*http.Request) (*http.Response, error)

func (f subscriptionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type runnerSubscriptionAuth struct {
	captured  openaiauth.CapturedCredentials
	refreshes int
	refresh   func() error
}

func (a *runnerSubscriptionAuth) Capture(ctx context.Context) (openaiauth.CapturedCredentials, error) {
	return a.captured, ctx.Err()
}

func (a *runnerSubscriptionAuth) Check(ctx context.Context, c openaiauth.CapturedCredentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Generation != a.captured.Generation || c.Credentials.AccountID != a.captured.Credentials.AccountID {
		return openaiauth.ErrLoginChanged
	}
	return nil
}

func (a *runnerSubscriptionAuth) RefreshCaptured(ctx context.Context, c openaiauth.CapturedCredentials) (openaiauth.CapturedCredentials, error) {
	if err := a.Check(ctx, c); err != nil {
		return openaiauth.CapturedCredentials{}, err
	}
	a.refreshes++
	if a.refresh != nil {
		if err := a.refresh(); err != nil {
			return openaiauth.CapturedCredentials{}, err
		}
	}
	a.captured.Credentials.AccessToken = "rotated"
	return a.captured, nil
}

type subscriptionLedger struct {
	*store.Store
	afterReserve  func()
	afterDispatch func()
	settle        func(session.ModelAttemptResult) error
}

func (s *subscriptionLedger) ReserveModelAttempt(ctx context.Context, spec session.ModelAttemptSpec) (session.ModelAttempt, error) {
	attempt, err := s.Store.ReserveModelAttempt(ctx, spec)
	if err == nil && s.afterReserve != nil {
		s.afterReserve()
	}
	return attempt, err
}

func (s *subscriptionLedger) DispatchModelAttempt(ctx context.Context, id session.ModelAttemptID) (bool, error) {
	allowed, err := s.Store.DispatchModelAttempt(ctx, id)
	if err == nil && allowed && s.afterDispatch != nil {
		s.afterDispatch()
	}
	return allowed, err
}

func (s *subscriptionLedger) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	if s.settle != nil {
		if err := s.settle(result); err != nil {
			return session.ModelAttempt{}, err
		}
	}
	return s.Store.SettleModelAttempt(ctx, id, result, message)
}

func subscriptionRunnerFixture(t *testing.T) (*subscriptionLedger, session.Turn, model.Request, model.OpenAI, *runnerSubscriptionAuth) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	selection := session.ModelSelection{Provider: "openai-codex", Name: "gpt-6-astra"}
	_, owner, err := db.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: ref, WorkingDirectory: t.TempDir(), Defaults: session.Configuration{Model: selection}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "input"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "question"}}})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := db.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := model.Request{SessionID: owner.ID, TurnID: claim.Turn.ID, Selection: selection, Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "question"}}}}}
	auth := &runnerSubscriptionAuth{captured: openaiauth.CapturedCredentials{Generation: 1, Credentials: openaiauth.Credentials{AccessToken: "access", AccountID: "account"}}}
	provider := model.OpenAI{Auth: auth, Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
		return model.Route{Kind: "openai-codex", MaxAttempts: 3, TimeoutMillis: 1000, ContextWindowTokens: new(int64(400000))}, nil
	}}
	return &subscriptionLedger{Store: db}, claim.Turn, request, provider, auth
}

func subscriptionHTTP(status int) *http.Response {
	body := `{"error":{"message":"private provider failure"},"usage":{"input_tokens":7,"output_tokens":0}}`
	if status == http.StatusOK {
		body = `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":9,"output_tokens":2}}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestSubscriptionRefreshUsesSettledAttempts(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		statuses                                        []int
		refreshErr, settleErr, cancelRefresh, uncertain bool
		max                                             int
		requests, refreshes                             int
	}{
		{name: "refresh then success", statuses: []int{401, 200}, requests: 2, refreshes: 1},
		{name: "second unauthorized", statuses: []int{401, 401, 200}, requests: 2, refreshes: 1},
		{name: "refresh failure", statuses: []int{401, 200}, refreshErr: true, requests: 1, refreshes: 1},
		{name: "cancel refresh", statuses: []int{401, 200}, cancelRefresh: true, requests: 1, refreshes: 1},
		{name: "settlement failure", statuses: []int{401, 200}, settleErr: true, requests: 1},
		{name: "uncertain transport", uncertain: true, requests: 1},
		{name: "attempt limit", statuses: []int{401, 200}, max: 1, requests: 1},
		{name: "ordinary retry then refresh", statuses: []int{500, 401, 200}, requests: 3, refreshes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger, turn, request, provider, auth := subscriptionRunnerFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var bodies []string
			provider.Client = &http.Client{Transport: subscriptionTransport(func(r *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(r.Body)
				bodies = append(bodies, string(raw))
				if tc.uncertain {
					return nil, errors.New("private transport detail")
				}
				if len(bodies) > len(tc.statuses) {
					t.Fatal("unexpected hidden provider request")
				}
				return subscriptionHTTP(tc.statuses[len(bodies)-1]), nil
			})}
			auth.refresh = func() error {
				attempts, err := ledger.ModelAttempts(t.Context(), turn.ID, "", 100)
				if err != nil {
					t.Fatal(err)
				}
				if len(attempts) != len(bodies) {
					t.Fatal("refresh began before rejected request was recorded")
				}
				for _, attempt := range attempts {
					if attempt.State != session.AttemptFailed || attempt.Result == nil {
						t.Fatal("refresh began before rejection settled")
					}
				}
				if tc.cancelRefresh {
					cancel()
				}
				if tc.refreshErr {
					return errors.New("refresh failed safely")
				}
				return nil
			}
			if tc.settleErr {
				ledger.settle = func(session.ModelAttemptResult) error { return session.ErrInvalid }
			}
			prepared, err := provider.Prepare(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if tc.max != 0 {
				prepared.MaxAttempts = tc.max
			}
			runner := Runner{attempts: ledger}
			outcome, err := runner.completePrepared(ctx, turn, prepared, "logical", nil)
			if tc.settleErr && !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("settlement error lost: %v", err)
			}
			if tc.cancelRefresh && !errors.Is(err, context.Canceled) && !errors.Is(outcome.failure, context.Canceled) {
				t.Fatalf("cancellation lost: %v %v", err, outcome.failure)
			}
			if len(bodies) != tc.requests || auth.refreshes != tc.refreshes {
				t.Fatalf("requests=%d refreshes=%d err=%v outcome=%v", len(bodies), auth.refreshes, err, outcome.failure)
			}
			attempts, listErr := ledger.ModelAttempts(t.Context(), turn.ID, "", 100)
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(attempts) != tc.requests {
				t.Fatalf("attempt count %d != HTTP count %d", len(attempts), tc.requests)
			}
			for i, attempt := range attempts {
				if !reflect.DeepEqual(attempt.Request, prepared.Snapshot) || attempt.Number != i+1 || bodies[i] != bodies[0] {
					t.Fatal("retry changed frozen request or reused attempt identity")
				}
				if !tc.settleErr && (attempt.Result == nil || attempt.FinishedAt == nil) {
					t.Fatal("attempt did not settle")
				}
				if attempt.CostNanoUSD != nil {
					t.Fatal("unknown subscription price became free/API price")
				}
			}
			if tc.requests > 1 && tc.statuses[tc.requests-1] == 200 && (err != nil || outcome.failure != nil || len(outcome.parts) != 1) {
				t.Fatalf("successful retry: %v %v", err, outcome.failure)
			}
		})
	}
}

func TestSubscriptionAuthorityChangesAcrossReservationAndDispatch(t *testing.T) {
	for _, point := range []string{"reserved", "dispatched", "reserved settlement failure"} {
		for _, change := range []string{"logout", "same account", "different account"} {
			t.Run(point+"/"+change, func(t *testing.T) {
				ledger, turn, request, provider, _ := subscriptionRunnerFixture(t)
				manager := openaiauth.New(t.Context(), t.TempDir())
				t.Cleanup(manager.Close)
				credentials := openaiauth.Credentials{AccessToken: "access", RefreshToken: "refresh", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour)}
				if err := manager.Install(t.Context(), manager.Generation(), credentials); err != nil {
					t.Fatal(err)
				}
				provider.Auth = manager
				calls := 0
				provider.Client = &http.Client{Transport: subscriptionTransport(func(*http.Request) (*http.Response, error) { calls++; return subscriptionHTTP(200), nil })}
				prepared, err := provider.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				changeLogin := func() {
					var err error
					if change == "logout" {
						err = manager.Logout()
					} else {
						if change == "different account" {
							credentials.AccountID = "other"
						}
						err = manager.Install(t.Context(), manager.Generation(), credentials)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if point == "dispatched" {
					ledger.afterDispatch = changeLogin
				} else {
					ledger.afterReserve = changeLogin
				}
				if point == "reserved settlement failure" {
					ledger.settle = func(session.ModelAttemptResult) error { return session.ErrInvalid }
				}
				runner := Runner{attempts: ledger}
				outcome, err := runner.completePrepared(t.Context(), turn, prepared, "logical", nil)
				if point == "reserved settlement failure" {
					if !errors.Is(err, session.ErrInvalid) {
						t.Fatalf("settlement failure: %v", err)
					}
				} else if err != nil || !errors.Is(outcome.failure, openaiauth.ErrLoginChanged) {
					t.Fatalf("authority result: %v %v", err, outcome.failure)
				}
				attempts, err := ledger.ModelAttempts(t.Context(), turn.ID, "", 100)
				if err != nil {
					t.Fatal(err)
				}
				if len(attempts) != 1 || calls != 0 {
					t.Fatalf("stale work was replayed/contacted provider: attempts=%d calls=%d", len(attempts), calls)
				}
				a := attempts[0]
				switch point {
				case "reserved":
					if a.State != session.AttemptCancelled || a.DispatchedAt != nil || a.CostNanoUSD == nil || *a.CostNanoUSD != 0 {
						t.Fatal("undispatched authority failure did not release reservation")
					}
				case "dispatched":
					if a.State != session.AttemptFailed || a.DispatchedAt == nil || a.CostNanoUSD != nil || a.Result.Usage.Input != nil || a.Result.Usage.Output != nil {
						t.Fatal("late refusal erased conservative dispatch evidence")
					}
				default:
					if a.State != session.AttemptReserved {
						t.Fatal("failed settlement invented terminal evidence")
					}
				}
				budgets, err := ledger.Budgets(t.Context(), turn.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				for _, b := range budgets {
					if b.Kind == session.BudgetModelCalls {
						if point == "reserved" && (b.Used != 0 || b.Reserved != 0 || b.Uncertain != 0) {
							t.Fatal("undispatched call remained charged")
						}
						if point == "dispatched" && b.Used != 1 {
							t.Fatal("late refusal erased ledger dispatch")
						}
					}
				}
			})
		}
	}
}

func TestSubscriptionFiniteBudgetRefusalBeforeHTTP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    session.BudgetKind
		limit   int64
		purpose string
	}{
		{"unknown spend", session.BudgetModelCostNanoUSD, 1, "turn"},
		{"natural output", session.BudgetModelTokens, 527999, "turn"},
		{"helper natural output", session.BudgetModelTokens, 527999, "compaction"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger, turn, request, provider, _ := subscriptionRunnerFixture(t)
			if _, err := ledger.SetBudget(t.Context(), turn.SessionID, 0, session.BudgetLimit{Kind: tc.kind, Limit: &tc.limit}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			provider.Client = &http.Client{Transport: subscriptionTransport(func(*http.Request) (*http.Response, error) { calls++; return subscriptionHTTP(200), nil })}
			request.Purpose = tc.purpose
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			runner := Runner{attempts: ledger}
			_, err = runner.completePrepared(t.Context(), turn, prepared, "logical", nil)
			if !errors.Is(err, store.ErrLimit) || calls != 0 {
				t.Fatalf("finite budget did not refuse unknown/full exposure: %v calls=%d", err, calls)
			}
			attempts, err := ledger.ModelAttempts(t.Context(), turn.ID, "", 100)
			if err != nil || len(attempts) != 0 {
				t.Fatalf("refused reservation left evidence: %v count=%d", err, len(attempts))
			}
		})
	}
}
