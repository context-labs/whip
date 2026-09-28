package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func modelHelperFixture(t *testing.T, provider runner.Provider) (*Runtime, session.Session, session.Cell) {
	t.Helper()
	r := openTest(t, t.TempDir(), provider)
	owner := createTest(t, r)
	owner, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "captured", Effort: "high", Temperature: new(0.0), TopP: new(0.6)}})
	if err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "helper")
	claimed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := r.store.AppendMessage(t.Context(), claimed.Turn.ID, session.MessageDraft{ID: "helper_message", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := r.store.BeginCell(t.Context(), session.CellSpec{ID: "helper_cell", TurnID: claimed.Turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil || !dispatch {
		t.Fatal(dispatch, err)
	}
	return r, owner, cell
}

func modelHelperGrant(t *testing.T, r *Runtime, owner session.Session, capability string) {
	t.Helper()
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(strings.ReplaceAll(capability, ".", "_")), SessionID: owner.ID, Capability: capability, Resource: string(owner.TreeID)}); err != nil {
		t.Fatal(err)
	}
}

func modelInvocation(owner session.Session, cell session.Cell, id, name string, args map[string]any) tool.Invocation {
	return tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: id, Module: "models", Name: name, Arguments: args}
}

func TestModelHelperCapturedSelectionAndStrictArguments(t *testing.T) {
	captured := make(chan model.Request, 1)
	r, owner, cell := modelHelperFixture(t, providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		captured <- request
		return model.Response{Parts: []session.Part{{Type: "text", Text: "answer"}}}, nil
	}))
	current, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "changed", Temperature: new(1.0)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"call", map[string]any{}},
		{"call", map[string]any{"prompt": ""}},
		{"call", map[string]any{"prompt": "\xff"}},
		{"call", map[string]any{"prompt": "ok", "model": "override"}},
		{"call", map[string]any{"prompt": "ok", "max_tokens": nil}},
		{"call", map[string]any{"prompt": "ok", "max_tokens": 0}},
		{"call", map[string]any{"prompt": "ok", "max_tokens": 1000001}},
		{"call", map[string]any{"prompt": "ok", "max_tokens": 1.5}},
		{"call", map[string]any{"prompt": strings.Repeat("a", session.MaxDocumentBytes)}},
		{"batch", map[string]any{"prompts": []string{}}},
		{"batch", map[string]any{"prompts": make([]string, 33)}},
		{"batch", map[string]any{"prompts": []any{"ok", "\xff"}}},
		{"batch", map[string]any{"prompts": []string{"\xff"}}},
		{"batch", map[string]any{"prompts": []any{"ok", nil}}},
		{"batch", map[string]any{"prompts": []string{strings.Repeat("x", 600000), strings.Repeat("x", 600000)}}},
		{"unknown", map[string]any{"prompt": "ok"}},
	} {
		if _, err := r.prepareModel(t.Context(), current, modelInvocation(current, cell, "invalid", tc.name, tc.args)); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("accepted %s arguments: %v", tc.name, err)
		}
	}
	other := createTest(t, r)
	if _, err := r.prepareModel(t.Context(), other, modelInvocation(other, cell, "foreign", "call", map[string]any{"prompt": "ok"})); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("foreign cell accepted", err)
	}
	modelHelperGrant(t, r, owner, "models.call")
	call := modelInvocation(owner, cell, "captured", "call", map[string]any{"prompt": "only this prompt", "max_tokens": 128})
	prepared, err := r.prepareModel(t.Context(), current, call)
	if err != nil || !prepared.ModelTimeouts || prepared.Apply != nil || prepared.Mutating || prepared.Resource != string(owner.TreeID) {
		t.Fatal(prepared, err)
	}
	value, operation, err := r.tools.Call(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	item := value.(modelHelperItem)
	if item.AttemptID == nil || item.Text != "answer" || item.Failure != nil || item.Truncated || item.ContentRef != nil || item.Bytes != 6 {
		t.Fatal(item)
	}
	request := <-captured
	if !request.Selection.Equal(owner.Config.Model) || request.Purpose != session.ModelHelperPurpose || request.Instructions != "" || len(request.Tools) != 0 || len(request.Messages) != 1 || len(request.Contents) != 0 || request.Messages[0].Parts[0].Text != "only this prompt" || request.OutputTokenLimit == nil || *request.OutputTokenLimit != 128 {
		t.Fatalf("helper did not isolate captured request: %+v", request)
	}
	attempt, err := r.store.ModelAttempt(t.Context(), *item.AttemptID)
	if err != nil || attempt.OperationID == nil || *attempt.OperationID != operation || attempt.BatchIndex == nil || *attempt.BatchIndex != 0 || attempt.MessageID != nil || attempt.Request.MaxOutputTokens != 128 {
		t.Fatal(attempt, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 2 {
		t.Fatal("helper appended transcript", history, err)
	}
}

func TestModelHelperAdmissionRefusalRejectsMixedErrors(t *testing.T) {
	for _, err := range []error{store.ErrLimit, fmt.Errorf("limit: %w", store.ErrLimit), errors.Join(store.ErrStopped, fmt.Errorf("invalid: %w", session.ErrInvalid))} {
		if !modelAdmissionRefused(err) {
			t.Fatal("semantic refusal became fatal", err)
		}
	}
	for _, err := range []error{nil, context.Canceled, errors.New("unknown"), errors.Join(store.ErrLimit, errors.New("rollback failed")), fmt.Errorf("wrapped: %w", errors.Join(session.ErrInvalid, context.Canceled))} {
		if modelAdmissionRefused(err) {
			t.Fatal("unknown leaf accepted", err)
		}
	}
}

func TestModelHelperBudgetRefusalIsPositional(t *testing.T) {
	var calls atomic.Int32
	r, owner, cell := modelHelperFixture(t, providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls.Add(1)
		return model.Response{}, nil
	}))
	modelHelperGrant(t, r, owner, "models.batch")
	if _, err := r.SetBudget(t.Context(), owner.ID, 0, session.BudgetLimit{Kind: session.BudgetModelCalls, Limit: new(int64(0))}); err != nil {
		t.Fatal(err)
	}
	value, operation, err := r.tools.Call(t.Context(), modelInvocation(owner, cell, "refused", "batch", map[string]any{"prompts": []string{"one", "two"}}))
	if err != nil {
		t.Fatal(err)
	}
	items := value.([]modelHelperItem)
	if len(items) != 2 || calls.Load() != 0 {
		t.Fatal(items, calls.Load())
	}
	for _, item := range items {
		if item.AttemptID != nil || item.Failure == nil || item.Text != "" {
			t.Fatal(item)
		}
	}
	recorded, err := r.Operation(t.Context(), operation)
	if err != nil || recorded.State != session.OperationSucceeded {
		t.Fatal(recorded, err)
	}
}

func modelHelperCellOutput(t *testing.T, r *Runtime, owner session.Session, key string) (store.Admission, string) {
	t.Helper()
	result := waitTestWithin(t, r, key, terminal, 30*time.Second)
	if result.Turn.State != session.Succeeded {
		t.Fatalf("turn failed: %+v runtime=%v", result.Turn, r.Err())
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Result process.Result `json:"result"`
	}
	if err := json.Unmarshal([]byte(history[len(history)-1].Parts[0].Text), &output); err != nil {
		t.Fatal(err)
	}
	return result, output.Result.Output
}

func TestBothEnginesModelHelpersOrderedResultsAndFailures(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `a=models.call(prompt="single",max_tokens=128)
b=models.batch(prompts=["slow","fast","reject"])
print(a["text"])
print(b[0]["text"])
print(b[1]["text"])
print(len(b))`
			if engine == session.QuickJS {
				code = `const a=await models.call({prompt:"single",max_tokens:128});const b=await models.batch({prompts:["slow","fast","reject"]});console.log(a.text);console.log(b[0].text);console.log(b[1].text);console.log(b.length);`
			}
			ordinary := cellProvider(map[string]string{"ordered": code})
			fast := make(chan struct{})
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Purpose != session.ModelHelperPurpose {
					return ordinary(ctx, request)
				}
				prompt := request.Messages[0].Parts[0].Text
				if prompt == "slow" {
					select {
					case <-fast:
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					}
				}
				if prompt == "fast" {
					close(fast)
				}
				if prompt == "reject" {
					return model.Response{ReportedCostNanoUSD: new(int64(3))}, &model.CallError{Message: strings.Repeat("provider rejected 🦊", 200)}
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: prompt}}, ReportedCostNanoUSD: new(int64(2))}, nil
			})
			r := openEngineTest(t, t.TempDir(), provider)
			owner := createEngineSession(t, r, engine)
			modelHelperGrant(t, r, owner, "models.batch")
			submitTest(t, r, owner.ID, "ordered")
			waiting := awaitRuntimeFilePermission(t, r, owner.ID, "ordered", "models.call")
			before, err := r.ModelAttempts(t.Context(), waiting.TurnID, "", 100)
			if err != nil || len(before) != 1 || before[0].Request.Purpose == session.ModelHelperPurpose {
				t.Fatal("helper dispatched before explicit permission", before, err)
			}
			if _, err := r.ResolvePermission(t.Context(), waiting.ID, true); err != nil {
				t.Fatal(err)
			}
			admission, output := modelHelperCellOutput(t, r, owner, "ordered")
			if output != "single\nslow\nfast\n3\n" {
				t.Fatal(output)
			}
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(operations) != 2 {
				t.Fatal(operations, err)
			}
			for _, op := range operations {
				if op.State != session.OperationSucceeded {
					t.Fatal(op)
				}
				if op.Capability == "models.batch" {
					var items []modelHelperItem
					if err := json.Unmarshal(op.Result.Value, &items); err != nil {
						t.Fatal(err)
					}
					if len(items) != 3 || items[0].Text != "slow" || items[1].Text != "fast" || items[2].Failure == nil || len(*items[2].Failure) > 1024 || !utf8.ValidString(*items[2].Failure) || items[2].AttemptID == nil {
						t.Fatal(items)
					}
				}
			}
			attempts, err := r.ModelAttempts(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(attempts) != 6 {
				t.Fatal(attempts, err)
			}
			for _, attempt := range attempts {
				if attempt.Request.Purpose == session.ModelHelperPurpose && (attempt.OperationID == nil || attempt.MessageID != nil || attempt.FinishedAt == nil) {
					t.Fatal(attempt)
				}
			}
		})
	}
}

func TestBothEnginesModelHelpersLargeBatchEvidenceRestartAndDeletion(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			text := strings.Repeat("\x00<>&🦊", 1800)
			code := `items=models.batch(prompts=["large"]*32)
page=artifacts.read(id=items[0]["content_ref"],offset="0",length=65536)
print(len(items))
print(page["total_bytes"])`
			if engine == session.QuickJS {
				code = `var items=await models.batch({prompts:Array(32).fill("large")});var page=await artifacts.read({id:items[0].content_ref,offset:"0",length:65536});console.log(items.length);console.log(page.total_bytes);`
			}
			ordinary := cellProvider(map[string]string{"large": code})
			var helperCalls atomic.Int32
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Purpose != session.ModelHelperPurpose {
					return ordinary(ctx, request)
				}
				helperCalls.Add(1)
				return model.Response{Parts: []session.Part{{Type: "text", Text: text}}, ReportedCostNanoUSD: new(int64(7))}, nil
			})
			directory := t.TempDir()
			r := openEngineTest(t, directory, provider)
			owner := createEngineSession(t, r, engine)
			modelHelperGrant(t, r, owner, "models.batch")
			modelHelperGrant(t, r, owner, "artifacts.read")
			submitTest(t, r, owner.ID, "large")
			admission, output := modelHelperCellOutput(t, r, owner, "large")
			if output != fmt.Sprintf("32\n%d\n", len(text)) {
				t.Fatal(output)
			}
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(operations) != 2 {
				t.Fatal(operations, err)
			}
			var items []modelHelperItem
			for _, op := range operations {
				if op.Capability == "models.batch" {
					if len(op.Result.Value) >= 512*1024 {
						t.Fatal("aggregate exceeds dispatcher bound")
					}
					if err := json.Unmarshal(op.Result.Value, &items); err != nil {
						t.Fatal(err)
					}
				}
			}
			if len(items) != 32 || helperCalls.Load() != 32 {
				t.Fatal(len(items), helperCalls.Load())
			}
			for _, item := range items {
				raw, err := json.Marshal(item)
				if err != nil || len(raw) > 15*1024 || !item.Truncated || item.ContentRef == nil || item.AttemptID == nil || item.Failure != nil || item.Bytes != int64(len(text)) || !utf8.ValidString(item.Text) || !strings.HasPrefix(text, strings.Split(item.Text, "\n…\n")[0]) {
					t.Fatal(item, err)
				}
			}
			reference := *items[0].ContentRef
			stranger := createEngineSession(t, r, engine)
			if _, _, err := r.ReadContentRange(t.Context(), stranger.ID, reference, 0, 65536); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("foreign helper output readable", err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openTest(t, directory, provider)
			metadata, body, err := reopened.ReadContentRange(t.Context(), owner.ID, reference, 0, 65536)
			if err != nil || string(body) != text || metadata.MediaType != "text/plain" || helperCalls.Load() != 32 {
				t.Fatal(metadata, len(body), err)
			}
			if err := reopened.DeleteSubtree(t.Context(), owner.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := reopened.ReadContentRange(t.Context(), owner.ID, reference, 0, 65536); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("deleted helper output readable", err)
			}
		})
	}
}

func TestModelHelperPublicationFailureKeepsAccounting(t *testing.T) {
	for _, failure := range []string{"blob", "registration"} {
		t.Run(failure, func(t *testing.T) {
			var calls atomic.Int32
			r, owner, cell := modelHelperFixture(t, providerFunc(func(context.Context, model.Request) (model.Response, error) {
				calls.Add(1)
				return model.Response{Parts: []session.Part{{Type: "text", Text: strings.Repeat("large 🦊", 2000)}}, ReportedCostNanoUSD: new(int64(9))}, nil
			}))
			modelHelperGrant(t, r, owner, "models.call")
			if failure == "blob" {
				path := filepath.Join(r.directory, "artifacts", "sha256")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", filepath.Join(r.directory, "state.db")+"?_pragma=busy_timeout(5000)")
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				}()
				if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER fail_helper_publication BEFORE INSERT ON content_references BEGIN SELECT RAISE(ABORT,'injected registration failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			value, operation, err := r.tools.Call(t.Context(), modelInvocation(owner, cell, "publication", "call", map[string]any{"prompt": "large"}))
			if err != nil {
				t.Fatal(err)
			}
			item := value.(modelHelperItem)
			if item.AttemptID == nil || item.Failure == nil || !strings.Contains(*item.Failure, "output unavailable") || item.ContentRef != nil || !item.Truncated || len(item.Text) > 4096 || calls.Load() != 1 {
				t.Fatal(item, calls.Load())
			}
			attempt, err := r.store.ModelAttempt(t.Context(), *item.AttemptID)
			if err != nil || attempt.State != session.AttemptSucceeded || attempt.CostNanoUSD == nil || *attempt.CostNanoUSD != 9 {
				t.Fatal(attempt, err)
			}
			op, err := r.Operation(t.Context(), operation)
			if err != nil || op.State != session.OperationSucceeded {
				t.Fatal(op, err)
			}
		})
	}
}

func TestModelHelperEncodedBoundsAndExhaustedWriteAllowance(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		retained   bool
	}{
		{"inline_boundary", strings.Repeat("x", 8192), false},
		{"escaped_below_raw_bound", strings.Repeat("<", 4096), true},
		{"unicode_above_raw_bound", strings.Repeat("🦊", 2049), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, owner, cell := modelHelperFixture(t, providerFunc(func(context.Context, model.Request) (model.Response, error) {
				return model.Response{Parts: []session.Part{{Type: "text", Text: tc.text}}}, nil
			}))
			modelHelperGrant(t, r, owner, "models.call")
			budgets, err := r.Budgets(t.Context(), owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, budget := range budgets {
				if budget.Kind == session.BudgetLogicalWrites || budget.Kind == session.BudgetLogicalWriteBytes {
					if _, err := r.SetBudget(t.Context(), owner.ID, budget.Revision, session.BudgetLimit{Kind: budget.Kind, Limit: new(int64(0))}); err != nil {
						t.Fatal(err)
					}
				}
			}
			value, _, err := r.tools.Call(t.Context(), modelInvocation(owner, cell, "bounds", "call", map[string]any{"prompt": "text"}))
			if err != nil {
				t.Fatal(err)
			}
			item := value.(modelHelperItem)
			raw, err := json.Marshal(item)
			if err != nil || len(raw) > 15*1024 || item.Failure != nil || item.Truncated != tc.retained || (item.ContentRef != nil) != tc.retained || !utf8.ValidString(item.Text) {
				t.Fatal(item, err)
			}
			if tc.retained {
				_, body, err := r.ReadContentRange(t.Context(), owner.ID, *item.ContentRef, 0, 65536)
				if err != nil || string(body) != tc.text {
					t.Fatal(len(body), err)
				}
			} else if item.Text != tc.text {
				t.Fatal("inline output changed")
			}
		})
	}
}

func TestBothEnginesModelHelperCancellationSettlesBeforeCell(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `models.call(prompt="wait")
print("should not continue")`
			if engine == session.QuickJS {
				code = `await models.call({prompt:"wait"});console.log("should not continue");`
			}
			ordinary := cellProvider(map[string]string{"cancel": code})
			started := make(chan struct{})
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Purpose != session.ModelHelperPurpose {
					return ordinary(ctx, request)
				}
				close(started)
				<-ctx.Done()
				return model.Response{}, ctx.Err()
			})
			r := openEngineTest(t, t.TempDir(), provider)
			owner := createEngineSession(t, r, engine)
			modelHelperGrant(t, r, owner, "models.call")
			submitTest(t, r, owner.ID, "cancel")
			select {
			case <-started:
			case <-time.After(30 * time.Second):
				t.Fatal("helper did not start")
			}
			admission, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "cancel"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.CancelTurn(t.Context(), admission.Turn.ID); err != nil {
				t.Fatal(err)
			}
			done := waitTestWithin(t, r, "cancel", terminal, 30*time.Second)
			if done.Turn.State != session.Cancelled || r.Err() != nil {
				t.Fatal(done, r.Err())
			}
			attempts, err := r.ModelAttempts(t.Context(), done.Turn.ID, "", 100)
			if err != nil || len(attempts) != 2 {
				t.Fatal(attempts, err)
			}
			for _, attempt := range attempts {
				if attempt.FinishedAt == nil {
					t.Fatal("cell finished before attempt", attempt)
				}
			}
			operations, err := r.Operations(t.Context(), done.Turn.ID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].State != session.OperationUncertain {
				t.Fatal(operations, err)
			}
		})
	}
}

type modelHelperBrokenAccounting struct{ *store.Store }

func (a modelHelperBrokenAccounting) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	if message == nil {
		return session.ModelAttempt{}, errors.Join(session.ErrInvalid, context.Canceled)
	}
	return a.Store.SettleModelAttempt(ctx, id, result, message)
}

func TestBothEnginesModelHelperAccountingFaultCannotBeCaught(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `models.call(prompt="first")
models.call(prompt="must not run")`
			if engine == session.QuickJS {
				code = `try { await models.call({prompt:"first"}); } catch(e) {} await models.call({prompt:"must not run"});`
			}
			ordinary := cellProvider(map[string]string{"fatal": code})
			var helperCalls atomic.Int32
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Purpose != session.ModelHelperPurpose {
					return ordinary(ctx, request)
				}
				helperCalls.Add(1)
				return model.Response{Parts: []session.Part{{Type: "text", Text: "account this"}}}, nil
			})
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			r, err := Open(t.Context(), directory, provider, engineOptions(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			})
			r.runner, err = runner.New(provider, r.store, modelHelperBrokenAccounting{r.store}, r, r, r, r, r.store, r.store)
			if err != nil {
				t.Fatal(err)
			}
			owner := createEngineSession(t, r, engine)
			modelHelperGrant(t, r, owner, "models.call")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "fatal")
			deadline := time.NewTimer(30 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for r.Err() == nil {
				select {
				case <-deadline.C:
					t.Fatal("accounting fault did not stop runtime")
				case <-ticker.C:
				}
			}
			if helperCalls.Load() != 1 {
				t.Fatal("guest continued after accounting fault", helperCalls.Load())
			}
			cell, err := r.store.LatestCell(t.Context(), owner.ID)
			if err != nil || cell == nil || cell.Checkpoint != nil {
				t.Fatal("unsafe checkpoint after accounting fault", cell, err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openTest(t, directory, provider)
			if _, err := reopened.store.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			recovered, err := reopened.store.Cell(t.Context(), cell.ID)
			if err != nil || recovered.State != session.CellUncertain || recovered.Checkpoint != nil || helperCalls.Load() != 1 {
				t.Fatal(recovered, err)
			}
		})
	}
}
