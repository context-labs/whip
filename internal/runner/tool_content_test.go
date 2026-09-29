package runner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type imageExecutor struct{ wrong bool }

func (imageExecutor) Instructions(context.Context, session.Turn, session.Instructions) (string, error) {
	return "", nil
}

func (e imageExecutor) Execute(_ context.Context, _ session.Turn, _ session.MessageID, call session.ToolCall) ([]session.Part, error) {
	id := call.ID
	if e.wrong {
		id = "wrong"
	}
	return []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: id, Output: "canonical output"}}, {Type: "content", ReferenceID: "owned-image"}}, nil
}

type imageReader struct{ reads int }

func (r *imageReader) ReadContent(_ context.Context, owner session.SessionID, id string, limit int64) (session.ContentReference, []byte, error) {
	r.reads++
	if owner != "owner" || id != "owned-image" || limit < 5 {
		return session.ContentReference{}, nil, errors.New("wrong content authority")
	}
	return session.ContentReference{ID: id, SessionID: owner, MediaType: "image/jpeg", Size: 5}, []byte("image"), nil
}

func TestCommittedToolImagesHydrateBeforeNextModelCall(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "wrong call"}[wrong], func(t *testing.T) {
			transcript := &flakyTranscript{calls: 1}
			reader := &imageReader{}
			calls := 0
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				calls++
				if calls == 1 {
					return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
				}
				last := request.Messages[len(request.Messages)-1]
				if last.Role != session.Tool || len(last.Parts) != 2 || last.Parts[0].Result.Output != "canonical output" || string(request.Contents["owned-image"].Data) != "image" {
					t.Fatal("current turn lost canonical image", request)
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "observed"}}}, nil
			})
			runner, err := New(provider, transcript, transcript, reader, imageExecutor{wrong: wrong}, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := runner.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner"}, session.Configuration{})
			if wrong {
				if err == nil || calls != 1 || reader.reads != 0 {
					t.Fatal("wrong committed call escaped", calls, reader.reads, err)
				}
			} else if err != nil || outcome.State != session.Succeeded || calls != 2 || reader.reads != 1 {
				t.Fatal(outcome, calls, reader.reads, err)
			}
		})
	}
}
