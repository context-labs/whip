package runtime

import (
	"context"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type prefillProvider struct {
	prepares               atomic.Int64
	execute                func(context.Context, model.Request) (model.Response, error)
	refresh                bool
	changedRefreshCapacity bool
}

func (p *prefillProvider) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	p.prepares.Add(1)
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	if err != nil {
		return prepared, err
	}
	prepared.ContextWindowTokens = new(int64(10000))
	prepared.Execute = func(ctx context.Context, _ func(model.Chunk)) (model.Response, error) { return p.execute(ctx, request) }
	if p.refresh {
		prepared.MaxAttempts = 2
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			return model.Response{}, &model.CallError{StatusCode: 401, AuthRejected: true, Message: "test unauthorized"}
		}
		prepared.RefreshCredentials = func(ctx context.Context) (model.Prepared, error) {
			fresh, err := (model.Scripted{}).Prepare(ctx, request)
			fresh.ContextWindowTokens = new(int64(10000))
			if p.changedRefreshCapacity {
				fresh.ContextWindowTokens = nil
			}
			fresh.Execute = func(ctx context.Context, _ func(model.Chunk)) (model.Response, error) { return p.execute(ctx, request) }
			return fresh, err
		}
	}
	return prepared, nil
}

func TestBothEnginesContextUsageCapturesExactPrefillAndReadsAcrossRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := "print(42)"
			if engine == session.QuickJS {
				code = "console.log(42)"
			}
			complete := cellProvider(map[string]string{"work": code})
			var runtime *Runtime
			provider := &prefillProvider{execute: func(ctx context.Context, request model.Request) (model.Response, error) {
				value, err := runtime.ContextUsage(ctx, request.SessionID)
				if err != nil || value.Prefill == nil || value.Prefill.InputSource != "estimated" || value.Prefill.InputTokens != model.EstimateInputTokens(request) || value.Prefill.ContextWindowTokens == nil || *value.Prefill.ContextWindowTokens != 10000 || value.Prefill.Stale {
					t.Error("capture was not committed before dispatch", value, err)
				}
				result, err := complete(ctx, request)
				result.Usage.Input = new(model.EstimateInputTokens(request) + 5)
				return result, err
			}}
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			var err error
			runtime, err = Open(t.Context(), directory, provider, engineOptions(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Close() })
			root := createEngineSession(t, runtime, engine)
			before, err := runtime.ContextUsage(t.Context(), root.ID)
			if err != nil || before.Prefill != nil || provider.prepares.Load() != 0 {
				t.Fatal(before, err)
			}
			if err = runtime.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, runtime, root.ID, "work", "42\n")
			current, err := runtime.ContextUsage(t.Context(), root.ID)
			if err != nil || current.Prefill == nil || !current.Prefill.Stale || current.Prefill.InputSource != "reported" {
				t.Fatal(current, err)
			}
			for range 5 {
				if got, err := runtime.ContextUsage(t.Context(), root.ID); err != nil || !reflect.DeepEqual(got, current) {
					t.Fatal(got, err)
				}
			}
			if provider.prepares.Load() != 2 {
				t.Fatal("observation prepared a model", provider.prepares.Load())
			}
			if err = runtime.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openTest(t, directory, provider)
			if after, err := reopened.ContextUsage(t.Context(), root.ID); err != nil || !reflect.DeepEqual(after, current) || provider.prepares.Load() != 2 {
				t.Fatal("prefill lost or model prepared during read", after, err)
			}
		})
	}
}

func TestContextUsageCredentialRefreshKeepsFrozenEvidence(t *testing.T) {
	provider := &prefillProvider{refresh: true, execute: func(context.Context, model.Request) (model.Response, error) {
		return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
	}}
	r := openTest(t, t.TempDir(), provider)
	root := createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "refresh")
	done := waitTest(t, r, "refresh", terminal)
	if done.Turn.State != session.Succeeded {
		t.Fatal(done.Turn)
	}
	attempts, err := r.ModelAttempts(t.Context(), done.Turn.ID, "", 100)
	if err != nil || len(attempts) != 2 || attempts[0].Request.Context == nil || !reflect.DeepEqual(attempts[0].Request.Context, attempts[1].Request.Context) {
		t.Fatal(attempts, err)
	}
	value, err := r.ContextUsage(t.Context(), root.ID)
	if err != nil || value.Prefill == nil || value.Prefill.AttemptID != attempts[1].ID || value.Prefill.InputSource != "estimated" || provider.prepares.Load() != 1 {
		t.Fatal(value, err)
	}
}

func TestContextUsageCredentialRefreshRejectsChangedCapacityBeforeDispatch(t *testing.T) {
	provider := &prefillProvider{refresh: true, changedRefreshCapacity: true, execute: func(context.Context, model.Request) (model.Response, error) {
		t.Error("changed preparation was dispatched")
		return model.Response{}, nil
	}}
	r := openTest(t, t.TempDir(), provider)
	root := createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "changed_refresh")
	done := waitTest(t, r, "changed_refresh", terminal)
	if done.Turn.State != session.Failed {
		t.Fatal(done.Turn)
	}
	attempts, err := r.ModelAttempts(t.Context(), done.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 || attempts[0].Request.Context == nil || *attempts[0].Request.Context.ContextWindowTokens != 10000 {
		t.Fatal(attempts, err)
	}
}
