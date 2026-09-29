package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func designSubmission(t *testing.T, s *Store, owner session.SessionID) Submission {
	t.Helper()
	text := contentReference(owner, "context", "<design_context>original evidence</design_context>")
	image := contentReference(owner, "screenshot", "original image bytes")
	image.MediaType = "image/png"
	for _, ref := range []session.ContentReference{text, image} {
		if _, err := s.RegisterContent(t.Context(), ref); err != nil {
			t.Fatal(err)
		}
	}
	return Submission{
		SessionID: owner, Source: session.UserInput,
		Parts:         []session.Part{{Type: "text", Text: "Keep my exact text."}, {Type: "content", ReferenceID: text.ID}, {Type: "content", ReferenceID: image.ID}},
		DesignContext: &session.DesignContext{ContextAttachmentID: text.ID, ScreenshotAttachmentID: image.ID, Elements: []session.DesignContextElement{{Label: "Save", Selector: "button.save"}}, ElementCount: 1, PageURL: "https://example.test/", PageTitle: "Settings"},
	}
}

func TestDesignContextPersistsExactEvidenceAcrossRetryHistoryForkAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	request := designSubmission(t, s, owner.ID)
	identity := session.RequestIdentity{ClientID: "human", RequestID: "design"}
	accepted, err := s.Admit(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accepted.Input.DesignContext, request.DesignContext) || !reflect.DeepEqual(accepted.Input.Parts, request.Parts) {
		t.Fatal("admission changed authored evidence", accepted)
	}
	changed := request
	changed.DesignContext = request.DesignContext.Clone()
	changed.DesignContext.PageTitle = "Changed metadata"
	if _, err := s.Admit(t.Context(), identity, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("same identity accepted different metadata", err)
	}
	turn := claim(t, s, owner.ID).Turn
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	check := func(messages []session.Message, err error) {
		t.Helper()
		if err != nil || len(messages) != 1 {
			t.Fatal(messages, err)
		}
		message := messages[0]
		if !reflect.DeepEqual(message.Parts, request.Parts) || message.DesignContext == nil || !reflect.DeepEqual(message.DesignContext.DesignContext, *request.DesignContext) || message.DesignContext.ContextPartIndex != 1 || message.DesignContext.ScreenshotPartIndex == nil || *message.DesignContext.ScreenshotPartIndex != 2 {
			t.Fatal("lost exact design provenance", message)
		}
	}
	check(s.History(t.Context(), owner.ID, 0, 10))
	_, forward, err := s.HistoryPage(t.Context(), owner.ID, 0, 10, nil)
	check(forward, err)
	page, err := s.TranscriptPage(t.Context(), session.HistoryPageRequest{SessionID: owner.ID, Direction: "backward", Limit: 10})
	check(page.Messages, err)
	mustFail(t, s, "UPDATE inputs SET design_context=NULL WHERE id=?", accepted.Input.ID)
	fork := forkTest(t, s, forkRequestTest(t, s, owner.ID, "design_fork", 1))
	check(s.History(t.Context(), fork.Root.ID, 0, 10))
	mustFail(t, s, "UPDATE messages SET design_context=NULL WHERE session_id=?", fork.Root.ID)
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	check(s.History(t.Context(), fork.Root.ID, 0, 10))
	for _, ref := range []string{"context", "screenshot"} {
		if _, err := s.ContentReference(t.Context(), fork.Root.ID, ref); err != nil {
			t.Fatal("fork lost owned evidence", err)
		}
	}
	for _, inspect := range []func() (Admission, error){
		func() (Admission, error) { return s.Admit(t.Context(), identity, request) },
		func() (Admission, error) { return s.MatchSubmission(t.Context(), identity, request) },
	} {
		value, err := inspect()
		if err != nil || value.Receipt.DeletedAt == nil || value.Input != nil {
			t.Fatal("retry did not use receipt before missing owner/evidence", value, err)
		}
	}
}

func TestDesignContextRejectsInvalidReferencesWithoutAdmission(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	_, foreign := create(t, s, nil)
	base := designSubmission(t, s, owner.ID)
	for _, test := range []struct {
		name   string
		change func(*Submission)
	}{
		{"foreign owner", func(r *Submission) { r.SessionID = foreign.ID }},
		{"missing reference", func(r *Submission) { r.DesignContext.ContextAttachmentID = "missing" }},
		{"duplicate reference", func(r *Submission) { r.Parts = append(r.Parts, r.Parts[1]) }},
		{"wrong media type", func(r *Submission) {
			r.DesignContext.ContextAttachmentID = "screenshot"
			r.DesignContext.ScreenshotAttachmentID = "context"
		}},
		{"same reference", func(r *Submission) { r.DesignContext.ScreenshotAttachmentID = "context" }},
		{"agent source", func(r *Submission) { r.Source = session.AgentInput }},
		{"oversized title", func(r *Submission) { r.DesignContext.PageTitle = strings.Repeat("界", 256) }},
		{"invalid utf8", func(r *Submission) { r.DesignContext.PageURL = string([]byte{0xff}) }},
		{"too many summaries", func(r *Submission) {
			r.DesignContext.Elements = make([]session.DesignContextElement, 9)
			r.DesignContext.ElementCount = 9
		}},
		{"count underflow", func(r *Submission) { r.DesignContext.ElementCount = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.DesignContext = base.DesignContext.Clone()
			request.Parts = append([]session.Part{}, base.Parts...)
			test.change(&request)
			identity := session.RequestIdentity{ClientID: "invalid", RequestID: strings.ReplaceAll(test.name, " ", "_")}
			if _, err := s.Admit(t.Context(), identity, request); err == nil {
				t.Fatal("invalid metadata accepted")
			}
			if _, err := s.Admission(t.Context(), identity); !errors.Is(err, ErrNotFound) {
				t.Fatal("invalid admission persisted", err)
			}
		})
	}
	base.DesignContext = nil
	accepted, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "human", RequestID: "literal"}, base)
	if err != nil || accepted.Input.DesignContext != nil {
		t.Fatal("inferred metadata from literal evidence", accepted, err)
	}
	claim(t, s, owner.ID)
	messages, err := s.History(t.Context(), owner.ID, 0, 10)
	if err != nil || len(messages) != 1 || messages[0].DesignContext != nil {
		t.Fatal(messages, err)
	}
}
