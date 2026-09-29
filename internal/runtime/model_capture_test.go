package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestModelCaptureExistsBeforeDispatchAndKeepsCapturedInstructions(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			var runtime *Runtime
			provider := captureProvider{execute: func(ctx context.Context, request model.Request) (model.Response, error) {
				attempts, err := runtime.ModelAttempts(ctx, request.TurnID, "", 100)
				if err != nil || len(attempts) != 1 {
					t.Fatal(attempts, err)
				}
				evidence, err := runtime.ModelInspection(ctx, request.SessionID, attempts[0].ID)
				if err != nil || evidence.Capture == nil {
					t.Fatal(evidence, err)
				}
				var body strings.Builder
				for _, ref := range evidence.Capture.Instructions.Chunks {
					_, data, err := runtime.ReadContent(ctx, request.SessionID, ref.ID, session.MaxContentBytes)
					if err != nil {
						t.Fatal(err)
					}
					body.Write(data)
				}
				if body.String() != strings.TrimSuffix(request.Instructions, request.Notices) {
					t.Fatal("capture differs from dispatched input")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}}
			directory := t.TempDir()
			runtime = openTest(t, directory, provider)
			root := createEngineSession(t, runtime, engine)
			if err := runtime.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, runtime, root.ID, "capture")
			got := waitTest(t, runtime, "capture", terminal)
			if got.Turn.State != session.Succeeded {
				t.Fatal(got.Turn)
			}
			exported, err := runtime.ExportTrace(t.Context(), root.ID, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, body, err := runtime.ReadContent(t.Context(), root.ID, exported.Reference.ID, session.MaxContentBytes)
			if err != nil || !bytes.Contains(body, []byte("captured_instructions_only")) {
				t.Fatal("missing exact captured instructions in export", err)
			}
			attempts, err := runtime.ModelAttempts(t.Context(), got.Turn.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			before, err := runtime.ModelInspection(t.Context(), root.ID, attempts[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openTest(t, directory, model.Scripted{})
			after, err := reopened.ModelInspection(t.Context(), root.ID, attempts[0].ID)
			if err != nil || after.Capture == nil || after.Capture.SourceDigest != before.Capture.SourceDigest {
				t.Fatal("capture lost across restart", after, err)
			}
		})
	}
}

type captureProvider struct {
	execute func(context.Context, model.Request) (model.Response, error)
}

func (p captureProvider) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	prepared.Execute = func(ctx context.Context, _ func(model.Chunk)) (model.Response, error) { return p.execute(ctx, request) }
	return prepared, err
}

func TestModelCapturePublicationFailureAndChunkBounds(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	root := createTest(t, r)
	request := model.Request{Instructions: strings.Repeat("x", session.MaxContentBytes-1) + "😃", Notices: ""}
	capture := model.Inspect(request)
	capture.RequestDigest = strings.Repeat("a", 64)
	published, err := r.PublishModelCapture(t.Context(), root.ID, capture)
	if err != nil || len(published.Instructions.Chunks) != 2 {
		t.Fatal(published, err)
	}
	for _, chunk := range published.Instructions.Chunks {
		if _, err := r.store.RegisterContent(t.Context(), chunk); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.readCapturedText(t.Context(), root.ID, published.Instructions, session.MaxModelCaptureBytes)
	if err != nil || got != request.Instructions {
		t.Fatal("chunked UTF8 changed", err)
	}
	// A storage failure is visible evidence; it never claims an accessible body.
	if err := os.Rename(filepath.Join(directory, "artifacts", "sha256"), filepath.Join(directory, "artifacts", "saved")); err != nil {
		t.Fatal(err)
	}
	capture = model.Inspect(model.Request{Instructions: "new body"})
	capture.RequestDigest = strings.Repeat("a", 64)
	published, err = r.PublishModelCapture(t.Context(), root.ID, capture)
	if err != nil || published.Instructions.Status != "storage_error" || len(published.Instructions.Chunks) != 0 || published.Notices.Status != "available" {
		t.Fatal(published, err)
	}
}
