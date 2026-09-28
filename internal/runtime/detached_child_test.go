package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestDetachedChildSurvivesParentCompletionAndReportsExplicitCancellation(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			type heldChild struct {
				request model.Request
				context context.Context
			}
			childEntered := make(chan heldChild, 1)
			childCancelled := make(chan struct{})
			finishParent := make(chan struct{})
			var childCalls atomic.Int32
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				switch request.Messages[0].Parts[0].Text {
				case "detached_child":
					if childCalls.Add(1) != 1 {
						return model.Response{}, errors.New("detached child was replayed")
					}
					childEntered <- heldChild{request: request, context: ctx}
					<-ctx.Done()
					close(childCancelled)
					return model.Response{}, ctx.Err()
				case "detached_parent":
					if len(request.Messages) == 1 {
						code := `child=agents.spawn(prompt="detached_child", overrides={"report_mode":"notice"})`
						if engine == session.QuickJS {
							code = `var child=await agents.spawn({prompt:"detached_child",overrides:{report_mode:"notice"}});`
						}
						return childControlCode(1, code), nil
					}
					select {
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					case <-finishParent:
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "parent finished without waiting"}}}, nil
				default:
					return model.Response{}, errors.New("unexpected autonomous turn")
				}
			})
			r, root := controlRuntime(t, provider, engine, 2)
			// An idle ancestor lets the resource projection count both descendants'
			// execution permits while the ordinary parent/child path stays unchanged.
			parent, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "detached_parent"}, store.ChildRequest{
				ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "detached_parent"}}, Overrides: session.ConfigPatch{ReportMode: new(session.ReportMessage)},
			})
			if err != nil {
				t.Fatal(err)
			}
			held := awaitMailSignal(t, childEntered)
			childRequest := held.request
			capacityState(t, r, 2, 2, 0, 0)
			if used := detachedRunnableUsage(t, r, root.ID); used != 2 {
				t.Fatalf("parent/child permits=%d, want2", used)
			}
			close(finishParent)
			finished := waitTestWithin(t, r, "detached_parent", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded || finished.Input.ID != parent.Admission.Input.ID {
				t.Fatalf("parent did not finish independently: %+v runtime=%v", finished, r.Err())
			}
			capacityState(t, r, 1, 1, 0, 0)
			if used := detachedRunnableUsage(t, r, root.ID); used != 1 {
				t.Fatalf("finished parent retained runnable permit: %d", used)
			}
			r.mu.Lock()
			parentActive := r.active[parent.Session.ID] != nil
			activeChild := r.active[childRequest.SessionID]
			childActive := activeChild != nil && activeChild.turn == childRequest.TurnID
			r.mu.Unlock()
			if parentActive || !childActive {
				t.Fatal("parent completion changed child execution ownership")
			}
			if err := held.context.Err(); err != nil {
				t.Fatal("parent completion cancelled retained child", err)
			}
			child, err := r.Session(t.Context(), childRequest.SessionID)
			if err != nil || child.ParentID == nil || *child.ParentID != parent.Session.ID || child.Lifecycle != session.Active || child.Config.ReportMode != session.ReportNotice {
				t.Fatalf("child was not retained: %+v %v", child, err)
			}
			current, err := r.Turn(t.Context(), childRequest.TurnID)
			if err != nil || current.State != session.Running {
				t.Fatalf("parent ended child turn: %+v %v", current, err)
			}
			history, err := r.History(t.Context(), child.ID, 0, 100)
			if err != nil || len(history) != 1 || history[0].InputID == nil {
				t.Fatalf("child lost exact admitted input: %+v %v", history, err)
			}
			inputID := *history[0].InputID
			// Keep the automatic cancellation report available for inspection. Stopping
			// this already-finished parent must not cancel its retained descendant.
			if _, err := r.SetLifecycle(t.Context(), parent.Session.ID, session.Stopped); err != nil {
				t.Fatal(err)
			}
			if err := held.context.Err(); err != nil {
				t.Fatal("stopping parent implicitly cancelled child", err)
			}
			cancelled, err := r.CancelInput(t.Context(), inputID)
			if err != nil || cancelled.TurnID == nil || *cancelled.TurnID != childRequest.TurnID {
				t.Fatalf("cancellation targeted another turn: %+v %v", cancelled, err)
			}
			awaitMailSignal(t, childCancelled)
			terminalChild := awaitMailTurn(t, r, childRequest.TurnID)
			if terminalChild.State != session.Cancelled || childCalls.Load() != 1 {
				t.Fatalf("child outcome=%+v calls=%d", terminalChild, childCalls.Load())
			}
			capacityState(t, r, 0, 0, 0, 0)
			if used := detachedRunnableUsage(t, r, root.ID); used != 0 {
				t.Fatalf("cancelled child retained permit: %d", used)
			}
			mail := awaitDetachedCompletion(t, r, parent.Session.ID)
			if mail.Source != (session.MailSource{Kind: "completion", ID: string(child.ID)}) || mail.State != session.MailPending || mail.Delivery != session.MailQueued {
				t.Fatalf("wrong completion delivery: %+v", mail)
			}
			var notice session.CompletionNotice
			if err := json.Unmarshal([]byte(mail.Body), &notice); err != nil {
				t.Fatal(err)
			}
			reference, raw, err := r.ReadContentRange(t.Context(), parent.Session.ID, notice.EvidenceRef, 0, 64<<10)
			if err != nil || reference.SessionID != parent.Session.ID || reference.Size != int64(len(raw)) {
				t.Fatalf("missing parent-owned cancellation evidence: %+v %v", reference, err)
			}
			var evidence session.Completion
			if err := json.Unmarshal(raw, &evidence); err != nil {
				t.Fatal(err)
			}
			for _, metadata := range []session.CompletionMetadata{notice.CompletionMetadata, evidence.CompletionMetadata} {
				if metadata.ParentID != parent.Session.ID || metadata.ChildID != child.ID || metadata.TurnID != childRequest.TurnID || metadata.InputID == nil || *metadata.InputID != inputID || metadata.State != session.Cancelled || metadata.Mode != session.ReportNotice || !metadata.FinishedAt.Equal(*terminalChild.FinishedAt) {
					t.Fatalf("report does not identify exact cancelled input: %+v", metadata)
				}
			}
			if evidence.Text != "" || evidence.MessageID != nil || evidence.TextBytes != 0 || evidence.OmittedParts != 0 {
				t.Fatalf("cancellation invented child output: %+v", evidence)
			}
			retained, err := r.Session(t.Context(), child.ID)
			if err != nil || retained.Lifecycle != session.Active {
				t.Fatalf("cancellation deleted/stopped child: %+v %v", retained, err)
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func detachedRunnableUsage(t *testing.T, r *Runtime, owner session.SessionID) int64 {
	t.Helper()
	resources, err := r.Resources(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.SessionID == owner && resource.Kind == session.ResourceRunnableDescendants {
			return resource.Used
		}
	}
	t.Fatal("missing runnable descendant resource")
	return 0
}

func awaitDetachedCompletion(t *testing.T, r *Runtime, parent session.SessionID) session.Mail {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		mails, err := r.ListMail(ctx, parent, session.MailPending, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(mails) != 0 {
			if len(mails) != 1 {
				t.Fatalf("duplicated completion mail: %+v", mails)
			}
			mail, err := r.ReadMail(ctx, parent, mails[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			return mail
		}
		select {
		case <-ctx.Done():
			t.Fatalf("completion publication did not progress: %v", r.Err())
		case <-ticker.C:
		}
	}
}
