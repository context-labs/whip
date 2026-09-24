package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestQuestionSnapshotCursorMatchesPendingState(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var cursor int64
	for range 12 {
		asked := make(chan error, 1)
		go func() {
			_, err := root.AskUser(ctx, rootID, []session.QuestionSet{{Question: "Continue?", Options: []session.QuestionOption{{Label: "yes"}}}})
			asked <- err
		}()
		question, seq := waitQuestionEvent(t, store, rootID, "question.pending", cursor)
		cursor = seq
		// Take snapshots while an answer commits on another goroutine.
		answer := make(chan error, 1)
		go func() {
			_, err := root.answerQuestion(ctx, question.QuestionID, protocol.QuestionAnswerParams{ID: question.QuestionID, Answer: []string{"yes"}})
			answer <- err
		}()
		for range 4 {
			snapshot, err := root.SnapshotView(ctx)
			if err != nil {
				t.Fatal(err)
			}
			events, _, err := store.ReplayEvents(ctx, rootID, 0, session.MaxEventReplay)
			if err != nil {
				t.Fatal(err)
			}
			pending := map[string]bool{}
			for _, event := range events {
				if event.Seq > snapshot.Cursor {
					break
				}
				var payload session.LifecycleEvent
				if err := json.Unmarshal(event.Payload.Inline, &payload); err != nil {
					t.Fatal(err)
				}
				switch event.Kind {
				case "question.pending":
					pending[payload.QuestionID] = true
				case "question.answered", "question.closed":
					delete(pending, payload.QuestionID)
				}
			}
			if len(pending) != len(snapshot.Questions) {
				t.Fatalf("cursor %d pending=%v snapshot=%+v", snapshot.Cursor, pending, snapshot.Questions)
			}
			for _, q := range snapshot.Questions {
				if !pending[q.QuestionID] {
					t.Fatalf("question ahead of snapshot cursor: %s", q.QuestionID)
				}
			}
		}
		if err := <-answer; err != nil {
			t.Fatal(err)
		}
		if err := <-asked; err != nil {
			t.Fatal(err)
		}
	}
}
