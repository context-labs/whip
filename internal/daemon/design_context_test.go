package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestDesignContextAbsentDoesNotAddPresentation(t *testing.T) {
	node := storeBackedInputSession(t, t.TempDir())
	message, err := node.root.decodeInboxMessage(t.Context(), session.InboxItem{
		AgentID: node.id, Kind: "submit.parts",
		Payload: session.RuntimeValue{Inline: json.RawMessage(`{"text":"authored"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "authored" || message.Presentation != nil {
		t.Fatalf("unexpected presentation for plain submission: %+v", message)
	}
}

func TestDesignContextProvenancePersistsWithoutChangingEvidence(t *testing.T) {
	node := storeBackedInputSession(t, t.TempDir())
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	text := inputAttachment(t, node, "", "text", "text/plain", []byte("full raw context @untouched $untouched"))
	photo := inputAttachment(t, node, "", "image", "image/png", imageBytes.Bytes())
	// Misleading names must not determine presentation; unrelated parts remain separate.
	text.Name, photo.Name = "ordinary.txt", "ordinary.png"
	payload := SubmitPayload{Text: "fix this", Parts: []llm.ContentPart{{Type: "text", Text: "unrelated evidence"}}, Attachments: []protocol.InputAttachment{photo, text}, DesignContext: &llm.DesignContextInput{ContextAttachmentID: text.Content.ReferenceID, ScreenshotAttachmentID: photo.Content.ReferenceID, Elements: []llm.DesignContextElement{{Label: "Button", Selector: "#save"}}, ElementCount: 1}}
	body, _ := json.Marshal(payload)
	accepted, err := node.root.store.EnqueueInbox(t.Context(), session.InboxEnqueue{RootID: node.id, AgentID: node.id, Kind: "submit.parts", Origin: "client", Payload: session.RuntimePayload{Data: body, MediaType: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := node.root.store.LoadQueuedInbox(t.Context(), node.id, node.id, accepted.InboxSeq-1, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("inbox: %v %v", items, err)
	}
	if !reflect.DeepEqual(items[0].Preview.DesignContext, payload.DesignContext) {
		t.Fatalf("preview: %+v", items[0].Preview)
	}
	message, err := node.root.decodeInboxMessage(t.Context(), items[0])
	if err != nil {
		t.Fatal(err)
	}
	plainText, plainParts, err := node.root.resolveAttachments(t.Context(), node.id, payload)
	if err != nil || message.Content != plainText || !reflect.DeepEqual(message.Parts, plainParts) {
		t.Fatal("presentation changed evidence", err)
	}
	node.recordTranscriptMessage(message)
	if err := node.root.store.Save(node.id, 0, node.turnJournal().Messages, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	_, restored, err := node.root.store.Load(node.id)
	if err != nil || len(restored) != 1 {
		t.Fatal("reload", err)
	}
	wire, _ := json.Marshal(restored[0])
	var decoded struct {
		Content      []llm.ContentPart           `json:"content"`
		Presentation *llm.TranscriptPresentation `json:"presentation"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	d := decoded.Presentation.DesignContext
	if d.ContextPartIndex != 3 || d.ScreenshotPartIndex == nil || *d.ScreenshotPartIndex != 2 || !strings.Contains(decoded.Content[d.ContextPartIndex].Text, "full raw context @untouched $untouched") || decoded.Content[*d.ScreenshotPartIndex].Type != "image_url" || decoded.Content[1].Text != "unrelated evidence" {
		t.Fatalf("provenance: %s", wire)
	}
}

func TestDesignContextIndicesWithAndWithoutAuthoredText(t *testing.T) {
	for _, text := range []string{"", "authored"} {
		t.Run(text, func(t *testing.T) {
			payload := SubmitPayload{
				Text: text,
				Attachments: []protocol.InputAttachment{
					{Kind: "text", Content: ContentHandle{ReferenceID: "context"}},
					{Kind: "image", Content: ContentHandle{ReferenceID: "image"}},
				},
				DesignContext: &llm.DesignContextInput{ContextAttachmentID: "context", ScreenshotAttachmentID: "image"},
			}
			presentation, err := designContextPresentation(payload)
			if err != nil {
				t.Fatal(err)
			}
			message := llm.Message{Role: "user", Content: text, Parts: []llm.ContentPart{
				{Type: "text", Text: "raw context"}, llm.ImagePart("png", []byte("x")),
			}, Presentation: &presentation}
			wire, _ := json.Marshal(message)
			var restored llm.Message
			if err := json.Unmarshal(wire, &restored); err != nil {
				t.Fatal(err)
			}
			wire, _ = json.Marshal(restored)
			var decoded struct {
				Content      []llm.ContentPart           `json:"content"`
				Presentation *llm.TranscriptPresentation `json:"presentation"`
			}
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			design := decoded.Presentation.DesignContext
			if decoded.Content[design.ContextPartIndex].Text != "raw context" || decoded.Content[*design.ScreenshotPartIndex].Type != "image_url" || design.Elements == nil {
				t.Fatalf("wire indices or empty summaries: %s", wire)
			}
		})
	}
}

func TestDesignContextRejectsInvalidReferencesAndBounds(t *testing.T) {
	base := SubmitPayload{Attachments: []protocol.InputAttachment{{Kind: "text", Content: ContentHandle{ReferenceID: "context"}}, {Kind: "image", Content: ContentHandle{ReferenceID: "image"}}}, DesignContext: &llm.DesignContextInput{ContextAttachmentID: "context", ScreenshotAttachmentID: "image", Elements: []llm.DesignContextElement{{Label: "Button"}}, ElementCount: 1}}
	for _, name := range []string{"missing", "wrong-kind", "duplicate", "label", "count", "url"} {
		t.Run(name, func(t *testing.T) {
			p := base
			p.DesignContext = base.DesignContext.Clone()
			p.Attachments = append([]protocol.InputAttachment(nil), base.Attachments...)
			switch name {
			case "missing":
				p.DesignContext.ContextAttachmentID = "missing"
			case "wrong-kind":
				p.DesignContext.ContextAttachmentID = "image"
				p.DesignContext.ScreenshotAttachmentID = ""
			case "duplicate":
				p.Attachments = append(p.Attachments, p.Attachments[0])
			case "label":
				p.DesignContext.Elements[0].Label = strings.Repeat("x", 161)
			case "count":
				p.DesignContext.ElementCount = 0
			case "url":
				p.DesignContext.PageURL = strings.Repeat("x", 2049)
			}
			if _, err := designContextPresentation(p); !errors.Is(err, session.ErrInvalidInput) {
				t.Fatalf("accepted invalid descriptor: %v", err)
			}
		})
	}
}
