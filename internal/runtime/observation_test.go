package runtime

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type observationResult struct {
	response model.Response
	err      error
}

type observationCall struct {
	ctx      context.Context
	emit     func(model.Chunk)
	complete chan observationResult
}

type observationProvider struct {
	started     chan observationCall
	calls       atomic.Int32
	maxAttempts int
}

func (p *observationProvider) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	if err != nil {
		return prepared, err
	}
	if p.maxAttempts > 0 {
		prepared.MaxAttempts = p.maxAttempts
	}
	prepared.Execute = func(ctx context.Context, emit func(model.Chunk)) (model.Response, error) {
		p.calls.Add(1)
		call := observationCall{ctx: ctx, emit: emit, complete: make(chan observationResult, 1)}
		select {
		case p.started <- call:
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
		select {
		case result := <-call.complete:
			return result.response, result.err
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}
	return prepared, nil
}

func nextObservationCall(t *testing.T, provider *observationProvider) observationCall {
	t.Helper()
	select {
	case call := <-provider.started:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
		return observationCall{}
	}
}

func observeTest(t *testing.T, r *Runtime, id session.SessionID) Observation {
	t.Helper()
	value, err := r.Observe(t.Context(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestObservationReplacesPreviewWithoutGivingObserversExecutionOwnership(t *testing.T) {
	provider := &observationProvider{started: make(chan observationCall)}
	r := openTest(t, t.TempDir(), provider)
	current, other := createTest(t, r), createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, current.ID, "first-preview")
	first := nextObservationCall(t, provider)
	first.emit(model.Chunk{Reasoning: "considering "})
	first.emit(model.Chunk{Text: "partial "})
	first.emit(model.Chunk{Reasoning: "the request"})
	first.emit(model.Chunk{Text: "answer", Call: &model.CallChunk{Index: 0, ID: "call-1", Name: "execute", Arguments: `{"code":"print(`}})
	live := observeTest(t, r, current.ID)
	if live.Epoch == "" || live.Preview == nil || live.Preview.Text != "partial answer" || live.Preview.Reasoning != "considering the request" || live.Preview.Revision != 4 || len(live.Preview.Calls) != 1 {
		t.Fatalf("missing provisional fragments: %+v", live)
	}
	if len(live.Messages) != 1 || live.Messages[0].Role != session.User {
		t.Fatalf("partial provider output entered history: %+v", live.Messages)
	}
	if isolated := observeTest(t, r, other.ID); isolated.Preview != nil || len(isolated.Messages) != 0 {
		t.Fatalf("preview escaped its session: %+v", isolated)
	}
	cells, err := r.Cells(t.Context(), live.Preview.TurnID, "", 100)
	if err != nil || len(cells) != 0 {
		t.Fatalf("partial tool arguments became executable: %+v %v", cells, err)
	}
	arguments := live.Preview.Calls[0].Arguments
	live.Preview.Calls[0].Arguments = "observer mutation"
	if again := observeTest(t, r, current.ID); again.Preview == nil || again.Preview.Calls[0].Arguments != arguments {
		t.Fatal("observation aliased the live preview")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Observe(ctx, current.ID, 0, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation returned %v", err)
	}
	if err := first.ctx.Err(); err != nil {
		t.Fatalf("observer cancellation reached provider: %v", err)
	}
	first.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "authoritative answer"}}}}
	finished := waitTest(t, r, "first-preview", terminal)
	if finished.Turn.State != session.Succeeded {
		t.Fatalf("observer cancellation stopped execution: %+v", finished.Turn)
	}
	committed := observeTest(t, r, current.ID)
	if committed.Preview != nil || len(committed.Messages) != 2 || committed.Messages[1].ID != live.Preview.MessageID || committed.Messages[1].Parts[0].Text != "authoritative answer" {
		t.Fatalf("committed message did not replace its preview: %+v", committed)
	}
	page, err := r.Observe(t.Context(), current.ID, committed.Messages[1].Sequence, 1)
	if err != nil || page.Preview != nil || len(page.Messages) != 0 {
		t.Fatalf("settled preview reappeared beyond the history cursor: %+v %v", page, err)
	}
	submitTest(t, r, current.ID, "second-preview")
	second := nextObservationCall(t, provider)
	second.emit(model.Chunk{Text: "fresh", Reasoning: "new reasoning"})
	first.emit(model.Chunk{Text: "late callback", Reasoning: "stale reasoning", Call: &model.CallChunk{Index: 1, Arguments: "stale"}})
	newer := observeTest(t, r, current.ID)
	if newer.Preview == nil || newer.Preview.Text != "fresh" || newer.Preview.Reasoning != "new reasoning" || newer.Preview.AttemptID == live.Preview.AttemptID || newer.Preview.Revision != 1 || len(newer.Preview.Calls) != 0 {
		t.Fatalf("old provider callback changed the next preview: %+v", newer.Preview)
	}
	second.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "second answer"}}}}
	waitTest(t, r, "second-preview", terminal)
}

func TestObservationDiscardsFailedCancelledAndRestartedPreviews(t *testing.T) {
	for _, ending := range []string{"failure", "cancel", "reopen"} {
		t.Run(ending, func(t *testing.T) {
			provider := &observationProvider{started: make(chan observationCall)}
			directory := t.TempDir()
			r := openTest(t, directory, provider)
			current := createTest(t, r)
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, current.ID, ending)
			call := nextObservationCall(t, provider)
			call.emit(model.Chunk{Reasoning: "never committed"})
			before := observeTest(t, r, current.ID)
			if before.Preview == nil || before.Preview.Reasoning != "never committed" || before.Preview.Text != "" {
				t.Fatal("reasoning-only provider preview did not appear")
			}
			want := session.Failed
			switch ending {
			case "failure":
				call.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "also uncommitted"}}}, err: errors.New("provider failed after partial output")}
			case "cancel":
				want = session.Cancelled
				if _, err := r.CancelTurn(t.Context(), before.Preview.TurnID); err != nil {
					t.Fatal(err)
				}
			case "reopen":
				want = session.Interrupted
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				r = openTest(t, directory, provider)
				if err := r.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			finished := waitTest(t, r, ending, terminal)
			if finished.Turn.State != want {
				t.Fatalf("turn state=%s want=%s", finished.Turn.State, want)
			}
			call.emit(model.Chunk{Reasoning: "late after termination"})
			after := observeTest(t, r, current.ID)
			if after.Preview != nil || len(after.Messages) != 1 || after.Messages[0].Role != session.User {
				t.Fatalf("partial output survived %s: %+v", ending, after)
			}
			if ending == "reopen" && (after.Epoch == "" || after.Epoch == before.Epoch) {
				t.Fatalf("reopen reused the old presentation epoch: %q", after.Epoch)
			}
			if provider.calls.Load() != 1 {
				t.Fatalf("partial attempt replayed: calls=%d", provider.calls.Load())
			}
		})
	}
}

type observationSettlements struct {
	*store.Store
	failed chan struct{}
	calls  atomic.Int32
}

func (s *observationSettlements) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	s.calls.Add(1)
	value, err := s.Store.SettleModelAttempt(ctx, id, result, message)
	if err != nil {
		select {
		case s.failed <- struct{}{}:
		default:
		}
	}
	return value, err
}

func TestObservationKeepsPreviewDuringSQLSettlementRetry(t *testing.T) {
	provider := &observationProvider{started: make(chan observationCall)}
	directory := t.TempDir()
	r := openTest(t, directory, provider)
	current := createTest(t, r)
	settlements := &observationSettlements{Store: r.store, failed: make(chan struct{}, 1)}
	var err error
	r.runner, err = runner.New(provider, r.store, settlements, r, r, r, r, r.store, r.store)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, "state.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER fail_preview_settlement BEFORE INSERT ON messages WHEN NEW.role='assistant' BEGIN SELECT RAISE(ABORT,'injected settlement failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, current.ID, "sql-retry")
	call := nextObservationCall(t, provider)
	call.emit(model.Chunk{Text: "visible until committed", Reasoning: "provisional reasoning"})
	before := observeTest(t, r, current.ID)
	if before.Preview == nil {
		t.Fatal("missing active preview")
	}
	call.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "durable answer"}}}}
	select {
	case <-settlements.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("SQL settlement fault was not exercised")
	}
	during := observeTest(t, r, current.ID)
	if during.Preview == nil || during.Preview.MessageID != before.Preview.MessageID || during.Preview.Text != before.Preview.Text || during.Preview.Reasoning != before.Preview.Reasoning || len(during.Messages) != 1 {
		t.Fatalf("failed SQL settlement lost preview or leaked message: %+v", during)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TRIGGER fail_preview_settlement"); err != nil {
		t.Fatal(err)
	}
	finished := waitTest(t, r, "sql-retry", terminal)
	after := observeTest(t, r, current.ID)
	if finished.Turn.State != session.Succeeded || after.Preview != nil || len(after.Messages) != 2 || after.Messages[1].ID != before.Preview.MessageID {
		t.Fatalf("settlement retry did not commit original result: turn=%+v observation=%+v", finished.Turn, after)
	}
	if provider.calls.Load() != 1 || settlements.calls.Load() < 2 {
		t.Fatalf("retry redispatched provider: provider=%d settlement=%d", provider.calls.Load(), settlements.calls.Load())
	}
}

func TestObservationSharesUTF8ByteBoundAcrossAllPreviewFields(t *testing.T) {
	provider := &observationProvider{started: make(chan observationCall)}
	r := openTest(t, t.TempDir(), provider)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"text", "tool", "reasoning", "mixed"} {
		t.Run(field, func(t *testing.T) {
			current := createTest(t, r)
			submitTest(t, r, current.ID, field)
			call := nextObservationCall(t, provider)
			large := strings.Repeat("界", maxPreviewBytes/3+2)
			switch field {
			case "text":
				call.emit(model.Chunk{Text: large})
			case "tool":
				call.emit(model.Chunk{Call: &model.CallChunk{Index: 0, ID: "id", Name: "execute", Arguments: large}})
			case "reasoning":
				call.emit(model.Chunk{Reasoning: large})
			case "mixed":
				call.emit(model.Chunk{Text: "partial", Call: &model.CallChunk{Index: 0, ID: "id", Name: "execute", Arguments: `{"code":"`}})
				call.emit(model.Chunk{Reasoning: large})
			}
			view := observeTest(t, r, current.ID)
			if view.Preview == nil || !view.Preview.Truncated || len(view.Messages) != 1 {
				t.Fatalf("oversized preview was not bounded: %+v", view)
			}
			preview := view.Preview
			size := len(preview.Text) + len(preview.Reasoning)
			if !utf8.ValidString(preview.Text) || !utf8.ValidString(preview.Reasoning) {
				t.Fatal("preview truncation split a UTF-8 codepoint")
			}
			for _, fragment := range preview.Calls {
				size += len(fragment.ID) + len(fragment.Name) + len(fragment.Arguments)
				if len(fragment.ID) > 128 || len(fragment.Name) > 64 || !utf8.ValidString(fragment.Arguments) {
					t.Fatal("tool preview violated its bounds or UTF-8 encoding")
				}
			}
			if size > maxPreviewBytes || size < maxPreviewBytes-3 {
				t.Fatalf("preview payload bytes=%d limit=%d", size, maxPreviewBytes)
			}
			if field == "tool" && (len(preview.Calls) != 1 || preview.Calls[0].Arguments == "") {
				t.Fatal("partial tool arguments were lost")
			}
			call.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "complete despite preview limit"}}}}
			finished := waitTest(t, r, field, terminal)
			if finished.Turn.State != session.Succeeded || observeTest(t, r, current.ID).Preview != nil {
				t.Fatal("preview truncation affected durable completion")
			}
		})
	}
}

func TestObservationClearsReasoningBeforeConfirmedRetry(t *testing.T) {
	provider := &observationProvider{started: make(chan observationCall), maxAttempts: 2}
	r := openTest(t, t.TempDir(), provider)
	current := createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, current.ID, "preview-retry")
	first := nextObservationCall(t, provider)
	first.emit(model.Chunk{Reasoning: "first attempt"})
	before := observeTest(t, r, current.ID)
	if before.Preview == nil || before.Preview.Reasoning != "first attempt" {
		t.Fatal("missing first attempt preview")
	}
	// This controlled provider explicitly confirms that retry is safe. The HTTP
	// adapters never classify an interrupted stream as a retryable response.
	first.complete <- observationResult{err: &model.CallError{StatusCode: 429, Retryable: true, Message: "confirmed rejection"}}
	second := nextObservationCall(t, provider)
	fresh := observeTest(t, r, current.ID)
	if fresh.Preview == nil || fresh.Preview.AttemptID == before.Preview.AttemptID || fresh.Preview.Reasoning != "" || fresh.Preview.Revision != 0 {
		t.Fatalf("retry retained the previous preview: %+v", fresh.Preview)
	}
	second.emit(model.Chunk{Reasoning: "second attempt"})
	first.emit(model.Chunk{Reasoning: "stale first callback"})
	fresh = observeTest(t, r, current.ID)
	if fresh.Preview == nil || fresh.Preview.Reasoning != "second attempt" || fresh.Preview.Revision != 1 {
		t.Fatalf("retry preview was changed by a stale callback: %+v", fresh.Preview)
	}
	second.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "completed retry"}}}}
	finished := waitTest(t, r, "preview-retry", terminal)
	after := observeTest(t, r, current.ID)
	if finished.Turn.State != session.Succeeded || after.Preview != nil || len(after.Messages) != 2 || after.Messages[1].ID != fresh.Preview.MessageID || len(after.Messages[1].Parts) != 1 || after.Messages[1].Parts[0].Text != "completed retry" {
		t.Fatalf("retry did not replace only its own preview: turn=%+v observation=%+v", finished.Turn, after)
	}
	attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || len(attempts) != 2 || provider.calls.Load() != 2 {
		t.Fatalf("retry evidence: attempts=%+v calls=%d err=%v", attempts, provider.calls.Load(), err)
	}
}
