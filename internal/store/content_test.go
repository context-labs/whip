package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func contentReference(owner session.SessionID, id, text string) session.ContentReference {
	digest := sha256.Sum256([]byte(text))
	return session.ContentReference{ID: id, SessionID: owner, Digest: hex.EncodeToString(digest[:]), Size: int64(len(text)), MediaType: "text/plain"}
}

func TestContentQuotaIsSharedAcrossStoreConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	other := openTest(t, path)
	_, owner := create(t, s, nil)
	for i := range 15 {
		ref := contentReference(owner.ID, fmt.Sprintf("existing-%d", i), "shared digest")
		ref.Size = session.MaxContentBytes
		if _, err := s.RegisterContent(t.Context(), ref); err != nil {
			t.Fatal(err)
		}
	}
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for i, connection := range []*Store{s, other} {
		workers.Go(func() {
			ref := contentReference(owner.ID, fmt.Sprintf("concurrent-%d", i), "shared digest")
			ref.Size = session.MaxContentBytes
			_, err := connection.RegisterContent(t.Context(), ref)
			results <- err
		})
	}
	workers.Wait()
	close(results)
	successes, limited := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrLimit):
			limited++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || limited != 1 || count(t, s, "content_references") != 16 {
		t.Fatalf("quota escaped: successes=%d limited=%d", successes, limited)
	}
}

func TestContentAdmissionScopeAndAtomicity(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	_, other := create(t, s, nil)
	reference, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "reference", "bytes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{reference.ID, reference.Digest, "absent"} {
		identity := session.RequestIdentity{ClientID: "test", RequestID: id}
		parts := []session.Part{{Type: "content", ReferenceID: id}}
		if _, err := s.Admit(t.Context(), identity, Submission{SessionID: other.ID, Source: session.UserInput, Parts: parts}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign/missing content admitted: %v", err)
		}
		if _, err := s.Admission(t.Context(), identity); !errors.Is(err, ErrNotFound) {
			t.Fatal("denied input retained a receipt", err)
		}
	}
	identity := session.RequestIdentity{ClientID: "test", RequestID: "allowed"}
	request := Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "content", ReferenceID: reference.ID}}}
	first, err := s.Admit(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Admit(t.Context(), identity, request)
	if err != nil || first.Input.ID != retry.Input.ID {
		t.Fatal("admission retry changed identity", err)
	}
	if _, err := s.Claim(t.Context(), other.ID); !errors.Is(err, ErrNoWork) {
		t.Fatal("denied recipient acquired queued work", err)
	}
}

func TestContentReferencesProtectEveryTranscriptWrite(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	_, other := create(t, s, nil)
	reference, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "reference", "body"))
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, other.ID, "input")
	turn := claim(t, s, other.ID).Turn
	draft := session.MessageDraft{ID: "message", Role: session.Assistant, Parts: []session.Part{{Type: "content", ReferenceID: reference.ID}}}
	if _, err := s.AppendMessage(t.Context(), turn.ID, draft); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign reference appended", err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign reference completed turn", err)
	}
	stored, err := s.Turn(t.Context(), turn.ID)
	if err != nil || stored.State != session.Running {
		t.Fatal("denied completion changed turn", err)
	}
	history, err := s.History(t.Context(), other.ID, 0, 100)
	if err != nil || len(history) != 1 {
		t.Fatalf("denied output retained a message: %+v %v", history, err)
	}
}

func TestContentRegistrationRollbackIdentityAndSharedBodies(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	_, other := create(t, s, nil)
	draft := contentReference(owner.ID, "reference", "same bytes")
	execTest(t, s, `CREATE TRIGGER injected_content BEFORE INSERT ON content_references BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.RegisterContent(t.Context(), draft); err == nil {
		t.Fatal("injected write succeeded")
	}
	if count(t, s, "content_bodies") != 0 || count(t, s, "content_references") != 0 {
		t.Fatal("partial content registration committed")
	}
	execTest(t, s, "DROP TRIGGER injected_content")
	first, err := s.RegisterContent(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.RegisterContent(t.Context(), draft)
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("content retry changed metadata", err)
	}
	if _, err := s.RegisterContent(t.Context(), contentReference(owner.ID, draft.ID, "different")); !errors.Is(err, ErrConflict) {
		t.Fatal("reference identity changed", err)
	}
	if _, err := s.RegisterContent(t.Context(), contentReference(other.ID, "other-reference", "same bytes")); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "content_bodies") != 1 || count(t, s, "content_references") != 2 {
		t.Fatal("body metadata duplicated")
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.ContentReferenced(t.Context(), draft.Digest); err != nil || !exists {
		t.Fatal("surviving reference lost access", err)
	}
	if _, err := s.ContentReference(t.Context(), owner.ID, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted owner retained access", err)
	}
	if err := s.DeleteSubtree(t.Context(), other.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneUnusedContent(t.Context()); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "content_bodies") != 0 {
		t.Fatal("unreferenced metadata retained")
	}
}
