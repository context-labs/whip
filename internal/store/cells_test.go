package store

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func cellCall(t *testing.T, s *Store, owner session.SessionID) (session.Turn, session.Message) {
	t.Helper()
	submit(t, s, owner, "execute")
	turn := claim(t, s, owner).Turn
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "call-message", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"x = 1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	return turn, message
}

func TestCellDispatchAndAtomicCheckpointBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTest(t, path)
	other := openTest(t, path)
	_, root := create(t, s, nil)
	turn, message := cellCall(t, s, root.ID)
	spec := session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"}
	var wins atomic.Int32
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			_, dispatch, err := db.BeginCell(t.Context(), spec)
			if err != nil {
				t.Error(err)
			}
			if dispatch {
				wins.Add(1)
			}
		})
	}
	workers.Wait()
	if wins.Load() != 1 {
		t.Fatalf("cell dispatches=%d", wins.Load())
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Failed, new("failed"), nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("finished running cell: %v", err)
	}
	result := session.ToolResult{CallID: "call", Output: "done"}
	checkpoint := &session.Checkpoint{Engine: session.Starlark, Digest: strings.Repeat("a", 64), Size: 2, Metadata: []byte(`{}`)}
	execTest(t, s, `CREATE TRIGGER fail_cell BEFORE UPDATE ON cells BEGIN SELECT RAISE(ABORT,'injected cell failure'); END`)
	if _, err := s.SettleCell(t.Context(), spec.ID, session.CellSucceeded, result, checkpoint); err == nil {
		t.Fatal("injected write succeeded")
	}
	latest, err := s.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.State != session.CellRunning || latest.Checkpoint != nil || count(t, s, "messages") != 2 {
		t.Fatalf("partial cell commit: %+v", latest)
	}
	execTest(t, s, "DROP TRIGGER fail_cell")
	for range 2 {
		if _, err := s.SettleCell(t.Context(), spec.ID, session.CellSucceeded, result, checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	latest, err = s.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Checkpoint.Digest != checkpoint.Digest || latest.ResultMessageID == nil || count(t, s, "messages") != 3 {
		t.Fatalf("incorrect boundary: %+v", latest)
	}
	changed := result
	changed.Output = "different"
	if _, err := s.SettleCell(t.Context(), spec.ID, session.CellSucceeded, changed, checkpoint); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed result retry: %v", err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	referenced, err := s.ContentReferenced(t.Context(), checkpoint.Digest)
	if err != nil || !referenced {
		t.Fatalf("checkpoint not retained by collection: %v %v", referenced, err)
	}
}

func TestCellRecoveryReconcilesCallsWithoutReplay(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(map[bool]string{false: "not_dispatched", true: "uncertain"}[dispatched], func(t *testing.T) {
			s := fresh(t)
			_, root := create(t, s, nil)
			turn, message := cellCall(t, s, root.ID)
			if dispatched {
				if _, _, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			history, err := s.History(t.Context(), root.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != 3 || history[2].Role != session.Tool || !history[2].Parts[0].Result.IsError {
				t.Fatalf("unreconciled history: %+v", history)
			}
			want := "not dispatched"
			if dispatched {
				want = "outcome is unknown"
			}
			if !strings.Contains(history[2].Parts[0].Result.Output, want) {
				t.Fatal(history[2].Parts[0].Result.Output)
			}
			if dispatched {
				latest, err := s.LatestCell(t.Context(), root.ID)
				if err != nil || latest.State != session.CellUncertain || latest.Checkpoint != nil {
					t.Fatalf("false recovery: %+v %v", latest, err)
				}
			}
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if count(t, s, "messages") != 3 {
				t.Fatal("recovery duplicated tool result")
			}
			submit(t, s, root.ID, "next")
			next := claim(t, s, root.ID)
			if _, err := s.AppendMessage(t.Context(), next.Turn.ID, session.MessageDraft{ID: "next-answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "inspection is still possible"}}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestToolTranscriptRejectsSpoofingAndMissingResults(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	turn, _ := cellCall(t, s, root.ID)
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "spoof"}, Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "spoof", Name: "execute", Arguments: []byte(`{}`)}}}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("input call spoof: %v", err)
	}
	if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "premature", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "done"}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unanswered calls ignored: %v", err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("successful turn with missing result: %v", err)
	}
	if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "wrong", Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "other", Output: "fake"}}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unrelated result: %v", err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Failed, new("provider failed"), nil); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "messages") != 3 {
		t.Fatal("terminal failure did not reconcile unexecuted call")
	}
}
