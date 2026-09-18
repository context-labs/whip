package tools

import (
	"context"
	"strings"
	"testing"
)

func TestQuestionTool(t *testing.T) {
	old := Ask
	t.Cleanup(func() { Ask = old })
	tool := QuestionTool()

	if _, err := tool.Run(context.Background(), []byte(`{`)); err == nil {
		t.Fatal("invalid arguments should fail")
	}

	Ask = nil
	if _, err := tool.Run(context.Background(), []byte(`{"question":"Pick","options":[{"label":"A"},{"label":"B"}]}`)); err == nil || !strings.Contains(err.Error(), "no interactive user") {
		t.Fatalf("missing UI error = %v", err)
	}

	Ask = func(context.Context, AskRequest) ([]string, bool) { return nil, false }
	if _, err := tool.Run(context.Background(), []byte(`{"question":"Pick","options":[{"label":"A"},{"label":"B"}]}`)); err == nil || !strings.Contains(err.Error(), "dismissed") {
		t.Fatalf("dismissed error = %v", err)
	}

	Ask = func(_ context.Context, req AskRequest) ([]string, bool) {
		if req.Question != "Pick" || !req.Multiple || len(req.Options) != 2 {
			t.Fatalf("decoded request = %+v", req)
		}
		return []string{"A", "B"}, true
	}
	got, err := tool.Run(context.Background(), []byte(`{"question":"Pick","multiple":true,"options":[{"label":"A"},{"label":"B"}]}`))
	if err != nil || got != `User answered "Pick": A, B` {
		t.Fatalf("answer = %q, %v", got, err)
	}
}
