package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func waitTitle(t *testing.T, r *Runtime, owner session.Session) store.Admission {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		value, err := r.Admission(ctx, session.RequestIdentity{ClientID: "automatic-title", RequestID: string(owner.TreeID)})
		if err == nil && terminal(value) {
			return value
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("title did not reach a terminal boundary", r.Err())
		case <-time.After(time.Millisecond):
		}
	}
}

func TestAutomaticTitlePendingIntentSurvivesForegroundCancellationAndRestart(t *testing.T) {
	path := t.TempDir()
	var calls atomic.Int32
	provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		calls.Add(1)
		if request.Purpose != session.AutomaticTitlePurpose || len(request.Messages) != 1 || len(request.Tools) != 0 || request.Messages[0].Parts[0].Text != "First accepted authored source remains eligible" {
			return model.Response{}, errors.New("unexpected conversation side effects")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "Accepted source"}}}, nil
	})
	r := openTest(t, path, provider)
	owner := createTest(t, r)
	if _, err := r.SetResource(t.Context(), owner.ID, 1, session.ResourceLimit{Kind: session.ResourceQueuedInputs, Limit: new(int64(1))}); err != nil {
		t.Fatal(err)
	}
	first, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "first"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "First accepted authored source remains eligible"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CancelInput(t.Context(), first.Input.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, provider)
	if _, err := r.AutomaticTitleDecision(t.Context(), owner.TreeID); err != nil || calls.Load() != 0 {
		t.Fatal("reading intent executed it", err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	result := waitTitle(t, r, owner)
	if result.Turn.State != session.Succeeded || calls.Load() != 1 {
		t.Fatal("pending title did not execute once after restart", result, calls.Load())
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 0 {
		t.Fatal("independent title authored transcript", err)
	}
	tree, err := r.Tree(t.Context(), owner.TreeID)
	if err != nil || tree.Metadata.Title == nil || *tree.Metadata.Title != "Accepted source" {
		t.Fatal("generated title missing", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, provider)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if pending, err := r.store.QueuedSessions(t.Context(), store.QueueCursor{}, 100); err != nil || len(pending) != 0 || calls.Load() != 1 {
		t.Fatal("restart replayed completed naming", err)
	}
}

func TestAutomaticTitleDeletionAndShutdownJoinDispatchedWork(t *testing.T) {
	for _, mode := range []string{"delete", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			var calls atomic.Int32
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Purpose != session.AutomaticTitlePurpose {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "ordinary answer"}}}, nil
				}
				calls.Add(1)
				close(entered)
				<-ctx.Done()
				close(cancelled)
				<-release
				return model.Response{}, ctx.Err()
			})
			path := t.TempDir()
			r := openTest(t, path, provider)
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			owner := createTest(t, r)
			if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "first"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "An accepted prompt long enough for automatic naming"}}}); err != nil {
				t.Fatal(err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			capacitySignal(t, entered)
			if _, err := r.store.SendMail(t.Context(), session.MailSpec{ID: "private-mail", SenderID: owner.ID, RecipientID: owner.ID, Delivery: session.MailNextTurn, Body: "private next-turn mail"}); err != nil {
				t.Fatal(err)
			}
			if err := r.DeleteSubtree(t.Context(), owner.ID); !errors.Is(err, store.ErrBusy) {
				t.Fatal("deletion passed an active title attempt", err)
			}
			closed := make(chan error, 1)
			if mode == "shutdown" {
				go func() { closed <- r.Close() }()
			} else if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
				t.Fatal(err)
			}
			capacitySignal(t, cancelled)
			if mode == "shutdown" {
				select {
				case err := <-closed:
					t.Fatal("shutdown returned before title joined", err)
				default:
				}
			} else if err := r.DeleteSubtree(t.Context(), owner.ID); !errors.Is(err, store.ErrBusy) {
				t.Fatal("deletion passed unjoined title work", err)
			}
			once.Do(func() { close(release) })
			if mode == "shutdown" {
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("shutdown did not join title")
				}
				r = openTest(t, path, provider)
				if err := r.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			result := waitTitle(t, r, owner)
			attempts, err := r.store.ModelAttempts(t.Context(), result.Turn.ID, "", 100)
			if err != nil || len(attempts) != 1 || attempts[0].State != session.AttemptUncertain || calls.Load() != 1 {
				t.Fatal("interrupted title replayed or lost dispatch evidence", attempts, err)
			}
			mail, err := r.store.ReadMail(t.Context(), owner.ID, "private-mail")
			if err != nil || mail.State != session.MailPending {
				t.Fatal("title consumed ordinary mail", err)
			}
			if mode == "delete" {
				if err := r.DeleteSubtree(t.Context(), owner.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := r.AutomaticTitleDecision(t.Context(), owner.TreeID); !errors.Is(err, store.ErrNotFound) {
					t.Fatal("deleted tree retained pending naming authority", err)
				}
			}
		})
	}
}
