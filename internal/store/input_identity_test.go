package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestInputIdentitySurvivesAcknowledgementLossClaimAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child").Session
	if child == nil {
		t.Fatal("missing child")
	}
	first := session.RequestIdentity{ClientID: "first-client", RequestID: "same-request"}
	second := session.RequestIdentity{ClientID: "second-client", RequestID: first.RequestID}
	request := Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "same text"}}}
	// Discard both acknowledgements. The page must identify each exact request
	// from durable facts, before any client can attach an acknowledged input ID.
	for _, identity := range []session.RequestIdentity{first, second} {
		if _, err := s.Admit(t.Context(), identity, request); err != nil {
			t.Fatal(err)
		}
	}
	request.SessionID = child.ID
	if _, err := s.Admit(t.Context(), first, request); !errors.Is(err, ErrConflict) {
		t.Fatal("same identity acquired a different owner", err)
	}
	execTest(t, s, `INSERT INTO inputs(id,session_id,source,kind,parts,created_at)
 VALUES('internal_input',?,'agent','prompt','[{"type":"text","text":"same text"}]',?)`, root.ID, now())
	checkPage := func(store *Store) []session.InputSummary {
		t.Helper()
		page, err := store.InputPage(t.Context(), root.ID, "all", 0, 100)
		if err != nil || len(page.Items) != 3 {
			t.Fatal(page, err)
		}
		if page.Items[0].Identity == nil || *page.Items[0].Identity != first ||
			page.Items[1].Identity == nil || *page.Items[1].Identity != second || page.Items[2].Identity != nil {
			t.Fatal("receipt projection conflated inputs", page.Items)
		}
		childPage, err := store.InputPage(t.Context(), child.ID, "all", 0, 100)
		if err != nil || len(childPage.Items) != 1 || childPage.Items[0].SessionID != child.ID ||
			childPage.Items[0].Identity == nil || childPage.Items[0].Identity.RequestID != "child" {
			t.Fatal("receipt projection crossed owners", childPage, err)
		}
		return page.Items
	}
	items := checkPage(s)
	turn := claim(t, s, root.ID).Turn
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{outputDraft("answer", "reply")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if s.Identity() == "" || checkPage(s)[0].ID != items[0].ID {
		t.Fatal("restart changed input identity")
	}
	messages, err := s.History(t.Context(), root.ID, 0, 100)
	if err != nil || len(messages) != 2 || messages[0].InputIdentity == nil ||
		*messages[0].InputIdentity != first || messages[1].InputIdentity != nil {
		t.Fatal("canonical history lost the authored identity", messages, err)
	}
	_, page, err := s.HistoryPage(t.Context(), root.ID, 0, 100, nil)
	if err != nil || len(page) != 2 || page[0].InputIdentity == nil || *page[0].InputIdentity != first {
		t.Fatal(page, err)
	}
	metadata, err := s.HistoryMetadata(t.Context(), root.ID, 0, messages[1].Sequence, 100)
	if err != nil || len(metadata.Items) != 2 || metadata.Items[0].InputIdentity == nil || *metadata.Items[0].InputIdentity != first {
		t.Fatal(metadata, err)
	}
	fork := forkTest(t, s, forkRequestTest(t, s, root.ID, "copy", messages[1].Sequence))
	copied, err := s.History(t.Context(), fork.Root.ID, 0, 100)
	if err != nil || len(copied) != 2 || copied[0].InputIdentity != nil || copied[0].InputID != nil || copied[0].Source == nil {
		t.Fatal("fork borrowed source input identity", copied, err)
	}
}
