package runtime

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestImageHistoryCompactsThroughCanonicalStoreAndPreservesContent(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 768, 768))
	state := uint32(1)
	for i := 0; i < len(pixels.Pix); i += 4 {
		for offset := range 3 {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			pixels.Pix[i+offset] = byte(state)
		}
		pixels.Pix[i+3] = 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	var helpers, ordinary atomic.Int32
	r := openTest(t, t.TempDir(), providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "title" {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "Images"}}}, nil
		}
		if request.Purpose == "compaction" {
			helpers.Add(1)
			return model.Response{Parts: []session.Part{{Type: "text", Text: "older image facts"}}, Usage: session.ModelUsage{Input: new(int64(17))}}, nil
		}
		ordinary.Add(1)
		return model.Response{Parts: []session.Part{{Type: "text", Text: "observed"}}}, nil
	}))
	owner := createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second", "third"} {
		ref, err := r.PutContent(t.Context(), owner.ID, id, "image/png", encoded.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: id}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "Inspect this image"}, {Type: "content", ReferenceID: ref.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		done := waitTest(t, r, id, terminal)
		if done.Turn.State != session.Succeeded {
			t.Fatal("image turn failed", id, done.Turn.Failure)
		}
	}
	if ordinary.Load() != 3 || helpers.Load() != 1 {
		t.Fatal("wrong dispatch counts", ordinary.Load(), helpers.Load())
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 6 {
		t.Fatal(history, err)
	}
	for i, id := range []string{"first", "second", "third"} {
		if history[2*i].Parts[1].ReferenceID != id {
			t.Fatal("raw reference changed", history[2*i])
		}
		_, body, err := r.ReadContent(t.Context(), owner.ID, id, session.MaxContentBytes)
		if err != nil || !bytes.Equal(body, encoded.Bytes()) {
			t.Fatal("raw body changed", id, err)
		}
	}
	head, err := r.store.ContextHead(t.Context(), owner.ID)
	if err != nil || head.CompactionID == nil {
		t.Fatal(head, err)
	}
	fold, err := r.store.Compaction(t.Context(), owner.ID, *head.CompactionID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := r.store.ModelAttempt(t.Context(), fold.AttemptID)
	if err != nil || attempt.State != session.AttemptSucceeded || attempt.Result == nil || attempt.Result.Usage.Input == nil || *attempt.Result.Usage.Input != 17 {
		t.Fatal(attempt, err)
	}
}
