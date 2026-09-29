package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestInputSteeringContinuesTextAndCompleteToolBatchAtEveryDepth(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		for _, child := range []bool{false, true} {
			for _, tools := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/child=%t/tools=%t", engine, child, tools), func(t *testing.T) {
					entered, release := make(chan struct{}), make(chan struct{})
					var calls atomic.Int32
					provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
						if calls.Add(1) == 1 {
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
								return model.Response{}, ctx.Err()
							}
							if tools {
								code := "steering_value=41"
								second := "print(steering_value+1)"
								if engine == session.QuickJS {
									code = "var steeringValue=41;"
									second = "print(steeringValue+1);"
								}
								first := childControlCode(1, code)
								last := childControlCode(2, second)
								first.Parts = append(first.Parts, last.Parts...)
								return first, nil
							}
							return model.Response{Parts: []session.Part{{Type: "text", Text: "initial answer"}}}, nil
						}
						want := 3
						if tools {
							want = 5
						}
						if len(request.Messages) != want {
							return model.Response{}, fmt.Errorf("messages=%d, expected full completed batch then authored steer", len(request.Messages))
						}
						last := request.Messages[len(request.Messages)-1]
						if last.Role != session.User || len(last.Parts) != 2 || last.Parts[0].Text != "also do this" || last.Parts[1].ReferenceID != "design" || string(request.Contents["design"].Data) != "exact attachment" {
							return model.Response{}, fmt.Errorf("lost original parts/content: %+v", last)
						}
						if tools && (request.Messages[2].Role != session.Tool || request.Messages[3].Role != session.Tool || !strings.Contains(request.Messages[3].Parts[0].Result.Output, "42")) {
							return model.Response{}, errors.New("steering interrupted tool batch")
						}
						return model.Response{Parts: []session.Part{{Type: "text", Text: "steered answer"}}}, nil
					})
					r, root := controlRuntime(t, provider, engine, 2)
					owner := root
					if child {
						admitted, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "opening"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "opening"}}})
						if err != nil {
							t.Fatal(err)
						}
						owner = *admitted.Session
					} else {
						submitTest(t, r, owner.ID, "opening")
					}
					select {
					case <-entered:
					case <-t.Context().Done():
						t.Fatal(t.Context().Err())
					}
					opening := waitTest(t, r, "opening", func(a store.Admission) bool { return a.Turn != nil })
					if _, err := r.PutContent(t.Context(), owner.ID, "design", "text/plain", []byte("exact attachment")); err != nil {
						t.Fatal(err)
					}
					request := store.Submission{SessionID: owner.ID, Source: session.UserInput, Delivery: session.DeliverySteer, TargetTurnID: &opening.Turn.ID, Parts: []session.Part{{Type: "text", Text: "also do this"}, {Type: "content", ReferenceID: "design"}}, DesignContext: &session.DesignContext{ContextAttachmentID: "design", Elements: []session.DesignContextElement{{Label: "Save", Selector: "button"}}, ElementCount: 1, PageTitle: "display only"}}
					steer, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "steer"}, request)
					if err != nil {
						t.Fatal(err)
					}
					if steer.Input.TurnID != nil || steer.Input.Steering.Consumed {
						t.Fatal("consumed while model was active", steer)
					}
					close(release)
					done := waitTest(t, r, "steer", terminal)
					if done.Turn.ID != opening.Turn.ID || done.Turn.State != session.Succeeded || !done.Input.Steering.Consumed || calls.Load() != 2 {
						t.Fatal("steering opened a new turn or failed", done, calls.Load())
					}
					history, err := r.History(t.Context(), owner.ID, 0, 20)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, m := range history {
						if m.InputID != nil && *m.InputID == steer.Input.ID {
							found = true
							if m.OpeningInput || m.DesignContext == nil {
								t.Fatal(m)
							}
						}
					}
					if !found {
						t.Fatal("no authored steering message")
					}
				})
			}
		}
	}
}
