package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func openMailRuntime(t *testing.T, directory string, provider providerFunc) *Runtime {
	t.Helper()
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
	return r
}

func mailSender(t *testing.T, r *Runtime, parent session.Session) session.Session {
	t.Helper()
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "mail_sender"}, store.ChildRequest{
		ParentID: parent.ID, Parts: []session.Part{{Type: "text", Text: "sender seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *child.Session
}

func sendRuntimeMail(t *testing.T, r *Runtime, from, to session.SessionID, id string, delivery session.MailDelivery) session.MailMetadata {
	t.Helper()
	result, err := r.SendMail(t.Context(), session.MailSpec{ID: session.MailID(id), SenderID: from, RecipientID: to, Delivery: delivery, Body: "body for " + id})
	if err != nil || result.Mail == nil {
		t.Fatalf("send mail: %+v %v", result, err)
	}
	return *result.Mail
}

func awaitMailSignal[T any](t *testing.T, signal <-chan T) T {
	t.Helper()
	select {
	case result := <-signal:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("mail runtime did not reach expected execution boundary")
		var zero T
		return zero
	}
}

func awaitMailTurn(t *testing.T, r *Runtime, id session.TurnID) session.Turn {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		turn, err := r.Turn(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if turn.FinishedAt != nil {
			return turn
		}
		select {
		case <-timer.C:
			t.Fatalf("mail turn did not settle: %+v runtime=%v", turn, r.Err())
		case <-ticker.C:
		}
	}
}

func assertMailState(t *testing.T, r *Runtime, recipient session.SessionID, id string, state session.MailState) {
	t.Helper()
	mail, err := r.ReadMail(t.Context(), recipient, session.MailID(id))
	if err != nil || mail.State != state {
		t.Fatalf("mail %s state=%+v err=%v, want %s", id, mail, err, state)
	}
}

func mailCode(id, code string) session.Part {
	arguments, _ := json.Marshal(map[string]string{"code": code})
	return session.Part{Type: "tool_call", Call: &session.ToolCall{ID: id, Name: "execute", Arguments: arguments}}
}

func TestBothEnginesMailOnlyTurnSeparatesInspectionListAndRead(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			started := make(chan session.TurnID, 1)
			finishCell, finishTurn := make(chan struct{}), make(chan struct{})
			cellSettled := make(chan struct{}, 1)
			var rootID, senderID session.SessionID
			var firstTurn session.TurnID
			calls := 0
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.SessionID != rootID {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "sender ready"}}}, nil
				}
				if firstTurn == "" {
					firstTurn = request.TurnID
				}
				if request.TurnID != firstTurn {
					<-ctx.Done()
					return model.Response{}, ctx.Err()
				}
				calls++
				if calls == 1 {
					started <- request.TurnID
					if err := capacityWait(ctx, finishCell); err != nil {
						return model.Response{}, err
					}
					code := fmt.Sprintf("listed=mail.list()\nread=mail.read(id=\"mail_read\")\nprint(read[\"body\"])\nmail.send(recipient_id=%q, body=\"reply\", delivery=\"next_turn\")", senderID)
					if engine == session.QuickJS {
						code = fmt.Sprintf("var listed=await mail.list({}); var read=await mail.read({id:'mail_read'}); print(read.body); await mail.send({recipient_id:%q,body:'reply',delivery:'next_turn'});", senderID)
					}
					return model.Response{Parts: []session.Part{mailCode("read_mail", code)}}, nil
				}
				cellSettled <- struct{}{}
				if err := capacityWait(ctx, finishTurn); err != nil {
					return model.Response{}, err
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "mail handled"}}}, nil
			})
			r := openMailRuntime(t, t.TempDir(), provider)
			root := createEngineSession(t, r, engine)
			rootID = root.ID
			for _, name := range []string{"list", "read", "send"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("mail_" + name), SessionID: root.ID, Capability: "mail." + name, Resource: string(root.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			sender := mailSender(t, r, root)
			senderID = sender.ID
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_initial", session.MailQueued)
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_carried", session.MailNextTurn)
			listed, err := r.ListMail(t.Context(), root.ID, "", "", 100)
			if err != nil || len(listed) != 2 {
				t.Fatalf("inspect mail=%+v %v", listed, err)
			}
			assertMailState(t, r, root.ID, "mail_initial", session.MailPending)
			history, err := r.History(t.Context(), root.ID, 0, 100)
			if err != nil || len(history) != 0 || calls != 0 {
				t.Fatal("inspection started execution or wrote transcript")
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			turnID := awaitMailSignal(t, started)
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_listed", session.MailQueued)
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_read", session.MailQueued)
			close(finishCell)
			awaitMailSignal(t, cellSettled)
			for _, id := range []string{"mail_initial", "mail_carried", "mail_listed", "mail_read"} {
				assertMailState(t, r, root.ID, id, session.MailPending)
			}
			close(finishTurn)
			if turn := awaitMailTurn(t, r, turnID); turn.State != session.Succeeded {
				t.Fatalf("mail-only turn=%+v", turn)
			}
			for _, id := range []string{"mail_initial", "mail_carried", "mail_read"} {
				assertMailState(t, r, root.ID, id, session.MailDelivered)
			}
			assertMailState(t, r, root.ID, "mail_listed", session.MailPending)
			history, err = r.History(t.Context(), root.ID, 0, 100)
			if err != nil || len(history) == 0 || history[0].Mail == nil || history[0].InputID != nil {
				t.Fatalf("mail-only history was not reference-backed: %+v %v", history, err)
			}
			foundRead := false
			for _, message := range history {
				if message.TurnID != turnID || message.Role != session.Tool {
					continue
				}
				foundRead = strings.Contains(message.Parts[0].Result.Output, "body for mail_read")
			}
			if !foundRead {
				t.Fatal("agent read did not return mail body through the actual engine")
			}
			replies, err := r.ListMail(t.Context(), sender.ID, session.MailPending, "", 100)
			if err != nil || len(replies) != 1 || replies[0].Delivery != session.MailNextTurn {
				t.Fatalf("host send did not preserve next-turn delivery: %+v %v", replies, err)
			}
		})
	}
}

func TestBothEnginesMailSteerWaitsForEntireToolBatch(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			started := make(chan session.TurnID, 1)
			releaseFirst, releaseFinal := make(chan struct{}), make(chan struct{})
			observed := make(chan model.Request, 1)
			var rootID session.SessionID
			var firstTurn session.TurnID
			calls := 0
			r := openMailRuntime(t, t.TempDir(), providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.SessionID != rootID {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "ready"}}}, nil
				}
				if firstTurn == "" {
					firstTurn = request.TurnID
				}
				if request.TurnID != firstTurn {
					<-ctx.Done()
					return model.Response{}, ctx.Err()
				}
				calls++
				if calls == 1 {
					started <- request.TurnID
					if err := capacityWait(ctx, releaseFirst); err != nil {
						return model.Response{}, err
					}
					code := "print(mail.read(id=\"mail_gate\")[\"body\"])"
					if engine == session.QuickJS {
						code = "print((await mail.read({id:'mail_gate'})).body)"
					}
					return model.Response{Parts: []session.Part{mailCode("first", "print(1)"), mailCode("second", code)}}, nil
				}
				observed <- request
				if err := capacityWait(ctx, releaseFinal); err != nil {
					return model.Response{}, err
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}))
			root := createEngineSession(t, r, engine)
			rootID = root.ID
			sender := mailSender(t, r, root)
			submitTest(t, r, root.ID, "steer_boundary")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			turnID := awaitMailSignal(t, started)
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_gate", session.MailNextTurn)
			close(releaseFirst)
			permission := awaitRuntimeFilePermission(t, r, root.ID, "steer_boundary", "mail.read")
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_steer", session.MailSteer)
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_queued", session.MailQueued)
			history, err := r.History(t.Context(), root.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range history {
				if message.Mail != nil {
					t.Fatal("mail entered transcript while a code cell was running")
				}
			}
			if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
				t.Fatal(err)
			}
			request := awaitMailSignal(t, observed)
			if len(request.Messages) != 5 || request.Messages[2].Role != session.Tool || request.Messages[3].Role != session.Tool || !strings.Contains(request.Messages[4].Parts[0].Text, "mail_steer") {
				t.Fatalf("steer interleaved with tool batch: %+v", request.Messages)
			}
			for _, message := range request.Messages {
				for _, part := range message.Parts {
					if strings.Contains(part.Text, "mail_queued") {
						t.Fatal("queued mail entered an already running turn")
					}
				}
			}
			close(releaseFinal)
			if turn := awaitMailTurn(t, r, turnID); turn.State != session.Succeeded {
				t.Fatalf("steered turn=%+v", turn)
			}
			assertMailState(t, r, root.ID, "mail_steer", session.MailDelivered)
			assertMailState(t, r, root.ID, "mail_gate", session.MailDelivered)
			assertMailState(t, r, root.ID, "mail_queued", session.MailPending)
		})
	}
}

func TestMailFailureBarrierSurvivesRestartAndExplicitInputRecovers(t *testing.T) {
	for _, state := range []session.TurnState{session.Failed, session.Cancelled, session.Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			directory := t.TempDir()
			started := make(chan session.TurnID, 1)
			fail := make(chan struct{})
			var rootID session.SessionID
			r := openMailRuntime(t, directory, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.SessionID != rootID {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "ready"}}}, nil
				}
				started <- request.TurnID
				if err := capacityWait(ctx, fail); err != nil {
					return model.Response{}, err
				}
				return model.Response{}, errors.New("injected provider failure")
			}))
			root := createTest(t, r)
			rootID = root.ID
			sender := mailSender(t, r, root)
			sendRuntimeMail(t, r, sender.ID, root.ID, "mail_retry", session.MailQueued)
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			turnID := awaitMailSignal(t, started)
			switch state {
			case session.Failed:
				close(fail)
			case session.Cancelled:
				if _, err := r.CancelTurn(t.Context(), turnID); err != nil {
					t.Fatal(err)
				}
			case session.Interrupted:
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if state != session.Interrupted {
				if turn := awaitMailTurn(t, r, turnID); turn.State != state {
					t.Fatalf("first turn=%+v", turn)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			unexpected := make(chan struct{}, 4)
			r = openMailRuntime(t, directory, providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				if request.SessionID == rootID {
					calls.Add(1)
					unexpected <- struct{}{}
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "recovered"}}}, nil
			}))
			if turn := awaitMailTurn(t, r, turnID); turn.State != state {
				t.Fatalf("restart turn=%+v", turn)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			for range 10 {
				r.Wake()
				assertMailState(t, r, root.ID, "mail_retry", session.MailPending)
			}
			select {
			case <-unexpected:
				t.Fatal("restart or observation bypassed the durable mail retry barrier")
			case <-time.After(50 * time.Millisecond):
			}
			submitTest(t, r, root.ID, "explicit_recovery")
			if a := waitTest(t, r, "explicit_recovery", terminal); a.Turn.State != session.Succeeded || calls.Load() != 1 {
				t.Fatalf("explicit recovery=%+v calls=%d", a.Turn, calls.Load())
			}
			assertMailState(t, r, root.ID, "mail_retry", session.MailDelivered)
		})
	}
}

func TestDeferredMailWakesWithoutAnotherNotification(t *testing.T) {
	started := make(chan session.TurnID, 1)
	r := openMailRuntime(t, t.TempDir(), providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		started <- request.TurnID
		return model.Response{Parts: []session.Part{{Type: "text", Text: "handled due mail"}}}, nil
	}))
	root := createTest(t, r)
	if _, err := r.SendMail(t.Context(), session.MailSpec{ID: "deferred", SenderID: root.ID, RecipientID: root.ID, Delivery: session.MailQueued, Body: "wake later", AvailableAt: new(time.Now().Add(150 * time.Millisecond))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	turn := awaitMailSignal(t, started)
	if result := awaitMailTurn(t, r, turn); result.State != session.Succeeded {
		t.Fatalf("due mail turn=%+v", result)
	}
	assertMailState(t, r, root.ID, "deferred", session.MailDelivered)
}
