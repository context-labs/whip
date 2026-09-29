package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func captureSpec(owner session.SessionID, digest string) *session.ModelCapture {
	body := []byte("exact instructions")
	ref := session.ContentReference{ID: "model_" + session.CaptureDigest(body), SessionID: owner, Digest: session.CaptureDigest(body), Size: int64(len(body)), MediaType: "text/plain"}
	capture := &session.ModelCapture{RequestDigest: digest, Instructions: session.CapturedText{Digest: ref.Digest, Bytes: ref.Size, Status: "available", Chunks: []session.ContentReference{ref}}, Notices: session.CapturedText{Digest: session.CaptureDigest(nil), Status: "available", Chunks: []session.ContentReference{}}, Messages: []session.CapturedMessage{}, ToolsDigest: session.CaptureDigest(nil), ContextComplete: true}
	capture.Seal()
	return capture
}

func TestModelCaptureAtomicReservationExactRetryAndHistoricalAbsence(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state.db"))
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "input")
	claimed := claim(t, s, root.ID)
	spec := attemptRequest(claimed.Turn.ID, "capture")
	spec.Capture = captureSpec(root.ID, spec.Request.RequestDigest)
	execTest(t, s, `CREATE TRIGGER fail_capture BEFORE INSERT ON model_captures BEGIN SELECT RAISE(ABORT,'fixture failure'); END`)
	if _, err := s.ReserveModelAttempt(t.Context(), spec); err == nil || count(t, s, "model_attempts") != 0 || count(t, s, "content_references") != 0 {
		t.Fatal("partial capture reservation", err)
	}
	execTest(t, s, `DROP TRIGGER fail_capture`)
	reserveTest(t, s, spec)
	dispatchTest(t, s, spec.ID)
	reserveTest(t, s, spec)
	if count(t, s, "model_captures") != 1 || count(t, s, "content_references") != 1 {
		t.Fatal("retry duplicated capture")
	}
	got, err := s.ModelInspection(t.Context(), root.ID, spec.ID)
	if err != nil || got.Capture == nil || got.Capture.Instructions.Status != "available" || got.RequestDigest != spec.Request.RequestDigest {
		t.Fatal(got, err)
	}
	spec.Capture.Instructions.Digest = strings.Repeat("b", 64)
	spec.Capture.Seal()
	if _, err := s.ReserveModelAttempt(t.Context(), spec); !errors.Is(err, ErrConflict) {
		t.Fatal("changed evidence retry", err)
	}
	if _, err := s.ModelInspection(t.Context(), "foreign", spec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-owner inspection", err)
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE model_captures SET source_digest='changed'`); err == nil {
		t.Fatal("capture mutated")
	}
	old := attemptRequest(claimed.Turn.ID, "historical")
	reserveTest(t, s, old)
	absent, err := s.ModelInspection(t.Context(), root.ID, old.ID)
	if err != nil || absent.Capture != nil {
		t.Fatal("historical evidence reconstructed", absent, err)
	}
}

func TestModelCaptureQuotaDoesNotRefuseAttemptOrPublishPartialReferences(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state.db"))
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "input")
	claimed := claim(t, s, root.ID)
	execTest(t, s, `INSERT INTO content_bodies VALUES (?,?)`, strings.Repeat("f", 64), session.MaxContentBytes)
	for i := range 16 {
		execTest(t, s, `INSERT INTO content_references VALUES (?,?,?,?,?)`, strings.Repeat("x", i+1), root.ID, strings.Repeat("f", 64), "text/plain", now())
	}
	spec := attemptRequest(claimed.Turn.ID, "quota")
	spec.Capture = captureSpec(root.ID, spec.Request.RequestDigest)
	reserveTest(t, s, spec)
	got, err := s.ModelInspection(t.Context(), root.ID, spec.ID)
	if err != nil || got.Capture.Instructions.Status != "quota" || len(got.Capture.Instructions.Chunks) != 0 || count(t, s, "content_references") != 16 {
		t.Fatal(got, err)
	}
	reserveTest(t, s, spec)
}

func TestModelInspectionUsesExactCommittedCompactionAfterSelectionChanges(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state.db"))
	_, root := create(t, s, nil)
	history := compactionHistoryTest(t, s, root.ID, "history")
	turn := compactionTurnTest(t, s, root.ID, "fold")
	attempt := compactionAttemptTest(t, s, turn.ID, "summary_attempt")
	draft := session.CompactionDraft{ID: "summary", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "exact summary"}
	settled := settleCompactionTest(t, s, attempt.ID, draft)
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCompaction(t.Context(), root.ID, settled.Head.Revision, nil); err != nil {
		t.Fatal(err)
	}
	evidence, err := s.ModelInspection(t.Context(), root.ID, attempt.ID)
	if err != nil || evidence.Compaction == nil || evidence.Compaction.ID != draft.ID || evidence.Compaction.ThroughSequence != 2 {
		t.Fatal(evidence, err)
	}
	page, err := s.TracePage(t.Context(), session.TraceQuery{RootID: root.ID, Limit: 100, MaxBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Items {
		if row.SourceKind == "attempt" && row.SourceID == string(attempt.ID) {
			input, output, err := s.TraceBodies(t.Context(), row, page.Revision)
			if err != nil || input != nil || output == nil || !strings.Contains(*output, "exact summary") {
				t.Fatal(input, output, err)
			}
			return
		}
	}
	t.Fatal("missing attempt trace")
}
