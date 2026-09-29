package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"reflect"
	"testing"
	"testing/synctest"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type compactionContents struct {
	body  []byte
	reads int
}

func (r *compactionContents) ContentReference(_ context.Context, owner session.SessionID, id string) (session.ContentReference, error) {
	if owner != "owner" || (id != "first" && id != "second" && id != "third") {
		return session.ContentReference{}, errors.New("foreign reference")
	}
	return session.ContentReference{ID: id, SessionID: owner, MediaType: "image/png", Size: int64(len(r.body))}, nil
}

func (r *compactionContents) ReadContent(ctx context.Context, owner session.SessionID, id string, limit int64) (session.ContentReference, []byte, error) {
	r.reads++
	ref, err := r.ContentReference(ctx, owner, id)
	if err != nil || ref.Size > limit {
		return session.ContentReference{}, nil, errors.New("content exceeds read limit")
	}
	return ref, r.body, nil
}

func contextPNG(t *testing.T) []byte {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, 768, 768))
	state := uint32(1)
	for i := 0; i < len(value.Pix); i += 4 {
		for offset := range 3 {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			value.Pix[i+offset] = byte(state)
		}
		value.Pix[i+3] = 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, value); err != nil {
		t.Fatal(err)
	}
	body := encoded.Bytes()
	if len(body)*3 <= session.MaxContentBytes || len(body)*2 > session.MaxContentBytes {
		t.Fatal("fixture must require incremental image coverage", len(body))
	}
	return body
}

func TestCompactionFitsHydratedContentBeforeDispatchAndPreservesRawImages(t *testing.T) {
	reader := &compactionContents{body: contextPNG(t)}
	ledger := newCompactionLedger(compactionMessages(7, 2))
	for i, id := range []string{"first", "second", "third"} {
		ledger.raw[i*2].Parts = append(ledger.raw[i*2].Parts, session.Part{Type: "content", ReferenceID: id})
	}
	before := append([]session.Message(nil), ledger.raw...)
	ledger.loseReply = true
	helpers, ordinary := 0, 0
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		contentBytes := 0
		partBytes := len(request.Instructions)
		for _, message := range request.Messages {
			raw, err := json.Marshal(message.Parts)
			if err != nil {
				t.Fatal(err)
			}
			partBytes += len(raw)
		}
		for _, content := range request.Contents {
			contentBytes += len(content.Data)
		}
		if contentBytes+partBytes > maxContextBytes || len(request.Messages) > maxContextMessages {
			t.Fatal("oversized provider request dispatched", contentBytes, len(request.Messages))
		}
		if request.Purpose == "compaction" {
			helpers++
			if contentBytes == 0 {
				t.Fatal("helper lost image evidence")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "image facts"}}, Usage: session.ModelUsage{Input: new(int64(17)), Output: new(int64(3))}}, nil
		}
		ordinary++
		if contentBytes != 0 {
			t.Fatal("covered old image still replayed")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "answer"}}}, nil
	}), ledger, ledger, reader, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || helpers != 2 || ordinary != 1 {
		t.Fatal("foldable image history failed", outcome, err, helpers, ordinary)
	}
	if !reflect.DeepEqual(before, ledger.raw[:len(before)]) || len(ledger.specs) != 3 || len(ledger.values) != 2 {
		t.Fatal("raw image history changed or lost ACK replayed provider")
	}
	for _, spec := range ledger.specs {
		if spec.Request.Purpose == "compaction" && (ledger.writes[spec.ID] != 2 || *ledger.results[spec.ID].Usage.Input != 17) {
			t.Fatal("helper accounting did not settle exactly", spec.ID)
		}
	}
}

func TestCompactionRejectsIndivisibleImageInputWithoutReadingOrDispatch(t *testing.T) {
	reader := &compactionContents{body: contextPNG(t)}
	ledger := newCompactionLedger(compactionMessages(1, 1))
	for _, id := range []string{"first", "second", "third"} {
		ledger.raw[0].Parts = append(ledger.raw[0].Parts, session.Part{Type: "content", ReferenceID: id})
	}
	before := append([]session.Message(nil), ledger.raw...)
	dispatched := 0
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		dispatched++
		return model.Response{}, errors.New("must not dispatch")
	}), ledger, ledger, reader, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Failed || outcome.Failure == nil || reader.reads != 0 || dispatched != 0 || len(ledger.specs) != 0 || len(ledger.values) != 0 || !reflect.DeepEqual(before, ledger.raw) {
		t.Fatal("indivisible input was read, mutated or dispatched", outcome, err, reader.reads, dispatched)
	}
}

type imageCompactionExecutor struct {
	ledger *compactionLedger
	calls  int
}

func (*imageCompactionExecutor) Instructions(context.Context, session.Turn, session.Instructions) (string, error) {
	return "", nil
}

func (e *imageCompactionExecutor) Execute(_ context.Context, turn session.Turn, _ session.MessageID, call session.ToolCall) ([]session.Part, error) {
	e.calls++
	parts := []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: call.ID, Output: "completed effect"}}, {Type: "content", ReferenceID: "third"}}
	e.ledger.recordMessage(turn.ID, "tool-result", session.Tool, parts)
	return parts, nil
}

func TestCompactionAfterCommittedImageToolBatchDoesNotReplayEffect(t *testing.T) {
	reader := &compactionContents{body: contextPNG(t)}
	ledger := newCompactionLedger(compactionMessages(7, 2))
	for i, id := range []string{"first", "second"} {
		ledger.raw[i*2].Parts = append(ledger.raw[i*2].Parts, session.Part{Type: "content", ReferenceID: id})
	}
	ledger.loseReply = true
	executor := &imageCompactionExecutor{ledger: ledger}
	ordinary, helpers := 0, 0
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "compaction" {
			helpers++
			return model.Response{Parts: []session.Part{{Type: "text", Text: "older image facts"}}}, nil
		}
		ordinary++
		if ordinary == 1 {
			return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "effect", Name: "execute", Arguments: []byte(`{}`)}}}}, nil
		}
		if len(request.Contents) != 1 || len(request.Contents["third"].Data) != len(reader.body) {
			t.Fatal("next request lost newly committed image or retained covered images")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "observed"}}}, nil
	}), ledger, ledger, reader, executor, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || executor.calls != 1 || ordinary != 2 || helpers != 1 || len(ledger.specs) != 3 {
		t.Fatal("image context handling replayed or lost effect", outcome, err, executor.calls, ordinary, helpers)
	}
}

func TestImageCompactionAdmissionAndSettlementFailuresNeverReplay(t *testing.T) {
	body := contextPNG(t)
	for _, failure := range []string{"admission", "settlement"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reader := &compactionContents{body: body}
				ledger := newCompactionLedger(compactionMessages(7, 2))
				for i, id := range []string{"first", "second", "third"} {
					ledger.raw[i*2].Parts = append(ledger.raw[i*2].Parts, session.Part{Type: "content", ReferenceID: id})
				}
				before := append([]session.Message(nil), ledger.raw...)
				if failure == "admission" {
					ledger.reserveErr = errors.New("rejected budget")
				} else {
					ledger.settleErr = errors.New("SQL write rolled back")
				}
				calls := 0
				r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
					calls++
					if request.Purpose != "compaction" {
						t.Fatal("ordinary request bypassed failure")
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "image facts"}}, Usage: session.ModelUsage{Input: new(int64(17))}}, nil
				}), ledger, ledger, reader, nil, nil, nil, ledger, nil)
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
				wantCalls := 0
				if failure == "settlement" {
					wantCalls = 1
				}
				if outcome.State != session.Failed || outcome.Failure == nil || calls != wantCalls || len(ledger.values) != 0 || ledger.head.CompactionID != nil || !reflect.DeepEqual(before, ledger.raw) {
					t.Fatal("failure mutated coverage or replayed helper", outcome, err, calls)
				}
			})
		})
	}
}

func TestContextContentCountsOccurrencesAndCombinedTextBeforeHydration(t *testing.T) {
	reader := &compactionContents{body: contextPNG(t)}
	r := &Runner{content: reader}
	for _, parts := range [][]session.Part{
		{{Type: "content", ReferenceID: "first"}, {Type: "content", ReferenceID: "first"}, {Type: "content", ReferenceID: "first"}},
		{{Type: "content", ReferenceID: "first"}, {Type: "text", Text: string(bytes.Repeat([]byte("t"), maxContextBytes-len(reader.body)))}},
	} {
		request := model.Request{SessionID: "owner", Messages: []model.Message{{Role: session.User, Parts: parts}}}
		if _, err := r.contentBudget(t.Context(), &request); !errors.Is(err, errContextLimit) || reader.reads != 0 {
			t.Fatal("request material undercounted", err, reader.reads)
		}
	}
}

func TestManualImageCompactionRetainsOwnerForFinalProjection(t *testing.T) {
	reader := &compactionContents{body: contextPNG(t)}
	ledger := newCompactionLedger(compactionMessages(7, 2))
	ledger.raw[0].Parts = append(ledger.raw[0].Parts, session.Part{Type: "content", ReferenceID: "first"})
	// A retained tail image exercises the final post-compaction projection too.
	ledger.raw[12].Parts = append(ledger.raw[12].Parts, session.Part{Type: "content", ReferenceID: "second"})
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		return model.Response{Parts: []session.Part{{Type: "text", Text: "facts"}}}, nil
	}), ledger, ledger, reader, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "compact", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || len(ledger.values) != 1 {
		t.Fatal(outcome, err)
	}
}

func TestPinnedImageBytesCannotMakeAnExpandingSummaryLookEffective(t *testing.T) {
	reader := &compactionContents{body: contextPNG(t)}
	ledger := newCompactionLedger(compactionMessages(1, 4))
	ledger.raw[0].Parts = append(ledger.raw[0].Parts, session.Part{Type: "content", ReferenceID: "first"})
	ledger.raw[1].Parts[0].Text = string(bytes.Repeat([]byte("important evidence "), 50))
	before := append([]session.Message(nil), ledger.raw...)
	calls := 0
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		return model.Response{Parts: []session.Part{{Type: "text", Text: string(bytes.Repeat([]byte("expanded "), 300))}}, Usage: session.ModelUsage{Input: new(int64(17))}}, nil
	}), ledger, ledger, reader, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := r.selection(t.Context(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	folds := 0
	_, err = r.foldToBoundary(t.Context(), session.Turn{ID: "compact", SessionID: "owner"}, session.Configuration{}, selection, 2, &folds)
	if err == nil || calls != 1 || len(ledger.values) != 0 || ledger.head.CompactionID != nil || len(ledger.results) != 1 || !reflect.DeepEqual(before, ledger.raw) {
		t.Fatal("pinned image hid expanding summary", err, calls, ledger.values)
	}
}
