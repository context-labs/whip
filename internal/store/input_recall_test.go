package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func recallFixtureInput(t *testing.T, s *Store, owner session.SessionID, id string, source session.InputSource, kind session.InputKind, text string) {
	t.Helper()
	parts := []session.Part{}
	if kind == session.PromptInput {
		parts = append(parts, session.Part{Type: "text", Text: text})
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `INSERT INTO inputs(id,session_id,source,kind,parts,created_at) VALUES(?,?,?,?,?,1)`, id, owner, source, kind, string(raw))
}

func TestRecentInputTextCrossOwnerExactCursorAndExcludedWork(t *testing.T) {
	s := fresh(t)
	_, first := create(t, s, nil)
	_, second := create(t, s, nil)
	child := controlChild(t, s, first.ID, "child")
	const base = int64(9007199254740992)
	execTest(t, s, "UPDATE sqlite_sequence SET seq=? WHERE name='inputs'", base)
	older, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "recall", RequestID: "older"}, Submission{SessionID: first.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: " first exact text \n"}}})
	if err != nil {
		t.Fatal(err)
	}
	newer := submit(t, s, second.ID, "second")
	recallFixtureInput(t, s, child.Session.ID, "injected", session.AgentInput, session.PromptInput, "not a human draft")
	recallFixtureInput(t, s, second.ID, "maintenance", session.UserInput, session.CompactInput, "")
	page, err := s.RecentInputText(t.Context(), 0, 2)
	if err != nil || len(page.Items) != 0 || page.NextCursor == nil || *page.NextCursor != base+3 || page.ScannedCount != 2 || page.SkippedCount != 0 {
		t.Fatal(page, err)
	}
	// A concurrent new admission cannot move into the older keyset page.
	submit(t, s, child.Session.ID, "new-child-text")
	page, err = s.RecentInputText(t.Context(), *page.NextCursor, 2)
	if err != nil || len(page.Items) != 2 || page.Items[0].InputID != newer.Input.ID || page.Items[1].InputID != older.Input.ID || page.Items[1].Text != " first exact text \n" || page.Items[1].Ordinal != base+1 {
		t.Fatal(page, err)
	}
	latest, err := s.RecentInputText(t.Context(), 0, 500)
	if err != nil || len(latest.Items) != 3 || latest.Items[0].SessionID != child.Session.ID || latest.NextCursor != nil || count(t, s, "turns") != 0 {
		t.Fatal(latest, err)
	}
	if err := s.DeleteSubtree(t.Context(), second.ID); err != nil {
		t.Fatal(err)
	}
	latest, err = s.RecentInputText(t.Context(), 0, 500)
	if err != nil || len(latest.Items) != 2 {
		t.Fatal(latest, err)
	}
}

func TestRecentInputTextBoundsNeverReturnPartialTextOrStallCursor(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	older := submit(t, s, owner.ID, "older")
	for i := range 5 {
		recallFixtureInput(t, s, owner.ID, fmt.Sprintf("oversize-%d", i), session.UserInput, session.PromptInput, strings.Repeat("x", 900000))
	}
	page, err := s.RecentInputText(t.Context(), 0, 500)
	if err != nil || len(page.Items) != 0 || page.ScannedCount != 4 || page.SkippedCount != 4 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	page, err = s.RecentInputText(t.Context(), *page.NextCursor, 500)
	if err != nil || len(page.Items) != 1 || page.Items[0].InputID != older.Input.ID || page.SkippedCount != 1 || page.NextCursor != nil {
		t.Fatal(page, err)
	}
	for i := range 3 {
		recallFixtureInput(t, s, owner.ID, fmt.Sprintf("exact-%d", i), session.UserInput, session.PromptInput, strings.Repeat("界", 50000))
	}
	page, err = s.RecentInputText(t.Context(), 0, 500)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Text) != 150000 || page.NextCursor == nil || page.ScannedCount != 1 || page.SkippedCount != 0 {
		t.Fatal(page, err)
	}
	second, err := s.RecentInputText(t.Context(), *page.NextCursor, 500)
	if err != nil || len(second.Items) != 1 || second.Items[0].InputID == page.Items[0].InputID || len(second.Items[0].Text) != 150000 {
		t.Fatal(second, err)
	}
}

func TestRecentInputTextProjectsOnlyFirstTextWithoutPayloadAuthority(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	raw := `[{"type":"content","ref":"foreign-cannot-be-recalled"},{"type":"text","text":"visible"},{"type":"text","text":"second part"}]`
	execTest(t, s, `INSERT INTO inputs(id,session_id,source,kind,parts,created_at) VALUES('projection',?,'user','prompt',?,1)`, owner.ID, raw)
	page, err := s.RecentInputText(t.Context(), 0, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Text != "visible" {
		t.Fatal(page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "foreign-cannot-be-recalled") || strings.Contains(string(encoded), "second part") {
		t.Fatal(string(encoded))
	}
	for _, tc := range []struct {
		before int64
		limit  int
	}{{-1, 1}, {0, 0}, {0, 501}} {
		if _, err := s.RecentInputText(t.Context(), tc.before, tc.limit); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
