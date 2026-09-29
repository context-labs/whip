package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type designTranscript struct {
	*flakyTranscript
	message session.Message
}

func (s *designTranscript) History(_ context.Context, _ session.SessionID, after int64, _ int) ([]session.Message, error) {
	if after >= s.message.Sequence {
		return nil, nil
	}
	return []session.Message{s.message}, nil
}

func (*designTranscript) ReadContent(_ context.Context, owner session.SessionID, id string, _ int64) (session.ContentReference, []byte, error) {
	data := []byte("Literal selected evidence")
	return session.ContentReference{SessionID: owner, ID: id, MediaType: "text/plain", Size: int64(len(data))}, data, nil
}

func TestDesignContextIsDisplayOnlyWhileProviderReceivesExactEvidence(t *testing.T) {
	parts := []session.Part{{Type: "text", Text: "My exact request"}, {Type: "content", ReferenceID: "evidence"}}
	design := &session.DesignContext{ContextAttachmentID: "evidence", PageTitle: "DISPLAY_ONLY_SECRET", Elements: []session.DesignContextElement{}}
	presentation, err := design.Presentation(parts)
	if err != nil {
		t.Fatal(err)
	}
	transcript := &designTranscript{flakyTranscript: &flakyTranscript{calls: 1}, message: session.Message{ID: "input_message", SessionID: "owner", Role: session.User, Sequence: 1, Parts: parts, DesignContext: presentation}}
	calls := 0
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		calls++
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("DISPLAY_ONLY_SECRET")) || bytes.Contains(raw, []byte("context_part_index")) || !reflect.DeepEqual(request.Messages[0].Parts, parts) || string(request.Contents["evidence"].Data) != "Literal selected evidence" {
			t.Fatal("presentation leaked or input bytes changed", string(raw))
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "Done"}}}, nil
	})
	r, err := New(provider, transcript, transcript, transcript, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || calls != 1 {
		t.Fatalf("outcome=%+v failure=%v err=%v calls=%d", outcome, outcome.Failure, err, calls)
	}
}
