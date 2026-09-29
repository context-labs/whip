package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestTraceExportScopedDeduplicatedHistoricalWithoutWorkers(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	owner := createTest(t, r)
	submitTest(t, r, owner.ID, "input")
	work, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.ExportTrace(t.Context(), owner.ID, "", nil)
	if err != nil || first.Spans != 1 || first.Traces != 1 || first.Reference.SessionID != owner.ID {
		t.Fatal(first, err)
	}
	_, body, err := r.ReadContent(t.Context(), owner.ID, first.Reference.ID, session.MaxContentBytes)
	if err != nil || !bytes.Contains(body, []byte(`"whip.span.in_progress"`)) {
		t.Fatal(string(body), err)
	}
	repeated, err := r.ExportTrace(t.Context(), owner.ID, "", &first.Revision)
	if err != nil || repeated.Reference.ID != first.Reference.ID {
		t.Fatal(repeated, err)
	}
	if len(r.kernels) != 0 {
		t.Fatal("trace hydrated worker")
	}
	foreign := createTest(t, r)
	if _, _, err := r.ReadContent(t.Context(), foreign.ID, first.Reference.ID, session.MaxContentBytes); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := r.store.Finish(t.Context(), work.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ExportTrace(t.Context(), owner.ID, "", &first.Revision); !errors.Is(err, store.ErrConflict) {
		t.Fatal("export mixed revisions", err)
	}
	finished, err := r.ExportTrace(t.Context(), owner.ID, "", nil)
	if err != nil || finished.Reference.ID == first.Reference.ID {
		t.Fatal(finished, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, directory, model.Scripted{})
	historical, err := reopened.ExportTrace(t.Context(), owner.ID, "", nil)
	if err != nil || historical.Reference.ID != finished.Reference.ID || len(reopened.kernels) != 0 {
		t.Fatal(historical, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reopened.ExportTrace(ctx, owner.ID, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := reopened.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ExportTrace(t.Context(), owner.ID, "", nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("export resurrected deleted owner", err)
	}
	page, err := reopened.TracePage(t.Context(), session.TraceQuery{RootID: owner.ID, Limit: 10, MaxBytes: 4096})
	if err != nil || len(page.Items) != 1 || page.Items[0].Span != nil {
		t.Fatal(page, err)
	}
}
