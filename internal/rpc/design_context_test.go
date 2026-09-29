package rpc_test

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestDesignContextRPCPreservesMetadataAndRejectsCallerIndices(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	ref := call[protocol.ContentReference](t, c, "content.put", protocol.PutContentParams{SessionID: root.ID, ReferenceID: "context", MediaType: "text/plain", DataBase64: base64.StdEncoding.EncodeToString([]byte("Literal evidence"))})
	design := &protocol.DesignContext{ContextAttachmentID: ref.ID, Elements: []protocol.DesignContextElement{{Label: "Save"}}, ElementCount: 1, PageTitle: "Selected page"}
	params := protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "human", RequestID: "design"}, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "My request"}, {Type: "content", ReferenceID: ref.ID}}, DesignContext: design}
	accepted := call[protocol.Admission](t, c, "sessions.submit", params)
	if !reflect.DeepEqual(accepted.Input.DesignContext, design) {
		t.Fatal(accepted)
	}
	input := call[protocol.Input](t, c, "inputs.get", protocol.SessionInputParams{SessionID: root.ID, InputID: accepted.Input.ID})
	if !reflect.DeepEqual(input.DesignContext, design) {
		t.Fatal(input)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := c.Wait(ctx, params.Identity); err != nil {
		t.Fatal(err)
	}
	page := call[protocol.HistoryPageResult](t, c, "sessions.history_page", protocol.HistoryPageParams{SessionID: root.ID, Direction: "backward", Limit: 10})
	if len(page.Messages) < 1 || page.Messages[0].DesignContext == nil || page.Messages[0].DesignContext.ContextPartIndex != 1 || !reflect.DeepEqual(page.Messages[0].Parts, params.Parts) {
		t.Fatal(page)
	}
	for _, indices := range []string{"context_part_index", "screenshot_part_index"} {
		bad := map[string]any{"identity": protocol.RequestIdentity{ClientID: "human", RequestID: protocol.ID(indices)}, "session_id": root.ID, "source": "user", "parts": params.Parts, "design_context": map[string]any{"context_attachment_id": ref.ID, "elements": []any{}, "element_count": 0, indices: 1}}
		var result protocol.Admission
		if err := c.Call(t.Context(), "sessions.submit", bad, &result); err == nil {
			t.Fatal("caller supplied derived coordinates")
		}
	}
}
