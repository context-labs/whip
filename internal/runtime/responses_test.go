package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestResponsesFailureSettlesAccountingWithoutExecutableOutput(t *testing.T) {
	for _, tc := range []struct{ name, output, state string }{
		{name: "malformed output", output: `"not-an-array"`, state: "completed"},
		{name: "incomplete response", output: `[]`, state: "incomplete"},
		{name: "oversized private output", output: `[{"type":"reasoning","encrypted_content":"` + strings.Repeat("x", session.MaxContinuationBytes) + `"},{"type":"function_call","call_id":"call","name":"execute","arguments":"{\"code\":\"print(42)\"}"}]`, state: "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.%s\",\"response\":{\"status\":\"%s\",\"output\":%s,\"usage\":{\"input_tokens\":17,\"output_tokens\":3,\"cost\":0.25}}}\n\n", tc.state, tc.state, tc.output)
			}))
			defer server.Close()
			provider := model.OpenAI{Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
				return model.Route{Kind: "openai-responses", URL: server.URL, MaxOutputTokens: 100, TimeoutMillis: 3000, MaxAttempts: 3}, nil
			}}
			r := openTest(t, t.TempDir(), provider)
			owner := createTest(t, r)
			submitTest(t, r, owner.ID, "responses-failure")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			finished := waitTest(t, r, "responses-failure", terminal)
			attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
			if err != nil || finished.Turn.State != session.Failed || len(attempts) != 1 || calls.Load() != 1 {
				t.Fatalf("failed output replayed: %+v attempts=%d error=%v", finished.Turn, len(attempts), err)
			}
			attempt := attempts[0]
			if attempt.State != session.AttemptUncertain || attempt.MessageID != nil || attempt.Result.Usage.Input == nil || *attempt.Result.Usage.Input != 17 || attempt.CostNanoUSD == nil || *attempt.CostNanoUSD != 250000000 {
				t.Fatalf("accounting lost: %+v", attempt)
			}
			history, err := r.History(t.Context(), owner.ID, 0, 100)
			if err != nil || len(history) != 1 {
				t.Fatal("unusable provider output entered history", err)
			}
			cells, err := r.Cells(t.Context(), finished.Turn.ID, "", 100)
			if err != nil || len(cells) != 0 {
				t.Fatal("unusable provider output dispatched a cell", err)
			}
		})
	}
}

func TestResponsesHTTPFallbackSettlesPartialKnownUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":17,"output_tokens":"invalid","cost":0.25}}`)
	}))
	defer server.Close()
	provider := model.OpenAI{Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
		return model.Route{Kind: "openai-responses", URL: server.URL, MaxOutputTokens: 100, TimeoutMillis: 3000, MaxAttempts: 1}, nil
	}}
	r := openTest(t, t.TempDir(), provider)
	owner := createTest(t, r)
	submitTest(t, r, owner.ID, "responses-partial-usage")
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	finished := waitTest(t, r, "responses-partial-usage", terminal)
	attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || finished.Turn.State != session.Succeeded || len(attempts) != 1 {
		t.Fatalf("partial usage discarded output: %+v %v", finished.Turn, err)
	}
	result := attempts[0].Result
	if result.Usage.Input == nil || *result.Usage.Input != 17 || result.Usage.Output != nil || result.UsageNote == nil || attempts[0].CostNanoUSD == nil || *attempts[0].CostNanoUSD != 250000000 {
		t.Fatal("partial accounting was not retained")
	}
}
