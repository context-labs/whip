package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestAncestorModelCallBudgetDenialLeavesRuntimeAvailable(t *testing.T) {
	var requests atomic.Int32
	provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		requests.Add(1)
		return (model.Scripted{}).Complete(ctx, request)
	})
	r := openTest(t, t.TempDir(), provider)
	root := createTest(t, r)
	_, err := r.SetBudget(t.Context(), root.ID, 0, session.BudgetLimit{Kind: session.BudgetModelCalls, Limit: new(int64(1))})
	if err != nil {
		t.Fatal(err)
	}
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "budget_child"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "child consumes allowance"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if result := waitTest(t, r, "budget_child", terminal); result.Turn.State != session.Succeeded {
		t.Fatalf("child outcome: %+v", result.Turn)
	}
	if err := r.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "budget_exhausted")
	blocked := waitTest(t, r, "budget_exhausted", terminal)
	if blocked.Turn.State != session.Failed || requests.Load() != 1 || r.Err() != nil {
		t.Fatalf("budget admission did not isolate denial: %+v requests=%d runtime=%v", blocked, requests.Load(), r.Err())
	}
	attempts, err := r.ModelAttempts(t.Context(), blocked.Turn.ID, "", 100)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("denied attempt dispatched: %+v %v", attempts, err)
	}
	other := createTest(t, r)
	submitTest(t, r, other.ID, "budget_unrelated")
	if result := waitTest(t, r, "budget_unrelated", terminal); result.Turn.State != session.Succeeded || requests.Load() != 2 {
		t.Fatalf("unrelated tree lost execution: %+v requests=%d", result.Turn, requests.Load())
	}
}
